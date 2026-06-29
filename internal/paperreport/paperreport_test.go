package paperreport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildTradeLabelsClassifiesLongAndShortPatterns(t *testing.T) {
	records := []ClosedTradeRecord{
		{
			TradeID: "long-1",
			Symbol:  "LABUSDT",
			Side:    "BUY",
			Identity: ClosedTradeIdentity{
				SetupFamily:    "breakout_retest",
				StrategyFamily: "cont",
			},
			Entry: ClosedTradeEntry{EntryPrice: 100, Qty: 1},
			Plan:  ClosedTradePlan{OriginalStop: 99, Pct24hAtEntry: 5, Pct4hAtEntry: 2, Pct1hAtEntry: -1},
			Exit:  ClosedTradeExit{NetPnL: -1, Pct24hAtExit: 3, Pct4hAtExit: -2, Pct1hAtExit: -3, StopOutType: "loss"},
			PostExit: ClosedTradePostExit{
				PostExitPeakR:      1.4,
				EODCaptureR:        1.2,
				StoppedThenReclaim: true,
				ReentryWouldWork:   true,
				WorstR15m:          -0.3,
				WorstR60m:          -0.4,
				EODPct24h:          4,
				EODPct4h:           1,
				EODPct1h:           -1,
			},
		},
		{
			TradeID: "short-1",
			Symbol:  "GUAUSDT",
			Side:    "SELL",
			Identity: ClosedTradeIdentity{
				SetupFamily:    "breakout_retest",
				StrategyFamily: "cont",
			},
			Entry: ClosedTradeEntry{EntryPrice: 10, Qty: 2},
			Plan:  ClosedTradePlan{OriginalStop: 10.5, Pct24hAtEntry: -8, Pct4hAtEntry: -3, Pct1hAtEntry: -1},
			Exit:  ClosedTradeExit{NetPnL: -0.5, Pct24hAtExit: -4, Pct4hAtExit: 2, Pct1hAtExit: 3, StopOutType: "loss"},
			PostExit: ClosedTradePostExit{
				PostExitPeakR: 0.2,
				EODCaptureR:   -0.8,
				WorstR15m:     -1.2,
				WorstR60m:     -1.5,
				EODPct24h:     2,
				EODPct4h:      4,
				EODPct1h:      2,
			},
		},
	}

	labels := BuildTradeLabels(records)
	if got := labels[0].ScannerPatternEntry; got != "trend_with_1h_pullback" {
		t.Fatalf("unexpected long scanner pattern: %s", got)
	}
	if !labels[0].ShakeoutCandidate || !labels[0].ReentryCandidate {
		t.Fatalf("expected long shakeout/reentry candidate, got %+v", labels[0])
	}
	if got := labels[1].ScannerPatternEntry; got != "full_alignment" {
		t.Fatalf("unexpected short scanner pattern: %s", got)
	}
	if !labels[1].ReversalCandidate {
		t.Fatalf("expected short reversal candidate, got %+v", labels[1])
	}
}

