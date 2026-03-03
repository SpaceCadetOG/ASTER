package strategies

import "go-machine/internal/features"

type BOSPB struct{}

func (s BOSPB) Name() string { return "bos_pb" }

func (s BOSPB) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	c := snap.Candle
	if snap.Structure.Trend == features.TrendRange {
		return Signal{Name: s.Name()}
	}
	if len(snap.OBs) == 0 && len(snap.FVGs) == 0 {
		return Signal{Name: s.Name()}
	}
	side := features.SideLong
	if snap.Structure.Trend == features.TrendBear {
		side = features.SideShort
	}
	// Prefer OB rejection, else FVG leave.
	for i := len(snap.OBs) - 1; i >= 0; i-- {
		ob := snap.OBs[i]
		if !ob.Rejected || ob.Side != side {
			continue
		}
		entry := c.C
		stop := ob.Low
		if side == features.SideShort {
			stop = ob.High
		}
		tp1, tp2 := rrTargets(entry, stop, side)
		return Signal{Active: tp1 > 0, Name: s.Name(), Side: side, Entry: entry, Stop: stop, TP1: tp1, TP2: tp2, Confidence: clamp01(0.62), Tags: []string{"bos", "pullback", "ob"}, Invalidation: "zone fails"}
	}
	for i := len(snap.FVGs) - 1; i >= 0; i-- {
		z := snap.FVGs[i]
		if z.Side != side || !z.Mitigated {
			continue
		}
		entry := c.C
		stop := z.Low
		if side == features.SideShort {
			stop = z.High
		}
		tp1, tp2 := rrTargets(entry, stop, side)
		return Signal{Active: tp1 > 0, Name: s.Name(), Side: side, Entry: entry, Stop: stop, TP1: tp1, TP2: tp2, Confidence: clamp01(0.58), Tags: []string{"bos", "pullback", "fvg"}, Invalidation: "fvg invalidated"}
	}
	return Signal{Name: s.Name()}
}
