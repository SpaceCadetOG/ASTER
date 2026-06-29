package paperreport

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

func BuildOutputs(records []ClosedTradeRecord) Outputs {
	labels := BuildTradeLabels(records)
	return Outputs{
		Summary:            buildSummary(labels),
		BySetupFamily:      groupBy(labels, func(l TradeLabel) (string, string) { return l.SetupFamily, "" }),
		ByStrategyFamily:   groupBy(labels, func(l TradeLabel) (string, string) { return l.StrategyFamily, "" }),
		BySetup:            groupBy(labels, func(l TradeLabel) (string, string) { return l.Setup, "" }),
		BySymbolSideSetup:  groupBy(labels, func(l TradeLabel) (string, string) { return l.Symbol + "|" + l.Side + "|" + l.Setup, "" }),
		ByEntryTiming:      groupBy(labels, func(l TradeLabel) (string, string) { return l.EntryTiming, "" }),
		ByEntryOutcome:     groupBy(labels, func(l TradeLabel) (string, string) { return l.EntryOutcomeLabel, "" }),
		ByEntryScoreBucket: groupBy(labels, func(l TradeLabel) (string, string) { return l.EntryScoreBucket, "" }),
		ByScannerPattern:   groupBy(labels, func(l TradeLabel) (string, string) { return l.ScannerPatternEntry, l.Side }),
		ByStopOutType:      groupBy(labels, func(l TradeLabel) (string, string) { return l.StopOutType, "" }),
		TradeLabels:        labels,
		RuleCandidates:     buildRuleCandidates(labels),
	}
}

func buildSummary(labels []TradeLabel) Summary {
	var s Summary
	if len(labels) == 0 {
		return s
	}
	s.TradeCount = len(labels)
	var winR, lossR []float64
	var allR []float64
	for _, l := range labels {
		allR = append(allR, l.RealizedR)
		s.NetRealized += l.Record.Exit.NetPnL
		s.AvgHoldMin += l.Record.Exit.HoldMinutes
		s.AvgPostExitPeakR += l.PostExitPeakR
		s.AvgEODR += l.EODR
		if l.Record.Exit.HitTP1 {
			s.TP1TouchRate++
		}
		if l.Record.Exit.HitTP2 {
			s.TP2TouchRate++
		}
		if l.Record.Exit.HitTP3 {
			s.TP3TouchRate++
		}
		switch l.StopOutType {
		case "profit_lock":
			s.ProfitLockStopRate++
		case "breakeven":
			s.BreakevenStopRate++
		case "loss":
			s.LossStopRate++
		}
		if l.StoppedThenReclaim {
			s.StoppedThenReclaimRate++
		}
		if l.ReentryWouldWork {
			s.ReentryWouldHaveWorkedRate++
		}
		if l.RealizedR > 0 {
			winR = append(winR, l.RealizedR)
		} else if l.RealizedR < 0 {
			lossR = append(lossR, l.RealizedR)
		}
	}
	s.AvgRealizedR = average(allR)
	s.MedianRealizedR = median(allR)
	s.AvgHoldMin /= float64(len(labels))
	s.AvgPostExitPeakR /= float64(len(labels))
	s.AvgEODR /= float64(len(labels))
	s.TP1TouchRate /= float64(len(labels))
	s.TP2TouchRate /= float64(len(labels))
	s.TP3TouchRate /= float64(len(labels))
	s.ProfitLockStopRate /= float64(len(labels))
	s.BreakevenStopRate /= float64(len(labels))
	s.LossStopRate /= float64(len(labels))
	s.StoppedThenReclaimRate /= float64(len(labels))
	s.ReentryWouldHaveWorkedRate /= float64(len(labels))
	s.WinRate = float64(len(winR)) / float64(len(labels))
	s.AvgWinR = average(winR)
	s.AvgLossR = average(lossR)
	if len(lossR) > 0 {
		s.ExpectancyR = s.WinRate*s.AvgWinR - (1-s.WinRate)*abs(s.AvgLossR)
	}
	if lossSum := abs(sum(lossR)); lossSum > 0 {
		s.ProfitFactor = sum(winR) / lossSum
	}
	return s
}

