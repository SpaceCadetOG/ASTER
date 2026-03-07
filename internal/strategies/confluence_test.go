package strategies

import (
	"testing"
	"time"

	"go-machine/internal/features"
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
