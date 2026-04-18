package strategies

import (
	"testing"
	"time"

	"go-machine/internal/features"
)

func TestCalculateConfluenceScore_AutoEntryLong(t *testing.T) {
	now := time.Now().UTC()
	candles := []features.Candle{
		{Ts: now.Add(-90 * time.Minute), O: 100, H: 101, L: 99.5, C: 100.5, V: 1200},
		{Ts: now.Add(-75 * time.Minute), O: 100.5, H: 104, L: 100.2, C: 103.8, V: 1600},
		{Ts: now.Add(-60 * time.Minute), O: 103.8, H: 108, L: 103.2, C: 107.5, V: 1900},
		{Ts: now.Add(-45 * time.Minute), O: 107.5, H: 110, L: 107.0, C: 109.5, V: 2000},
		{Ts: now.Add(-30 * time.Minute), O: 109.5, H: 110.2, L: 105.8, C: 106.8, V: 1800}, // golden pocket region
		{Ts: now.Add(-15 * time.Minute), O: 106.8, H: 108.4, L: 106.5, C: 108.1, V: 1700},
	}
	ctx := Context{
		Candles: candles,
		Snapshot: features.Snapshot{
			Candle: features.Candle{C: 108.1},
			Flow: features.FlowState{
				WhaleDelta1m:  450,
				WhaleDeltaCum: 2000,
			},
			VP: features.VolumeProfile{
				HVNs: []features.PriceVolume{{Price: 108.0, Volume: 100000}},
			},
		},
	}
	flow := OrderFlowSignal{
		CumulativeDelta: 2000,
		DeltaRising:     true,
		HasStackedBuy:   true,
	}
	res := CalculateConfluenceScore(ctx, features.SideLong, flow)
	if !res.Approved {
		t.Fatalf("expected approved score, got %.2f", res.Score)
	}
	if res.Score < 85 {
		t.Fatalf("expected auto-entry score >=85, got %.2f", res.Score)
	}
	if res.Tier != ConfluenceTierAutoEntry {
		t.Fatalf("expected auto-entry tier, got %s", res.Tier)
	}
}

func TestCalculateConfluenceScore_WatchlistStackedBoost(t *testing.T) {
	now := time.Now().UTC()
	candles := []features.Candle{
		{Ts: now.Add(-75 * time.Minute), O: 100, H: 103, L: 99.8, C: 102.9, V: 900},
		{Ts: now.Add(-60 * time.Minute), O: 102.9, H: 104.5, L: 102.5, C: 104.1, V: 950},
		{Ts: now.Add(-45 * time.Minute), O: 104.1, H: 106.0, L: 103.9, C: 105.7, V: 990},
		{Ts: now.Add(-30 * time.Minute), O: 105.7, H: 106.1, L: 104.0, C: 104.8, V: 970},
		{Ts: now.Add(-15 * time.Minute), O: 104.8, H: 105.2, L: 104.2, C: 104.9, V: 930},
	}
	ctx := Context{
		Candles: candles,
		Snapshot: features.Snapshot{
			Candle: features.Candle{C: 104.9},
			Flow: features.FlowState{
				WhaleDelta1m:  120,
				WhaleDeltaCum: 300,
			},
			VP: features.VolumeProfile{
				HVNs: []features.PriceVolume{{Price: 104.95, Volume: 50000}},
			},
		},
	}
	flow := OrderFlowSignal{
		CumulativeDelta: 300,
		DeltaRising:     true,
		HasStackedBuy:   true,
	}
	res := CalculateConfluenceScore(ctx, features.SideLong, flow)
	if res.StackedFlow <= 0 {
		t.Fatalf("expected stacked flow boost")
	}
	if res.Score < 80 {
		t.Fatalf("expected boosted score >=80, got %.2f", res.Score)
	}
}

func TestCalculateConfluenceScore_BelowThreshold(t *testing.T) {
	now := time.Now().UTC()
	ctx := Context{
		Candles: []features.Candle{
			{Ts: now.Add(-30 * time.Minute), O: 100, H: 100.4, L: 99.7, C: 100.0, V: 200},
			{Ts: now.Add(-15 * time.Minute), O: 100.0, H: 100.2, L: 99.8, C: 99.9, V: 220},
		},
		Snapshot: features.Snapshot{
			Candle: features.Candle{C: 99.9},
			Flow: features.FlowState{
				WhaleDelta1m:  -50,
				WhaleDeltaCum: -120,
			},
			VP: features.VolumeProfile{},
		},
	}
	flow := OrderFlowSignal{
		CumulativeDelta: -120,
		DeltaRising:     false,
	}
	res := CalculateConfluenceScore(ctx, features.SideLong, flow)
	if res.Approved {
		t.Fatalf("expected rejection below threshold, got %.2f", res.Score)
	}
	if res.Score >= 70 {
		t.Fatalf("expected score <70, got %.2f", res.Score)
	}
}
