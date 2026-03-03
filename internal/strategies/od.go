package strategies

import "go-machine/internal/features"

type OpenDrive struct{}

func (s OpenDrive) Name() string { return "od" }

func (s OpenDrive) Eval(ctx Context) Signal {
	cands := ctx.Candles
	if len(cands) < 4 {
		return Signal{Name: s.Name()}
	}
	snap := ctx.Snapshot
	open := snap.Anchors.SessionOpen[snap.Anchors.Session]
	if open <= 0 {
		return Signal{Name: s.Name()}
	}
	// last 3 candles directional displacement
	last3 := cands[len(cands)-3:]
	up, dn := 0, 0
	for _, c := range last3 {
		if c.C > c.O {
			up++
		} else if c.C < c.O {
			dn++
		}
	}
	side := features.SideLong
	if dn > up {
		side = features.SideShort
	}
	entry := cands[len(cands)-1].C
	if side == features.SideLong && entry < open {
		return Signal{Name: s.Name()}
	}
	if side == features.SideShort && entry > open {
		return Signal{Name: s.Name()}
	}
	stop := open
	if side == features.SideLong {
		stop = min(stop, min(last3[0].L, min(last3[1].L, last3[2].L)))
	} else {
		stop = max(stop, max(last3[0].H, max(last3[1].H, last3[2].H)))
	}
	tp1, tp2 := rrTargets(entry, stop, side)
	return Signal{Active: tp1 > 0, Name: s.Name(), Side: side, Entry: entry, Stop: stop, TP1: tp1, TP2: tp2, Confidence: clamp01(0.55), Tags: []string{"open_drive", snap.Anchors.Session}, Invalidation: "lose drive origin"}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
