package aster

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Trader is a convenience wrapper around RESTAuth that provides the
// higher-level execution helpers you prototyped in Python (market/limit,
// reduce-only closes, brackets, wait-for-fill).
type Trader struct {
	Symbol   string
	SymbolLC string
	Rest     *RESTAuth
	Meta     SymbolMeta

	// Optional shared market state fed by StreamClient. If nil, some helpers
	// require refPrice to be passed in.
	State *MarketState
}

func NewTrader(symbol, apiKey, apiSecret string, state *MarketState) (*Trader, error) {
	if symbol == "" {
		return nil, errors.New("symbol required")
	}
	rest := NewRESTAuth(apiKey, apiSecret)
	meta, err := rest.SymbolMeta(symbol, false)
	if err != nil {
		return nil, err
	}
	return &Trader{
		Symbol:   strings.ToUpper(symbol),
		SymbolLC: strings.ToLower(symbol),
		Rest:     rest,
		Meta:     meta,
		State:    state,
	}, nil
}

func (t *Trader) qtyFromUSD(usd, refPrice float64) (float64, error) {
	if refPrice <= 0 {
		return 0, errors.New("refPrice <= 0")
	}
	raw := usd / refPrice
	q, _, err := t.Rest.RoundQty(t.Symbol, raw)
	if err != nil {
		return 0, err
	}
	if q <= 0 {
		return 0, errors.New("qty <= 0 (check stepSize/usd/price)")
	}
	return q, nil
}

func (t *Trader) EnterLimitUSD(side string, usd float64, limitPrice float64, leverage *int) (map[string]any, error) {
	if leverage != nil {
		_, _ = t.Rest.ChangeLeverage(t.Symbol, *leverage)
	}
	q, err := t.qtyFromUSD(usd, limitPrice)
	if err != nil {
		return nil, err
	}
	px, _, err := t.Rest.RoundPrice(t.Symbol, limitPrice)
	if err != nil {
		return nil, err
	}
	vals := url.Values{}
	vals.Set("symbol", t.Symbol)
	vals.Set("side", strings.ToUpper(side))
	vals.Set("type", "LIMIT")
	vals.Set("timeInForce", "GTC")
	vals.Set("quantity", formatFloat(q, t.Meta.QtyPrecision))
	vals.Set("price", formatFloat(px, t.Meta.PricePrecision))
	vals.Set("reduceOnly", "false")
	vals.Set("positionSide", "BOTH")
	return t.Rest.PlaceOrder(vals)
}

func (t *Trader) EnterMarketUSD(side string, usd float64, refPrice *float64, leverage *int) (map[string]any, error) {
	if leverage != nil {
		_, _ = t.Rest.ChangeLeverage(t.Symbol, *leverage)
	}

	rp := 0.0
	if refPrice != nil {
		rp = *refPrice
	} else if t.State != nil {
		// use book ticker side
		b, a, _ := t.Rest.BookTicker(t.Symbol)
		if strings.ToUpper(side) == "BUY" {
			rp = a
		} else {
			rp = b
		}
		if rp == 0 {
			rp = t.State.Mid()
		}
	}
	if rp == 0 {
		return nil, errors.New("no ref price available (pass refPrice or attach StreamClient)")
	}

	q, err := t.qtyFromUSD(usd, rp)
	if err != nil {
		return nil, err
	}
	vals := url.Values{}
	vals.Set("symbol", t.Symbol)
	vals.Set("side", strings.ToUpper(side))
	vals.Set("type", "MARKET")
	vals.Set("quantity", formatFloat(q, t.Meta.QtyPrecision))
	vals.Set("reduceOnly", "false")
	vals.Set("positionSide", "BOTH")
	return t.Rest.PlaceOrder(vals)
}

// ClosePositionMarket closes the current position for the symbol (reduce-only).
func (t *Trader) ClosePositionMarket() (map[string]any, error) {
	pos, err := t.currentPosition()
	if err != nil {
		return nil, err
	}
	if pos == nil {
		return nil, nil
	}
	qty, _, err := t.Rest.RoundQty(t.Symbol, mathAbs(pos.Amount))
	if err != nil {
		return nil, err
	}
	side := "SELL"
	if pos.Amount < 0 {
		side = "BUY"
	}

	vals := url.Values{}
	vals.Set("symbol", t.Symbol)
	vals.Set("side", side)
	vals.Set("type", "MARKET")
	vals.Set("quantity", formatFloat(qty, t.Meta.QtyPrecision))
	vals.Set("reduceOnly", "true")
	vals.Set("positionSide", "BOTH")
	return t.Rest.PlaceOrder(vals)
}

