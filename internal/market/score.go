package market

import (
	"math"
	"sort"
	"strings"
	"time"
)

func ScoreMarket(m Market) Scored {
	cfg := CurrentRankConfig()
	rawBase := baseLongRawScore(m)
	mom := evalMomentum(m, "long", cfg)
	rev := evalReversalSignal(mom, "long")
	integ := evalIntegrity(m, cfg, time.Now().UTC())
	exec := evalExecution(m, cfg)

	raw := rawBase + mom.Score - integ.IntegrityPenalty - exec.Penalty
	raw = round2(raw)
	norm := normalizeRawScore(raw, cfg.NormMinRaw, cfg.NormMaxRaw)
	grade := gradeFromNormalized(norm)

	execFrac := 0.0
	if cfg.ExecMaxPenalty > 0 {
		execFrac = exec.Penalty / cfg.ExecMaxPenalty
	}
	conf := confidenceFromParts(cfg, integ.Completeness, mom.Agreement, execFrac)
	scoreOut := raw
	if cfg.EnableNormalizedScore && cfg.UseNormalizedInScore {
		scoreOut = norm
	}
	flags := append([]string{}, integ.Flags...)
	flags = append(flags, exec.Flags...)

	return Scored{
		Market:            m,
		Score:             scoreOut,
		RawScore:          raw,
		NormalizedScore:   norm,
		Grade:             grade,
		Completeness:      integ.Completeness,
		IntegrityPenalty:  round2(integ.IntegrityPenalty),
		ExecutionPenalty:  round2(exec.Penalty),
		SpreadBps:         round2(exec.SpreadBps),
		EstSlippageBps:    round2(exec.EstSlippageBps),
		TopBookUSDVal:     round2(exec.TopBookUSD),
		Momentum5m:        round2(mom.M5m * 100),
		Momentum30m:       round2(mom.M30m * 100),
		Momentum4h:        round2(mom.M4h * 100),
		Momentum24h:       round2(mom.M24h * 100),
		MomentumAgreement: round2(mom.Agreement),
		Regime:            mom.Regime,
		Confidence:        round2(conf),
		Uncertainty:       round2(1 - conf),
		DataFlags:         flags,
		ReversalReadyLong: rev.Ready && strings.EqualFold(rev.Direction, "SELL"),
		ReversalSignal:    rev,
	}
}

