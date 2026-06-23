package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestRuntimeModeControllerDefaultsToPaperWhenDryRun(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_MODE", "")
	ctrl := newRuntimeModeController(true, false, true)
	if got := ctrl.operatingMode(); got != runtimeModePaper {
		t.Fatalf("expected paper, got %s", got)
	}
}

func TestRuntimeModeControllerEnablesPaperModeWhenExplicit(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_MODE", "paper")
	ctrl := newRuntimeModeController(true, false, true)
	if got := ctrl.operatingMode(); got != runtimeModePaper {
		t.Fatalf("expected paper, got %s", got)
	}
	ctrl = newRuntimeModeController(true, false, false)
	if got := ctrl.operatingMode(); got != runtimeModeLive {
		t.Fatalf("expected live fallback when paper is not enabled, got %s", got)
	}
}

func TestRuntimeModeControllerTreatsLiveAliasesAsLive(t *testing.T) {
	for _, raw := range []string{"live", "live_auto", "manual_only"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("LIVE_RUNTIME_MODE", raw)
			ctrl := newRuntimeModeController(false, true, false)
			if got := ctrl.operatingMode(); got != runtimeModeLive {
				t.Fatalf("expected live, got %s", got)
			}
		})
	}
}

func TestDispatchPaperDecisionLiveSkipsPaperDispatch(t *testing.T) {
	decision, cand := testPaperDecision()
	paperCalls := 0
	liveCalls := 0
	out := dispatchPaperDecision(runtimeModeLive, time.Now().UTC(), decision, cand, 2, 50, 3, nil, nil, nil, paperDispatchHooks{
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
		t.Fatalf("expected no paper dispatch in live mode, got %+v", out)
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

func TestBuildEntryQualityAccumulatorAllowsPenaltyOnlyCandidate(t *testing.T) {
	t.Setenv("LIVE_META_MIN_QUALITY", "0.52")
	t.Setenv("LIVE_META_MIN_QUALITY_CONT", "0.52")
	cand := candidate{
		Strat:           "continuation_fast",
		Side:            "BUY",
		CombinedScore:   0.72,
		Conf:            0.72,
		ClosedBreakHold: true,
		Entry: inplay.Entry{
			ScoreSlope: 0.20,
		},
	}
	quality := buildEntryQualityAccumulator(cand, []string{"avoid_chase"})
	if len(quality.HardBlockReasons) != 0 {
		t.Fatalf("expected no hard blocks, got %+v", quality.HardBlockReasons)
	}
	if !containsString(quality.QualityFlags, "avoid_chase") {
		t.Fatalf("expected avoid_chase quality flag, got %+v", quality.QualityFlags)
	}
	if quality.ScoreAfterPenalties < quality.MinScore {
		t.Fatalf("expected penalties to keep candidate above min score, got after=%.2f min=%.2f", quality.ScoreAfterPenalties, quality.MinScore)
	}
	if quality.BlockReason != "" {
		t.Fatalf("expected no quality block reason, got %q", quality.BlockReason)
	}
}

func TestBuildEntryQualityAccumulatorBlocksWhenPenaltyDropsBelowMinScore(t *testing.T) {
	t.Setenv("LIVE_META_MIN_QUALITY", "0.52")
	t.Setenv("LIVE_META_MIN_QUALITY_CONT", "0.52")
	cand := candidate{
		Strat:         "continuation_fast",
		Side:          "BUY",
		CombinedScore: 0.58,
		Conf:          0.58,
		Entry: inplay.Entry{
			EntryStyle: "avoid_chase",
			ScoreSlope: -0.01,
		},
	}
	quality := buildEntryQualityAccumulator(cand, []string{"late_chase_rapid_expansion"})
	if len(quality.HardBlockReasons) != 0 {
		t.Fatalf("expected no hard blocks for quality-only reject, got %+v", quality.HardBlockReasons)
	}
	if quality.BlockReason != "quality_score_too_low" {
		t.Fatalf("expected quality_score_too_low, got %q", quality.BlockReason)
	}
	if quality.ScoreAfterPenalties >= quality.MinScore {
		t.Fatalf("expected penalized score below min, got after=%.2f min=%.2f", quality.ScoreAfterPenalties, quality.MinScore)
	}
}

func TestBuildEntryQualityAccumulatorPreservesHardSafetyBlock(t *testing.T) {
	cand := candidate{
		Strat:         "continuation_fast",
		Side:          "BUY",
		CombinedScore: 0.90,
		Conf:          0.90,
	}
	quality := buildEntryQualityAccumulator(cand, []string{"pending_add_order"})
	if len(quality.HardBlockReasons) != 1 || quality.HardBlockReasons[0] != "pending_add_order" {
		t.Fatalf("expected hard pending_add_order block, got %+v", quality.HardBlockReasons)
	}
	if quality.BlockReason != "hard_safety_block" {
		t.Fatalf("expected hard_safety_block, got %q", quality.BlockReason)
	}
}

func TestBuildEntryQualityAccumulatorRewardsAlignedLongContinuation(t *testing.T) {
	t.Setenv("LIVE_META_MIN_QUALITY", "0.52")
	cand := candidate{
		Strat:         "continuation_fast",
		Side:          "BUY",
		CombinedScore: 0.50,
		Conf:          0.50,
		DayUTC24h:     12,
		UTC4hPct:      4,
		UTC1hPct:      1.5,
		Entry: inplay.Entry{
			ScoreSlope: 0.10,
		},
	}
	if got := runtimeMinQualityForCandidate(cand); got >= 0.52 {
		t.Fatalf("expected aligned long relief below base quality, got %.2f", got)
	}
}

func TestBuildEntryQualityAccumulatorBlocksMaturePullbackLongWithoutReclaim(t *testing.T) {
	cand := candidate{
		Strat:         "continuation_fast",
		Side:          "BUY",
		CombinedScore: 0.82,
		Conf:          0.82,
		DayUTC24h:     68,
		UTC4hPct:      6,
		UTC1hPct:      -4.5,
		Entry: inplay.Entry{
			EntryStyle: "pullback_long",
			ScoreSlope: 0.08,
		},
	}
	quality := buildEntryQualityAccumulator(cand, nil)
	if !containsString(quality.HardBlockReasons, "mature_pullback_long_needs_reclaim") {
		t.Fatalf("expected mature pullback block, got %+v", quality.HardBlockReasons)
	}
}

func TestBuildEntryQualityAccumulatorAllowsMaturePullbackLongAfterReclaim(t *testing.T) {
	cand := candidate{
		Strat:         "continuation_fast",
		Side:          "BUY",
		CombinedScore: 0.82,
		Conf:          0.82,
		DayUTC24h:     68,
		UTC4hPct:      6,
		UTC1hPct:      -4.5,
		ReclaimHold:   true,
		Entry: inplay.Entry{
			EntryStyle: "pullback_long",
			ScoreSlope: 0.08,
		},
	}
	quality := buildEntryQualityAccumulator(cand, nil)
	if containsString(quality.HardBlockReasons, "mature_pullback_long_needs_reclaim") {
		t.Fatalf("expected reclaim-confirmed pullback to avoid hard block, got %+v", quality.HardBlockReasons)
	}
}

func TestBuildEntryQualityAccumulatorBlocksAllRedShortChaseWithoutBounceFailure(t *testing.T) {
	cand := candidate{
		Strat:         "continuation_fast",
		Side:          "SELL",
		CombinedScore: 0.86,
		Conf:          0.86,
		DayUTC24h:     -30,
		UTC4hPct:      -12,
		UTC1hPct:      -4,
		Entry: inplay.Entry{
			State:      inplay.StatePumping,
			ScoreSlope: 0.07,
		},
	}
	quality := buildEntryQualityAccumulator(cand, nil)
	if !containsString(quality.HardBlockReasons, "short_block_late_chase") {
		t.Fatalf("expected all-red short chase block, got %+v", quality.HardBlockReasons)
	}
}

func TestBuildEntryQualityAccumulatorAllowsAllRedShortAfterBounceFailure(t *testing.T) {
	cand := candidate{
		Strat:         "continuation_fast",
		Side:          "SELL",
		CombinedScore: 0.86,
		Conf:          0.86,
		DayUTC24h:     -30,
		UTC4hPct:      -12,
		UTC1hPct:      -4,
		Entry: inplay.Entry{
			DrawupFromTroughPct: 2.5,
			FailedBounceCount:   1,
			State:               inplay.StateCooling,
			ScoreSlope:          -0.05,
		},
	}
	quality := buildEntryQualityAccumulator(cand, nil)
	if containsString(quality.HardBlockReasons, "short_block_late_chase") {
		t.Fatalf("expected bounce-failure short to avoid hard block, got %+v", quality.HardBlockReasons)
	}
}

func TestShortPhase2LateChaseShortBlocked(t *testing.T) {
	cand := candidate{
		Side:      "SELL",
		DayUTC24h: -30,
		UTC4hPct:  -12,
		UTC1hPct:  -5,
	}
	ctx := shortPhase2ContextForCandidate(cand)
	if ctx.Bucket != "late_chase_short" || ctx.DirectShortAllowed {
		t.Fatalf("expected late chase short blocked, got %+v", ctx)
	}
	if ctx.RequireConfirmation != "failed_bounce" {
		t.Fatalf("expected failed_bounce confirmation, got %+v", ctx)
	}
}

func TestShortPhase2FreshBreakdownShortAllowed(t *testing.T) {
	cand := candidate{
		Side:      "SELL",
		DayUTC24h: -8,
		UTC4hPct:  -3,
		UTC1hPct:  -1.5,
	}
	ctx := shortPhase2ContextForCandidate(cand)
	if ctx.Bucket != "fresh_breakdown_short" || !ctx.DirectShortAllowed {
		t.Fatalf("expected fresh breakdown short allowed, got %+v", ctx)
	}
}

func TestShortPhase2PostPumpBreakdownBlocked(t *testing.T) {
	cand := candidate{
		Side:      "SELL",
		DayUTC24h: 100,
		UTC4hPct:  -35,
		UTC1hPct:  -12,
	}
	ctx := shortPhase2ContextForCandidate(cand)
	if ctx.Bucket != "post_pump_breakdown" || ctx.DirectShortAllowed {
		t.Fatalf("expected post-pump breakdown blocked, got %+v", ctx)
	}
	if ctx.RequireConfirmation != "failed_bounce" {
		t.Fatalf("expected failed_bounce requirement, got %+v", ctx)
	}
}

func TestShortPhase2PostPumpFreshBreakdownNeedsStructureBreak(t *testing.T) {
	cand := candidate{
		Side:      "SELL",
		DayUTC24h: 100,
		UTC4hPct:  -8,
		UTC1hPct:  -4,
	}
	ctx := shortPhase2ContextForCandidate(cand)
	if ctx.Bucket != "post_pump_fresh_breakdown" || ctx.DirectShortAllowed {
		t.Fatalf("expected post-pump fresh breakdown to wait for structure, got %+v", ctx)
	}

	cand.SessionVWAP = 100
	cand.LastClose = 98
	ctx = shortPhase2ContextForCandidate(cand)
	if ctx.Bucket != "post_pump_fresh_breakdown" || !ctx.DirectShortAllowed {
		t.Fatalf("expected post-pump fresh breakdown to allow after structure loss, got %+v", ctx)
	}
}

func TestShortPhase2LateChaseBecomesFailedBounceShort(t *testing.T) {
	cand := candidate{
		Side:      "SELL",
		DayUTC24h: -30,
		UTC4hPct:  -12,
		UTC1hPct:  -5,
		Entry: inplay.Entry{
			DrawupFromTroughPct: 2.5,
			FailedBounceCount:   1,
			State:               inplay.StateCooling,
			ScoreSlope:          -0.05,
		},
	}
	ctx := shortPhase2ContextForCandidate(cand)
	if ctx.Bucket != "failed_bounce_short" || !ctx.DirectShortAllowed || !ctx.FailedBounceConfirmed {
		t.Fatalf("expected late chase to convert to failed bounce short, got %+v", ctx)
	}
}

func TestShortPhase2PostPumpBreakdownBecomesFailedBounceShort(t *testing.T) {
	cand := candidate{
		Side:      "SELL",
		DayUTC24h: 100,
		UTC4hPct:  -35,
		UTC1hPct:  -12,
		Entry: inplay.Entry{
			DrawupFromTroughPct: 2.8,
			FailedReclaimCount:  1,
			State:               inplay.StateDumping,
			ScoreSlope:          -0.08,
		},
	}
	ctx := shortPhase2ContextForCandidate(cand)
	if ctx.Bucket != "failed_bounce_short" || !ctx.DirectShortAllowed || !ctx.FailedBounceConfirmed {
		t.Fatalf("expected post-pump breakdown to convert to failed bounce short, got %+v", ctx)
	}
}

func TestPaperPreflightRejectsUnresolvedStrategyLabels(t *testing.T) {
	for _, strat := range []string{"none", "", "no_strategy", "unknown", "unresolved"} {
		t.Run(stratLabel(strat), func(t *testing.T) {
			ctx := testPaperDecisionCtx()
			ctx.Candidate.Strat = strat
			verdict := paperPreflightVerdict(ctx)
			if verdict.Approved {
				t.Fatalf("expected unresolved strategy %q to be rejected", strat)
			}
			if verdict.Reason != "strategy_unresolved" {
				t.Fatalf("expected strategy_unresolved reason, got %q", verdict.Reason)
			}
			if verdict.Quality.BlockReason != "hard_safety_block" {
				t.Fatalf("expected hard_safety_block, got %q", verdict.Quality.BlockReason)
			}
			if !containsString(verdict.Quality.HardBlockReasons, "strategy_unresolved") {
				t.Fatalf("expected hard block reasons to include strategy_unresolved, got %+v", verdict.Quality.HardBlockReasons)
			}
		})
	}
}

func TestPaperPreflightCarriesUnresolvedSourceForFallbackCandidate(t *testing.T) {
	ctx := testPaperDecisionCtx()
	ctx.Candidate = applySimpleContinuationFallbackAt(candidate{
		Side:  "BUY",
		Strat: "",
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			EntryStyle:   "none",
			CurrentScore: 78,
			ScoreSlope:   0.02,
			State:        inplay.StateBalanced,
		},
	}, time.Now().UTC())
	verdict := paperPreflightVerdict(ctx)
	if !containsString(verdict.Quality.HardBlockReasons, "strategy_unresolved_source:continuation_fallback_unmapped") {
		t.Fatalf("expected unresolved source tag, got %+v", verdict.Quality.HardBlockReasons)
	}
}

func TestPaperPreflightAllowsExecutableStrategy(t *testing.T) {
	ctx := testPaperDecisionCtx()
	ctx.Candidate.Strat = "vp_trend"
	verdict := paperPreflightVerdict(ctx)
	if !verdict.Approved {
		t.Fatalf("expected executable strategy to pass, got reason=%q quality=%+v", verdict.Reason, verdict.Quality)
	}
}

func TestPaperMaybeEnterAppliesPostPumpFreshShortSizeMultiplier(t *testing.T) {
	paper := testPaperRuntimePaper()
	now := time.Now().UTC()
	cand := candidate{
		Side:        "SELL",
		Strat:       "vp_trend",
		DayUTC24h:   100,
		UTC4hPct:    -8,
		UTC1hPct:    -4,
		SessionVWAP: 100,
		LastClose:   98,
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 95,
			ScoreSlope:   -0.10,
			State:        inplay.StateCooling,
		},
	}
	pos, err := paper.MaybeEnter(now, cand, 0, 100, 5, map[string]symbolMeta{"BTCUSDT": {LastPrice: 98}}, map[string]aster.OrderBook{}, map[string]inplay.Entry{})
	if err != nil {
		t.Fatalf("MaybeEnter error: %v", err)
	}
	if pos == nil {
		t.Fatalf("expected paper position")
	}
	if pos.Margin != 75 {
		t.Fatalf("expected 0.75 size multiplier margin, got %.2f", pos.Margin)
	}
	if pos.ShortBucket != "post_pump_fresh_breakdown" || !pos.DirectShortAllowed {
		t.Fatalf("expected short bucket frozen on position, got %+v", pos)
	}
}

func TestDispatchPaperDecisionUnresolvedDoesNotCallMaybeEnter(t *testing.T) {
	decision, cand := testPaperDecision()
	decision.Approved = false
	decision.RejectReason = "strategy_unresolved"
	decision.Quality = strategies.EntryQualityAccumulator{
		HardBlockReasons: []string{"strategy_unresolved"},
		BlockReason:      "hard_safety_block",
	}
	paperCalls := 0
	out := dispatchPaperDecision(runtimeModePaper, time.Now().UTC(), decision, cand, 2, 50, 3, nil, nil, nil, paperDispatchHooks{
		Paper: func(time.Time, candidate, float64, float64, int, map[string]symbolMeta, map[string]aster.OrderBook, map[string]inplay.Entry) (*paperPosition, error) {
			paperCalls++
			return &paperPosition{}, nil
		},
	})
	if paperCalls != 0 {
		t.Fatalf("expected unresolved candidate not to call MaybeEnter, got %d calls", paperCalls)
	}
	if out.RejectReason != "strategy_unresolved" {
		t.Fatalf("expected strategy_unresolved reject, got %+v", out)
	}
}

func TestPaperLogDecisionIncludesQualityTelemetryForRejects(t *testing.T) {
	decision, cand := testPaperDecision()
	decision.Approved = false
	decision.RejectReason = "quality_score_too_low"
	decision.Quality = strategies.EntryQualityAccumulator{
		QualityFlags:        []string{"avoid_chase", "weak_slope"},
		PenaltyTotal:        0.16,
		ScoreBefore:         0.58,
		ScoreAfterPenalties: 0.42,
		MinScore:            0.52,
		BlockReason:         "quality_score_too_low",
	}
	output := capturePaperRuntimeStdout(t, func() {
		paperLogDecision(cand, decision, paperDispatchResult{})
	})
	for _, want := range []string{
		"unresolved_source=n/a",
		"quality_flags=avoid_chase|weak_slope",
		"penalty_total=0.16",
		"score_before=0.58",
		"score_after_penalties=0.42",
		"min_score=0.52",
		"hard_block_reasons=",
		"block_reason=quality_score_too_low",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, output)
		}
	}
}

