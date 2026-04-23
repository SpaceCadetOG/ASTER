package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type candle struct {
	Ts time.Time
	O  float64
	H  float64
	L  float64
	C  float64
	V  float64
}

type replaySpec struct {
	Label      string
	Symbol     string
	Side       string
	EntryTS    time.Time
	EntryPrice float64
	Qty        float64
	Candles    string
	ToTS       time.Time
}

type phasePoint struct {
	Minutes int     `json:"minutes"`
	TS      string  `json:"ts"`
	Close   float64 `json:"close"`
	PnL     float64 `json:"pnl"`
	Pct     float64 `json:"pct"`
}

type replayResult struct {
	Label            string       `json:"label"`
	Symbol           string       `json:"symbol"`
	Side             string       `json:"side"`
	EntryTS          string       `json:"entry_ts"`
	EndTS            string       `json:"end_ts"`
	EntryPrice       float64      `json:"entry_price"`
	EndClose         float64      `json:"end_close"`
	Qty              float64      `json:"qty"`
	Rows             int          `json:"rows"`
	BreakEvenTouchTS string       `json:"break_even_touch_ts,omitempty"`
	MaxFavorablePx   float64      `json:"max_favorable_px"`
	MaxFavorableTS   string       `json:"max_favorable_ts"`
	MaxAdversePx     float64      `json:"max_adverse_px"`
	MaxAdverseTS     string       `json:"max_adverse_ts"`
	PeakPnL          float64      `json:"peak_pnl"`
	PeakPct          float64      `json:"peak_pct"`
	MaxDrawdownPnL   float64      `json:"max_drawdown_pnl"`
	MaxDrawdownPct   float64      `json:"max_drawdown_pct"`
	EndPnL           float64      `json:"end_pnl"`
	EndPct           float64      `json:"end_pct"`
	GivebackPnL      float64      `json:"giveback_pnl"`
	GivebackPct      float64      `json:"giveback_pct"`
	Phase            []phasePoint `json:"phase"`
}

func main() {
	var (
		symbol         = flag.String("symbol", "", "Symbol (e.g. CHIPUSDT)")
		side           = flag.String("side", "BUY", "Side: BUY or SELL")
		entryTSRaw     = flag.String("entry-ts", "", "Entry time (unix sec/ms, RFC3339, or '2006-01-02 15:04:05')")
		entryPrice     = flag.Float64("entry-price", 0, "Entry price")
		qty            = flag.Float64("qty", 0, "Quantity (if omitted, qty is derived from -notional)")
		notional       = flag.Float64("notional", 0, "Optional notional in quote currency; used only when -qty <= 0")
		candlesPath    = flag.String("candles", "", "Candles CSV path (ts,o,h,l,c,v)")
		tradesCSV      = flag.String("trades-csv", "", "Optional multi-trade input CSV")
		toTSRaw        = flag.String("to-ts", "", "Optional end time")
		durationMin    = flag.Int("duration-min", 0, "Optional replay duration in minutes from entry")
		checkpointsRaw = flag.String("checkpoints", "20,60,180,360,540,720,900,1080,1200,1320", "Checkpoint minutes (comma-separated)")
		tzRaw          = flag.String("tz", "America/Chicago", "Timezone used when parsing naive timestamps")
		outDir         = flag.String("out-dir", "", "Optional output directory (writes replay_summary.json/csv/md)")
	)
	flag.Parse()

	loc, err := time.LoadLocation(strings.TrimSpace(*tzRaw))
	if err != nil {
		fail("invalid -tz: %v", err)
	}
	checkpoints, err := parseCheckpoints(*checkpointsRaw)
	if err != nil {
		fail("invalid -checkpoints: %v", err)
	}

	specs := make([]replaySpec, 0, 8)
	if strings.TrimSpace(*tradesCSV) != "" {
		loaded, lerr := loadTradeSpecsCSV(strings.TrimSpace(*tradesCSV), loc)
		if lerr != nil {
			fail("load -trades-csv: %v", lerr)
		}
		specs = append(specs, loaded...)
	} else {
		if strings.TrimSpace(*symbol) == "" || strings.TrimSpace(*entryTSRaw) == "" || *entryPrice <= 0 || strings.TrimSpace(*candlesPath) == "" {
			fail("single replay requires -symbol -entry-ts -entry-price -candles")
		}
		ets, perr := parseAnyTime(*entryTSRaw, loc)
		if perr != nil {
			fail("invalid -entry-ts: %v", perr)
		}
		end, eerr := resolveEndTS(*toTSRaw, *durationMin, ets, loc)
		if eerr != nil {
			fail("invalid end time: %v", eerr)
		}
		useQty := *qty
		if useQty <= 0 && *notional > 0 {
			useQty = *notional / *entryPrice
		}
		if useQty <= 0 {
			fail("set -qty > 0, or provide -notional > 0")
		}
		specs = append(specs, replaySpec{
			Label:      strings.ToUpper(strings.TrimSpace(*symbol)),
			Symbol:     strings.ToUpper(strings.TrimSpace(*symbol)),
			Side:       normalizeSide(*side),
			EntryTS:    ets,
			EntryPrice: *entryPrice,
			Qty:        useQty,
			Candles:    strings.TrimSpace(*candlesPath),
			ToTS:       end,
		})
	}
	if len(specs) == 0 {
		fail("no replay specs")
	}

	results := make([]replayResult, 0, len(specs))
	for _, s := range specs {
		candles, cerr := loadCandles(s.Candles)
		if cerr != nil {
			fail("load candles %s: %v", s.Candles, cerr)
		}
		r, rerr := runReplay(s, candles, checkpoints)
		if rerr != nil {
			fail("replay %s: %v", s.Symbol, rerr)
		}
		results = append(results, r)
	}

	printConsoleSummary(results)

	if strings.TrimSpace(*outDir) != "" {
		if err := writeOutputs(strings.TrimSpace(*outDir), results); err != nil {
			fail("write outputs: %v", err)
		}
		fmt.Printf("wrote: %s\n", strings.TrimSpace(*outDir))
	}
}

