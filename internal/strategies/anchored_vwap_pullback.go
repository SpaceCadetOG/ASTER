package strategies

import (
	"math"
	"os"
	"strconv"
	"time"
)

type AnchoredVWAPPullbackStrategy struct{}

func (s AnchoredVWAPPullbackStrategy) ID() StrategyID {
	return StrategyAnchoredVWAPPullback
}

func (s AnchoredVWAPPullbackStrategy) Detect(ctx StrategyContext) (*EntryIntent, bool) {
	if !envBoolAVWAP("LIVE_STRAT_AVWAP_ENABLE", true) {
		return nil, false
	}
	if !ctx.AnchoredVWAP.Valid {
		return nil, false
	}
	if ctx.AnchoredVWAP.Dev1Upper <= ctx.AnchoredVWAP.Dev1Lower {
		return nil, false
	}
	requireTrendAlign := envBoolAVWAP("LIVE_STRAT_AVWAP_REQUIRE_TREND_ALIGN", true)
	maxOvershootBps := envFloatAVWAP("LIVE_STRAT_AVWAP_MAX_DEV1_OVERSHOOT_BPS", 25.0)
	minScore := envFloatAVWAP("LIVE_STRAT_AVWAP_MIN_SCORE", 70.0)
	touchMode := envStringAVWAP("LIVE_STRAT_AVWAP_TOUCH_MODE", "first_touch")
	if ctx.CandidateScore < minScore {
		return nil, false
	}
	avwap := ctx.AnchoredVWAP.VWAP
	dev1Upper := ctx.AnchoredVWAP.Dev1Upper
	dev1Lower := ctx.AnchoredVWAP.Dev1Lower
	slope := ctx.AnchoredVWAP.Slope
	if avwap <= 0 {
		return nil, false
	}

	if isLongAVWAPSetup(ctx, avwap, dev1Lower, slope, requireTrendAlign, maxOvershootBps, touchMode) {
		trigger := math.Max(ctx.MarkPrice, avwap)
		stop := math.Min(dev1Lower, ctx.Trend.CompressionLow)
		if stop <= 0 || trigger <= stop {
			stop = dev1Lower
		}
		risk := trigger - stop
		if risk <= 0 {
			return nil, false
		}
		return &EntryIntent{
			Strategy:     StrategyAnchoredVWAPPullback,
			Symbol:       ctx.Symbol,
			Side:         SideLong,
			Timeframe:    "5m",
			Confidence:   normalizedAVWAPConfidence(ctx),
			Score:        ctx.CandidateScore,
			TriggerPrice: trigger,
			Invalidation: stop,
			StopPrice:    stop,
			TimeStopBars: envIntAVWAP("LIVE_STRAT_AVWAP_TIME_STOP_BARS", 6),
			Targets: []Target{
				{Label: "tp1", Price: trigger + risk, Size: 0.40},
				{Label: "tp2", Price: math.Max(dev1Upper, trigger+(2.0*risk)), Size: 0.35},
				{Label: "runner", Price: trigger + (3.0 * risk), Size: 0.25},
			},
			ReasonCodes: []string{
				"avwap_uptrend",
				"pullback_to_avwap",
				"reaction_long",
			},
			RequiresConfirm: []string{
				"reclaim_or_hold",
			},
			Features: map[string]float64{
				"candidate_score":    ctx.CandidateScore,
				"avwap":              avwap,
				"avwap_dev1_upper":   dev1Upper,
				"avwap_dev1_lower":   dev1Lower,
				"avwap_slope":        slope,
				"avwap_distance_bps": ctx.AnchoredVWAP.DistanceBps,
				"spread_bps":         ctx.SpreadBps,
				"volume_ratio":       ctx.VolumeRatio,
				"oi_change_pct":      ctx.OIChangePct,
			},
			CreatedAt: time.Now().UTC(),
		}, true
	}
	if isShortAVWAPSetup(ctx, avwap, dev1Upper, slope, requireTrendAlign, maxOvershootBps, touchMode) {
		trigger := math.Min(ctx.MarkPrice, avwap)
		stop := math.Max(dev1Upper, ctx.Trend.CompressionHigh)
		if stop <= 0 || stop <= trigger {
			stop = dev1Upper
		}
		risk := stop - trigger
		if risk <= 0 {
			return nil, false
		}
		return &EntryIntent{
			Strategy:     StrategyAnchoredVWAPPullback,
			Symbol:       ctx.Symbol,
			Side:         SideShort,
			Timeframe:    "5m",
			Confidence:   normalizedAVWAPConfidence(ctx),
			Score:        ctx.CandidateScore,
			TriggerPrice: trigger,
			Invalidation: stop,
			StopPrice:    stop,
			TimeStopBars: envIntAVWAP("LIVE_STRAT_AVWAP_TIME_STOP_BARS", 6),
			Targets: []Target{
				{Label: "tp1", Price: trigger - risk, Size: 0.40},
				{Label: "tp2", Price: math.Min(dev1Lower, trigger-(2.0*risk)), Size: 0.35},
				{Label: "runner", Price: trigger - (3.0 * risk), Size: 0.25},
			},
			ReasonCodes: []string{
				"avwap_downtrend",
				"pullback_to_avwap",
				"reaction_short",
			},
			RequiresConfirm: []string{
				"reject_or_fail_reclaim",
			},
			Features: map[string]float64{
				"candidate_score":    ctx.CandidateScore,
				"avwap":              avwap,
				"avwap_dev1_upper":   dev1Upper,
				"avwap_dev1_lower":   dev1Lower,
				"avwap_slope":        slope,
				"avwap_distance_bps": ctx.AnchoredVWAP.DistanceBps,
				"spread_bps":         ctx.SpreadBps,
				"volume_ratio":       ctx.VolumeRatio,
				"oi_change_pct":      ctx.OIChangePct,
			},
			CreatedAt: time.Now().UTC(),
		}, true
	}
	return nil, false
}

