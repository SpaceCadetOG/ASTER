package main

import (
	"path/filepath"
	"testing"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/features"
	flowfeed "go-machine/internal/flow"
	"go-machine/internal/inplay"
	"go-machine/internal/risk"
	"go-machine/internal/stats"
	"go-machine/internal/strategies"
)

func TestRuntimeModeControllerDefaultsToManualOnly(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_MODE", "")
	ctrl := newRuntimeModeController(true, false, true)
	if got := ctrl.operatingMode(); got != runtimeModeManualOnly {
		t.Fatalf("expected manual_only, got %s", got)
	}
}

func TestRuntimeModeControllerEnablesPaperModeWhenExplicit(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_MODE", "paper")
	ctrl := newRuntimeModeController(true, false, true)
	if got := ctrl.operatingMode(); got != runtimeModePaper {
		t.Fatalf("expected paper, got %s", got)
	}
	ctrl = newRuntimeModeController(true, false, false)
	if got := ctrl.operatingMode(); got != runtimeModeManualOnly {
		t.Fatalf("expected manual_only when paper is not enabled, got %s", got)
	}
}

func TestDispatchPaperDecisionManualOnlySkipsDispatch(t *testing.T) {
	decision, cand := testPaperDecision()
	paperCalls := 0
	liveCalls := 0
	out := dispatchPaperDecision(runtimeModeManualOnly, time.Now().UTC(), decision, cand, 2, 50, 3, nil, nil, nil, paperDispatchHooks{
		Paper: func(time.Time, candidate, float64, float64, int, map[string]symbolMeta, map[string]aster.OrderBook, map[string]inplay.Entry) (*paperPosition, error) {
			paperCalls++
			return &paperPosition{}, nil
		},
		Live: func() error {
			liveCalls++
			return nil
		},
	})
	if out.Attempted || out.Entered {
		t.Fatalf("expected no dispatch in manual_only, got %+v", out)
	}
	if paperCalls != 0 || liveCalls != 0 {
		t.Fatalf("expected no paper/live calls, got paper=%d live=%d", paperCalls, liveCalls)
	}
}

func TestDispatchPaperDecisionApprovedUsesPaperOnly(t *testing.T) {
	decision, cand := testPaperDecision()
	paperCalls := 0
	liveCalls := 0
	out := dispatchPaperDecision(runtimeModePaper, time.Now().UTC(), decision, cand, 2, 50, 3, nil, nil, nil, paperDispatchHooks{
		Paper: func(time.Time, candidate, float64, float64, int, map[string]symbolMeta, map[string]aster.OrderBook, map[string]inplay.Entry) (*paperPosition, error) {
			paperCalls++
			return &paperPosition{Symbol: "BTCUSDT"}, nil
		},
		Live: func() error {
			liveCalls++
			return nil
		},
	})
	if !out.Attempted || !out.Entered {
		t.Fatalf("expected entered paper dispatch, got %+v", out)
	}
	if paperCalls != 1 {
		t.Fatalf("expected one paper call, got %d", paperCalls)
	}
	if liveCalls != 0 {
		t.Fatalf("expected no live calls, got %d", liveCalls)
	}
}

func TestDispatchPaperDecisionRejectedSkipsEntry(t *testing.T) {
	decision, cand := testPaperDecision()
	decision.Approved = false
	decision.RejectReason = "risk_reject"
	paperCalls := 0
	out := dispatchPaperDecision(runtimeModePaper, time.Now().UTC(), decision, cand, 2, 50, 3, nil, nil, nil, paperDispatchHooks{
		Paper: func(time.Time, candidate, float64, float64, int, map[string]symbolMeta, map[string]aster.OrderBook, map[string]inplay.Entry) (*paperPosition, error) {
			paperCalls++
			return &paperPosition{}, nil
		},
	})
	if out.Attempted || out.Entered {
		t.Fatalf("expected no entry for rejected decision, got %+v", out)
	}
	if paperCalls != 0 {
		t.Fatalf("expected no paper dispatch, got %d calls", paperCalls)
	}
}

