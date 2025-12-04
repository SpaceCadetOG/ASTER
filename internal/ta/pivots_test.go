package ta_test

import (
	"testing"
	"time"

	"go-machine/internal/ta"
	"go-machine/internal/types"
)

// helper to build a simple series (ascending times, custom H/L)
func mkCandle(t0 time.Time, i int, o, h, l, c float64) types.Candle {
	return types.Candle{
		T: t0.Add(time.Duration(i) * time.Minute),
		O: o, H: h, L: l, C: c, V: 0,
	}
}

// A simple up-then-down sequence that should produce one pivot high and one pivot low
func TestDetectFractalPivots_Basic(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()

	// Build 13 bars:
	// - first hump (increasing highs) peaking at bar 4
	// - then dip to bar 8
	// - then small rise again
	var bars []types.Candle
	// 0..4 rising highs
	for i := 0; i <= 4; i++ {
		h := 100.0 + float64(i)*2.0
		l := h - 3.0
		bars = append(bars, mkCandle(t0, i, l, h, l, h-0.5))
	}
	// 5..8 falling highs/lows (the valley near 8)
	for i := 5; i <= 8; i++ {
		h := 108.0 - float64(i-4)*2.0 // 106,104,102,100
		l := h - 5.0
		bars = append(bars, mkCandle(t0, i, l, h, l, h-0.5))
	}
	// 9..12 mild rise
	for i := 9; i <= 12; i++ {
		h := 101.0 + float64(i-9)*1.0 // 101,102,103,104
		l := h - 3.0
		bars = append(bars, mkCandle(t0, i, l, h, l, h-0.2))
	}

	left, right := 2, 2
	pivs := ta.DetectFractalPivots(bars, left, right)
	if len(pivs) == 0 {
		t.Fatalf("expected some pivots, got none")
	}

	// Count H / L
	var highs, lows int
	for _, p := range pivs {
		switch p.Typ {
		case "H":
			highs++
		case "L":
			lows++
		default:
			t.Fatalf("unexpected pivot type: %q", p.Typ)
		}
	}

	if highs == 0 || lows == 0 {
		t.Fatalf("expected at least one high and one low, got H=%d L=%d (pivots=%v)", highs, lows, pivs)
	}
}
