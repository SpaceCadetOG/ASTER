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
	"go-machine/internal/scanneruniverse"
	"go-machine/internal/ws"
)

type whaleAggTrade struct {
	P string `json:"p"`
	Q string `json:"q"`
	T int64  `json:"T"`
	M bool   `json:"m"`
	S string `json:"s"`
}

type whaleStatus struct {
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

type whaleAssetState struct {
	LastUSD     float64
	LastSide    string
	LastTradeAt time.Time
	Recent      []whalePrint
}

type whalePrint struct {
	Ts           time.Time `json:"ts"`
	Side         string    `json:"side"`
	USD          float64   `json:"usd"`
	Tier         string    `json:"tier"`
	WindowCount  int       `json:"window_count"`
	BuyPct       float64   `json:"buy_pct"`
	Burst        bool      `json:"burst"`
	DominantSide string    `json:"dominant_side"`
}

type whaleAssetSnapshot struct {
	Symbol       string       `json:"symbol"`
	Subscribed   bool         `json:"subscribed"`
	Connected    bool         `json:"connected"`
	UpdatedAt    time.Time    `json:"updated_at"`
	MinUSD       float64      `json:"min_usd"`
	WindowSec    int          `json:"window_sec"`
	Count        int          `json:"count"`
	TotalUSD     float64      `json:"total_usd"`
	BuyUSD       float64      `json:"buy_usd"`
	SellUSD      float64      `json:"sell_usd"`
	DeltaUSD     float64      `json:"delta_usd"`
	LargeCount   int          `json:"large_count"`
	BuyPct       float64      `json:"buy_pct"`
	SellPct      float64      `json:"sell_pct"`
	Burst        bool         `json:"burst"`
	DominantSide string       `json:"dominant_side"`
	LastUSD      float64      `json:"last_usd,omitempty"`
	LastSide     string       `json:"last_side,omitempty"`
	LastTradeAt  *time.Time   `json:"last_trade_at,omitempty"`
	Recent       []whalePrint `json:"recent,omitempty"`
}

type whaleRuntime struct {
	mu      sync.RWMutex
	windows map[string]*flow.Window
	assets  map[string]whaleAssetState
}

func newWhaleRuntime() *whaleRuntime {
	return &whaleRuntime{
		windows: make(map[string]*flow.Window),
		assets:  make(map[string]whaleAssetState),
	}
}

func (s *whaleStatus) snapshot() whaleStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s
}

func (r *whaleRuntime) setWindow(symbol string, w *flow.Window) {
	r.mu.Lock()
	r.windows[symbol] = w
	r.mu.Unlock()
}

func (r *whaleRuntime) getWindow(symbol string) *flow.Window {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.windows[symbol]
}

func (r *whaleRuntime) record(symbol string, usd float64, side string, ts time.Time, recent whalePrint) {
	r.mu.Lock()
	asset := r.assets[symbol]
	asset.LastUSD = usd
	asset.LastSide = side
	asset.LastTradeAt = ts
	asset.Recent = append(asset.Recent, recent)
	if len(asset.Recent) > 20 {
		asset.Recent = append([]whalePrint(nil), asset.Recent[len(asset.Recent)-20:]...)
	}
	r.assets[symbol] = asset
	r.mu.Unlock()
}

func (r *whaleRuntime) snapshot(symbol string, st *whaleStatus, burstCount int, imbalancePct float64) whaleAssetSnapshot {
	stats := flow.Stats{}
	asset := whaleAssetState{}
	if w := r.getWindow(symbol); w != nil {
		stats = w.Snapshot()
	}
	r.mu.RLock()
	asset = r.assets[symbol]
	r.mu.RUnlock()
	dominant := "NEUTRAL"
	if stats.BuyPct >= imbalancePct {
		dominant = "BUY"
	} else if stats.SellPct >= imbalancePct {
		dominant = "SELL"
	}
	snap := st.snapshot()
	out := whaleAssetSnapshot{
		Symbol:       symbol,
		Subscribed:   r.getWindow(symbol) != nil,
		Connected:    snap.Connected,
		UpdatedAt:    snap.UpdatedAt,
		MinUSD:       snap.MinUSD,
		WindowSec:    snap.WindowSec,
		Count:        stats.Count,
		TotalUSD:     stats.TotalUSD,
		BuyUSD:       stats.BuyUSD,
		SellUSD:      stats.SellUSD,
		DeltaUSD:     stats.DeltaUSD,
		LargeCount:   stats.LargeCount,
		BuyPct:       stats.BuyPct,
		SellPct:      stats.SellPct,
		Burst:        stats.Count >= burstCount,
		DominantSide: dominant,
		LastUSD:      asset.LastUSD,
		LastSide:     asset.LastSide,
		Recent:       append([]whalePrint(nil), asset.Recent...),
	}
	if !asset.LastTradeAt.IsZero() {
		out.LastTradeAt = &asset.LastTradeAt
	}
	return out
}

