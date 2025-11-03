// go-machine/internal/ta/effort.go
package ta

import (
	"math"

	"go-machine/internal/types"
)

// EffortResult aggregates volume/effort-based metrics used in confluence.
type EffortResult struct {
	Symbol       string     `json:"symbol"`
	TF           string     `json:"tf"`
	Count        int        `json:"count"`
	VWAP         float64    `json:"vwap"`
	EMAvol       float64    `json:"emaV"`
	MeanVol      float64    `json:"meanV"`
	StdVol       float64    `json:"stdV"`
	SpikeDensity float64    `json:"spikeDensity"`
	Spikes       []VolSpike `json:"spikes"`
	TrendBias    string     `json:"trendBias"`
	EffortScore  float64    `json:"effortScore"`
}

// ComputeEffort calculates a volume-based “effort” score and supporting stats.
func ComputeEffort(symbol string, tf types.TF, bars []types.Candle, win int, zmin, vmin float64) EffortResult {
	cs := types.EnsureSorted(bars)
	n := len(cs)
	if n == 0 {
		return EffortResult{Symbol: symbol, TF: string(tf)}
	}

	// VWAP
	var pv, vtot float64
	for _, b := range cs {
		tp := (b.H + b.L + b.C) / 3.0
		pv += tp * b.V
		vtot += b.V
	}
	vwap := 0.0
	if vtot > 0 {
		vwap = pv / vtot
	}

	// volume series
	vs := make([]float64, n)
	for i, b := range cs {
		vs[i] = b.V
	}

	emavSeq := emaVolSeq(vs, max(2, win))
	emav := 0.0
	if len(emavSeq) > 0 {
		emav = emavSeq[len(emavSeq)-1]
	}
	meanV, stdV := meanStd(vs)

	// spikes detection
	var spikes []VolSpike
	if stdV > 0 {
		for i, b := range cs {
			z := (b.V - meanV) / stdV
			if z >= zmin && b.V >= vmin {
				// FIX: convert time.Time to unix ms
				spikes = append(spikes, VolSpike{
					I: i,
					T: b.T.UnixMilli(),
					V: b.V,
					Z: z,
				})
			}
		}
	}
	spikeDensity := float64(len(spikes)) / float64(n)

	// bias relative to VWAP
	trendBias := "neutral"
	if cs[n-1].C > vwap {
		trendBias = "bull"
	} else if cs[n-1].C < vwap {
		trendBias = "bear"
	}

	// scoring
	clamp01 := func(x float64) float64 {
		if x < 0 {
			return 0
		}
		if x > 1 {
			return 1
		}
		return x
	}
	emu := 0.0
	if meanV > 0 {
		emu = (emav - meanV) / meanV
	}
	emuScore := clamp01(emu/0.5) * 40.0
	spkScore := clamp01(spikeDensity/0.08) * 60.0
	score := emuScore + spkScore
	if score > 100 {
		score = 100
	}

	return EffortResult{
		Symbol:       symbol,
		TF:           string(tf),
		Count:        n,
		VWAP:         vwap,
		EMAvol:       emav,
		MeanVol:      meanV,
		StdVol:       stdV,
		SpikeDensity: spikeDensity,
		Spikes:       spikes,
		TrendBias:    trendBias,
		EffortScore:  score,
	}
}

func emaVolSeq(vs []float64, win int) []float64 {
	if len(vs) == 0 || win <= 1 {
		return nil
	}
	k := 2.0 / (float64(win) + 1.0)
	out := make([]float64, len(vs))
	out[0] = vs[0]
	for i := 1; i < len(vs); i++ {
		out[i] = k*vs[i] + (1-k)*out[i-1]
	}
	return out
}

func meanStd(vs []float64) (float64, float64) {
	n := float64(len(vs))
	if n == 0 {
		return 0, 0
	}
	var s float64
	for _, v := range vs {
		s += v
	}
	m := s / n
	var s2 float64
	for _, v := range vs {
		d := v - m
		s2 += d * d
	}
	return m, math.Sqrt(s2 / n)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
