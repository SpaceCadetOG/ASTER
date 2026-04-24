package execution

type WinnerLifecycle string

const (
	WinnerLifecycleStarter      WinnerLifecycle = "starter"
	WinnerLifecycleProofArmed   WinnerLifecycle = "proof_armed"
	WinnerLifecycleWinnerLocked WinnerLifecycle = "winner_locked"
	WinnerLifecycleRunner       WinnerLifecycle = "runner"
	WinnerLifecycleLateTrail    WinnerLifecycle = "late_trail"
	WinnerLifecycleFailed       WinnerLifecycle = "failed"
)

type WinnerLifecycleInput struct {
	MaxR             float64
	CurrentR         float64
	ProofObserved    bool
	MatureTrend      bool
	TrailingActive   bool
	RealInvalidation bool
	WinnerReversion  bool
}

func NormalizeWinnerLifecycle(raw string) WinnerLifecycle {
	switch WinnerLifecycle(raw) {
	case WinnerLifecycleStarter,
		WinnerLifecycleProofArmed,
		WinnerLifecycleWinnerLocked,
		WinnerLifecycleRunner,
		WinnerLifecycleLateTrail,
		WinnerLifecycleFailed:
		return WinnerLifecycle(raw)
	default:
		return WinnerLifecycleStarter
	}
}

func ResolveWinnerLifecycle(current WinnerLifecycle, in WinnerLifecycleInput) WinnerLifecycle {
	stage := NormalizeWinnerLifecycle(string(current))
	if stage == WinnerLifecycleFailed {
		return stage
	}
	if stage != WinnerLifecycleStarter && (in.RealInvalidation || in.WinnerReversion) {
		return WinnerLifecycleFailed
	}
	if in.ProofObserved {
		if stage == WinnerLifecycleStarter {
			stage = WinnerLifecycleProofArmed
		}
		if stage == WinnerLifecycleProofArmed && in.MaxR >= envFloatExit("LIVE_EXIT_BE_LOCK_R", 0.5) {
			stage = WinnerLifecycleWinnerLocked
		}
		if stage == WinnerLifecycleWinnerLocked && in.MaxR >= envFloatExit("LIVE_EXIT_EARLY_TRAIL_R", 1.0) {
			stage = WinnerLifecycleRunner
		}
		if stage == WinnerLifecycleRunner && (in.TrailingActive || in.MatureTrend) {
			stage = WinnerLifecycleLateTrail
		}
	}
	return stage
}

func winnerProofObserved(maxR, currentR float64, advancedReady, hitTP1, hitTP2, hitTP3 bool) bool {
	if hitTP1 || hitTP2 || hitTP3 || advancedReady {
		return true
	}
	minProofR := envFloatExit("LIVE_EXIT_FIRST_PROOF_R", 0.15)
	return maxR >= minProofR || currentR >= minProofR
}
