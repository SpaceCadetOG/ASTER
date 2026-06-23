package main

import (
	"strings"
	"testing"
	"time"

	"go-machine/internal/inplay"
)

func TestPaperContinuationCleanDisablesReversal(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	t.Setenv("LIVE_ENABLE_MOMENTUM_REVERSAL", "1")
	if effectiveMomentumReversalEnabled() {
		t.Fatalf("expected reversal disabled by profile")
	}
}

func TestPaperContinuationCleanDisablesResetImpulse(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	t.Setenv("LIVE_ENABLE_RESET_IMPULSE", "1")
	if effectiveResetImpulseEnabled() {
		t.Fatalf("expected reset impulse disabled by profile")
	}
}

func TestPaperContinuationCleanDisablesImpulsiveStarters(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_LONG_STARTER", "1")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_SHORT_STARTER", "1")
	if effectiveImpulsiveLongStarterEnabled() {
		t.Fatalf("expected impulsive long starter disabled by profile")
	}
	if effectiveImpulsiveShortStarterEnabled() {
		t.Fatalf("expected impulsive short starter disabled by profile")
	}
}

func TestPaperContinuationCleanDisablesReentry(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	t.Setenv("LIVE_REENTRY_ENABLE", "1")
	cfg := loadReentryConfig(10)
	if cfg.Enable {
		t.Fatalf("expected reentry disabled by profile")
	}
}

func TestPaperContinuationCleanKeepsVPAndInstitutionalPAEnabled(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	t.Setenv("LIVE_ENABLE_VP_SETUPS", "0")
	t.Setenv("LIVE_ENABLE_INSTITUTIONAL_PA", "0")
	if !effectiveVPSetupsEnabled() {
		t.Fatalf("expected VP setups enabled by profile")
	}
	if !effectiveInstitutionalPAEnabled() {
		t.Fatalf("expected institutional PA enabled by profile")
	}
}

func TestPaperContinuationCleanIgnoresLowerLevelOverrides(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	t.Setenv("LIVE_ENABLE_MOMENTUM_REVERSAL", "1")
	t.Setenv("LIVE_ENABLE_RESET_IMPULSE", "1")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_LONG_STARTER", "1")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_SHORT_STARTER", "1")
	t.Setenv("LIVE_REENTRY_ENABLE", "1")
	t.Setenv("LIVE_ENABLE_VP_SETUPS", "0")
	t.Setenv("LIVE_ENABLE_INSTITUTIONAL_PA", "0")

	cfg := resolveRuntimeProfileConfig()
	if cfg.EffectiveReversal {
		t.Fatalf("expected reversal override to be ignored")
	}
	if cfg.EffectiveImpulse {
		t.Fatalf("expected impulse override to be ignored")
	}
	if cfg.EffectiveReentry {
		t.Fatalf("expected reentry override to be ignored")
	}
	if !cfg.EffectiveVPEnabled {
		t.Fatalf("expected VP disable override to be ignored")
	}
	if !cfg.EffectiveInstitutional {
		t.Fatalf("expected institutional PA disable override to be ignored")
	}
}

func TestPaperContinuationCleanUsesPenaltyBasedDirectionalConflict(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	t.Setenv("LIVE_DIRECTIONAL_CONFLICT_BLOCK_ENABLE", "1")
	t.Setenv("LIVE_DIRECTIONAL_CONFLICT_BLOCK_PCT", "3")
	t.Setenv("LIVE_PENALTY_DIRECTIONAL_CONFLICT_PCT", "3")

	cand := candidate{
		Side:      "BUY",
		Strat:     "continuation_fast",
		Entry:     inplay.Entry{Symbol: "BTCUSDT"},
		DayUTC24h: -4,
	}
	if got := directionalConflictRejectReason(cand); got != "" {
		t.Fatalf("expected non-extreme directional conflict to stay penalty-only, got %q", got)
	}
	quality := buildEntryQualityAccumulator(cand, []string{"directional_dayutc_conflict"})
	if len(quality.HardBlockReasons) != 0 {
		t.Fatalf("expected no hard block reasons, got %+v", quality.HardBlockReasons)
	}
	if !containsString(quality.QualityFlags, "directional_dayutc_conflict") {
		t.Fatalf("expected directional conflict quality flag, got %+v", quality.QualityFlags)
	}
}

func TestPaperContinuationCleanAllowsUnresolvedWatchButNotExecution(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")

	now := time.Now().UTC()
	cands := applyCandidateLifecycle([]candidate{{
		Side:  "BUY",
		Strat: "none",
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentScore: 80,
			ScoreSlope:   0.10,
			State:        inplay.StateInPlay,
		},
	}}, now, map[string]candidateMemory{}, candidateLifecycleConfig{
		Enable:        effectiveCandidateMemoryEnabled(),
		ArmScans:      2,
		ReadyScans:    3,
		ReadyMinScore: 65,
		ReadyMinSlope: 0.01,
		ExpireAfter:   20 * time.Minute,
	})
	if len(cands) != 1 {
		t.Fatalf("expected unresolved candidate to remain watchable, got %d candidates", len(cands))
	}
	if got := cands[0].LifecycleStage; got == "" {
		t.Fatalf("expected unresolved candidate to receive a lifecycle stage")
	}

	ctx := testPaperDecisionCtx()
	ctx.Now = now
	ctx.Candidate = cands[0]
	verdict := paperPreflightVerdict(ctx)
	if verdict.Approved {
		t.Fatalf("expected unresolved candidate to be blocked from execution")
	}
	if verdict.Reason != "strategy_unresolved" {
		t.Fatalf("expected strategy_unresolved, got %q", verdict.Reason)
	}
}

