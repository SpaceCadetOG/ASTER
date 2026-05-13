package aster

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type BracketUpdate struct {
	Symbol         string
	PositionSide   string
	CloseSide      string
	Qty            float64
	QtyPrecision   int
	PricePrecision int
	OldStopOrderID int64
	OldTPOrderID   int64
	NewStopPrice   float64
	NewTPPrice     float64
}

type SymbolMeta struct {
	Symbol         string  `json:"symbol"`
	TickSize       float64 `json:"tickSize"`
	StepSize       float64 `json:"stepSize"`
	MinQty         float64 `json:"minQty"`
	MaxQty         float64 `json:"maxQty"`
	MinNotional    float64 `json:"minNotional"`
	MaxNotional    float64 `json:"maxNotional"`
	QtyPrecision   int     `json:"qtyPrecision"`
	PricePrecision int     `json:"pricePrecision"`
}

func (r *RESTAuth) BookTicker(symbol string) (bid, ask float64, err error) {
	q := url.Values{}
	q.Set("symbol", strings.ToUpper(strings.TrimSpace(symbol)))
	b, err := r.doPublicGETAny([]string{"/fapi/v3/ticker/bookTicker", "/fapi/v1/ticker/bookTicker"}, q)
	if err != nil {
		return 0, 0, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return 0, 0, err
	}
	bid = parseFloatAny(out["bidPrice"])
	ask = parseFloatAny(out["askPrice"])
	if bid <= 0 || ask <= 0 {
		return 0, 0, fmt.Errorf("bookTicker missing bid/ask: %s", string(b))
	}
	return bid, ask, nil
}