func isLongAVWAPSetup(ctx StrategyContext, avwap, dev1Lower, slope float64, requireTrendAlign bool, maxOvershootBps float64, touchMode string) bool {
	if requireTrendAlign {
		if ctx.Trend.TF5mDir != "up" || ctx.Trend.TF15mDir != "up" {
			return false
		}
		if slope < envFloatAVWAP("LIVE_AVWAP_MIN_SLOPE", 0.0) {
			return false
		}
	}
	if ctx.MarkPrice < dev1Lower {
		overshootBps := bpsDistanceAVWAP(ctx.MarkPrice, dev1Lower)
		if overshootBps > maxOvershootBps {
			return false
		}
	}
	switch touchMode {
	case "first_touch":
		return ctx.MarkPrice >= dev1Lower && ctx.MarkPrice <= avwap*1.0025
	case "reclaim":
		return ctx.MarkPrice >= avwap
	default:
		return ctx.MarkPrice >= dev1Lower && ctx.MarkPrice <= avwap*1.0025
	}
}

func isShortAVWAPSetup(ctx StrategyContext, avwap, dev1Upper, slope float64, requireTrendAlign bool, maxOvershootBps float64, touchMode string) bool {
	if requireTrendAlign {
		if ctx.Trend.TF5mDir != "down" || ctx.Trend.TF15mDir != "down" {
			return false
		}
		if slope > -envFloatAVWAP("LIVE_AVWAP_MIN_SLOPE", 0.0) {
			return false
		}
	}
	if ctx.MarkPrice > dev1Upper {
		overshootBps := bpsDistanceAVWAP(ctx.MarkPrice, dev1Upper)
		if overshootBps > maxOvershootBps {
			return false
		}
	}
	switch touchMode {
	case "first_touch":
		return ctx.MarkPrice <= dev1Upper && ctx.MarkPrice >= avwap*0.9975
	case "reclaim":
		return ctx.MarkPrice <= avwap
	default:
		return ctx.MarkPrice <= dev1Upper && ctx.MarkPrice >= avwap*0.9975
	}
}

func normalizedAVWAPConfidence(ctx StrategyContext) float64 {
	score := ctx.CandidateScore / 100.0
	if ctx.AnchoredVWAP.Valid {
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

func bpsDistanceAVWAP(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return math.Abs((a-b)/b) * 10000.0
}

func envBoolAVWAP(key string, def bool) bool {
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

func envFloatAVWAP(key string, def float64) float64 {
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

func envIntAVWAP(key string, def int) int {
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

func envStringAVWAP(key string, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