func ScoreAndFilter(mkts []Market) []Scored {
	out := make([]Scored, 0, len(mkts))
	cfg := CurrentRankConfig()
	for _, m := range mkts {
		s := ScoreMarket(m)
		ok, reason := Eligible(m)
		if ok && cfg.EnableDataIntegrity && s.Completeness < cfg.MinCompleteness {
			ok = false
			reason = "low_completeness"
		}
		if ok && cfg.EnableConfidence && s.Confidence < cfg.MinConfidence {
			ok = false
			reason = "low_confidence"
		}
		s.Eligible, s.Reason = ok, reason
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

func TopN(scored []Scored, n int) []Scored {
	filtered := make([]Scored, 0, len(scored))
	for _, s := range scored {
		if s.Eligible {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) <= n {
		return filtered
	}
	return filtered[:n]
}

const (
	W_CHANGE_SHORT  = 1.0
	W_LOG_VOL_SHORT = 8.0
	W_LOG_OI_SHORT  = 3.0
	FUND_K_SHORT    = 500.0
	CROWD_PEN_SHORT = 10.0
)

func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

func EligibleShort(m Market) (bool, string) {
	if m.VolumeUSD < 1_000_000 {
		return false, "low volume"
	}
	if m.Change24h > -3.0 {
		return false, "weak down move"
	}
	return true, ""
}

func ScoreAndFilterShort(ms []Market) []Scored {
	out := make([]Scored, 0, len(ms))
	cfg := CurrentRankConfig()
	for _, m := range ms {
		ok, reason := EligibleShort(m)
		rawBase := baseShortRawScore(m)
		mom := evalMomentum(m, "short", cfg)
		rev := evalReversalSignal(mom, "short")
		integ := evalIntegrity(m, cfg, time.Now().UTC())
		exec := evalExecution(m, cfg)
		raw := rawBase + mom.Score - integ.IntegrityPenalty - exec.Penalty
		raw = round2(raw)
		norm := normalizeRawScore(raw, cfg.NormMinRaw, cfg.NormMaxRaw)
		grade := gradeFromNormalized(norm)
		execFrac := 0.0
		if cfg.ExecMaxPenalty > 0 {
			execFrac = exec.Penalty / cfg.ExecMaxPenalty
		}
		conf := confidenceFromParts(cfg, integ.Completeness, mom.Agreement, execFrac)
		scoreOut := raw
		if cfg.EnableNormalizedScore && cfg.UseNormalizedInScore {
			scoreOut = norm
		}
		flags := append([]string{}, integ.Flags...)
		flags = append(flags, exec.Flags...)
		if ok && cfg.EnableDataIntegrity && integ.Completeness < cfg.MinCompleteness {
			ok = false
			reason = "low_completeness"
		}
		if ok && cfg.EnableConfidence && conf < cfg.MinConfidence {
			ok = false
			reason = "low_confidence"
		}
		out = append(out, Scored{
			Market:             m,
			Eligible:           ok,
			Reason:             reason,
			Score:              scoreOut,
			RawScore:           raw,
			NormalizedScore:    norm,
			Grade:              grade,
			Completeness:       integ.Completeness,
			IntegrityPenalty:   round2(integ.IntegrityPenalty),
			ExecutionPenalty:   round2(exec.Penalty),
			SpreadBps:          round2(exec.SpreadBps),
			EstSlippageBps:     round2(exec.EstSlippageBps),
			TopBookUSDVal:      round2(exec.TopBookUSD),
			Momentum5m:         round2(mom.M5m * 100),
			Momentum30m:        round2(mom.M30m * 100),
			Momentum4h:         round2(mom.M4h * 100),
			Momentum24h:        round2(mom.M24h * 100),
			MomentumAgreement:  round2(mom.Agreement),
			Regime:             mom.Regime,
			Confidence:         round2(conf),
			Uncertainty:        round2(1 - conf),
			DataFlags:          flags,
			ReversalReadyShort: rev.Ready && strings.EqualFold(rev.Direction, "BUY"),
			ReversalSignal:     rev,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// FallbackGrade converts a scanner score + 24h move into a coarse letter grade.
// - score: your computed momentum/quality score (0..~120 typical)
// - delta24h: 24h % change (e.g., +42.5 => 42.5, -17.3 => -17.3)
// We add a small bonus for big absolute 24h moves so rippers get nudged up.
// FallbackGrade assigns an A–D grade using the scanner's core stats
// when no confluence grade is available.
// FallbackGrade keeps backward compatibility (assumes long bias).
func FallbackGrade(score, delta24h float64) string {
	return FallbackGradeDirectional(score, delta24h, "long")
}

// FallbackGradeDirectional returns A+/A/B/C/D/N/A based on momentum score
// and 24h move, with bias for LONG vs SHORT.
func FallbackGradeDirectional(score, delta24h float64, side string) string {
	side = strings.ToLower(side)
	if score < 0 {
		score = 0
	}
	if score > 150 {
		score = 150
	}

	// Movement that helps the current bias:
	// - LONG likes positive delta
	// - SHORT likes negative delta (so we flip the sign)
	mover := delta24h
	if side == "short" {
		mover = -delta24h
	}

	// Volatility bonus: reward strong directional movers (cap ±15)
	// +0.25 per 1% move feels responsive but not crazy.
	bonus := math.Max(-15, math.Min(15, mover*0.25))
	adj := score + bonus

	switch {
	case adj >= 100:
		return "A+"
	case adj >= 95:
		return "A"
	case adj >= 89:
		return "B"
	case adj >= 79:
		return "C"
	case adj >= 69:
		return "D"
	default:
		return "N/A"
	}
}

func gradeFromNormalized(score float64) string {
	switch {
	case score >= 92:
		return "A+"
	case score >= 85:
		return "A"
	case score >= 75:
		return "B"
	case score >= 62:
		return "C"
	case score >= 50:
		return "D"
	default:
		return "N/A"
	}
}

func confidenceFromParts(cfg RankConfig, completeness, agreement, execPenaltyFrac float64) float64 {
	c := 0.45 + 0.35*clamp01(completeness) + 0.20*clamp01(agreement) - 0.15*clamp01(execPenaltyFrac)
	return clamp(c, 0.05, 1.0)
}

func baseLongRawScore(m Market) float64 {
	score := 0.0
	score += W_CHANGE * m.Change24h
	v := math.Max(m.VolumeUSD, 1)
	score += W_LOG_VOL * math.Log10(v)
	if m.OIUSD != nil {
		oi := math.Max(*m.OIUSD, 1)
		score += W_LOG_OI * math.Log10(oi)
	}
	if m.FundingRate != nil {
		score -= math.Abs(*m.FundingRate) * FUND_K
		if *m.FundingRate > 0.001 && m.LongsPct != nil && *m.LongsPct > CROWD_LONG_P {
			score -= CROWD_PENALTY
		}
	}
	return score
}

func baseShortRawScore(m Market) float64 {
	score := W_CHANGE_SHORT * math.Max(0, -m.Change24h)
	v := math.Max(m.VolumeUSD, 1)
	score += W_LOG_VOL_SHORT * math.Log10(v)
	if m.OIUSD != nil {
		oi := math.Max(*m.OIUSD, 1)
		score += W_LOG_OI_SHORT * math.Log10(oi)
	}
	if m.FundingRate != nil {
		score -= math.Abs(*m.FundingRate) * FUND_K_SHORT
		if *m.FundingRate < -0.001 {
			score -= CROWD_PEN_SHORT
		}
	}
	return score
}

// GradeColor returns ANSI color for terminal output (console).
func GradeColor(grade string) string {
	switch grade {
	case "A+":
		return "\033[38;5;135m" // purple
	case "A":
		return "\033[38;5;45m" // 💎 crystal blue
	case "B":
		return "\033[32m" // green
	case "C":
		return "\033[38;5;214m" // amber/orange
	case "D":
		return "\033[31m" // red
	default:
		return "\033[37m" // gray
	}
}

func ResetColor() string { return "\033[0m" }

// --- Web/UI colors ---

// GradeHex: text color for each grade badge.
func GradeHex(grade string) string {
	switch grade {
	case "A+":
		return "#a64eff" // purple
	case "A":
		return "#4fc3ff" // 💎 crystal blue
	case "B":
		return "#00cc66" // green
	case "C":
		return "#ffb347" // amber/orange
	case "D":
		return "#ff3333" // red
	default:
		return "#9aa0a6" // gray
	}
}

// GradeBG: background tint for each grade badge (subtle transparent color).
func GradeBG(grade string) string {
	switch grade {
	case "A+":
		return "rgba(166, 78, 255, 0.15)" // purple
	case "A":
		return "rgba(79, 195, 255, 0.15)" // 💎 crystal blue glow
	case "B":
		return "rgba(0, 204, 102, 0.12)" // green
	case "C":
		return "rgba(255, 179, 71, 0.14)" // amber
	case "D":
		return "rgba(255, 51, 51, 0.14)" // red
	default:
		return "rgba(154, 160, 166, 0.10)" // gray
	}
}

// GradePalette returns both (textColorHex, backgroundCSS)
func GradePalette(grade string) (string, string) {
	return GradeHex(grade), GradeBG(grade)
}