// ClosePositionLimit places a reduce-only LIMIT to close.
func (t *Trader) ClosePositionLimit(limitPrice float64) (map[string]any, error) {
	pos, err := t.currentPosition()
	if err != nil {
		return nil, err
	}
	if pos == nil {
		return nil, nil
	}
	qty, _, err := t.Rest.RoundQty(t.Symbol, mathAbs(pos.Amount))
	if err != nil {
		return nil, err
	}
	side := "SELL"
	if pos.Amount < 0 {
		side = "BUY"
	}
	px, _, err := t.Rest.RoundPrice(t.Symbol, limitPrice)
	if err != nil {
		return nil, err
	}

	vals := url.Values{}
	vals.Set("symbol", t.Symbol)
	vals.Set("side", side)
	vals.Set("type", "LIMIT")
	vals.Set("timeInForce", "GTC")
	vals.Set("quantity", formatFloat(qty, t.Meta.QtyPrecision))
	vals.Set("price", formatFloat(px, t.Meta.PricePrecision))
	vals.Set("reduceOnly", "true")
	vals.Set("positionSide", "BOTH")
	return t.Rest.PlaceOrder(vals)
}

// Bracket places reduce-only TP/SL triggers (TAKE_PROFIT_MARKET and STOP_MARKET).
func (t *Trader) Bracket(isLong bool, qty float64, tpPrice *float64, slPrice *float64) (map[string]map[string]any, error) {
	closeSide := "SELL"
	if !isLong {
		closeSide = "BUY"
	}
	q, _, err := t.Rest.RoundQty(t.Symbol, qty)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{"tp": nil, "sl": nil}

	if tpPrice != nil {
		tp, _, err := t.Rest.RoundPrice(t.Symbol, *tpPrice)
		if err != nil {
			return nil, err
		}
		vals := url.Values{}
		vals.Set("symbol", t.Symbol)
		vals.Set("side", closeSide)
		vals.Set("type", "TAKE_PROFIT_MARKET")
		vals.Set("quantity", formatFloat(q, t.Meta.QtyPrecision))
		vals.Set("stopPrice", formatFloat(tp, t.Meta.PricePrecision))
		vals.Set("reduceOnly", "true")
		vals.Set("positionSide", "BOTH")
		resp, err := t.Rest.PlaceOrder(vals)
		if err != nil {
			return nil, err
		}
		out["tp"] = resp
	}

	if slPrice != nil {
		sl, _, err := t.Rest.RoundPrice(t.Symbol, *slPrice)
		if err != nil {
			return nil, err
		}
		vals := url.Values{}
		vals.Set("symbol", t.Symbol)
		vals.Set("side", closeSide)
		vals.Set("type", "STOP_MARKET")
		vals.Set("quantity", formatFloat(q, t.Meta.QtyPrecision))
		vals.Set("stopPrice", formatFloat(sl, t.Meta.PricePrecision))
		vals.Set("reduceOnly", "true")
		vals.Set("positionSide", "BOTH")
		resp, err := t.Rest.PlaceOrder(vals)
		if err != nil {
			return nil, err
		}
		out["sl"] = resp
	}
	return out, nil
}

func (t *Trader) CancelAll() (map[string]any, error) {
	return t.Rest.CancelAllOrders(t.Symbol)
}

func (t *Trader) Flatten() (map[string]any, error) {
	out := map[string]any{"cancel": nil, "close": nil}
	if c, err := t.CancelAll(); err == nil {
		out["cancel"] = c
	}
	cl, err := t.ClosePositionMarket()
	if err != nil {
		return nil, err
	}
	out["close"] = cl
	return out, nil
}

func (t *Trader) WaitForFill(orderID int64, timeout time.Duration, poll time.Duration) (map[string]any, error) {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	if poll <= 0 {
		poll = 350 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	var last map[string]any
	for time.Now().Before(deadline) {
		o, err := t.Rest.GetOrder(t.Symbol, orderID)
		if err != nil {
			last = map[string]any{"error": err.Error()}
		} else {
			last = o
			if st, ok := o["status"].(string); ok {
				up := strings.ToUpper(st)
				if up == "FILLED" || up == "CANCELED" || up == "REJECTED" || up == "EXPIRED" {
					return o, nil
				}
			}
		}
		time.Sleep(poll)
	}
	if last == nil {
		return nil, fmt.Errorf("no order state")
	}
	return last, nil
}

// ---- helpers ----

type position struct {
	Amount    float64
	EntryPx   float64
	MarkPx    float64
	IsLong    bool
	RawSymbol string
}

func (t *Trader) currentPosition() (*position, error) {
	rows, err := t.Rest.PositionRisk(t.Symbol)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		// positionAmt is a string in payload
		amt, _ := parseAnyFloat(r["positionAmt"])
		if amt == 0 {
			continue
		}
		ep, _ := parseAnyFloat(r["entryPrice"])
		mp, _ := parseAnyFloat(r["markPrice"])
		sym, _ := r["symbol"].(string)
		return &position{Amount: amt, EntryPx: ep, MarkPx: mp, IsLong: amt > 0, RawSymbol: sym}, nil
	}
	return nil, nil
}

func parseAnyFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func formatFloat(v float64, prec int) string {
	if prec < 0 {
		prec = 0
	}
	return strconv.FormatFloat(v, 'f', prec, 64)
}
