package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-machine/internal/flow"
)

type Candle struct {
	Ts     int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type ScannerSnap struct {
	Ts     int64   `json:"ts"`
	Symbol string  `json:"symbol"`
	Score  float64 `json:"score"`
	Conf   float64 `json:"conf"`
	Grade  string  `json:"grade"`
}

type WhaleEvent struct {
	Ts     int64   `json:"ts"`
	Symbol string  `json:"symbol"`
	USD    float64 `json:"usd"`
	Side   string  `json:"side"`
}

type Trade struct {
	EntryTs    int64
	ExitTs     int64
	EntryPrice float64
	ExitPrice  float64
	Qty        float64
	PnLUSD     float64
	PnLPct     float64
	Reason     string
}

type Config struct {
	CandlesPath string
	ScannerPath string
	WhalesPath  string
	Symbol      string

	StartBalance   float64
	ScoreMin       float64
	ConfMin        float64
	WhaleDeltaMin  float64
	BuyPctMin      float64
	WhaleWindowSec int

	RiskPct float64
	TPR     float64
	SLR     float64
}

func main() {
	cfg := Config{
		CandlesPath: envStr("BT_CANDLES", "data/POWERUSDT_1m.csv"),
		ScannerPath: envStr("BT_SCANNER", ""),
		WhalesPath:  envStr("BT_WHALES", ""),
		Symbol:      strings.ToUpper(envStr("BT_SYMBOL", "POWERUSDT")),

		StartBalance:   envFloat("BT_START_BALANCE", 10000),
		ScoreMin:       envFloat("BT_SCORE_MIN", 75),
		ConfMin:        envFloat("BT_CONF_MIN", 0.65),
		WhaleDeltaMin:  envFloat("BT_WHALE_DELTA_MIN", 50000),
		BuyPctMin:      envFloat("BT_BUY_PCT_MIN", 55),
		WhaleWindowSec: envInt("BT_WHALE_WINDOW_SEC", 30),

		RiskPct: envFloat("BT_RISK_PCT", 0.01),
		TPR:     envFloat("BT_TP_R", 2.0),
		SLR:     envFloat("BT_SL_R", 1.0),
	}
	if cfg.RiskPct <= 0 {
		cfg.RiskPct = 0.01
	}
	if cfg.TPR <= 0 {
		cfg.TPR = 2.0
	}
	if cfg.SLR <= 0 {
		cfg.SLR = 1.0
	}

	candles, err := loadCandles(cfg.CandlesPath)
	if err != nil {
		fail("load candles: %v", err)
	}
	if len(candles) < 2 {
		fail("not enough candles in %s", cfg.CandlesPath)
	}
	scans, err := loadScanner(cfg.ScannerPath, cfg.Symbol)
	if err != nil {
		fail("load scanner: %v", err)
	}
	whales, err := loadWhales(cfg.WhalesPath, cfg.Symbol)
	if err != nil {
		fail("load whales: %v", err)
	}

	fmt.Printf("backtest symbol=%s candles=%d scanner=%d whales=%d\n", cfg.Symbol, len(candles), len(scans), len(whales))

	var (
		balance = cfg.StartBalance
		posQty  float64
		entryPx float64
		stopPx  float64
		tpPx    float64
		entryTs int64
		trades  []Trade

		whaleIdx int
		scanIdx  int
		lastScan ScannerSnap
	)

	equitySeries := make([]float64, 0, len(candles))
	whaleWin := flow.NewWindow(time.Duration(cfg.WhaleWindowSec)*time.Second, 0)

	for i := 1; i < len(candles); i++ {
		c := candles[i]
		prev := candles[i-1]

		// Apply scanner snapshots up to candle timestamp.
		for scanIdx < len(scans) && scans[scanIdx].Ts <= c.Ts {
			lastScan = scans[scanIdx]
			scanIdx++
		}

		// Apply whale events up to candle timestamp.
		for whaleIdx < len(whales) && whales[whaleIdx].Ts <= c.Ts {
			w := whales[whaleIdx]
			whaleWin.Add(flow.Event{
				Ts:    time.Unix(w.Ts, 0),
				USD:   w.USD,
				IsBuy: strings.EqualFold(strings.TrimSpace(w.Side), "BUY"),
			})
			whaleIdx++
		}
		ws := whaleWin.SnapshotAt(time.Unix(c.Ts, 0))

		// Entry condition (LONG only V1)
		if posQty == 0 {
			scoreOK := len(scans) == 0 || lastScan.Score >= cfg.ScoreMin
			confOK := len(scans) == 0 || lastScan.Conf >= cfg.ConfMin
			whaleDeltaOK := len(whales) == 0 || ws.DeltaUSD >= cfg.WhaleDeltaMin
			whaleBuyOK := len(whales) == 0 || ws.BuyPct >= cfg.BuyPctMin

			if scoreOK && confOK && whaleDeltaOK && whaleBuyOK {
				entryPx = c.Close
				if entryPx <= 0 {
					continue
				}
				riskDist := entryPx * cfg.RiskPct
				stopPx = entryPx - cfg.SLR*riskDist
				tpPx = entryPx + cfg.TPR*riskDist
				if stopPx <= 0 {
					continue
				}
				posQty = balance / entryPx
				entryTs = c.Ts
			}
		} else {
			// Exit checks using candle range; conservative if both touched.
			exitNow := false
			exitReason := ""
			exitPx := c.Close

			slHit := c.Low <= stopPx
			tpHit := c.High >= tpPx
			switch {
			case slHit && tpHit:
				exitNow = true
				exitReason = "SL+TP same candle (conservative SL)"
				exitPx = stopPx
			case slHit:
				exitNow = true
				exitReason = "SL"
				exitPx = stopPx
			case tpHit:
				exitNow = true
				exitReason = "TP"
				exitPx = tpPx
			case len(whales) > 0 && ws.DeltaUSD < 0:
				exitNow = true
				exitReason = "whale delta flip"
				exitPx = c.Close
			}

			if exitNow {
				newBal := posQty * exitPx
				pnl := newBal - balance
				ret := (exitPx - entryPx) / entryPx
				trades = append(trades, Trade{
					EntryTs:    entryTs,
					ExitTs:     c.Ts,
					EntryPrice: entryPx,
					ExitPrice:  exitPx,
					Qty:        posQty,
					PnLUSD:     pnl,
					PnLPct:     ret,
					Reason:     exitReason,
				})
				balance = newBal
				posQty = 0
			}
		}

		equity := balance
		if posQty > 0 && prev.Close > 0 {
			equity = posQty * c.Close
		}
		equitySeries = append(equitySeries, equity)
	}

	// Force close open position at last close.
	if posQty > 0 {
		last := candles[len(candles)-1]
		newBal := posQty * last.Close
		pnl := newBal - balance
		ret := (last.Close - entryPx) / entryPx
		trades = append(trades, Trade{
			EntryTs:    entryTs,
			ExitTs:     last.Ts,
			EntryPrice: entryPx,
			ExitPrice:  last.Close,
			Qty:        posQty,
			PnLUSD:     pnl,
			PnLPct:     ret,
			Reason:     "EOD",
		})
		balance = newBal
		equitySeries = append(equitySeries, balance)
	}

	printReport(cfg, trades, equitySeries, balance)
}

func loadCandles(path string) ([]Candle, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	out := make([]Candle, 0, len(rows))
	for i, row := range rows {
		if len(row) < 5 {
			continue
		}
		if i == 0 && !isNumeric(strings.TrimSpace(row[0])) {
			continue
		}
		ts, err := parseTS(row[0])
		if err != nil {
			continue
		}
		o, _ := strconv.ParseFloat(strings.TrimSpace(row[1]), 64)
		h, _ := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
		l, _ := strconv.ParseFloat(strings.TrimSpace(row[3]), 64)
		c, _ := strconv.ParseFloat(strings.TrimSpace(row[4]), 64)
		v := 0.0
		if len(row) > 5 {
			v, _ = strconv.ParseFloat(strings.TrimSpace(row[5]), 64)
		}
		if c <= 0 {
			continue
		}
		out = append(out, Candle{Ts: ts, Open: o, High: h, Low: l, Close: c, Volume: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts < out[j].Ts })
	return out, nil
}

func loadScanner(path string, symbol string) ([]ScannerSnap, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make([]ScannerSnap, 0, 1024)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s ScannerSnap
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		if s.Ts <= 0 {
			continue
		}
		if symbol != "" && !strings.EqualFold(strings.TrimSpace(s.Symbol), symbol) {
			continue
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts < out[j].Ts })
	return out, nil
}

func loadWhales(path string, symbol string) ([]WhaleEvent, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make([]WhaleEvent, 0, 4096)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var w WhaleEvent
		if err := json.Unmarshal([]byte(line), &w); err != nil {
			continue
		}
		if w.Ts <= 0 || w.USD <= 0 {
			continue
		}
		if symbol != "" && !strings.EqualFold(strings.TrimSpace(w.Symbol), symbol) {
			continue
		}
		out = append(out, w)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts < out[j].Ts })
	return out, nil
}

