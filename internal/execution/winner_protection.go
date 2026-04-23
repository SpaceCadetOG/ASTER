package execution

import (
	"math"
	"os"
	"strconv"
	"strings"
)

type WinnerProtectionState struct {
	MaxR              float64
	BEArmed           bool
	LockedR           float64
	TP1Taken          bool
	LastProtectReason string
}

type WinnerProtectionDecision struct {
	MoveStop        bool
	NewStop         float64
	TakePartial     bool
	PartialFraction float64
	Reason          string
}

func EvaluateWinnerProtection(
	strategyID string,
	side string,
	entry float64,
	stop float64,
	mark float64,
	maxR float64,
	tp1Taken bool,
	structureValidated bool,
) WinnerProtectionDecision {
	currentR := currentRMultiple(side, entry, stop, mark)
	if currentR <= 0 {
		return WinnerProtectionDecision{}
	}
	if maxR >= envFloatWP("LIVE_EXIT_EARLY_TRAIL_R", 1.0) {
		return WinnerProtectionDecision{
			MoveStop: true,
			NewStop:  trailLockFromPeakWP(side, entry, mark, envFloatWP("LIVE_EXIT_EARLY_TRAIL_LOCK_FRAC", 0.30)),
			Reason:   "early_profit_trail",
		}
	}
	if maxR >= envFloatWP("LIVE_EXIT_BE_LOCK_R", 0.5) {
		return WinnerProtectionDecision{
			MoveStop: true,
			NewStop:  breakEvenPlus(side, entry, stop, 0.00),
			Reason:   "instant_be_lock",
		}
	}
	switch strings.ToLower(strings.TrimSpace(strategyID)) {
	case "impulse_continuation", "entry_now_long", "entry_now_short":
		if currentR >= envFloatWP("LIVE_EXIT_FORCE_PARTIAL_AT_R", 1.0) && !tp1Taken {
			return WinnerProtectionDecision{
				MoveStop:        true,
				NewStop:         breakEvenPlus(side, entry, stop, 0.10),
				TakePartial:     true,
				PartialFraction: clampWP(envFloatWP("LIVE_EXIT_FORCE_PARTIAL_FRACTION", 0.50), 0.05, 0.95),
				Reason:          "fast_tp1_be",
			}
		}
		if currentR >= envFloatWP("LIVE_EXIT_LOCK_HALF_R_AT", 1.5) {
			return WinnerProtectionDecision{
				MoveStop: true,
				NewStop:  lockR(side, entry, stop, 0.50),
				Reason:   "lock_half_r",
			}
		}
		if currentR >= envFloatWP("LIVE_EXIT_LOCK_ONE_R_AT", 2.0) {
			return WinnerProtectionDecision{
				MoveStop: true,
				NewStop:  lockR(side, entry, stop, 1.00),
				Reason:   "lock_one_r",
			}
		}
	case "anchored_vwap_pullback":
		if currentR >= 1.0 && structureValidated {
			return WinnerProtectionDecision{
				MoveStop: true,
				NewStop:  breakEvenPlus(side, entry, stop, 0.05),
				Reason:   "avwap_be",
			}
		}
		if currentR >= 2.0 {
			return WinnerProtectionDecision{
				MoveStop: true,
				NewStop:  lockR(side, entry, stop, 0.75),
				Reason:   "avwap_lock",
			}
		}
	case "vp_retest":
		if currentR >= 1.0 && structureValidated {
			return WinnerProtectionDecision{
				MoveStop: true,
				NewStop:  breakEvenPlus(side, entry, stop, 0.05),
				Reason:   "vp_be",
			}
		}
		if currentR >= 2.0 {
			return WinnerProtectionDecision{
				MoveStop: true,
				NewStop:  lockR(side, entry, stop, 0.50),
				Reason:   "vp_lock",
			}
		}
	}
	if maxR >= envFloatWP("LIVE_EXIT_PROOF_R", 0.5) {
		return WinnerProtectionDecision{
			MoveStop: true,
			NewStop:  breakEvenPlus(side, entry, stop, 0.02),
			Reason:   "proof_r_be",
		}
	}
	return WinnerProtectionDecision{}
}

func trailLockFromPeakWP(side string, entry, mark, frac float64) float64 {
	if frac <= 0 || frac > 1 {
		frac = 0.30
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		if mark <= entry {
			return entry
		}
		return entry + (mark-entry)*frac
	}
	if mark >= entry {
		return entry
	}
	return entry - (entry-mark)*frac
}

func currentRMultiple(side string, entry, stop, mark float64) float64 {
	risk := math.Abs(entry - stop)
	if entry <= 0 || stop <= 0 || mark <= 0 || risk <= 0 {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		return (mark - entry) / risk
	}
	return (entry - mark) / risk
}

func breakEvenPlus(side string, entry, stop, lockR float64) float64 {
	risk := math.Abs(entry - stop)
	if risk <= 0 {
		return stop
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		return entry + risk*lockR
	}
	return entry - risk*lockR
}

func lockR(side string, entry, stop, lockedR float64) float64 {
	return breakEvenPlus(side, entry, stop, lockedR)
}

func envFloatWP(key string, def float64) float64 {
	v := os.Getenv(key)
	if strings.TrimSpace(v) == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func clampWP(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
