package execution

import (
	"math"
	"strings"
)

type StopTemplate string

const (
	StopTemplateContinuationImpulse StopTemplate = "continuation_impulse"
	StopTemplateReclaimPullback     StopTemplate = "reclaim_pullback"
	StopTemplateReversalExhaustion  StopTemplate = "reversal_exhaustion"
	StopTemplateMeanRevertRotation  StopTemplate = "mean_revert_rotation"
)

type HybridStopConfig struct {
	Enabled               bool
	TemplateMode          string
	ATRMultCont           float64
	ATRMultPullback       float64
	ATRMultReversal       float64
	ATRMultMeanRevert     float64
	SweepBufferBps        float64
	MinWidthPct           float64
	MaxWidthPct           float64
	MinRRToTP1            float64
	SoftRejectEnable      bool
	SoftRejectMaxWidthPct float64
	SoftRejectMinRRToTP1  float64
}

type HybridStopInput struct {
	Side           string
	Entry          float64
	SignalStop     float64
	StructureLow   float64
	StructureHigh  float64
	SessionVWAP    float64
	EMA9           float64
	ATR            float64
	TargetPrice    float64
	Template       StopTemplate
	EliteCandidate bool
}

type HybridStopResult struct {
	Enabled         bool
	Template        StopTemplate
	StopPrice       float64
	StopReason      string
	StopDistancePct float64
	StopDistanceR   float64
	Rejected        bool
	RejectReason    string
	StarterOnly     bool
}

func DefaultHybridStopConfig() HybridStopConfig {
	return HybridStopConfig{
		Enabled:               false,
		TemplateMode:          "off",
		ATRMultCont:           1.35,
		ATRMultPullback:       1.10,
		ATRMultReversal:       1.65,
		ATRMultMeanRevert:     1.20,
		SweepBufferBps:        18,
		MinWidthPct:           0.25,
		MaxWidthPct:           8.00,
		MinRRToTP1:            1.00,
		SoftRejectEnable:      false,
		SoftRejectMaxWidthPct: 12.0,
		SoftRejectMinRRToTP1:  0.65,
	}
}

func ComputeHybridStop(cfg HybridStopConfig, in HybridStopInput) HybridStopResult {
	res := HybridStopResult{
		Enabled:  cfg.Enabled,
		Template: in.Template,
	}
	if !cfg.Enabled {
		return res
	}
	side := strings.ToUpper(strings.TrimSpace(in.Side))
	if in.Entry <= 0 || (side != "BUY" && side != "SELL") {
		res.Rejected = true
		res.RejectReason = "hybrid_stop_invalid_input"
		return res
	}
	if res.Template == "" {
		res.Template = StopTemplateReclaimPullback
	}
	minWidthPct := cfg.MinWidthPct / 100.0
	maxWidthPct := cfg.MaxWidthPct / 100.0
	if minWidthPct < 0 {
		minWidthPct = 0
	}
	if maxWidthPct <= 0 {
		maxWidthPct = DefaultHybridStopConfig().MaxWidthPct / 100.0
	}

	stopPrice, reason := structureAnchor(side, in)
	if stopPrice <= 0 {
		stopPrice = atrStop(side, in.Entry, in.ATR, atrMultForTemplate(cfg, res.Template))
		reason = "atr_fallback"
	}
	if stopPrice <= 0 {
		res.Rejected = true
		res.RejectReason = "hybrid_stop_missing_anchor"
		return res
	}

	bufferPct := cfg.SweepBufferBps / 10000.0
	if bufferPct > 0 {
		if side == "BUY" {
			stopPrice *= 1 - bufferPct
		} else {
			stopPrice *= 1 + bufferPct
		}
		reason += "+sweep_buffer"
	}

	atrFloor := atrStop(side, in.Entry, in.ATR, atrMultForTemplate(cfg, res.Template))
	if atrFloor > 0 {
		if side == "BUY" && stopPrice > atrFloor {
			stopPrice = atrFloor
			reason += "+atr_floor"
		}
		if side == "SELL" && stopPrice < atrFloor {
			stopPrice = atrFloor
			reason += "+atr_floor"
		}
	}

	if side == "BUY" && stopPrice >= in.Entry {
		stopPrice = in.Entry * (1 - maxFloat(minWidthPct, 0.001))
		reason += "+entry_guard"
	}
	if side == "SELL" && stopPrice <= in.Entry {
		stopPrice = in.Entry * (1 + maxFloat(minWidthPct, 0.001))
		reason += "+entry_guard"
	}

	dist := math.Abs(in.Entry - stopPrice)
	distPct := dist / in.Entry
	if minWidthPct > 0 && distPct < minWidthPct {
		if side == "BUY" {
			stopPrice = in.Entry * (1 - minWidthPct)
		} else {
			stopPrice = in.Entry * (1 + minWidthPct)
		}
		dist = math.Abs(in.Entry - stopPrice)
		distPct = dist / in.Entry
		reason += "+min_width"
	}
	if maxWidthPct > 0 && distPct > maxWidthPct {
		if side == "BUY" {
			stopPrice = in.Entry * (1 - maxWidthPct)
		} else {
			stopPrice = in.Entry * (1 + maxWidthPct)
		}
		dist = math.Abs(in.Entry - stopPrice)
		distPct = dist / in.Entry
		reason += "+max_width"
	}
	if in.TargetPrice > 0 && cfg.MinRRToTP1 > 0 {
		reward := math.Abs(in.TargetPrice - in.Entry)
		rr := reward / maxFloat(dist, 1e-9)
		if rr < cfg.MinRRToTP1 {
			reason += "+rr_low"
		}
	}

	res.StopPrice = stopPrice
	res.StopReason = reason
	res.StopDistanceR = dist
	res.StopDistancePct = distPct * 100.0
	return res
}

