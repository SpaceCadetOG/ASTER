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