func TestPaperContinuationCleanPromotesContinuationSetupFamilyToExecutableStrategy(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")

	got := applySimpleContinuationFallbackAt(candidate{
		Side:        "BUY",
		Strat:       "none",
		ReclaimHold: true,
		LastClose:   101,
		SessionVWAP: 100,
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			EntryStyle:   "pullback_long",
			CurrentScore: 84,
			ScoreSlope:   0.08,
			State:        inplay.StateInPlay,
		},
	}, time.Now().UTC())

	if got.SetupFamily != "micro_pullback_continuation" {
		t.Fatalf("expected continuation setup family, got %q", got.SetupFamily)
	}
	if got.Strat != "micro_pullback_continuation" {
		t.Fatalf("expected executable strategy promoted from setup family, got %q", got.Strat)
	}
}

func TestPaperContinuationCleanSetupFamilyOwnsContinuationExecution(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")

	got := applySimpleContinuationFallbackAt(candidate{
		Side:            "BUY",
		Strat:           "",
		ClosedBreakHold: true,
		LastClose:       101,
		SessionVWAP:     100,
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			EntryStyle:   "breakout_hold_long",
			CurrentScore: 88,
			ScoreSlope:   0.10,
			State:        inplay.StateInPlay,
		},
	}, time.Now().UTC())

	if got.SetupFamily != "breakout_retest" {
		t.Fatalf("expected breakout_retest setup family, got %q", got.SetupFamily)
	}
	if got.Strat != "breakout_retest" {
		t.Fatalf("expected continuation setup family to own execution, got %q", got.Strat)
	}
}

func TestPaperContinuationCleanKeepsReversalWatchUnresolved(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")

	got := applySimpleContinuationFallbackAt(candidate{
		Side:  "BUY",
		Strat: "",
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			EntryStyle:   "reversal_watch_long",
			CurrentScore: 83,
			ScoreSlope:   -0.03,
			State:        inplay.StateCooling,
		},
	}, time.Now().UTC())

	if got.SetupFamily != "reversal_exhaustion" {
		t.Fatalf("expected reversal setup family, got %q", got.SetupFamily)
	}
	if got.Strat != "" {
		t.Fatalf("expected reversal watch to remain unresolved for non-continuation setup, got %q", got.Strat)
	}

	ctx := testPaperDecisionCtx()
	ctx.Candidate = got
	verdict := paperPreflightVerdict(ctx)
	if verdict.Approved {
		t.Fatalf("expected reversal-style candidate to stay blocked")
	}
	if verdict.Reason != "strategy_unresolved" {
		t.Fatalf("expected strategy_unresolved for unresolved reversal-style candidate, got %q", verdict.Reason)
	}
}

func TestPaperContinuationCleanKeepsResolvedRouterStrategyLabel(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")

	got := applySimpleContinuationFallbackAt(candidate{
		Side:        "BUY",
		Strat:       "vp_trend",
		ReclaimHold: true,
		LastClose:   101,
		SessionVWAP: 100,
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			EntryStyle:   "pullback_long",
			CurrentScore: 90,
			ScoreSlope:   0.12,
			State:        inplay.StateInPlay,
		},
	}, time.Now().UTC())

	if got.SetupFamily != "micro_pullback_continuation" {
		t.Fatalf("expected continuation setup family, got %q", got.SetupFamily)
	}
	if got.Strat != "vp_trend" {
		t.Fatalf("expected resolved router strategy to be preserved, got %q", got.Strat)
	}
}

func TestPaperContinuationCleanKeepsSharedManagementActive(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	if !effectiveSharedManagementEnabled() {
		t.Fatalf("expected shared management to remain active")
	}
}

func TestPaperContinuationCleanDisablesStrategySpecificManagementOverrides(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	t.Setenv("LIVE_TRAIL_ATR_MULT_CONT", "2.6")
	t.Setenv("LIVE_TRAIL_ATR_MULT_REV", "4.5")

	cont := trailATRMultForContext("continuation_fast", 0.8, 2_000_000)
	rev := trailATRMultForContext("mom_reversal", 0.8, 2_000_000)
	if cont != rev {
		t.Fatalf("expected shared management multiplier, got cont=%.4f rev=%.4f", cont, rev)
	}
}

func TestStartupSummaryIncludesGroupedEffectiveProfileSettings(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")

	lines := startupSummaryLines("paper", time.Second, watchConfig{}, ladderConfig{StarterUSDT: 10}, reentryConfig{}, safetyConfig{}, nil)
	found := ""
	for _, line := range lines {
		if strings.Contains(line, "runtime_profile=") {
			found = line
			break
		}
	}
	if found == "" {
		t.Fatalf("expected startup summary to include runtime profile line")
	}
	for _, want := range []string{
		"runtime_profile=paper_continuation_clean",
		"effective_strategy_paths=",
		"effective_quality_policy=",
		"effective_reentry_policy=",
		"effective_management_policy=",
	} {
		if !strings.Contains(found, want) {
			t.Fatalf("expected startup summary to contain %q, got %q", want, found)
		}
	}
}