func appendRejectReason(cur, reason string) string {
	if reason == "" {
		return cur
	}
	if cur == "" {
		return reason
	}
	if strings.Contains(cur, reason) {
		return cur
	}
	return cur + "," + reason
}

func atrMultForTemplate(cfg HybridStopConfig, template StopTemplate) float64 {
	switch template {
	case StopTemplateContinuationImpulse:
		return positiveOr(cfg.ATRMultCont, DefaultHybridStopConfig().ATRMultCont)
	case StopTemplateReclaimPullback:
		return positiveOr(cfg.ATRMultPullback, DefaultHybridStopConfig().ATRMultPullback)
	case StopTemplateReversalExhaustion:
		return positiveOr(cfg.ATRMultReversal, DefaultHybridStopConfig().ATRMultReversal)
	case StopTemplateMeanRevertRotation:
		return positiveOr(cfg.ATRMultMeanRevert, DefaultHybridStopConfig().ATRMultMeanRevert)
	default:
		return positiveOr(cfg.ATRMultPullback, DefaultHybridStopConfig().ATRMultPullback)
	}
}

func atrStop(side string, entry, atr, atrMult float64) float64 {
	if entry <= 0 || atr <= 0 || atrMult <= 0 {
		return 0
	}
	dist := atr * atrMult
	if dist <= 0 {
		return 0
	}
	if strings.EqualFold(side, "BUY") {
		return entry - dist
	}
	return entry + dist
}

func structureAnchor(side string, in HybridStopInput) (float64, string) {
	if strings.EqualFold(side, "BUY") {
		cands := []struct {
			price  float64
			reason string
		}{
			{price: belowEntry(in.SignalStop, in.Entry), reason: "signal_stop"},
			{price: belowEntry(in.StructureLow, in.Entry), reason: "structure_low"},
			{price: belowEntry(in.SessionVWAP, in.Entry), reason: "session_vwap"},
			{price: belowEntry(in.EMA9, in.Entry), reason: "ema9"},
		}
		best := 0.0
		bestReason := ""
		for _, c := range cands {
			if c.price <= 0 {
				continue
			}
			if best == 0 || c.price < best {
				best = c.price
				bestReason = c.reason
			}
		}
		return best, bestReason
	}

	cands := []struct {
		price  float64
		reason string
	}{
		{price: aboveEntry(in.SignalStop, in.Entry), reason: "signal_stop"},
		{price: aboveEntry(in.StructureHigh, in.Entry), reason: "structure_high"},
		{price: aboveEntry(in.SessionVWAP, in.Entry), reason: "session_vwap"},
		{price: aboveEntry(in.EMA9, in.Entry), reason: "ema9"},
	}
	best := 0.0
	bestReason := ""
	for _, c := range cands {
		if c.price <= 0 {
			continue
		}
		if best == 0 || c.price > best {
			best = c.price
			bestReason = c.reason
		}
	}
	return best, bestReason
}

func belowEntry(px, entry float64) float64 {
	if px > 0 && px < entry {
		return px
	}
	return 0
}

func aboveEntry(px, entry float64) float64 {
	if px > 0 && px > entry {
		return px
	}
	return 0
}

func positiveOr(v, fallback float64) float64 {
	if v > 0 {
		return v
	}
	return fallback
}
