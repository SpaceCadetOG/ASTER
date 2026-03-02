package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
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

func main() {
	syms := parseSymbols("WHALE_SYMBOLS", defaultTapeSymbols())
	minUSD := envFloat("WHALE_MIN_USD", 5000)
	tier1 := envFloat("WHALE_TIER1_USD", 10000)
	tier2 := envFloat("WHALE_TIER2_USD", 50000)
	tier3 := envFloat("WHALE_TIER3_USD", 150000)
	windowSec := envInt("WHALE_WINDOW_SEC", 30)
	burstCount := envInt("WHALE_BURST_COUNT", 5)
	imbalancePct := envFloat("WHALE_IMBALANCE_PCT", 65)

	windowDur := time.Duration(windowSec) * time.Second
	windows := make(map[string]*flow.Window, len(syms))
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

	lastSummary := time.Now()
	for {
		conn, err := ws.Dial(context.Background(), wsURL, 10*time.Second)
		if err != nil {
			fmt.Println("dial error:", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for {
			b, err := conn.ReadText(70 * time.Second)
			if err != nil {
				_ = conn.Close()
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
			w.Add(flow.Event{
				Ts:    time.UnixMilli(t.T),
				USD:   usd,
				IsBuy: isBuy,
			})
			s := w.Snapshot()

			side := "SELL"
			if isBuy {
				side = "BUY"
			}
			coloredSide := colorx.Sell(side)
			if isBuy {
				coloredSide = colorx.Buy(side)
			}

			tier := "TIER1"
			if usd >= tier3 {
				tier = "TIER3"
			} else if usd >= tier2 {
				tier = "TIER2"
			}
			tierOut := tier
			if tier == "TIER3" {
				tierOut = colorx.Sell(tier)
			} else if tier == "TIER2" {
				tierOut = colorx.Gold(tier)
			} else {
				tierOut = colorx.Blue(tier)
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
			fmt.Printf("%s %-4s %-5s %s %-6s window:%d trades | buy%%:%d | burst:%s%s\n",
				sec,
				symbol,
				coloredSide,
				colorx.BySize(usd, fmt.Sprintf("$%.0f", usd)),
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

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
