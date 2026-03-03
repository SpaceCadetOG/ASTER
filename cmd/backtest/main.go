package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-machine/internal/backtest"
)

type scannerSnap struct {
	Ts     int64   `json:"ts"`
	Symbol string  `json:"symbol"`
	Score  float64 `json:"score"`
	Conf   float64 `json:"conf"`
	Grade  string  `json:"grade"`
}

type whaleEvent struct {
	Ts     int64   `json:"ts"`
	Symbol string  `json:"symbol"`
	USD    float64 `json:"usd"`
	Side   string  `json:"side"`
}

func main() {
	symbols := symbolsList()
	cfg := backtest.Config{
		Strategy:    strings.ToLower(envStr("BT_STRATEGY", "router")),
		TF:          envStr("BT_TF", "1m"),
		StartBal:    envFloat("BT_START_BALANCE", 1000),
		FeesBps:     envFloat("BT_FEES_BPS", 2),
		SlipBps:     envFloat("BT_SLIPPAGE_BPS", 1),
		Leverage:    envFloat("BT_LEVERAGE", 3),
		MarginUSD:   envFloat("BT_MARGIN_USD", 10),
		ReserveUSD:  envFloat("BT_RESERVE_USD", 5),
		MaxPos:      envInt("BT_MAX_POS", 1),
		ScoreMin:    envFloat("BT_SCORE_MIN", 70),
		GradeMin:    envStr("BT_GRADE_MIN", "B"),
		WhaleDelta:  envFloat("BT_WHALE_DELTA_MIN", 0),
		EntryMethod: strings.ToLower(envStr("BT_ENTRY", "next_open")),
	}
	outRoot := envStr("BT_OUT_DIR", "out/backtests")
	from := parseDate(envStr("BT_FROM", ""))
	to := parseDate(envStr("BT_TO", ""))

	reports := make([]backtest.Report, 0, len(symbols))

	for _, symbol := range symbols {
		sym := strings.ToUpper(strings.TrimSpace(symbol))
		if sym == "" {
			continue
		}
		cpath := resolvePath(envStr("BT_CANDLES", ""), fmt.Sprintf("data/%s_%s.csv", sym, cfg.TF), sym)
		spath := resolvePath(envStr("BT_SCANNER", ""), "data/scanner.jsonl", sym)
		wpath := resolvePath(envStr("BT_WHALES", ""), fmt.Sprintf("data/%s_whales.jsonl", sym), sym)

		candles, err := loadCandles(cpath)
		if err != nil {
			fmt.Printf("skip %s: candles load error: %v\n", sym, err)
			continue
		}
		candles = filterRange(candles, from, to)
		if len(candles) < 5 {
			fmt.Printf("skip %s: not enough candles after filter\n", sym)
			continue
		}
		scans, err := loadScans(spath, sym)
		if err != nil {
			fmt.Printf("warn %s: scanner load error: %v\n", sym, err)
		}
		whales, err := loadWhales(wpath, sym)
		if err != nil {
			fmt.Printf("warn %s: whales load error: %v\n", sym, err)
		}

		rcfg := cfg
		rcfg.Symbol = sym
		fmt.Printf("backtest symbol=%s strategy=%s tf=%s candles=%d scanner=%d whales=%d\n", sym, rcfg.Strategy, rcfg.TF, len(candles), len(scans), len(whales))
		res, err := backtest.Run(rcfg, candles, scans, whales)
		if err != nil {
			fmt.Printf("skip %s: run error: %v\n", sym, err)
			continue
		}
		outDir := filepath.Join(outRoot, strings.ToLower(sym), strings.ToLower(rcfg.Strategy))
		if err := backtest.WriteOutputs(res, outDir); err != nil {
			fmt.Printf("warn %s: write error: %v\n", sym, err)
		}
		reports = append(reports, res.Report)
		b, _ := json.MarshalIndent(res.Report, "", "  ")
		fmt.Println(string(b))
	}

	if len(reports) == 0 {
		fail("no symbol produced a backtest result")
	}
	summaryPath := filepath.Join(outRoot, "summary.json")
	if err := backtest.WriteBatchSummary(summaryPath, reports); err != nil {
		fail("write summary: %v", err)
	}
	fmt.Printf("batch summary: %s\n", summaryPath)
}

