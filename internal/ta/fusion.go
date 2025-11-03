// go-machine/internal/ta/fusion.go
package ta

import (
	"sort"

	"go-machine/internal/types"
)

// ---------- Public result ----------

type FusionResult struct {
	Symbol          string   `json:"symbol"`
	TF              string   `json:"tf"`
	Side            string   `json:"side"`      // long | short (chosen)
	Structure       string   `json:"structure"` // HH | LH | HL | LL | NA
	Zone            string   `json:"zone"`      // support | resistance | neutral
	TrendBias       string   `json:"trendBias"` // bull | bear | neutral (from TrendResult)
	ConfluenceScore float64  `json:"confluenceScore"`
	Grade           string   `json:"grade"`
	Notes           []string `json:"notes,omitempty"`
}

// ---------- Internal helpers ----------

func lastTwoHighsLows(pivs []Pivot) (highs, lows []Pivot) {
	for _, p := range pivs {
		if p.Typ == "H" {
			highs = append(highs, p)
		} else if p.Typ == "L" {
			lows = append(lows, p)
		}
	}
	// keep chronological order, then take last 2
	if len(highs) > 2 {
		highs = highs[len(highs)-2:]
	}
	if len(lows) > 2 {
		lows = lows[len(lows)-2:]
	}
	return highs, lows
}

func structureAndZone(bars []types.Candle, left, right int) (structure, zone string) {
	pivs := DetectFractalPivots(bars, left, right)
	if len(pivs) < 2 {
		return "NA", "neutral"
	}
	// latest pivot decides zone
	last := pivs[len(pivs)-1]
	if last.Typ == "H" {
		zone = "resistance"
	} else {
		zone = "support"
	}

	highs, lows := lastTwoHighsLows(pivs)
	if len(highs) >= 2 {
		// compare the last two highs
		if highs[1].P > highs[0].P {
			structure = "HH"
		} else {
			structure = "LH"
		}
	}
	if len(lows) >= 2 {
		// if no structure decided by highs, use lows;
		// or, if we want to update with lows when last pivot is L:
		if structure == "" || last.Typ == "L" {
			if lows[1].P > lows[0].P {
				structure = "HL"
			} else {
				structure = "LL"
			}
		}
	}
	if structure == "" {
		structure = "NA"
	}
	return structure, zone
}

func sideFromContext(tr TrendResult, ob OBContext) string {
	// Prefer trend bias first; if neutral, lean toward OB side.
	switch tr.Bias {
	case "bull":
		return "long"
	case "bear":
		return "short"
	default:
		if ob.Imbalance < -0.05 { // asks dominate
			return "short"
		}
		if ob.Imbalance > 0.05 { // bids dominate
			return "long"
		}
		return "long"
	}
}

// ---------- Fusion (public) ----------

// Fuse everything into one decision snapshot.
// left/right are pivot window params (typical 3/3).
func ComputeFusion(symbol string, tf types.TF, bars []types.Candle,
	tr TrendResult, ef EffortResult, ob OBContext,
	left, right int,
) FusionResult {

	// 1) Structure & zone (from pivots)
	structure, zone := structureAndZone(bars, left, right)

	// 2) Choose side (trend first, then OB)
	side := sideFromContext(tr, ob)

	// 3) Base confluence from M2-D
	conf := ComputeConfluence(tr, ef, ob, side)
	score := conf.Score
	notes := make([]string, 0, 8)
	if len(conf.Notes) > 0 {
		notes = append(notes, conf.Notes...)
	}

	// 4) Structure/zone nudges (side-aware, small but meaningful)
	switch side {
	case "long":
		if structure == "HH" || structure == "HL" {
			score += 5
			notes = append(notes, "structure: bullish (HH/HL)")
		}
		if zone == "support" {
			score += 3
			notes = append(notes, "zone: near support")
		}
	case "short":
		if structure == "LH" || structure == "LL" {
			score += 5
			notes = append(notes, "structure: bearish (LH/LL)")
		}
		if zone == "resistance" {
			score += 3
			notes = append(notes, "zone: near resistance")
		}
	}

	// 5) Safety clamp + grading
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	grade := grade(score)

	// 6) De-duplicate notes (nice for readability)
	if len(notes) > 1 {
		m := map[string]bool{}
		out := make([]string, 0, len(notes))
		for _, n := range notes {
			if !m[n] {
				m[n] = true
				out = append(out, n)
			}
		}
		// keep stable order
		sort.Strings(out)
		notes = out
	}

	return FusionResult{
		Symbol:          symbol,
		TF:              string(tf),
		Side:            side,
		Structure:       structure,
		Zone:            zone,
		TrendBias:       tr.Bias,
		ConfluenceScore: round2(score),
		Grade:           grade,
		Notes:           notes,
	}
}