func normalizeAssetSymbol(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.TrimSuffix(s, "USDT")
	s = strings.TrimSuffix(s, "USD")
	return s
}

func startStatusServer(addr string, st *whaleStatus, rt *whaleRuntime, burstCount int, imbalancePct float64) {
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
		_ = json.NewEncoder(w).Encode(rt.snapshot(symbol, st, burstCount, imbalancePct))
	})
	go func() {
		fmt.Println("whale status server:", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Println("whale status server error:", err)
		}
	}()
}

func main() {
	syms := scanneruniverse.ResolveCSVOrScanner("WHALE_SYMBOLS", defaultTapeSymbols(), []string{
		envStr("WHALE_LIVE_STATUS_URL", "http://127.0.0.1:8787/api/status"),
	})
	minUSD := envFloat("WHALE_MIN_USD", 500)
	tier1 := envFloat("WHALE_TIER1_USD", 500)
	tier2 := envFloat("WHALE_TIER2_USD", 500)
	tier3 := envFloat("WHALE_TIER3_USD", 1000)
	tier4 := envFloat("WHALE_TIER4_USD", 5000)
	windowSec := envInt("WHALE_WINDOW_SEC", 30)
	burstCount := envInt("WHALE_BURST_COUNT", 5)
	imbalancePct := envFloat("WHALE_IMBALANCE_PCT", 65)

	windowDur := time.Duration(windowSec) * time.Second
	rt := newWhaleRuntime()
	st := &whaleStatus{
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		MinUSD:    minUSD,
		WindowSec: windowSec,
	}
	for _, s := range syms {
		rt.setWindow(normalizeAssetSymbol(s), flow.NewWindow(windowDur, tier1))
	}

	streams := make([]string, 0, len(syms))
	for _, s := range syms {
		streams = append(streams, strings.ToLower(s)+"@aggTrade")
	}
	wsURL := "wss://fstream.asterdex.com/stream?streams=" + strings.Join(streams, "/")
	fmt.Printf("whale detector started (min=$%.0f, window=%ds)\n", minUSD, windowSec)
	fmt.Println(wsURL)
	startStatusServer(envStr("WHALE_HTTP_ADDR", ":8092"), st, rt, burstCount, imbalancePct)

	lastSummary := time.Now()
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

			var t whaleAggTrade
			if err := json.Unmarshal(msg.Data, &t); err != nil {
				continue
			}
			p, errP := strconv.ParseFloat(strings.TrimSpace(t.P), 64)
			q, errQ := strconv.ParseFloat(strings.TrimSpace(t.Q), 64)
			if errP != nil || errQ != nil || p <= 0 || q <= 0 {
				continue
			}
			usd := p * q
			if usd < minUSD {
				continue
			}

			symbol := strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(t.S)), "USDT")
			key := symbol
			w := rt.getWindow(key)
			if w == nil {
				w = flow.NewWindow(windowDur, tier1)
				rt.setWindow(key, w)
			}

			isBuy := !t.M
			plainSide := "SELL"
			if isBuy {
				plainSide = "BUY"
			}
			eventTS := time.UnixMilli(t.T)
			w.Add(flow.Event{
				Ts:    eventTS,
				USD:   usd,
				IsBuy: isBuy,
			})
			st.mu.Lock()
			st.UpdatedAt = time.Now().UTC()
			st.Events++
			st.LastSymbol = symbol
			st.LastUSD = usd
			st.LastSide = plainSide
			st.mu.Unlock()
			s := w.Snapshot()

			side := "SELL"
			if isBuy {
				side = "BUY"
			}
			coloredSide := colorx.Sell(side)
			if isBuy {
				coloredSide = colorx.Buy(side)
			}

			tier := "$10k+"
			tierOut := colorx.Blue(tier)
			whales := "🐋"
			if usd >= tier4 {
				tier = "$100k+"
				tierOut = colorx.Sell(tier)
				whales = "🐋🐋🐋🐋"
			} else if usd >= tier3 {
				tier = "$50k+"
				tierOut = colorx.Gold(tier)
				whales = "🐋🐋🐋"
			} else if usd >= tier2 {
				tier = "$25k+"
				tierOut = colorx.Gold(tier)
				whales = "🐋🐋"
			}

			buyPct := s.BuyPct
			if buyPct < 0 {
				buyPct = 0
			}
			burst := "NO"
			if s.Count >= burstCount {
				burst = "YES"
			}
			dominant := ""
			if s.BuyPct >= imbalancePct {
				dominant = " dominant:BUY"
			} else if s.SellPct >= imbalancePct {
				dominant = " dominant:SELL"
			}
			dominantSide := "NEUTRAL"
			if s.BuyPct >= imbalancePct {
				dominantSide = "BUY"
			} else if s.SellPct >= imbalancePct {
				dominantSide = "SELL"
			}
			rt.record(symbol, usd, plainSide, eventTS, whalePrint{
				Ts:           eventTS,
				Side:         plainSide,
				USD:          usd,
				Tier:         tier,
				WindowCount:  s.Count,
				BuyPct:       buyPct,
				Burst:        s.Count >= burstCount,
				DominantSide: dominantSide,
			})

			sec := eventTS.Format("15:04:05")
			usdText := colorByWhaleTier(usd, tier1, tier2, tier3, tier4, compactUSD(usd))
			fmt.Printf("%s %s %-4s %-5s %s %s | window:%d buy%%:%d burst:%s%s\n",
				sec,
				whales,
				symbol,
				coloredSide,
				usdText,
				tierOut,
				s.Count,
				int(buyPct+0.5),
				burst,
				dominant,
			)

			if time.Since(lastSummary) >= 10*time.Second {
				printWhaleSummary(rt)
				lastSummary = time.Now()
			}
		}

		time.Sleep(1500 * time.Millisecond)
	}
}