func TestApplyPaperDecisionStatusReportsState(t *testing.T) {
	decision, _ := testPaperDecision()
	st := liveStatus{}
	applyPaperDecisionStatus(&st, decision, paperDispatchResult{Attempted: true, Entered: true})
	if st.Mode != "paper" || st.ModeState != "paper_entered" {
		t.Fatalf("unexpected status: %+v", st)
	}
}

func TestRunPaperLifecycleClosesPositionAndRetainsPaperOnly(t *testing.T) {
	paper := &paperTrader{
		enabled:        true,
		positions:      map[string]*paperPosition{},
		dayStats:       map[string]*paperDayStats{},
		lastFundKey:    map[string]string{},
		reportLoc:      time.UTC,
		stopTriggerRef: "mark",
		tpTriggerRef:   "mark",
	}
	paper.positions["BTCUSDT"] = &paperPosition{
		Symbol:     "BTCUSDT",
		Side:       "BUY",
		Entry:      100,
		Qty:        1,
		InitialQty: 1,
		Stop:       99,
		TP1:        101,
		TP2:        102,
		TP3:        103,
		OpenedAt:   time.Now().UTC().Add(-10 * time.Minute),
	}
	now := time.Now().UTC()
	meta := map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 98.5},
	}
	runPaperLifecycle(now, paper, meta, map[string]aster.OrderBook{}, map[string]inplay.Entry{}, map[string]inplay.Entry{}, map[string]momentumView{}, map[string]flowMetrics{}, map[string]flowfeed.ExternalSignal{})
	if len(paper.positions) != 0 {
		t.Fatalf("expected paper position closed by lifecycle check")
	}
}

