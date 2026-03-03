package strategies

import "go-machine/internal/features"

type VPRejection struct{}

func (s VPRejection) Name() string { return "vp_rejection" }

func (s VPRejection) Eval(ctx Context) Signal {
	c := ctx.Candles
	if len(c) < 8 {
		return Signal{Name: s.Name()}
	}
	last := c[len(c)-1]
	prev := c[len(c)-2]
	// Strong rejection proxy: large wick and reversal close.
	upWick := last.H - maxf(last.O, last.C)
	loWick := minf(last.O, last.C) - last.L
	body := absf(last.C - last.O)
	if body <= 0 {
		return Signal{Name: s.Name()}
	}
	var side features.Side
	rejLow := false
	if loWick >= body*1.2 && last.C > prev.C {
		side = features.SideLong
		rejLow = true
	} else if upWick >= body*1.2 && last.C < prev.C {
		side = features.SideShort
	} else {
		return Signal{Name: s.Name()}
	}
	lvl, ok := ctx.Snapshot.VP.LevelAtHeaviestInRange(last.L, last.H)
	if !ok || lvl <= 0 {
		return Signal{Name: s.Name()}
	}
	stop := last.L
	if side == features.SideShort {
		stop = last.H
	}
	tp1, tp2 := rrTargets(last.C, stop, side)
	conf := 0.50
	if rejLow && ctx.Snapshot.Flow.WhaleDelta1m > 0 {
		conf += 0.06
	}
	if !rejLow && ctx.Snapshot.Flow.WhaleDelta1m < 0 {
		conf += 0.06
	}
	return Signal{
		Active:       tp1 > 0,
		Name:         s.Name(),
		Side:         side,
		Entry:        last.C,
		Stop:         stop,
		TP1:          tp1,
		TP2:          tp2,
		Confidence:   clamp01(conf),
		Tags:         []string{"vp", "rejection"},
		Invalidation: "rejection level fails",
		VPSetup:      "rejection",
		VPLevel:      lvl,
		Ts:           last.Ts,
	}
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
