package main

import (
	"reflect"
	"testing"
	"time"

	exitmgr "go-machine/internal/execution"
	"go-machine/internal/features"
	"go-machine/internal/inplay"
	"go-machine/internal/risk"
	"go-machine/internal/strategies"
)

func parityTestCandidate() candidate {
	return candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 94,
			ScoreSlope:   0.22,
			State:        inplay.StateInPlay,
			EntryStyle:   "pullback_long",
		},
		Side:                "BUY",
		Strat:               "continuation_fast",
		StrategyID:          "continuation_fast",
		SetupFamily:         "micro_pullback_continuation",
		SetupSource:         "runtime",
		TradeHorizon:        "intraday",
		Conf:                0.81,
		FinalRank:           0.88,
		TriggerState:        "ready",
		ExitProfile:         "trend",
		VolumeUSD:           2_000_000,
		SpreadBps:           2.0,
		DepthBid:            100_000,
		DepthAsk:            100_000,
		BookImbalance:       1.2,
		ATRPct:              1.1,
		ATR:                 1.2,
		SessionVWAP:         100.5,
		EMA9:                100.4,
		CombinedScore:       0.89,
		DiscoveryScore:      0.86,
		TriggerScore:        0.87,
		ExecutionScore:      0.84,
		EntryTiming:         "fresh",
		SessionLabel:        "LONDON",
		CandidateAgeSeconds: 45,
		DistanceToVWAPPct:   0.22,
		Sig: strategies.Signal{
			Active: true,
			Name:   "continuation_fast",
			Side:   features.SideLong,
			Entry:  101,
			Stop:   99,
			TP1:    103,
			TP2:    105,
			TP3:    107,
		},
	}
}

func TestPaperAndLiveSharedDecisionMatch(t *testing.T) {
	cand := parityTestCandidate()
	meta := map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 101, FundingRate: 0.001},
	}
	paperDec := buildPaperExecutionDecision(paperDecisionCtx{
		Candidate:           cand,
		MetaBySymbol:        meta,
		RiskShell:           risk.DefaultConfig(),
		RiskFallbackStopPct: 3,
		RiskHoldHours:       8,
		LeverageMode:        "fixed",
		LeverageFixed:       2,
		LeverageMin:         2,
		MaxLeverage:         5,
		EffectiveMargin:     50,
		Paper:               &paperTrader{enabled: true, positions: map[string]*paperPosition{}},
	})
	liveDec := buildSharedRuntimeDecision(sharedRuntimeDecisionContext{
		Candidate:           cand,
		MetaBySymbol:        meta,
		RiskShell:           risk.DefaultConfig(),
		RiskFallbackStopPct: 3,
		RiskHoldHours:       8,
		LeverageMode:        "fixed",
		LeverageFixed:       2,
		LeverageMin:         2,
		MaxLeverage:         5,
		EffectiveMargin:     50,
		PreflightSource:     "live",
	}).ExecutionDecision
	if paperDec.Approved != liveDec.Approved {
		t.Fatalf("approved mismatch: paper=%v live=%v", paperDec.Approved, liveDec.Approved)
	}
	if paperDec.RejectReason != liveDec.RejectReason {
		t.Fatalf("reject reason mismatch: paper=%q live=%q", paperDec.RejectReason, liveDec.RejectReason)
	}
	if !reflect.DeepEqual(paperDec.Quality, liveDec.Quality) {
		t.Fatalf("quality mismatch: paper=%+v live=%+v", paperDec.Quality, liveDec.Quality)
	}
	if !reflect.DeepEqual(paperDec.Targets, liveDec.Targets) {
		t.Fatalf("target mismatch: paper=%+v live=%+v", paperDec.Targets, liveDec.Targets)
	}
}

func TestPaperAndLiveSharedTradePlanMatch(t *testing.T) {
	cand := parityTestCandidate()
	cfg := sharedTradePlanConfig{
		MinStopPct:    0.5,
		MaxStopPct:    5.0,
		MinTP1RR:      0.5,
		TP1R:          1.0,
		TP2R:          2.0,
		TP3R:          3.0,
		HybridStopCfg: exitmgr.HybridStopConfig{},
		FrontRunner:   exitmgr.NewManager(exitmgr.Config{}),
	}
	paperPlan, err := buildSharedTradePlan(cand, 101, cand.VolumeUSD, cfg)
	if err != nil {
		t.Fatalf("paper plan error: %v", err)
	}
	livePlan, err := buildSharedTradePlan(cand, 101, cand.VolumeUSD, cfg)
	if err != nil {
		t.Fatalf("live plan error: %v", err)
	}
	if !reflect.DeepEqual(paperPlan, livePlan) {
		t.Fatalf("plan mismatch: paper=%+v live=%+v", paperPlan, livePlan)
	}
}

