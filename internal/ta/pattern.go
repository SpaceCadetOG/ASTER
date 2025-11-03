// go-machine/internal/ta/patterns.go
package ta

import (
	"math"

	"go-machine/internal/types"
)

// ----- Public types -----

type PatternID string

const (
	PatHammer       PatternID = "Hammer"
	PatShootingStar PatternID = "ShootingStar"
	PatMarubozuBull PatternID = "MarubozuBull"
	PatMarubozuBear PatternID = "MarubozuBear"
	PatSpinningTop  PatternID = "SpinningTop"
	PatDoji         PatternID = "Doji"
	PatBullEngulf   PatternID = "BullishEngulfing"
	PatBearEngulf   PatternID = "BearishEngulfing"
	PatPiercing     PatternID = "PiercingLine"
	PatDarkCloud    PatternID = "DarkCloudCover"
)

type Dir string

const (
	DirBull Dir = "bull"
	DirBear Dir = "bear"
	DirNeu  Dir = "neutral"
)

type PatternTag struct {
	Index    int
	Name     PatternID
	Dir      Dir
	Strength float64 // 0..1
}

// ----- Tunable thresholds -----

const (
	minBodyPct     = 0.15 // to avoid counting near-doji as bodies
	dojiMaxBodyPct = 0.10

	hammerLowerMin = 0.60 // long lower wick
	hammerUpperMax = 0.15 // small upper wick
	hammerBodyMin  = 0.15 // not doji

	starUpperMin = 0.60
	starLowerMax = 0.15
	starBodyMin  = 0.15

	marubozuBodyMin = 0.80
	marubozuWickMax = 0.10

	spinBodyMin = 0.15
	spinBodyMax = 0.35
	spinWickMin = 0.20

	engulfBodyRatio = 1.20 // body2 >= 1.2 * body1 for leniency
)

// ----- Helpers (pure) -----

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

type candleParts struct {
	Body, Upper, Lower, Range   float64
	BodyPct, UpperPct, LowerPct float64
	IsBull, IsBear, IsDoji      bool
}

func split(c types.Candle) candleParts {
	tr := c.H - c.L
	if tr <= 0 {
		// degenerate; guard with epsilon
		tr = 1e-9
	}
	body := math.Abs(c.C - c.O)
	upper := c.H - math.Max(c.C, c.O)
	lower := math.Min(c.C, c.O) - c.L

	cp := candleParts{
		Body: body, Upper: upper, Lower: lower, Range: tr,
		BodyPct:  body / tr,
		UpperPct: upper / tr,
		LowerPct: lower / tr,
		IsBull:   c.C > c.O,
		IsBear:   c.O > c.C,
	}
	cp.IsDoji = cp.BodyPct <= dojiMaxBodyPct
	return cp
}

// clamp 0..1
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// ----- Single-candle detectors -----

func matchHammer(cp candleParts) (ok bool, strength float64) {
	if cp.IsDoji {
		return false, 0
	}
	if cp.BodyPct < hammerBodyMin {
		return false, 0
	}
	if cp.LowerPct < hammerLowerMin {
		return false, 0
	}
	if cp.UpperPct > hammerUpperMax {
		return false, 0
	}

	// Strength: longer lower wick and decent body
	s := 0.6*clamp01((cp.LowerPct-hammerLowerMin)/(1.0-hammerLowerMin)) +
		0.4*clamp01((cp.BodyPct-hammerBodyMin)/(1.0-hammerBodyMin))
	return true, clamp01(s)
}

func matchShootingStar(cp candleParts) (ok bool, strength float64) {
	// Accept doji-like bodies; the key is long upper shadow & small lower shadow.
	if cp.UpperPct < starUpperMin { // long upper wick
		return false, 0
	}
	if cp.LowerPct > starLowerMax { // small lower wick
		return false, 0
	}

	// Strength: mostly from upper wick dominance; bonus if lower wick is very small.
	upperScore := clamp01((cp.UpperPct - starUpperMin) / (1.0 - starUpperMin))
	lowerScore := clamp01((starLowerMax - cp.LowerPct) / starLowerMax) // smaller lower wick -> higher score
	s := 0.7*upperScore + 0.3*lowerScore
	return true, clamp01(s)
}

func matchMarubozu(cp candleParts) (bull, bear bool, strength float64) {
	if cp.BodyPct < marubozuBodyMin {
		return false, false, 0
	}
	if cp.UpperPct > marubozuWickMax || cp.LowerPct > marubozuWickMax {
		return false, false, 0
	}
	str := clamp01((cp.BodyPct - marubozuBodyMin) / (1.0 - marubozuBodyMin))
	if cp.IsBull {
		return true, false, str
	}
	if cp.IsBear {
		return false, true, str
	}
	return false, false, 0
}

func matchSpinningTop(cp candleParts) (ok bool) {
	if cp.IsDoji {
		return false
	}
	if cp.BodyPct < spinBodyMin || cp.BodyPct > spinBodyMax {
		return false
	}
	if cp.UpperPct < spinWickMin || cp.LowerPct < spinWickMin {
		return false
	}
	return true
}

func matchDoji(cp candleParts) bool {
	return cp.IsDoji
}

// ----- Two-candle detectors -----

