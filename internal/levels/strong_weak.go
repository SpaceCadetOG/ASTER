package levels

import "go-machine/internal/features"

type SwingStrength struct {
	Index         int
	Price         float64
	IsHigh        bool
	Strength      float64
	Tests         int
	FastRejection bool
	Strong        bool
}

func DetectStrongWeakSwings(c []features.Candle, leftRight int, testTolPct float64) []SwingStrength {
	if len(c) < 2*leftRight+1 {
		return nil
	}
	if leftRight <= 0 {
		leftRight = 2
	}
	if testTolPct <= 0 {
		testTolPct = 0.10
	}
	out := make([]SwingStrength, 0, 8)
	for i := leftRight; i < len(c)-leftRight; i++ {
		hi := c[i].H
		lo := c[i].L
		isHi, isLo := true, true
		for j := i - leftRight; j <= i+leftRight; j++ {
			if j == i {
				continue
			}
			if c[j].H >= hi {
				isHi = false
			}
			if c[j].L <= lo {
				isLo = false
			}
		}
		if !isHi && !isLo {
			continue
		}
		isHigh := isHi
		lvl := hi
		if !isHigh {
			lvl = lo
		}
		tests := 0
		tol := lvl * (testTolPct / 100.0)
		if tol <= 0 {
			tol = 1e-8
		}
		for k := i + 1; k < len(c); k++ {
			if absf(c[k].H-lvl) <= tol || absf(c[k].L-lvl) <= tol {
				tests++
			}
		}
		body := absf(c[i].C - c[i].O)
		rng := c[i].H - c[i].L
		wick := 0.0
		if isHigh {
			wick = c[i].H - maxf(c[i].O, c[i].C)
		} else {
			wick = minf(c[i].O, c[i].C) - c[i].L
		}
		fastRej := wick > body && rng > 0
		strength := 0.55
		if fastRej {
			strength += 0.20
		}
		if tests > 0 {
			strength -= minf(0.25, float64(tests)*0.05)
		}
		if strength < 0 {
			strength = 0
		}
		if strength > 1 {
			strength = 1
		}
		out = append(out, SwingStrength{
			Index:         i,
			Price:         lvl,
			IsHigh:        isHigh,
			Strength:      strength,
			Tests:         tests,
			FastRejection: fastRej,
			Strong:        strength >= 0.60,
		})
	}
	return out
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
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
