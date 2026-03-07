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

func (s *whaleStatus) snapshot() whaleStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s
}

func startStatusServer(addr string, st *whaleStatus) {
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
		fmt.Println("whale status server:", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Println("whale status server error:", err)
		}
	}()
}

func main() {
	syms := parseSymbols("WHALE_SYMBOLS", defaultTapeSymbols())
	minUSD := envFloat("WHALE_MIN_USD", 10000)
	tier1 := envFloat("WHALE_TIER1_USD", 10000)
	tier2 := envFloat("WHALE_TIER2_USD", 25000)
	tier3 := envFloat("WHALE_TIER3_USD", 50000)
	tier4 := envFloat("WHALE_TIER4_USD", 100000)
	windowSec := envInt("WHALE_WINDOW_SEC", 30)
	burstCount := envInt("WHALE_BURST_COUNT", 5)
	imbalancePct := envFloat("WHALE_IMBALANCE_PCT", 65)

	windowDur := time.Duration(windowSec) * time.Second
	windows := make(map[string]*flow.Window, len(syms))
	st := &whaleStatus{
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		MinUSD:    minUSD,
		WindowSec: windowSec,
	}
	for _, s := range syms {
		windows[strings.ToUpper(s)] = flow.NewWindow(windowDur, tier1)
	}

	streams := make([]string, 0, len(syms))
	for _, s := range syms {
		streams = append(streams, strings.ToLower(s)+"@aggTrade")
	}
	wsURL := "wss://fstream.asterdex.com/stream?streams=" + strings.Join(streams, "/")
	fmt.Printf("whale detector started (min=$%.0f, window=%ds)\n", minUSD, windowSec)
	fmt.Println(wsURL)
	startStatusServer(envStr("WHALE_HTTP_ADDR", ":8092"), st)

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
			w, ok := windows[key]
			if !ok {
				w = flow.NewWindow(windowDur, tier1)
				windows[key] = w
			}

			isBuy := !t.M
			plainSide := "SELL"
			if isBuy {
				plainSide = "BUY"
			}
			w.Add(flow.Event{
				Ts:    time.UnixMilli(t.T),
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

			sec := time.UnixMilli(t.T).Format("15:04:05")
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
				printWhaleSummary(windows)
				lastSummary = time.Now()
			}
		}

		time.Sleep(1500 * time.Millisecond)
	}
}

func printWhaleSummary(windows map[string]*flow.Window) {
	type row struct {
		Sym      string
		TotalUSD float64
		DeltaUSD float64
	}
	rows := make([]row, 0, len(windows))
	for sym, w := range windows {
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
