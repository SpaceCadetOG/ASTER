package main

import (
	"testing"
	"time"

	"go-machine/internal/types"
)

func TestHTFPersistentFailedAndCaution_LongPolicy(t *testing.T) {
	now := time.Now().UTC()
	s := HTFStructureSnapshot{
		State:      HTFLongRange,
		DeltaBias:  "bearish",
		UpdatedAt:  now,
		TF:         "1h",
		LastClose:  100,
		TrendScore: 0.5,
	}
	if !htfPersistent("BUY", s) {
		t.Fatalf("expected long range to remain persistent")
	}
	if htfFailed("BUY", s) {
		t.Fatalf("expected bearish delta alone not to fail persistent long structure")
	}
	if !htfCaution("BUY", s) {
		t.Fatalf("expected caution for persistent long with bearish delta")
	}
}

func TestHTFFailed_LongDegradedAndBearish(t *testing.T) {
	now := time.Now().UTC()
	s := HTFStructureSnapshot{
		State:     HTFShortLHLL,
		DeltaBias: "bearish",
		UpdatedAt: now,
	}
	if !htfFailed("BUY", s) {
		t.Fatalf("expected long failure when structure degrades away from bullish with bearish delta")
	}
}

func TestHTFStaleSnapshotDoesNotHardFail(t *testing.T) {
	t.Setenv("LIVE_HTF_MAX_STALENESS_SEC", "900")
	s := HTFStructureSnapshot{
		State:     HTFLongBroken,
		DeltaBias: "bearish",
		UpdatedAt: time.Now().UTC().Add(-2 * time.Hour),
	}
	if htfFailed("BUY", s) {
		t.Fatalf("expected stale snapshot to not assert hard HTF failure")
	}
}

func TestBuildHTFSnapshotFromCandles_LongBreakNeedsTwoCloses(t *testing.T) {
	t.Setenv("LIVE_HTF_SWING_LR", "2")
	t.Setenv("LIVE_HTF_BREAK_CONFIRM_CLOSES", "2")
	t.Setenv("LIVE_HTF_DELTA_POS", "250000")
	t.Setenv("LIVE_HTF_DELTA_NEG", "-250000")
	base := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	bars := []types.Candle{
		{T: base.Add(0 * time.Hour), O: 108, H: 110, L: 104, C: 109, V: 1000},
		{T: base.Add(1 * time.Hour), O: 109, H: 111, L: 105, C: 110, V: 1000},
		{T: base.Add(2 * time.Hour), O: 110, H: 112, L: 106, C: 111, V: 1000},
		{T: base.Add(3 * time.Hour), O: 111, H: 113, L: 105, C: 110, V: 1000},
		{T: base.Add(4 * time.Hour), O: 110, H: 112, L: 104, C: 109, V: 1000},
		{T: base.Add(5 * time.Hour), O: 109, H: 111, L: 103, C: 108, V: 1000},
		{T: base.Add(6 * time.Hour), O: 108, H: 110, L: 102, C: 107, V: 1000}, // swing low candidate
		{T: base.Add(7 * time.Hour), O: 107, H: 109, L: 103, C: 106, V: 1000},
		{T: base.Add(8 * time.Hour), O: 106, H: 108, L: 104, C: 105, V: 1000},
		{T: base.Add(9 * time.Hour), O: 105, H: 107, L: 103, C: 104, V: 1000},
		{T: base.Add(10 * time.Hour), O: 104, H: 106, L: 100, C: 101, V: 1000}, // close below swing low
		{T: base.Add(11 * time.Hour), O: 101, H: 103, L: 99, C: 100, V: 1000},  // 2nd close below
	}
	now := base.Add(13 * time.Hour)
	snap := buildHTFSnapshotFromCandles("TESTUSDT", "BUY", bars, now)
	if !snap.StructureBreakDown {
		t.Fatalf("expected structure breakdown with two confirming closes below swing low: %+v", snap)
	}
	if snap.BreakConfirmCountDown < 2 {
		t.Fatalf("expected break confirm count >=2, got %d", snap.BreakConfirmCountDown)
	}
	if snap.State != HTFLongBroken {
		t.Fatalf("expected long_broken state, got %s", snap.State)
	}
}
