package strategies

import "go-machine/internal/features"

type OBR struct{}

func (s OBR) Name() string { return "ob_r" }

func (s OBR) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	c := snap.Candle
	for i := len(snap.OBs) - 1; i >= 0; i-- {
		ob := snap.OBs[i]
		if !ob.Rejected {
			continue
		}
		if ob.Side == features.SideLong && snap.Flow.WhaleDelta1m < 0 {
			continue
		}
		if ob.Side == features.SideShort && snap.Flow.WhaleDelta1m > 0 {
			continue
		}
		entry := c.C
		stop := ob.Low
		if ob.Side == features.SideShort {
			stop = ob.High
		}
		tp1, tp2 := rrTargets(entry, stop, ob.Side)
		return Signal{Active: tp1 > 0, Name: s.Name(), Side: ob.Side, Entry: entry, Stop: stop, TP1: tp1, TP2: tp2, Confidence: clamp01(0.6), Tags: []string{"ob", "rejection"}, Invalidation: "close through OB"}
	}
	return Signal{Name: s.Name()}
}
