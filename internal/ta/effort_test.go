package ta

import (
	"testing"
	"time"

	"go-machine/internal/types"
)

func mkVolSeries(vals []float64) []types.Candle {
	out := make([]types.Candle, len(vals))
	t := time.Unix(0, 0).UTC()
	for i, v := range vals {
		out[i] = types.Candle{
			T: t.Add(time.Duration(i) * time.Minute),
			O: 100, H: 101, L: 99, C: 100.5,
			V: v,
		}
	}
	return out
}

func TestEffort_SpikesDetected(t *testing.T) {
	// flat vols with a few big spikes
	vals := make([]float64, 200)
	for i := range vals {
		vals[i] = 10_000_000
	}
	vals[150] = 80_000_000
	vals[151] = 90_000_000
	vals[160] = 70_000_000

	bars := mkVolSeries(vals)
	ef := ComputeEffort("TEST", "15m", bars, 20, 2.0, 5_000_000)

	if ef.EffortScore <= 0 {
		t.Fatalf("expected positive effort score with clear spikes, got %.2f", ef.EffortScore)
	}
	if len(ef.Spikes) < 2 {
		t.Fatalf("expected at least 2 spike points, got %d", len(ef.Spikes))
	}
}