func TestEmitPaperDecisionEventCapturesRejectTelemetry(t *testing.T) {
	decision, cand := testPaperDecision()
	decision.Approved = false
	decision.RejectReason = "risk_reject"
	decision.Rejects = []string{"risk_reject", "paper_block"}
	logPath := filepath.Join(t.TempDir(), "events.jsonl")
	logger := stats.NewEventLogger(logPath, true, false, true)
	now := time.Now().UTC()
	emitPaperDecisionEvent(logger, now, cand, decision)
	events, err := stats.LoadEvents(logPath, nil, nil)
	if err != nil {
		t.Fatalf("LoadEvents error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if events[0].Type != "ENTRY_DECISION" || events[0].Reason != "risk_reject" {
		t.Fatalf("unexpected event: %+v", events[0])
	}
	if events[0].GateAllow == nil || *events[0].GateAllow {
		t.Fatalf("expected reject gate telemetry: %+v", events[0])
	}
}

func TestEmitPaperPositionOpenEventCarriesDecisionMetadata(t *testing.T) {
	decision, cand := testPaperDecision()
	pos := &paperPosition{
		Symbol:           "BTCUSDT",
		Side:             "BUY",
		Entry:            100,
		Qty:              1,
		InitialQty:       1,
		Margin:           50,
		Leverage:         3,
		EntryReason:      cand.Strat,
		EntryStrategyID:  cand.Strat,
		EntrySetupFamily: "reversal",
		EntryGrade:       "A",
		EntryState:       inplay.StateInPlay,
		EntryTrigger:     "ready",
		DiscoveryScore:   0.8,
		TriggerScore:     0.7,
		ExecutionScore:   0.75,
		CombinedScore:    0.78,
		StopDistancePct:  1.0,
	}
	annotatePaperPositionFromDecision(pos, cand, decision)
	logPath := filepath.Join(t.TempDir(), "events.jsonl")
	logger := stats.NewEventLogger(logPath, true, false, true)
	emitPaperPositionOpenEvent(logger, time.Now().UTC(), cand, pos, decision)
	events, err := stats.LoadEvents(logPath, nil, nil)
	if err != nil {
		t.Fatalf("LoadEvents error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	ev := events[0]
	if ev.Type != "POSITION_OPEN" || ev.Source != "paper" || ev.Mode != "paper" {
		t.Fatalf("unexpected open event: %+v", ev)
	}
	if ev.SetupFamily != "reversal" || ev.ConfluenceScore != decision.Signal.ConfluenceScore.TotalScore {
		t.Fatalf("missing decision metadata: %+v", ev)
	}
}

func TestPaperTradeCloseEventCarriesOutcomeTelemetry(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "events.jsonl")
	logger := stats.NewEventLogger(logPath, true, false, true)
	paper := &paperTrader{
		enabled:        true,
		balance:        10_000,
		tradesCSV:      filepath.Join(t.TempDir(), "paper_trades.csv"),
		eventLog:       logger,
		markLastModel:  "mark",
		stopTriggerRef: "mark",
		tpTriggerRef:   "mark",
	}
	pos := &paperPosition{
		Symbol:               "BTCUSDT",
		Side:                 "BUY",
		Entry:                100,
		Qty:                  1,
		Margin:               50,
		Leverage:             3,
		Stop:                 99,
		EntryMode:            "paper",
		EntryReason:          "exhaustion_flip_long",
		EntrySetupFamily:     "reversal",
		EntryGrade:           "A",
		EntryState:           inplay.StateInPlay,
		EntryTrigger:         "ready",
		EntryConfluenceScore: 0.82,
		EntrySignalReasons:   []string{"absorption", "ema_alignment"},
		EntrySignalSources:   []string{"shared_strategy"},
		OpenedAt:             time.Now().UTC().Add(-12 * time.Minute),
		FirstProtectAt:       time.Now().UTC().Add(-7 * time.Minute),
		MaxFavorableR:        1.3,
		MaxAdverseR:          0.4,
		CaptureRatio:         0.55,
		MaxGivebackR:         0.3,
		DiscoveryScore:       0.9,
		TriggerScore:         0.75,
		ExecutionScore:       0.72,
		CombinedScore:        0.8,
		StopDistancePct:      1.0,
	}
	now := time.Now().UTC()
	if err := paper.logTrade(now, pos, 98, pos.Qty, "SL", -2, 0.1, -2.1, 12, symbolMeta{LastPrice: 98}, aster.OrderBook{}); err != nil {
		t.Fatalf("logTrade error: %v", err)
	}
	events, err := stats.LoadEvents(logPath, nil, nil)
	if err != nil {
		t.Fatalf("LoadEvents error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one close event, got %d", len(events))
	}
	ev := events[0]
	if ev.Type != "POSITION_CLOSE" || ev.Source != "paper" || ev.Mode != "paper" {
		t.Fatalf("unexpected close event identity: %+v", ev)
	}
	if ev.Reason != "SL" || ev.MFER != 1.3 || ev.MAER != 0.4 {
		t.Fatalf("missing outcome metrics: %+v", ev)
	}
	if ev.ProofMin <= 0 || ev.FailureMin != 12 {
		t.Fatalf("missing proof/failure timing: %+v", ev)
	}
}

func testPaperDecision() (strategies.ExecutionDecision, candidate) {
	sig := strategies.Signal{
		Active:     true,
		Name:       "paper_test",
		Side:       features.SideLong,
		Entry:      100,
		Stop:       99,
		TP1:        101,
		TP2:        102,
		TP3:        103,
		Confidence: 0.7,
	}
	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 98,
			ScoreSlope:   0.6,
		},
		Side:           "BUY",
		Strat:          "exhaustion_flip_long",
		Conf:           0.7,
		FinalRank:      12,
		LifecycleStage: "READY",
		TriggerStage:   "armed",
		TriggerState:   "ready",
		Sig:            sig,
	}
	decision := strategies.NewExecutionDecision(
		cand.Entry.Symbol,
		sig,
		risk.Decision{Approved: true},
		strategies.PreflightVerdict{Checked: true, Approved: true, Source: "test"},
		strategies.AdmissionSummary{
			LifecycleStage: cand.LifecycleStage,
			TriggerStage:   cand.TriggerStage,
			TriggerState:   cand.TriggerState,
			CandidateGrade: cand.Entry.CurrentGrade,
			CandidateScore: cand.Entry.CurrentScore,
			FinalRank:      cand.FinalRank,
		},
		"paper_test",
	)
	return decision, cand
}
