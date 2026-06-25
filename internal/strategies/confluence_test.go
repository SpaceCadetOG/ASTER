package strategies

import (
	"testing"
	"time"

	"go-machine/internal/features"
	"go-machine/internal/risk"
)

type testStrat struct {
	sig Signal
}

func (t testStrat) Name() string        { return "test" }
func (t testStrat) Eval(Context) Signal { return t.sig }

func TestRouterConfluenceThreshold(t *testing.T) {
	r := NewRouter(RouterConfig{
		MinGrade:           "B",
		MinScore:           0,
		AllowWarmup:        true,
		RiskShellEnabled:   true,
		MinConfluenceScore: 0.70,
		MaxOne:             false,
	})
	r.strat = []Strategy{
		testStrat{sig: Signal{
			Active:     true,
			Name:       "x",
			Side:       features.SideLong,
			Entry:      100,
			Stop:       99,
			TP1:        101,
			Confidence: 0.55,
			Ts:         time.Now(),
		}},
	}
	out := r.Eval(Context{
		ScannerGrade: "A",
		ScannerScore: 90,
		Snapshot: features.Snapshot{
			Flow:      features.FlowState{WhaleDelta1m: -10},
			Structure: features.StructureState{Trend: features.TrendBear},
		},
	})
	if len(out) != 0 {
		t.Fatalf("expected rejection below weighted confluence")
	}
}

func TestRouterCanDisableRiskShellRejects(t *testing.T) {
	c := make([]features.Candle, 0, 40)
	base := 100.0
	for i := 0; i < 40; i++ {
		px := base + float64(i)*0.2
		c = append(c, features.Candle{
			Ts: time.Unix(int64(i+1), 0),
			O:  px - 0.05,
			H:  px + 0.2,
			L:  px - 0.2,
			C:  px,
			V:  100 + float64(i),
		})
	}
	ctx := Context{
		Symbol:       "TESTUSDT",
		ScannerGrade: "A",
		ScannerScore: 90,
		SpreadBps:    50,
		TopBookUSD:   10,
		Candles:      c,
		Snapshot: features.Snapshot{
			Candle:    c[len(c)-1],
			Structure: features.StructureState{Trend: features.TrendBull},
			Flow:      features.FlowState{WhaleDelta1m: 1000},
			VP: features.VolumeProfile{
				Bins: []features.PriceVolume{
					{Price: 104.0, Volume: 80},
					{Price: 105.0, Volume: 120},
					{Price: 106.0, Volume: 350},
					{Price: 107.0, Volume: 160},
				},
			},
		},
	}
	cfg := risk.DefaultConfig()
	cfg.MaxSpreadBps = 1
	r := NewRouter(RouterConfig{
		MinGrade:           "B",
		MinScore:           0,
		AllowWarmup:        true,
		RiskShellEnabled:   false,
		RiskShell:          risk.NewRiskShell(cfg),
		MinConfluenceScore: 0.20,
		MaxOne:             false,
	})
	r.strat = []Strategy{
		testStrat{sig: Signal{
			Active:     true,
			Name:       "x",
			Side:       features.SideLong,
			Entry:      100,
			Stop:       99,
			TP1:        101,
			Confidence: 0.80,
			Ts:         time.Now(),
		}},
	}
	out := r.Eval(ctx)
	if len(out) == 0 {
		t.Fatalf("expected candidate when risk shell is disabled")
	}
	if out[0].Signal.RejectReason != "" {
		t.Fatalf("expected no risk-shell reject, got %+v", out[0].Signal)
	}
}
