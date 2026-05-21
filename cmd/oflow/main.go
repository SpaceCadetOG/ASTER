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

	"go-machine/internal/flow"
	"go-machine/internal/scanneruniverse"
	"go-machine/internal/ws"
)

type aggTrade struct {
	P string `json:"p"`
	Q string `json:"q"`
	T int64  `json:"T"`
	M bool   `json:"m"`
	S string `json:"s"`
}

type oflowStatus struct {
	mu         sync.RWMutex
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Connected  bool      `json:"connected"`
	WindowSec  int       `json:"window_sec"`
	LargeUSD   float64   `json:"large_usd"`
	TopN       int       `json:"top_n"`
	LastSymbol string    `json:"last_symbol,omitempty"`
	LastUSD    float64   `json:"last_usd,omitempty"`
	LastSide   string    `json:"last_side,omitempty"`
	Events     int64     `json:"events"`
}

type oflowAssetSnapshot struct {
	Symbol      string       `json:"symbol"`
	Subscribed  bool         `json:"subscribed"`
	Connected   bool         `json:"connected"`
	UpdatedAt   time.Time    `json:"updated_at"`
	WindowSec   int          `json:"window_sec"`
	LargeUSD    float64      `json:"large_usd"`
	Count       int          `json:"count"`
	TotalUSD    float64      `json:"total_usd"`
	BuyUSD      float64      `json:"buy_usd"`
	SellUSD     float64      `json:"sell_usd"`
	DeltaUSD    float64      `json:"delta_usd"`
	LargeCount  int          `json:"large_count"`
	BuyPct      float64      `json:"buy_pct"`
	SellPct     float64      `json:"sell_pct"`
	Score       float64      `json:"score"`
	Signal      string       `json:"signal"`
	LastUSD     float64      `json:"last_usd,omitempty"`
	LastSide    string       `json:"last_side,omitempty"`
	LastTradeAt *time.Time   `json:"last_trade_at,omitempty"`
	Recent      []oflowPrint `json:"recent,omitempty"`
}

type oflowPrint struct {
	Ts         time.Time `json:"ts"`
	Side       string    `json:"side"`
	USD        float64   `json:"usd"`
	Score      float64   `json:"score"`
	Signal     string    `json:"signal"`
	DeltaUSD   float64   `json:"delta_usd"`
	WindowSec  int       `json:"window_sec"`
	LargeCount int       `json:"large_count"`
}

type oflowAssetState struct {
	LastUSD     float64
	LastSide    string
	LastTradeAt time.Time
	Recent      []oflowPrint
}

type oflowRuntime struct {
	mu      sync.RWMutex
	windows map[string]*flow.Window
	assets  map[string]oflowAssetState
}

func newOflowRuntime() *oflowRuntime {
	return &oflowRuntime{
		windows: make(map[string]*flow.Window),
		assets:  make(map[string]oflowAssetState),
	}
}

func (r *oflowRuntime) setWindow(symbol string, w *flow.Window) {
	r.mu.Lock()
	r.windows[symbol] = w
	r.mu.Unlock()
}

func (r *oflowRuntime) getWindow(symbol string) *flow.Window {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.windows[symbol]
}

func (r *oflowRuntime) record(symbol string, usd float64, side string, ts time.Time, recent oflowPrint) {
	r.mu.Lock()
	asset := r.assets[symbol]
	asset.LastUSD = usd
	asset.LastSide = side
	asset.LastTradeAt = ts
	asset.Recent = append(asset.Recent, recent)
	if len(asset.Recent) > 20 {
		asset.Recent = append([]oflowPrint(nil), asset.Recent[len(asset.Recent)-20:]...)
	}
	r.assets[symbol] = asset
	r.mu.Unlock()
}

