package execution

import "testing"

func TestEvaluateWinnerProtection_InstantBELock(t *testing.T) {
	t.Setenv("LIVE_EXIT_BE_LOCK_R", "0.5")
	dec := EvaluateWinnerProtection("impulse_continuation", "BUY", 100, 98, 101, 0.5, false, true)
	if !dec.MoveStop {
		t.Fatalf("expected stop move, got %+v", dec)
	}
	if dec.NewStop < 100 {
		t.Fatalf("expected BE lock at/above entry, got %.8f", dec.NewStop)
	}
	if dec.Reason != "instant_be_lock" {
		t.Fatalf("expected instant_be_lock, got %+v", dec)
	}
}

func TestEvaluateWinnerProtection_EarlyTrail(t *testing.T) {
	t.Setenv("LIVE_EXIT_BE_LOCK_R", "0.7")
	t.Setenv("LIVE_EXIT_EARLY_TRAIL_R", "1.0")
	t.Setenv("LIVE_EXIT_EARLY_TRAIL_LOCK_FRAC", "0.30")
	dec := EvaluateWinnerProtection("impulse_continuation", "BUY", 100, 98, 104, 1.2, false, true)
	if !dec.MoveStop {
		t.Fatalf("expected stop move, got %+v", dec)
	}
	if dec.NewStop < 101.2 {
		t.Fatalf("expected 30%% peak lock, got %.8f", dec.NewStop)
	}
	if dec.Reason != "early_profit_trail" {
		t.Fatalf("expected early_profit_trail, got %+v", dec)
	}
}