func TestPaperAndLiveManagementDecisionsMatchForSamePricePath(t *testing.T) {
	now := time.Now().UTC()
	manager := exitmgr.NewManager(exitmgr.Config{})
	input := exitmgr.ProtectInput{
		Side:               "BUY",
		Entry:              100,
		Stop:               98,
		Mark:               103.5,
		MFER:               1.75,
		MAER:               0.15,
		BarsHeld:           25,
		StallBars:          1,
		UnrealizedPct:      3.5,
		Sponsored:          true,
		HitTP1:             true,
		HitTP2:             false,
		HitTP3:             false,
		WeakSponsorStreak:  0,
		EntryReason:        "continuation_fast",
		EntryStrategyID:    "continuation_fast",
		AdvancedReady:      true,
		HTFTrendState:      "trend",
		HTFTrendPersistent: true,
		TriggerRef:         "mark",
		ComputedStop:       98,
		SubmittedStop:      98,
		AcceptedStop:       98,
		WinnerLifecycle:    "starter",
	}
	paperDecision := manager.EvaluateProtect(input)
	liveDecision := manager.EvaluateProtect(input)
	if !reflect.DeepEqual(paperDecision, liveDecision) {
		t.Fatalf("protect decision mismatch: paper=%+v live=%+v", paperDecision, liveDecision)
	}

	paperTrail := calcSharedTrailStop(true, 103.5, "hybrid", 1.0, 0.2, 0.7, false, 0.012, 0, sharedTrailContext{
		EntryReason:           "continuation_fast",
		EntryVolumeUSD:        2_000_000,
		ExitProfile:           "trend",
		Sponsored:             true,
		HitTP1:                true,
		LastConfluenceRefresh: now,
	})
	liveTrail := calcSharedTrailStop(true, 103.5, "hybrid", 1.0, 0.2, 0.7, false, 0.012, 0, sharedTrailContext{
		EntryReason:           "continuation_fast",
		EntryVolumeUSD:        2_000_000,
		ExitProfile:           "trend",
		Sponsored:             true,
		HitTP1:                true,
		LastConfluenceRefresh: now,
	})
	if paperTrail != liveTrail {
		t.Fatalf("trail mismatch: paper=%.8f live=%.8f", paperTrail, liveTrail)
	}
}

func TestExecutionAdaptersDifferOnlyAtVenueBoundary(t *testing.T) {
	cand := parityTestCandidate()
	paper := &paperTrader{
		enabled:    true,
		maxOpen:    1,
		positions:  map[string]*paperPosition{},
		minStopPct: 0.5,
		maxStopPct: 5.0,
		minTP1RR:   0.5,
		tp1R:       1.0,
		tp2R:       2.0,
		tp3R:       3.0,
	}
	pos, err := paperExecutionAdapter(paper, time.Now().UTC(), cand, 0, 10, 1, map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 101, VolumeUSD: cand.VolumeUSD},
	}, nil, nil)
	if err != nil || pos == nil {
		t.Fatalf("paper adapter expected simulated position, got pos=%v err=%v", pos, err)
	}
	if err := liveExecutionAdapter(nil, cand, 0, 10, 1, ladderPlan{}); err == nil || err.Error() != "execution manager not ready" {
		t.Fatalf("expected live venue boundary error, got %v", err)
	}
}

func testLiveDispatchManager() *liveExecManager {
	return &liveExecManager{
		minStopPct:    0.5,
		maxStopPct:    5.0,
		minTP1RR:      0.5,
		tp1R:          1.0,
		tp2R:          2.0,
		tp3R:          3.0,
		positions:     map[string]*livePosition{},
		ladderCfg:     loadLadderConfig(10),
		hybridStopCfg: exitmgr.HybridStopConfig{},
		exitManager:   exitmgr.NewManager(exitmgr.Config{}),
	}
}

func TestLiveLoopEligibleCandidateNoLongerStopsAtScannerOnlyManualExecution(t *testing.T) {
	cand := parityTestCandidate()
	execMgr := testLiveDispatchManager()
	adapterCalls := 0
	dispatch := dispatchLiveRuntimeDecision(time.Now().UTC(), cand, map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 101, FundingRate: 0.001},
	}, execMgr, risk.DefaultConfig(), 3, 8, "fixed", 2, 2, 5, 50, 0, liveRuntimeDispatchHooks{
		Adapter: func(c candidate, entryBps, margin float64, leverage int, plan ladderPlan) error {
			adapterCalls++
			return nil
		},
	})
	if !dispatch.Decision.Approved {
		t.Fatalf("expected live shared decision approved, got %+v", dispatch.Decision)
	}
	if !dispatch.Attempted || !dispatch.Entered {
		t.Fatalf("expected live dispatch attempt and enter, got %+v", dispatch)
	}
	if adapterCalls != 1 {
		t.Fatalf("expected adapter to be called once, got %d", adapterCalls)
	}
	var st liveStatus
	applyLiveDecisionStatus(&st, cand, dispatch)
	if got := st.TopDecisionWhy; got == "scanner_only_manual_execution" {
		t.Fatalf("expected scanner-only path removed, got %q", got)
	}
}

