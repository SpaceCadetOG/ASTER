package main

import (
	"path/filepath"
	"testing"
	"time"
)

func testRecoveryManager(dir string) *liveExecManager {
	return &liveExecManager{
		path:             filepath.Join(dir, "live_exec_state.json"),
		positions:        map[string]*livePosition{},
		recentBotEntries: map[string]recentBotEntryMemory{},
		stopPct:          2.0,
		minStopPct:       0.5,
		maxStopPct:       5.0,
		minTP1RR:         0.5,
		tp1R:             1.0,
		tp2R:             2.0,
		tp3R:             3.0,
		tp1Frac:          0.40,
		tp2Frac:          0.30,
		tp3Frac:          0.20,
	}
}

func TestRecentBotEntryRecoveryPreservesBotSource(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	mgr := testRecoveryManager(dir)
	botPos := &livePosition{
		Symbol:                 "LITUSDT",
		Side:                   "BUY",
		EntryPrice:             2.6823,
		FilledQty:              9,
		Qty:                    9,
		Margin:                 4.84,
		DeployedMargin:         4.84,
		Leverage:               5,
		EntryReason:            "impulse_breakout",
		EntryStrategyID:        "impulse_breakout",
		EntrySource:            "BOT",
		EntryGrade:             "A",
		EntryState:             "IN_PLAY",
		ExitProfile:            "trend",
		EntryConf:              0.81,
		DiscoveryScore:         0.78,
		TriggerScore:           0.82,
		ExecutionScore:         0.79,
		CombinedScore:          0.80,
		EntryVolumeUSD:         1500000,
		EntryATRPct:            1.2,
		EntryATRExtension:      0.4,
		EntrySession:           "ASIA_OPEN",
		EntryTiming:            "mid",
		CandidateAgeSeconds:    18,
		EntryDistanceToVWAPPct: 0.23,
		EntrySetupFamily:       "reset_impulse_breakout",
		EntrySetupSource:       "router",
		EntryTradeHorizon:      "intraday",
		ExecBucket:             "ignite",
	}
	mgr.rememberRecentBotEntry(now, botPos)
	if err := mgr.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded := testRecoveryManager(dir)
	if err := loaded.load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	recovered, ok := loaded.recoverBotRemotePosition("LITUSDT", "BUY", 9, 2.6823, 4.84, 5, now.Add(2*time.Minute))
	if !ok {
		t.Fatalf("expected bot recovery match")
	}
	if recovered.EntrySource != "BOT" {
		t.Fatalf("expected BOT source, got %q", recovered.EntrySource)
	}
	if recovered.EntryReason != "impulse_breakout" {
		t.Fatalf("expected original entry reason, got %q", recovered.EntryReason)
	}
	if recovered.EntrySetupFamily != "reset_impulse_breakout" {
		t.Fatalf("expected original setup family, got %q", recovered.EntrySetupFamily)
	}
	if recovered.ManualManageState != "" {
		t.Fatalf("expected no manual manage state, got %q", recovered.ManualManageState)
	}
	if recovered.StopPrice <= 0 || recovered.TP1Price <= 0 {
		t.Fatalf("expected reconstructed bracket levels, got stop=%.4f tp1=%.4f", recovered.StopPrice, recovered.TP1Price)
	}
}

func TestRecentBotEntryRecoveryExpires(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	mgr := testRecoveryManager(dir)
	mgr.recentBotEntries[positionLookupKey("LITUSDT", "LONG")] = recentBotEntryMemory{
		Symbol:     "LITUSDT",
		Side:       "BUY",
		OccurredAt: now.Add(-recentBotEntryTTL()).Add(-time.Minute),
		EntryPrice: 2.6823,
		Qty:        9,
	}

	if _, ok := mgr.recoverBotRemotePosition("LITUSDT", "BUY", 9, 2.6823, 4.84, 5, now); ok {
		t.Fatalf("expected stale bot recovery memory to expire")
	}
}

func TestLoadRepairsPassiveManualRequestFromPersistedPosition(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	mgr := testRecoveryManager(dir)
	mgr.manualRequests = map[string]manualManageRequest{}
	mgr.positions["SNDKUSDT"] = &livePosition{
		Symbol:            "SNDKUSDT",
		Side:              "SHORT",
		State:             execOpen,
		CreatedAt:         now.Add(-10 * time.Minute),
		UpdatedAt:         now,
		EntryPrice:        1741.82,
		Qty:               0.01,
		FilledQty:         0.01,
		RemainingQty:      0.01,
		Margin:            3.48621163,
		DeployedMargin:    3.48621163,
		Leverage:          5,
		EntryReason:       manualEntryReasonPassive,
		EntrySource:       manualEntrySourcePassive,
		ManualManageState: manualManageStatePassive,
	}
	if err := mgr.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded := testRecoveryManager(dir)
	if err := loaded.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	req, ok := loaded.manualRequests[positionLookupKey("SNDKUSDT", "SHORT")]
	if !ok {
		t.Fatalf("expected passive manual request to be rebuilt")
	}
	if req.Status != manualRequestPending {
		t.Fatalf("expected rebuilt request pending, got %q", req.Status)
	}
	if req.Symbol != "SNDKUSDT" || req.Side != "SHORT" {
		t.Fatalf("unexpected rebuilt request: %+v", req)
	}
}