func symbolsList() []string {
	if s := strings.TrimSpace(os.Getenv("BT_SYMBOL")); s != "" {
		return []string{s}
	}
	s := strings.TrimSpace(os.Getenv("BT_SYMBOLS"))
	if s == "" {
		s = "BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,ASTERUSDT,HYPEUSDT"
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, strings.ToUpper(v))
		}
	}
	return out
}

func resolvePath(pattern, def, symbol string) string {
	if strings.TrimSpace(pattern) == "" {
		return def
	}
	p := pattern
	p = strings.ReplaceAll(p, "{symbol}", symbol)
	p = strings.ReplaceAll(p, "{SYMBOL}", symbol)
	return p
}

func loadCandles(path string) ([]backtest.Candle, error) {
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
	out := make([]backtest.Candle, 0, len(rows))
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
		out = append(out, backtest.Candle{Ts: ts, O: o, H: h, L: l, C: c, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts.Before(out[j].Ts) })
	return out, nil
}

func loadScans(path, symbol string) ([]backtest.ScannerPoint, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make([]backtest.ScannerPoint, 0, 1024)
	sc := bufio.NewScanner(f)
	var prevScore float64
	var prevTs time.Time
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s scannerSnap
		if json.Unmarshal([]byte(line), &s) != nil {
			continue
		}
		if symbol != "" && !strings.EqualFold(strings.TrimSpace(s.Symbol), symbol) {
			continue
		}
		ts := time.Unix(s.Ts, 0)
		if s.Ts > 1e12 {
			ts = time.UnixMilli(s.Ts)
		}
		slope := 0.0
		if !prevTs.IsZero() {
			dt := ts.Sub(prevTs).Minutes()
			if dt > 0 {
				slope = (s.Score - prevScore) / dt
			}
		}
		out = append(out, backtest.ScannerPoint{Ts: ts, Score: s.Score, Grade: s.Grade, Slope: slope})
		prevScore, prevTs = s.Score, ts
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts.Before(out[j].Ts) })
	for i := 1; i < len(out); i++ {
		out[i].Accel = out[i].Slope - out[i-1].Slope
	}
	return out, nil
}

func loadWhales(path, symbol string) ([]backtest.WhalePoint, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make([]backtest.WhalePoint, 0, 4096)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var w whaleEvent
		if json.Unmarshal([]byte(line), &w) != nil {
			continue
		}
		if symbol != "" && !strings.EqualFold(strings.TrimSpace(w.Symbol), symbol) {
			continue
		}
		ts := time.Unix(w.Ts, 0)
		if w.Ts > 1e12 {
			ts = time.UnixMilli(w.Ts)
		}
		out = append(out, backtest.WhalePoint{Ts: ts, USD: w.USD, IsBuy: strings.EqualFold(w.Side, "BUY")})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts.Before(out[j].Ts) })
	return out, nil
}

func filterRange(in []backtest.Candle, from, to time.Time) []backtest.Candle {
	if from.IsZero() && to.IsZero() {
		return in
	}
	out := make([]backtest.Candle, 0, len(in))
	for _, c := range in {
		if !from.IsZero() && c.Ts.Before(from) {
			continue
		}
		if !to.IsZero() && c.Ts.After(to) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func parseTS(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	if n > 1e12 {
		return time.UnixMilli(n), nil
	}
	return time.Unix(n, 0), nil
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
func fail(f string, a ...any) {
	fmt.Printf(f+"\n", a...)
	os.Exit(1)
}