func runReplay(spec replaySpec, candles []candle, checkpoints []int) (replayResult, error) {
	start := spec.EntryTS.UTC()
	end := spec.ToTS.UTC()
	rows := make([]candle, 0, len(candles))
	for _, c := range candles {
		if c.Ts.Before(start) {
			continue
		}
		if !end.IsZero() && c.Ts.After(end) {
			continue
		}
		rows = append(rows, c)
	}
	if len(rows) == 0 {
		return replayResult{}, fmt.Errorf("no candles in range")
	}

	res := replayResult{
		Label:      firstNonEmpty(strings.TrimSpace(spec.Label), strings.ToUpper(strings.TrimSpace(spec.Symbol))),
		Symbol:     strings.ToUpper(strings.TrimSpace(spec.Symbol)),
		Side:       normalizeSide(spec.Side),
		EntryTS:    spec.EntryTS.Format(time.RFC3339),
		EndTS:      rows[len(rows)-1].Ts.Format(time.RFC3339),
		EntryPrice: spec.EntryPrice,
		EndClose:   rows[len(rows)-1].C,
		Qty:        spec.Qty,
		Rows:       len(rows),
	}

	maxFavPx := 0.0
	minAdversePx := 0.0
	var maxFavTS time.Time
	var minAdverseTS time.Time
	var breakEvenTS time.Time
	phase := make([]phasePoint, 0, len(checkpoints))
	cpHit := make(map[int]bool, len(checkpoints))

	for _, c := range rows {
		if breakEvenTS.IsZero() && barTouchesBreakEven(res.Side, c, res.EntryPrice) {
			breakEvenTS = c.Ts
		}
		favPx, advPx := favorableAndAdversePrices(res.Side, c)
		if maxFavPx == 0 || betterFavorable(res.Side, favPx, maxFavPx) {
			maxFavPx = favPx
			maxFavTS = c.Ts
		}
		if minAdversePx == 0 || worseAdverse(res.Side, advPx, minAdversePx) {
			minAdversePx = advPx
			minAdverseTS = c.Ts
		}
		for _, m := range checkpoints {
			if cpHit[m] {
				continue
			}
			target := start.Add(time.Duration(m) * time.Minute)
			if c.Ts.Before(target) {
				continue
			}
			pnl, pct := pnlAndPct(res.Side, res.EntryPrice, c.C, res.Qty)
			phase = append(phase, phasePoint{
				Minutes: m,
				TS:      c.Ts.Format(time.RFC3339),
				Close:   c.C,
				PnL:     pnl,
				Pct:     pct,
			})
			cpHit[m] = true
		}
	}
	sort.Slice(phase, func(i, j int) bool { return phase[i].Minutes < phase[j].Minutes })
	res.Phase = phase

	peakPnL, peakPct := pnlAndPct(res.Side, res.EntryPrice, maxFavPx, res.Qty)
	ddPnL, ddPct := pnlAndPct(res.Side, res.EntryPrice, minAdversePx, res.Qty)
	endPnL, endPct := pnlAndPct(res.Side, res.EntryPrice, res.EndClose, res.Qty)
	res.MaxFavorablePx = maxFavPx
	res.MaxFavorableTS = maxFavTS.Format(time.RFC3339)
	res.MaxAdversePx = minAdversePx
	res.MaxAdverseTS = minAdverseTS.Format(time.RFC3339)
	res.PeakPnL = peakPnL
	res.PeakPct = peakPct
	res.MaxDrawdownPnL = ddPnL
	res.MaxDrawdownPct = ddPct
	res.EndPnL = endPnL
	res.EndPct = endPct
	res.GivebackPnL = peakPnL - endPnL
	res.GivebackPct = peakPct - endPct
	if !breakEvenTS.IsZero() {
		res.BreakEvenTouchTS = breakEvenTS.Format(time.RFC3339)
	}
	return res, nil
}