func TestPaperLogDecisionIncludesUnresolvedSourceForStrategyRejects(t *testing.T) {
	decision, cand := testPaperDecision()
	decision.Approved = false
	decision.RejectReason = "strategy_unresolved"
	decision.Quality = strategies.EntryQualityAccumulator{
		HardBlockReasons: []string{"strategy_unresolved", "strategy_unresolved_source:feature_bars_insufficient"},
		BlockReason:      "hard_safety_block",
	}
	cand.Strat = ""
	cand.RejectReason = withUnresolvedSource("", "feature_bars_insufficient")
	output := capturePaperRuntimeStdout(t, func() {
		paperLogDecision(cand, decision, paperDispatchResult{})
	})
	if !strings.Contains(output, "unresolved_source=feature_bars_insufficient") {
		t.Fatalf("expected unresolved source in log output, got %q", output)
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

func testPaperDecisionCtx() paperDecisionCtx {
	_, cand := testPaperDecision()
	return paperDecisionCtx{
		Now:           time.Now().UTC(),
		LocalMaintNow: time.Now(),
		Candidate:     cand,
		MetaBySymbol: map[string]symbolMeta{
			"BTCUSDT": {LastPrice: 100},
		},
		EntryDepth:          map[string]aster.OrderBook{},
		Paper:               testPaperRuntimePaper(),
		CurrentEntries:      map[string]inplay.Entry{},
		RiskShell:           risk.Config{},
		RiskFallbackStopPct: 1,
		RiskHoldHours:       1,
		LeverageMode:        "fixed",
		LeverageFixed:       3,
		LeverageMin:         1,
		MaxLeverage:         5,
		EffectiveReserve:    0,
		EffectiveMargin:     50,
		AvailableUSDT:       1000,
		OBFilterEnable:      false,
		OBLevels:            5,
		OBImbMin:            0,
		OBMaxSpreadBps:      1000,
		MaxOpenPos:          5,
		MaxOpenPerSide:      5,
	}
}

func testPaperRuntimePaper() *paperTrader {
	return &paperTrader{
		enabled:          true,
		balance:          1000,
		maxOpen:          5,
		positions:        map[string]*paperPosition{},
		dayStats:         map[string]*paperDayStats{},
		lastFundKey:      map[string]string{},
		lastExitAt:       map[string]time.Time{},
		lastExitLoss:     map[string]bool{},
		lastHarvestAt:    map[string]time.Time{},
		symbolTradeDay:   map[string]string{},
		symbolTradeCount: map[string]int{},
		reportLoc:        time.UTC,
	}
}

func stratLabel(strat string) string {
	if strings.TrimSpace(strat) == "" {
		return "empty"
	}
	return strat
}

func capturePaperRuntimeStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe error: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = orig
	}()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("Copy error: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Read close error: %v", err)
	}
	return buf.String()
}

