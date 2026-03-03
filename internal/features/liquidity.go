package features

import "math"

type LiquidityConfig struct {
	TolBps   float64
	MinCount int
	Lookback int
}

type LiquidityEngine struct {
	cfg LiquidityConfig
}

func NewLiquidityEngine(cfg LiquidityConfig) *LiquidityEngine {
	if cfg.TolBps <= 0 {
		cfg.TolBps = 5
	}
	if cfg.MinCount <= 0 {
		cfg.MinCount = 2
	}
	if cfg.Lookback <= 0 {
		cfg.Lookback = 80
	}
	return &LiquidityEngine{cfg: cfg}
}

func (e *LiquidityEngine) Eval(c []Candle) ([]LiquidityPool, *SweepEvent) {
	if len(c) == 0 {
		return nil, nil
	}
	start := len(c) - e.cfg.Lookback
	if start < 0 {
		start = 0
	}
	segment := c[start:]
	pools := make([]LiquidityPool, 0, 8)

	for i := range segment {
		for j := i + 1; j < len(segment); j++ {
			hiA, hiB := segment[i].H, segment[j].H
			if withinBps(hiA, hiB, e.cfg.TolBps) {
				lvl := (hiA + hiB) / 2
				cnt := 2
				for k := j + 1; k < len(segment); k++ {
					if withinBps(segment[k].H, lvl, e.cfg.TolBps) {
						cnt++
					}
				}
				if cnt >= e.cfg.MinCount {
					pools = append(pools, LiquidityPool{Side: SideShort, Level: lvl, Count: cnt, First: segment[i].Ts, Last: segment[j].Ts, Active: true})
				}
			}
			loA, loB := segment[i].L, segment[j].L
			if withinBps(loA, loB, e.cfg.TolBps) {
				lvl := (loA + loB) / 2
				cnt := 2
				for k := j + 1; k < len(segment); k++ {
					if withinBps(segment[k].L, lvl, e.cfg.TolBps) {
						cnt++
					}
				}
				if cnt >= e.cfg.MinCount {
					pools = append(pools, LiquidityPool{Side: SideLong, Level: lvl, Count: cnt, First: segment[i].Ts, Last: segment[j].Ts, Active: true})
				}
			}
		}
	}
	last := c[len(c)-1]
	var best *SweepEvent
	for _, p := range pools {
		s := detectSweep(last, p)
		if s == nil {
			continue
		}
		if best == nil || s.Strength > best.Strength {
			best = s
		}
	}
	return dedupePools(pools), best
}

func detectSweep(last Candle, p LiquidityPool) *SweepEvent {
	if p.Level <= 0 {
		return nil
	}
	s := &SweepEvent{Side: p.Side, Level: p.Level, Ts: last.Ts}
	if p.Side == SideLong { // sell-side liquidity under lows, bullish sweep
		if last.L < p.Level && last.C > p.Level {
			s.CloseBackInside = true
			s.WickPct = (p.Level - last.L) / p.Level * 100
			s.Strength = s.WickPct + 0.5
			return s
		}
		return nil
	}
	if last.H > p.Level && last.C < p.Level {
		s.CloseBackInside = true
		s.WickPct = (last.H - p.Level) / p.Level * 100
		s.Strength = s.WickPct + 0.5
		return s
	}
	return nil
}

func dedupePools(in []LiquidityPool) []LiquidityPool {
	out := make([]LiquidityPool, 0, len(in))
	for _, p := range in {
		dup := false
		for _, q := range out {
			if p.Side == q.Side && withinBps(p.Level, q.Level, 3) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	return out
}

func withinBps(a, b, bps float64) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	m := (a + b) / 2
	if m == 0 {
		return false
	}
	return math.Abs(a-b)/m*10000 <= bps
}
