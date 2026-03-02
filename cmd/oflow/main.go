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

	"go-machine/internal/flow"
	"go-machine/internal/ws"
)

type aggTrade struct {
	P string `json:"p"`
	Q string `json:"q"`
	T int64  `json:"T"`
	M bool   `json:"m"`
	S string `json:"s"`
}

func main() {
	syms := parseSymbols("OFLOW_SYMBOLS", []string{"btcusdt", "ethusdt", "solusdt"})
	windowSec := envInt("OFLOW_WINDOW_SEC", 20)
	largeUSD := envFloat("OFLOW_LARGE_USD", 5000)
	printEveryMS := envInt("OFLOW_PRINT_EVERY_MS", 1000)
	topN := envInt("OFLOW_TOP_N", 5)
	if topN <= 0 {
		topN = 5
	}

	windowDur := time.Duration(windowSec) * time.Second
	windows := make(map[string]*flow.Window, len(syms))
	for _, s := range syms {
		windows[strings.ToUpper(strings.TrimSuffix(s, "usdt"))] = flow.NewWindow(windowDur, largeUSD)
	}

	streams := make([]string, 0, len(syms))
	for _, s := range syms {
		streams = append(streams, strings.ToLower(strings.TrimSpace(s))+"@aggTrade")
	}
	wsURL := "wss://fstream.asterdex.com/stream?streams=" + strings.Join(streams, "/")
	fmt.Printf("oflow started (window=%ds, large>=$%.0f)\n", windowSec, largeUSD)
	fmt.Println(wsURL)

	go func() {
		tk := time.NewTicker(time.Duration(printEveryMS) * time.Millisecond)
		defer tk.Stop()
		for range tk.C {
			printOflow(windows, windowSec, topN)
		}
	}()

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
			symbol := strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(t.S), "USDT"))
			w, ok := windows[symbol]
			if !ok {
				w = flow.NewWindow(windowDur, largeUSD)
				windows[symbol] = w
			}
			w.Add(flow.Event{
				Ts:    time.UnixMilli(t.T),
				USD:   usd,
				IsBuy: !t.M,
			})
		}

		time.Sleep(1500 * time.Millisecond)
	}
}

func printOflow(windows map[string]*flow.Window, windowSec int, topN int) {
	type row struct {
		Sym        string
		Score      float64
		Delta      float64
		Buy        float64
		Sell       float64
		LargeCount int
	}
	rows := make([]row, 0, len(windows))
	for sym, w := range windows {
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
