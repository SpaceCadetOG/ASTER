// go-machine/cmd/short/main.go
package main

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/api"
	"go-machine/internal/market"
	"go-machine/internal/sessions"
	"go-machine/internal/status"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

func calcVWAP(bars []types.Candle) float64 {
	var pv, v float64
	for _, b := range bars {
		tp := (b.H + b.L + b.C) / 3.0
		pv += tp * b.V
		v += b.V
	}
	if v == 0 {
		return 0
	}
	return pv / v
}

func symbolCandidates(ui string) []string {
	base := strings.ReplaceAll(ui, "-", "")
	cands := []string{}
	if strings.HasSuffix(base, "USD") {
		cands = append(cands, base+"T")
		cands = append(cands, base)
	}
	cands = append(cands, ui)
	return cands
}

func confluenceLabel(c *aster.Client, symbol, side string) string {
	const (
		tf     = types.TF5m // was TF15m
		n      = 200
		win    = 20
		zmin   = 2.0
		vmin   = 1_000_000.0 // was 5_000_000
		levels = 50
	)

	var useSym string
	var bars []types.Candle
	for _, cand := range symbolCandidates(symbol) {
		b, err := c.LoadCandles(cand, tf, n)
		if err == nil && len(b) > 0 {
			useSym, bars = cand, b
			break
		}
	}
	if len(bars) == 0 {
		return "C"
	}

	vwap := calcVWAP(bars)
	tr := ta.TrendMetrics(useSym, tf, bars, vwap)
	ef := ta.ComputeEffort(useSym, tf, bars, win, zmin, vmin)

	obRaw, err := c.FetchOrderBook(useSym, levels)
	if err != nil {
		return "C"
	}
	ob := ta.OrderBookContext(useSym, obRaw.Bids, obRaw.Asks, levels)

	conf := ta.ComputeConfluence(tr, ef, ob, side)
	return conf.Label
}

// --- scanner loop ---

func runOnce(st *status.Store, asterClient *aster.Client) {
	now := time.Now().UTC()
	fmt.Printf("🔧 ASTER SHORT adapter — live fetch @ %s\n", now.Format(time.RFC3339))

	active := sessions.ActiveSessionLabels(now)
	mkts := asterClient.FetchAllMarkets()

	scored := market.ScoreAndFilterShort(mkts)
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })

	fmt.Println(market.FormatHeader("asterdex (SHORTS)", active))
	fmt.Println("Symbol       | Score  | Δ%    | Vol($)  | OI($)   | Funding(%) |  Prev24h |     Last")
	fmt.Println("-------------+--------+-------+---------+---------+------------+---------+---------")

	confMap := make(map[string]string, len(scored))
	eligible := make([]market.Scored, 0, len(scored))

	for _, s := range scored {
		if s.Eligible {
			eligible = append(eligible, s)
			lbl := confluenceLabel(asterClient, s.Symbol, "short")
			if lbl == "" || lbl == "_" || lbl == "C" {
				lbl = market.FallbackGradeDirectional(s.Score, s.Change24h, "short")
			}
			confMap[s.Symbol] = lbl

			// >>> colorized grade in terminal
			col := market.GradeColor(lbl)
			reset := market.ResetColor()
			fmt.Printf("%s | %s%s%s\n", market.FormatRow(s), col, lbl, reset)
			// <<<
		}
	}

	st.SetSnap(status.Snapshot{
		Generated: now,
		Exchange:  "asterdex (SHORTS)",
		Active:    active,
		Rows:      eligible,
		Conf:      confMap,
	})
}

func main() {
	st := status.NewStore()
	asterClient := aster.New("")

	http.HandleFunc("/api/candles", api.CandlesHandler(asterClient))
	http.HandleFunc("/api/pivots", api.PivotsHandler(asterClient))
	http.HandleFunc("/api/structure", api.StructureHandler(asterClient))
	http.HandleFunc("/api/patterns", api.PatternsHandler(asterClient))
	http.HandleFunc("/api/volstats", api.VolStatsHandler(asterClient))
	http.HandleFunc("/api/confluence", api.ConfluenceHandler(asterClient))
	http.HandleFunc("/api/fusion", api.FusionHandler(asterClient))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		symbol := q.Get("symbol")
		if symbol == "" {
			symbol = "BTCUSDT"
		}
		tf := q.Get("tf")
		if tf == "" {
			tf = "5m"
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `
<html>
<head><title>TraderBot SHORT Scanner</title></head>
<body style="font-family:monospace;background:#111;color:#eee;">
	<div style="margin-bottom:10px;">
		<a href="http://34.174.250.99:8080/?symbol=%s&tf=%s" style="color:#7fffd4;">🟢 Long Scanner</a> |
		&nbsp;&nbsp;|&nbsp;&nbsp;
		<a href="/api/candles?symbol=%s&tf=%s&n=200" style="color:#9cf">candles (json)</a>
		&nbsp;|&nbsp;
		<a href="/api/pivots?symbol=%s&tf=%s&n=200&left=3&right=3" style="color:#9cf">pivots (json)</a>
		&nbsp;|&nbsp;
		<a href="/api/structure?symbol=%s&tf=%s&n=200&left=3&right=3" style="color:#9cf">structure (json)</a>
		&nbsp;|&nbsp;
		<a href="/api/volstats?symbol=%s&tf=%s&n=200&win=20&zmin=2.0&vmin=500000" style="color:#9cf">volstats (json)</a>
		&nbsp;|&nbsp;
		<a href="/api/confluence?symbol=%s&tf=%s&n=200&win=20&zmin=2.0&vmin=500000&levels=50" style="color:#9cf">confluence (json)</a>
		&nbsp;|&nbsp;
		<a href="/api/fusion?symbol=%s&tf=%s&n=200&left=3&right=3" style="color:#9cf">fusion (json)</a>
	</div>

<!-- 🔮 Grade Legend -->
<div style="margin-bottom:10px;">
	<span style="background:#a64eff;color:#fff;padding:2px 6px;border-radius:6px;">A+</span>
	<span style="background:#4fc3ff;color:#fff;padding:2px 6px;border-radius:6px;">A</span>
	<span style="background:#00cc66;color:#fff;padding:2px 6px;border-radius:6px;">B</span>
	<span style="background:#ffb347;color:#000;padding:2px 6px;border-radius:6px;">C</span>
	<span style="background:#ff3333;color:#fff;padding:2px 6px;border-radius:6px;">D</span>
</div>

	<iframe src="/status" width="100%%" height="90%%" frameborder="0" style="border:none;"></iframe>
</body>
</html>`,
			symbol, tf, symbol, tf,
			symbol, tf,
			symbol, tf,
			symbol, tf,
			symbol, tf,
			symbol, tf,
			symbol, tf,
		)
	})
	http.Handle("/status", st.Handler())
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	go func() {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8081"
		}
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			fmt.Println("http server error:", err)
		}
	}()

	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		runOnce(st, asterClient)
		<-tick.C
	}
}
