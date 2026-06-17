package execution

import "testing"

func TestResolveWinnerLifecycleProgression(t *testing.T) {
	t.Setenv("LIVE_EXIT_BE_LOCK_R", "0.5")
	t.Setenv("LIVE_EXIT_EARLY_TRAIL_R", "1.0")

	stage := ResolveWinnerLifecycle(WinnerLifecycleStarter, WinnerLifecycleInput{
		MaxR:          0.2,
		CurrentR:      0.2,
		ProofObserved: true,
	})
	if stage != WinnerLifecycleProofArmed {
		t.Fatalf("expected proof_armed, got %s", stage)
	}

	stage = ResolveWinnerLifecycle(stage, WinnerLifecycleInput{
		MaxR:          0.6,
		CurrentR:      0.4,
		ProofObserved: true,
	})
	if stage != WinnerLifecycleWinnerLocked {
		t.Fatalf("expected winner_locked, got %s", stage)
	}

	stage = ResolveWinnerLifecycle(stage, WinnerLifecycleInput{
		MaxR:          1.1,
		CurrentR:      0.9,
		ProofObserved: true,
	})
	if stage != WinnerLifecycleRunner {
		t.Fatalf("expected runner, got %s", stage)
	}

	stage = ResolveWinnerLifecycle(stage, WinnerLifecycleInput{
		MaxR:           1.4,
		CurrentR:       1.0,
		ProofObserved:  true,
		TrailingActive: true,
	})
	if stage != WinnerLifecycleLateTrail {
		t.Fatalf("expected late_trail, got %s", stage)
	}
}

func TestResolveWinnerLifecycleFailsOnlyFromWinnerStates(t *testing.T) {
	stage := ResolveWinnerLifecycle(WinnerLifecycleStarter, WinnerLifecycleInput{
		RealInvalidation: true,
	})
	if stage != WinnerLifecycleStarter {
		t.Fatalf("expected starter to remain starter, got %s", stage)
	}

	stage = ResolveWinnerLifecycle(WinnerLifecycleRunner, WinnerLifecycleInput{
		ProofObserved:    true,
		RealInvalidation: true,
	})
	if stage != WinnerLifecycleFailed {
		t.Fatalf("expected runner invalidation to fail, got %s", stage)
	}
}
