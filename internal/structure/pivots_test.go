package structure

import (
	"go-machine/internal/types"
	"testing"
	"time"
)

func makeSeq(vals []float64) []types.Candle {
	out := make([]types.Candle, len(vals))
	for i, v := range vals {
		out[i] = types.Candle{T: time.Unix(int64(i), 0), O: v, H: v, L: v, C: v, V: 1}
	}
	return out
}

func TestSwingHighLow(t *testing.T) {
	c := makeSeq([]float64{1, 2, 3, 2, 1, 2, 4, 2, 1})
	highs := SwingHighIdx(c, 1)
	lows := SwingLowIdx(c, 1)

	if len(highs) != 2 {
		t.Fatalf("expected 2 highs got %d", len(highs))
	}
	if len(lows) != 2 {
		t.Fatalf("expected 2 lows got %d", len(lows))
	}
}
