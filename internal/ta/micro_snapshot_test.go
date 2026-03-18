package ta

import (
	"testing"
	"time"

	"go-machine/internal/features"
	"go-machine/internal/types"
)

func TestSnapshotFromTypesCandles(t *testing.T) {
	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	bars := []types.Candle{
		{T: base.Add(2 * time.Minute), O: 102, H: 104, L: 101, C: 103, V: 140},
		{T: base, O: 100, H: 101, L: 99, C: 100.5, V: 100},
		{T: base.Add(time.Minute), O: 100.5, H: 103, L: 100, C: 102, V: 120},
		{T: base.Add(3 * time.Minute), O: 103, H: 105, L: 102, C: 104, V: 160},
	}
	snap := SnapshotFromTypesCandles(bars, 3, 2, 3, 3)
	if snap.LastClose != 104 {
		t.Fatalf("expected last close 104, got %.2f", snap.LastClose)
	}
	if snap.EMA9 <= 0 || snap.SessionVWAP <= 0 || snap.ATR <= 0 || snap.ATRPct <= 0 {
		t.Fatalf("expected positive snapshot values, got %+v", snap)
	}
	if snap.FastSlopePct <= 0 || snap.SlowSlopePct <= 0 {
		t.Fatalf("expected positive slopes, got %+v", snap)
	}
	if snap.VolumeRatio <= 0 {
		t.Fatalf("expected positive volume ratio, got %.4f", snap.VolumeRatio)
	}
}

func TestSnapshotFromFeatureCandles(t *testing.T) {
	base := time.Date(2026, 3, 15, 13, 0, 0, 0, time.UTC)
	bars := []features.Candle{
		{Ts: base, O: 50, H: 51, L: 49, C: 50.5, V: 100},
		{Ts: base.Add(time.Minute), O: 50.5, H: 52, L: 50, C: 51.5, V: 130},
		{Ts: base.Add(2 * time.Minute), O: 51.5, H: 53, L: 51, C: 52.5, V: 150},
	}
	snap := SnapshotFromFeatureCandles(bars, 2, 2, 2, 2)
	if snap.LastClose != 52.5 {
		t.Fatalf("expected last close 52.5, got %.2f", snap.LastClose)
	}
	if snap.EMA9 <= 0 || snap.SessionVWAP <= 0 || snap.ATR <= 0 {
		t.Fatalf("expected positive snapshot values, got %+v", snap)
	}
}

func TestEMAPairFromTypesCandles(t *testing.T) {
	base := time.Date(2026, 3, 15, 14, 0, 0, 0, time.UTC)
	bars := []types.Candle{
		{T: base, C: 10},
		{T: base.Add(time.Minute), C: 11},
		{T: base.Add(2 * time.Minute), C: 12},
		{T: base.Add(3 * time.Minute), C: 13},
		{T: base.Add(4 * time.Minute), C: 14},
	}
	fast, slow := EMAPairFromTypesCandles(bars, 2, 4)
	if fast <= slow {
		t.Fatalf("expected fast EMA > slow EMA in rising series, got fast=%.4f slow=%.4f", fast, slow)
	}
}
