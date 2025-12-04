package ta_test

import (
	"testing"
	"time"

	"go-machine/internal/ta"
	"go-machine/internal/types"
)

func c(t time.Time, i int, o, h, l, c float64) types.Candle {
	return types.Candle{T: t.Add(time.Duration(i) * time.Minute), O: o, H: h, L: l, C: c}
}

// Build a sequence that guarantees alternating H/L pivots → validate swings alternate up/down.
func TestBuildSwings_Alternation(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()

	// Construct a clear H-L-H-L sequence:
	// indexes:    0   1   2   3   4   5   6
	// highs:     100 105 103 101 104 102 103
	// lows:       95  96  94  93  96  95  96
	bars := []types.Candle{
		c(t0, 0, 97, 100, 95, 99),
		c(t0, 1, 98, 105, 96, 104), // local H near 1
		c(t0, 2, 96, 103, 94, 95),
		c(t0, 3, 95, 101, 93, 94),  // local L near 3
		c(t0, 4, 97, 104, 96, 103), // local H near 4
		c(t0, 5, 96, 102, 95, 96),  // local L near 5
		c(t0, 6, 97, 103, 96, 102),
	}

	pivs := ta.DetectFractalPivots(bars, 1, 1)
	if len(pivs) < 3 {
		t.Fatalf("need >=3 pivots for swings, got %d: %v", len(pivs), pivs)
	}

	sw := ta.BuildSwings(pivs)
	if len(sw) == 0 {
		t.Fatalf("expected some swings, got none (pivs=%v)", pivs)
	}

	// Swings should alternate dir ("up"/"down") after coalescing
	last := sw[0].Dir
	for i := 1; i < len(sw); i++ {
		if sw[i].Dir == last {
			t.Fatalf("expected alternating swing dirs, but sw[%d].Dir == %q equals previous", i, sw[i].Dir)
		}
		last = sw[i].Dir
	}

	// Validate pct is signed and non-zero
	for i, s := range sw {
		if s.Pct == 0 {
			t.Fatalf("swing %d has zero pct; from %.2f to %.2f", i, s.FromP, s.ToP)
		}
		if s.Dir == "up" && s.Pct < 0 {
			t.Fatalf("swing %d up but pct negative: %f", i, s.Pct)
		}
		if s.Dir == "down" && s.Pct > 0 {
			t.Fatalf("swing %d down but pct positive: %f", i, s.Pct)
		}
	}
}
