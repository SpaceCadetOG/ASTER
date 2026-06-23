package main

import (
	"testing"
	"time"

	exitmgr "go-machine/internal/execution"
	"go-machine/internal/inplay"
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

func TestLiveEligibilityQualityPenaltyCanStillAllowEntry(t *testing.T) {
	t.Setenv("LIVE_META_MIN_QUALITY", "0.52")
	t.Setenv("LIVE_META_MIN_QUALITY_CONT", "0.52")

	cand := candidate{
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentGrade: "A",
			CurrentScore: 95,
			ScoreSlope:   0.01,
			State:        inplay.StateInPlay,
			Rank:         1,
		},
		Side:            "BUY",
		Strat:           "continuation_fast",
		Conf:            0.80,
		CombinedScore:   0.80,
		RejectReason:    "weak_slope",
		ClosedBreakHold: true,
	}

	summary := newEligibilitySummary(cand)
	summary.FullEntryAllowed = true
	chooseFinalDecision(&summary, ladderPlan{})

	if got := summary.FinalDecision; got != "full_entry" {
		t.Fatalf("expected full_entry, got %q", got)
	}
	if got := summary.FinalReason; got != "continuation_fast" {
		t.Fatalf("expected strategy reason preserved, got %q", got)
	}
	if got := summary.Quality.BlockReason; got != "" {
		t.Fatalf("expected no quality block reason, got %q", got)
	}
	if summary.AdjustedConfidence >= cand.Conf {
		t.Fatalf("expected adjusted confidence to reflect penalty, before=%.2f after=%.2f", cand.Conf, summary.AdjustedConfidence)
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

func TestPlaceEntryRejectsUnresolvedStrategyBeforeSubmit(t *testing.T) {
	err := (*liveExecManager)(nil).PlaceEntry(candidate{Strat: "none"}, 0, 10, 5, ladderPlan{})
	if err == nil || err.Error() != "strategy_unresolved" {
		t.Fatalf("expected strategy_unresolved, got %v", err)
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
