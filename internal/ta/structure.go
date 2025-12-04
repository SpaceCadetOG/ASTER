package ta

// Swing leg between alternating pivots (H→L or L→H).
type Swing struct {
	FromIdx int
	ToIdx   int
	FromT   int64
	ToT     int64
	FromP   float64
	ToP     float64
	Dir     string  // "up" or "down"
	Range   float64 // abs(ToP-FromP)
	Pct     float64 // 100*Range/FromP signed from From->To
}

// BuildSwings expects alternating pivot types; consecutive same-type pivots are coalesced by extremity.
func BuildSwings(pivs []Pivot) []Swing {
	if len(pivs) < 2 {
		return nil
	}
	// coalesce consecutive same-type pivots: keep more extreme one
	clean := make([]Pivot, 0, len(pivs))
	for _, p := range pivs {
		n := len(clean)
		if n == 0 {
			clean = append(clean, p)
			continue
		}
		prev := clean[n-1]
		if prev.Typ == p.Typ {
			switch p.Typ {
			case "H":
				if p.P > prev.P {
					clean[n-1] = p
				}
			case "L":
				if p.P < prev.P {
					clean[n-1] = p
				}
			}
			continue
		}
		clean = append(clean, p)
	}
	if len(clean) < 2 {
		return nil
	}

	out := make([]Swing, 0, len(clean)-1)
	for i := 1; i < len(clean); i++ {
		a, b := clean[i-1], clean[i]
		dir := "up"
		if b.P < a.P {
			dir = "down"
		}
		diff := b.P - a.P
		if diff < 0 {
			diff = -diff
		}
		pct := 0.0
		if a.P != 0 {
			pct = 100.0 * (b.P - a.P) / a.P
		}
		out = append(out, Swing{
			FromIdx: a.Idx, ToIdx: b.Idx,
			FromT: a.T, ToT: b.T,
			FromP: a.P, ToP: b.P,
			Dir: dir, Range: diff, Pct: pct,
		})
	}
	return out
}

func PivotsAndSwings(bars []interface {
	TUnix() int64
	High() float64
	Low() float64
}, left, right int) []Pivot {
	// Not used (we keep the types.Candle version in pivots.go), left for future generic use if needed.
	return nil
}
