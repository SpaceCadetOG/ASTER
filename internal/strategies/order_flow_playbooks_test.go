package strategies

import (
	"testing"

	"go-machine/internal/features"
)

func TestStackedImbalancesEvalLong(t *testing.T) {
	ctx := Context{
		Candles: []features.Candle{
			mkC(1, 100, 101, 99.5, 100.4, 100),
			mkC(2, 100.4, 101.4, 100.1, 101.1, 180),
		},
		Snapshot: features.Snapshot{
			Candle: features.Candle{C: 101.1, H: 101.4, L: 100.1},
			Flow: features.FlowState{
				VolumeSpike:          true,
				StackedImbalanceBull: true,
			},
		},
	}
	sig := StackedImbalances{}.Eval(ctx)
	if !sig.Active || sig.Name != "stacked_imbalances" || sig.Side != features.SideLong {
		t.Fatalf("expected active long stacked imbalance signal, got %+v", sig)
	}
}

func TestUnfinishedBusinessEvalShort(t *testing.T) {
	ctx := Context{
		Candles: []features.Candle{
			mkC(1, 100, 101, 99.8, 100.2, 100),
			mkC(2, 100.2, 100.8, 99.5, 99.7, 150),
		},
		Snapshot: features.Snapshot{
			Candle: features.Candle{C: 99.7, H: 100.8, L: 99.5},
			Flow: features.FlowState{
				WhaleDelta1m:         -50,
				UnfinishedBusinessDn: true,
			},
		},
	}
	sig := UnfinishedBusiness{}.Eval(ctx)
	if !sig.Active || sig.Side != features.SideShort {
		t.Fatalf("expected unfinished business short signal, got %+v", sig)
	}
}

func TestVolumeClustersEvalTrendLong(t *testing.T) {
	ctx := Context{
		Candles: []features.Candle{
			mkC(1, 100, 100.5, 99.8, 100.1, 100),
			mkC(2, 100.1, 100.8, 99.9, 100.3, 120),
			mkC(3, 100.3, 100.9, 100.0, 100.2, 130),
			mkC(4, 100.2, 100.7, 99.9, 100.0, 140),
			mkC(5, 100.0, 100.6, 99.7, 100.15, 150),
			mkC(6, 100.15, 100.7, 99.9, 100.18, 150),
			mkC(7, 100.18, 100.8, 99.95, 100.22, 160),
			mkC(8, 100.22, 100.85, 100.0, 100.21, 160),
		},
		Snapshot: features.Snapshot{
			Candle:    features.Candle{C: 100.21, H: 100.85, L: 100.0},
			Structure: features.StructureState{Trend: features.TrendBull},
			Flow:      features.FlowState{WhaleDelta1m: 75, VolumeSpike: true},
			VP: features.VolumeProfile{
				HVNs: []features.PriceVolume{{Price: 100.2, Volume: 250}, {Price: 101.0, Volume: 180}},
			},
		},
	}
	sig := VolumeClusters{}.Eval(ctx)
	if !sig.Active || sig.Side != features.SideLong {
		t.Fatalf("expected volume cluster long signal, got %+v", sig)
	}
}

func TestMultipleNodesEval(t *testing.T) {
	ctx := Context{
		Snapshot: features.Snapshot{
			Candle:    features.Candle{C: 100.4, H: 100.8, L: 100.1},
			Structure: features.StructureState{Trend: features.TrendBull},
			Flow:      features.FlowState{WhaleDelta1m: 60},
			VP: features.VolumeProfile{
				HVNs: []features.PriceVolume{
					{Price: 100.1, Volume: 200},
					{Price: 100.7, Volume: 220},
				},
			},
		},
	}
	sig := MultipleNodes{}.Eval(ctx)
	if !sig.Active || sig.Name != "multiple_nodes" {
		t.Fatalf("expected multiple_nodes signal, got %+v", sig)
	}
}

func TestTradesFilterEval(t *testing.T) {
	ctx := Context{
		Candles: []features.Candle{
			mkC(1, 100, 100.4, 99.7, 100.1, 100),
			mkC(2, 100.1, 100.8, 100.0, 100.7, 220),
		},
		Snapshot: features.Snapshot{
			Candle: features.Candle{C: 100.7, H: 100.8, L: 100.0},
			Flow: features.FlowState{
				VolumeSpike:       true,
				LargeTradeCount1m: 4,
				LargeBuyCount1m:   3,
				LargeSellCount1m:  1,
				WhaleDelta1m:      125,
				WhaleDeltaCum:     300,
			},
		},
	}
	sig := TradesFilter{}.Eval(ctx)
	if !sig.Active || sig.Name != "trades_filter" {
		t.Fatalf("expected trades_filter signal, got %+v", sig)
	}
}
