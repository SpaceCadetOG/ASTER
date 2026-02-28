// cmd/exec/main.go
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"go-machine/adapters/aster"
)

// cmd/exec = single control tool for Aster.
//
// Required:
//
//	ASTER_API_KEY / ASTER_API_SECRET
//
// Core:
//
//	EXEC_ACTION=place|cancel|cancel_all|status|open_orders|position|close_market|close_limit   (default: place)
//	EXEC_SYMBOL=BTCUSDT                                                                       (default: BTCUSDT)
//	DRY_RUN=1                                                                                 (default: 1)
//	EXEC_DEBUG=0|1                                                                            (default: 0)
//
// Place (EXEC_ACTION=place):
//
//	EXEC_SIDE=BUY|SELL                      (default: BUY)
//	EXEC_KIND=LIMIT|MARKET                  (default: LIMIT)
//	EXEC_USD=50                             (default: 50)  // ignored if EXEC_QTY set
//	EXEC_QTY=0.0                            (optional)     // overrides EXEC_USD conversion
//	EXEC_PRICE=65000                        (LIMIT only; optional if EXEC_AT used)
//	EXEC_AT=bid|ask|mid                     (optional; used when EXEC_PRICE not provided)
//	EXEC_OFFSET_BPS=-1                      (optional; +bps/-bps around EXEC_AT)
//	EXEC_OFFSET_PCT=-0.05                   (optional; percent, used if BPS not set)
//	EXEC_TIF=GTC                            (default: GTC; LIMIT only)
//	EXEC_REDUCE_ONLY=0|1                    (default: 0)
//	EXEC_LEV=3                              (optional)
//	EXEC_MIN_NOTIONAL=0                     (optional; if set, auto-bump qty to satisfy)
//
// Cancel/Status:
//
//	EXEC_ORDER_ID=2275537148                (required for cancel/status)
//
// Close (reduce-only):
//
//	EXEC_CLOSE_PCT=100                      (optional; closes % of position)
func main() {
	key := strings.TrimSpace(os.Getenv("ASTER_API_KEY"))
	sec := strings.TrimSpace(os.Getenv("ASTER_API_SECRET"))
	if key == "" || sec == "" {
		fmt.Println("missing ASTER_API_KEY / ASTER_API_SECRET")
		os.Exit(2)
	}

	action := strings.ToLower(strings.TrimSpace(os.Getenv("EXEC_ACTION")))
	if action == "" {
		action = "place"
	}

	symbol := strings.ToUpper(strings.TrimSpace(os.Getenv("EXEC_SYMBOL")))
	if symbol == "" {
		symbol = "BTCUSDT"
	}

	dry := true
	if v := strings.TrimSpace(os.Getenv("DRY_RUN")); v != "" {
		dry = v != "0" && strings.ToLower(v) != "false"
	}

	debug := false
	if v := strings.TrimSpace(os.Getenv("EXEC_DEBUG")); v != "" {
		debug = v != "0" && strings.ToLower(v) != "false"
	}

	rest := aster.NewRESTAuth(key, sec)
	_ = rest.SyncTime() // best-effort

	// Non-place actions
	switch action {
	case "cancel":
		oid := mustInt64Env("EXEC_ORDER_ID")
		if dry {
			fmt.Printf("DRY_RUN=1 would cancel: symbol=%s orderId=%d\n", symbol, oid)
			return
		}
		out, err := rest.CancelOrder(symbol, oid)
		mustPrintJSON(out, err)
		return

	case "cancel_all":
		if dry {
			fmt.Printf("DRY_RUN=1 would cancel_all: symbol=%s\n", symbol)
			return
		}
		out, err := rest.CancelAllOrders(symbol)
		mustPrintJSON(out, err)
		return

	case "status":
		oid := mustInt64Env("EXEC_ORDER_ID")
		out, err := rest.GetOrder(symbol, oid)
		mustPrintJSON(out, err)
		return

	case "open_orders":
		out, err := rest.OpenOrders(symbol)
		mustPrintJSON(out, err)
		return

	case "position":
		out, err := rest.PositionRisk(symbol)
		mustPrintJSON(out, err)
		return

	case "close_market":
		closeMarket(rest, symbol, dry, debug)
		return

	case "close_limit":
		closeLimit(rest, symbol, dry, debug)
		return

	case "place":
		// continue below
	default:
		fmt.Println("unknown EXEC_ACTION (place|cancel|cancel_all|status|open_orders|position|close_market|close_limit)")
		os.Exit(2)
	}

	// ---- PLACE ----

	side := strings.ToUpper(strings.TrimSpace(os.Getenv("EXEC_SIDE")))
	if side == "" {
		side = "BUY"
	}
	kind := strings.ToUpper(strings.TrimSpace(os.Getenv("EXEC_KIND")))
	if kind == "" {
		kind = "LIMIT"
	}

	usd := floatEnv("EXEC_USD", 50.0)
	qtyOverride := floatEnv("EXEC_QTY", 0.0)

	lev := intEnv("EXEC_LEV", 0)
	reduceOnly := boolEnv("EXEC_REDUCE_ONLY", false)
	tif := strings.ToUpper(strings.TrimSpace(os.Getenv("EXEC_TIF")))
	if tif == "" {
		tif = "GTC"
	}

	minNotional := floatEnv("EXEC_MIN_NOTIONAL", 0.0)

	price := floatEnv("EXEC_PRICE", 0.0)
	at := strings.ToLower(strings.TrimSpace(os.Getenv("EXEC_AT")))
	offsetBps := floatEnv("EXEC_OFFSET_BPS", 0.0)
	offsetPct := floatEnv("EXEC_OFFSET_PCT", 0.0)

	// Symbol rules
	meta, err := rest.SymbolMeta(symbol, false)
	if err != nil {
		fmt.Println("symbol meta:", err)
		os.Exit(1)
	}

	// For LIMIT: compute price if not provided
	var bid, ask float64
	if kind == "LIMIT" && price <= 0 {
		if at == "" {
			fmt.Println("LIMIT requires EXEC_PRICE or EXEC_AT")
			os.Exit(2)
		}
		bid, ask, err = rest.BookTicker(symbol)
		if err != nil {
			fmt.Println("bookTicker:", err)
			os.Exit(1)
		}
		base := pickBasePrice(at, bid, ask)
		if base <= 0 {
			fmt.Println("cannot derive base price from EXEC_AT (need bid/ask/mid)")
			os.Exit(2)
		}
		adj := 1.0
		if offsetBps != 0 {
			adj = 1.0 + (offsetBps / 10000.0)
		} else if offsetPct != 0 {
			adj = 1.0 + (offsetPct / 100.0)
		}
		price = base * adj
	}

	// Round price (LIMIT)
	roundedPrice := price
	if kind == "LIMIT" {
		roundedPrice, _, err = rest.RoundPrice(symbol, price)
		if err != nil {
			fmt.Println("round price:", err)
			os.Exit(1)
		}
		if roundedPrice <= 0 {
			fmt.Println("price <= 0 after rounding")
			os.Exit(1)
		}
	}

	// Reference price for qty conversion
	refPx := 0.0
	if qtyOverride > 0 {
		if kind == "LIMIT" {
			refPx = roundedPrice
		} else {
			bid, ask, err = rest.BookTicker(symbol)
			if err != nil {
				fmt.Println("bookTicker:", err)
				os.Exit(1)
			}
			if side == "BUY" {
				refPx = ask
			} else {
				refPx = bid
			}
		}
	} else {
		switch kind {
		case "LIMIT":
			refPx = roundedPrice
		case "MARKET":
			if bid == 0 && ask == 0 {
				bid, ask, err = rest.BookTicker(symbol)
				if err != nil {
					fmt.Println("bookTicker:", err)
					os.Exit(1)
				}
			}
			if side == "BUY" {
				refPx = ask
			} else {
				refPx = bid
			}
		default:
			fmt.Println("unknown EXEC_KIND (LIMIT|MARKET)")
			os.Exit(2)
		}
		if refPx <= 0 {
			fmt.Println("ref price <= 0; cannot compute qty")
			os.Exit(1)
		}
	}

	// Change leverage (optional)
	if lev > 0 && !dry {
		_, _ = rest.ChangeLeverage(symbol, lev)
	}

	// Compute qty
	rawQty := qtyOverride
	if rawQty <= 0 {
		rawQty = usd / refPx
	}

	roundedQty, _, err := rest.RoundQty(symbol, rawQty)
	if err != nil {
		fmt.Println("round qty:", err)
		os.Exit(1)
	}
	if roundedQty <= 0 {
		minUSD := meta.StepSize * refPx
		fmt.Printf("qty <= 0 after rounding (stepSize=%.10f). Need at least about $%.4f notional at this price.\n", meta.StepSize, minUSD)
		os.Exit(1)
	}

	// Min-notional bump (if user provided)
	if minNotional > 0 {
		roundedQty = bumpQtyToMinNotional(roundedQty, refPx, minNotional, meta.StepSize, meta.QtyPrecision)
	}

	if debug {
		fmt.Printf("DEBUG symbol=%s tick=%.10f step=%.10f pricePrec=%d qtyPrec=%d\n",
			symbol, meta.TickSize, meta.StepSize, meta.PricePrecision, meta.QtyPrecision)
		fmt.Printf("DEBUG bid=%.6f ask=%.6f refPx=%.6f\n", bid, ask, refPx)
		fmt.Printf("DEBUG rawQty=%.12f roundedQty=%.12f notional=%.6f\n",
			rawQty, roundedQty, roundedQty*refPx)
		if kind == "LIMIT" {
			fmt.Printf("DEBUG rawPrice=%.12f roundedPrice=%.12f\n", price, roundedPrice)
		}
	}

	// Build order params
	vals := url.Values{}
	vals.Set("symbol", symbol)
	vals.Set("side", side)
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", fmt.Sprintf("%v", reduceOnly))

	switch kind {
	case "MARKET":
		vals.Set("type", "MARKET")
		vals.Set("quantity", formatFloat(roundedQty, meta.QtyPrecision))
	case "LIMIT":
		vals.Set("type", "LIMIT")
		vals.Set("timeInForce", tif)
		vals.Set("quantity", formatFloat(roundedQty, meta.QtyPrecision))
		vals.Set("price", formatFloat(roundedPrice, meta.PricePrecision))
	default:
		fmt.Println("unknown EXEC_KIND (LIMIT|MARKET)")
		os.Exit(2)
	}

	if dry {
		fmt.Printf("DRY_RUN=1 place %s %s symbol=%s qty=%s",
			kind, side, symbol, vals.Get("quantity"))
		if kind == "LIMIT" {
			fmt.Printf(" price=%s tif=%s", vals.Get("price"), tif)
		}
		fmt.Printf(" reduceOnly=%s", vals.Get("reduceOnly"))
		fmt.Println()
		return
	}

	// Send order (auto-retry once on Aster -4164 minNotional error if EXEC_MIN_NOTIONAL not set)
	out, err := rest.PlaceOrder(vals)
	if err != nil {
		if minNotional == 0 {
			if parsed, ok := parseMinNotionalFromErr(err); ok && parsed > 0 && refPx > 0 {
				roundedQty2 := bumpQtyToMinNotional(roundedQty, refPx, parsed, meta.StepSize, meta.QtyPrecision)
				if roundedQty2 > roundedQty {
					vals.Set("quantity", formatFloat(roundedQty2, meta.QtyPrecision))
					out2, err2 := rest.PlaceOrder(vals)
					if err2 == nil {
						mustPrintJSON(out2, nil)
						return
					}
				}
			}
		}
		fmt.Println("order error:", err)
		os.Exit(1)
	}
	mustPrintJSON(out, nil)
}

