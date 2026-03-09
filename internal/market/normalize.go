package market

import "math"

func clamp01(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func normalizeRawScore(raw, minRaw, maxRaw float64) float64 {
	if maxRaw <= minRaw {
		return clamp(raw, 0, 100)
	}
	x := (raw - minRaw) / (maxRaw - minRaw)
	return math.Round(clamp01(x)*10000) / 100
}
