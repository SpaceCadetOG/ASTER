// go-machine/internal/ta/fusion_test.go
package ta

import (
	"testing"
	"time"

	"go-machine/internal/types"
)

func makeBars(n int, base float64, step float64) []types.Candle {
	out := make([]types.Candle, n)
	t0 := time.Unix(0, 0).UTC()
	price := base
	for i := 0; i < n; i++ {
		o := price
		h := o * 1.002
		l := o * 0.998
		c := o + step
		out[i] = types.Candle{
			T: t0.Add(time.Duration(i) * time.Minute),
			O: o, H: h, L: l, C: c, V: 1_000_000,
		}
		price = c
	}
	return out
}

func TestComputeFusion_Bullish(t *testing.T) {
	sym := "TESTUSDT"
	tf := types.TF15m
	bars := makeBars(120, 10000, 2) // uptrend

	tr := TrendResult{
		Symbol: sym, TF: string(tf),
		EMARatio: 1.003, AboveVWAP: 0.7,
		Slope9: 3, Slope21: 2, Bias: "bull",
		TrendScore: 70,
	}
	ef := EffortResult{
		Symbol: sym, TF: string(tf),
		EffortScore: 40, SpikeDensity: 0.06,
		EMAvol: 12, MeanVol: 10,
	}
	ob := OBContext{
		Imbalance:  +0.30,
		TopBidWall: &OBWall{Rank: 1, Size: 40, Side: "bid"},
		TopAskWall: &OBWall{Rank: 5, Size: 10, Side: "ask"},
		LevelsUsed: 50,
	}

	res := ComputeFusion(sym, tf, bars, tr, ef, ob, 3, 3)
	if res.Side != "long" {
		t.Fatalf("expected side=long, got %s", res.Side)
	}
	if !(res.Grade == "A" || res.ConfluenceScore >= 70) {
		t.Fatalf("expected strong grade/score, got grade=%s score=%.2f", res.Grade, res.ConfluenceScore)
	}
	if res.Zone == "" || res.Structure == "" {
		t.Fatalf("expected zone/structure filled")
	}
}

func TestComputeFusion_Bearish(t *testing.T) {
	sym := "TESTUSDT"
	tf := types.TF15m
	bars := makeBars(120, 10000, -2) // downtrend

	tr := TrendResult{
		Symbol: sym, TF: string(tf),
		EMARatio: 0.997, AboveVWAP: 0.3,
		Slope9: -3, Slope21: -2, Bias: "bear",
		TrendScore: 68,
	}
	ef := EffortResult{
		Symbol: sym, TF: string(tf),
		EffortScore: 30, SpikeDensity: 0.04,
		EMAvol: 9, MeanVol: 10,
	}
	ob := OBContext{
		Imbalance:  +0.30,
		TopBidWall: &OBWall{Rank: 1, Size: 40, Side: "bid"},
		TopAskWall: &OBWall{Rank: 5, Size: 10, Side: "ask"},
		LevelsUsed: 50,
	}

	res := ComputeFusion(sym, tf, bars, tr, ef, ob, 3, 3)
	if res.Side != "short" {
		t.Fatalf("expected side=short, got %s", res.Side)
	}
	if res.ConfluenceScore < 55 {
		t.Fatalf("expected >=55, got %.2f", res.ConfluenceScore)
	}
}
