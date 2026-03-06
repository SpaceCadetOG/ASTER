package discovery

import (
	"math"

	"go-machine/internal/indicators"
	"go-machine/internal/types"
)

// VolumeRatio returns current volume / SMA(window).
func VolumeRatio(c []types.Candle, window int) (float64, bool) {
	if len(c) < window+1 || window <= 1 {
		return 0, false
	}
	vs := make([]float64, 0, len(c))
	for i := range c {
		vs = append(vs, c[i].V)
	}
	sma := indicators.SMA(vs, window)
	lastSMA := sma[len(sma)-1]
	if math.IsNaN(lastSMA) || lastSMA <= 0 {
		return 0, false
	}
	return vs[len(vs)-1] / lastSMA, true
}

// VolumeZScore returns z-score of current bar volume vs rolling window.
func VolumeZScore(c []types.Candle, window int) (float64, bool) {
	if len(c) < window+1 || window <= 1 {
		return 0, false
	}
	vs := make([]float64, 0, len(c))
	for i := range c {
		vs = append(vs, c[i].V)
	}
	mean, std := indicators.MeanStd(vs, window)
	m := mean[len(mean)-1]
	s := std[len(std)-1]
	if math.IsNaN(m) || math.IsNaN(s) || s <= 0 {
		return 0, false
	}
	return (vs[len(vs)-1] - m) / s, true
}

// Volatility computes stddev of close-to-close returns over window.
func Volatility(c []types.Candle, window int) float64 {
	if len(c) < window+1 || window <= 1 {
		return 0
	}
	rets := make([]float64, 0, window)
	start := len(c) - (window + 1)
	if start < 0 {
		start = 0
	}
	for i := start + 1; i < len(c); i++ {
		prev := c[i-1].C
		if prev <= 0 {
			continue
		}
		rets = append(rets, (c[i].C-prev)/prev)
	}
	if len(rets) < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range rets {
		mean += r
	}
	mean /= float64(len(rets))
	v := 0.0
	for _, r := range rets {
		d := r - mean
		v += d * d
	}
	v /= float64(len(rets))
	return math.Sqrt(v)
}
