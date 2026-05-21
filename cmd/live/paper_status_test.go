package main

import (
	"path/filepath"
	"testing"
	"time"

	"go-machine/internal/stats"
)

func TestBuildLivePaperSnapshotIncludesOpenClosedAndDecisionViews(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "events.jsonl")
	logger := stats.NewEventLogger(logPath, true, false, true)
	allow := true
	now := time.Now().UTC()
	logger.Emit(stats.Event{
		Timestamp:       now.Add(-2 * time.Minute),
		Type:            "ENTRY_DECISION",
		Simulated:       true,
		Symbol:          "BTCUSDT",
		Side:            "BUY",
		Source:          "paper_auto",
		Mode:            "paper_auto",
		Strategy:        "paper_auto_test",
		SetupFamily:     "reversal",
		Grade:           "A",
		State:           "IN_PLAY",
		Score:           99,
		Slope:           0.5,
		ConfluenceScore: 0.81,
		EntryPx:         100,
		StopDistPct:     1.0,
		GateAllow:       &allow,
		GateReasons:     []string{"paper_auto"},
	})
	logger.Emit(stats.Event{
		Timestamp:      now.Add(-1 * time.Minute),
		Type:           "POSITION_CLOSE",
		Simulated:      true,
		Symbol:         "ETHUSDT",
		Side:           "SELL",
		Source:         "paper_auto",
		Mode:           "paper_auto",
		Strategy:       "exhaustion_flip_short",
		SetupFamily:    "reversal",
		Grade:          "B",
		State:          "IN_PLAY",
		EntryPx:        200,
		ExitPx:         190,
		PnLUSD:         12.5,
		PnLPct:         5,
		Fees:           0.4,
		RiskR:          1.2,
		HoldMin:        18,
		MFER:           2.1,
		MAER:           0.4,
		CaptureRatio:   0.55,
		MaxGivebackR:   0.2,
		Reason:         "TP2",
		TriggerState:   "ready",
		ExitProfile:    "ladder",
	})

	paper := &paperTrader{
		enabled:   true,
		balance:   1005,
		reserve:   5,
		reportLoc: time.UTC,
		positions: map[string]*paperPosition{
			"BTCUSDT": {
				Symbol:         "BTCUSDT",
				Side:           "BUY",
				Entry:          100,
				Qty:            1,
				Margin:         50,
				Leverage:       3,
				Stop:           98,
				TP1:            101,
				TP2:            102,
				TP3:            103,
				OpenedAt:       now.Add(-5 * time.Minute),
				MaxFavorableR:  1.5,
				MaxAdverseR:    0.3,
				LastMark:       101.5,
				EntryMode:      "paper_auto",
				EntryReason:    "paper_auto_test",
				EntryStrategyID:"paper_auto_test",
				EntrySetupFamily:"reversal",
				EntryGrade:     "A",
				EntryTrigger:   "ready",
			},
		},
		dayStats: map[string]*paperDayStats{
			now.Format("2006-01-02"): {Net: 7.25},
		},
	}
	meta := map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 101.5},
	}

	snap := buildLivePaperSnapshot(runtimeModePaperAuto, paper, meta, logger, 5)
	if snap == nil {
		t.Fatal("expected paper snapshot")
	}
	if snap.OpenCount != 1 || len(snap.OpenPositions) != 1 {
		t.Fatalf("expected one open position, got %+v", snap)
	}
	if snap.OpenPositions[0].Symbol != "BTCUSDT" || snap.OpenPositions[0].Mode != "paper_auto" {
		t.Fatalf("unexpected open position payload: %+v", snap.OpenPositions[0])
	}
	if len(snap.RecentClosed) != 1 || snap.RecentClosed[0].Symbol != "ETHUSDT" {
		t.Fatalf("unexpected recent closed payload: %+v", snap.RecentClosed)
	}
	if len(snap.RecentDecisions) != 1 || !snap.RecentDecisions[0].Approved {
		t.Fatalf("unexpected recent decisions payload: %+v", snap.RecentDecisions)
	}
}

func TestLiveStatusStoreSnapshotInjectsPaperProvider(t *testing.T) {
	store := newLiveStatusStore()
	store.Set(liveStatus{Mode: "paper_auto", DryRun: true})
	store.SetPaperProvider(func() *livePaperSnapshot {
		return &livePaperSnapshot{
			Mode:      "paper_auto",
			OpenCount: 1,
		}
	})
	snap := store.Snapshot()
	if snap.Paper == nil || snap.Paper.OpenCount != 1 {
		t.Fatalf("expected injected paper snapshot, got %+v", snap.Paper)
	}
}
