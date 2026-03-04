package levels

import (
	"testing"
	"time"

	"go-machine/internal/features"
)

func mk(ts time.Time, o, h, l, c, v float64) features.Candle {
	return features.Candle{Ts: ts, O: o, H: h, L: l, C: c, V: v}
}

func TestDailyOpenSeriesAndAnchor(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	base := time.Date(2026, 3, 3, 6, 59, 0, 0, loc)
	candles := []features.Candle{
		mk(base, 100, 101, 99, 100.2, 10),
		mk(base.Add(time.Minute), 101, 102, 100, 101.2, 10),
		mk(base.Add(2*time.Minute), 102, 103, 101, 102.2, 10),
	}
	open, ok := OpenForAnchorDay(candles, candles[2].Ts, 7, loc)
	if !ok {
		t.Fatal("expected open")
	}
	if open != 101 {
		t.Fatalf("expected 101 got %.2f", open)
	}
}

func TestPrevLevelsAt(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	start := time.Date(2026, 3, 1, 15, 50, 0, 0, loc)
	candles := make([]features.Candle, 0, 200)
	for i := 0; i < 1800; i++ {
		c := 100 + float64(i)*0.1
		candles = append(candles, mk(start.Add(time.Duration(i)*time.Minute), c, c+1, c-1, c, 10))
	}
	pl, ok := PrevLevelsAt(candles, len(candles)-1, 16, loc)
	if !ok {
		t.Fatal("expected prev levels")
	}
	if pl.PDH <= 0 || pl.PDL <= 0 {
		t.Fatalf("bad prev levels: %+v", pl)
	}
}

func TestDetectStrongWeakSwings(t *testing.T) {
	now := time.Now().UTC()
	candles := []features.Candle{
		mk(now.Add(1*time.Minute), 100, 101, 99, 100.5, 10),
		mk(now.Add(2*time.Minute), 100.5, 102.5, 100, 101.0, 10),
		mk(now.Add(3*time.Minute), 101, 101.5, 99.2, 99.4, 10),
		mk(now.Add(4*time.Minute), 99.4, 100.0, 98.8, 99.8, 10),
		mk(now.Add(5*time.Minute), 99.8, 101.0, 99.5, 100.8, 10),
	}
	sw := DetectStrongWeakSwings(candles, 1, 0.1)
	if len(sw) == 0 {
		t.Fatal("expected swings")
	}
}

func TestDetectFailedAuctionMagnet(t *testing.T) {
	now := time.Now().UTC()
	candles := []features.Candle{
		mk(now.Add(1*time.Minute), 100, 105, 99, 104, 10),
		mk(now.Add(2*time.Minute), 104, 105.02, 101, 103, 10),
		mk(now.Add(3*time.Minute), 103, 103.5, 97, 98, 10),
		mk(now.Add(4*time.Minute), 98, 99, 96, 97, 10),
		mk(now.Add(5*time.Minute), 97, 98, 95.5, 96.8, 10),
	}
	m, ok := DetectFailedAuctionMagnet(candles, 10, 0.05)
	if !ok {
		t.Fatal("expected failed auction magnet")
	}
	if m.Level <= 0 {
		t.Fatalf("bad magnet: %+v", m)
	}
}
