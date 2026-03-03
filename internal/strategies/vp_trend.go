package strategies

import "go-machine/internal/features"

type VPTrendRetest struct{}

func (s VPTrendRetest) Name() string { return "vp_trend" }

func (s VPTrendRetest) Eval(ctx Context) Signal {
	c := ctx.Candles
	if len(c) < 24 {
		return Signal{Name: s.Name()}
	}
	snap := ctx.Snapshot
	if snap.Structure.Trend == features.TrendRange {
		return Signal{Name: s.Name()}
	}
	side := sideFromTrend(snap.Structure.Trend)
	seg := c[len(c)-16:]
	lo, hi := seg[0].L, seg[0].H
	for i := range seg {
		if seg[i].L < lo {
			lo = seg[i].L
		}
		if seg[i].H > hi {
			hi = seg[i].H
		}
	}
	lvl, ok := snap.VP.LevelAtHeaviestInRange(lo, hi)
	if !ok || lvl <= 0 {
		return Signal{Name: s.Name()}
	}
	last := c[len(c)-1]
	touchTol := last.C * 0.0012
	if touchTol <= 0 {
		touchTol = 1e-8
	}
	if absf(last.C-lvl) > touchTol {
		return Signal{Name: s.Name()}
	}
	entry := last.C
	stop := lo
	if side == features.SideShort {
		stop = hi
	}
	tp1, tp2 := rrTargets(entry, stop, side)
	conf := 0.60
	if snap.Flow.VolumeSpike {
		conf += 0.06
	}
	if side == features.SideLong && snap.Flow.WhaleDelta1m > 0 {
		conf += 0.09
	}
	if side == features.SideShort && snap.Flow.WhaleDelta1m < 0 {
		conf += 0.09
	}
	return Signal{
		Active:       tp1 > 0,
		Name:         s.Name(),
		Side:         side,
		Entry:        entry,
		Stop:         stop,
		TP1:          tp1,
		TP2:          tp2,
		Confidence:   clamp01(conf),
		Tags:         []string{"vp", "trend", "retest"},
		Invalidation: "trend retest fails",
		VPSetup:      "trend_retest",
		VPLevel:      lvl,
		Ts:           last.Ts,
	}
}
