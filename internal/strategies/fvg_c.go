package strategies

import "go-machine/internal/features"

type FVGC struct{}

func (s FVGC) Name() string { return "fvg_c" }

func (s FVGC) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	c := snap.Candle
	for i := len(snap.FVGs) - 1; i >= 0; i-- {
		z := snap.FVGs[i]
		if !z.Mitigated {
			continue
		}
		if z.Side == features.SideLong && snap.Structure.Trend == features.TrendBear {
			continue
		}
		if z.Side == features.SideShort && snap.Structure.Trend == features.TrendBull {
			continue
		}
		entry := c.C
		stop := z.Low
		if z.Side == features.SideShort {
			stop = z.High
		}
		tp1, tp2 := rrTargets(entry, stop, z.Side)
		return Signal{Active: tp1 > 0, Name: s.Name(), Side: z.Side, Entry: entry, Stop: stop, TP1: tp1, TP2: tp2, Confidence: clamp01(0.57), Tags: []string{"fvg", "continuation"}, Invalidation: "lose gap edge"}
	}
	return Signal{Name: s.Name()}
}
