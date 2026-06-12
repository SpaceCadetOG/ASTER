package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
		Source:          "paper",
		Mode:            "paper",
		Strategy:        "paper_test",
		SetupFamily:     "reversal",
		Grade:           "A",
		State:           "IN_PLAY",
		Score:           99,
		Slope:           0.5,
		ConfluenceScore: 0.81,
		EntryPx:         100,
		StopDistPct:     1.0,
		GateAllow:       &allow,
		GateReasons:     []string{"paper"},
	})
	logger.Emit(stats.Event{
		Timestamp:    now.Add(-1 * time.Minute),
		Type:         "POSITION_CLOSE",
		Simulated:    true,
		Symbol:       "ETHUSDT",
		Side:         "SELL",
		Source:       "paper",
		Mode:         "paper",
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
				EntryMode:        "paper",
				EntryReason:      "paper_test",
				EntryStrategyID:  "paper_test",
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

	snap := buildLivePaperSnapshot(runtimeModePaper, paper, meta, logger, 5)
	if snap == nil {
		t.Fatal("expected paper snapshot")
	}
	if snap.OpenCount != 1 || len(snap.OpenPositions) != 1 {
		t.Fatalf("expected one open position, got %+v", snap)
	}
	if snap.Balance != 1005 || snap.MarginUsed != 50 || snap.AvailableUSDT != 950 {
		t.Fatalf("expected paper balance/free/margin to match terminal model, got balance=%.2f available=%.2f margin=%.2f", snap.Balance, snap.AvailableUSDT, snap.MarginUsed)
	}
	if snap.OpenPnL != 1.5 || snap.Equity != 1006.5 {
		t.Fatalf("expected paper equity to use mark fallback open pnl, got openPnL=%.4f equity=%.4f", snap.OpenPnL, snap.Equity)
	}
	if snap.OpenPositions[0].Symbol != "BTCUSDT" || snap.OpenPositions[0].Mode != "paper" {
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
	snap := buildLivePaperSnapshot(runtimeModeLive, paper, map[string]symbolMeta{}, nil, 5)
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

func TestPaperMarkResolutionNormalizesSymbolAndFallsBack(t *testing.T) {
	pos := &paperPosition{Symbol: "IN-USD", Side: "BUY", Entry: 0.070, Qty: 10}
	meta := map[string]symbolMeta{"INUSDT": {LastPrice: 0.074}}
	res := resolvePaperPositionMark(pos, meta, nil)
	if res.Mark != 0.074 || res.Source != "meta_last" {
		t.Fatalf("expected normalized meta mark, got %+v", res)
	}

	pos = &paperPosition{Symbol: "IN/USDT", Side: "BUY", Entry: 0.070, Qty: 10, LastMark: 0.073, LastMarkSource: "meta_last"}
	res = resolvePaperPositionMark(pos, map[string]symbolMeta{}, nil)
	if res.Mark != 0.073 || res.Source != "meta_last" {
		t.Fatalf("expected last known mark fallback, got %+v", res)
	}

	pos = &paperPosition{Symbol: "INUSDT", Side: "BUY", Entry: 0.070, Qty: 10}
	res = resolvePaperPositionMark(pos, map[string]symbolMeta{}, nil)
	if res.Mark != 0.070 || res.Source != "entry_price" {
		t.Fatalf("expected entry price fallback, got %+v", res)
	}
}

func TestPaperTradeUpdateMessageDoesNotDisplayZeroMarkOnSymbolMismatch(t *testing.T) {
	now := time.Now().UTC()
	paper := &paperTrader{
		enabled:   true,
		balance:   1000,
		reportLoc: time.UTC,
		maxOpen:   5,
		positions: map[string]*paperPosition{
			"IN-USD": {
				Symbol:      "IN-USD",
				Side:        "BUY",
				Entry:       0.070,
				Qty:         100,
				Margin:      5,
				Leverage:    2,
				OpenedAt:    now.Add(-time.Minute),
				EntryReason: "impulsive_long_starter",
			},
		},
		dayStats: map[string]*paperDayStats{now.Format("2006-01-02"): {Net: 0}},
	}
	msg := paper.TradeUpdateMessage(map[string]symbolMeta{"INUSDT": {LastPrice: 0.074}}, 5)
	if strings.Contains(msg, "0.000000") || strings.Contains(msg, "-100.00%") {
		t.Fatalf("expected resolved mark in paper update, got:\n%s", msg)
	}
	if !strings.Contains(msg, "0.074000") {
		t.Fatalf("expected normalized mark in paper update, got:\n%s", msg)
	}
}

func TestEnrichPaperRuntimeStatusAddsModeAndPayloadToSparseStatus(t *testing.T) {
	now := time.Now().UTC()
	paper := &paperTrader{
		enabled:   true,
		balance:   1000,
		reportLoc: time.UTC,
		positions: map[string]*paperPosition{},
		dayStats: map[string]*paperDayStats{
			now.Format("2006-01-02"): {Net: 0},
		},
	}
	st := liveStatus{
		Generated:   now,
		DryRun:      true,
		LiveEnabled: false,
	}

	enrichPaperRuntimeStatus(&st, runtimeModePaper, paper, map[string]symbolMeta{}, nil)

	if st.Mode != "paper" || st.ModeState != "paper_enabled" {
		t.Fatalf("expected paper mode/status, got mode=%q state=%q", st.Mode, st.ModeState)
	}
	if !st.DryRun || st.LiveEnabled {
		t.Fatalf("expected paper-safe runtime flags, got dry_run=%v live_enabled=%v", st.DryRun, st.LiveEnabled)
	}
	if st.Paper == nil {
		t.Fatal("expected paper payload")
	}
	if st.Paper.OpenPositions == nil || st.Paper.RecentClosed == nil || st.Paper.RecentDecisions == nil {
		t.Fatalf("expected safe paper arrays, got %+v", st.Paper)
	}
}

func TestLiveStatusStoreSnapshotClonesPaperSlices(t *testing.T) {
	store := newLiveStatusStore()
	store.Set(liveStatus{
		Mode:   "paper",
		DryRun: true,
		Paper: &livePaperSnapshot{
			Mode:          "paper",
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

func TestStatusMuxReturnsQuicklyInPaperModes(t *testing.T) {
	cases := []liveStatus{
		{
			Mode:        "paper",
			ModeState:   "scanner_only",
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
			Mode:        "paper",
			ModeState:   "paper_enabled",
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
		Mode:        "paper",
		ModeState:   "paper_enabled",
		DryRun:      true,
		LiveEnabled: false,
		Paper: &livePaperSnapshot{
			Mode:            "paper",
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
		if payload.Mode != "paper" {
			t.Fatalf("unexpected payload on iteration %d: %+v", i, payload)
		}
	}
}