func printWhaleSummary(rt *whaleRuntime) {
	type row struct {
		Sym      string
		TotalUSD float64
		DeltaUSD float64
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
		if s.TotalUSD <= 0 {
			continue
		}
		rows = append(rows, row{Sym: sym, TotalUSD: s.TotalUSD, DeltaUSD: s.DeltaUSD})
	}
	if len(rows) == 0 {
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TotalUSD > rows[j].TotalUSD })
	fmt.Println("---- whale summary (top whale USD in window) ----")
	for i := 0; i < len(rows) && i < 5; i++ {
		fmt.Printf("%d) %s total=$%.0f delta=$%+.0f\n", i+1, rows[i].Sym, rows[i].TotalUSD, rows[i].DeltaUSD)
	}
	sort.Slice(rows, func(i, j int) bool { return abs(rows[i].DeltaUSD) > abs(rows[j].DeltaUSD) })
	fmt.Println("---- whale summary (top net delta in window) ----")
	for i := 0; i < len(rows) && i < 5; i++ {
		fmt.Printf("%d) %s delta=$%+.0f total=$%.0f\n", i+1, rows[i].Sym, rows[i].DeltaUSD, rows[i].TotalUSD)
	}
}

func defaultTapeSymbols() []string {
	return []string{
		"btcusdt", "ethusdt", "usdtusdt", "bnbusdt", "xrpusdt", "solusdt",
		"adausdt", "dogeusdt", "maticusdt", "avaxusdt", "linkusdt", "ltcusdt",
		"atomusdt", "nearusdt", "etcusdt", "suiusdt", "hypeusdt", "pepeusdt",
		"shibusdt", "wldusdt", "usdcusdt", "fttusdt", "aptusdt", "ltusdt", "asterusdt",
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

func envStr(k, def string) string {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	return s
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func compactUSD(v float64) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("$%.1fm", v/1_000_000)
	case v >= 1000:
		return fmt.Sprintf("$%.0fk", v/1000)
	default:
		return fmt.Sprintf("$%.0f", v)
	}
}

func colorByWhaleTier(usd, t1, t2, t3, t4 float64, text string) string {
	switch {
	case usd >= t4:
		return colorx.Sell(text)
	case usd >= t3:
		return colorx.Gold(text)
	case usd >= t2:
		return colorx.Blue(text)
	case usd >= t1:
		return colorx.Gray(text)
	default:
		return text
	}
}
