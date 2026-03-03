package features

import (
	"testing"
	"time"
)

func mk(ts int, o, h, l, c float64) Candle {
	return Candle{Ts: time.Unix(int64(ts), 0), O: o, H: h, L: l, C: c, V: 100}
}

func TestPivotDetectionCausal(t *testing.T) {
	c := []Candle{
		mk(1, 1, 2, 0.9, 1.8),
		mk(2, 1.8, 2.5, 1.7, 2.2),
		mk(3, 2.2, 2.9, 2.0, 2.1), // high pivot
		mk(4, 2.1, 2.2, 1.5, 1.6),
		mk(5, 1.6, 1.7, 1.2, 1.3),
		mk(6, 1.3, 1.4, 1.0, 1.2),
	}
	p := detectPivotsCausal(c, 1, 1)
	if len(p) == 0 {
		t.Fatal("expected pivots")
	}
}

func TestEqualLowsSweep(t *testing.T) {
	eng := NewLiquidityEngine(LiquidityConfig{TolBps: 20, MinCount: 2, Lookback: 20})
	c := []Candle{
		mk(1, 10, 10.2, 9.8, 10.1),
		mk(2, 10.1, 10.2, 9.8, 10.0),
		mk(3, 10.0, 10.1, 9.7, 10.05),
		mk(4, 10.05, 10.3, 9.6, 10.15), // sweep below and close back above ~9.8
	}
	pools, sweep := eng.Eval(c)
	if len(pools) == 0 {
		t.Fatal("expected pools")
	}
	if sweep == nil || !sweep.CloseBackInside {
		t.Fatal("expected sweep event")
	}
}

func TestFVGDetectAndMitigate(t *testing.T) {
	eng := NewFVGEngine(FVGConfig{MaxAge: 20})
	c := []Candle{
		mk(1, 10, 10.2, 9.9, 10.1),
		mk(2, 10.1, 10.5, 10.0, 10.4),
		mk(3, 10.6, 10.9, 10.6, 10.8), // c1 high < c3 low => bullish fvg
	}
	zones, _ := eng.Eval(c)
	if len(zones) == 0 {
		t.Fatal("expected fvg zone")
	}
	c = append(c, mk(4, 10.8, 10.85, 10.15, 10.3))
	zones, _ = eng.Eval(c)
	if len(zones) == 0 || !zones[0].Mitigated {
		t.Fatal("expected mitigated zone")
	}
}

func TestVolumeProfilePOCAndValueArea(t *testing.T) {
	eng := NewVolumeProfileEngine(VolumeProfileConfig{
		Bins:     20,
		ValuePct: 0.70,
		HVNTopN:  2,
		LVNTopN:  2,
	})
	c := []Candle{
		{Ts: time.Unix(1, 0), O: 100, H: 101, L: 99, C: 100, V: 100},
		{Ts: time.Unix(2, 0), O: 100, H: 101, L: 99, C: 100, V: 180},
		{Ts: time.Unix(3, 0), O: 100, H: 100.5, L: 99.5, C: 100, V: 220},
		{Ts: time.Unix(4, 0), O: 102, H: 103, L: 101, C: 102, V: 40},
	}
	vp := eng.Eval(c)
	if vp.TotalVolume <= 0 {
		t.Fatal("expected total volume > 0")
	}
	if vp.POCPrice <= 0 {
		t.Fatal("expected poc > 0")
	}
	if vp.VAH < vp.VAL {
		t.Fatalf("invalid value area bounds: vah %.4f < val %.4f", vp.VAH, vp.VAL)
	}
	if len(vp.HVNs) == 0 || len(vp.LVNs) == 0 {
		t.Fatal("expected hvn/lvn points")
	}
	if vp.POCShare <= 0 || vp.POCShare > 1 {
		t.Fatalf("expected valid poc share, got %.4f", vp.POCShare)
	}
	if vp.VAWidthPct <= 0 {
		t.Fatalf("expected positive value area width pct, got %.4f", vp.VAWidthPct)
	}
	if vp.Shape != "D" && vp.Shape != "P" && vp.Shape != "b" {
		t.Fatalf("unexpected shape: %q", vp.Shape)
	}
	if vp.NearestHVNAbove <= 0 && vp.NearestHVNBelow <= 0 {
		t.Fatalf("expected at least one nearest hvn level, got above=%.4f below=%.4f", vp.NearestHVNAbove, vp.NearestHVNBelow)
	}
	if vp.FirstOpposingVolumeDistPct <= 0 {
		t.Fatalf("expected first opposing distance pct > 0, got %.4f", vp.FirstOpposingVolumeDistPct)
	}
}

func TestVolumeProfileFlexibleHelpers(t *testing.T) {
	vp := VolumeProfile{
		TotalVolume: 1000,
		VAH:         101.5,
		VAL:         98.5,
		Bins: []PriceVolume{
			{Price: 98.0, Volume: 80},
			{Price: 99.0, Volume: 120},
			{Price: 100.0, Volume: 320},
			{Price: 101.0, Volume: 220},
			{Price: 102.0, Volume: 160},
		},
	}
	lvl, ok := vp.LevelAtHeaviestInRange(99.2, 101.2)
	if !ok || lvl != 100.0 {
		t.Fatalf("expected heaviest in range at 100.0, got lvl=%.4f ok=%v", lvl, ok)
	}
	lvl, ok = vp.FirstSignificantOpposingLevel(100.0, SideLong, 0.10)
	if !ok || lvl <= 100 {
		t.Fatalf("expected long opposing level > entry, got lvl=%.4f ok=%v", lvl, ok)
	}
	lvl, ok = vp.FirstSignificantOpposingLevel(100.0, SideShort, 0.10)
	if !ok || lvl >= 100 {
		t.Fatalf("expected short opposing level < entry, got lvl=%.4f ok=%v", lvl, ok)
	}
}
