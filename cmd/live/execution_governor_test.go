package main

import (
	"strings"
	"testing"
	"time"

	"go-machine/internal/inplay"
)

func TestExecutionGovernorRejectReasonBlocksSymbolWindow(t *testing.T) {
	t.Setenv("LIVE_EXEC_CAP_ENABLE", "1")
	t.Setenv("LIVE_EXEC_CAP_SYMBOL_MAX_ENTRIES", "2")
	t.Setenv("LIVE_EXEC_CAP_SYMBOL_WINDOW_MIN", "180")
	t.Setenv("LIVE_EXEC_CAP_BUCKET_MAX_ACTIVE", "0")
	t.Setenv("LIVE_EXEC_CAP_BUCKET_MAX_NEW_ENTRIES", "0")

	now := time.Date(2026, 4, 23, 18, 0, 0, 0, time.UTC)
	c := candidate{
		Side:        "BUY",
		Strat:       "continuation_fast",
		SetupFamily: "micro_pullback_continuation",
		Entry:       inplay.Entry{Symbol: "CHIPUSDT", CurrentScore: 94},
	}
	records := []executionGovernorRecord{
		{Kind: "ENTRY", Symbol: "CHIPUSDT", Side: "BUY", Bucket: "microcap_momentum_long", OccurredAt: now.Add(-70 * time.Minute)},
		{Kind: "ENTRY", Symbol: "CHIPUSDT", Side: "BUY", Bucket: "microcap_momentum_long", OccurredAt: now.Add(-20 * time.Minute)},
	}

	if got := executionGovernorRejectReasonFromState(now, c, 10, nil, records); got != "exec_cap_symbol_window" {
		t.Fatalf("expected exec_cap_symbol_window, got %q", got)
	}
}

func TestExecutionGovernorRejectReasonBlocksBucketWinnerPriority(t *testing.T) {
	t.Setenv("LIVE_EXEC_CAP_ENABLE", "1")
	t.Setenv("LIVE_EXEC_CAP_BUCKET_MAX_ACTIVE", "0")
	t.Setenv("LIVE_EXEC_CAP_BUCKET_MAX_NEW_ENTRIES", "0")
	t.Setenv("LIVE_EXEC_CAP_WINNER_OVERRIDE_SCORE_DELTA", "5")

	now := time.Date(2026, 4, 23, 18, 0, 0, 0, time.UTC)
	c := candidate{
		Side:        "BUY",
		Strat:       "continuation_fast",
		SetupFamily: "micro_pullback_continuation",
		Entry:       inplay.Entry{Symbol: "CHIPUSDT", CurrentScore: 93},
	}
	positions := []executionGovernorPositionView{
		{
			Symbol:          "SPKUSDT",
			Side:            "LONG",
			Bucket:          "microcap_momentum_long",
			SetupFamily:     "micro_pullback_continuation",
			WinnerLifecycle: "runner",
			Score:           92,
			Margin:          25,
			Active:          true,
		},
	}

	if got := executionGovernorRejectReasonFromState(now, c, 10, positions, nil); got != "exec_cap_bucket_winner_priority" {
		t.Fatalf("expected exec_cap_bucket_winner_priority, got %q", got)
	}

	c.Entry.CurrentScore = 98
	if got := executionGovernorRejectReasonFromState(now, c, 10, positions, nil); got != "" {
		t.Fatalf("expected stronger override to pass, got %q", got)
	}
}

func TestExecutionGovernorRejectLogLineIncludesSuppressingWinner(t *testing.T) {
	t.Setenv("LIVE_EXEC_CAP_ENABLE", "1")
	now := time.Date(2026, 4, 23, 18, 0, 0, 0, time.UTC)
	c := candidate{
		Side:        "BUY",
		Strat:       "continuation_fast",
		SetupFamily: "micro_pullback_continuation",
		Entry:       inplay.Entry{Symbol: "CHIPUSDT", CurrentScore: 93},
	}
	positions := []executionGovernorPositionView{
		{
			Symbol:          "SPKUSDT",
			Side:            "LONG",
			Bucket:          "microcap_momentum_long",
			SetupFamily:     "micro_pullback_continuation",
			WinnerLifecycle: "runner",
			Score:           92,
			Active:          true,
		},
	}

	dec := executionGovernorDecisionFromState(now, c, 10, positions, nil)
	if dec.Reason != "exec_cap_bucket_winner_priority" {
		t.Fatalf("expected winner-priority decision, got %+v", dec)
	}
	line := executionGovernorRejectLogLine(dec)
	if !strings.Contains(line, "suppressing_winner_symbol=SPKUSDT") {
		t.Fatalf("expected suppressing winner symbol in log line, got %s", line)
	}
	if !strings.Contains(line, "suppressing_winner_state=runner") {
		t.Fatalf("expected suppressing winner state in log line, got %s", line)
	}
	if !strings.Contains(line, "bucket_has_active_winner=true") {
		t.Fatalf("expected active winner flag in log line, got %s", line)
	}
}

func TestExecutionGovernorRejectReasonBlocksRecentSoftChurnRecycle(t *testing.T) {
	t.Setenv("LIVE_EXEC_CAP_ENABLE", "1")
	t.Setenv("LIVE_EXEC_CAP_SYMBOL_MAX_ENTRIES", "0")
	t.Setenv("LIVE_EXEC_CAP_BUCKET_MAX_ACTIVE", "0")
	t.Setenv("LIVE_EXEC_CAP_BUCKET_MAX_NEW_ENTRIES", "0")
	t.Setenv("LIVE_EXEC_CAP_SYMBOL_SOFT_COOLDOWN_MIN", "30")
	t.Setenv("LIVE_EXEC_CAP_SOFT_OVERRIDE_SCORE_DELTA", "7.5")

	now := time.Date(2026, 4, 23, 18, 0, 0, 0, time.UTC)
	c := candidate{
		Side:        "BUY",
		Strat:       "continuation_fast",
		SetupFamily: "micro_pullback_continuation",
		Entry:       inplay.Entry{Symbol: "SPKUSDT", CurrentScore: 92},
	}
	records := []executionGovernorRecord{
		{
			Kind:        "EXIT",
			Symbol:      "SPKUSDT",
			Side:        "BUY",
			Bucket:      "microcap_momentum_long",
			SetupFamily: "micro_pullback_continuation",
			Reason:      "NO_FOLLOW_THROUGH",
			Score:       90,
			OccurredAt:  now.Add(-10 * time.Minute),
		},
	}

	if got := executionGovernorRejectReasonFromState(now, c, 10, nil, records); got != "exec_cap_symbol_soft_churn" {
		t.Fatalf("expected exec_cap_symbol_soft_churn, got %q", got)
	}

	c.Entry.CurrentScore = 99
	if got := executionGovernorRejectReasonFromState(now, c, 10, nil, records); got != "" {
		t.Fatalf("expected stronger recycle override to pass, got %q", got)
	}
}
