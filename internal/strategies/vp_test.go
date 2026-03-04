package strategies

import (
	"testing"
	"time"

	"go-machine/internal/data"
	"go-machine/internal/features"
)

func mkC(ts int, o, h, l, c, v float64) features.Candle {
	return features.Candle{Ts: time.Unix(int64(ts), 0), O: o, H: h, L: l, C: c, V: v}
}

func TestApplyRiskPolicyVPHybrid(t *testing.T) {
	sig := Signal{
		Active: true,
		Side:   features.SideLong,
		Entry:  100,
		Stop:   99,
		TP1:    102,
		TP2:    103,
	}
	snap := features.Snapshot{
		VP: features.VolumeProfile{
			VAH:         103,
			VAL:         98,
			TotalVolume: 1000,
			Bins: []features.PriceVolume{
				{Price: 98, Volume: 150},
				{Price: 99, Volume: 100},
				{Price: 100, Volume: 200},
				{Price: 101, Volume: 250},
				{Price: 102, Volume: 160},
			},
		},
	}
	cfg := DefaultRiskPolicy()
	cfg.StopMode = StopModeHybrid
	cfg.TargetMode = TargetModeVP
	out := ApplyRiskPolicy(sig, snap, cfg)
	if out.Stop <= 0 || out.TP1 <= 0 {
		t.Fatalf("invalid resolved levels stop=%.4f tp1=%.4f", out.Stop, out.TP1)
	}
	if out.StopMode == "" || out.TargetMode == "" {
		t.Fatalf("expected policy modes to be set")
	}
}

func TestVPTrendRetestSignal(t *testing.T) {
	c := make([]features.Candle, 0, 40)
	base := 100.0
	for i := 0; i < 40; i++ {
		px := base + float64(i)*0.2
		c = append(c, mkC(i+1, px-0.05, px+0.2, px-0.2, px, 100+float64(i)))
	}
	// Retest into the cluster level.
	c[len(c)-1] = mkC(40, 106.0, 106.1, 105.8, 106.0, 200)
	ctx := Context{
		Symbol:  "BTCUSDT",
		TF:      "1m",
		Candles: c,
		Snapshot: features.Snapshot{
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
	sig := VPTrendRetest{}.Eval(ctx)
	if !sig.Active {
		t.Fatal("expected vp trend retest signal active")
	}
	if sig.VPSetup != "trend_retest" {
		t.Fatalf("expected vp setup trend_retest got %q", sig.VPSetup)
	}
}

func TestRouterVPTargetTooCloseGate(t *testing.T) {
	c := make([]features.Candle, 0, 40)
	for i := 0; i < 40; i++ {
		c = append(c, mkC(i+1, 100, 101, 99, 100.0+float64(i)*0.02, 100))
	}
	ctx := Context{
		Symbol:       "BTCUSDT",
		TF:           "1m",
		ScannerScore: 90,
		ScannerGrade: "A",
		Snapshot: features.Snapshot{
			Structure: features.StructureState{Trend: features.TrendBull},
			VP: features.VolumeProfile{
				Bins: []features.PriceVolume{
					{Price: 100.7, Volume: 200},
					{Price: 100.8, Volume: 300},
					{Price: 100.9, Volume: 200},
				},
				TotalVolume: 700,
			},
		},
		Candles: c,
	}
	r := NewRouter(RouterConfig{
		MinGrade:                  "B",
		MinScore:                  0,
		MinWhaleDelta:             -1e9,
		EnableVPSetups:            true,
		UseVPReversal:             false,
		RejectIfTargetTooClosePct: 5.0,
		RiskPolicy:                DefaultRiskPolicy(),
	})
	out := r.Eval(ctx)
	if len(out) != 0 {
		t.Fatalf("expected no candidates due to target-too-close gate, got %d", len(out))
	}
}

func TestRouterSessionRegimeTagSet(t *testing.T) {
	c := make([]features.Candle, 0, 40)
	ts := time.Date(2026, 3, 4, 8, 10, 0, 0, time.FixedZone("UTC", 0)) // 02:10 CT overlap
	for i := 0; i < 40; i++ {
		c = append(c, features.Candle{
			Ts: ts.Add(time.Duration(i) * time.Minute),
			O:  100, H: 101, L: 99, C: 100.0 + float64(i)*0.01, V: 100,
		})
	}
	ctx := Context{
		Symbol:       "BTCUSDT",
		TF:           "1m",
		ScannerScore: 90,
		ScannerGrade: "A",
		Snapshot: features.Snapshot{
			Structure: features.StructureState{Trend: features.TrendBull},
			Flow:      features.FlowState{WhaleDelta1m: 100},
			VP: features.VolumeProfile{
				Bins: []features.PriceVolume{
					{Price: 100.2, Volume: 200},
					{Price: 100.4, Volume: 300},
					{Price: 100.6, Volume: 220},
				},
				TotalVolume: 720,
			},
		},
		Candles: c,
	}
	r := NewRouter(RouterConfig{
		MinGrade:               "B",
		MinScore:               0,
		MinWhaleDelta:          -1e9,
		EnableVPSetups:         true,
		UseVPReversal:          true,
		UseSessionRegimeRisk:   true,
		EnableInstitutionalPA:  true,
		MinConfluenceScore:     0.2,
		AllowDeadZoneOnlyAPlus: false,
		RiskPolicy:             DefaultRiskPolicy(),
	})
	out := r.Eval(ctx)
	if len(out) == 0 {
		t.Fatalf("expected candidates")
	}
	if out[0].Signal.RegimeTag == "" {
		t.Fatalf("expected regime tag")
	}
	if out[0].Signal.RegimeTag != string(data.OverlapAE) && out[0].Signal.RegimeTag != string(data.RegimeEU) {
		t.Fatalf("unexpected regime tag: %s", out[0].Signal.RegimeTag)
	}
}