func printReport(cfg Config, trades []Trade, equity []float64, finalBalance float64) {
	totalTrades := len(trades)
	wins := 0
	grossProfit := 0.0
	grossLoss := 0.0
	sumRet := 0.0
	rets := make([]float64, 0, totalTrades)
	for _, t := range trades {
		if t.PnLUSD >= 0 {
			wins++
			grossProfit += t.PnLUSD
		} else {
			grossLoss += -t.PnLUSD
		}
		sumRet += t.PnLPct
		rets = append(rets, t.PnLPct)
	}
	winRate := 0.0
	if totalTrades > 0 {
		winRate = 100 * float64(wins) / float64(totalTrades)
	}
	profitFactor := 0.0
	if grossLoss > 0 {
		profitFactor = grossProfit / grossLoss
	}
	expectancy := 0.0
	if totalTrades > 0 {
		expectancy = sumRet / float64(totalTrades)
	}
	maxDD := maxDrawdownPct(equity)
	sharpe := sharpeRatio(rets)

	fmt.Println("------ backtest report ------")
	fmt.Printf("Start Balance: $%.2f\n", cfg.StartBalance)
	fmt.Printf("Final Balance: $%.2f\n", finalBalance)
	fmt.Printf("Total Trades: %d\n", totalTrades)
	fmt.Printf("Win Rate: %.2f%%\n", winRate)
	fmt.Printf("Profit Factor: %.3f\n", profitFactor)
	fmt.Printf("Max Drawdown: %.2f%%\n", maxDD)
	fmt.Printf("Expectancy (avg trade return): %.4f%%\n", 100*expectancy)
	fmt.Printf("Sharpe (trade returns): %.3f\n", sharpe)
	if totalTrades > 0 {
		last := trades[len(trades)-1]
		fmt.Printf("Last Trade: entry=%s exit=%s reason=%s pnl=$%.2f\n",
			time.Unix(last.EntryTs, 0).Format(time.RFC3339),
			time.Unix(last.ExitTs, 0).Format(time.RFC3339),
			last.Reason, last.PnLUSD)
	}
}

func maxDrawdownPct(equity []float64) float64 {
	if len(equity) == 0 {
		return 0
	}
	peak := equity[0]
	maxDD := 0.0
	for _, e := range equity {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			dd := 100 * (peak - e) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

func sharpeRatio(rets []float64) float64 {
	n := len(rets)
	if n < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range rets {
		mean += r
	}
	mean /= float64(n)
	variance := 0.0
	for _, r := range rets {
		d := r - mean
		variance += d * d
	}
	variance /= float64(n - 1)
	std := math.Sqrt(variance)
	if std < 1e-9 {
		return 0
	}
	return mean / std * math.Sqrt(float64(n))
}

func parseTS(s string) (int64, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	// tolerate ms timestamp in csv
	if n > 1e12 {
		n = n / 1000
	}
	return n, nil
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func envStr(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func envFloat(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func fail(format string, a ...any) {
	fmt.Printf(format+"\n", a...)
	os.Exit(1)
}
