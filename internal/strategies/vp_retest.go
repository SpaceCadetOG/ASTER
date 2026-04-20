package strategies

import (
	"math"
	"os"
	"strconv"
	"time"
)

type VPRetestVariant string

const (
	VPRetestTrend        VPRetestVariant = "trend"
	VPRetestAccumulation VPRetestVariant = "accumulation"
	VPRetestRejection    VPRetestVariant = "rejection"
)

type VPRetestStrategy struct{}

func (s VPRetestStrategy) ID() StrategyID {
	return StrategyVPRetest
}

func (s VPRetestStrategy) Detect(ctx StrategyContext) (*EntryIntent, bool) {
	if !envBoolVP("LIVE_STRAT_VP_ENABLE", true) {
		return nil, false
	}
	if ctx.VolumeProfile.POC <= 0 {
		return nil, false
	}
	if ctx.VolumeProfile.WidthBps > envFloatVP("LIVE_STRAT_VP_ZONE_WIDTH_BPS_MAX", 120.0) {
		return nil, false
	}
	if ctx.CandidateScore < envFloatVP("LIVE_STRAT_VP_MIN_SCORE", 68.0) {
		return nil, false
	}
	variant, side, ok := classifyVPRetest(ctx)
	if !ok {
		return nil, false
	}
	trigger, stop, ok := buildVPTriggerStop(ctx, variant, side)
	if !ok {
		return nil, false
	}
	risk := math.Abs(trigger - stop)
	if risk <= 0 {
		return nil, false
	}
	targets := buildVPTargets(trigger, risk, side, ctx)
	intent := &EntryIntent{
		Strategy:     StrategyVPRetest,
		Symbol:       ctx.Symbol,
		Side:         side,
		Timeframe:    "5m",
		Confidence:   normalizedVPConfidence(ctx, variant),
		Score:        ctx.CandidateScore,
		TriggerPrice: trigger,
		Invalidation: stop,
		StopPrice:    stop,
		Targets:      targets,
		TimeStopBars: envIntVP("LIVE_STRAT_VP_TIME_STOP_BARS", 8),
		ReasonCodes: []string{
			"vp_retest",
			"vp_variant_" + string(variant),
		},
		RequiresConfirm: []string{},
		Features: map[string]float64{
			"candidate_score": ctx.CandidateScore,
			"vp_poc":          ctx.VolumeProfile.POC,
			"vp_vah":          ctx.VolumeProfile.ValueAreaHigh,
			"vp_val":          ctx.VolumeProfile.ValueAreaLow,
			"vp_width_bps":    ctx.VolumeProfile.WidthBps,
			"spread_bps":      ctx.SpreadBps,
			"volume_ratio":    ctx.VolumeRatio,
			"oi_change_pct":   ctx.OIChangePct,
		},
		CreatedAt: time.Now().UTC(),
	}
	switch variant {
	case VPRetestTrend, VPRetestAccumulation, VPRetestRejection:
		intent.RequiresConfirm = maybeRequireFlowConfirmVP("LIVE_FLOW_REQUIRE_FOR_VP", true)
	}
	return intent, true
}

func classifyVPRetest(ctx StrategyContext) (VPRetestVariant, Side, bool) {
	if ctx.Trend.TF15mDir == "up" && ctx.MarkPrice >= ctx.VolumeProfile.ValueAreaLow && ctx.MarkPrice <= ctx.VolumeProfile.POC {
		return VPRetestTrend, SideLong, true
	}
	if ctx.Trend.TF15mDir == "down" && ctx.MarkPrice <= ctx.VolumeProfile.ValueAreaHigh && ctx.MarkPrice >= ctx.VolumeProfile.POC {
		return VPRetestTrend, SideShort, true
	}
	if ctx.MarketRegime == "balance" || ctx.MarketRegime == "rotation" || ctx.MarketRegime == "rotating" {
		if ctx.MarkPrice >= ctx.VolumeProfile.ValueAreaLow && ctx.MarkPrice <= ctx.VolumeProfile.POC {
			return VPRetestAccumulation, SideLong, true
		}
		if ctx.MarkPrice <= ctx.VolumeProfile.ValueAreaHigh && ctx.MarkPrice >= ctx.VolumeProfile.POC {
			return VPRetestAccumulation, SideShort, true
		}
	}
	if ctx.Flow.AbsorptionBull || ctx.Flow.DeltaDivBull {
		return VPRetestRejection, SideLong, true
	}
	if ctx.Flow.AbsorptionBear || ctx.Flow.DeltaDivBear {
		return VPRetestRejection, SideShort, true
	}
	return "", "", false
}

func buildVPTriggerStop(ctx StrategyContext, variant VPRetestVariant, side Side) (float64, float64, bool) {
	switch side {
	case SideLong:
		trigger := math.Max(ctx.MarkPrice, ctx.VolumeProfile.POC)
		stop := ctx.VolumeProfile.ValueAreaLow
		if ctx.Trend.CompressionLow > 0 && ctx.Trend.CompressionLow < trigger && ctx.Trend.CompressionLow > stop {
			stop = ctx.Trend.CompressionLow
		}
		if stop <= 0 || trigger <= stop {
			return 0, 0, false
		}
		return trigger, stop, true
	case SideShort:
		trigger := math.Min(ctx.MarkPrice, ctx.VolumeProfile.POC)
		stop := ctx.VolumeProfile.ValueAreaHigh
		if ctx.Trend.CompressionHigh > 0 && ctx.Trend.CompressionHigh > trigger && ctx.Trend.CompressionHigh < stop {
			stop = ctx.Trend.CompressionHigh
		}
		if stop <= 0 || stop <= trigger {
			return 0, 0, false
		}
		return trigger, stop, true
	}
	return 0, 0, false
}

func buildVPTargets(trigger, risk float64, side Side, ctx StrategyContext) []Target {
	switch side {
	case SideLong:
		tp1 := math.Max(trigger+risk, ctx.VolumeProfile.ValueAreaHigh)
		return []Target{
			{Label: "tp1", Price: tp1, Size: 0.40},
			{Label: "tp2", Price: trigger + 2.0*risk, Size: 0.35},
			{Label: "runner", Price: trigger + 3.0*risk, Size: 0.25},
		}
	case SideShort:
		tp1 := math.Min(trigger-risk, ctx.VolumeProfile.ValueAreaLow)
		return []Target{
			{Label: "tp1", Price: tp1, Size: 0.40},
			{Label: "tp2", Price: trigger - 2.0*risk, Size: 0.35},
			{Label: "runner", Price: trigger - 3.0*risk, Size: 0.25},
		}
	}
	return nil
}

func normalizedVPConfidence(ctx StrategyContext, variant VPRetestVariant) float64 {
	score := ctx.CandidateScore / 100.0
	if ctx.Flow.Confidence > 0.55 {
		score += 0.10
	}
	if variant == VPRetestRejection {
		score += 0.05
	}
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}
	return score
}

func maybeRequireFlowConfirmVP(envKey string, def bool) []string {
	if envBoolVP(envKey, def) {
		return []string{"flow_confirm"}
	}
	return []string{}
}

func envBoolVP(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return def
	}
}

func envFloatVP(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envIntVP(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

