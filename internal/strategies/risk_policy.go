package strategies

import (
	"strings"

	"go-machine/internal/features"
)

type StopMode string
type TargetMode string

const (
	StopModeFixed  StopMode = "fixed"
	StopModeVP     StopMode = "vp"
	StopModeHybrid StopMode = "hybrid"

	TargetModeRR     TargetMode = "rr"
	TargetModeVP     TargetMode = "vp"
	TargetModeHybrid TargetMode = "hybrid"
)

type RiskPolicyConfig struct {
	StopMode             StopMode
	TargetMode           TargetMode
	FixedStopPct         float64
	FixedStopPctPullback float64
	FixedStopPctReversal float64
	VPMinShare           float64
	VPFrontRunPct        float64
	MinTargetDistancePct float64
	MinRMultiple         float64
	MinRMultiplePullback float64
	MinRMultipleReversal float64
}

func DefaultRiskPolicy() RiskPolicyConfig {
	return RiskPolicyConfig{
		StopMode:             StopModeHybrid,
		TargetMode:           TargetModeHybrid,
		FixedStopPct:         0.60,
		FixedStopPctPullback: 0.45,
		FixedStopPctReversal: 0.80,
		VPMinShare:           0.10,
		VPFrontRunPct:        0.10,
		MinTargetDistancePct: 0.10,
		MinRMultiple:         1.20,
		MinRMultiplePullback: 1.00,
		MinRMultipleReversal: 1.10,
	}
}

func ApplyRiskPolicy(sig Signal, snap features.Snapshot, cfg RiskPolicyConfig) Signal {
	if !sig.Active || sig.Entry <= 0 {
		return sig
	}
	if cfg.FixedStopPct <= 0 {
		cfg.FixedStopPct = 0.60
	}
	if cfg.FixedStopPctPullback <= 0 {
		cfg.FixedStopPctPullback = 0.45
	}
	if cfg.FixedStopPctReversal <= 0 {
		cfg.FixedStopPctReversal = 0.80
	}
	if cfg.VPMinShare <= 0 || cfg.VPMinShare >= 1 {
		cfg.VPMinShare = 0.10
	}
	if cfg.VPFrontRunPct <= 0 {
		cfg.VPFrontRunPct = 0.10
	}
	if cfg.MinRMultiple <= 0 {
		cfg.MinRMultiple = 1.20
	}
	if cfg.MinRMultiplePullback <= 0 {
		cfg.MinRMultiplePullback = 1.00
	}
	if cfg.MinRMultipleReversal <= 0 {
		cfg.MinRMultipleReversal = 1.10
	}

	template := riskTemplateForSignal(sig)
	stopPct := cfg.FixedStopPct
	minRMultiple := cfg.MinRMultiple
	switch template {
	case "pullback":
		stopPct = cfg.FixedStopPctPullback
		minRMultiple = cfg.MinRMultiplePullback
	case "reversal":
		stopPct = cfg.FixedStopPctReversal
		minRMultiple = cfg.MinRMultipleReversal
	}
	stopFixed := stopByFixedPct(sig.Entry, sig.Side, stopPct)
	stopVP := stopByVP(sig, snap)
	stop := sig.Stop
	switch cfg.StopMode {
	case StopModeFixed:
		stop = stopFixed
	case StopModeVP:
		if stopVP > 0 {
			stop = stopVP
		}
	case StopModeHybrid:
		stop = stopFixed
		if stopVP > 0 {
			// In hybrid mode we keep the wider protective stop.
			if riskDistance(sig.Entry, stopVP, sig.Side) > riskDistance(sig.Entry, stopFixed, sig.Side) {
				stop = stopVP
			}
		}
	}
	if stop <= 0 {
		stop = sig.Stop
	}
	tp1RR, tp2RR := rrTargets(sig.Entry, stop, sig.Side)
	tp1VP, tp2VP, tgtLevel := targetsByVP(sig.Entry, sig.Side, snap, cfg.VPMinShare, cfg.VPFrontRunPct)

	tp1, tp2 := tp1RR, tp2RR
	switch cfg.TargetMode {
	case TargetModeRR:
		tp1, tp2 = tp1RR, tp2RR
	case TargetModeVP:
		if tp1VP > 0 {
			tp1, tp2 = tp1VP, tp2VP
		}
	case TargetModeHybrid:
		// Prefer VP target when it gives sufficient R multiple.
		if tp1VP > 0 {
			risk := riskDistance(sig.Entry, stop, sig.Side)
			rewardVP := rewardDistance(sig.Entry, tp1VP, sig.Side)
			if risk > 0 && rewardVP/risk >= minRMultiple {
				tp1, tp2 = tp1VP, tp2VP
			}
		}
	}
	sig.Stop = stop
	sig.TP1 = tp1
	sig.TP2 = tp2
	sig.VPTargetLevel = tgtLevel
	sig.StopMode = string(cfg.StopMode)
	sig.TargetMode = string(cfg.TargetMode)
	return sig
}

