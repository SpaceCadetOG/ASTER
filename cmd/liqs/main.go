package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-machine/internal/colorx"
	"go-machine/internal/flow"
	"go-machine/internal/ws"
)

type liqEvt struct {
	Symbol string
	Side   string
	Price  float64
	Qty    float64
	Ts     time.Time
	USD    float64
}

type liqsStatus struct {
	mu         sync.RWMutex
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Connected  bool      `json:"connected"`
	MinUSD     float64   `json:"min_usd"`
	WindowSec  int       `json:"window_sec"`
	LastSymbol string    `json:"last_symbol,omitempty"`
	LastUSD    float64   `json:"last_usd,omitempty"`
	LastSide   string    `json:"last_side,omitempty"`
	Events     int64     `json:"events"`
}

func (s *liqsStatus) snapshot() liqsStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s
}

func startStatusServer(addr string, st *liqsStatus) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st.snapshot())
	})
	go func() {
		fmt.Println("liqs status server:", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Println("liqs status server error:", err)
		}
	}()
}

func main() {
	syms := parseSymbols("LIQ_SYMBOLS", nil)
	minUSD := envFloat("LIQ_MIN_USD", 0)
	windowSec := envInt("LIQ_WINDOW_SEC", 60)
	printRaw := envBool("LIQ_PRINT_RAW", false)
	tier1 := envFloat("LIQ_TIER1_USD", 20000)
	tier2 := envFloat("LIQ_TIER2_USD", 100000)
	tier3 := envFloat("LIQ_TIER3_USD", 500000)

	windowDur := time.Duration(windowSec) * time.Second
	windows := map[string]*flow.Window{}
	st := &liqsStatus{
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		MinUSD:    minUSD,
		WindowSec: windowSec,
	}
	allowed := map[string]struct{}{}
	for _, s := range syms {
		key := strings.ToUpper(strings.TrimSuffix(s, "usdt"))
		windows[key] = flow.NewWindow(windowDur, minUSD)
		allowed[key] = struct{}{}
	}

	// Liquidation stream names per Aster futures docs are compatible with:
	//  - "<symbol>@forceOrder" (per symbol)
	//  - "!forceOrder@arr"     (all symbols array)
	// Fallback behavior requested: try docs stream first, then alternative naming.
	// Docs: https://github.com/asterdex/api-docs/blob/master/README.md
	urls := liquidationURLs(syms)
	urlIdx := 0
	lastSummary := time.Now()
	startStatusServer(envStr("LIQS_HTTP_ADDR", ":8093"), st)

	for {
		wsURL := urls[urlIdx%len(urls)]
		urlIdx++
		fmt.Println("liqs connecting:", wsURL)

		conn, err := ws.Dial(context.Background(), wsURL, 10*time.Second)
		if err != nil {
			st.mu.Lock()
			st.Connected = false
			st.UpdatedAt = time.Now().UTC()
			st.mu.Unlock()
			fmt.Println("dial error:", err)
			time.Sleep(2 * time.Second)
			continue
		}
		st.mu.Lock()
		st.Connected = true
		st.UpdatedAt = time.Now().UTC()
		st.mu.Unlock()

		for {
			b, err := conn.ReadText(70 * time.Second)
			if err != nil {
				_ = conn.Close()
				st.mu.Lock()
				st.Connected = false
				st.UpdatedAt = time.Now().UTC()
				st.mu.Unlock()
				break
			}
			if printRaw {
				fmt.Println("raw:", string(b))
			}

			events := parseLiquidationEvents(b)
			for _, e := range events {
				if e.USD <= 0 || e.Symbol == "" {
					continue
				}
				sym := strings.ToUpper(strings.TrimSuffix(e.Symbol, "USDT"))
				if len(allowed) > 0 {
					if _, ok := allowed[sym]; !ok {
						continue
					}
				}

				w, ok := windows[sym]
				if !ok {
					w = flow.NewWindow(windowDur, minUSD)
					windows[sym] = w
				}
				w.Add(flow.Event{Ts: e.Ts, USD: e.USD, IsBuy: strings.EqualFold(e.Side, "BUY")})
				st.mu.Lock()
				st.UpdatedAt = time.Now().UTC()
				st.Events++
				st.LastSymbol = sym
				st.LastUSD = e.USD
				st.LastSide = e.Side
				st.mu.Unlock()

				if e.USD >= minUSD {
					side := colorx.Sell(e.Side)
					if strings.EqualFold(e.Side, "BUY") {
						side = colorx.Buy(e.Side)
					}
					usdText := colorByUSD(e.USD, tier1, tier2, tier3, fmt.Sprintf("$%.0f", e.USD))
					fmt.Printf("%s LIQ %-5s %-4s %s price=%s qty=%s\n",
						e.Ts.Format("15:04:05"),
						sym,
						side,
						usdText,
						formatFloat(e.Price),
						formatFloat(e.Qty),
					)
				}
			}

			if time.Since(lastSummary) >= 15*time.Second {
				printLiqSummary(windows)
				lastSummary = time.Now()
			}
		}

		time.Sleep(1500 * time.Millisecond)
	}
}

