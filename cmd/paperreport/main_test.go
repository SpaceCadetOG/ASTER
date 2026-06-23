package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildReportSummariesAndFilters(t *testing.T) {
	rows := []tradeRow{
		{
			TradeID:          "t1",
			Symbol:           "GUAUSDT",
			Side:             "BUY",
			Strategy:         "vp_trend",
			SetupFamily:      "micro_pullback_continuation",
			Session:          "NY_OPEN",
			EntryTiming:      "late",
			NetPnL:           5,
			HoldMinutes:      30,
			CandidateAgeSecs: 90,
			ATRExension:      0.7,
			PostExitBestR15m: 0.5,
			PostExitBestR60m: 1.2,
			PeakPrice60m:     1.12,
			ExitPrice:        1.05,
			Qty:              10,
			MaxRSeen:         0.4,
		},
		{
			TradeID:          "t2",
			Symbol:           "LABUSDT",
			Side:             "BUY",
			Strategy:         "breakout_retest",
			SetupFamily:      "none",
			Session:          "UTC_OFF_HOURS",
			EntryTiming:      "late",
			NetPnL:           -3,
			HoldMinutes:      80,
			CandidateAgeSecs: 480,
			ATRExension:      1.8,
			PostExitBestR15m: 0.1,
			PostExitBestR60m: 0.3,
			PeakPrice60m:     9.5,
			ExitPrice:        10,
			Qty:              5,
		},
	}

	rep := buildReport(rows, "test", "fixture", 4)
	if rep.Overall.Trades != 2 || rep.Overall.Wins != 1 || rep.Overall.Losses != 1 {
		t.Fatalf("unexpected overall summary: %+v", rep.Overall)
	}
	if got := rep.BySetup[0].Label; got != "micro_pullback_continuation" {
		t.Fatalf("expected profitable setup first, got %s", got)
	}
	var combo filterImpact
	found := false
	for _, f := range rep.FilterImpacts {
		if f.Label == "combo_clean_core" {
			combo = f
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected combo_clean_core filter impact")
	}
	if combo.Removed != 1 || combo.Remaining != 1 {
		t.Fatalf("unexpected combo filter impact: %+v", combo)
	}
	if combo.NetPnL != 5 {
		t.Fatalf("expected filtered net 5, got %.2f", combo.NetPnL)
	}
	if len(rep.ByEntryAction) == 0 || len(rep.ByExitAction) == 0 {
		t.Fatalf("expected entry/exit improvement summaries")
	}
}

func TestLoadClosedTradesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper_closed_trades.jsonl")
	rows := []closedTradeRecord{
		{
			TradeID: "paper-1",
			Symbol:  "REUSDT",
			Side:    "BUY",
			Identity: closedTradeIdentity{
				Strategy:         "vp_trend",
				SetupFamily:      "breakout_retest",
				ExecBucket:       "continuation",
				EntryStyle:       "pullback_long",
				StrategyFamily:   "cont",
				Session:          "NY_OPEN",
				EntryTiming:      "mid",
				CandidateAgeSecs: 120,
				DistanceToVWAP:   1.4,
				ATRExension:      0.9,
				ConfluenceScore:  81,
			},
			Entry: closedTradeEntry{
				EntryTs:    time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
				EntryPrice: 1.02,
				Qty:        10,
				Leverage:   5,
				MarginUsed: 10,
			},
			Plan: closedTradePlan{
				OriginalStop: 1.00,
				OriginalTP1:  1.04,
				OriginalTP2:  1.06,
				OriginalTP3:  1.08,
			},
			Exit: closedTradeExit{
				ExitTs:            time.Date(2026, 6, 20, 10, 30, 0, 0, time.UTC),
				RealizedExitPrice: 1.05,
				ExitReason:        "TP1",
				RawExitReason:     "TP1",
				NetPnL:            3.2,
				HoldMinutes:       30,
				ProtectionState:   "locked",
				MaxRSeen:          1.2,
				MinRSeen:          -0.2,
			},
			PostExit: closedTradePostExitStats{
				PeakPrice60m: 1.08,
				BestR15m:     0.4,
				BestR60m:     0.8,
				MissedTP2:    true,
				ExitVsTP1:    0.0,
			},
		},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jsonl: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			t.Fatalf("encode jsonl: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close jsonl: %v", err)
	}

	got, err := loadClosedTradesJSONL(path)
	if err != nil {
		t.Fatalf("load jsonl: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Strategy != "vp_trend" || got[0].SetupFamily != "breakout_retest" {
		t.Fatalf("unexpected mapped row: %+v", got[0])
	}
	if got[0].ExitReason != "TP1" || got[0].NetPnL != 3.2 {
		t.Fatalf("unexpected exit mapping: %+v", got[0])
	}
	if !got[0].MissedTP2 || got[0].PeakPrice60m != 1.08 {
		t.Fatalf("expected post-exit fields mapped, got %+v", got[0])
	}
}

func TestEnrichTradeRowClassifiesEntryAndExitImprovements(t *testing.T) {
	row := enrichTradeRow(tradeRow{
		Symbol:           "LABUSDT",
		Side:             "BUY",
		SetupFamily:      "none",
		Session:          "UTC_OFF_HOURS",
		CandidateAgeSecs: 480,
		ATRExension:      1.9,
		DistanceToVWAP:   6.2,
		EntryPrice:       10,
		ExitPrice:        10.5,
		Qty:              2,
		MaxRSeen:         0.6,
		PostExitBestR60m: 1.9,
		PeakPrice60m:     12.0,
		MissedTP3:        true,
	})
	if row.EntryImprovementAction != "resolve_setup_before_entry" {
		t.Fatalf("unexpected entry action: %+v", row)
	}
	if row.ExitImprovementAction != "let_runner_breathe_after_partial" {
		t.Fatalf("unexpected exit action: %+v", row)
	}
	if row.PostExitOpportunityPnL60m <= 0 || row.PostExitOpportunityR60m <= 0 {
		t.Fatalf("expected positive post-exit opportunity, got %+v", row)
	}
}
