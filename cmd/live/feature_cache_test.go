package main

import (
	"testing"
	"time"

	"go-machine/internal/types"
)

func TestFeatureRuntimeCacheCachesCandlesAndMicro(t *testing.T) {
	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	loads := 0
	cache := newFeatureRuntimeCache(func(symbol string, tf types.TF, limit int) ([]types.Candle, error) {
		loads++
		return []types.Candle{
			{T: base, O: 100, H: 101, L: 99, C: 100.5, V: 100},
			{T: base.Add(time.Minute), O: 100.5, H: 102, L: 100, C: 101.5, V: 120},
			{T: base.Add(2 * time.Minute), O: 101.5, H: 103, L: 101, C: 102.5, V: 140},
			{T: base.Add(3 * time.Minute), O: 102.5, H: 104, L: 102, C: 103.5, V: 160},
		}, nil
	})
	now := base
	cache.now = func() time.Time { return now }
	cache.ttl = 2 * time.Minute

	if _, err := cache.candleSeries("BTCUSDT", types.TF1m, 64); err != nil {
		t.Fatalf("candleSeries error: %v", err)
	}
	if _, err := cache.candleSeries("BTCUSDT", types.TF1m, 64); err != nil {
		t.Fatalf("second candleSeries error: %v", err)
	}
	if loads != 1 {
		t.Fatalf("expected one candle load, got %d", loads)
	}
	snap, _, err := cache.microSnapshot("BTCUSDT", 64, 3, 2, 3, 3)
	if err != nil {
		t.Fatalf("microSnapshot error: %v", err)
	}
	if snap.ATRPct <= 0 || snap.EMA9 <= 0 {
		t.Fatalf("expected populated snapshot, got %+v", snap)
	}
	if _, _, err := cache.microSnapshot("BTCUSDT", 64, 3, 2, 3, 3); err != nil {
		t.Fatalf("cached microSnapshot error: %v", err)
	}
	if loads != 1 {
		t.Fatalf("expected micro snapshot to reuse cached candles, got %d loads", loads)
	}
	stats := cache.statsSnapshot()
	if stats.CandleHits == 0 || stats.CandleMisses == 0 {
		t.Fatalf("expected candle hit/miss accounting, got %+v", stats)
	}
	if stats.MicroHits == 0 || stats.MicroMisses == 0 {
		t.Fatalf("expected micro hit/miss accounting, got %+v", stats)
	}
	if stats.CandleKeys == 0 || stats.MicroKeys == 0 {
		t.Fatalf("expected populated cache key counts, got %+v", stats)
	}
}
