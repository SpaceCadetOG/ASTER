package ta

import "go-machine/internal/types"

// Fractal pivots: bar i is a high if H[i] is max over [i-left, i+right]; low if min.
type Pivot struct {
	Idx int
	T   int64   // unix ms
	P   float64 // price
	Typ string  // "H" or "L"
}

func DetectFractalPivots(bars []types.Candle, left, right int) []Pivot {
	if len(bars) < left+right+1 {
		return nil
	}
	out := make([]Pivot, 0, len(bars)/5)
	for i := left; i < len(bars)-right; i++ {
		hi, lo := true, true
		for j := i - left; j <= i+right; j++ {
			if bars[j].H > bars[i].H {
				hi = false
			}
			if bars[j].L < bars[i].L {
				lo = false
			}
			if !hi && !lo {
				break
			}
		}
		if hi {
			out = append(out, Pivot{Idx: i, T: bars[i].T.UnixMilli(), P: bars[i].H, Typ: "H"})
		} else if lo {
			out = append(out, Pivot{Idx: i, T: bars[i].T.UnixMilli(), P: bars[i].L, Typ: "L"})
		}
	}
	return out
}
