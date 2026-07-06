package main

import (
	"testing"
	"time"

	exitmgr "go-machine/internal/execution"
	"go-machine/internal/inplay"
	"go-machine/internal/strategies"
)

func TestLiveEligibilityQualityRejectUsesQualityScoreTooLow(t *testing.T) {
	t.Setenv("LIVE_META_MIN_QUALITY", "0.52")
	t.Setenv("LIVE_META_MIN_QUALITY_CONT", "0.52")

	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 92,
			ScoreSlope:   0.01,
			State:        inplay.StateInPlay,
			Rank:         1,
		},
		Side:            "BUY",
		Strat:           "continuation_fast",
		Conf:            0.54,
		CombinedScore:   0.54,
		RejectReason:    "weak_slope",
		ClosedBreakHold: true,
	}

	summary := newEligibilitySummary(cand)
	summary.FullEntryAllowed = true
	chooseFinalDecision(&summary, ladderPlan{})

	if got := summary.FinalReason; got != "quality_score_too_low" {
		t.Fatalf("expected quality_score_too_low, got %q", got)
	}
	if got := summary.FinalDecision; got != "reject" {
		t.Fatalf("expected reject decision, got %q", got)
	}
	if len(summary.HardBlocks) != 0 {
		t.Fatalf("expected no hard blocks, got %+v", summary.HardBlocks)
	}
	if !containsString(summary.Quality.QualityFlags, "weak_slope") {
		t.Fatalf("expected weak_slope quality flag, got %+v", summary.Quality.QualityFlags)
	}
}

func TestLiveEligibilityProjectedWeakProofRejectsEntry(t *testing.T) {
	t.Setenv("LIVE_META_MIN_QUALITY", "0.52")
	t.Setenv("LIVE_META_MIN_QUALITY_CONT", "0.52")

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
		EntryScoreBreakdown: EntryScoreBreakdown{
			TrendScore:    8,
			LocationScore: 14,
			TriggerScore:  12,
			FlowScore:     8,
			FinalScore:    58,
			TrendLabel:    "scored",
		},
		Sig: strategies.Signal{
			Entry: 101,
			Stop:  99.8,
			TP1:   102.4,
		},
	}

	summary := newEligibilitySummary(cand)
	summary.FullEntryAllowed = true
	chooseFinalDecision(&summary, ladderPlan{})

	if got := summary.FinalDecision; got != "reject" {
		t.Fatalf("expected reject, got %q", got)
	}
	if got := summary.FinalReason; got != "quality_score_too_low" {
		t.Fatalf("expected quality_score_too_low, got %q", got)
	}
	if !containsString(summary.Quality.QualityFlags, "projected_weak_proof") {
		t.Fatalf("expected projected_weak_proof flag, got %+v", summary.Quality.QualityFlags)
	}
}

func TestLiveEligibilityProjectedNoProofRejectsEntry(t *testing.T) {
	t.Setenv("LIVE_META_MIN_QUALITY", "0.52")
	t.Setenv("LIVE_META_MIN_QUALITY_CONT", "0.52")

	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "B",
			CurrentScore: 70,
			ScoreSlope:   -0.01,
			State:        inplay.StateHeating,
			Rank:         2,
			EntryStyle:   "none",
		},
		Side:              "BUY",
		Strat:             "pullback_reclaim",
		Conf:              0.58,
		CombinedScore:     0.58,
		DayUTC24h:         1,
		UTC4hPct:          -0.4,
		UTC1hPct:          -0.2,
		LastClose:         101,
		SessionVWAP:       102,
		EMA9:              101.8,
		ExtensionATR:      1.05,
		DistanceToVWAPPct: 1.20,
		VolumeRatio:       0.85,
		OFIZ:              -0.04,
		EntryScoreBreakdown: EntryScoreBreakdown{
			TrendScore:    8,
			LocationScore: 8,
			TriggerScore:  10,
			FlowScore:     5,
			FinalScore:    40,
			TrendLabel:    "scored",
		},
		Sig: strategies.Signal{
			Entry: 101,
			Stop:  99.9,
			TP1:   102.0,
		},
	}

	summary := newEligibilitySummary(cand)
	summary.FullEntryAllowed = true
	chooseFinalDecision(&summary, ladderPlan{})

	if got := summary.FinalDecision; got != "reject" {
		t.Fatalf("expected reject, got %q", got)
	}
	if got := summary.FinalReason; got != "quality_score_too_low" {
		t.Fatalf("expected quality_score_too_low, got %q", got)
	}
	if !containsString(summary.Quality.QualityFlags, "projected_no_proof") {
		t.Fatalf("expected projected_no_proof flag, got %+v", summary.Quality.QualityFlags)
	}
}

