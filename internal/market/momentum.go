package market

import "math"

type MomentumResult struct {
	M5m       float64
	M30m      float64
	M4h       float64
	M24h      float64
	Agreement float64
	Score     float64
	Regime    string
}

func evalMomentum(m Market, side string, cfg RankConfig) MomentumResult {
	m5 := directional(m.Change5m, side)
	m30 := directional(m.Change30m, side)
	m4h := directional(m.Change4h, side)
	m24 := directional(&m.Change24h, side)

	weights := []float64{cfg.MomW5m, cfg.MomW30m, cfg.MomW4h, cfg.MomW24h}
	values := []float64{m5, m30, m4h, m24}

	sumW := 0.0
	weighted := 0.0
	signRef := 0.0
	agreeW := 0.0
	for i, v := range values {
		w := weights[i]
		if w <= 0 {
			continue
		}
		sumW += w
		weighted += w * v
		if v != 0 {
			sgn := math.Copysign(1, v)
			if signRef == 0 {
				signRef = sgn
			}
			if sgn == signRef {
				agreeW += w
			}
		}
	}
	agreement := 0.0
	if sumW > 0 {
		agreement = clamp01(agreeW / sumW)
	}

	score := 0.0
	if cfg.EnableMomentum {
		score = clamp(weighted*cfg.MomMaxBoost, -cfg.MomMaxBoost, cfg.MomMaxBoost)
	}

	regime := "mixed"
	switch {
	case m4h > 0 && m30 > 0 && m5 > 0:
		regime = "continuation_long"
	case m4h > 0 && m30 > 0 && m5 < 0:
		regime = "countertrend_short"
	case m4h < 0 && m30 < 0 && m5 < 0:
		regime = "continuation_short"
	case m4h < 0 && m30 < 0 && m5 > 0:
		regime = "countertrend_long"
	}
	if agreement < 0.45 {
		regime = "mixed"
	}
	if agreement < 0.30 {
		regime = "exhaustion_risk"
	}

	return MomentumResult{
		M5m:       m5,
		M30m:      m30,
		M4h:       m4h,
		M24h:      m24,
		Agreement: agreement,
		Score:     score,
		Regime:    regime,
	}
}

func directional(v *float64, side string) float64 {
	if v == nil {
		return 0
	}
	x := *v / 100.0 // percentage -> roughly -1..1
	if side == "short" {
		x = -x
	}
	return clamp(x, -1, 1)
}

func evalReversalSignal(mom MomentumResult, side string) ReversalSignal {
	rs := ReversalSignal{
		Ready:     false,
		Direction: "",
		Score:     0,
		Reasons:   []string{},
	}
	side = normalizeSide(side)
	switch side {
	case "long":
		// Long book reversal short scaffold: fast impulse down against stale long continuation.
		if mom.M5m < -0.18 && mom.M30m < -0.05 && (mom.M4h > 0 || mom.M24h > 0) {
			rs.Ready = true
			rs.Direction = "SELL"
			rs.Score = clamp01((-mom.M5m * 0.55) + (-mom.M30m * 0.25) + (mom.M4h * 0.10) + (mom.M24h * 0.10))
			rs.Reasons = append(rs.Reasons, "fast_down_impulse")
			if mom.Regime == "countertrend_short" || mom.Regime == "exhaustion_risk" {
				rs.Reasons = append(rs.Reasons, "regime_countertrend_short")
			}
		}
	case "short":
		// Short book reversal long scaffold.
		if mom.M5m > 0.18 && mom.M30m > 0.05 && (mom.M4h < 0 || mom.M24h < 0) {
			rs.Ready = true
			rs.Direction = "BUY"
			rs.Score = clamp01((mom.M5m * 0.55) + (mom.M30m * 0.25) + (-mom.M4h * 0.10) + (-mom.M24h * 0.10))
			rs.Reasons = append(rs.Reasons, "fast_up_impulse")
			if mom.Regime == "countertrend_long" || mom.Regime == "exhaustion_risk" {
				rs.Reasons = append(rs.Reasons, "regime_countertrend_long")
			}
		}
	}
	return rs
}

func normalizeSide(side string) string {
	switch side {
	case "short", "SHORT":
		return "short"
	default:
		return "long"
	}
}