func (r *oflowRuntime) snapshot(symbol string, st *oflowStatus) oflowAssetSnapshot {
	stats := flow.Stats{}
	asset := oflowAssetState{}
	if w := r.getWindow(symbol); w != nil {
		stats = w.Snapshot()
	}
	r.mu.RLock()
	asset = r.assets[symbol]
	r.mu.RUnlock()
	score := 50.0
	total := stats.BuyUSD + stats.SellUSD + 1
	if total > 0 {
		score = clamp(50+50*clamp(stats.DeltaUSD/total, -1, 1)+5*float64(stats.LargeCount), 0, 100)
	}
	signal := "NEUTRAL"
	if score > 70 {
		signal = "BULL"
	} else if score < 30 {
		signal = "BEAR"
	}
	out := oflowAssetSnapshot{
		Symbol:     symbol,
		Subscribed: r.getWindow(symbol) != nil,
		Connected:  st.snapshot().Connected,
		UpdatedAt:  st.snapshot().UpdatedAt,
		WindowSec:  st.snapshot().WindowSec,
		LargeUSD:   st.snapshot().LargeUSD,
		Count:      stats.Count,
		TotalUSD:   stats.TotalUSD,
		BuyUSD:     stats.BuyUSD,
		SellUSD:    stats.SellUSD,
		DeltaUSD:   stats.DeltaUSD,
		LargeCount: stats.LargeCount,
		BuyPct:     stats.BuyPct,
		SellPct:    stats.SellPct,
		Score:      score,
		Signal:     signal,
		LastUSD:    asset.LastUSD,
		LastSide:   asset.LastSide,
		Recent:     append([]oflowPrint(nil), asset.Recent...),
	}
	if !asset.LastTradeAt.IsZero() {
		out.LastTradeAt = &asset.LastTradeAt
	}
	return out
}

func (s *oflowStatus) snapshot() oflowStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s
}

func normalizeAssetSymbol(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.TrimSuffix(s, "USDT")
	s = strings.TrimSuffix(s, "USD")
	return s
}

func startStatusServer(addr string, st *oflowStatus, rt *oflowRuntime) {
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
	mux.HandleFunc("/api/asset", func(w http.ResponseWriter, r *http.Request) {
		symbol := normalizeAssetSymbol(r.URL.Query().Get("symbol"))
		if symbol == "" {
			http.Error(w, "missing symbol", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rt.snapshot(symbol, st))
	})
	go func() {
		fmt.Println("oflow status server:", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Println("oflow status server error:", err)
		}
	}()
}

func main() {
	syms := scanneruniverse.ResolveCSVOrScanner("OFLOW_SYMBOLS", []string{"btcusdt", "ethusdt", "solusdt"}, []string{
		envStr("OFLOW_LIVE_STATUS_URL", "http://127.0.0.1:8787/api/status"),
	})
	windowSec := envInt("OFLOW_WINDOW_SEC", 20)
	largeUSD := envFloat("OFLOW_LARGE_USD", 50)
	printEveryMS := envInt("OFLOW_PRINT_EVERY_MS", 1000)
	topN := envInt("OFLOW_TOP_N", 5)
	if topN <= 0 {
		topN = 5
	}

	windowDur := time.Duration(windowSec) * time.Second
	rt := newOflowRuntime()
	st := &oflowStatus{
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		WindowSec: windowSec,
		LargeUSD:  largeUSD,
		TopN:      topN,
	}
	for _, s := range syms {
		rt.setWindow(strings.ToUpper(strings.TrimSuffix(s, "usdt")), flow.NewWindow(windowDur, largeUSD))
	}

	streams := make([]string, 0, len(syms))
	for _, s := range syms {
		streams = append(streams, strings.ToLower(strings.TrimSpace(s))+"@aggTrade")
	}
	wsURL := "wss://fstream.asterdex.com/stream?streams=" + strings.Join(streams, "/")
	fmt.Printf("oflow started (window=%ds, large>=$%.0f)\n", windowSec, largeUSD)
	fmt.Println(wsURL)
	startStatusServer(envStr("OFLOW_HTTP_ADDR", ":8090"), st, rt)

	go func() {
		tk := time.NewTicker(time.Duration(printEveryMS) * time.Millisecond)
		defer tk.Stop()
		for range tk.C {
			printOflow(rt, windowSec, topN)
		}
	}()

	for {
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

			var msg struct {
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(b, &msg); err != nil || len(msg.Data) == 0 {
				continue
			}

			var t aggTrade
			if err := json.Unmarshal(msg.Data, &t); err != nil {
				continue
			}
			p, errP := strconv.ParseFloat(strings.TrimSpace(t.P), 64)
			q, errQ := strconv.ParseFloat(strings.TrimSpace(t.Q), 64)
			if errP != nil || errQ != nil || p <= 0 || q <= 0 {
				continue
			}
			usd := p * q
			side := "SELL"
			if !t.M {
				side = "BUY"
			}
			symbol := strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(t.S), "USDT"))
			w := rt.getWindow(symbol)
			if w == nil {
				w = flow.NewWindow(windowDur, largeUSD)
				rt.setWindow(symbol, w)
			}
			eventTS := time.UnixMilli(t.T)
			w.Add(flow.Event{
				Ts:    eventTS,
				USD:   usd,
				IsBuy: !t.M,
			})
			s := w.Snapshot()
			total := s.BuyUSD + s.SellUSD + 1
			score := clamp(50+50*clamp(s.DeltaUSD/total, -1, 1)+5*float64(s.LargeCount), 0, 100)
			signal := "NEUTRAL"
			if score > 70 {
				signal = "BULL"
			} else if score < 30 {
				signal = "BEAR"
			}
			rt.record(symbol, usd, side, eventTS, oflowPrint{
				Ts:         eventTS,
				Side:       side,
				USD:        usd,
				Score:      score,
				Signal:     signal,
				DeltaUSD:   s.DeltaUSD,
				WindowSec:  windowSec,
				LargeCount: s.LargeCount,
			})
			st.mu.Lock()
			st.UpdatedAt = time.Now().UTC()
			st.Events++
			st.LastSymbol = symbol
			st.LastUSD = usd
			st.LastSide = side
			st.mu.Unlock()
		}

		time.Sleep(1500 * time.Millisecond)
	}
}