func groupBy(labels []TradeLabel, keyFn func(TradeLabel) (string, string)) []GroupRow {
	type acc struct {
		label string
		side  string
		rows  []TradeLabel
	}
	buckets := map[string]*acc{}
	for _, l := range labels {
		label, side := keyFn(l)
		if label == "" {
			label = "unknown"
		}
		key := label + "|" + side
		if buckets[key] == nil {
			buckets[key] = &acc{label: label, side: side}
		}
		buckets[key].rows = append(buckets[key].rows, l)
	}
	out := make([]GroupRow, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, buildGroupRow(b.label, b.side, b.rows))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].NetRealized == out[j].NetRealized {
			return out[i].Trades > out[j].Trades
		}
		return out[i].NetRealized > out[j].NetRealized
	})
	return out
}

func buildGroupRow(label, side string, rows []TradeLabel) GroupRow {
	gr := GroupRow{Label: label, Side: side, Trades: len(rows)}
	var rs []float64
	for _, l := range rows {
		rs = append(rs, l.RealizedR)
		gr.NetRealized += l.Record.Exit.NetPnL
		gr.AvgHoldMin += l.Record.Exit.HoldMinutes
		gr.AvgPostExitPeakR += l.PostExitPeakR
		gr.AvgEODR += l.EODR
		if l.RealizedR > 0 {
			gr.Wins++
		} else if l.RealizedR < 0 {
			gr.Losses++
		}
		if l.Record.Exit.HitTP1 {
			gr.TP1TouchRate++
		}
		if l.Record.Exit.HitTP2 {
			gr.TP2TouchRate++
		}
		if l.Record.Exit.HitTP3 {
			gr.TP3TouchRate++
		}
		switch l.StopOutType {
		case "loss":
			gr.LossStopRate++
		case "breakeven":
			gr.BreakevenStopRate++
		case "profit_lock":
			gr.ProfitLockStopRate++
		}
		if l.StoppedThenReclaim {
			gr.StoppedThenReclaimRate++
		}
		if l.ReentryWouldWork {
			gr.ReentryWouldHaveWorkedRate++
		}
		if l.ReversalCandidate {
			gr.OppositeSideCandidateRate++
		}
	}
	if len(rows) > 0 {
		n := float64(len(rows))
		gr.WinRate = float64(gr.Wins) / n
		gr.AvgRealizedR = average(rs)
		gr.MedianRealizedR = median(rs)
		gr.AvgHoldMin /= n
		gr.TP1TouchRate /= n
		gr.TP2TouchRate /= n
		gr.TP3TouchRate /= n
		gr.LossStopRate /= n
		gr.BreakevenStopRate /= n
		gr.ProfitLockStopRate /= n
		gr.AvgPostExitPeakR /= n
		gr.AvgEODR /= n
		gr.StoppedThenReclaimRate /= n
		gr.ReentryWouldHaveWorkedRate /= n
		gr.OppositeSideCandidateRate /= n
	}
	if len(rows) < 10 {
		gr.SampleWarning = "LOW_SAMPLE"
	}
	return gr
}

func buildRuleCandidates(labels []TradeLabel) RuleCandidates {
	var rc RuleCandidates
	for _, row := range groupBy(labels, func(l TradeLabel) (string, string) { return l.SetupFamily, "" }) {
		switch {
		case row.Trades >= 10 && row.NetRealized < 0 && row.LossStopRate >= 0.4:
			rc.AvoidSetups = append(rc.AvoidSetups, row.Label)
		case row.Trades >= 10 && row.NetRealized > 0 && row.WinRate >= 0.55:
			rc.PromoteSetups = append(rc.PromoteSetups, row.Label)
		}
		if row.Trades >= 10 && row.ReentryWouldHaveWorkedRate >= 0.35 && row.AvgPostExitPeakR >= 1.0 {
			rc.ReentryCandidates = append(rc.ReentryCandidates, row.Label)
		}
		if row.Trades >= 10 && row.OppositeSideCandidateRate >= 0.25 {
			rc.ReversalCandidates = append(rc.ReversalCandidates, row.Label)
		}
		if row.Trades >= 10 && (row.BreakevenStopRate >= 0.25 || row.ProfitLockStopRate >= 0.25) {
			rc.ExitAdjustmentCandidates = append(rc.ExitAdjustmentCandidates, row.Label)
		}
	}
	return rc
}

