package main

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"go-machine/adapters/aster"
)

func TestPaperClosedTradeLedgerPreservesOriginalTargetsAndMetadata(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Date(2026, 6, 20, 17, 25, 10, 0, time.UTC)
	pos := &paperPosition{
		Symbol:               "REUSDT",
		Side:                 "BUY",
		TradeID:              "paper-1",
		Entry:                1.029312,
		Qty:                  10,
		InitialQty:           10,
		Margin:               10,
		Leverage:             5,
		Stop:                 1.026447,
		OriginalStop:         1.026447,
		TP1:                  1.034000,
		TP2:                  1.039000,
		TP3:                  1.045000,
		OriginalTP1:          1.034000,
		OriginalTP2:          1.039000,
		OriginalTP3:          1.045000,
		OpenedAt:             now.Add(-20 * time.Minute),
		EntryReason:          "vp_trend",
		RawEntryReason:       "vp_trend",
		EntrySetupFamily:     "breakout_retest",
		ExecBucket:           "continuation",
		EntryStyle:           "pullback_long",
		EntryStrategyFamily:  "cont",
		EntryGrade:           "A",
		EntrySession:         "US",
		EntryTiming:          "mid",
		EntryConfluenceScore: 82.5,
		EntryPct24h:          6.6,
		EntryPct4h:           2.1,
		EntryPct1h:           -0.4,
	}

	paper.positions["REUSDT"] = pos
	paper.exitPortion(now, pos, "SL", 1.026447, pos.Qty, symbolMeta{LastPrice: 1.026447, DayUTC24h: 7.1, UTC4hPct: 2.4, UTC1hPct: -0.2}, aster.OrderBook{})

	rec, ok := paper.closedTradeLedger["paper-1"]
	if !ok {
		t.Fatalf("expected closed trade ledger row")
	}
	if rec.Plan.OriginalTP1 != 1.034000 || rec.Plan.OriginalTP2 != 1.039000 || rec.Plan.OriginalTP3 != 1.045000 {
		t.Fatalf("expected original targets preserved, got %+v", rec.Plan)
	}
	if rec.Plan.Pct24hAtEntry != 6.6 || rec.Plan.Pct4hAtEntry != 2.1 || rec.Plan.Pct1hAtEntry != -0.4 {
		t.Fatalf("expected entry scanner values preserved, got %+v", rec.Plan)
	}
	if rec.Exit.Pct24hAtExit != 7.1 || rec.Exit.Pct4hAtExit != 2.4 || rec.Exit.Pct1hAtExit != -0.2 {
		t.Fatalf("expected exit scanner values preserved, got %+v", rec.Exit)
	}
	if rec.Exit.RealizedExitPrice == rec.Plan.OriginalTP1 {
		t.Fatalf("expected realized exit to remain separate from original tp1, got %.6f", rec.Exit.RealizedExitPrice)
	}
	if rec.Identity.RawStrategy != "vp_trend" || !rec.Identity.StrategyMissing || rec.Identity.SetupFamily != "breakout_retest" || rec.Identity.ExecBucket != "continuation" {
		t.Fatalf("expected strategy/setup/bucket preserved, got %+v", rec.Identity)
	}

	f, err := os.Open(paper.tradesCSV)
	if err != nil {
		t.Fatalf("open csv: %v", err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected header + 1 row, got %d", len(rows))
	}
	header := rows[0]
	record := rows[1]
	idx := map[string]int{}
	for i, h := range header {
		idx[h] = i
	}
	if got := record[idx["tp"]]; got != "1.03400000" {
		t.Fatalf("expected legacy tp column to preserve original tp1, got %s", got)
	}
	if got := record[idx["realized_exit_price"]]; got != "1.02567729" {
		t.Fatalf("expected simulated realized exit price column, got %s", got)
	}
	if got := record[idx["pct24h_at_entry"]]; got != "6.6000" {
		t.Fatalf("expected entry day scanner in csv, got %s", got)
	}
	if got := record[idx["pct24h_at_exit"]]; got != "7.1000" {
		t.Fatalf("expected exit day scanner in csv, got %s", got)
	}
}

func TestPaperClosedTradeLedgerPreservesScannerChainForShorts(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Date(2026, 6, 20, 18, 15, 0, 0, time.UTC)
	pos := &paperPosition{
		Symbol:           "GUAUSDT",
		Side:             "SELL",
		TradeID:          "paper-short-scanner-1",
		Entry:            0.6500,
		Qty:              100,
		InitialQty:       100,
		Margin:           10,
		Leverage:         5,
		Stop:             0.6700,
		OriginalStop:     0.6700,
		TP1:              0.6300,
		TP2:              0.6200,
		TP3:              0.6100,
		OriginalTP1:      0.6300,
		OriginalTP2:      0.6200,
		OriginalTP3:      0.6100,
		OpenedAt:         now.Add(-12 * time.Minute),
		EntryReason:      "vp_trend",
		RawEntryReason:   "vp_trend",
		EntrySetupFamily: "breakout_retest",
		ExecBucket:       "continuation",
		EntryStyle:       "pullback_short",
		EntryPct24h:      -8.5,
		EntryPct4h:       -3.4,
		EntryPct1h:       -1.2,
	}

	paper.positions["GUAUSDT"] = pos
	paper.exitPortion(now, pos, "TP1", 0.6300, pos.Qty, symbolMeta{LastPrice: 0.6300, DayUTC24h: -9.1, UTC4hPct: -4.0, UTC1hPct: -1.8}, aster.OrderBook{})

	rec := paper.closedTradeLedger["paper-short-scanner-1"]
	if rec.Plan.Pct24hAtEntry != -8.5 || rec.Plan.Pct4hAtEntry != -3.4 || rec.Plan.Pct1hAtEntry != -1.2 {
		t.Fatalf("expected short entry scanner values preserved, got %+v", rec.Plan)
	}
	if rec.Exit.Pct24hAtExit != -9.1 || rec.Exit.Pct4hAtExit != -4.0 || rec.Exit.Pct1hAtExit != -1.8 {
		t.Fatalf("expected short exit scanner values preserved, got %+v", rec.Exit)
	}
}

func TestPaperClosedTradeLedgerTracksLongAndShortPostExitOpportunity(t *testing.T) {
	t.Run("long", func(t *testing.T) {
		paper := testCleanupPaperTrader(t)
		exitTs := time.Date(2026, 6, 20, 17, 25, 10, 0, time.UTC)
		rec := paperClosedTradeRecord{
			TradeID: "long-1",
			Mode:    "paper",
			Symbol:  "LABUSDT",
			Side:    "BUY",
			Entry:   paperClosedTradeEntry{EntryTs: exitTs.Add(-15 * time.Minute), EntryPrice: 100},
			Plan:    paperClosedTradePlan{OriginalStop: 98, OriginalTP1: 102, OriginalTP2: 104, OriginalTP3: 106},
			Exit:    paperClosedTradeExit{ExitTs: exitTs, RealizedExitPrice: 101},
		}
		paper.closedTradeLedger["long-1"] = rec
		paper.startPostExitObservation(rec)
		paper.updatePostExitTrackers(exitTs.Add(10*time.Minute), map[string]symbolMeta{"LABUSDT": {LastPrice: 105}})
		paper.updatePostExitTrackers(exitTs.Add(20*time.Minute), map[string]symbolMeta{"LABUSDT": {LastPrice: 99}})
		paper.updatePostExitTrackers(exitTs.Add(61*time.Minute), map[string]symbolMeta{"LABUSDT": {LastPrice: 107}})

		got := paper.closedTradeLedger["long-1"].PostExit
		if !got.MissedTP2 {
			t.Fatalf("expected long trade to mark missed tp2 after exit, got %+v", got)
		}
		if got.PeakPrice15m != 105 {
			t.Fatalf("expected 15m peak 105, got %.2f", got.PeakPrice15m)
		}
		if got.BestR15m <= 0 {
			t.Fatalf("expected positive best R, got %.2f", got.BestR15m)
		}
	})

	t.Run("short", func(t *testing.T) {
		paper := testCleanupPaperTrader(t)
		exitTs := time.Date(2026, 6, 20, 17, 25, 10, 0, time.UTC)
		rec := paperClosedTradeRecord{
			TradeID: "short-1",
			Mode:    "paper",
			Symbol:  "BTWUSDT",
			Side:    "SELL",
			Entry:   paperClosedTradeEntry{EntryTs: exitTs.Add(-15 * time.Minute), EntryPrice: 100},
			Plan:    paperClosedTradePlan{OriginalStop: 102, OriginalTP1: 98, OriginalTP2: 96, OriginalTP3: 94},
			Exit:    paperClosedTradeExit{ExitTs: exitTs, RealizedExitPrice: 99},
		}
		paper.closedTradeLedger["short-1"] = rec
		paper.startPostExitObservation(rec)
		paper.updatePostExitTrackers(exitTs.Add(10*time.Minute), map[string]symbolMeta{"BTWUSDT": {LastPrice: 95}})
		paper.updatePostExitTrackers(exitTs.Add(20*time.Minute), map[string]symbolMeta{"BTWUSDT": {LastPrice: 103}})
		paper.updatePostExitTrackers(exitTs.Add(61*time.Minute), map[string]symbolMeta{"BTWUSDT": {LastPrice: 94}})

		got := paper.closedTradeLedger["short-1"].PostExit
		if !got.MissedTP2 {
			t.Fatalf("expected short trade to mark missed tp2 after exit, got %+v", got)
		}
		if got.TroughPrice15m != 95 {
			t.Fatalf("expected 15m trough 95, got %.2f", got.TroughPrice15m)
		}
		if got.BestR15m <= 0 {
			t.Fatalf("expected positive best R, got %.2f", got.BestR15m)
		}
	})
}

func TestPaperClosedTradeLedgerMarksMissingStrategyAndNormalizesProtectedStop(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Date(2026, 6, 21, 19, 55, 37, 0, time.UTC)
	pos := &paperPosition{
		Symbol:              "TNSRUSDT",
		Side:                "BUY",
		TradeID:             "paper-2",
		Entry:               100,
		Qty:                 1,
		InitialQty:          1,
		Margin:              10,
		Leverage:            5,
		Stop:                101,
		OriginalStop:        99,
		TP1:                 103,
		TP2:                 104,
		TP3:                 105,
		OriginalTP1:         103,
		OriginalTP2:         104,
		OriginalTP3:         105,
		OpenedAt:            now.Add(-7 * time.Minute),
		EntryReason:         "unknown",
		RawEntryReason:      "unknown",
		EntrySetupFamily:    "unknown",
		ExecBucket:          "unknown",
		EntryStyle:          "unknown",
		EntryStrategyFamily: "unknown",
		TrailOn:             true,
	}

	paper.positions["TNSRUSDT"] = pos
	paper.exitPortion(now, pos, "SL", 101.5, pos.Qty, symbolMeta{LastPrice: 101.5}, aster.OrderBook{})

	rec := paper.closedTradeLedger["paper-2"]
	if !rec.Identity.StrategyMissing || rec.Identity.Strategy != "unknown" {
		t.Fatalf("expected missing strategy flag, got %+v", rec.Identity)
	}
	if rec.Exit.ExitReason != "Trailing Stop" && rec.Exit.ExitReason != "Protected Stop" {
		t.Fatalf("expected profitable SL to normalize to protected/trailing stop, got %+v", rec.Exit)
	}
	if rec.Exit.StopOutType != "profit_lock" {
		t.Fatalf("expected profitable stop to classify as profit_lock, got %+v", rec.Exit)
	}

	f, err := os.Open(paper.closedTradesJSONL)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	found := false
	for dec.More() {
		var row paperClosedTradeRecord
		if err := dec.Decode(&row); err != nil {
			t.Fatalf("decode jsonl: %v", err)
		}
		if row.TradeID == "paper-2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected jsonl ledger to contain trade paper-2")
	}
}

