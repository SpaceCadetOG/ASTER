package ta

import (
	"math"

	"go-machine/internal/indicators"
	"go-machine/internal/types"
)

type VolSpike struct {
	Index int     `json:"index"`
	I     int     `json:"i"` // index
	T     int64   `json:"t"` // unix ms
	V     float64 `json:"v"` // raw volume
	Z     float64 `json:"z"` // z-score
}

// DetectVolumeSpikes flags bars where:
//   - z = (V - mean(V,p)) / std(V,p)  >= zMin
//   - V >= vMin (optional floor, e.g., $5m)
//
// Returns indices + values. Warmup region yields no spikes.
func DetectVolumeSpikes(bars []types.Candle, p int, zMin float64, vMin float64) []VolSpike {
	if len(bars) == 0 || p <= 1 {
		return nil
	}
	vol := make([]float64, len(bars))
	for i := range bars {
		vol[i] = bars[i].V
	}
	mean, std := indicators.MeanStd(vol, p)

	out := make([]VolSpike, 0, len(bars)/10)
	for i := range vol {
		m := mean[i]
		s := std[i]
		if math.IsNaN(m) || math.IsNaN(s) || s == 0 {
			continue // warmup or flat volumes
		}
		z := (vol[i] - m) / s
		if z >= zMin && vol[i] >= vMin {
			out = append(out, VolSpike{Index: i, V: vol[i], Z: z})
		}
	}
	return out
}
