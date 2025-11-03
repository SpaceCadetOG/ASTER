package aster

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go-machine/internal/types"
)

func tfDuration(tf types.TF) time.Duration {
	switch tf {
	case types.TF1m:
		return time.Minute
	case types.TF5m:
		return 5 * time.Minute
	case types.TF15m:
		return 15 * time.Minute
	case types.TF1h:
		return time.Hour
	case types.TF4h:
		return 4 * time.Hour
	default:
		return 5 * time.Minute
	}
}

const pageLimit = 200 // safer cap; many gateways dislike big limits with time filters

// LoadCandlesRange fetches mark-price klines over [start,end], using adaptive paging.
// It tries (1) startTime+limit, (2) startTime+endTime (no limit), (3) endTime+limit (paging backward).
// Falls back to /klines if /markPriceKlines is unavailable.
func (c *Client) LoadCandlesRange(symbol string, tf types.TF, start, end time.Time) ([]types.Candle, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol required")
	}
	if !end.After(start) {
		return nil, fmt.Errorf("end must be after start")
	}
	interval, err := tfToInterval(tf)
	if err != nil {
		return nil, err
	}
	tfDur := tfDuration(tf)

	// --- attempt 1: forward page with startTime+limit ---
	out, err := c.pageForward(symbol, interval, tfDur, start, end, true /*useMark*/)
	if err == nil && len(out) > 0 {
		return out, nil
	}
	if isLimitComplaint(err) {
		// Retry same pattern but on /klines
		out, err = c.pageForwardKlines(symbol, interval, tfDur, start, end)
		if err == nil && len(out) > 0 {
			return out, nil
		}
	}

	// --- attempt 2: single shot with startTime+endTime (no limit) ---
	out, err = c.singleWindow(symbol, interval, start, end, true /*useMark*/)
	if err == nil && len(out) > 0 {
		return out, nil
	}
	if isLimitComplaint(err) || isNotFound(err) {
		out, err = c.singleWindow(symbol, interval, start, end, false /*useMark->/klines*/)
		if err == nil && len(out) > 0 {
			return out, nil
		}
	}

	// --- attempt 3: backward page using endTime+limit ---
	out, err = c.pageBackward(symbol, interval, tfDur, start, end, true /*useMark*/)
	if err == nil && len(out) > 0 {
		return out, nil
	}
	if isLimitComplaint(err) || isNotFound(err) {
		out, err = c.pageBackward(symbol, interval, tfDur, start, end, false /*/klines*/)
		if err == nil && len(out) > 0 {
			return out, nil
		}
	}

	// As a last resort, fetch recent N bars (no times) and filter.
	out, err = c.recentAndFilter(symbol, interval, tfDur, start, end, true /*useMark*/)
	if err == nil && len(out) > 0 {
		return out, nil
	}
	out, err = c.recentAndFilter(symbol, interval, tfDur, start, end, false /*/klines*/)
	if err == nil && len(out) > 0 {
		return out, nil
	}

	return nil, fmt.Errorf("unable to load candles for %s (%s..%s): %v", symbol, start, end, err)
}

// ---------- strategies ----------

func (c *Client) pageForward(symbol, interval string, tfDur time.Duration, start, end time.Time, useMark bool) ([]types.Candle, error) {
	base := "/markPriceKlines"
	if !useMark {
		base = "/klines"
	}
	cursor := start
	var out []types.Candle

	for cursor.Before(end) {
		u, _ := url.Parse(c.BaseURL + base)
		q := u.Query()
		q.Set("symbol", symbol)
		q.Set("interval", interval)
		q.Set("startTime", strconv.FormatInt(cursor.UnixMilli(), 10))
		q.Set("limit", strconv.Itoa(pageLimit))
		u.RawQuery = q.Encode()

		raw, err := c.fetchKlineArray(u.String())
		if err != nil {
			return nil, err
		}
		added, lastTS := 0, int64(0)
		for _, r := range raw {
			cdl, ok := klineRowToCandle(r)
			if !ok {
				continue
			}
			if cdl.T.Before(start) || cdl.T.After(end) {
				continue
			}
			out = append(out, cdl)
			added++
			lastTS = cdl.T.UnixMilli()
		}
		if added == 0 {
			// bump by a page to avoid infinite loop
			cursor = cursor.Add(tfDur * pageLimit)
		} else {
			next := time.UnixMilli(lastTS).Add(tfDur)
			if !next.After(cursor) {
				next = cursor.Add(tfDur * pageLimit)
			}
			cursor = next
		}
	}
	return out, nil
}

