package strategies

import "go-machine/internal/features"

type FailedAuction struct{}

func (s FailedAuction) Name() string { return "fa" }

func (s FailedAuction) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	if snap.Sweep == nil {
		return Signal{Name: s.Name()}
	}
	// Require pool count >=2 nearby.
	ok := false
	for _, p := range snap.Pools {
		if p.Count >= 2 && p.Side == snap.Sweep.Side {
			ok = true
			break
		}
	}
	if !ok {
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
	return Signal{Active: tp1 > 0, Name: s.Name(), Side: side, Entry: entry, Stop: stop, TP1: tp1, TP2: tp2, Confidence: clamp01(0.63), Tags: []string{"failed_auction", "sweep"}, Invalidation: "reclaim opposite side"}
}