func TestPaperClosedTradeLedgerCarriesShortPhase2Fields(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Date(2026, 6, 22, 2, 10, 0, 0, time.UTC)
	pos := &paperPosition{
		Symbol:                "GUAUSDT",
		Side:                  "SELL",
		TradeID:               "paper-short-1",
		Entry:                 0.6500,
		Qty:                   100,
		InitialQty:            100,
		Margin:                75,
		Leverage:              5,
		Stop:                  0.6700,
		OriginalStop:          0.6700,
		TP1:                   0.6300,
		TP2:                   0.6200,
		TP3:                   0.6100,
		OriginalTP1:           0.6300,
		OriginalTP2:           0.6200,
		OriginalTP3:           0.6100,
		OpenedAt:              now.Add(-12 * time.Minute),
		EntryReason:           "vp_trend",
		RawEntryReason:        "vp_trend",
		EntryStyle:            "pullback_short",
		EntrySetupFamily:      "breakout_retest",
		ExecBucket:            "continuation",
		ShortBucket:           "failed_bounce_short",
		ShortFilterReason:     "short_allowed_failed_bounce",
		DirectShortAllowed:    true,
		ShortRequireConfirm:   "",
		EntryPct24h:           100,
		EntryPct4h:            -35,
		EntryPct1h:            -12,
		BounceFromLocalLowPct: 2.7,
		FailedBounceConfirmed: true,
		PostPumpBreakdown:     true,
		LateChaseBlocked:      false,
	}

	paper.positions["GUAUSDT"] = pos
	paper.exitPortion(now, pos, "TP1", 0.6300, pos.Qty, symbolMeta{LastPrice: 0.6300}, aster.OrderBook{})

	rec := paper.closedTradeLedger["paper-short-1"]
	if rec.Plan.ShortBucket != "failed_bounce_short" {
		t.Fatalf("expected short bucket persisted, got %+v", rec.Plan)
	}
	if !rec.Plan.DirectShortAllowed || !rec.Plan.FailedBounceConfirmed || !rec.Plan.PostPumpBreakdown {
		t.Fatalf("expected short-phase flags preserved, got %+v", rec.Plan)
	}
	if rec.Plan.Pct24hAtEntry != 100 || rec.Plan.Pct4hAtEntry != -35 || rec.Plan.Pct1hAtEntry != -12 {
		t.Fatalf("expected timeframe values preserved, got %+v", rec.Plan)
	}
}

