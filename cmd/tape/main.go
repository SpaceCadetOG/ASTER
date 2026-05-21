// cmd/tape/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-machine/internal/colorx"
	"go-machine/internal/flow"
	"go-machine/internal/scanneruniverse"
	"go-machine/internal/ws"
)

type tapeStatus struct {
	mu         sync.RWMutex
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Connected  bool      `json:"connected"`
	MinUSD     float64   `json:"min_usd"`
	LastSymbol string    `json:"last_symbol,omitempty"`
	LastUSD    float64   `json:"last_usd,omitempty"`
	LastSide   string    `json:"last_side,omitempty"`
	Events     int64     `json:"events"`
}

type tapePrint struct {
	Ts    time.Time `json:"ts"`
	Side  string    `json:"side"`
	USD   float64   `json:"usd"`
	Price float64   `json:"price"`
	Qty   float64   `json:"qty"`
}

type tapeAsset struct {
	window *flow.Window
	recent []tapePrint
}

type tapeRuntime struct {
	mu        sync.RWMutex
	windowSec int
	assets    map[string]*tapeAsset
}

type tapeAssetSnapshot struct {
	Symbol      string      `json:"symbol"`
	Subscribed  bool        `json:"subscribed"`
	Connected   bool        `json:"connected"`
	UpdatedAt   time.Time   `json:"updated_at"`
	MinUSD      float64     `json:"min_usd"`
	WindowSec   int         `json:"window_sec"`
	Count       int         `json:"count"`
	TotalUSD    float64     `json:"total_usd"`
	BuyUSD      float64     `json:"buy_usd"`
	SellUSD     float64     `json:"sell_usd"`
	DeltaUSD    float64     `json:"delta_usd"`
	BuyPct      float64     `json:"buy_pct"`
	SellPct     float64     `json:"sell_pct"`
	LastUSD     float64     `json:"last_usd,omitempty"`
	LastSide    string      `json:"last_side,omitempty"`
	LastPrice   float64     `json:"last_price,omitempty"`
	LastQty     float64     `json:"last_qty,omitempty"`
	LastTradeAt *time.Time  `json:"last_trade_at,omitempty"`
	Recent      []tapePrint `json:"recent,omitempty"`
}

func newTapeRuntime(windowSec int) *tapeRuntime {
	if windowSec <= 0 {
		windowSec = 60
	}
	return &tapeRuntime{
		windowSec: windowSec,
		assets:    make(map[string]*tapeAsset),
	}
}

func (r *tapeRuntime) subscribe(symbol string, minUSD float64) {
	key := normalizeAssetSymbol(symbol)
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.assets[key] != nil {
		return
	}
	r.assets[key] = &tapeAsset{
		window: flow.NewWindow(time.Duration(r.windowSec)*time.Second, minUSD),
		recent: make([]tapePrint, 0, 16),
	}
}

func normalizeAssetSymbol(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.TrimSuffix(s, "USDT")
	s = strings.TrimSuffix(s, "USD")
	return s
}

func (s *tapeStatus) snapshot() tapeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s
}

func (r *tapeRuntime) record(symbol string, evt tapePrint, minUSD float64) {
	symbol = normalizeAssetSymbol(symbol)
	r.mu.Lock()
	asset := r.assets[symbol]
	if asset == nil {
		asset = &tapeAsset{
			window: flow.NewWindow(time.Duration(r.windowSec)*time.Second, minUSD),
			recent: make([]tapePrint, 0, 16),
		}
		r.assets[symbol] = asset
	}
	asset.recent = append(asset.recent, evt)
	if len(asset.recent) > 20 {
		asset.recent = append([]tapePrint(nil), asset.recent[len(asset.recent)-20:]...)
	}
	r.mu.Unlock()

	asset.window.Add(flow.Event{
		Ts:    evt.Ts,
		USD:   evt.USD,
		IsBuy: evt.Side == "BUY",
	})
}

