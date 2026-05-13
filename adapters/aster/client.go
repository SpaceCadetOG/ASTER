package aster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-machine/internal/market"
	"go-machine/internal/ratelimit"
	"go-machine/internal/types"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
	RL      *ratelimit.Limiter // 10 req/s, burst 20

	openMu    sync.RWMutex
	openCache map[string]map[string]float64
}

func New(base string) *Client {
	if base == "" {
		base = "https://fapi.asterdex.com/fapi/v1"
	}
	return &Client{
		BaseURL: base,
		HTTP: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}).DialContext,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   5 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		},
		RL:        ratelimit.New(10, 20),
		openCache: map[string]map[string]float64{},
	}
}

func (c *Client) Name() string { return "asterdex" }

func (c *Client) buildURL(endpoint string, params map[string]string) string {
	u, err := url.Parse(c.BaseURL + endpoint)
	if err != nil {
		panic(fmt.Sprintf("invalid base URL or endpoint: %v", err))
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Client) fetchJSON(fullURL string, target interface{}) error {
	if c.RL != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.RL.Wait(ctx); err != nil {
			return fmt.Errorf("rate limit: %w", err)
		}
	}
	resp, err := c.HTTP.Get(fullURL)
	if err != nil {
		return fmt.Errorf("http GET failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	return dec.Decode(target)
}

func numToFloat(n json.Number) (float64, error) { return strconv.ParseFloat(n.String(), 64) }
func numToFloatOK(n json.Number) (float64, bool) {
	if n == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(n.String(), 64)
	return f, err == nil
}

// NormalizeFunding: keep as fraction (0.0008 = 0.08%)
func NormalizeFunding(x float64) float64 {
	if x > 0.5 || x < -0.5 {
		return x / 100.0
	}
	return x
}

func deriveOpen(last, pct float64) float64 {
	den := 1 + pct/100.0
	if den == 0 {
		return 0
	}
	return last / den
}

func topEnrichmentSymbols(mkts []market.Market, perSide int) []string {
	if perSide <= 0 {
		perSide = 12
	}
	longTop := market.TopN(market.ScoreAndFilter(mkts), perSide)
	shortTop := market.TopN(market.ScoreAndFilterShort(mkts), perSide)
	seen := make(map[string]struct{}, len(longTop)+len(shortTop))
	out := make([]string, 0, len(longTop)+len(shortTop))
	appendUnique := func(rows []market.Scored) {
		for _, row := range rows {
			if _, ok := seen[row.Symbol]; ok {
				continue
			}
			seen[row.Symbol] = struct{}{}
			out = append(out, row.Symbol)
		}
	}
	appendUnique(longTop)
	appendUnique(shortTop)
	return out
}

func (c *Client) fetchAll24h() ([]tickerStats, error) {
	var arr []tickerStats
	if err := c.fetchJSON(c.buildURL("/ticker/24hr", nil), &arr); err == nil {
		return arr, nil
	}
	// Some deployments return a single object when no params provided; try anyway
	var one tickerStats
	if err := c.fetchJSON(c.buildURL("/ticker/24hr", nil), &one); err == nil {
		return []tickerStats{one}, nil
	}
	return nil, fmt.Errorf("/ticker/24hr fetch failed")
}

func (c *Client) fundingLatest(symbol string) (*float64, error) {
	var fr []fundingRateEntry
	if err := c.fetchJSON(c.buildURL("/fundingRate", map[string]string{"symbol": symbol, "limit": "1"}), &fr); err != nil {
		return nil, err
	}
	if len(fr) == 0 {
		return nil, nil
	}
	latest := fr[0]
	for _, e := range fr[1:] {
		if e.FundingTime > latest.FundingTime {
			latest = e
		}
	}
	r, err := numToFloat(latest.FundingRate)
	if err != nil {
		return nil, nil
	}
	norm := NormalizeFunding(r)
	return &norm, nil
}

func (c *Client) priceTicker(symbol string) (float64, error) {
	var pt priceTicker
	if err := c.fetchJSON(c.buildURL("/ticker/price", map[string]string{"symbol": symbol}), &pt); err != nil {
		return 0, err
	}
	return numToFloat(pt.Price)
}

func (c *Client) openInterestUSD(symbol string, lastPrice float64) (*float64, error) {
	if lastPrice <= 0 {
		return nil, nil
	}
	var oi openInterestEntry
	if err := c.fetchJSON(c.buildURL("/openInterest", map[string]string{"symbol": symbol}), &oi); err != nil {
		return nil, err
	}
	v, err := numToFloat(oi.OpenInterest)
	if err != nil || v <= 0 {
		return nil, nil
	}
	usd := v * lastPrice
	return &usd, nil
}

func utcDayKey(t time.Time) string { return t.UTC().Format("2006-01-02") }

func utcBucketStart(t time.Time, d time.Duration) time.Time {
	u := t.UTC()
	switch {
	case d >= 24*time.Hour:
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	case d > 0:
		secs := int64(d / time.Second)
		if secs <= 0 {
			return u
		}
		floored := u.Unix() - (u.Unix() % secs)
		return time.Unix(floored, 0).UTC()
	default:
		return u
	}
}

func (c *Client) bucketOpenUTC(symbol string, now time.Time, bucket time.Duration, bucketName string) (float64, error) {
	start := utcBucketStart(now, bucket)
	cacheKey := bucketName + "|" + start.Format(time.RFC3339)

	c.openMu.RLock()
	if bySymbol, ok := c.openCache[cacheKey]; ok {
		if px, ok := bySymbol[symbol]; ok && px > 0 {
			c.openMu.RUnlock()
			return px, nil
		}
	}
	c.openMu.RUnlock()

	params := map[string]string{
		"symbol":    symbol,
		"interval":  "1m",
		"startTime": strconv.FormatInt(start.UnixMilli(), 10),
		"limit":     "1",
	}
	u := c.buildURL("/klines", params)
	var arr [][]json.Number
	if err := c.fetchJSON(u, &arr); err != nil {
		return 0, err
	}
	if len(arr) == 0 || len(arr[0]) < 2 {
		return 0, fmt.Errorf("no klines at utc open for %s", symbol)
	}
	px, err := numToFloat(arr[0][1]) // open
	if err != nil || px <= 0 {
		return 0, fmt.Errorf("bad %s open for %s", bucketName, symbol)
	}

	c.openMu.Lock()
	if _, ok := c.openCache[cacheKey]; !ok {
		c.openCache[cacheKey] = map[string]float64{}
	}
	c.openCache[cacheKey][symbol] = px
	c.openMu.Unlock()
	return px, nil
}

func (c *Client) dayOpenUTC(symbol string, now time.Time) (float64, error) {
	return c.bucketOpenUTC(symbol, now, 24*time.Hour, "1d")
}

func (c *Client) open4hUTC(symbol string, now time.Time) (float64, error) {
	return c.bucketOpenUTC(symbol, now, 4*time.Hour, "4h")
}

func (c *Client) open1hUTC(symbol string, now time.Time) (float64, error) {
	return c.bucketOpenUTC(symbol, now, time.Hour, "1h")
}

// ToMarket maps raw stats to internal market.Market
func toMarket(exchange string, ts tickerStats, funding *float64) market.Market {
	pct, _ := numToFloat(ts.PriceChangePercent)
	vol, _ := numToFloat(ts.QuoteVolume)
	open, okOpen := numToFloatOK(ts.OpenPrice)
	last, okLast := numToFloatOK(ts.LastPrice)
	if !okOpen && okLast {
		open = deriveOpen(last, pct)
	}

	m := market.Market{
		Exchange:  exchange,
		Symbol:    NormSymbol(ts.Symbol),
		Change24h: pct,
		VolumeUSD: vol,
		OpenPrice: open,
		LastPrice: last,
	}
	if funding != nil {
		m.FundingRate = funding
	}
	return m
}

// FetchAllMarkets scans the full DEX, filters by quote assets, enriches top candidates.
// If quoteAssets is nil/empty, defaults to ["USDT", "USD"].
func (c *Client) FetchAllMarkets(quoteAssets ...string) []market.Market {
	if len(quoteAssets) == 0 {
		quoteAssets = []string{"USDT", "USD"}
	}

	rows, err := c.fetchAll24h()
	if err != nil || len(rows) == 0 {
		return nil
	}

	mkts := make([]market.Market, 0, len(rows))
	for _, ts := range rows {
		if !MatchesQuoteAsset(ts.Symbol, quoteAssets) {
			continue
		}
		mkts = append(mkts, toMarket(c.Name(), ts, nil))
	}

	// Map for quick lookup
	idx := map[string]int{}
	for i := range mkts {
		idx[mkts[i].Symbol] = i
	}

	for _, dashed := range topEnrichmentSymbols(mkts, 12) {
		raw := strings.ReplaceAll(dashed, "-USD", "USDT")

		if f, err := c.fundingLatest(raw); err == nil && f != nil {
			if i, ok := idx[dashed]; ok {
				mkts[i].FundingRate = f
			}
		}
		if p, err := c.priceTicker(raw); err == nil && p > 0 {
			if i, ok := idx[dashed]; ok {
				mkts[i].LastPrice = p
				if oiUSD, err := c.openInterestUSD(raw, p); err == nil && oiUSD != nil {
					mkts[i].OIUSD = oiUSD
				}
				if dayOpen, err := c.dayOpenUTC(raw, time.Now().UTC()); err == nil && dayOpen > 0 {
					mkts[i].OpenPrice = dayOpen
					v := (p/dayOpen - 1.0) * 100.0
					mkts[i].DayUTC24h = &v
				} else if mkts[i].OpenPrice == 0 {
					mkts[i].OpenPrice = deriveOpen(p, mkts[i].Change24h)
				}
				if open4h, err := c.open4hUTC(raw, time.Now().UTC()); err == nil && open4h > 0 {
					mkts[i].Open4hUTC = open4h
					v := (p/open4h - 1.0) * 100.0
					mkts[i].UTC4hPct = &v
				}
				if open1h, err := c.open1hUTC(raw, time.Now().UTC()); err == nil && open1h > 0 {
					mkts[i].Open1hUTC = open1h
					v := (p/open1h - 1.0) * 100.0
					mkts[i].UTC1hPct = &v
				}
			}
		}
	}

	return mkts
}

// FetchAllMarketsLite fetches only the 24h ticker snapshot and skips per-symbol
// enrichment calls. It is intended for WS-first scanner mode where live quotes
// are sourced from websocket streams.
func (c *Client) FetchAllMarketsLite(quoteAssets ...string) []market.Market {
	if len(quoteAssets) == 0 {
		quoteAssets = []string{"USDT", "USD"}
	}
	rows, err := c.fetchAll24h()
	if err != nil || len(rows) == 0 {
		return nil
	}
	mkts := make([]market.Market, 0, len(rows))
	for _, ts := range rows {
		if !MatchesQuoteAsset(ts.Symbol, quoteAssets) {
			continue
		}
		m := toMarket(c.Name(), ts, nil)
		// In lite mode, approximate UTC day move with 24h change until WS/replay overlay refines it.
		v := m.Change24h
		m.DayUTC24h = &v
		mkts = append(mkts, m)
	}
	return mkts
}

// ---- Candle loader (mark-price klines) ----

// LoadCandles is a convenience wrapper that uses a default client (base URL = Aster).
// It matches the call site in your /api/candles handler: aster.LoadCandles(...)
func LoadCandles(symbol string, tf types.TF, n int) ([]types.Candle, error) {
	return New("").LoadCandles(symbol, tf, n)
}

// LoadCandles (method) fetches mark-price klines and normalizes to []types.Candle.
// Mark series tends to be smoother for ATR/RR planning. Switch to "/klines" if you
// want raw trade OHLCV instead.
func (c *Client) LoadCandle(symbol string, tf types.TF, n int) ([]types.Candle, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol required")
	}
	if n <= 0 {
		n = 200
	}
	interval, err := tfToInterval(tf)
	if err != nil {
		return nil, err
	}

	// Aster mark-price klines (Binance-like schema: [][]any)
	u, _ := url.Parse(c.BaseURL + "/markPriceKlines")
	q := u.Query()
	q.Set("symbol", symbol) // e.g., BTCUSDT
	q.Set("interval", interval)
	q.Set("limit", strconv.Itoa(n))
	u.RawQuery = q.Encode()

	req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aster request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("aster status %d: %s", resp.StatusCode, string(b))
	}

	// Expect: [
	//   [ openTime, "open", "high", "low", "close", "volume", closeTime, ... ],
	//   ...
	// ]
	var raw [][]any
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode klines: %w", err)
	}

	out := make([]types.Candle, 0, len(raw))
	for _, r := range raw {
		if len(r) < 6 {
			continue
		}
		ot, err1 := anyToInt64(r[0]) // ms
		o, err2 := anyToFloat(r[1])
		h, err3 := anyToFloat(r[2])
		l, err4 := anyToFloat(r[3])
		cpx, err5 := anyToFloat(r[4])
		v, err6 := anyToFloat(r[5])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
			continue // skip malformed rows
		}
		out = append(out, types.Candle{
			T: time.UnixMilli(ot),
			O: o, H: h, L: l, C: cpx, V: v,
		})
	}
	return out, nil
}

func tfToInterval(tf types.TF) (string, error) {
	switch tf {
	case types.TF1m:
		return "1m", nil
	case types.TF5m:
		return "5m", nil
	case types.TF15m:
		return "15m", nil
	case types.TF30m:
		return "30m", nil
	case types.TF1h:
		return "1h", nil
	case types.TF4h:
		return "4h", nil
	case types.TF1d:
		return "1d", nil
	default:
		return "", fmt.Errorf("unsupported TF: %s", tf)
	}
}

// Helpers to parse Aster's mixed-type JSON arrays safely.
func anyToFloat(x any) (float64, error) {
	switch t := x.(type) {
	case string:
		return strconv.ParseFloat(t, 64)
	case float64:
		return t, nil
	case json.Number:
		return t.Float64()
	default:
		return 0, fmt.Errorf("unexpected number type %T", x)
	}
}

func anyToInt64(x any) (int64, error) {
	switch t := x.(type) {
	case string:
		return strconv.ParseInt(t, 10, 64)
	case float64:
		return int64(t), nil
	case json.Number:
		return t.Int64()
	default:
		return 0, fmt.Errorf("unexpected int type %T", x)
	}
}
