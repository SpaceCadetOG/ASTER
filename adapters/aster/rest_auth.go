package aster

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RESTAuth is a minimal Binance-style REST client for Aster Perps that supports
// both public and signed endpoints (orders/positions) plus listenKey.
//
// Python reference: aster/aster_api.py in aster.zip.
type RESTAuth struct {
	BaseURL      string
	WSBaseURL    string
	APIKey       string
	APISecret    string
	RecvWindowMS int64
	Timeout      time.Duration
	UseSrvTime   bool

	http   *http.Client
	mu     sync.Mutex
	offset int64 // ms

	exMu         sync.Mutex
	exchangeInfo *exchangeInfo
	symMeta      map[string]SymbolMeta
}

func NewRESTAuth(apiKey, apiSecret string) *RESTAuth {
	tr := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &RESTAuth{
		BaseURL:      "https://fapi.asterdex.com",
		WSBaseURL:    "wss://fstream.asterdex.com",
		APIKey:       apiKey,
		APISecret:    apiSecret,
		RecvWindowMS: 5000,
		Timeout:      15 * time.Second,
		UseSrvTime:   true,
		http:         &http.Client{Timeout: 15 * time.Second, Transport: tr},
		symMeta:      map[string]SymbolMeta{},
	}
}

// ---- time helpers ----

func (c *RESTAuth) nowMS() int64 { return time.Now().UnixMilli() }

func (c *RESTAuth) timestampMS() int64 {
	c.mu.Lock()
	off := c.offset
	c.mu.Unlock()
	return c.nowMS() + off
}

type serverTimeResp struct {
	ServerTime int64 `json:"serverTime"`
}

func (c *RESTAuth) GetServerTime() (int64, error) {
	var st serverTimeResp
	if err := c.doJSON(http.MethodGet, "/fapi/v1/time", nil, false, true, &st); err != nil {
		return 0, err
	}
	if st.ServerTime == 0 {
		return 0, errors.New("serverTime missing")
	}
	return st.ServerTime, nil
}

func (c *RESTAuth) SyncTime() error {
	ms, err := c.GetServerTime()
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.offset = ms - c.nowMS()
	c.mu.Unlock()
	return nil
}

// ---- exchange info / symbol meta ----

type exchangeInfo struct {
	Symbols []struct {
		Symbol  string `json:"symbol"`
		Filters []struct {
			FilterType string `json:"filterType"`
			TickSize   string `json:"tickSize"`
			StepSize   string `json:"stepSize"`
		} `json:"filters"`
	} `json:"symbols"`
}

// SymbolMeta captures tick/step sizes and derived precision.
type SymbolMeta struct {
	TickSize       float64
	StepSize       float64
	PricePrecision int
	QtyPrecision   int
}

func decimalPlaces(step string) int {
	if strings.Contains(step, "e") || strings.Contains(step, "E") {
		// rare; ignore
		return 8
	}
	parts := strings.Split(step, ".")
	if len(parts) != 2 {
		return 0
	}
	s := strings.TrimRight(parts[1], "0")
	return len(s)
}

func (c *RESTAuth) ExchangeInfo(refresh bool) (*exchangeInfo, error) {
	c.exMu.Lock()
	cached := c.exchangeInfo
	c.exMu.Unlock()
	if cached != nil && !refresh {
		return cached, nil
	}
	var ei exchangeInfo
	if err := c.doJSON(http.MethodGet, "/fapi/v1/exchangeInfo", nil, false, true, &ei); err != nil {
		return nil, err
	}
	c.exMu.Lock()
	c.exchangeInfo = &ei
	c.exMu.Unlock()
	return &ei, nil
}