func loadCandles(path string) ([]candle, error) {
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
	out := make([]candle, 0, len(rows))
	for i, row := range rows {
		if len(row) < 5 {
			continue
		}
		if i == 0 && !isNumeric(strings.TrimSpace(unquote(row[0]))) {
			continue
		}
		ts, err := parseTS(strings.TrimSpace(unquote(row[0])))
		if err != nil {
			continue
		}
		o, _ := strconv.ParseFloat(strings.TrimSpace(unquote(row[1])), 64)
		h, _ := strconv.ParseFloat(strings.TrimSpace(unquote(row[2])), 64)
		l, _ := strconv.ParseFloat(strings.TrimSpace(unquote(row[3])), 64)
		c, _ := strconv.ParseFloat(strings.TrimSpace(unquote(row[4])), 64)
		v := 0.0
		if len(row) > 5 {
			v, _ = strconv.ParseFloat(strings.TrimSpace(unquote(row[5])), 64)
		}
		if c <= 0 {
			continue
		}
		out = append(out, candle{Ts: ts.UTC(), O: o, H: h, L: l, C: c, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts.Before(out[j].Ts) })
	return out, nil
}

func loadTradeSpecsCSV(path string, loc *time.Location) ([]replaySpec, error) {
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
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty csv")
	}
	header := map[string]int{}
	for i, h := range rows[0] {
		header[strings.ToLower(strings.TrimSpace(h))] = i
	}
	required := []string{"symbol", "side", "entry_ts", "entry_price", "candles"}
	for _, k := range required {
		if _, ok := header[k]; !ok {
			return nil, fmt.Errorf("missing column %q", k)
		}
	}

	out := make([]replaySpec, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if len(row) == 0 {
			continue
		}
		get := func(k string) string {
			i, ok := header[k]
			if !ok || i >= len(row) {
				return ""
			}
			return strings.TrimSpace(unquote(row[i]))
		}
		symbol := strings.ToUpper(strings.TrimSpace(get("symbol")))
		if symbol == "" {
			continue
		}
		ets, err := parseAnyTime(get("entry_ts"), loc)
		if err != nil {
			return nil, fmt.Errorf("%s entry_ts: %w", symbol, err)
		}
		entry, err := strconv.ParseFloat(get("entry_price"), 64)
		if err != nil || entry <= 0 {
			return nil, fmt.Errorf("%s invalid entry_price", symbol)
		}
		qty := 0.0
		if q := get("qty"); q != "" {
			qty, _ = strconv.ParseFloat(q, 64)
		}
		if qty <= 0 {
			if n := get("notional"); n != "" {
				notional, _ := strconv.ParseFloat(n, 64)
				if notional > 0 {
					qty = notional / entry
				}
			}
		}
		if qty <= 0 {
			qty = 1
		}
		end := time.Time{}
		if t := get("to_ts"); t != "" {
			to, err := parseAnyTime(t, loc)
			if err != nil {
				return nil, fmt.Errorf("%s to_ts: %w", symbol, err)
			}
			end = to
		} else if dm := get("duration_min"); dm != "" {
			mins, _ := strconv.Atoi(dm)
			if mins > 0 {
				end = ets.Add(time.Duration(mins) * time.Minute)
			}
		}
		out = append(out, replaySpec{
			Label:      get("label"),
			Symbol:     symbol,
			Side:       normalizeSide(get("side")),
			EntryTS:    ets,
			EntryPrice: entry,
			Qty:        qty,
			Candles:    get("candles"),
			ToTS:       end,
		})
	}
	return out, nil
}

func parseCheckpoints(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty")
	}
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	seen := map[int]struct{}{}
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("bad checkpoint %q", p)
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

func resolveEndTS(toRaw string, durationMin int, entry time.Time, loc *time.Location) (time.Time, error) {
	toRaw = strings.TrimSpace(toRaw)
	if toRaw != "" {
		return parseAnyTime(toRaw, loc)
	}
	if durationMin > 0 {
		return entry.Add(time.Duration(durationMin) * time.Minute), nil
	}
	return time.Time{}, nil
}

func parseAnyTime(raw string, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if n > 1e12 {
			return time.UnixMilli(n).UTC(), nil
		}
		return time.Unix(n, 0).UTC(), nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	}
	for _, layout := range layouts {
		if layout == time.RFC3339 {
			if t, err := time.Parse(layout, raw); err == nil {
				return t.UTC(), nil
			}
			continue
		}
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %q", raw)
}

