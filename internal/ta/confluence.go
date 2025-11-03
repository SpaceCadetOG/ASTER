// go-machine/internal/ta/confluence.go
package ta

import (
	"math"
	"strings"
)

type ConfluenceResult struct {
	Score float64  `json:"score"` // 0..100
	Label string   `json:"label"` // A/B/C
	Notes []string `json:"notes"`
}

// side: "long" or "short"
func ComputeConfluence(tr TrendResult, ef EffortResult, ob OBContext, side string) ConfluenceResult {
	side = strings.ToLower(side)
	if side != "short" {
		side = "long"
	}

	// weights
	const trendW = 0.45
	const effortW = 0.35
	const obW = 0.20

	// normalize components to 0..1
	trendNorm := clamp01(tr.TrendScore / 80.0)   // cap strong trend ~80
	effortNorm := clamp01(ef.EffortScore / 60.0) // cap strong effort ~60

	// orderbook: make sign matter per side
	var obNorm float64
	switch side {
	case "long":
		obNorm = clamp01(0.5 + 0.5*clampSym(ob.Imbalance))
	case "short":
		obNorm = clamp01(0.5 - 0.5*clampSym(ob.Imbalance))
	}

	score := 100.0 * (trendW*trendNorm + effortW*effortNorm + obW*obNorm)
	label := grade(score)

	notes := make([]string, 0, 6)
	// Trend notes
	if tr.ABOVE() && tr.SLOPES_UP() && tr.Bias == "bull" {
		notes = append(notes, "trend: ema9>ema21 & above VWAP")
	}
	if tr.Slope9 > 0 && tr.Slope21 > 0 {
		notes = append(notes, "trend: ema slope rising")
	}
	if tr.Bias == "bear" && side == "short" {
		notes = append(notes, "trend: HTF aligns with short")
	}

	// Effort notes
	if ef.SpikeDensity >= 0.05 {
		notes = append(notes, "effort: spike cluster")
	}
	if ef.EMAvol > ef.MeanVol {
		notes = append(notes, "effort: volume EMA rising")
	}

	// OB notes (nearby walls + imbalance)
	if side == "long" {
		if ob.Imbalance > 0.1 {
			notes = append(notes, "ob: bid support > asks")
		} else if ob.Imbalance < -0.1 {
			notes = append(notes, "ob: ask pressure")
		}
		if ob.TopAskWall.Rank <= 3 && ob.TopAskWall.Size > ob.TopBidWall.Size*1.5 {
			notes = append(notes, "ob: large ask wall near")
		}
	} else { // short
		if ob.Imbalance < -0.1 {
			notes = append(notes, "ob: ask supply > bids")
		} else if ob.Imbalance > 0.1 {
			notes = append(notes, "ob: bid absorption risk")
		}
		if ob.TopBidWall.Rank <= 3 && ob.TopBidWall.Size > ob.TopAskWall.Size*1.5 {
			notes = append(notes, "ob: large bid wall near")
		}
	}

	// small, side-aware OB nudges (helps decisive cases clear thresholds)
	if side == "long" {
		if ob.Imbalance > 0.20 {
			score += 3
		}
		if ob.TopBidWall.Rank <= 2 {
			score += 2
		}
	}
	if side == "short" {
		if ob.Imbalance < -0.20 {
			score += 3
		}
		if ob.TopAskWall.Rank <= 2 {
			score += 2
		}
	}

	return ConfluenceResult{
		Score: round2(score),
		Label: label,
		Notes: notes,
	}
}

// ---- helpers ----

func grade(s float64) string {
	switch {
	case s >= 75:
		return "A"
	case s >= 60:
		return "B"
	default:
		return "C"
	}
}

// clamp symmetrical-ish (soft clip) to [-1, +1]
func clampSym(x float64) float64 {
	if x > 1 {
		return 1
	}
	if x < -1 {
		return -1
	}
	return x
}

func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// tiny convenience so we can note trend conditions without leaking impl details
func (t TrendResult) ABOVE() bool     { return t.EMARatio >= 1.0 && t.AboveVWAP >= 0.5 }
func (t TrendResult) SLOPES_UP() bool { return t.Slope9 > 0 && t.Slope21 > 0 }
