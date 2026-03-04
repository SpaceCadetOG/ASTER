package strategies

import (
	"testing"
	"time"

	"go-machine/internal/features"
)

func mkc(ts int, o, h, l, c float64) features.Candle {
	return features.Candle{Ts: time.Unix(int64(ts), 0), O: o, H: h, L: l, C: c, V: 100}
}

func TestDailyOpenSREval(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	start := time.Date(2026, 3, 4, 7, 0, 0, 0, loc).UTC()
	candles := make([]features.Candle, 0, 30)
	for i := 0; i < 30; i++ {
		c := 100.0 + float64(i)*0.02
		candles = append(candles, features.Candle{Ts: start.Add(time.Duration(i) * time.Minute), O: c, H: c + 0.2, L: c - 0.2, C: c, V: 120})
	}
	// Retest near daily open.
	candles[len(candles)-1].C = candles[0].O
	ctx := Context{Candles: candles, Snapshot: features.Snapshot{Flow: features.FlowState{WhaleDelta1m: 10}}}
	sig := DailyOpenSR{}.Eval(ctx)
	if !sig.Active {
		t.Fatal("expected daily open signal")
	}
}

func TestPDLevelsRetestEval(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	start := time.Date(2026, 3, 2, 16, 1, 0, 0, loc).UTC()
	candles := make([]features.Candle, 0, 120)
	for i := 0; i < 120; i++ {
		p := 100 + float64(i)*0.05
		candles = append(candles, features.Candle{Ts: start.Add(time.Duration(i) * time.Minute), O: p, H: p + 0.4, L: p - 0.4, C: p, V: 90})
	}
	// Force acceptance above and retest near level.
	candles[len(candles)-3].C = candles[len(candles)-3].H + 0.2
	candles[len(candles)-2].C = candles[len(candles)-2].H + 0.2
	candles[len(candles)-1].C = candles[len(candles)-30].H
	ctx := Context{Candles: candles, Snapshot: features.Snapshot{Anchors: features.AnchorLevels{DailyOpen: candles[0].O}}}
	sig := PDLevelsRetest{}.Eval(ctx)
	if sig.Name != "pd_levels_retest" {
		t.Fatalf("unexpected signal name: %s", sig.Name)
	}
}

func TestVWAPConfluenceEval(t *testing.T) {
	candles := make([]features.Candle, 0, 25)
	for i := 0; i < 25; i++ {
		p := 100 + float64(i)*0.2
		candles = append(candles, mkc(i+1, p-0.1, p+0.2, p-0.2, p))
	}
	ctx := Context{Candles: candles, Snapshot: features.Snapshot{Flow: features.FlowState{WhaleDelta1m: 200}}}
	sig := VWAPConfluenceStrategy{}.Eval(ctx)
	if !sig.Active {
		t.Fatal("expected vwap confluence signal")
	}
}

func TestFailedAuctionMagnetEval(t *testing.T) {
	candles := []features.Candle{
		mkc(1, 100, 105, 99, 104),
		mkc(2, 104, 105.02, 101, 103),
		mkc(3, 103, 103.2, 98, 98.4),
		mkc(4, 98.4, 99.0, 97.6, 98.0),
		mkc(5, 98.0, 98.8, 97.0, 97.2),
		mkc(6, 97.2, 98.0, 96.8, 97.4),
		mkc(7, 97.4, 98.5, 97.0, 98.2),
		mkc(8, 98.2, 99.1, 97.9, 98.7),
		mkc(9, 98.7, 99.2, 98.2, 98.6),
		mkc(10, 98.6, 99.0, 98.0, 98.4),
		mkc(11, 98.4, 98.9, 97.8, 98.1),
		mkc(12, 98.1, 98.8, 97.6, 98.0),
	}
	ctx := Context{Candles: candles, Snapshot: features.Snapshot{Flow: features.FlowState{WhaleDelta1m: -50}}}
	sig := FailedAuctionMagnetStrategy{}.Eval(ctx)
	if !sig.Active {
		t.Fatal("expected failed auction magnet signal")
	}
}
