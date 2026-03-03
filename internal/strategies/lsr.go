package strategies

import "go-machine/internal/features"

type LSR struct{}

func (s LSR) Name() string { return "lsr" }

func (s LSR) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	if snap.Sweep == nil || !snap.Sweep.CloseBackInside {
		return Signal{Name: s.Name()}
	}
	c := snap.Candle
	side := snap.Sweep.Side
	entry := c.C
	stop := c.L
	if side == features.SideShort {
		stop = c.H
	}
	tp1, tp2 := rrTargets(entry, stop, side)
	conf := 0.45 + 0.1*snap.Sweep.Strength
	if snap.Flow.VolumeSpike {
		conf += 0.15
	}
	if side == features.SideLong && snap.Flow.WhaleDelta1m > 0 {
		conf += 0.2
	}
	if side == features.SideShort && snap.Flow.WhaleDelta1m < 0 {
		conf += 0.2
	}
	return Signal{
		Active:       tp1 > 0 && stop > 0,
		Name:         s.Name(),
		Side:         side,
		Entry:        entry,
		Stop:         stop,
		TP1:          tp1,
		TP2:          tp2,
		Confidence:   clamp01(conf),
		Tags:         []string{"sweep", "liquidity"},
		Invalidation: "close beyond sweep extreme",
		Ts:           c.Ts,
	}
}
