package ta

import (
	"testing"
	"time"

	"go-machine/internal/types"
)

// helper: make N candles around a base, gently trending by step
func mkTrend(b float64, step float64, n int) []types.Candle {
	out := make([]types.Candle, n)
	t := time.Unix(0, 0).UTC()
	for i := 0; i < n; i++ {
		p := b + float64(i)*step
		out[i] = types.Candle{
			T: t.Add(time.Duration(i) * time.Minute),
			O: p * 0.999,
			H: p * 1.001,
			L: p * 0.998,
			C: p,
			V: 1_000_000, // constant vol is fine here
		}
	}
	return out
}

func TestTrendMetrics_Bull(t *testing.T) {
	bars := mkTrend(100, 0.5, 200) // uptrend
	vwap := 99.5                   // below last price
	tr := TrendMetrics("TEST", types.TF15m, bars, vwap)

	if tr.Bias != "bull" {
		t.Fatalf("expected bull bias, got %s", tr.Bias)
	}
	if tr.EMARatio <= 1.0 {
		t.Fatalf("expected EMARatio > 1.0, got %.6f", tr.EMARatio)
	}
	if tr.Slope9 <= 0 || tr.Slope21 <= 0 {
		t.Fatalf("expected positive slopes, got slope9=%.2f slope21=%.2f", tr.Slope9, tr.Slope21)
	}
	if tr.TrendScore <= 30 {
		t.Fatalf("trendScore too low for clear uptrend: %.2f", tr.TrendScore)
	}
}

func TestTrendMetrics_Bear(t *testing.T) {
	bars := mkTrend(100, -0.5, 200) // downtrend
	vwap := 100.5                   // above last price
	tr := TrendMetrics("TEST", types.TF15m, bars, vwap)

	if tr.Bias != "bear" {
		t.Fatalf("expected bear bias, got %s", tr.Bias)
	}
	if tr.EMARatio >= 1.0 {
		t.Fatalf("expected EMARatio < 1.0, got %.6f", tr.EMARatio)
	}
	if tr.Slope9 >= 0 || tr.Slope21 >= 0 {
		t.Fatalf("expected negative slopes, got slope9=%.2f slope21=%.2f", tr.Slope9, tr.Slope21)
	}
	if tr.TrendScore <= 30 {
		t.Fatalf("trendScore too low for clear downtrend: %.2f", tr.TrendScore)
	}
}
