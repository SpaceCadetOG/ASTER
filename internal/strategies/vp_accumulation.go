package strategies

import "go-machine/internal/features"

type VPAccumulation struct{}

func (s VPAccumulation) Name() string { return "vp_accumulation" }

func (s VPAccumulation) Eval(ctx Context) Signal {
	c := ctx.Candles
	if len(c) < 30 {
		return Signal{Name: s.Name()}
	}
	snap := ctx.Snapshot
	last := c[len(c)-1]
	look := c[len(c)-20:]
	lo, hi := look[0].L, look[0].H
	for i := range look {
		if look[i].L < lo {
			lo = look[i].L
		}
		if look[i].H > hi {
			hi = look[i].H
		}
	}
	if hi <= lo {
		return Signal{Name: s.Name()}
	}
	lvl, ok := snap.VP.LevelAtHeaviestInRange(lo, hi)
	if !ok || lvl <= 0 {
		return Signal{Name: s.Name()}
	}
	side := features.SideLong
	impulseUp := c[len(c)-1].C > c[len(c)-6].C
	if !impulseUp {
		side = features.SideShort
	}
	touchTol := last.C * 0.0015
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
	conf := 0.54
	if snap.Flow.VolumeSpike {
		conf += 0.07
	}
	if side == features.SideLong && snap.Flow.WhaleDelta1m > 0 {
		conf += 0.07
	}
	if side == features.SideShort && snap.Flow.WhaleDelta1m < 0 {
		conf += 0.07
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
		Tags:         []string{"vp", "accumulation", "retest"},
		Invalidation: "lose accumulation zone",
		VPSetup:      "accumulation",
		VPLevel:      lvl,
		Ts:           last.Ts,
	}
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
