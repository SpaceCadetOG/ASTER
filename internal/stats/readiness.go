package stats

import "fmt"

type ReadinessConfig struct {
	MinTrades         int
	MinExpectancy     float64
	MinProfitFactor   float64
	MaxDrawdown       float64
	MinWatchToEntry   float64
	MaxLeaderMisses   int
	MaxFundingDragAbs float64
	MaxSlippageAbs    float64
}

type ReadinessReport struct {
	Ready   bool
	Reasons []string
}

func DefaultReadinessConfig() ReadinessConfig {
	return ReadinessConfig{
		MinTrades:         80,
		MinExpectancy:     0.10,
		MinProfitFactor:   1.15,
		MaxDrawdown:       35.0,
		MinWatchToEntry:   1.0,
		MaxLeaderMisses:   12,
		MaxFundingDragAbs: 15.0,
		MaxSlippageAbs:    20.0,
	}
}

func EvaluateReadiness(r Report, cfg ReadinessConfig) ReadinessReport {
	out := ReadinessReport{Ready: true}
	if r.TotalTrades < cfg.MinTrades {
		out.Ready = false
		out.Reasons = append(out.Reasons, fmt.Sprintf("trades %d < %d", r.TotalTrades, cfg.MinTrades))
	}
	if r.Expectancy < cfg.MinExpectancy {
		out.Ready = false
		out.Reasons = append(out.Reasons, fmt.Sprintf("expectancy %.2f < %.2f", r.Expectancy, cfg.MinExpectancy))
	}
	if r.ProfitFac < cfg.MinProfitFactor {
		out.Ready = false
		out.Reasons = append(out.Reasons, fmt.Sprintf("profit_factor %.2f < %.2f", r.ProfitFac, cfg.MinProfitFactor))
	}
	if cfg.MaxDrawdown > 0 && r.MaxDrawdown > cfg.MaxDrawdown {
		out.Ready = false
		out.Reasons = append(out.Reasons, fmt.Sprintf("max_drawdown %.2f > %.2f", r.MaxDrawdown, cfg.MaxDrawdown))
	}
	if cfg.MinWatchToEntry > 0 && r.WatchToEntry < cfg.MinWatchToEntry {
		out.Ready = false
		out.Reasons = append(out.Reasons, fmt.Sprintf("watch_to_entry %.2f%% < %.2f%%", r.WatchToEntry, cfg.MinWatchToEntry))
	}
	if cfg.MaxLeaderMisses > 0 && r.LeaderMiss > cfg.MaxLeaderMisses {
		out.Ready = false
		out.Reasons = append(out.Reasons, fmt.Sprintf("leader_misses %d > %d", r.LeaderMiss, cfg.MaxLeaderMisses))
	}
	if cfg.MaxFundingDragAbs > 0 && -r.FundingPnL > cfg.MaxFundingDragAbs {
		out.Ready = false
		out.Reasons = append(out.Reasons, fmt.Sprintf("funding_drag %.2f > %.2f", -r.FundingPnL, cfg.MaxFundingDragAbs))
	}
	if cfg.MaxSlippageAbs > 0 && r.Slippage > cfg.MaxSlippageAbs {
		out.Ready = false
		out.Reasons = append(out.Reasons, fmt.Sprintf("slippage %.2f > %.2f", r.Slippage, cfg.MaxSlippageAbs))
	}
	return out
}
