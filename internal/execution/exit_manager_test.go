package execution

import (
	"math"
	"testing"
)

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

func TestEvaluateProtectNoFollowThrough(t *testing.T) {
	m := NewManager(Config{NoFollowThroughBars: 6, NoFollowThroughMinMFER: 0.3, NoFollowThroughMinMAER: 0.8})
	dec := m.EvaluateProtect(ProtectInput{
		Side: "BUY", Entry: 100, Stop: 98, Mark: 99.6, BarsHeld: 8, MFER: 0.0, MAER: 1.0,
	})
	if !dec.FullExit || dec.Reason != "NO_FOLLOW_THROUGH" {
		t.Fatalf("expected full pre-stop invalidation exit, got %+v", dec)
	}
}

func TestEvaluateProtectNoFollowThroughTightensAfterEarlyProof(t *testing.T) {
	t.Setenv("LIVE_EARLY_CONTINUATION_MIN_R", "0.35")
	t.Setenv("LIVE_NO_FOLLOW_THROUGH_TIGHTEN_R", "0.10")
	m := NewManager(Config{
		NoFollowThroughBars:    6,
		NoFollowThroughMinMFER: 0.3,
		NoFollowThroughMinMAER: 0.8,
	})
	dec := m.EvaluateProtect(ProtectInput{
		Side:          "BUY",
		Entry:         100,
		Stop:          98,
		Mark:          99.6,
		BarsHeld:      8,
		MFER:          0.20,
		MAER:          1.0,
		AdvancedReady: true,
	})
	if dec.FullExit {
		t.Fatalf("expected tighten, not full exit: %+v", dec)
	}
	if !dec.TightenStop || !dec.MoveStopToBE || dec.Reason != "NO_FOLLOW_THROUGH_TIGHTEN" {
		t.Fatalf("expected no-follow-through tighten decision, got %+v", dec)
	}
}

func TestEvaluateProtectNoFollowThroughUsesHTFCautionTighten(t *testing.T) {
	t.Setenv("LIVE_NO_FOLLOW_THROUGH_TIGHTEN_R", "0.08")
	t.Setenv("LIVE_NO_FOLLOW_THROUGH_TIGHTEN_R_CAUTION", "0.06")
	m := NewManager(Config{
		NoFollowThroughBars:    6,
		NoFollowThroughMinMFER: 0.3,
		NoFollowThroughMinMAER: 0.8,
	})
	dec := m.EvaluateProtect(ProtectInput{
		Side:               "BUY",
		Entry:              100,
		Stop:               98,
		Mark:               99.6,
		BarsHeld:           8,
		MFER:               0.20,
		MAER:               1.0,
		HTFTrendPersistent: true,
		HTFTrendFailed:     false,
		HTFCaution:         true,
	})
	if dec.FullExit {
		t.Fatalf("expected tighten with HTF caution, not full exit: %+v", dec)
	}
	if !dec.TightenStop || !dec.MoveStopToBE || dec.Reason != "NO_FOLLOW_THROUGH_TIGHTEN" {
		t.Fatalf("expected no-follow-through tighten decision with HTF caution, got %+v", dec)
	}
	expected := 99.88 // tightenToR(BUY,100,98,0.06)
	if math.Abs(dec.TightenToPrice-expected) > 1e-9 {
		t.Fatalf("expected caution tighten price %.8f, got %.8f", expected, dec.TightenToPrice)
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

func TestEvaluateProtectStarterSkipsImmediateAdvancedManagement(t *testing.T) {
	m := NewManager(Config{StarterStabilizeBars: 6})
	dec := m.EvaluateProtect(ProtectInput{
		Side:          "BUY",
		Entry:         100,
		Stop:          98,
		Mark:          101,
		StarterEntry:  true,
		EntryReason:   "impulsive_long_starter",
		BarsHeld:      2,
		WeakFlow:      true,
		LiqSpike:      true,
		UnrealizedPct: 0.4,
	})
	if dec.MoveStopToBE || dec.TightenStop || dec.PartialExitPct > 0 || dec.FullExit {
		t.Fatalf("expected no immediate advanced actions for starter first attach, got %+v", dec)
	}
	if dec.Reason != "STARTER_INITIAL_PROTECT_ONLY" {
		t.Fatalf("expected starter guard reason, got %+v", dec)
	}
}

func TestEvaluateProtectStarterAdvancedAfterStabilizationOrTP1(t *testing.T) {
	m := NewManager(Config{StarterStabilizeBars: 6})
	decByBars := m.EvaluateProtect(ProtectInput{
		Side:          "BUY",
		Entry:         100,
		Stop:          98,
		Mark:          101,
		StarterEntry:  true,
		EntryReason:   "impulsive_long_starter",
		BarsHeld:      8,
		WeakFlow:      true,
		UnrealizedPct: 0.6,
		MFER:          0.8,
	})
	if decByBars.Reason == "STARTER_INITIAL_PROTECT_ONLY" {
		t.Fatalf("expected advanced management to unlock after stabilization bars")
	}

	decByTP1 := m.EvaluateProtect(ProtectInput{
		Side:          "BUY",
		Entry:         100,
		Stop:          98,
		Mark:          101,
		StarterEntry:  true,
		EntryReason:   "impulsive_long_starter",
		BarsHeld:      2,
		HitTP1:        true,
		WeakFlow:      true,
		UnrealizedPct: 0.6,
		MFER:          0.8,
	})
	if decByTP1.Reason == "STARTER_INITIAL_PROTECT_ONLY" {
		t.Fatalf("expected advanced management to unlock after TP1")
	}

	decByFlag := m.EvaluateProtect(ProtectInput{
		Side:          "BUY",
		Entry:         100,
		Stop:          98,
		Mark:          101,
		StarterEntry:  true,
		EntryReason:   "impulsive_long_starter",
		BarsHeld:      2,
		AdvancedReady: true,
		WeakFlow:      true,
		UnrealizedPct: 0.6,
		MFER:          0.8,
	})
	if decByFlag.Reason == "STARTER_INITIAL_PROTECT_ONLY" {
		t.Fatalf("expected explicit advanced-ready flag to unlock advanced management")
	}
}
