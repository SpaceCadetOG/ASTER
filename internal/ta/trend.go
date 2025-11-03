// go-machine/internal/ta/trend.go
package ta

import (
	"math"

	"go-machine/internal/types"
)

type TrendResult struct {
	Symbol     string  `json:"symbol"`
	TF         string  `json:"tf"`
	EMA9       float64 `json:"ema9"`
	EMA21      float64 `json:"ema21"`
	EMASpread  float64 `json:"emaSpread"` // |EMA9-EMA21|
	EMARatio   float64 `json:"emaRatio"`  // EMA9/EMA21
	Slope9     float64 `json:"slope9"`    // per-bar slope (approx)
	Slope21    float64 `json:"slope21"`
	AboveVWAP  float64 `json:"aboveVWAP"` // fraction in last N above VWAP
	Bias       string  `json:"bias"`      // bull/bear/neutral
	TrendScore float64 `json:"trendScore"`
}

func emaSeq(closes []float64, win int) []float64 {
	if len(closes) == 0 || win <= 1 {
		return nil
	}
	k := 2.0 / (float64(win) + 1.0)
	out := make([]float64, len(closes))
	out[0] = closes[0]
	for i := 1; i < len(closes); i++ {
		out[i] = k*closes[i] + (1-k)*out[i-1]
	}
	return out
}

func TrendMetrics(symbol string, tf types.TF, bars []types.Candle, vwap float64) TrendResult {
	n := len(bars)
	if n == 0 {
		return TrendResult{Symbol: symbol, TF: string(tf)}
	}
	cs := types.EnsureSorted(bars)
	cls := make([]float64, n)
	for i, b := range cs {
		cls[i] = b.C
	}

	e9 := emaSeq(cls, 9)
	e21 := emaSeq(cls, 21)
	var ema9, ema21 float64
	if len(e9) > 0 {
		ema9 = e9[len(e9)-1]
	}
	if len(e21) > 0 {
		ema21 = e21[len(e21)-1]
	}

	// slopes
	var slope9, slope21 float64
	if len(e9) >= 2 {
		slope9 = e9[len(e9)-1] - e9[len(e9)-2]
	}
	if len(e21) >= 2 {
		slope21 = e21[len(e21)-1] - e21[len(e21)-2]
	}

	// fraction above VWAP
	var above int
	for _, b := range cs {
		if b.C > vwap {
			above++
		}
	}
	aboveVWAP := float64(above) / float64(n)

	last := cls[n-1]

	// magnitude scoring (direction-neutral magnitude drives score; bias is directional)
	abs := func(x float64) float64 {
		if x < 0 {
			return -x
		}
		return x
	}
	clamp01 := func(x float64) float64 {
		if x < 0 {
			return 0
		}
		if x > 1 {
			return 1
		}
		return x
	}

	emaRatio := 0.0
	if ema21 != 0 {
		emaRatio = ema9 / ema21
	}
	emaMag := abs(emaRatio - 1.0)
	emaScore := clamp01(emaMag/0.004) * 45.0 // ~0.4% separation → full

	slopeUnit := 5.0
	slopeMag := (abs(slope9) + abs(slope21)) / (2.0 * slopeUnit)
	slopeScore := clamp01(slopeMag) * 45.0

	distVWAP := 0.0
	if vwap > 0 && last > 0 {
		distVWAP = abs(last-vwap) / last
	}
	vwapScore := clamp01(distVWAP/0.002) * 10.0 // ~0.2% from VWAP → full

	score := emaScore + slopeScore + vwapScore
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	// directional bias
	bias := "neutral"
	switch {
	case ema9 > ema21 && last > vwap && slope9 >= 0:
		bias = "bull"
	case ema9 < ema21 && last < vwap && slope9 <= 0:
		bias = "bear"
	}

	return TrendResult{
		Symbol:     symbol,
		TF:         string(tf),
		EMA9:       ema9,
		EMA21:      ema21,
		EMASpread:  math.Abs(ema9 - ema21),
		EMARatio:   emaRatio,
		Slope9:     slope9,
		Slope21:    slope21,
		AboveVWAP:  aboveVWAP,
		Bias:       bias,
		TrendScore: score,
	}
}