func liquidationURLs(syms []string) []string {
	// Primary: all-liquidations stream.
	urls := []string{"wss://fstream.asterdex.com/stream?streams=!forceOrder@arr"}
	if len(syms) == 0 {
		return urls
	}
	perSymStreams := make([]string, 0, len(syms))
	for _, s := range syms {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			perSymStreams = append(perSymStreams, s+"@forceOrder")
		}
	}
	if len(perSymStreams) > 0 {
		urls = append(urls, "wss://fstream.asterdex.com/stream?streams="+strings.Join(perSymStreams, "/"))
	}
	return urls
}

func parseLiquidationEvents(raw []byte) []liqEvt {
	out := make([]liqEvt, 0, 2)

	var wrap struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Data) > 0 {
		out = append(out, parseLiquidationPayload(wrap.Data)...)
		return out
	}
	out = append(out, parseLiquidationPayload(raw)...)
	return out
}

func parseLiquidationPayload(data []byte) []liqEvt {
	events := make([]liqEvt, 0, 2)
	// object form
	if e, ok := parseLiquidationObject(data); ok {
		events = append(events, e)
		return events
	}
	// array form
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return events
	}
	for _, it := range arr {
		if e, ok := parseLiquidationObject(it); ok {
			events = append(events, e)
		}
	}
	return events
}

func parseLiquidationObject(data []byte) (liqEvt, bool) {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return liqEvt{}, false
	}

	// forceOrder schema usually nests order in "o".
	src := obj
	if o, ok := obj["o"].(map[string]any); ok {
		src = o
	}
	symbol := asString(src["s"])
	if symbol == "" {
		symbol = asString(obj["s"])
	}
	side := strings.ToUpper(asString(src["S"]))
	if side == "" {
		side = strings.ToUpper(asString(obj["S"]))
	}
	price := asFloat(src["ap"])
	if price <= 0 {
		price = asFloat(src["p"])
	}
	qty := asFloat(src["z"])
	if qty <= 0 {
		qty = asFloat(src["q"])
	}
	if qty <= 0 {
		qty = asFloat(src["l"])
	}
	tsMs := asInt64(src["T"])
	if tsMs <= 0 {
		tsMs = asInt64(obj["E"])
	}
	if tsMs <= 0 {
		tsMs = time.Now().UnixMilli()
	}

	e := liqEvt{
		Symbol: strings.ToUpper(symbol),
		Side:   side,
		Price:  price,
		Qty:    qty,
		Ts:     time.UnixMilli(tsMs),
	}
	e.USD = e.Price * e.Qty
	if e.Symbol == "" || e.Side == "" || e.USD <= 0 {
		return liqEvt{}, false
	}
	return e, true
}

func printLiqSummary(windows map[string]*flow.Window) {
	type row struct {
		Sym     string
		Total   float64
		BuyUSD  float64
		SellUSD float64
		MaxOne  float64
	}
	rows := make([]row, 0, len(windows))
	for sym, w := range windows {
		s := w.Snapshot()
		if s.TotalUSD <= 0 {
			continue
		}
		maxOne := 0.0
		for _, e := range w.Events() {
			if e.USD > maxOne {
				maxOne = e.USD
			}
		}
		rows = append(rows, row{
			Sym:     sym,
			Total:   s.TotalUSD,
			BuyUSD:  s.BuyUSD,
			SellUSD: s.SellUSD,
			MaxOne:  maxOne,
		})
	}
	if len(rows) == 0 {
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Total > rows[j].Total })
	fmt.Println("---- liquidation summary (window) ----")
	for i := 0; i < len(rows) && i < 5; i++ {
		net := "BUY"
		if rows[i].SellUSD > rows[i].BuyUSD {
			net = "SELL"
		}
		fmt.Printf("%d) %s total=$%.0f max=$%.0f net=%s\n", i+1, rows[i].Sym, rows[i].Total, rows[i].MaxOne, net)
	}
}

func parseSymbols(envName string, def []string) []string {
	raw := strings.TrimSpace(os.Getenv(envName))
	if raw == "" {
		return def
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.ToLower(strings.TrimSpace(p))
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func envFloat(k string, def float64) float64 {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

func envInt(k string, def int) int {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

func envBool(k string, def bool) bool {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	s = strings.ToLower(s)
	return !(s == "0" || s == "false" || s == "no" || s == "off")
}

func envStr(k, def string) string {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	return s
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func asFloat(v any) float64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	default:
		f, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(v)), 64)
		return f
	}
}

func asInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	default:
		n, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(v)), 10, 64)
		return n
	}
}

func formatFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 8, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}

func colorByUSD(usd, tier1, tier2, tier3 float64, text string) string {
	switch {
	case usd >= tier3:
		return colorx.Sell(text)
	case usd >= tier2:
		return colorx.Gold(text)
	case usd >= tier1:
		return colorx.Blue(text)
	default:
		return colorx.Gray(text)
	}
}