func TestLiveEligibilityQualityPenaltyCanStillAllowEntry(t *testing.T) {
	t.Setenv("LIVE_META_MIN_QUALITY", "0.52")
	t.Setenv("LIVE_META_MIN_QUALITY_CONT", "0.52")

	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 95,
			ScoreSlope:   0.18,
			State:        inplay.StateInPlay,
			Rank:         1,
			EntryStyle:   "pullback_long",
		},
		Side:              "BUY",
		Strat:             "pullback_reclaim",
		Conf:              0.80,
		CombinedScore:     0.80,
		RejectReason:      "weak_slope",
		ClosedBreakHold:   true,
		ReclaimHold:       true,
		DayUTC24h:         12,
		UTC4hPct:          4,
		UTC1hPct:          1.5,
		LastClose:         101,
		SessionVWAP:       100.9,
		EMA9:              100.7,
		ExtensionATR:      0.45,
		DistanceToVWAPPct: 0.10,
		VolumeRatio:       1.8,
		OFIZ:              0.45,
		Sig: strategies.Signal{
			Entry: 101,
			Stop:  99.5,
			TP1:   104.5,
		},
	}

	summary := newEligibilitySummary(cand)
	summary.FullEntryAllowed = true
	chooseFinalDecision(&summary, ladderPlan{})

	if got := summary.FinalDecision; got != "full_entry" {
		t.Fatalf("expected full_entry, got %q", got)
	}
	if got := summary.FinalReason; got != "pullback_reclaim" {
		t.Fatalf("expected strategy reason preserved, got %q", got)
	}
	if got := summary.Quality.BlockReason; got != "" {
		t.Fatalf("expected no quality block reason, got %q", got)
	}
	if summary.AdjustedConfidence >= cand.Conf {
		t.Fatalf("expected adjusted confidence to reflect penalty, before=%.2f after=%.2f", cand.Conf, summary.AdjustedConfidence)
	}
}

func TestLiveEntryDegradedReasonHardBlock(t *testing.T) {
	if liveEntryDegradedReasonHardBlock(degradedAccountHealthPartialReason) {
		t.Fatalf("expected partial account health to be advisory")
	}
	if liveEntryDegradedReasonHardBlock(degradedUserDataStaleReason) {
		t.Fatalf("expected stale userdata to be advisory")
	}
	if liveEntryDegradedReasonHardBlock(degradedReconcileStaleReason) {
		t.Fatalf("expected stale reconcile to be advisory")
	}
	if !liveEntryDegradedReasonHardBlock(degradedOrderLegalityQuarantineReason) {
		t.Fatalf("expected legality quarantine to remain hard-blocking")
	}
}

func TestProjectedProofOutcomeClassifiesGoodAndStrong(t *testing.T) {
	strong := candidate{
		Side:              "BUY",
		DayUTC24h:         12,
		UTC4hPct:          4,
		UTC1hPct:          1.5,
		LastClose:         101,
		SessionVWAP:       100.9,
		EMA9:              100.7,
		ExtensionATR:      0.45,
		DistanceToVWAPPct: 0.10,
		VolumeRatio:       1.8,
		OFIZ:              0.45,
		ReclaimHold:       true,
		TriggerState:      string(triggerOFReclaim),
		Sig: strategies.Signal{
			Entry: 101,
			Stop:  99.5,
			TP1:   104.5,
		},
		Entry: inplay.Entry{
			EntryStyle: "pullback_long",
			State:      inplay.StateInPlay,
			ScoreSlope: 0.22,
		},
	}
	if got := projectedProofOutcome(strong); got != EntryOutcomeStrongProof {
		t.Fatalf("expected strong projection, got %s", got)
	}

	good := strong
	good.VolumeRatio = 1.2
	good.OFIZ = 0.12
	good.ReclaimHold = false
	good.ClosedBreakHold = true
	good.TriggerState = string(triggerImpulseCont)
	good.ExtensionATR = 0.75
	good.DistanceToVWAPPct = 0.35
	if got := projectedProofOutcome(good); got != EntryOutcomeGoodProof {
		t.Fatalf("expected good projection, got %s", got)
	}
}