func (c *RESTAuth) SymbolMeta(symbol string, refresh bool) (SymbolMeta, error) {
	symbol = strings.ToUpper(symbol)

	c.exMu.Lock()
	if m, ok := c.symMeta[symbol]; ok && !refresh {
		c.exMu.Unlock()
		return m, nil
	}
	c.exMu.Unlock()

	ei, err := c.ExchangeInfo(refresh)
	if err != nil {
		return SymbolMeta{}, err
	}
	var tick, step string
	for _, s := range ei.Symbols {
		if s.Symbol != symbol {
			continue
		}
		for _, f := range s.Filters {
			switch f.FilterType {
			case "PRICE_FILTER":
				tick = f.TickSize
			case "LOT_SIZE":
				step = f.StepSize
			}
		}
		break
	}
	if tick == "" || step == "" {
		return SymbolMeta{}, fmt.Errorf("missing PRICE_FILTER/LOT_SIZE for %s", symbol)
	}
	tickF, _ := strconv.ParseFloat(tick, 64)
	stepF, _ := strconv.ParseFloat(step, 64)
	meta := SymbolMeta{
		TickSize:       tickF,
		StepSize:       stepF,
		PricePrecision: decimalPlaces(tick),
		QtyPrecision:   decimalPlaces(step),
	}
	c.exMu.Lock()
	c.symMeta[symbol] = meta
	c.exMu.Unlock()
	return meta, nil
}

func quantizeDown(v, step float64) float64 {
	if step == 0 {
		return v
	}
	return math.Floor(v/step) * step
}

func (c *RESTAuth) RoundPrice(symbol string, price float64) (float64, SymbolMeta, error) {
	meta, err := c.SymbolMeta(symbol, false)
	if err != nil {
		return 0, SymbolMeta{}, err
	}
	p := quantizeDown(price, meta.TickSize)
	// re-quantize to precision to avoid float tails
	pow := math.Pow10(meta.PricePrecision)
	p = math.Floor(p*pow+1e-9) / pow
	return p, meta, nil
}

func (c *RESTAuth) RoundQty(symbol string, qty float64) (float64, SymbolMeta, error) {
	meta, err := c.SymbolMeta(symbol, false)
	if err != nil {
		return 0, SymbolMeta{}, err
	}
	q := quantizeDown(qty, meta.StepSize)
	pow := math.Pow10(meta.QtyPrecision)
	q = math.Floor(q*pow+1e-9) / pow
	return q, meta, nil
}

// ---- core request/signing ----

func (c *RESTAuth) sign(vals url.Values) (string, error) {
	if c.APISecret == "" {
		return "", errors.New("api secret required")
	}
	mac := hmac.New(sha256.New, []byte(c.APISecret))
	mac.Write([]byte(vals.Encode()))
	return fmt.Sprintf("%x", mac.Sum(nil)), nil
}