func closeMarket(rest *aster.RESTAuth, symbol string, dry bool, debug bool) {
	closePct := floatEnv("EXEC_CLOSE_PCT", 100.0)
	if closePct <= 0 || closePct > 100 {
		closePct = 100
	}

	pos, err := rest.PositionRisk(symbol)
	if err != nil {
		fmt.Println("positionRisk:", err)
		os.Exit(1)
	}
	if len(pos) == 0 {
		fmt.Println("no position data returned")
		return
	}

	p := pos[0]
	amt := mapFloat(p, "positionAmt") // +long, -short
	if amt == 0 {
		fmt.Println("no open position to close")
		return
	}

	side := "SELL"
	qty := amt
	if amt < 0 {
		side = "BUY"
		qty = -amt
	}
	qty = qty * (closePct / 100.0)

	meta, err := rest.SymbolMeta(symbol, false)
	if err != nil {
		fmt.Println("symbol meta:", err)
		os.Exit(1)
	}
	qtyRounded, _, err := rest.RoundQty(symbol, qty)
	if err != nil {
		fmt.Println("round qty:", err)
		os.Exit(1)
	}
	if qtyRounded <= 0 {
		fmt.Println("close qty <= 0 after rounding")
		os.Exit(1)
	}

	if debug {
		fmt.Printf("DEBUG close_market symbol=%s positionAmt=%v closePct=%.2f side=%s rawQty=%.10f roundedQty=%.10f\n",
			symbol, p["positionAmt"], closePct, side, qty, qtyRounded)
	}

	if dry {
		fmt.Printf("DRY_RUN=1 would close MARKET %s symbol=%s qty=%s reduceOnly=true\n",
			side, symbol, formatFloat(qtyRounded, meta.QtyPrecision))
		return
	}

	vals := url.Values{}
	vals.Set("symbol", symbol)
	vals.Set("side", side)
	vals.Set("type", "MARKET")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "true")
	vals.Set("quantity", formatFloat(qtyRounded, meta.QtyPrecision))

	out, err := rest.PlaceOrder(vals)
	mustPrintJSON(out, err)
}