func TestBuildOutputsAndWriteReports(t *testing.T) {
	records := []ClosedTradeRecord{
		{
			TradeID: "t1",
			Symbol:  "LABUSDT",
			Side:    "BUY",
			Identity: ClosedTradeIdentity{
				SetupFamily:    "breakout_retest",
				StrategyFamily: "cont",
				RawStrategy:    "vp_trend",
			},
			Entry: ClosedTradeEntry{EntryPrice: 100, Qty: 1},
			Plan:  ClosedTradePlan{OriginalStop: 99, Pct24hAtEntry: 4, Pct4hAtEntry: 2, Pct1hAtEntry: 1},
			Exit: ClosedTradeExit{
				NetPnL:            2,
				HoldMinutes:       12,
				HitTP1:            true,
				StopOutType:       "profit_lock",
				Pct24hAtExit:      5,
				Pct4hAtExit:       3,
				Pct1hAtExit:       1,
				RealizedExitPrice: 103,
			},
			PostExit: ClosedTradePostExit{PostExitPeakR: 0.5, EODCaptureR: 0.2, EODPct24h: 5, EODPct4h: 3, EODPct1h: 1},
		},
	}
	outputs := BuildOutputs(records)
	if outputs.Summary.TradeCount != 1 || outputs.Summary.WinRate != 1 {
		t.Fatalf("unexpected summary: %+v", outputs.Summary)
	}
	if len(outputs.BySetupFamily) != 1 || outputs.BySetupFamily[0].Label != "breakout_retest" {
		t.Fatalf("unexpected setup family rows: %+v", outputs.BySetupFamily)
	}
	if len(outputs.ByEntryScoreBucket) != 1 || outputs.ByEntryScoreBucket[0].Label != "below_55" {
		t.Fatalf("unexpected score bucket rows: %+v", outputs.ByEntryScoreBucket)
	}
	dir := t.TempDir()
	if err := WriteCompatFiles(dir, outputs, filepath.Join(dir, "compat.csv"), filepath.Join(dir, "compat.json")); err != nil {
		t.Fatalf("write compat files: %v", err)
	}
	for _, name := range []string{
		"paper_summary.json",
		"paper_by_setup_family.csv",
		"paper_by_strategy_family.csv",
		"paper_by_setup.csv",
		"paper_by_symbol_side_setup.csv",
		"paper_by_entry_timing.csv",
		"paper_by_entry_outcome.csv",
		"paper_by_entry_score_bucket.csv",
		"paper_by_scanner_pattern.csv",
		"paper_stopout_report.csv",
		"paper_reversal_opportunity_report.csv",
		"paper_eod_hold_report.csv",
		"paper_trade_labels.csv",
		"paper_rule_candidates.json",
		"paper_setup_report.json",
		"paper_setup_report.csv",
		"compat.csv",
		"compat.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("expected %s, err=%v", name, err)
		}
	}
	f, err := os.Open(filepath.Join(dir, "paper_summary.json"))
	if err != nil {
		t.Fatalf("open summary: %v", err)
	}
	defer f.Close()
	var summary Summary
	if err := json.NewDecoder(f).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.TradeCount != 1 {
		t.Fatalf("unexpected decoded summary: %+v", summary)
	}
}

func TestBuildTradeLabelsCapturesEntryBuckets(t *testing.T) {
	records := []ClosedTradeRecord{
		{
			TradeID: "bucket-1",
			Symbol:  "SYNUSDT",
			Side:    "BUY",
			Identity: ClosedTradeIdentity{
				RawStrategy:    "pullback_reclaim",
				SetupFamily:    "micro_pullback_continuation",
				EntryTiming:    "late",
				EntryScore:     EntryScoreBreakdown{FinalScore: 78},
				StrategyFamily: "cont",
			},
			Entry: ClosedTradeEntry{EntryPrice: 1, Qty: 1},
			Plan:  ClosedTradePlan{OriginalStop: 0.9},
			Exit:  ClosedTradeExit{NetPnL: -0.1, EntryOutcomeLabel: "WEAK_PROOF"},
		},
	}
	labels := BuildTradeLabels(records)
	if len(labels) != 1 {
		t.Fatalf("expected one label, got %d", len(labels))
	}
	if labels[0].EntryTiming != "late" {
		t.Fatalf("expected entry timing late, got %q", labels[0].EntryTiming)
	}
	if labels[0].EntryOutcomeLabel != "WEAK_PROOF" {
		t.Fatalf("expected entry outcome WEAK_PROOF, got %q", labels[0].EntryOutcomeLabel)
	}
	if labels[0].EntryScoreBucket != "75_84" {
		t.Fatalf("expected score bucket 75_84, got %q", labels[0].EntryScoreBucket)
	}
}

func TestLoadClosedTradesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper_closed_trades.jsonl")
	rec := ClosedTradeRecord{
		TradeID: "x1",
		Symbol:  "reusdt",
		Side:    "buy",
		Entry:   ClosedTradeEntry{EntryTs: time.Now().UTC()},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := json.NewEncoder(f).Encode(rec); err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = f.Close()
	rows, err := LoadClosedTradesJSONL(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rows) != 1 || rows[0].Symbol != "REUSDT" || rows[0].Side != "BUY" {
		t.Fatalf("unexpected loaded rows: %+v", rows)
	}
}