func TestWeakSlopeEliteContinuationGetsGrace(t *testing.T) {
	t.Setenv("LIVE_CONT_FAST_MIN_SLOPE", "0.02")
	t.Setenv("LIVE_WEAK_SLOPE_ELITE_SCORE_MIN", "95")
	t.Setenv("LIVE_WEAK_SLOPE_ELITE_THRESHOLD_FRAC", "0.5")

	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A+",
			CurrentScore: 98,
			ScoreSlope:   0.015,
			State:        inplay.StateHeating,
			Momentum:     true,
		},
		Side:          "BUY",
		Strat:         "vp_trend",
		CombinedScore: 0.64,
		Conf:          0.64,
	}

	if weakSlopeForCandidate(cand) {
		t.Fatalf("expected elite continuation candidate to avoid weak_slope flag")
	}

	quality := buildEntryQualityAccumulator(cand, nil)
	if containsString(quality.QualityFlags, "weak_slope") {
		t.Fatalf("expected no weak_slope penalty for elite continuation, got %+v", quality.QualityFlags)
	}
}

func TestWeakSlopePenaltyReducedForNormalCandidate(t *testing.T) {
	t.Setenv("LIVE_WEAK_SLOPE_PENALTY", "0.03")

	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 90,
			ScoreSlope:   0.00,
			State:        inplay.StateInPlay,
			Momentum:     true,
		},
		Side:          "BUY",
		Strat:         "vp_trend",
		CombinedScore: 0.60,
		Conf:          0.60,
		LastClose:     100,
		SessionVWAP:   100,
		EMA9:          100,
	}

	base := buildEntryQualityAccumulator(cand, nil)
	quality := buildEntryQualityAccumulator(cand, []string{"weak_slope"})
	if !containsString(quality.QualityFlags, "weak_slope") {
		t.Fatalf("expected weak_slope flag, got %+v", quality.QualityFlags)
	}
	if delta := quality.PenaltyTotal - base.PenaltyTotal; delta != 0.03 {
		t.Fatalf("expected reduced weak_slope penalty increment 0.03, got %.2f (base=%.2f total=%.2f)", delta, base.PenaltyTotal, quality.PenaltyTotal)
	}
}

