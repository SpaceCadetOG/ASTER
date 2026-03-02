// cmd/exec/main.go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go-machine/adapters/aster"
)

// cmd/exec = single control tool for Aster.
//
// Credentials:
//   - env:  ASTER_API_KEY / ASTER_API_SECRET
//   - YAML: ASTER_CONFIG=/path/to/file.yaml or ~/.aster.yaml
//
// Core actions:
//
//	EXEC_ACTION=auth_check|balance|account|orderbook|quote|place|cancel|cancel_all|status|open_orders|position|close_market|close_limit|flatten
func main() {
	cfgPath := strings.TrimSpace(os.Getenv("ASTER_CONFIG"))
	if cfgPath == "" {
		// Prefer local repo config first, then fallback to ~/.aster.yaml.
		if _, err := os.Stat(".aster.yaml"); err == nil {
			cfgPath = ".aster.yaml"
		} else if home, err := os.UserHomeDir(); err == nil && home != "" {
			cfgPath = filepath.Join(home, ".aster.yaml")
		}
	}

	fileKV := map[string]string{}
	if cfgPath != "" {
		if kv, err := loadSimpleYAMLKV(cfgPath); err == nil {
			fileKV = kv
		}
	}
	getCfg := func(envName string, keys ...string) string {
		if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
			return v
		}
		for _, k := range keys {
			if v := strings.TrimSpace(fileKV[strings.ToLower(strings.TrimSpace(k))]); v != "" {
				return v
			}
		}
		return ""
	}

	key := getCfg("ASTER_API_KEY", "aster_api_key", "api_key", "key")
	sec := getCfg("ASTER_API_SECRET", "aster_api_secret", "api_secret", "secret")
	authMode := strings.ToLower(getCfg("ASTER_AUTH_MODE", "aster_auth_mode", "auth_mode"))
	if authMode == "" {
		authMode = "auto"
	}
	user := getCfg("ASTER_USER", "aster_user", "user")
	signer := getCfg("ASTER_SIGNER", "aster_signer", "signer")
	priv := getCfg("ASTER_PRIVATE_KEY", "aster_private_key", "private_key")
	baseURL := getCfg("EXEC_BASE_URL", "aster_base_url", "base_url")
	pyBin := getCfg("ASTER_PYTHON", "aster_python", "python")
	chainID := int64EnvWithFallback("ASTER_CHAIN_ID", fileKV, "aster_chain_id", 0)
	if pyBin != "" && strings.TrimSpace(os.Getenv("ASTER_PYTHON")) == "" {
		_ = os.Setenv("ASTER_PYTHON", pyBin)
	}

	hasHMAC := key != "" && sec != ""
	hasAgent := user != "" && signer != "" && priv != ""
	if !hasHMAC && !hasAgent {
		fmt.Println("missing credentials: set HMAC (ASTER_API_KEY/ASTER_API_SECRET) or agent wallet fields (ASTER_USER/ASTER_SIGNER/ASTER_PRIVATE_KEY), via env or ASTER_CONFIG")
		os.Exit(2)
	}

	action := strings.ToLower(strings.TrimSpace(os.Getenv("EXEC_ACTION")))
	if action == "" {
		action = "balance"
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

	rest := aster.NewRESTAuthWithConfig(aster.RESTAuthConfig{
		APIKey:     key,
		APISecret:  sec,
		User:       user,
		Signer:     signer,
		PrivateKey: priv,
		AuthMode:   authMode,
		ChainID:    chainID,
		BaseURL:    baseURL,
	})
	_ = rest.SyncTime() // best-effort

	switch action {
	case "auth_check":
		out, err := runAuthCheck(rest)
		mustPrintJSON(out, err)
		return

	case "balance":
		out, err := rest.GetBalance()
		if err == nil {
			out = filterBalancesByAssets(out, os.Getenv("EXEC_ASSETS"))
		}
		mustPrintJSON(out, err)
		return

	case "account":
		out, err := buildAccountSummary(rest, symbol)
		mustPrintJSON(out, err)
		return

	case "orderbook":
		bid, ask, err := rest.BookTicker(symbol)
		if err != nil {
			mustPrintJSON(nil, err)
			return
		}
		out := map[string]any{
			"symbol": symbol,
			"bid":    bid,
			"ask":    ask,
			"mid":    (bid + ask) / 2.0,
			"spread": ask - bid,
		}
		mustPrintJSON(out, nil)
		return

	case "quote":
		out, err := buildQuote(rest, symbol)
		mustPrintJSON(out, err)
		return

	case "cancel":
		ref, err := resolveOrderRef(os.Getenv("EXEC_ORDER_ID"), os.Getenv("EXEC_CLIENT_ORDER_ID"))
		if err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
		if dry {
			if ref.HasNumericID {
				fmt.Printf("DRY_RUN=1 would cancel: symbol=%s orderId=%d\n", symbol, ref.OrderID)
			} else {
				fmt.Printf("DRY_RUN=1 would cancel: symbol=%s origClientOrderId=%s\n", symbol, ref.ClientOrderID)
			}
			return
		}
		var out map[string]any
		if ref.HasNumericID {
			out, err = rest.CancelOrder(symbol, ref.OrderID)
		} else {
			out, err = rest.CancelOrderByClientID(symbol, ref.ClientOrderID)
		}
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
		ref, err := resolveOrderRef(os.Getenv("EXEC_ORDER_ID"), os.Getenv("EXEC_CLIENT_ORDER_ID"))
		if err != nil {
			fmt.Println(err)
			os.Exit(2)
		}
		var out map[string]any
		if ref.HasNumericID {
			out, err = rest.GetOrder(symbol, ref.OrderID)
		} else {
			out, err = rest.GetOrderByClientID(symbol, ref.ClientOrderID)
		}
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

	case "flatten":
		flatten(rest, symbol, dry, debug)
		return

	case "place":
		// continue below
	default:
		fmt.Println("unknown EXEC_ACTION (auth_check|balance|account|orderbook|quote|place|cancel|cancel_all|status|open_orders|position|close_market|close_limit|flatten)")
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

	meta, err := rest.SymbolMeta(symbol, false)
	if err != nil {
		fmt.Println("symbol meta:", err)
		os.Exit(1)
	}

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

	if lev > 0 && !dry {
		_, _ = rest.ChangeLeverage(symbol, lev)
	}

	rawQty := qtyOverride
	if rawQty <= 0 {
		rawQty = usd / refPx
	}

	roundedQty, _, err := rest.RoundQty(symbol, rawQty)
	if err != nil {
		fmt.Println("round qty:", err)
		os.Exit(1)
	}
	if minNotional > 0 {
		// Apply min-notional bump before failing on roundedQty==0, so small USD
		// test orders can still be lifted to the minimum tradable step.
		roundedQty = bumpQtyToMinNotional(roundedQty, refPx, minNotional, meta.StepSize, meta.QtyPrecision)
	}
	if roundedQty <= 0 {
		minUSD := meta.StepSize * refPx
		fmt.Printf("qty <= 0 after rounding (stepSize=%.10f). Need at least about $%.4f notional at this price.\n", meta.StepSize, minUSD)
		os.Exit(1)
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

func loadSimpleYAMLKV(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(parts[0]))
		v := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		if k != "" && v != "" {
			out[k] = v
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func buildAccountSummary(rest *aster.RESTAuth, symbol string) (map[string]any, error) {
	out := map[string]any{"symbol": symbol}

	acct, err := rest.GetAccountSummary()
	if err != nil {
		out["account_error"] = err.Error()
	} else {
		out["account"] = acct
	}

	bals, bErr := rest.GetBalance()
	if bErr != nil {
		out["balance_error"] = bErr.Error()
	} else {
		out["balances"] = bals
		for _, b := range bals {
			if strings.EqualFold(b.Asset, "USDT") {
				out["usdt"] = map[string]any{
					"balance":          b.Balance,
					"available":        b.AvailableBalance,
					"unrealized_pnl":   b.CrossUnPnl,
					"max_withdrawable": b.MaxWithdrawAmount,
				}
				break
			}
		}
	}

	pos, pErr := rest.PositionRisk(symbol)
	if pErr != nil {
		out["position_error"] = pErr.Error()
	} else {
		out["position"] = pos
	}

	if bErr != nil {
		return out, bErr
	}
	return out, nil
}

func runAuthCheck(rest *aster.RESTAuth) (map[string]any, error) {
	out := map[string]any{
		"base_url":  rest.BaseURL(),
		"auth_mode": rest.AuthMode(),
	}

	if err := rest.Ping(); err != nil {
		out["ping_ok"] = false
		out["ping_error"] = err.Error()
	} else {
		out["ping_ok"] = true
	}

	srvTime, err := rest.ServerTime()
	if err != nil {
		out["time_ok"] = false
		out["time_error"] = err.Error()
	} else {
		out["time_ok"] = true
		out["server_time"] = srvTime
	}

	acct, aErr := rest.GetAccountSummary()
	if aErr != nil {
		out["account_ok"] = false
		out["account_error"] = aErr.Error()
		attachAuthHints(out, aErr)
	} else {
		out["account_ok"] = true
		out["account_summary"] = acct
	}

	bals, bErr := rest.GetBalance()
	if bErr != nil {
		out["balance_ok"] = false
		out["balance_error"] = bErr.Error()
		attachAuthHints(out, bErr)
	} else {
		out["balance_ok"] = true
		out["balances_count"] = len(bals)
		out["balances"] = bals
	}

	if aErr != nil {
		return out, aErr
	}
	return out, bErr
}

func attachAuthHints(out map[string]any, err error) {
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	hints := []string{}

	if strings.Contains(msg, `code":-2015`) || strings.Contains(msg, "invalid api-key") {
		hints = append(hints, "API key rejected. Verify mainnet vs testnet base URL, key/secret pair, and IP whitelist.")
	}
	if strings.Contains(msg, `code":-2014`) || strings.Contains(msg, "api-key format invalid") {
		hints = append(hints, "API key format invalid for selected auth mode. Use HMAC key/secret for hmac mode.")
	}
	if strings.Contains(msg, `code":-1022`) || strings.Contains(msg, "signature for this request is not valid") {
		hints = append(hints, "Signature mismatch. Confirm correct secret and no extra whitespace in config values.")
	}
	if strings.Contains(msg, `code":-1102`) || strings.Contains(msg, "mandatory parameter 'nonce'") {
		hints = append(hints, "Signed request missing nonce/timestamp. Keep adapter on latest code and run SyncTime.")
	}
	if len(hints) > 0 {
		out["auth_hints"] = hints
	}
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
	amt := mapFloat(p, "positionAmt")
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

func flatten(rest *aster.RESTAuth, symbol string, dry bool, debug bool) {
	out := map[string]any{
		"symbol":         symbol,
		"cancel_all":     nil,
		"position_close": nil,
	}

	if dry {
		out["cancel_all"] = "DRY_RUN=1 would cancel all open orders for symbol"
	} else {
		c, err := rest.CancelAllOrders(symbol)
		if err != nil {
			out["cancel_all_error"] = err.Error()
		} else {
			out["cancel_all"] = c
		}
	}

	pos, err := rest.PositionRisk(symbol)
	if err != nil {
		out["position_error"] = err.Error()
		mustPrintJSON(out, nil)
		return
	}
	if len(pos) == 0 || mapFloat(pos[0], "positionAmt") == 0 {
		out["position_close"] = "no open position to close"
		mustPrintJSON(out, nil)
		return
	}

	p := pos[0]
	amt := mapFloat(p, "positionAmt")
	side := "SELL"
	qty := amt
	if amt < 0 {
		side = "BUY"
		qty = -amt
	}

	meta, err := rest.SymbolMeta(symbol, false)
	if err != nil {
		out["position_close_error"] = "symbol meta: " + err.Error()
		mustPrintJSON(out, nil)
		return
	}

	qtyRounded, _, err := rest.RoundQty(symbol, qty)
	if err != nil || qtyRounded <= 0 {
		out["position_close_error"] = "round qty failed"
		mustPrintJSON(out, nil)
		return
	}

	if debug {
		out["debug"] = map[string]any{
			"positionAmt": p["positionAmt"],
			"side":        side,
			"rawQty":      qty,
			"roundedQty":  qtyRounded,
		}
	}

	if dry {
		out["position_close"] = map[string]any{
			"type":       "MARKET",
			"side":       side,
			"reduceOnly": true,
			"quantity":   formatFloat(qtyRounded, meta.QtyPrecision),
		}
		mustPrintJSON(out, nil)
		return
	}

	vals := url.Values{}
	vals.Set("symbol", symbol)
	vals.Set("side", side)
	vals.Set("type", "MARKET")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "true")
	vals.Set("quantity", formatFloat(qtyRounded, meta.QtyPrecision))

	placed, err := rest.PlaceOrder(vals)
	if err != nil {
		out["position_close_error"] = err.Error()
	} else {
		out["position_close"] = placed
	}
	mustPrintJSON(out, nil)
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
	if prec < 0 {
		prec = 0
	}
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

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

	pow := 1.0
	for i := 0; i < qtyPrec; i++ {
		pow *= 10
	}
	out = float64(int64(out*pow+0.5)) / pow
	return out
}

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

type orderRef struct {
	HasNumericID  bool
	OrderID       int64
	ClientOrderID string
}

func resolveOrderRef(orderIDStr, clientIDStr string) (orderRef, error) {
	orderIDStr = strings.TrimSpace(orderIDStr)
	clientIDStr = strings.TrimSpace(clientIDStr)
	if orderIDStr == "" && clientIDStr == "" {
		return orderRef{}, fmt.Errorf("missing EXEC_ORDER_ID or EXEC_CLIENT_ORDER_ID")
	}
	if orderIDStr != "" {
		n, err := strconv.ParseInt(orderIDStr, 10, 64)
		if err != nil {
			return orderRef{}, fmt.Errorf("bad EXEC_ORDER_ID: %v", err)
		}
		return orderRef{HasNumericID: true, OrderID: n}, nil
	}
	return orderRef{ClientOrderID: clientIDStr}, nil
}

func buildQuote(rest *aster.RESTAuth, symbol string) (map[string]any, error) {
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
	minNotional := floatEnv("EXEC_MIN_NOTIONAL", 0.0)
	price := floatEnv("EXEC_PRICE", 0.0)
	at := strings.ToLower(strings.TrimSpace(os.Getenv("EXEC_AT")))
	offsetBps := floatEnv("EXEC_OFFSET_BPS", 0.0)
	offsetPct := floatEnv("EXEC_OFFSET_PCT", 0.0)

	meta, err := rest.SymbolMeta(symbol, false)
	if err != nil {
		return nil, err
	}
	bid, ask, err := rest.BookTicker(symbol)
	if err != nil {
		return nil, err
	}

	roundedPrice := price
	refPx := 0.0
	if kind == "LIMIT" {
		if roundedPrice <= 0 {
			if at == "" {
				at = "mid"
			}
			base := pickBasePrice(at, bid, ask)
			if base <= 0 {
				return nil, fmt.Errorf("cannot derive base price from EXEC_AT")
			}
			adj := 1.0
			if offsetBps != 0 {
				adj = 1.0 + (offsetBps / 10000.0)
			} else if offsetPct != 0 {
				adj = 1.0 + (offsetPct / 100.0)
			}
			roundedPrice = base * adj
		}
		roundedPrice, _, err = rest.RoundPrice(symbol, roundedPrice)
		if err != nil {
			return nil, err
		}
		refPx = roundedPrice
	} else {
		if side == "BUY" {
			refPx = ask
		} else {
			refPx = bid
		}
	}

	rawQty := qtyOverride
	if rawQty <= 0 && refPx > 0 {
		rawQty = usd / refPx
	}
	roundedQty, _, err := rest.RoundQty(symbol, rawQty)
	if err != nil {
		return nil, err
	}
	if minNotional > 0 {
		roundedQty = bumpQtyToMinNotional(roundedQty, refPx, minNotional, meta.StepSize, meta.QtyPrecision)
	}

	out := map[string]any{
		"symbol":             symbol,
		"side":               side,
		"kind":               kind,
		"bid":                bid,
		"ask":                ask,
		"refPrice":           refPx,
		"price":              roundedPrice,
		"rawQty":             rawQty,
		"roundedQty":         roundedQty,
		"notional":           roundedQty * refPx,
		"minStepUSDApprox":   meta.StepSize * refPx,
		"minNotionalSetting": minNotional,
		"qtyPrecision":       meta.QtyPrecision,
		"pricePrecision":     meta.PricePrecision,
		"tickSize":           meta.TickSize,
		"stepSize":           meta.StepSize,
	}
	return out, nil
}

func int64EnvWithFallback(envName string, fileKV map[string]string, key string, def int64) int64 {
	if s := strings.TrimSpace(os.Getenv(envName)); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	if s := strings.TrimSpace(fileKV[strings.ToLower(strings.TrimSpace(key))]); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func filterBalancesByAssets(in []aster.Balance, csv string) []aster.Balance {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return in
	}
	want := map[string]struct{}{}
	for _, p := range strings.Split(csv, ",") {
		s := strings.ToUpper(strings.TrimSpace(p))
		if s != "" {
			want[s] = struct{}{}
		}
	}
	if len(want) == 0 {
		return in
	}
	out := make([]aster.Balance, 0, len(in))
	for _, b := range in {
		if _, ok := want[strings.ToUpper(strings.TrimSpace(b.Asset))]; ok {
			out = append(out, b)
		}
	}
	return out
}