func closeLimit(rest *aster.RESTAuth, symbol string, dry bool, debug bool) {
	closePct := floatEnv("EXEC_CLOSE_PCT", 100.0)
	if closePct <= 0 || closePct > 100 {
		closePct = 100
	}

	at := strings.ToLower(strings.TrimSpace(os.Getenv("EXEC_AT")))
	if at == "" {
		at = "mid"
	}
	offsetBps := floatEnv("EXEC_OFFSET_BPS", 0.0)
	offsetPct := floatEnv("EXEC_OFFSET_PCT", 0.0)

	pos, err := rest.PositionRisk(symbol)
	if err != nil {
		fmt.Println("positionRisk:", err)
		os.Exit(1)
	}
	if len(pos) == 0 {
		fmt.Println("no position data returned")
		return
	}

	p := pos[0]
	amt := mapFloat(p, "positionAmt")
	if amt == 0 {
		fmt.Println("no open position to close")
		return
	}

	side := "SELL"
	closeQty := amt
	if amt < 0 {
		side = "BUY"
		closeQty = -amt
	}
	closeQty = closeQty * (closePct / 100.0)

	meta, err := rest.SymbolMeta(symbol, false)
	if err != nil {
		fmt.Println("symbol meta:", err)
		os.Exit(1)
	}

	bid, ask, err := rest.BookTicker(symbol)
	if err != nil {
		fmt.Println("bookTicker:", err)
		os.Exit(1)
	}
	base := pickBasePrice(at, bid, ask)
	if base <= 0 {
		fmt.Println("cannot derive base price for close_limit")
		os.Exit(1)
	}

	adj := 1.0
	if offsetBps != 0 {
		adj = 1.0 + (offsetBps / 10000.0)
	} else if offsetPct != 0 {
		adj = 1.0 + (offsetPct / 100.0)
	}

	rawPrice := base * adj
	priceRounded, _, err := rest.RoundPrice(symbol, rawPrice)
	if err != nil {
		fmt.Println("round price:", err)
		os.Exit(1)
	}
	if priceRounded <= 0 {
		fmt.Println("price <= 0 after rounding")
		os.Exit(1)
	}

	qtyRounded, _, err := rest.RoundQty(symbol, closeQty)
	if err != nil {
		fmt.Println("round qty:", err)
		os.Exit(1)
	}
	if qtyRounded <= 0 {
		fmt.Println("close qty <= 0 after rounding")
		os.Exit(1)
	}

	if debug {
		fmt.Printf("DEBUG close_limit symbol=%s positionAmt=%v closePct=%.2f side=%s bid=%.6f ask=%.6f at=%s rawPrice=%.6f price=%.6f rawQty=%.10f qty=%.10f\n",
			symbol, p["positionAmt"], closePct, side, bid, ask, at, rawPrice, priceRounded, closeQty, qtyRounded)
	}

	if dry {
		fmt.Printf("DRY_RUN=1 would close LIMIT %s symbol=%s qty=%s price=%s reduceOnly=true tif=GTC\n",
			side, symbol,
			formatFloat(qtyRounded, meta.QtyPrecision),
			formatFloat(priceRounded, meta.PricePrecision),
		)
		return
	}

	vals := url.Values{}
	vals.Set("symbol", symbol)
	vals.Set("side", side)
	vals.Set("type", "LIMIT")
	vals.Set("timeInForce", "GTC")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "true")
	vals.Set("quantity", formatFloat(qtyRounded, meta.QtyPrecision))
	vals.Set("price", formatFloat(priceRounded, meta.PricePrecision))

	out, err := rest.PlaceOrder(vals)
	mustPrintJSON(out, err)
}