func TestLiveEligibilityHardStateBlockStillWins(t *testing.T) {
	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 95,
			ScoreSlope:   0.30,
			State:        inplay.StateInPlay,
		},
		Side:          "BUY",
		Strat:         "continuation_fast",
		Conf:          0.80,
		CombinedScore: 0.80,
		RejectReason:  "pending_add_order",
	}

	summary := newEligibilitySummary(cand)
	summary.FullEntryAllowed = true
	chooseFinalDecision(&summary, ladderPlan{})

	if got := summary.FinalReason; got != "pending_add_order" {
		t.Fatalf("expected pending_add_order, got %q", got)
	}
	if got := summary.FinalDecision; got != "reject" {
		t.Fatalf("expected reject, got %q", got)
	}
}

func TestPlaceEntryTreatsUnresolvedStrategyAsAdvisoryBeforeSubmit(t *testing.T) {
	err := (*liveExecManager)(nil).PlaceEntry(candidate{Strat: "none"}, 0, 10, 5, ladderPlan{})
	if err == nil || err.Error() != "execution manager not ready" {
		t.Fatalf("expected venue-boundary error after advisory strategy reject, got %v", err)
	}
}

func TestPlaceEntryRejectsProjectedNoProofBeforeSubmit(t *testing.T) {
	err := (*liveExecManager)(nil).PlaceEntry(candidate{
		Entry:       inplay.Entry{Symbol: "BTCUSDT"},
		Side:        "BUY",
		Strat:       "pullback_reclaim",
		StrategyID:  "pullback_reclaim",
		SetupFamily: "micro_pullback_continuation",
		EntryScoreBreakdown: EntryScoreBreakdown{
			FinalScore: 40,
			TrendLabel: "scored",
		},
		Sig: strategies.Signal{
			Entry: 101,
			Stop:  99.9,
			TP1:   102.0,
		},
	}, 0, 10, 5, ladderPlan{})
	if err == nil || err.Error() != "execution manager not ready" {
		t.Fatalf("expected venue-boundary error after advisory proof reject, got %v", err)
	}
}

func TestCanonicalExecutionStrategyPreservesCleanStrategyLabels(t *testing.T) {
	tests := []struct {
		name  string
		strat string
		side  string
		want  string
	}{
		{name: "impulse long", strat: "impulse_long", side: "BUY", want: "impulse_long"},
		{name: "impulse short", strat: "impulse_short", side: "SELL", want: "impulse_short"},
		{name: "continuation fast", strat: "continuation_fast", side: "BUY", want: "continuation_fast"},
		{name: "breakout retest", strat: "breakout_retest", side: "BUY", want: "breakout_retest"},
	}
	for _, tt := range tests {
		if got := canonicalExecutionStrategy(tt.strat, tt.side); got != tt.want {
			t.Fatalf("%s: expected %q, got %q", tt.name, tt.want, got)
		}
	}
}

func TestResolveLadderPlanUsesTradeMarginAndBlocksSameSymbolAddOns(t *testing.T) {
	t.Setenv("LIVE_TRADE_MARGIN_USDT", "10")
	now := time.Now().UTC()
	execMgr := &liveExecManager{
		ladderCfg: loadLadderConfig(10),
		positions: map[string]*livePosition{
			"BTCUSDT": {
				Symbol:       "BTCUSDT",
				Side:         "BUY",
				State:        execOpen,
				RemainingQty: 1,
			},
		},
	}
	plan := resolveLadderPlan(now, candidate{
		Side: "BUY",
		Entry: inplay.Entry{
			Symbol: "BTCUSDT",
		},
	}, execMgr, nil)
	if plan.MarginUSDT != 10 {
		t.Fatalf("expected trade margin 10, got %.2f", plan.MarginUSDT)
	}
	if plan.IsAdd || plan.IsReentry {
		t.Fatalf("expected plain entry-only plan, got %+v", plan)
	}
	if plan.RejectReason != "max_open_same_symbol" {
		t.Fatalf("expected same-symbol addon rejection, got %+v", plan)
	}
}

func TestCanonicalImpulseStrategiesStayInIgniteFamily(t *testing.T) {
	for _, strat := range []string{"impulse_long", "impulse_short"} {
		got := strategyFamily(candidate{
			Strat: strat,
			Entry: inplay.Entry{EntryStyle: "none"},
		})
		if got != "ignite" {
			t.Fatalf("expected %s to map to ignite family, got %q", strat, got)
		}
	}
}

func TestStopTemplateUsesContinuationImpulseForCanonicalImpulseLabels(t *testing.T) {
	for _, strat := range []string{"impulse_long", "impulse_short"} {
		got := stopTemplateForCandidate(candidate{Strat: strat})
		if got != exitmgr.StopTemplateContinuationImpulse {
			t.Fatalf("expected %s to use continuation impulse stop template, got %q", strat, got)
		}
	}
}