func TestPaperClosedTradeLedgerCapturesTPHitsAndRatchetMode(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	paper.tpRatchetOnly = false
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	pos := &paperPosition{
		Symbol:       "LABUSDT",
		Side:         "BUY",
		TradeID:      "paper-hits-1",
		Entry:        10,
		Qty:          1,
		InitialQty:   1,
		Margin:       10,
		Leverage:     5,
		Stop:         10.4,
		OriginalStop: 9.7,
		TP1:          10.3,
		TP2:          10.6,
		TP3:          10.9,
		OriginalTP1:  10.3,
		OriginalTP2:  10.6,
		OriginalTP3:  10.9,
		HitTP1:       true,
		HitTP2:       true,
		OpenedAt:     now.Add(-15 * time.Minute),
	}

	out := captureStdout(t, func() {
		paper.positions["LABUSDT"] = pos
		paper.exitPortion(now, pos, "SL", 10.4, pos.Qty, symbolMeta{LastPrice: 10.4}, aster.OrderBook{})
	})

	rec := paper.closedTradeLedger["paper-hits-1"]
	if !rec.Exit.HitTP1 || !rec.Exit.HitTP2 || rec.Exit.HitTP3 {
		t.Fatalf("expected TP hit state preserved, got %+v", rec.Exit)
	}
	if rec.Exit.FinalStopPrice != 10.4 {
		t.Fatalf("expected final stop price recorded, got %.4f", rec.Exit.FinalStopPrice)
	}
	if rec.Exit.TPRatchetOnly {
		t.Fatalf("expected ratchet mode false in ledger, got %+v", rec.Exit)
	}
	for _, want := range []string{"hit_tp1=true", "hit_tp2=true", "hit_tp3=false", "tp_ratchet_only=false"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected paper exit log to contain %q, got:\n%s", want, out)
		}
	}
}

