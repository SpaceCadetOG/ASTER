// go-machine/internal/ta/patterns_test.go
package ta

import (
	"testing"
	"time"

	"go-machine/internal/types"
)

func c(ti int, O, H, L, C float64) types.Candle {
	// simple increasing timestamps
	return types.Candle{T: time.Unix(int64(ti*60), 0), O: O, H: H, L: L, C: C}
}

func find(tags []PatternTag, name PatternID) *PatternTag {
	for i := range tags {
		if tags[i].Name == name {
			return &tags[i]
		}
	}
	return nil
}

func TestHammer(t *testing.T) {
	// Lower wick long, small upper, body present
	bars := []types.Candle{
		c(0, 100, 105, 80, 104), // hammer-ish
	}
	tags := DetectPatterns(bars)
	tag := find(tags, PatHammer)
	if tag == nil {
		t.Fatalf("expected Hammer, got none")
	}
	if tag.Dir != DirBull {
		t.Fatalf("hammer dir want bull, got %v", tag.Dir)
	}
	if tag.Strength <= 0 {
		t.Fatalf("hammer strength should be >0")
	}
}

func TestShootingStar(t *testing.T) {
	bars := []types.Candle{
		c(0, 96, 120, 95, 97), // big upper wick, small body near low
	}
	tags := DetectPatterns(bars)
	tag := find(tags, PatShootingStar)
	if tag == nil {
		t.Fatalf("expected ShootingStar, got none")
	}
	if tag.Dir != DirBear {
		t.Fatalf("dir want bear")
	}
}

func TestDoji(t *testing.T) {
	bars := []types.Candle{
		c(0, 100, 110, 90, 100.5), // body tiny vs range
	}
	tags := DetectPatterns(bars)
	if find(tags, PatDoji) == nil {
		t.Fatalf("expected Doji")
	}
}

func TestMarubozuBull(t *testing.T) {
	bars := []types.Candle{
		c(0, 100, 110, 100, 110), // full body, no wicks
	}
	tags := DetectPatterns(bars)
	if find(tags, PatMarubozuBull) == nil {
		t.Fatalf("expected MarubozuBull")
	}
}

func TestEngulfing(t *testing.T) {
	bars := []types.Candle{
		c(0, 110, 111, 90, 95), // bear body
		c(1, 94, 120, 93, 115), // bull body that engulfs
	}
	tags := DetectPatterns(bars)
	if find(tags, PatBullEngulf) == nil {
		t.Fatalf("expected Bullish Engulfing on second candle")
	}
}