func TestMinorExtensionEliteContinuationGetsLighterPenalty(t *testing.T) {
	t.Setenv("LIVE_MINOR_EXTENSION_PENALTY", "0.03")
	t.Setenv("LIVE_MINOR_EXTENSION_ELITE_SCORE_MIN", "96")
	t.Setenv("LIVE_MINOR_EXTENSION_ELITE_PENALTY_FRAC", "0.5")
	t.Setenv("LIVE_ADD_MAX_EXTENSION_ATR", "1.35")

	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A+",
			CurrentScore: 98,
			ScoreSlope:   0.18,
			State:        inplay.StateInPlay,
			Momentum:     true,
		},
		Side:            "BUY",
		Strat:           "continuation_fast",
		CombinedScore:   0.68,
		Conf:            0.68,
		ExtensionATR:    1.50,
		ClosedBreakHold: true,
		LastClose:       100,
		SessionVWAP:     100,
		EMA9:            100,
	}

	baseCand := cand
	baseCand.ExtensionATR = 0.50
	base := buildEntryQualityAccumulator(baseCand, nil)
	quality := buildEntryQualityAccumulator(cand, nil)
	if !containsString(quality.QualityFlags, "minor_extension") {
		t.Fatalf("expected minor_extension flag, got %+v", quality.QualityFlags)
	}
	if delta := quality.PenaltyTotal - base.PenaltyTotal; !(delta > 0 && delta < 0.06) {
		t.Fatalf("expected elite extension penalty increment below legacy 0.06, got %.3f (base=%.3f total=%.3f)", delta, base.PenaltyTotal, quality.PenaltyTotal)
	}
}

func TestMinorExtensionPenaltyReducedForNormalCandidate(t *testing.T) {
	t.Setenv("LIVE_MINOR_EXTENSION_PENALTY", "0.03")

	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 90,
			ScoreSlope:   0.10,
			State:        inplay.StateInPlay,
		},
		Side:            "BUY",
		Strat:           "continuation_fast",
		CombinedScore:   0.60,
		Conf:            0.60,
		ClosedBreakHold: true,
		LastClose:       100,
		SessionVWAP:     100,
		EMA9:            100,
	}

	base := buildEntryQualityAccumulator(cand, nil)
	quality := buildEntryQualityAccumulator(cand, []string{"late_extension_no_reset"})
	if !containsString(quality.QualityFlags, "minor_extension") {
		t.Fatalf("expected minor_extension flag, got %+v", quality.QualityFlags)
	}
	if delta := quality.PenaltyTotal - base.PenaltyTotal; !(delta > 0 && delta < 0.08) {
		t.Fatalf("expected reduced extension penalty increment below legacy 0.08, got %.2f (base=%.2f total=%.2f)", delta, base.PenaltyTotal, quality.PenaltyTotal)
	}
}