func mustPrintJSON(v any, err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func mustInt64Env(k string) int64 {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		fmt.Printf("missing %s\n", k)
		os.Exit(2)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		fmt.Printf("bad %s: %v\n", k, err)
		os.Exit(2)
	}
	return n
}

func floatEnv(k string, def float64) float64 {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

func intEnv(k string, def int) int {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return i
}

func boolEnv(k string, def bool) bool {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	s = strings.ToLower(s)
	return !(s == "0" || s == "false" || s == "no" || s == "off")
}

func pickBasePrice(at string, bid, ask float64) float64 {
	switch at {
	case "bid":
		return bid
	case "ask":
		return ask
	case "mid":
		if bid > 0 && ask > 0 {
			return (bid + ask) / 2
		}
		return 0
	default:
		return 0
	}
}

func formatFloat(v float64, prec int) string {
	return strconv.FormatFloat(v, 'f', prec, 64)
}

// Reads numeric values out of PositionRisk map (positionAmt is often a string).
func mapFloat(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f
	}
}

// bumpQtyToMinNotional rounds qty UP to satisfy notional >= minNotional, in step increments.
func bumpQtyToMinNotional(qty, price, minNotional, step float64, qtyPrec int) float64 {
	if price <= 0 || step <= 0 || minNotional <= 0 {
		return qty
	}
	if qty*price >= minNotional {
		return qty
	}
	needQty := minNotional / price

	steps := int64(needQty / step)
	if float64(steps)*step < needQty {
		steps++
	}
	out := float64(steps) * step

	// trim float tails to qty precision
	pow := 1.0
	for i := 0; i < qtyPrec; i++ {
		pow *= 10
	}
	out = float64(int64(out*pow+0.5)) / pow
	return out
}

// parseMinNotionalFromErr extracts min notional from Aster's -4164 error message.
// Example msg:
//
//	Order's notional must be no smaller than 5.0 (unless you choose reduce only)
func parseMinNotionalFromErr(err error) (float64, bool) {
	if err == nil {
		return 0, false
	}
	s := err.Error()
	idx := strings.Index(s, "no smaller than")
	if idx < 0 {
		return 0, false
	}
	rest := strings.TrimSpace(s[idx+len("no smaller than"):])

	token := ""
	for _, r := range rest {
		if (r >= '0' && r <= '9') || r == '.' {
			token += string(r)
		} else {
			break
		}
	}
	if token == "" {
		return 0, false
	}
	f, e := strconv.ParseFloat(token, 64)
	if e != nil || f <= 0 {
		return 0, false
	}
	return f, true
}
