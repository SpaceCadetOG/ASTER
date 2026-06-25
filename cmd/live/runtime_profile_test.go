package main

import (
	"strings"
	"testing"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/inplay"
)

func TestLegacyPaperContinuationCleanProfileIsIgnored(t *testing.T) {
	t.Setenv("LIVE_RUNTIME_PROFILE", "paper_continuation_clean")
	if got := activeRuntimeProfile(); got != "" {
		t.Fatalf("expected legacy paper_continuation_clean profile to be ignored, got %q", got)
	}
}

func TestSetupFamilyOwnsExecutionWithoutProfileGuardrails(t *testing.T) {
	got := applySimpleContinuationFallbackAt(candidate{
		Side:         "BUY",
		Strat:        "",
		ResetRebreak: true,
		LastClose:    101,
		SessionVWAP:  100,
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			EntryStyle:   "momentum_ignite_long",
			CurrentScore: 88,
			ScoreSlope:   0.10,
			State:        inplay.StateInPlay,
		},
	}, time.Now().UTC())

	if got.SetupFamily != "reset_impulse_breakout" {
		t.Fatalf("expected reset_impulse_breakout setup family, got %q", got.SetupFamily)
	}
	if got.Strat != "impulse_breakout" {
		t.Fatalf("expected canonical execution ID, got %q", got.Strat)
	}
	if reason := executionStrategyRejectReason(got); reason != "" {
		t.Fatalf("expected no execution reject reason, got %q", reason)
	}
}

func TestPaperPreflightIgnoresThinDepthAndQualityBlocks(t *testing.T) {
	ctx := testPaperDecisionCtx()
	ctx.Candidate = candidate{
		Side:  "BUY",
		Strat: "micro_pullback_continuation",
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			CurrentScore: 55,
			ScoreSlope:   0.01,
			State:        inplay.StateBalanced,
		},
	}
	ctx.MetaBySymbol["BTCUSDT"] = symbolMeta{LastPrice: 100}
	ctx.EntryDepth = map[string]aster.OrderBook{
		"BTCUSDT": {},
	}
	ctx.OBFilterEnable = true

	verdict := paperPreflightVerdict(ctx)
	if !verdict.Approved {
		t.Fatalf("expected thin-depth/quality candidate to pass paper preflight, got reason=%q quality=%+v", verdict.Reason, verdict.Quality)
	}
}

func TestPaperPreflightAllowsPreviouslyUnresolvedWatchCandidate(t *testing.T) {
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
		t.Fatalf("expected watch candidate to remain available, got %d", len(cands))
	}

	ctx := testPaperDecisionCtx()
	ctx.Now = now
	ctx.Candidate = cands[0]
	verdict := paperPreflightVerdict(ctx)
	if !verdict.Approved {
		t.Fatalf("expected watch candidate to pass paper preflight, got reason=%q quality=%+v", verdict.Reason, verdict.Quality)
	}
}

func TestStartupSummaryShowsNoLegacyProfileTag(t *testing.T) {
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
	if strings.Contains(found, "paper_continuation_clean") {
		t.Fatalf("expected legacy paper_continuation_clean tag to be removed, got %q", found)
	}
	if !strings.Contains(found, "runtime_profile=none") {
		t.Fatalf("expected startup summary to fall back to runtime_profile=none, got %q", found)
	}
}