func matchBullEngulf(cp1, cp2 candleParts, c1, c2 types.Candle) (ok bool, strength float64) {
	if !(cp1.IsBear && cp2.IsBull) {
		return false, 0
	}
	body1 := cp1.Body
	body2 := cp2.Body

	fullEngulf := (c2.C > c1.O) && (c2.O < c1.C)
	sizeEngulf := body2 >= engulfBodyRatio*body1

	if !(fullEngulf || sizeEngulf) {
		return false, 0
	}
	// Strength via size ratio + overlap
	ratio := clamp01(body2 / (body1 + 1e-9) / engulfBodyRatio) // >=1 → 1.0
	// overlap fraction relative to c2 body
	low2, high2 := math.Min(c2.O, c2.C), math.Max(c2.O, c2.C)
	low1, high1 := math.Min(c1.O, c1.C), math.Max(c1.O, c1.C)
	overlap := math.Max(0, math.Min(high2, high1)-math.Max(low2, low1))
	overlapPct := clamp01(overlap / (body2 + 1e-9))
	str := 0.6*ratio + 0.4*overlapPct
	return true, clamp01(str)
}

func matchBearEngulf(cp1, cp2 candleParts, c1, c2 types.Candle) (ok bool, strength float64) {
	if !(cp1.IsBull && cp2.IsBear) {
		return false, 0
	}
	body1 := cp1.Body
	body2 := cp2.Body

	fullEngulf := (c2.O > c1.C) && (c2.C < c1.O)
	sizeEngulf := body2 >= engulfBodyRatio*body1

	if !(fullEngulf || sizeEngulf) {
		return false, 0
	}
	ratio := clamp01(body2 / (body1 + 1e-9) / engulfBodyRatio)
	low2, high2 := math.Min(c2.O, c2.C), math.Max(c2.O, c2.C)
	low1, high1 := math.Min(c1.O, c1.C), math.Max(c1.O, c1.C)
	overlap := math.Max(0, math.Min(high2, high1)-math.Max(low2, low1))
	overlapPct := clamp01(overlap / (body2 + 1e-9))
	str := 0.6*ratio + 0.4*overlapPct
	return true, clamp01(str)
}

// (Optional; stub for later)
func matchPiercing(cp1, cp2 candleParts, c1, c2 types.Candle) (ok bool) {
	// Bear then bull; bull opens below C1 close, closes above mid of C1 body
	if !(cp1.IsBear && cp2.IsBull) {
		return false
	}
	mid1 := (c1.O + c1.C) / 2
	if !(c2.O < c1.C && c2.C > mid1) {
		return false
	}
	return true
}

func matchDarkCloud(cp1, cp2 candleParts, c1, c2 types.Candle) (ok bool) {
	// Bull then bear; bear opens above C1 close, closes below mid of C1 body
	if !(cp1.IsBull && cp2.IsBear) {
		return false
	}
	mid1 := (c1.O + c1.C) / 2
	if !(c2.O > c1.C && c2.C < mid1) {
		return false
	}
	return true
}

// ----- Public entrypoint -----

func DetectPatterns(bars []types.Candle) []PatternTag {
	if len(bars) == 0 {
		return nil
	}

	parts := make([]candleParts, len(bars))
	for i := range bars {
		parts[i] = split(bars[i])
	}

	out := make([]PatternTag, 0, len(bars))

	for i := range bars {
		cp := parts[i]

		// Single-candle
		if ok, s := matchHammer(cp); ok {
			out = append(out, PatternTag{Index: i, Name: PatHammer, Dir: DirBull, Strength: s})
		}
		if ok, s := matchShootingStar(cp); ok {
			out = append(out, PatternTag{Index: i, Name: PatShootingStar, Dir: DirBear, Strength: s})
		}
		if bull, bear, s := matchMarubozu(cp); bull {
			out = append(out, PatternTag{Index: i, Name: PatMarubozuBull, Dir: DirBull, Strength: s})
		} else if bear {
			out = append(out, PatternTag{Index: i, Name: PatMarubozuBear, Dir: DirBear, Strength: s})
		}
		if matchSpinningTop(cp) {
			out = append(out, PatternTag{Index: i, Name: PatSpinningTop, Dir: DirNeu, Strength: 0.5})
		}
		if matchDoji(cp) {
			out = append(out, PatternTag{Index: i, Name: PatDoji, Dir: DirNeu, Strength: 0.5})
		}

		// Two-candle (need i>=1)
		if i >= 1 {
			cp1, cp2 := parts[i-1], parts[i]
			c1, c2 := bars[i-1], bars[i]

			if ok, s := matchBullEngulf(cp1, cp2, c1, c2); ok {
				out = append(out, PatternTag{Index: i, Name: PatBullEngulf, Dir: DirBull, Strength: s})
			}
			if ok, s := matchBearEngulf(cp1, cp2, c1, c2); ok {
				out = append(out, PatternTag{Index: i, Name: PatBearEngulf, Dir: DirBear, Strength: s})
			}
			if matchPiercing(cp1, cp2, c1, c2) {
				out = append(out, PatternTag{Index: i, Name: PatPiercing, Dir: DirBull, Strength: 0.6})
			}
			if matchDarkCloud(cp1, cp2, c1, c2) {
				out = append(out, PatternTag{Index: i, Name: PatDarkCloud, Dir: DirBear, Strength: 0.6})
			}
		}
	}

	return out
}
