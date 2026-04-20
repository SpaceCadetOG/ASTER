package strategies

import (
	"math"
	"os"
	"strconv"
	"time"
)

type ImpulseContinuationStrategy struct{}

func (s ImpulseContinuationStrategy) ID() StrategyID {
	return StrategyImpulseContinuation
}

func (s ImpulseContinuationStrategy) Detect(ctx StrategyContext) (*EntryIntent, bool) {
	if !envBoolImpulse("LIVE_STRAT_IMPULSE_ENABLE", true) {
		return nil, false
	}
	minScore := envFloatImpulse("LIVE_STRAT_IMPULSE_MIN_SCORE", 75.0)
	minVolRatio := envFloatImpulse("LIVE_STRAT_IMPULSE_MIN_VOL_RATIO", 1.5)
	maxExtBps := envFloatImpulse("LIVE_STRAT_IMPULSE_MAX_EXT_BPS", 90.0)
	timeStopBars := envIntImpulse("LIVE_STRAT_IMPULSE_TIME_STOP_BARS", 4)

	if ctx.CandidateScore < minScore {
		return nil, false
	}
	if ctx.VolumeRatio < minVolRatio {
		return nil, false
	}
	if math.Abs(ctx.VWAPDistBps) > maxExtBps {
		return nil, false
	}
	if !ctx.Trend.Compression {
		return nil, false
	}

	// Long setup.
	if ctx.Trend.ImpulseUp &&
		ctx.Trend.TF5mDir == "up" &&
		ctx.Trend.TF15mDir == "up" &&
		ctx.Trend.CompressionHigh > 0 &&
		ctx.Trend.CompressionLow > 0 &&
		ctx.MarkPrice <= ctx.Trend.CompressionHigh {
		trigger := ctx.Trend.CompressionHigh
		stop := ctx.Trend.CompressionLow
		risk := trigger - stop
		if risk <= 0 {
			return nil, false
		}
		return &EntryIntent{
			Strategy:     StrategyImpulseContinuation,
			Symbol:       ctx.Symbol,
			Side:         SideLong,
			Timeframe:    "5m",
			Confidence:   normalizedImpulseConfidence(ctx),
			Score:        ctx.CandidateScore,
			TriggerPrice: trigger,
			Invalidation: stop,
			StopPrice:    stop,
			TimeStopBars: timeStopBars,
			Targets: []Target{
				{Label: "tp1", Price: trigger + risk, Size: 0.50},
				{Label: "tp2", Price: trigger + (2.0 * risk), Size: 0.30},
				{Label: "runner", Price: trigger + (3.0 * risk), Size: 0.20},
			},
			ReasonCodes: []string{
				"impulse_up",
				"compression",
				"breakout_trigger",
			},
			RequiresConfirm: []string{},
			Features: map[string]float64{
				"candidate_score":   ctx.CandidateScore,
				"volume_ratio":      ctx.VolumeRatio,
				"vwap_dist_bps":     ctx.VWAPDistBps,
				"spread_bps":        ctx.SpreadBps,
				"oi_change_pct":     ctx.OIChangePct,
				"compression_high":  ctx.Trend.CompressionHigh,
				"compression_low":   ctx.Trend.CompressionLow,
				"breakout_level":    ctx.Trend.BreakoutLevel,
				"breakdown_level":   ctx.Trend.BreakdownLevel,
			},
			CreatedAt: time.Now().UTC(),
		}, true
	}

	// Short setup.
	if ctx.Trend.ImpulseDown &&
		ctx.Trend.TF5mDir == "down" &&
		ctx.Trend.TF15mDir == "down" &&
		ctx.Trend.CompressionHigh > 0 &&
		ctx.Trend.CompressionLow > 0 &&
		ctx.MarkPrice >= ctx.Trend.CompressionLow {
		trigger := ctx.Trend.CompressionLow
		stop := ctx.Trend.CompressionHigh
		risk := stop - trigger
		if risk <= 0 {
			return nil, false
		}
		return &EntryIntent{
			Strategy:     StrategyImpulseContinuation,
			Symbol:       ctx.Symbol,
			Side:         SideShort,
			Timeframe:    "5m",
			Confidence:   normalizedImpulseConfidence(ctx),
			Score:        ctx.CandidateScore,
			TriggerPrice: trigger,
			Invalidation: stop,
			StopPrice:    stop,
			TimeStopBars: timeStopBars,
			Targets: []Target{
				{Label: "tp1", Price: trigger - risk, Size: 0.50},
				{Label: "tp2", Price: trigger - (2.0 * risk), Size: 0.30},
				{Label: "runner", Price: trigger - (3.0 * risk), Size: 0.20},
			},
			ReasonCodes: []string{
				"impulse_down",
				"compression",
				"breakdown_trigger",
			},
			RequiresConfirm: []string{},
			Features: map[string]float64{
				"candidate_score":   ctx.CandidateScore,
				"volume_ratio":      ctx.VolumeRatio,
				"vwap_dist_bps":     ctx.VWAPDistBps,
				"spread_bps":        ctx.SpreadBps,
				"oi_change_pct":     ctx.OIChangePct,
				"compression_high":  ctx.Trend.CompressionHigh,
				"compression_low":   ctx.Trend.CompressionLow,
				"breakout_level":    ctx.Trend.BreakoutLevel,
				"breakdown_level":   ctx.Trend.BreakdownLevel,
			},
			CreatedAt: time.Now().UTC(),
		}, true
	}
	return nil, false
}

func normalizedImpulseConfidence(ctx StrategyContext) float64 {
	score := ctx.CandidateScore / 100.0
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

func envBoolImpulse(key string, def bool) bool {
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

func envFloatImpulse(key string, def float64) float64 {
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

func envIntImpulse(key string, def int) int {
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