func WriteOutputs(outDir string, outputs Outputs) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, "paper_summary.json"), outputs.Summary); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, "paper_rule_candidates.json"), outputs.RuleCandidates); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_by_setup_family.csv"), groupRowsToCSV(outputs.BySetupFamily)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_by_strategy_family.csv"), groupRowsToCSV(outputs.ByStrategyFamily)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_by_setup.csv"), groupRowsToCSV(outputs.BySetup)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_by_symbol_side_setup.csv"), groupRowsToCSV(outputs.BySymbolSideSetup)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_by_entry_timing.csv"), groupRowsToCSV(outputs.ByEntryTiming)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_by_entry_outcome.csv"), groupRowsToCSV(outputs.ByEntryOutcome)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_by_entry_score_bucket.csv"), groupRowsToCSV(outputs.ByEntryScoreBucket)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_by_scanner_pattern.csv"), groupRowsToCSV(outputs.ByScannerPattern)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_stopout_report.csv"), groupRowsToCSV(outputs.ByStopOutType)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_trade_labels.csv"), tradeLabelsToCSV(outputs.TradeLabels)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_reversal_opportunity_report.csv"), reversalRowsCSV(outputs.TradeLabels)); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_eod_hold_report.csv"), eodRowsCSV(outputs.TradeLabels)); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, "paper_setup_report.json"), outputs); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(outDir, "paper_setup_report.csv"), tradeLabelsToCSV(outputs.TradeLabels)); err != nil {
		return err
	}
	return nil
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeCSV(path string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func groupRowsToCSV(rows []GroupRow) [][]string {
	out := [][]string{{
		"label", "side", "trades", "wins", "losses", "win_rate", "net_realized", "avg_realized_r", "median_realized_r",
		"avg_hold_min", "tp1_touch_rate", "tp2_touch_rate", "tp3_touch_rate", "loss_stop_rate", "breakeven_stop_rate",
		"profit_lock_stop_rate", "avg_post_exit_peak_r", "avg_eod_r", "stopped_then_reclaim_rate",
		"reentry_would_have_worked_rate", "opposite_side_candidate_rate", "sample_warning",
	}}
	for _, row := range rows {
		out = append(out, []string{
			row.Label, row.Side, strconv.Itoa(row.Trades), strconv.Itoa(row.Wins), strconv.Itoa(row.Losses),
			f4(row.WinRate), f4(row.NetRealized), f4(row.AvgRealizedR), f4(row.MedianRealizedR), f4(row.AvgHoldMin),
			f4(row.TP1TouchRate), f4(row.TP2TouchRate), f4(row.TP3TouchRate), f4(row.LossStopRate),
			f4(row.BreakevenStopRate), f4(row.ProfitLockStopRate), f4(row.AvgPostExitPeakR), f4(row.AvgEODR),
			f4(row.StoppedThenReclaimRate), f4(row.ReentryWouldHaveWorkedRate), f4(row.OppositeSideCandidateRate), row.SampleWarning,
		})
	}
	return out
}

func tradeLabelsToCSV(labels []TradeLabel) [][]string {
	out := [][]string{{
		"trade_id", "symbol", "side", "setup_family", "strategy_family", "scanner_pattern_entry", "scanner_pattern_exit", "scanner_pattern_eod",
		"realized_r", "max_r_seen", "min_r_seen", "post_exit_peak_r", "eod_r", "tp_path", "stop_out_type", "exit_quality",
		"shakeout_candidate", "reentry_candidate", "reversal_candidate", "opposite_move_r_15m", "opposite_move_r_60m", "opposite_move_r_eod",
		"stopped_then_reclaim", "reentry_would_have_worked", "rule_candidate",
	}}
	for _, l := range labels {
		out = append(out, []string{
			l.TradeID, l.Symbol, l.Side, l.SetupFamily, l.StrategyFamily, l.ScannerPatternEntry, l.ScannerPatternExit, l.ScannerPatternEOD,
			f4(l.RealizedR), f4(l.MaxRSeen), f4(l.MinRSeen), f4(l.PostExitPeakR), f4(l.EODR), l.TPPath, l.StopOutType, l.ExitQuality,
			strconv.FormatBool(l.ShakeoutCandidate), strconv.FormatBool(l.ReentryCandidate), strconv.FormatBool(l.ReversalCandidate),
			f4(l.OppositeMoveR15m), f4(l.OppositeMoveR60m), f4(l.OppositeMoveREOD),
			strconv.FormatBool(l.StoppedThenReclaim), strconv.FormatBool(l.ReentryWouldWork), l.RuleCandidate,
		})
	}
	return out
}

func reversalRowsCSV(labels []TradeLabel) [][]string {
	out := [][]string{{"trade_id", "symbol", "side", "setup_family", "strategy_family", "entry_pct24h", "entry_pct4h", "entry_pct1h", "exit_pct24h", "exit_pct4h", "exit_pct1h", "eod_pct24h", "eod_pct4h", "eod_pct1h", "raw_exit_reason", "stop_out_type", "realized_r", "post_exit_peak_r", "eod_r", "stopped_then_reclaim", "reentry_would_have_worked", "reversal_candidate", "opposite_move_r_15m", "opposite_move_r_60m", "opposite_move_r_eod", "reason_codes"}}
	for _, l := range labels {
		rec := l.Record
		out = append(out, []string{
			l.TradeID, l.Symbol, l.Side, l.SetupFamily, l.StrategyFamily,
			f4(rec.Plan.Pct24hAtEntry), f4(rec.Plan.Pct4hAtEntry), f4(rec.Plan.Pct1hAtEntry),
			f4(rec.Exit.Pct24hAtExit), f4(rec.Exit.Pct4hAtExit), f4(rec.Exit.Pct1hAtExit),
			f4(rec.PostExit.EODPct24h), f4(rec.PostExit.EODPct4h), f4(rec.PostExit.EODPct1h),
			rec.Exit.RawExitReason, l.StopOutType, f4(l.RealizedR), f4(l.PostExitPeakR), f4(l.EODR),
			strconv.FormatBool(l.StoppedThenReclaim), strconv.FormatBool(l.ReentryWouldWork), strconv.FormatBool(l.ReversalCandidate),
			f4(l.OppositeMoveR15m), f4(l.OppositeMoveR60m), f4(l.OppositeMoveREOD), l.RuleCandidate,
		})
	}
	return out
}

func eodRowsCSV(labels []TradeLabel) [][]string {
	out := [][]string{{"trade_id", "symbol", "side", "setup_family", "realized_r", "post_exit_peak_r", "eod_r", "eod_price_cst_185959", "eod_pct24h", "eod_pct4h", "eod_pct1h", "eod_vs_exit_price_diff", "good_exit", "early_exit", "bad_hold_candidate"}}
	for _, l := range labels {
		rec := l.Record
		goodExit := l.EODR <= l.RealizedR+0.25
		earlyExit := l.EODR >= l.RealizedR+0.75
		badHold := l.EODR < l.RealizedR-0.75
		out = append(out, []string{
			l.TradeID, l.Symbol, l.Side, l.SetupFamily, f4(l.RealizedR), f4(l.PostExitPeakR), f4(l.EODR),
			f4(rec.PostExit.EODPriceCST185959), f4(rec.PostExit.EODPct24h), f4(rec.PostExit.EODPct4h), f4(rec.PostExit.EODPct1h),
			f4(rec.PostExit.EODCapturePriceDiff), strconv.FormatBool(goodExit), strconv.FormatBool(earlyExit), strconv.FormatBool(badHold),
		})
	}
	return out
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return sum(values) / float64(len(values))
}

func sum(values []float64) float64 {
	total := 0.0
	for _, v := range values {
		total += v
	}
	return total
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func f4(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}

func WriteCompatFiles(outDir string, outputs Outputs, csvOut, jsonOut string) error {
	if err := WriteOutputs(outDir, outputs); err != nil {
		return err
	}
	if csvOut != "" {
		if err := writeCSV(csvOut, tradeLabelsToCSV(outputs.TradeLabels)); err != nil {
			return err
		}
	}
	if jsonOut != "" {
		if err := writeJSON(jsonOut, outputs); err != nil {
			return err
		}
	}
	return nil
}

func DefaultReportDir(closedPath string) string {
	base := filepath.Dir(closedPath)
	if base == "" {
		base = "."
	}
	return filepath.Join(base, "reports")
}

func GeneratedAt() string {
	return time.Now().UTC().Format(time.RFC3339)
}