func parseTS(s string) (time.Time, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	if n > 1e12 {
		return time.UnixMilli(n), nil
	}
	return time.Unix(n, 0), nil
}

func normalizeSide(side string) string {
	s := strings.ToUpper(strings.TrimSpace(side))
	switch s {
	case "BUY", "LONG":
		return "BUY"
	case "SELL", "SHORT":
		return "SELL"
	default:
		return "BUY"
	}
}

func favorableAndAdversePrices(side string, c candle) (favorable float64, adverse float64) {
	if normalizeSide(side) == "BUY" {
		return c.H, c.L
	}
	return c.L, c.H
}

func betterFavorable(side string, cur, prev float64) bool {
	if normalizeSide(side) == "BUY" {
		return cur > prev
	}
	return cur < prev
}

func worseAdverse(side string, cur, prev float64) bool {
	if normalizeSide(side) == "BUY" {
		return cur < prev
	}
	return cur > prev
}

func barTouchesBreakEven(side string, c candle, entry float64) bool {
	if normalizeSide(side) == "BUY" {
		return c.H >= entry
	}
	return c.L <= entry
}

func pnlAndPct(side string, entry, px, qty float64) (float64, float64) {
	if entry <= 0 || qty <= 0 || px <= 0 {
		return 0, 0
	}
	if normalizeSide(side) == "BUY" {
		pnl := (px - entry) * qty
		return pnl, ((px / entry) - 1.0) * 100.0
	}
	pnl := (entry - px) * qty
	return pnl, ((entry / px) - 1.0) * 100.0
}

func printConsoleSummary(results []replayResult) {
	fmt.Println("Manual Replay Summary")
	fmt.Println("label,symbol,side,entry_ts,end_ts,peak_pnl,end_pnl,giveback_pnl,peak_pct,end_pct,rows")
	for _, r := range results {
		fmt.Printf("%s,%s,%s,%s,%s,%.4f,%.4f,%.4f,%.2f,%.2f,%d\n",
			r.Label, r.Symbol, r.Side, r.EntryTS, r.EndTS, r.PeakPnL, r.EndPnL, r.GivebackPnL, r.PeakPct, r.EndPct, r.Rows)
	}
}

func writeOutputs(outDir string, results []replayResult) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	jpath := filepath.Join(outDir, "replay_summary.json")
	b, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(jpath, b, 0o644); err != nil {
		return err
	}

	cpath := filepath.Join(outDir, "replay_summary.csv")
	cf, err := os.Create(cpath)
	if err != nil {
		return err
	}
	defer cf.Close()
	w := csv.NewWriter(cf)
	_ = w.Write([]string{"label", "symbol", "side", "entry_ts", "end_ts", "entry_price", "end_close", "qty", "rows", "break_even_touch_ts", "max_favorable_px", "max_favorable_ts", "max_adverse_px", "max_adverse_ts", "peak_pnl", "peak_pct", "max_drawdown_pnl", "max_drawdown_pct", "end_pnl", "end_pct", "giveback_pnl", "giveback_pct"})
	for _, r := range results {
		_ = w.Write([]string{
			r.Label, r.Symbol, r.Side, r.EntryTS, r.EndTS,
			f64(r.EntryPrice), f64(r.EndClose), f64(r.Qty), strconv.Itoa(r.Rows), r.BreakEvenTouchTS,
			f64(r.MaxFavorablePx), r.MaxFavorableTS, f64(r.MaxAdversePx), r.MaxAdverseTS,
			f64(r.PeakPnL), f64(r.PeakPct), f64(r.MaxDrawdownPnL), f64(r.MaxDrawdownPct),
			f64(r.EndPnL), f64(r.EndPct), f64(r.GivebackPnL), f64(r.GivebackPct),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}

	md := filepath.Join(outDir, "replay_summary.md")
	var sb strings.Builder
	sb.WriteString("# Manual Replay Summary\n\n")
	sb.WriteString("| Label | Symbol | Side | Entry (UTC) | End (UTC) | Peak PnL | End PnL | Giveback | Peak % | End % |\n")
	sb.WriteString("|---|---|---|---|---|---:|---:|---:|---:|---:|\n")
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %.4f | %.4f | %.4f | %.2f | %.2f |\n",
			r.Label, r.Symbol, r.Side, r.EntryTS, r.EndTS, r.PeakPnL, r.EndPnL, r.GivebackPnL, r.PeakPct, r.EndPct))
	}
	if err := os.WriteFile(md, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	return nil
}

func f64(v float64) string {
	return strconv.FormatFloat(v, 'f', 8, 64)
}

func unquote(s string) string {
	return strings.Trim(s, "\"")
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

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
