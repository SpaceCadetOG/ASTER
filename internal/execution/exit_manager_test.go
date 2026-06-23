package execution

import "testing"

func TestFrontRunTarget(t *testing.T) {
	m := NewManager(Config{FrontRunPct: 0.001})
	got := m.FrontRunTarget("BUY", 110, 108)
	if got >= 108 {
		t.Fatalf("expected front-run below friction, got %.6f", got)
	}
	gotS := m.FrontRunTarget("SELL", 90, 92)
	if gotS <= 92 {
		t.Fatalf("expected short front-run above friction, got %.6f", gotS)
	}
}

func TestEvaluateProtectWeakFlowBE(t *testing.T) {
	m := NewManager(Config{})
	dec := m.EvaluateProtect(ProtectInput{
		Side: "BUY", Entry: 100, Stop: 98, Mark: 101, MFER: 0.6, MAER: 0.2, WeakFlow: true, UnrealizedPct: 6.0,
	})
	if !dec.MoveStopToBE {
		t.Fatalf("expected BE arm on weak flow")
	}
}

func TestEvaluateProtectLiqSpikePartial(t *testing.T) {
	m := NewManager(Config{LiqSpikePartialPct: 0.4})
	dec := m.EvaluateProtect(ProtectInput{
		Side: "BUY", Entry: 100, Stop: 98, Mark: 101, LiqSpike: true, UnrealizedPct: 0.5,
	})
	if dec.PartialExitPct <= 0 {
		t.Fatalf("expected partial exit on liquidation spike")
	}
}

func TestEvaluateProtectProfitGiveback(t *testing.T) {
	m := NewManager(Config{ProfitLockArmR: 0.6, ProfitGivebackPct: 0.25})
	dec := m.EvaluateProtect(ProtectInput{
		Side:          "BUY",
		Entry:         100,
		Stop:          98,
		Mark:          100.1,
		MFER:          0.8,
		MAER:          0.4,
		WeakFlow:      true,
		UnrealizedPct: 0.10,
	})
	if dec.FullExit || !dec.MoveStopToBE || !dec.TightenStop || dec.Reason != "PROFIT_GIVEBACK_TIGHTEN" {
		t.Fatalf("expected profit giveback tighten decision, got %+v", dec)
	}
}

func TestEvaluateProtectProfitGivebackTightensAfterConfirm(t *testing.T) {
	m := NewManager(Config{ProfitLockArmR: 0.6, ProfitGivebackPct: 0.25, TightenAfterConfirm: true, ProfitLockTightenR: 0.35})
	dec := m.EvaluateProtect(ProtectInput{
		Side:          "BUY",
		Entry:         100,
		Stop:          98,
		Mark:          100.1,
		MFER:          0.8,
		MAER:          0.4,
		WeakFlow:      true,
		UnrealizedPct: 0.10,
		HitTP1:        true,
	})
	if dec.FullExit || !dec.TightenStop || !dec.MoveStopToBE || dec.Reason != "PROFIT_GIVEBACK_TIGHTEN" {
		t.Fatalf("expected tighten-after-confirm decision, got %+v", dec)
	}
}

func TestEvaluateProtectProfitGivebackManageOnlyPrefersTighten(t *testing.T) {
	t.Setenv("LIVE_EXIT_SOFT_SIGNALS_MANAGE_ONLY", "1")
	m := NewManager(Config{ProfitLockArmR: 0.6, ProfitGivebackPct: 0.25})
	dec := m.EvaluateProtect(ProtectInput{
		Side:               "BUY",
		Entry:              100,
		Stop:               98,
		Mark:               100.05,
		MFER:               0.8,
		MAER:               0.5,
		WeakFlow:           true,
		UnrealizedPct:      0.10,
		HTFTrendPersistent: false,
		HTFTrendFailed:     false,
	})
	if dec.FullExit {
		t.Fatalf("expected manage-only profit giveback to tighten, not full exit: %+v", dec)
	}
	if !dec.TightenStop || !dec.MoveStopToBE {
		t.Fatalf("expected tighten+BE in manage-only profit giveback mode, got %+v", dec)
	}
}

func TestEvaluateProtectSponsoredSkipsEarlyNoFollowThrough(t *testing.T) {
	m := NewManager(Config{
		NoFollowThroughBars:    6,
		NoFollowThroughMinMFER: 0.3,
		NoFollowThroughMinMAER: 0.8,
		SponsorshipGraceMin:    45,
	})
	dec := m.EvaluateProtect(ProtectInput{
		Side: "BUY", Entry: 100, Stop: 98, Mark: 99.6,
		BarsHeld: 20, MFER: 0.1, MAER: 1.0, Sponsored: true,
	})
	if dec.FullExit {
		t.Fatalf("expected sponsored trade to survive no-follow-through grace, got %+v", dec)
	}
}