func riskTemplateForSignal(sig Signal) string {
	name := sig.Name
	switch {
	case hasAnyTag(sig.Tags, "reversal", "role_flip", "failed_auction"):
		return "reversal"
	case hasAnyTag(sig.Tags, "pullback", "retest", "confluence"):
		return "pullback"
	case strings.Contains(name, "reversal"), strings.Contains(name, "flip"), strings.Contains(name, "failed_auction"):
		return "reversal"
	case strings.Contains(name, "pullback"), strings.Contains(name, "retest"), strings.Contains(name, "vwap"):
		return "pullback"
	default:
		return "continuation"
	}
}

func hasAnyTag(tags []string, want ...string) bool {
	for _, tag := range tags {
		for _, needle := range want {
			if strings.EqualFold(strings.TrimSpace(tag), strings.TrimSpace(needle)) {
				return true
			}
		}
	}
	return false
}

func stopByFixedPct(entry float64, side features.Side, stopPct float64) float64 {
	d := stopPct / 100.0
	if d <= 0 {
		d = 0.006
	}
	if side == features.SideLong {
		return entry * (1 - d)
	}
	return entry * (1 + d)
}

func stopByVP(sig Signal, snap features.Snapshot) float64 {
	vp := snap.VP
	if sig.Side == features.SideLong {
		if vp.NearestLVNBelow > 0 {
			return vp.NearestLVNBelow
		}
		if vp.VAL > 0 && vp.VAL < sig.Entry {
			return vp.VAL
		}
	} else {
		if vp.NearestLVNAbove > 0 {
			return vp.NearestLVNAbove
		}
		if vp.VAH > sig.Entry {
			return vp.VAH
		}
	}
	return 0
}

func targetsByVP(entry float64, side features.Side, snap features.Snapshot, minShare, frontRunPct float64) (float64, float64, float64) {
	lvl, ok := snap.VP.FirstSignificantOpposingLevel(entry, side, minShare)
	if !ok || lvl <= 0 {
		return 0, 0, 0
	}
	adj := frontRunPct / 100.0
	tp1 := lvl
	if side == features.SideLong {
		tp1 = lvl * (1 - adj)
	} else {
		tp1 = lvl * (1 + adj)
	}
	// TP2 as an extension from TP1 with the same distance from entry.
	dist := rewardDistance(entry, tp1, side)
	tp2 := tp1
	if side == features.SideLong {
		tp2 = tp1 + dist
	} else {
		tp2 = tp1 - dist
	}
	return tp1, tp2, lvl
}

func riskDistance(entry, stop float64, side features.Side) float64 {
	if side == features.SideLong {
		return entry - stop
	}
	return stop - entry
}

func rewardDistance(entry, target float64, side features.Side) float64 {
	if side == features.SideLong {
		return target - entry
	}
	return entry - target
}