func (c *RESTAuth) doJSON(method, endpoint string, vals url.Values, signed bool, addAPIKey bool, out any) error {
	b, err := c.doRaw(method, endpoint, vals, signed, addAPIKey)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

func (c *RESTAuth) doRaw(method, endpoint string, vals url.Values, signed bool, addAPIKey bool) ([]byte, error) {
	if vals == nil {
		vals = url.Values{}
	}

	// Signed endpoints: add timestamp/recvWindow/signature
	if signed {
		if c.UseSrvTime {
			c.mu.Lock()
			off := c.offset
			c.mu.Unlock()
			if off == 0 {
				_ = c.SyncTime() // best-effort
			}
		}
		vals.Set("timestamp", strconv.FormatInt(c.timestampMS(), 10))
		if c.RecvWindowMS > 0 {
			vals.Set("recvWindow", strconv.FormatInt(c.RecvWindowMS, 10))
		}
		sig, err := c.sign(vals)
		if err != nil {
			return nil, err
		}
		vals.Set("signature", sig)
	}

	full := c.BaseURL + endpoint
	var req *http.Request
	var err error

	method = strings.ToUpper(method)
	switch method {
	case http.MethodGet, http.MethodDelete:
		if len(vals) > 0 {
			full = full + "?" + vals.Encode()
		}
		req, err = http.NewRequest(method, full, nil)
	case http.MethodPost, http.MethodPut:
		body := bytes.NewBufferString(vals.Encode())
		req, err = http.NewRequest(method, full, body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if addAPIKey && c.APIKey != "" {
		req.Header.Set("X-MBX-APIKEY", c.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	bb, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(bb)))
	}
	return bb, nil
}

// ---- Public endpoints (subset used by execution helpers) ----

type bookTicker struct {
	Symbol string `json:"symbol"`
	Bid    string `json:"bidPrice"`
	Ask    string `json:"askPrice"`
}

func (c *RESTAuth) BookTicker(symbol string) (bid, ask float64, err error) {
	vals := url.Values{}
	if symbol != "" {
		vals.Set("symbol", strings.ToUpper(symbol))
	}
	var bt bookTicker
	if err := c.doJSON(http.MethodGet, "/fapi/v1/ticker/bookTicker", vals, false, true, &bt); err != nil {
		return 0, 0, err
	}
	bid, _ = strconv.ParseFloat(bt.Bid, 64)
	ask, _ = strconv.ParseFloat(bt.Ask, 64)
	return bid, ask, nil
}

// ---- Signed endpoints ----

func (c *RESTAuth) PositionRisk(symbol string) ([]map[string]any, error) {
	vals := url.Values{}
	if symbol != "" {
		vals.Set("symbol", strings.ToUpper(symbol))
	}
	var out []map[string]any
	err := c.doJSON(http.MethodGet, "/fapi/v3/positionRisk", vals, true, true, &out)
	return out, err
}

func (c *RESTAuth) OpenOrders(symbol string) ([]map[string]any, error) {
	vals := url.Values{}
	if symbol != "" {
		vals.Set("symbol", strings.ToUpper(symbol))
	}
	var out []map[string]any
	err := c.doJSON(http.MethodGet, "/fapi/v3/openOrders", vals, true, true, &out)
	return out, err
}

func (c *RESTAuth) GetOrder(symbol string, orderID int64) (map[string]any, error) {
	vals := url.Values{}
	vals.Set("symbol", strings.ToUpper(symbol))
	vals.Set("orderId", strconv.FormatInt(orderID, 10))
	var out map[string]any
	err := c.doJSON(http.MethodGet, "/fapi/v3/order", vals, true, true, &out)
	return out, err
}

func (c *RESTAuth) CancelOrder(symbol string, orderID int64) (map[string]any, error) {
	vals := url.Values{}
	vals.Set("symbol", strings.ToUpper(symbol))
	vals.Set("orderId", strconv.FormatInt(orderID, 10))
	var out map[string]any
	err := c.doJSON(http.MethodDelete, "/fapi/v3/order", vals, true, true, &out)
	return out, err
}

func (c *RESTAuth) CancelAllOrders(symbol string) (map[string]any, error) {
	vals := url.Values{}
	vals.Set("symbol", strings.ToUpper(symbol))
	var out map[string]any
	err := c.doJSON(http.MethodDelete, "/fapi/v3/allOpenOrders", vals, true, true, &out)
	return out, err
}

func (c *RESTAuth) ChangeLeverage(symbol string, lev int) (map[string]any, error) {
	vals := url.Values{}
	vals.Set("symbol", strings.ToUpper(symbol))
	vals.Set("leverage", strconv.Itoa(lev))
	var out map[string]any
	err := c.doJSON(http.MethodPost, "/fapi/v3/leverage", vals, true, true, &out)
	return out, err
}

func (c *RESTAuth) PlaceOrder(params url.Values) (map[string]any, error) {
	var out map[string]any
	err := c.doJSON(http.MethodPost, "/fapi/v3/order", params, true, true, &out)
	return out, err
}

// listenKey endpoints are NOT signed, but require API key header.

type listenKeyResp struct {
	ListenKey string `json:"listenKey"`
}

func (c *RESTAuth) StartListenKey() (string, error) {
	var out listenKeyResp
	if err := c.doJSON(http.MethodPost, "/fapi/v1/listenKey", nil, false, true, &out); err != nil {
		return "", err
	}
	if out.ListenKey == "" {
		return "", errors.New("listenKey missing")
	}
	return out.ListenKey, nil
}

func (c *RESTAuth) KeepAliveListenKey() error {
	_, err := c.doRaw(http.MethodPut, "/fapi/v1/listenKey", nil, false, true)
	return err
}

func (c *RESTAuth) CloseListenKey() error {
	_, err := c.doRaw(http.MethodDelete, "/fapi/v1/listenKey", nil, false, true)
	return err
}
