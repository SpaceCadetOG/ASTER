package main

import (
	"strings"
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

func TestMissedTrackerPromotesPersistenceEntry(t *testing.T) {
	t.Setenv("LIVE_OPP_TRACK_ENABLE", "1")
	t.Setenv("LIVE_OPP_TRACK_WINDOW_SEC", "1800")
	t.Setenv("LIVE_OPP_MIN_SEEN_COUNT", "3")
	t.Setenv("LIVE_OPP_MIN_TOPN_COUNT", "2")
	t.Setenv("LIVE_SOFT_REJECT_MEMORY_ENABLE", "1")
	t.Setenv("LIVE_SOFT_REJECT_MEMORY_TTL_SEC", "3600")
	t.Setenv("LIVE_PERSISTENCE_ENTRY_ENABLE", "1")
	t.Setenv("LIVE_PERSISTENCE_MIN_RANK", "0.70")
	t.Setenv("LIVE_PERSISTENCE_ALLOW_STABLE_VOLUME", "1")
	t.Setenv("LIVE_PERSISTENCE_ALLOW_STABLE_MOMENTUM", "1")
	t.Setenv("LIVE_CONT_FAST_MIN_OFI_Z", "0.35")
	t.Setenv("LIVE_OFI_MIN_SAMPLES", "8")

	trk := newMissedTracker()
	base := candidate{
		Side:          "BUY",
		Strat:         "none",
		CombinedScore: 0.76,
		VolumeUSD:     1_000_000,
		VolumeRatio:   1.05,
		OFIZ:          0.48,
		OFISamples:    10,
		LastClose:     1.05,
		SessionVWAP:   1.04,
		EMA9:          1.04,
		RejectReason:  "continuation_no_structure_confirm",
		Entry: inplay.Entry{
			Symbol:       "LYNUSDT",
			CurrentGrade: "A",
			State:        inplay.StateInPlay,
			CurrentScore: 91,
			ScoreSlope:   0.08,
		},
	}
	now := time.Date(2026, 3, 25, 13, 0, 0, 0, time.UTC)
	trk.ObserveCandidate(now, base, true)
	base.VolumeUSD = 1_050_000
	base.VolumeRatio = 1.10
	base.Entry.ScoreSlope = 0.09
	trk.ObserveCandidate(now.Add(30*time.Second), base, true)
	base.VolumeUSD = 1_100_000
	base.VolumeRatio = 1.12
	base.Entry.ScoreSlope = 0.11
	base.RejectReason = ""
	trk.ObserveCandidate(now.Add(time.Minute), base, false)

	got := trk.PromoteCandidate(now.Add(90*time.Second), base, nil, nil)
	if got.Strat != "persistence_entry" {
		t.Fatalf("expected persistence_entry, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
	if got.Conf <= 0 {
		t.Fatalf("expected positive confidence, got %.3f", got.Conf)
	}
}

func TestMissedTrackerReviewLinesShowsPersistentReady(t *testing.T) {
	t.Setenv("LIVE_OPP_TRACK_ENABLE", "1")
	t.Setenv("LIVE_OPP_MIN_SEEN_COUNT", "2")
	t.Setenv("LIVE_OPP_MIN_TOPN_COUNT", "1")
	t.Setenv("LIVE_PERSISTENCE_ENTRY_ENABLE", "1")
	t.Setenv("LIVE_PERSISTENCE_MIN_RANK", "0.70")
	t.Setenv("LIVE_CONT_FAST_MIN_OFI_Z", "0.35")
	trk := newMissedTracker()
	now := time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC)
	c := candidate{
		Side:          "SELL",
		Strat:         "none",
		CombinedScore: 0.79,
		VolumeUSD:     900_000,
		VolumeRatio:   1.15,
		OFIZ:          -0.60,
		OFISamples:    12,
		LastClose:     0.95,
		SessionVWAP:   0.96,
		EMA9:          0.955,
		Entry: inplay.Entry{
			Symbol:       "SIRENUSDT",
			CurrentGrade: "A",
			State:        inplay.StateInPlay,
			CurrentScore: 94,
			ScoreSlope:   0.10,
		},
	}
	trk.ObserveCandidate(now, c, true)
	trk.ObserveCandidate(now.Add(20*time.Second), c, true)
	_ = trk.PromoteCandidate(now.Add(25*time.Second), c, nil, nil)
	rows := trk.ReviewLines(now.Add(30*time.Second), 2)
	if len(rows) == 0 || !strings.Contains(rows[0], "SIRENUSDT") {
		t.Fatalf("expected review line for ready persistent opportunity, got %v", rows)
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

func TestComputeEntryScoreBreakdownUsesWallConfidence(t *testing.T) {
	cfg := entryQualityConfig{}
	base := candidate{
		Side:         "BUY",
		Conf:         0.55,
		SpreadBps:    5,
		VolumeRatio:  1.4,
		TriggerState: string(triggerDeltaFlip),
		Entry: inplay.Entry{
			CurrentScore: 90,
			CurrentGrade: "A",
			Rank:         95,
			ScoreSlope:   0.12,
		},
	}
	_, baseTrigger, baseExec, _, _ := computeEntryScoreBreakdown(base, cfg)
	withWall := base
	withWall.WallMode = "wall_defense"
	withWall.WallConfidence = 0.72
	withWall.WallBiasScore = 0.35
	withWall.WallSpoofRisk = 0.05
	_, wallTrigger, wallExec, _, reasons := computeEntryScoreBreakdown(withWall, cfg)
	if wallTrigger <= baseTrigger {
		t.Fatalf("expected wall confidence to boost trigger score, base=%.2f wall=%.2f", baseTrigger, wallTrigger)
	}
	if wallExec <= baseExec {
		t.Fatalf("expected wall confidence to boost execution score, base=%.2f wall=%.2f", baseExec, wallExec)
	}
	if len(reasons) == 0 {
		t.Fatalf("expected reasons from wall-enhanced score breakdown")
	}
}

func TestDeepQueuePreflightRejectsWallSpoofRisk(t *testing.T) {
	c := candidate{
		Entry:         inplay.Entry{Symbol: "BTCUSDT"},
		WallSpoofRisk: 0.90,
	}
	res := deepQueuePreflight(c, queueDeepPreflightCtx{
		MetaBySymbol: map[string]symbolMeta{"BTCUSDT": {LastPrice: 100}},
	})
	if res.RejectReason != "wall_spoof_risk" {
		t.Fatalf("expected wall_spoof_risk reject, got %s", res.RejectReason)
	}
}
