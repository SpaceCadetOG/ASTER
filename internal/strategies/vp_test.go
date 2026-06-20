package strategies

import (
	"os"
	"path/filepath"
	"strings"
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
	body, err := os.ReadFile(filepath.Join("router.go"))
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	if strings.Contains(string(body), "target_too_close") {
		t.Fatalf("expected target_too_close gate to be removed from router")
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
		MinGrade:              "B",
		MinScore:              0,
		MinWhaleDelta:         -1e9,
		EnableVPSetups:        true,
		UseVPReversal:         true,
		UseSessionRegimeRisk:  true,
		EnableInstitutionalPA: true,
		MinConfluenceScore:    0.2,
		RiskPolicy:            DefaultRiskPolicy(),
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

func TestApplyRiskPolicyUsesSetupFamilyTemplates(t *testing.T) {
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
	cfg.StopMode = StopModeFixed

	pullback := Signal{
		Active: true,
		Name:   "bos_pb",
		Side:   features.SideLong,
		Entry:  100,
		Stop:   99.4,
		TP1:    102,
		TP2:    103,
		Tags:   []string{"pullback", "retest"},
	}
	reversal := Signal{
		Active: true,
		Name:   "failed_auction_magnet",
		Side:   features.SideLong,
		Entry:  100,
		Stop:   99.4,
		TP1:    102,
		TP2:    103,
		Tags:   []string{"failed_auction", "reversal"},
	}
	outPullback := ApplyRiskPolicy(pullback, snap, cfg)
	outReversal := ApplyRiskPolicy(reversal, snap, cfg)
	if !(outReversal.Stop < outPullback.Stop) {
		t.Fatalf("expected reversal stop wider than pullback stop, pullback=%.4f reversal=%.4f", outPullback.Stop, outReversal.Stop)
	}
}

func TestRouterRejectsLateShortContinuationWithoutReset(t *testing.T) {
	c := make([]features.Candle, 0, 40)
	for i := 0; i < 40; i++ {
		c = append(c, mkC(i+1, 100, 101, 99, 100.0-float64(i)*0.2, 100))
	}
	ctx := Context{
		Symbol:       "LYNUSDT",
		TF:           "1m",
		ScannerScore: 95,
		ScannerGrade: "A+",
		ScoreSlope:   0.05,
		DayUTCPct:    -36,
		UTC1hPct:     -4.0,
		Snapshot: features.Snapshot{
			Structure: features.StructureState{Trend: features.TrendBear},
			Flow:      features.FlowState{WhaleDelta1m: -100},
			OBs: []features.OBZone{
				{Rejected: true, Side: features.SideShort, High: 101, Low: 99.5},
			},
			Candle: c[len(c)-1],
		},
		Candles: c,
	}
	r := NewRouter(RouterConfig{
		MinGrade:                 "B",
		MinScore:                 0,
		MinWhaleDelta:            -1e9,
		MinConfluenceScore:       0.1,
		ContinuationDayUTCPct:    25,
		ContinuationReset1hPct:   0.8,
		ContinuationLateSlopeMin: 0.16,
		RiskPolicy:               DefaultRiskPolicy(),
	})
	out := r.Eval(ctx)
	if len(out) != 0 {
		t.Fatalf("expected no candidates due to late extension without reset, got %d", len(out))
	}
}
