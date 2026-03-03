package strategies

import "go-machine/internal/features"

type VPReversal struct{}

func (s VPReversal) Name() string { return "vp_reversal" }

func (s VPReversal) Eval(ctx Context) Signal {
	c := ctx.Candles
	if len(c) < 12 {
		return Signal{Name: s.Name()}
	}
	last := c[len(c)-1]
	prev := c[len(c)-2]
	vp := ctx.Snapshot.VP
	if vp.POCPrice <= 0 {
		return Signal{Name: s.Name()}
	}
	// Decisive failure + retest proxy around POC/VA edges.
	side := features.SideLong
	if prev.C > vp.POCPrice && last.C < vp.POCPrice {
		side = features.SideShort
	} else if prev.C < vp.POCPrice && last.C > vp.POCPrice {
		side = features.SideLong
	} else {
		return Signal{Name: s.Name()}
	}
	stop := last.L
	if side == features.SideShort {
		stop = last.H
	}
	tp1, tp2 := rrTargets(last.C, stop, side)
	conf := 0.52
	if side == features.SideLong && ctx.Snapshot.Flow.WhaleDelta1m > 0 {
		conf += 0.06
	}
	if side == features.SideShort && ctx.Snapshot.Flow.WhaleDelta1m < 0 {
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
		Tags:         []string{"vp", "reversal", "role_flip"},
		Invalidation: "flip level reclaimed",
		VPSetup:      "reversal",
		VPLevel:      vp.POCPrice,
		Ts:           last.Ts,
	}
}