func (c *Client) pageForwardKlines(symbol, interval string, tfDur time.Duration, start, end time.Time) ([]types.Candle, error) {
	return c.pageForward(symbol, interval, tfDur, start, end, false)
}

func (c *Client) singleWindow(symbol, interval string, start, end time.Time, useMark bool) ([]types.Candle, error) {
	base := "/markPriceKlines"
	if !useMark {
		base = "/klines"
	}
	u, _ := url.Parse(c.BaseURL + base)
	q := u.Query()
	q.Set("symbol", symbol)
	q.Set("interval", interval)
	q.Set("startTime", strconv.FormatInt(start.UnixMilli(), 10))
	q.Set("endTime", strconv.FormatInt(end.UnixMilli(), 10))
	// NO limit here (important)
	u.RawQuery = q.Encode()

	raw, err := c.fetchKlineArray(u.String())
	if err != nil {
		return nil, err
	}
	out := make([]types.Candle, 0, len(raw))
	for _, r := range raw {
		cdl, ok := klineRowToCandle(r)
		if ok && !cdl.T.Before(start) && !cdl.T.After(end) {
			out = append(out, cdl)
		}
	}
	return out, nil
}

func (c *Client) pageBackward(symbol, interval string, tfDur time.Duration, start, end time.Time, useMark bool) ([]types.Candle, error) {
	base := "/markPriceKlines"
	if !useMark {
		base = "/klines"
	}
	cursor := end
	var stash []types.Candle

	for cursor.After(start) {
		u, _ := url.Parse(c.BaseURL + base)
		q := u.Query()
		q.Set("symbol", symbol)
		q.Set("interval", interval)
		q.Set("endTime", strconv.FormatInt(cursor.UnixMilli(), 10))
		q.Set("limit", strconv.Itoa(pageLimit))
		u.RawQuery = q.Encode()

		raw, err := c.fetchKlineArray(u.String())
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			break
		}
		added := 0
		firstTS := int64(0)
		for _, r := range raw {
			cdl, ok := klineRowToCandle(r)
			if !ok {
				continue
			}
			if cdl.T.Before(start) || cdl.T.After(end) {
				continue
			}
			stash = append(stash, cdl)
			added++
			if firstTS == 0 || cdl.T.UnixMilli() < firstTS {
				firstTS = cdl.T.UnixMilli()
			}
		}
		if added == 0 {
			cursor = cursor.Add(-tfDur * pageLimit)
		} else {
			prev := time.UnixMilli(firstTS).Add(-tfDur)
			if !prev.Before(cursor) {
				prev = cursor.Add(-tfDur * pageLimit)
			}
			cursor = prev
		}
	}
	// reverse stash to chronological
	for i, j := 0, len(stash)-1; i < j; i, j = i+1, j-1 {
		stash[i], stash[j] = stash[j], stash[i]
	}
	return stash, nil
}

func (c *Client) recentAndFilter(symbol, interval string, tfDur time.Duration, start, end time.Time, useMark bool) ([]types.Candle, error) {
	base := "/markPriceKlines"
	if !useMark {
		base = "/klines"
	}
	u, _ := url.Parse(c.BaseURL + base)
	q := u.Query()
	q.Set("symbol", symbol)
	q.Set("interval", interval)
	q.Set("limit", strconv.Itoa(pageLimit))
	u.RawQuery = q.Encode()

	raw, err := c.fetchKlineArray(u.String())
	if err != nil {
		return nil, err
	}
	var out []types.Candle
	for _, r := range raw {
		cdl, ok := klineRowToCandle(r)
		if ok && !cdl.T.Before(start) && !cdl.T.After(end) {
			out = append(out, cdl)
		}
	}
	return out, nil
}

// ---------- lower-level helpers ----------

func (c *Client) fetchKlineArray(fullURL string) ([][]any, error) {
	req, _ := http.NewRequest(http.MethodGet, fullURL, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aster request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	var raw [][]any
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode klines: %w", err)
	}
	return raw, nil
}

func klineRowToCandle(r []any) (types.Candle, bool) {
	if len(r) < 6 {
		return types.Candle{}, false
	}
	ot, e1 := anyToInt64(r[0])
	o, e2 := anyToFloat(r[1])
	h, e3 := anyToFloat(r[2])
	l, e4 := anyToFloat(r[3])
	cx, e5 := anyToFloat(r[4])
	v, e6 := anyToFloat(r[5])
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil {
		return types.Candle{}, false
	}
	return types.Candle{
		T: time.UnixMilli(ot),
		O: o, H: h, L: l, C: cx, V: v,
	}, true
}

func isLimitComplaint(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "'limit' is not valid") || strings.Contains(s, "code\":-1130")
}
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "status 404")
}