func (r *tapeRuntime) snapshot(symbol string, st *tapeStatus) tapeAssetSnapshot {
	key := normalizeAssetSymbol(symbol)
	r.mu.RLock()
	asset := r.assets[key]
	recent := []tapePrint(nil)
	if asset != nil && len(asset.recent) > 0 {
		recent = append([]tapePrint(nil), asset.recent...)
	}
	r.mu.RUnlock()

	stats := flow.Stats{}
	if asset != nil {
		stats = asset.window.Snapshot()
	}
	snap := st.snapshot()
	out := tapeAssetSnapshot{
		Symbol:     key,
		Subscribed: asset != nil,
		Connected:  snap.Connected,
		UpdatedAt:  snap.UpdatedAt,
		MinUSD:     snap.MinUSD,
		WindowSec:  r.windowSec,
		Count:      stats.Count,
		TotalUSD:   stats.TotalUSD,
		BuyUSD:     stats.BuyUSD,
		SellUSD:    stats.SellUSD,
		DeltaUSD:   stats.DeltaUSD,
		BuyPct:     stats.BuyPct,
		SellPct:    stats.SellPct,
		Recent:     recent,
	}
	if len(recent) > 0 {
		last := recent[len(recent)-1]
		out.LastUSD = last.USD
		out.LastSide = last.Side
		out.LastPrice = last.Price
		out.LastQty = last.Qty
		out.LastTradeAt = &last.Ts
	}
	return out
}

func startStatusServer(addr string, st *tapeStatus, rt *tapeRuntime) {
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
		fmt.Println("tape status server:", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Println("tape status server error:", err)
		}
	}()
}

func main() {
	syms := scanneruniverse.ResolveCSVOrScanner("TAPE_SYMBOLS", defaultTapeSymbols(), []string{
		envStr("TAPE_LIVE_STATUS_URL", "http://127.0.0.1:8787/api/status"),
	})

	minUSD := 500.0
	if v := strings.TrimSpace(os.Getenv("TAPE_MIN_USD")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			minUSD = f
		}
	}

	streams := make([]string, 0, len(syms))
	for _, s := range syms {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			streams = append(streams, s+"@aggTrade")
		}
	}

	wsURL := "wss://fstream.asterdex.com/stream?streams=" + strings.Join(streams, "/")
	fmt.Printf("ASTER tape connected (min $%.0f)\n", minUSD)
	fmt.Println(wsURL)
	rt := newTapeRuntime(60)
	for _, s := range syms {
		rt.subscribe(s, minUSD)
	}
	st := &tapeStatus{
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		MinUSD:    minUSD,
	}
	startStatusServer(envStr("TAPE_HTTP_ADDR", ":8091"), st, rt)

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

			var t struct {
				P string `json:"p"`
				Q string `json:"q"`
				T int64  `json:"T"`
				M bool   `json:"m"`
				S string `json:"s"`
			}
			if err := json.Unmarshal(msg.Data, &t); err != nil {
				continue
			}

			p, errP := strconv.ParseFloat(t.P, 64)
			q, errQ := strconv.ParseFloat(t.Q, 64)
			if errP != nil || errQ != nil || p <= 0 || q <= 0 {
				continue
			}

			usd := p * q
			if usd < minUSD {
				continue
			}

			side := "BUY"
			if t.M {
				side = "SELL"
			}

			if side == "BUY" {
				side = colorx.Buy(side)
			} else {
				side = colorx.Sell(side)
			}
			plainSide := "SELL"
			if !t.M {
				plainSide = "BUY"
			}

			size := colorx.BySize(usd, fmt.Sprintf("$%.0f", usd))
			sym := strings.TrimSuffix(strings.ToUpper(t.S), "USDT")
			eventTS := time.UnixMilli(t.T)
			rt.record(sym, tapePrint{
				Ts:    eventTS,
				Side:  plainSide,
				USD:   usd,
				Price: p,
				Qty:   q,
			}, minUSD)
			st.mu.Lock()
			st.UpdatedAt = time.Now().UTC()
			st.Events++
			st.LastSymbol = sym
			st.LastUSD = usd
			st.LastSide = plainSide
			st.mu.Unlock()
			sec := eventTS.Format("15:04:05")

			fmt.Printf("%s %s %-8s %s\n", sec, side, sym, size)
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

func defaultTapeSymbols() []string {
	return []string{
		"btcusdt", "ethusdt", "usdtusdt", "bnbusdt", "xrpusdt", "solusdt",
		"adausdt", "dogeusdt", "maticusdt", "avaxusdt", "linkusdt", "ltcusdt",
		"atomusdt", "nearusdt", "etcusdt", "suiusdt", "hypeusdt", "pepeusdt",
		"shibusdt", "wldusdt", "usdcusdt", "fttusdt", "aptusdt", "ltusdt", "asterusdt",
	}
}
