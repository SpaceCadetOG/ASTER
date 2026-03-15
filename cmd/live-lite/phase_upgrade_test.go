package main

import (
	"testing"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/inplay"
	"go-machine/internal/strategies"
)

func TestDeriveTriggerStateReclaimLong(t *testing.T) {
	c := candidate{
		Side:          "BUY",
		LastClose:     101,
		SessionVWAP:   100,
		EMA9:          100.5,
		OFIZ:          0.8,
		SpreadBps:     6,
		BookImbalance: 1.12,
		Entry: inplay.Entry{
			State:      inplay.StateInPlay,
			ScoreSlope: 0.12,
			Momentum:   true,
		},
	}
	state, score, reasons := deriveTriggerState(c)
	if state != string(triggerOFReclaim) {
		t.Fatalf("expected reclaim state, got %s", state)
	}
	if score < 0.8 {
		t.Fatalf("expected strong trigger score, got %.2f", score)
	}
	if len(reasons) == 0 {
		t.Fatalf("expected trigger reasons")
	}
}

func TestPaperMarkLastPricesUsesMid(t *testing.T) {
	m := symbolMeta{LastPrice: 100}
	ob := aster.OrderBook{
		Bids: [][2]float64{{99, 10}},
		Asks: [][2]float64{{101, 10}},
	}
	mark, last := paperMarkLastPrices(m, ob, "orderbook_mid", 0)
	if mark != 100 {
		t.Fatalf("expected mid mark 100, got %.2f", mark)
	}
	if last != 100 {
		t.Fatalf("expected last 100, got %.2f", last)
	}
}

func TestMissedTrackerEmitsAfterFifteenMinutes(t *testing.T) {
	trk := newMissedTracker()
	c := candidate{
		Entry:          inplay.Entry{Symbol: "BTCUSDT", Rank: 90},
		Side:           "BUY",
		LastClose:      100,
		DiscoveryScore: 0.8,
		TriggerScore:   0.7,
		ExecutionScore: 0.7,
		CombinedScore:  0.75,
		TriggerState:   string(triggerImpulseCont),
	}
	now := time.Now().UTC()
	trk.Observe(now, c, "architecture_miss:not_selected")
	meta := map[string]symbolMeta{"BTCUSDT": {LastPrice: 103}}
	trk.Update(now.Add(16*time.Minute), meta, nil, nil, nil)
	if len(trk.items) != 0 {
		t.Fatalf("expected tracker item to flush after 15m")
	}
}

func TestFundingHazardWindow(t *testing.T) {
	now := time.Date(2026, 3, 14, 10, 59, 50, 0, time.UTC)
	if !fundingHazardWindow(now, time.Hour, 15*time.Second, 45*time.Second) {
		t.Fatalf("expected funding hazard near boundary")
	}
	if fundingHazardWindow(now.Add(-10*time.Minute), time.Hour, 15*time.Second, 45*time.Second) {
		t.Fatalf("did not expect funding hazard far from boundary")
	}
}

func TestCategorizeMissReason(t *testing.T) {
	cases := map[string]string{
		"architecture_miss:not_selected": "architecture_miss",
		"spread_too_wide":                "execution_miss",
		"funding_hazard_entry_block":     "risk_miss",
		"meta_quality:0.54<0.58":         "filter_miss",
	}
	for in, want := range cases {
		if got := categorizeMissReason(in); got != want {
			t.Fatalf("categorizeMissReason(%q)=%q want %q", in, got, want)
		}
	}
}

func TestComputeDynamicTargetLadderUsesStructure(t *testing.T) {
	c := candidate{
		Side:        "BUY",
		ATR:         2,
		ATRPct:      0.02,
		SessionVWAP: 99,
		Sig:         strategies.Signal{VPTargetLevel: 105},
	}
	_, tp1, tp2, tp3 := computeDynamicTargetLadder(c, 100, 2, 1.0, 2.0, 3.0)
	if tp1 <= 100 || tp2 <= tp1 || tp3 <= tp2 {
		t.Fatalf("expected progressive long targets, got %.2f %.2f %.2f", tp1, tp2, tp3)
	}
	if tp1 > 105 {
		t.Fatalf("expected first target to respect structure, got %.2f", tp1)
	}
}

func TestApplyTriggerLifecycleRequiresReversalPersistence(t *testing.T) {
	now := time.Now().UTC()
	mem := map[string]triggerMemory{}
	cfg := normalizeTriggerLifecycleConfig(triggerLifecycleConfig{
		Enable:             true,
		ArmScans:           1,
		ReadyScans:         2,
		ReversalReadyScans: 3,
		InvalidateScans:    2,
		ExpireAfter:        10 * time.Minute,
		ArmedBoost:         0.05,
		ReadyBoost:         0.10,
		InvalidPenalty:     0.15,
	})
	c := candidate{
		Entry: inplay.Entry{Symbol: "BTCUSDT", State: inplay.StateCooling},
		Side:  "SELL", Strat: "mom_reversal_short",
		TriggerState: string(triggerExhaustion), TriggerStateN: 0.40,
	}
	c = applyTriggerLifecycle(c, now, mem, cfg)
	if c.TriggerStage != "ARMED" {
		t.Fatalf("expected first reversal scan to arm, got %s", c.TriggerStage)
	}
	c = applyTriggerLifecycle(c, now.Add(time.Minute), mem, cfg)
	if c.TriggerStage != "ARMED" {
		t.Fatalf("expected second reversal scan to stay armed, got %s", c.TriggerStage)
	}
	c = applyTriggerLifecycle(c, now.Add(2*time.Minute), mem, cfg)
	if c.TriggerStage != "READY" {
		t.Fatalf("expected reversal trigger to become ready after persistence, got %s", c.TriggerStage)
	}
}

func TestClassifySponsorship(t *testing.T) {
	mom := map[string]momentumView{
		"BTCUSDT": {
			Long: &inplay.Entry{CurrentScore: 92, ScoreSlope: 0.22, State: inplay.StateInPlay, Momentum: true},
		},
	}
	flow := map[string]flowMetrics{
		"BTCUSDT": {OFIZ: 0.8},
	}
	snap := classifySponsorship("BUY", "BTCUSDT", mom, flow)
	if !snap.Sponsored {
		t.Fatalf("expected strong long sponsorship")
	}
}

func TestParseOrderProgressPartial(t *testing.T) {
	prog := parseOrderProgress(map[string]any{
		"status":      "PARTIALLY_FILLED",
		"executedQty": 1.25,
		"avgPrice":    102.5,
		"origQty":     2.0,
	})
	if !prog.Working || prog.Filled || prog.ExecQty != 1.25 || prog.AvgPx != 102.5 {
		t.Fatalf("unexpected partial progress: %+v", prog)
	}
}