func envStr(k, def string) string {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	return s
}

func printOflow(rt *oflowRuntime, windowSec int, topN int) {
	type row struct {
		Sym        string
		Score      float64
		Delta      float64
		Buy        float64
		Sell       float64
		LargeCount int
	}
	rt.mu.RLock()
	pairs := make(map[string]*flow.Window, len(rt.windows))
	for sym, w := range rt.windows {
		pairs[sym] = w
	}
	rt.mu.RUnlock()
	rows := make([]row, 0, len(pairs))
	for sym, w := range pairs {
		s := w.Snapshot()
		if s.Count == 0 {
			continue
		}
		total := s.BuyUSD + s.SellUSD + 1
		deltaPct := s.DeltaUSD / total
		score := 50 + 50*clamp(deltaPct, -1, 1) + 5*float64(s.LargeCount)
		score = clamp(score, 0, 100)
		rows = append(rows, row{
			Sym:        sym,
			Score:      score,
			Delta:      s.DeltaUSD,
			Buy:        s.BuyUSD,
			Sell:       s.SellUSD,
			LargeCount: s.LargeCount,
		})
	}
	if len(rows) == 0 {
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		return abs(rows[i].Score-50) > abs(rows[j].Score-50)
	})
	fmt.Println("---- oflow ----")
	for i := 0; i < len(rows) && i < topN; i++ {
		signal := "NEUTRAL"
		if rows[i].Score > 70 {
			signal = "BULL"
		} else if rows[i].Score < 30 {
			signal = "BEAR"
		}
		fmt.Printf("%s score=%.0f delta=$%+.0f buy=$%.0f sell=$%.0f large=%d window=%ds signal=%s\n",
			rows[i].Sym, rows[i].Score, rows[i].Delta, rows[i].Buy, rows[i].Sell, rows[i].LargeCount, windowSec, signal)
	}
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

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