func TestPaperClosedTradeLedgerCapturesEODAndStopReclaim(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	paper.reportLoc = loc
	exitTs := time.Date(2026, 6, 22, 20, 0, 0, 0, time.UTC)
	rec := paperClosedTradeRecord{
		TradeID: "eod-1",
		Mode:    "paper",
		Symbol:  "LABUSDT",
		Side:    "BUY",
		Entry:   paperClosedTradeEntry{EntryTs: exitTs.Add(-10 * time.Minute), EntryPrice: 100},
		Plan:    paperClosedTradePlan{OriginalStop: 98, OriginalTP1: 102, OriginalTP2: 104, OriginalTP3: 106},
		Exit: paperClosedTradeExit{
			ExitTs:            exitTs,
			RealizedExitPrice: 99,
			RawExitReason:     "SL",
		},
	}
	paper.closedTradeLedger["eod-1"] = rec
	paper.startPostExitObservation(rec)

	paper.updatePostExitTrackers(exitTs.Add(10*time.Minute), map[string]symbolMeta{"LABUSDT": {LastPrice: 101}})
	targetUTC := paper.postExitTrackers["eod-1"].EODTargetUTC
	paper.updatePostExitTrackers(targetUTC.Add(time.Second), map[string]symbolMeta{"LABUSDT": {LastPrice: 105, DayUTC24h: 12, UTC4hPct: 4, UTC1hPct: 1}})

	got := paper.closedTradeLedger["eod-1"].PostExit
	if got.EODPriceCST185959 != 105 {
		t.Fatalf("expected EOD price captured, got %+v", got)
	}
	if got.EODPct24h != 12 || got.EODPct4h != 4 || got.EODPct1h != 1 {
		t.Fatalf("expected EOD scanner values captured, got %+v", got)
	}
	if got.EODTimestampUTC != targetUTC {
		t.Fatalf("expected EOD UTC timestamp preserved, got %+v", got)
	}
	if got.PostExitPeakPrice != 101 {
		t.Fatalf("expected post-exit peak price from first window update, got %+v", got)
	}
	if !got.StoppedThenReclaim || !got.ReentryWouldWork {
		t.Fatalf("expected stop reclaim and reentry flags, got %+v", got)
	}
}
