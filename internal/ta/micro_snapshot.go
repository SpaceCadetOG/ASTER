package ta

import (
	"sort"

	"go-machine/internal/features"
	"go-machine/internal/indicators"
	"go-machine/internal/types"
)

type MicroSnapshot struct {
	LastClose    float64
	EMA9         float64
	SessionVWAP  float64
	ATR          float64
	ATRPct       float64
	VolumeRatio  float64
	FastSlopePct float64
	SlowSlopePct float64
}

func SnapshotFromTypesCandles(bars []types.Candle, atrLen, fastSlopeN, slowSlopeN, volumeN int) MicroSnapshot {
	if len(bars) == 0 {
		return MicroSnapshot{}
	}
	sorted := types.EnsureSorted(append([]types.Candle(nil), bars...))
	fc := make([]features.Candle, 0, len(sorted))
	for _, b := range sorted {
		fc = append(fc, features.Candle{Ts: b.T, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V})
	}
	return SnapshotFromFeatureCandles(fc, atrLen, fastSlopeN, slowSlopeN, volumeN)
}

func SnapshotFromFeatureCandles(bars []features.Candle, atrLen, fastSlopeN, slowSlopeN, volumeN int) MicroSnapshot {
	if len(bars) == 0 {
		return MicroSnapshot{}
	}
	sorted := append([]features.Candle(nil), bars...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Ts.Before(sorted[j].Ts) })
	out := MicroSnapshot{LastClose: sorted[len(sorted)-1].C}
	if out.LastClose <= 0 {
		return out
	}
	out.EMA9 = emaLastFromFeatures(sorted, 9)
	out.SessionVWAP = sessionVWAPFromFeatures(sorted)
	out.FastSlopePct = closeSlopePctFromFeatures(sorted, fastSlopeN)
	out.SlowSlopePct = closeSlopePctFromFeatures(sorted, slowSlopeN)
	out.VolumeRatio = volumeRatioFromFeatures(sorted, volumeN)
	out.ATR = atrLastFromFeatures(sorted, atrLen)
	if out.ATR > 0 {
		out.ATRPct = out.ATR / out.LastClose
	}
	return out
}

func EMAPairFromTypesCandles(bars []types.Candle, fastLen, slowLen int) (float64, float64) {
	if len(bars) == 0 {
		return 0, 0
	}
	sorted := types.EnsureSorted(append([]types.Candle(nil), bars...))
	closes := make([]float64, 0, len(sorted))
	for _, b := range sorted {
		closes = append(closes, b.C)
	}
	return emaTailFromCloses(closes, fastLen), emaTailFromCloses(closes, slowLen)
}

func emaTailFromCloses(closes []float64, n int) float64 {
	if len(closes) == 0 || n <= 0 {
		return 0
	}
	seq := indicators.EMA(closes, n)
	if len(seq) == 0 {
		return 0
	}
	return seq[len(seq)-1]
}

func emaLastFromFeatures(c []features.Candle, n int) float64 {
	closes := make([]float64, 0, len(c))
	for _, b := range c {
		closes = append(closes, b.C)
	}
	return emaTailFromCloses(closes, n)
}

func sessionVWAPFromFeatures(c []features.Candle) float64 {
	if len(c) == 0 {
		return 0
	}
	pv, vol := 0.0, 0.0
	for _, b := range c {
		typ := (b.H + b.L + b.C) / 3.0
		pv += typ * b.V
		vol += b.V
	}
	if vol <= 0 {
		return c[len(c)-1].C
	}
	return pv / vol
}

func closeSlopePctFromFeatures(c []features.Candle, n int) float64 {
	if len(c) < 2 {
		return 0
	}
	if n <= 0 {
		n = 2
	}
	if len(c) <= n {
		n = len(c) - 1
	}
	start := c[len(c)-1-n].C
	end := c[len(c)-1].C
	if start <= 0 {
		return 0
	}
	return ((end - start) / start) * 100.0 / float64(n)
}

func volumeRatioFromFeatures(c []features.Candle, n int) float64 {
	if len(c) == 0 {
		return 0
	}
	if n <= 0 {
		n = 20
	}
	if len(c) < n {
		n = len(c)
	}
	sum := 0.0
	for i := len(c) - n; i < len(c); i++ {
		sum += c[i].V
	}
	if sum <= 0 {
		return 0
	}
	avg := sum / float64(n)
	if avg <= 0 {
		return 0
	}
	return c[len(c)-1].V / avg
}

func atrLastFromFeatures(c []features.Candle, n int) float64 {
	if len(c) < 2 {
		return 0
	}
	if n <= 0 {
		n = 14
	}
	trs := make([]float64, 0, len(c)-1)
	for i := 1; i < len(c); i++ {
		hi := c[i].H
		lo := c[i].L
		pc := c[i-1].C
		tr := maxf(hi-lo, maxf(absf(hi-pc), absf(lo-pc)))
		trs = append(trs, tr)
	}
	if len(trs) < n {
		n = len(trs)
	}
	if n <= 0 {
		return 0
	}
	sum := 0.0
	for i := len(trs) - n; i < len(trs); i++ {
		sum += trs[i]
	}
	return sum / float64(n)
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
