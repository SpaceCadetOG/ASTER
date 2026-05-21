package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		Timestamp:    now.Add(-1 * time.Minute),
		Type:         "POSITION_CLOSE",
		Simulated:    true,
		Symbol:       "ETHUSDT",
		Side:         "SELL",
		Source:       "paper_auto",
		Mode:         "paper_auto",
		Strategy:     "exhaustion_flip_short",
		SetupFamily:  "reversal",
		Grade:        "B",
		State:        "IN_PLAY",
		EntryPx:      200,
		ExitPx:       190,
		PnLUSD:       12.5,
		PnLPct:       5,
		Fees:         0.4,
		RiskR:        1.2,
		HoldMin:      18,
		MFER:         2.1,
		MAER:         0.4,
		CaptureRatio: 0.55,
		MaxGivebackR: 0.2,
		Reason:       "TP2",
		TriggerState: "ready",
		ExitProfile:  "ladder",
	})

	paper := &paperTrader{
		enabled:   true,
		balance:   1005,
		reserve:   5,
		reportLoc: time.UTC,
		positions: map[string]*paperPosition{
			"BTCUSDT": {
				Symbol:           "BTCUSDT",
				Side:             "BUY",
				Entry:            100,
				Qty:              1,
				Margin:           50,
				Leverage:         3,
				Stop:             98,
				TP1:              101,
				TP2:              102,
				TP3:              103,
				OpenedAt:         now.Add(-5 * time.Minute),
				MaxFavorableR:    1.5,
				MaxAdverseR:      0.3,
				LastMark:         101.5,
				EntryMode:        "paper_auto",
				EntryReason:      "paper_auto_test",
				EntryStrategyID:  "paper_auto_test",
				EntrySetupFamily: "reversal",
				EntryGrade:       "A",
				EntryTrigger:     "ready",
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

func TestBuildLivePaperSnapshotEmptyStateUsesSafeArrays(t *testing.T) {
	now := time.Now().UTC()
	paper := &paperTrader{
		enabled:   true,
		balance:   1000,
		reserve:   25,
		reportLoc: time.UTC,
		positions: map[string]*paperPosition{},
		dayStats: map[string]*paperDayStats{
			now.Format("2006-01-02"): {Net: 0},
		},
	}
	snap := buildLivePaperSnapshot(runtimeModeManualOnly, paper, map[string]symbolMeta{}, nil, 5)
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.OpenPositions == nil || snap.RecentClosed == nil || snap.RecentDecisions == nil {
		t.Fatalf("expected safe empty arrays, got %+v", snap)
	}
	if len(snap.OpenPositions) != 0 || len(snap.RecentClosed) != 0 || len(snap.RecentDecisions) != 0 {
		t.Fatalf("expected empty arrays, got %+v", snap)
	}
}

func TestLiveStatusStoreSnapshotClonesPaperSlices(t *testing.T) {
	store := newLiveStatusStore()
	store.Set(liveStatus{
		Mode:   "paper_auto",
		DryRun: true,
		Paper: &livePaperSnapshot{
			Mode:          "paper_auto",
			OpenCount:     1,
			OpenPositions: []livePaperPositionView{{Symbol: "BTCUSDT"}},
		},
	})
	snap := store.Snapshot()
	if snap.Paper == nil || snap.Paper.OpenCount != 1 {
		t.Fatalf("expected paper snapshot, got %+v", snap.Paper)
	}
	snap.Paper.OpenPositions[0].Symbol = "ETHUSDT"
	again := store.Snapshot()
	if again.Paper.OpenPositions[0].Symbol != "BTCUSDT" {
		t.Fatalf("expected cloned paper snapshot, got %+v", again.Paper.OpenPositions)
	}
}

func TestStatusMuxReturnsQuicklyInPaperAndPaperAutoModes(t *testing.T) {
	cases := []liveStatus{
		{
			Mode:        "paper",
			ModeState:   "manual_only",
			DryRun:      true,
			LiveEnabled: false,
			Paper: &livePaperSnapshot{
				Mode:            "paper",
				Summary:         "paper ok",
				OpenPositions:   []livePaperPositionView{},
				RecentClosed:    []livePaperClosedView{},
				RecentDecisions: []livePaperDecisionView{},
			},
		},
		{
			Mode:        "paper_auto",
			ModeState:   "paper_auto_enabled",
			DryRun:      true,
			LiveEnabled: false,
			Paper: &livePaperSnapshot{
				Mode:            "paper_auto",
				Summary:         "paper_auto ok",
				OpenPositions:   []livePaperPositionView{},
				RecentClosed:    []livePaperClosedView{},
				RecentDecisions: []livePaperDecisionView{},
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		store := newLiveStatusStore()
		store.Set(tc)
		handler := newLiveStatusMux(store)
		t.Run(tc.Mode, func(t *testing.T) {
			start := time.Now()
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if time.Since(start) > 500*time.Millisecond {
				t.Fatalf("status request took too long: %s", time.Since(start))
			}
			var payload liveStatus
			if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if payload.Paper == nil {
				t.Fatalf("expected paper payload for %s", tc.Mode)
			}
			if payload.DryRun != true || payload.LiveEnabled {
				t.Fatalf("unexpected runtime flags: %+v", payload)
			}
		})
	}
}

func TestStatusMuxRepeatedCallsDoNotDeadlock(t *testing.T) {
	store := newLiveStatusStore()
	store.Set(liveStatus{
		Mode:        "paper_auto",
		ModeState:   "paper_auto_enabled",
		DryRun:      true,
		LiveEnabled: false,
		Paper: &livePaperSnapshot{
			Mode:            "paper_auto",
			OpenPositions:   []livePaperPositionView{},
			RecentClosed:    []livePaperClosedView{},
			RecentDecisions: []livePaperDecisionView{},
		},
	})
	handler := newLiveStatusMux(store)
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var payload liveStatus
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode %d failed: %v", i, err)
		}
		if payload.Mode != "paper_auto" {
			t.Fatalf("unexpected payload on iteration %d: %+v", i, payload)
		}
	}
}