func (r *RESTAuth) SymbolMeta(symbol string, useCache bool) (SymbolMeta, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return SymbolMeta{}, fmt.Errorf("symbol required")
	}

	if useCache {
		r.metaMu.RLock()
		meta, ok := r.metaCache[symbol]
		r.metaMu.RUnlock()
		if ok {
			return meta, nil
		}
	}

	q := url.Values{}
	q.Set("symbol", RawSymbol(symbol))
	b, err := r.doPublicGETAny([]string{"/fapi/v3/exchangeInfo", "/fapi/v1/exchangeInfo"}, q)
	if err != nil {
		return SymbolMeta{}, err
	}

	var out struct {
		Symbols []struct {
			Symbol            string `json:"symbol"`
			PricePrecision    int    `json:"pricePrecision"`
			QuantityPrecision int    `json:"quantityPrecision"`
			Filters           []struct {
				FilterType  string `json:"filterType"`
				TickSize    string `json:"tickSize"`
				StepSize    string `json:"stepSize"`
				MinQty      string `json:"minQty"`
				MaxQty      string `json:"maxQty"`
				Notional    string `json:"notional"`
				MinNotional string `json:"minNotional"`
				MaxNotional string `json:"maxNotional"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := decodeJSONNumbers(b, &out); err != nil {
		return SymbolMeta{}, err
	}
	if len(out.Symbols) == 0 {
		return SymbolMeta{}, fmt.Errorf("symbol not found in exchangeInfo: %s", symbol)
	}
	rawSym := strings.ToUpper(RawSymbol(symbol))
	var s *struct {
		Symbol            string `json:"symbol"`
		PricePrecision    int    `json:"pricePrecision"`
		QuantityPrecision int    `json:"quantityPrecision"`
		Filters           []struct {
			FilterType  string `json:"filterType"`
			TickSize    string `json:"tickSize"`
			StepSize    string `json:"stepSize"`
			MinQty      string `json:"minQty"`
			MaxQty      string `json:"maxQty"`
			Notional    string `json:"notional"`
			MinNotional string `json:"minNotional"`
			MaxNotional string `json:"maxNotional"`
		} `json:"filters"`
	}
	for i := range out.Symbols {
		if strings.EqualFold(strings.TrimSpace(out.Symbols[i].Symbol), rawSym) {
			s = &out.Symbols[i]
			break
		}
	}
	if s == nil {
		return SymbolMeta{}, fmt.Errorf("symbol %s not found in exchangeInfo payload", rawSym)
	}
	meta := SymbolMeta{
		Symbol:         strings.ToUpper(s.Symbol),
		PricePrecision: s.PricePrecision,
		QtyPrecision:   s.QuantityPrecision,
	}

	for _, f := range s.Filters {
		switch strings.ToUpper(f.FilterType) {
		case "PRICE_FILTER":
			meta.TickSize = parseFloatString(f.TickSize)
		case "LOT_SIZE", "MARKET_LOT_SIZE":
			if meta.StepSize == 0 {
				meta.StepSize = parseFloatString(f.StepSize)
			}
			if meta.MinQty == 0 {
				meta.MinQty = parseFloatString(f.MinQty)
			}
			if meta.MaxQty == 0 {
				meta.MaxQty = parseFloatString(f.MaxQty)
			}
		case "MIN_NOTIONAL", "NOTIONAL":
			if meta.MinNotional == 0 {
				meta.MinNotional = parseFloatString(f.MinNotional)
				if meta.MinNotional == 0 {
					meta.MinNotional = parseFloatString(f.Notional)
				}
			}
			if meta.MaxNotional == 0 {
				meta.MaxNotional = parseFloatString(f.MaxNotional)
			}
		}
	}

	if meta.PricePrecision <= 0 && meta.TickSize > 0 {
		meta.PricePrecision = decimalsFromStep(meta.TickSize)
	}
	if meta.QtyPrecision <= 0 && meta.StepSize > 0 {
		meta.QtyPrecision = decimalsFromStep(meta.StepSize)
	}

	r.metaMu.Lock()
	r.metaCache[symbol] = meta
	r.metaCache[meta.Symbol] = meta
	r.metaMu.Unlock()

	return meta, nil
}

func (r *RESTAuth) RoundPrice(symbol string, price float64) (rounded float64, changed bool, err error) {
	if price <= 0 {
		return 0, false, fmt.Errorf("price must be > 0")
	}
	meta, err := r.SymbolMeta(symbol, true)
	if err != nil {
		return 0, false, err
	}
	if meta.TickSize > 0 {
		steps := float64(int64(price / meta.TickSize))
		rounded = steps * meta.TickSize
	} else {
		rounded = price
	}
	if rounded <= 0 {
		return 0, false, fmt.Errorf("rounded price <= 0")
	}
	if meta.PricePrecision >= 0 {
		rounded = parseFloatString(strconv.FormatFloat(rounded, 'f', meta.PricePrecision, 64))
	}
	changed = rounded != price
	return rounded, changed, nil
}

func (r *RESTAuth) RoundQty(symbol string, qty float64) (rounded float64, changed bool, err error) {
	if qty <= 0 {
		return 0, false, fmt.Errorf("qty must be > 0")
	}
	meta, err := r.SymbolMeta(symbol, true)
	if err != nil {
		return 0, false, err
	}
	if meta.StepSize > 0 {
		steps := float64(int64(qty / meta.StepSize))
		rounded = steps * meta.StepSize
	} else {
		rounded = qty
	}
	if meta.QtyPrecision >= 0 {
		rounded = parseFloatString(strconv.FormatFloat(rounded, 'f', meta.QtyPrecision, 64))
	}
	if meta.MinQty > 0 && rounded > 0 && rounded < meta.MinQty {
		rounded = 0
	}
	changed = rounded != qty
	return rounded, changed, nil
}

func (r *RESTAuth) PlaceOrder(vals url.Values) (map[string]any, error) {
	paths := []string{"/fapi/v3/order", "/fapi/v1/order"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/order"}
	}
	b, err := r.doSignedPOSTAny(paths, vals)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) PlaceBatchOrders(orders []url.Values) ([]map[string]any, error) {
	payload := make([]map[string]string, 0, len(orders))
	for _, ov := range orders {
		item := map[string]string{}
		for k, vv := range ov {
			if len(vv) == 0 {
				continue
			}
			item[k] = vv[0]
		}
		payload = append(payload, item)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("batchOrders", string(raw))
	paths := []string{"/fapi/v3/batchOrders", "/fapi/v1/batchOrders"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/batchOrders"}
	}
	b, err := r.doSignedPOSTAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) CancelOrder(symbol string, orderID int64) (map[string]any, error) {
	q := url.Values{}
	q.Set("symbol", strings.ToUpper(strings.TrimSpace(symbol)))
	q.Set("orderId", strconv.FormatInt(orderID, 10))
	paths := []string{"/fapi/v3/order", "/fapi/v1/order"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/order"}
	}
	b, err := r.doSignedDELETEAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) CancelOrderByClientID(symbol, clientOrderID string) (map[string]any, error) {
	q := url.Values{}
	q.Set("symbol", strings.ToUpper(strings.TrimSpace(symbol)))
	q.Set("origClientOrderId", strings.TrimSpace(clientOrderID))
	paths := []string{"/fapi/v3/order", "/fapi/v1/order"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/order"}
	}
	b, err := r.doSignedDELETEAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) CancelAllOrders(symbol string) (map[string]any, error) {
	q := url.Values{}
	q.Set("symbol", strings.ToUpper(strings.TrimSpace(symbol)))
	paths := []string{"/fapi/v3/allOpenOrders", "/fapi/v1/allOpenOrders"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/allOpenOrders"}
	}
	b, err := r.doSignedDELETEAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) GetOrder(symbol string, orderID int64) (map[string]any, error) {
	q := url.Values{}
	q.Set("symbol", strings.ToUpper(strings.TrimSpace(symbol)))
	q.Set("orderId", strconv.FormatInt(orderID, 10))
	paths := []string{"/fapi/v3/order", "/fapi/v1/order"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/order"}
	}
	b, err := r.doSignedGETAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) GetOrderByClientID(symbol, clientOrderID string) (map[string]any, error) {
	q := url.Values{}
	q.Set("symbol", strings.ToUpper(strings.TrimSpace(symbol)))
	q.Set("origClientOrderId", strings.TrimSpace(clientOrderID))
	paths := []string{"/fapi/v3/order", "/fapi/v1/order"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/order"}
	}
	b, err := r.doSignedGETAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) OpenOrders(symbol string) ([]map[string]any, error) {
	q := url.Values{}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym != "" {
		q.Set("symbol", sym)
	}
	paths := []string{"/fapi/v3/openOrders", "/fapi/v1/openOrders"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/openOrders"}
	}
	b, err := r.doSignedGETAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) PositionRisk(symbol string) ([]map[string]any, error) {
	q := url.Values{}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym != "" {
		q.Set("symbol", sym)
	}
	paths := []string{"/fapi/v3/positionRisk", "/fapi/v2/positionRisk"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/positionRisk"}
	}
	b, err := r.doSignedGETAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	if sym == "" {
		return out, nil
	}
	filtered := make([]map[string]any, 0, len(out))
	for _, row := range out {
		if strings.EqualFold(stringifyAny(row["symbol"]), sym) {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

func (r *RESTAuth) ChangeLeverage(symbol string, lev int) (map[string]any, error) {
	q := url.Values{}
	q.Set("symbol", strings.ToUpper(strings.TrimSpace(symbol)))
	q.Set("leverage", strconv.Itoa(lev))
	paths := []string{"/fapi/v3/leverage", "/fapi/v1/leverage"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/leverage"}
	}
	b, err := r.doSignedPOSTAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) ChangeMarginType(symbol, marginType string) (map[string]any, error) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	mt := strings.ToUpper(strings.TrimSpace(marginType))
	if sym == "" {
		return nil, fmt.Errorf("symbol required")
	}
	if mt == "" {
		mt = "ISOLATED"
	}
	q := url.Values{}
	q.Set("symbol", sym)
	q.Set("marginType", mt)
	paths := []string{"/fapi/v3/marginType", "/fapi/v1/marginType"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/marginType"}
	}
	b, err := r.doSignedPOSTAny(paths, q)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RESTAuth) ReplaceStopOrder(symbol, side string, oldOrderID int64, qty, stopPrice float64, qtyPrecision, pricePrecision int) (map[string]any, error) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return nil, fmt.Errorf("symbol required")
	}
	if oldOrderID > 0 {
		_, _ = r.CancelOrder(sym, oldOrderID)
	}
	vals := url.Values{}
	vals.Set("symbol", sym)
	vals.Set("side", strings.ToUpper(strings.TrimSpace(side)))
	vals.Set("type", "STOP_MARKET")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "true")
	vals.Set("quantity", formatPrecision(qty, qtyPrecision))
	vals.Set("stopPrice", formatPrecision(stopPrice, pricePrecision))
	vals.Set("workingType", triggerWorkingType())
	vals.Set("priceProtect", triggerPriceProtect())
	return r.PlaceOrder(vals)
}

func (r *RESTAuth) ReplaceTakeProfitOrder(symbol, side string, oldOrderID int64, qty, tpPrice float64, qtyPrecision, pricePrecision int) (map[string]any, error) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return nil, fmt.Errorf("symbol required")
	}
	if oldOrderID > 0 {
		_, _ = r.CancelOrder(sym, oldOrderID)
	}
	vals := url.Values{}
	vals.Set("symbol", sym)
	vals.Set("side", strings.ToUpper(strings.TrimSpace(side)))
	vals.Set("type", "LIMIT")
	vals.Set("timeInForce", "GTC")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "true")
	vals.Set("quantity", formatPrecision(qty, qtyPrecision))
	vals.Set("price", formatPrecision(tpPrice, pricePrecision))
	return r.PlaceOrder(vals)
}

func (r *RESTAuth) UpdateBracket(req BracketUpdate) (stopOut, tpOut map[string]any, err error) {
	stopOut = map[string]any{}
	tpOut = map[string]any{}
	if req.Symbol == "" {
		return stopOut, tpOut, fmt.Errorf("symbol required")
	}
	if req.NewStopPrice > 0 {
		stopOut, err = r.ReplaceStopOrder(req.Symbol, req.CloseSide, req.OldStopOrderID, req.Qty, req.NewStopPrice, req.QtyPrecision, req.PricePrecision)
		if err != nil {
			return stopOut, tpOut, err
		}
	}
	if req.NewTPPrice > 0 {
		tpOut, err = r.ReplaceTakeProfitOrder(req.Symbol, req.CloseSide, req.OldTPOrderID, req.Qty, req.NewTPPrice, req.QtyPrecision, req.PricePrecision)
		if err != nil {
			return stopOut, tpOut, err
		}
	}
	return stopOut, tpOut, nil
}

func formatPrecision(v float64, prec int) string {
	if prec < 0 {
		prec = 8
	}
	return strconv.FormatFloat(v, 'f', prec, 64)
}
