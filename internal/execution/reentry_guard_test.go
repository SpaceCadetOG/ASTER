package execution

import (
	"testing"
	"time"
)

func TestShouldBlockReentryAfterSoftExit(t *testing.T) {
	t.Setenv("LIVE_REENTRY_SOFT_EXIT_COOLDOWN_MIN", "20")
	t.Setenv("LIVE_REENTRY_REQUIRE_STRONGER_SCORE_AFTER_SOFT_EXIT", "true")
	t.Setenv("LIVE_REENTRY_SOFT_EXIT_STRONGER_SCORE_DELTA", "7.5")

	now := time.Now().UTC()
	rec := ReentryRecord{
		LastExitTime:   now.Add(-10 * time.Minute),
		LastExitReason: "MOMENTUM_FADE",
		LastStrategy:   "impulse_continuation",
		LastSide:       "BUY",
		LastStopScore:  90,
	}

	block, reason := ShouldBlockReentry("CHIPUSDT", "impulse_continuation", "BUY", rec, now, 94)
	if !block || reason != "same_setup_soft_exit_cooldown" {
		t.Fatalf("expected soft exit cooldown block, got block=%v reason=%s", block, reason)
	}

	block, _ = ShouldBlockReentry("CHIPUSDT", "impulse_continuation", "BUY", rec, now, 99)
	if block {
		t.Fatalf("expected reentry allowed after materially stronger score")
	}
}