func TestLiveLoopDispatchesValidSharedDecisionIntoAdapter(t *testing.T) {
	cand := parityTestCandidate()
	execMgr := testLiveDispatchManager()
	var gotCandidate candidate
	var gotPlan ladderPlan
	var gotMargin float64
	var gotLeverage int
	dispatch := dispatchLiveRuntimeDecision(time.Now().UTC(), cand, map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 101, FundingRate: 0.001},
	}, execMgr, risk.DefaultConfig(), 3, 8, "fixed", 2, 2, 5, 50, 2, liveRuntimeDispatchHooks{
		Adapter: func(c candidate, entryBps, margin float64, leverage int, plan ladderPlan) error {
			gotCandidate = c
			gotPlan = plan
			gotMargin = margin
			gotLeverage = leverage
			return nil
		},
	})
	if !dispatch.Entered {
		t.Fatalf("expected live dispatch success, got %+v", dispatch)
	}
	if gotCandidate.Entry.Symbol != cand.Entry.Symbol || gotCandidate.Strat != cand.Strat {
		t.Fatalf("unexpected candidate passed to adapter: %+v", gotCandidate)
	}
	if gotPlan.MarginUSDT <= 0 {
		t.Fatalf("expected resolved ladder plan with margin, got %+v", gotPlan)
	}
	if gotMargin != 50 {
		t.Fatalf("expected effective margin 50, got %.2f", gotMargin)
	}
	if gotLeverage != 2 {
		t.Fatalf("expected leverage 2, got %d", gotLeverage)
	}
}

func TestProjectedProofQualityRejectMatchesPaperAndLive(t *testing.T) {
	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 83,
			ScoreSlope:   0.03,
			State:        inplay.StateInPlay,
			Rank:         1,
			EntryStyle:   "pullback_long",
		},
		Side:              "BUY",
		Strat:             "pullback_reclaim",
		StrategyID:        "pullback_reclaim",
		Conf:              0.76,
		CombinedScore:     0.76,
		DayUTC24h:         6,
		UTC4hPct:          1.0,
		UTC1hPct:          0.3,
		LastClose:         101,
		SessionVWAP:       100.6,
		EMA9:              100.5,
		ExtensionATR:      0.80,
		DistanceToVWAPPct: 0.60,
		VolumeRatio:       1.0,
		OFIZ:              0.02,
		Sig: strategies.Signal{
			Active:       true,
			Name:         "pullback_reclaim",
			Side:         features.SideLong,
			Entry:        101,
			Stop:         99.8,
			TP1:          102.4,
			RejectReason: "quality_score_too_low",
		},
	}
	meta := map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 101, FundingRate: 0.001},
	}
	paperDec := buildPaperExecutionDecision(paperDecisionCtx{
		Candidate:           cand,
		MetaBySymbol:        meta,
		RiskShell:           risk.DefaultConfig(),
		RiskFallbackStopPct: 3,
		RiskHoldHours:       8,
		LeverageMode:        "fixed",
		LeverageFixed:       2,
		LeverageMin:         2,
		MaxLeverage:         5,
		EffectiveMargin:     50,
		Paper:               &paperTrader{enabled: true, positions: map[string]*paperPosition{}},
	})
	liveDispatch := dispatchLiveRuntimeDecision(time.Now().UTC(), cand, meta, testLiveDispatchManager(), risk.DefaultConfig(), 3, 8, "fixed", 2, 2, 5, 50, 0, liveRuntimeDispatchHooks{})
	if paperDec.RejectReason != "quality_score_too_low" {
		t.Fatalf("expected paper reject reason quality_score_too_low, got %q", paperDec.RejectReason)
	}
	if liveDispatch.RejectReason != paperDec.RejectReason {
		t.Fatalf("expected live reject reason %q, got %q", paperDec.RejectReason, liveDispatch.RejectReason)
	}
}

func TestApprovedPaperCandidateBuildsSameTradePlanInLiveDispatch(t *testing.T) {
	cand := parityTestCandidate()
	execMgr := testLiveDispatchManager()
	meta := map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 101, FundingRate: 0.001},
	}
	liveDispatch := dispatchLiveRuntimeDecision(time.Now().UTC(), cand, meta, execMgr, risk.DefaultConfig(), 3, 8, "fixed", 2, 2, 5, 50, 0, liveRuntimeDispatchHooks{
		Adapter: func(c candidate, entryBps, margin float64, leverage int, plan ladderPlan) error { return nil },
	})
	paperPlan, err := buildSharedTradePlan(cand, 101, cand.VolumeUSD, sharedTradePlanConfig{
		MinStopPct:    execMgr.minStopPct,
		MaxStopPct:    execMgr.maxStopPct,
		MinTP1RR:      execMgr.minTP1RR,
		RiskOnMargin:  execMgr.riskOnMargin,
		RiskMarginPct: execMgr.riskMarginPct,
		TP1R:          execMgr.tp1R,
		TP2R:          execMgr.tp2R,
		TP3R:          execMgr.tp3R,
		HybridStopCfg: execMgr.hybridStopCfg,
		FrontRunner:   execMgr.exitManager,
	})
	if err != nil {
		t.Fatalf("paper shared plan error: %v", err)
	}
	if !reflect.DeepEqual(paperPlan, liveDispatch.TradePlan) {
		t.Fatalf("expected same trade plan, paper=%+v live=%+v", paperPlan, liveDispatch.TradePlan)
	}
}