func TestEvaluateProtectUnsponsoredTighten(t *testing.T) {
	m := NewManager(Config{
		UnsponsoredTightenR:   0.15,
		UnsponsoredWeakStreak: 2,
	})
	dec := m.EvaluateProtect(ProtectInput{
		Side: "BUY", Entry: 100, Stop: 98, Mark: 101.2,
		HitTP1: true, WeakSponsorStreak: 2, Sponsored: false,
	})
	if !dec.TightenStop || !dec.MoveStopToBE || dec.Reason != "RUNNER_UNSPONSORED_TIGHTEN" {
		t.Fatalf("expected unsponsored tighten decision, got %+v", dec)
	}
}

func TestEvaluateProtectLegacyStarterReasonUsesNormalManagement(t *testing.T) {
	m := NewManager(Config{StarterStabilizeBars: 6})
	dec := m.EvaluateProtect(ProtectInput{
		Side:          "BUY",
		Entry:         100,
		Stop:          98,
		Mark:          101,
		StarterEntry:  true,
		EntryReason:   "breakout_retest",
		BarsHeld:      2,
		WeakFlow:      true,
		LiqSpike:      true,
		UnrealizedPct: 0.4,
	})
	if dec.Reason == "STARTER_INITIAL_PROTECT_ONLY" {
		t.Fatalf("expected starter-only management guard to be removed, got %+v", dec)
	}
}

func TestEvaluateProtect_InstantBELockAtHalfR(t *testing.T) {
	t.Setenv("LIVE_EXIT_BE_LOCK_R", "0.5")
	m := NewManager(Config{})
	dec := m.EvaluateProtect(ProtectInput{
		Side: "BUY", Entry: 100, Stop: 98, Mark: 101, MFER: 0.5, UnrealizedPct: 1.0,
	})
	if !dec.MoveStopToBE || !dec.TightenStop {
		t.Fatalf("expected immediate BE lock, got %+v", dec)
	}
	if dec.TightenToPrice < 100 {
		t.Fatalf("expected BE lock at/above entry, got %.8f", dec.TightenToPrice)
	}
}

func TestEvaluateProtect_EarlyTrailMonotonic(t *testing.T) {
	t.Setenv("LIVE_EXIT_EARLY_TRAIL_R", "1.0")
	t.Setenv("LIVE_EXIT_EARLY_TRAIL_LOCK_FRAC", "0.30")
	m := NewManager(Config{})
	dec := m.EvaluateProtect(ProtectInput{
		Side: "BUY", Entry: 100, Stop: 98, Mark: 104, MFER: 1.2, UnrealizedPct: 4.0,
	})
	if !dec.TightenStop {
		t.Fatalf("expected early trail tighten, got %+v", dec)
	}
	if dec.TightenToPrice < 100.72 {
		t.Fatalf("expected at least 30%% lock from peak move, got %.8f", dec.TightenToPrice)
	}
}

func TestEvaluateProtect_EarlyProfitTrailOverridesInstantBELockReason(t *testing.T) {
	t.Setenv("LIVE_EXIT_BE_LOCK_R", "0.5")
	t.Setenv("LIVE_EXIT_EARLY_TRAIL_R", "1.0")
	t.Setenv("LIVE_EXIT_EARLY_TRAIL_LOCK_FRAC", "0.30")
	m := NewManager(Config{})
	dec := m.EvaluateProtect(ProtectInput{
		Side:          "BUY",
		Entry:         100,
		Stop:          98,
		Mark:          104,
		MFER:          1.2,
		UnrealizedPct: 4.0,
	})
	if !dec.TightenStop {
		t.Fatalf("expected tighten stop, got %+v", dec)
	}
	if dec.Reason != "early_profit_trail" {
		t.Fatalf("expected reason early_profit_trail, got %+v", dec)
	}
}

func TestStrongerExitReason_PrefersTrailOverBE(t *testing.T) {
	got := strongerExitReason("instant_be_lock", "early_profit_trail")
	if got != "early_profit_trail" {
		t.Fatalf("expected early_profit_trail, got %q", got)
	}
}

func TestStrongerExitReason_DoesNotDowngradeTrailToFallback(t *testing.T) {
	got := strongerExitReason("early_profit_trail", "PROTECT_BE_FALLBACK")
	if got != "early_profit_trail" {
		t.Fatalf("expected early_profit_trail, got %q", got)
	}
}

func TestEvaluateProtect_WinnerLockedSoftExitConvertsToTighten(t *testing.T) {
	t.Setenv("LIVE_EXIT_SOFT_SIGNALS_MANAGE_ONLY", "0")
	m := NewManager(Config{ProfitLockArmR: 0.6, ProfitGivebackPct: 0.25})
	dec := m.EvaluateProtect(ProtectInput{
		Side:               "BUY",
		Entry:              100,
		Stop:               98,
		Mark:               100.1,
		MFER:               0.8,
		MAER:               0.5,
		WeakFlow:           true,
		UnrealizedPct:      0.10,
		WinnerLifecycle:    "winner_locked",
		HTFTrendPersistent: false,
	})
	if dec.FullExit {
		t.Fatalf("expected winner_locked soft exit to avoid hard close, got %+v", dec)
	}
	if !dec.TightenStop {
		t.Fatalf("expected winner_locked soft exit to tighten, got %+v", dec)
	}
}
