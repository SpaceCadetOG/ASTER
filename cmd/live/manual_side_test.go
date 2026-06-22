package main

import (
	"strings"
	"testing"
	"time"

	"go-machine/internal/notify"
)

func TestRealizedFromFillSupportsLongShortAliases(t *testing.T) {
	longPnL, longPct := realizedFromFill("LONG", 0.0592, 0.0630, 1746)
	if longPnL <= 0 || longPct <= 0 {
		t.Fatalf("expected profitable LONG pnl, got pnl=%f pct=%f", longPnL, longPct)
	}

	shortPnL, shortPct := realizedFromFill("SHORT", 0.0630, 0.0592, 1746)
	if shortPnL <= 0 || shortPct <= 0 {
		t.Fatalf("expected profitable SHORT pnl, got pnl=%f pct=%f", shortPnL, shortPct)
	}
}

func TestSamePositionSideMatchesBuyAndLong(t *testing.T) {
	if !samePositionSide("BUY", "LONG") {
		t.Fatal("expected BUY and LONG to match")
	}
	if !samePositionSide("SELL", "SHORT") {
		t.Fatal("expected SELL and SHORT to match")
	}
}

func TestBuildPositionCardTreatsShortAsShort(t *testing.T) {
	card := notify.BuildPositionCard(notify.PositionCard{
		Symbol:           "TESTUSDT",
		Side:             "SHORT",
		EntryPrice:       1.0,
		MarkPrice:        0.9,
		UnrealizedPnLPct: 10,
		UnrealizedPnL:    1,
		Leverage:         5,
	})
	if !strings.Contains(card, "SHORT") || strings.Contains(card, "SELL") {
		t.Fatalf("expected SHORT card to render as SHORT without SELL, got %q", card)
	}
}

func TestManualProtectionFailureBudgetEscalatesToCriticalAfterRetries(t *testing.T) {
	p := &livePosition{
		Symbol:            "ONUSDT",
		Side:              "LONG",
		EntrySource:       manualEntrySourceManaged,
		EntryReason:       manualEntryReasonManaged,
		ManualManageState: manualManageStatePendingProtection,
	}
	now := time.Now().UTC()
	for i := 0; i < manualProtectionRetryBudget()-1; i++ {
		if escalated := recordManualProtectionFailure(p, now, "exchange_immediate_trigger_retry_failed"); escalated {
			t.Fatalf("escalated too early on attempt %d", i+1)
		}
	}
	if strings.TrimSpace(p.ManualManageState) != manualManageStatePendingProtection {
		t.Fatalf("expected pending protection before budget exhausted, got %s", p.ManualManageState)
	}
	if escalated := recordManualProtectionFailure(p, now, "exchange_immediate_trigger_retry_failed"); !escalated {
		t.Fatal("expected critical escalation after exhausting retry budget")
	}
	if strings.TrimSpace(p.ManualManageState) != manualManageStateCritical {
		t.Fatalf("expected critical state, got %s", p.ManualManageState)
	}
}

func TestManualAssistResponseIncludesStarterAndBlockers(t *testing.T) {
	msg := manualAssistResponse("SIRENUSDT", "LONG", operatorDecision{
		Symbol:             "SIRENUSDT",
		Side:               "LONG",
		Strategy:           "impulsive_long_starter",
		RejectReason:       "vol_ratio:0.90<1.20,continuation_no_structure_confirm",
		BlockerClass:       string(rejectClassSoftConfirm),
		TopBlockers:        []string{"vol_ratio:0.90<1.20", "continuation_no_structure_confirm"},
		StarterAllowed:     true,
		PersistenceStatus:  "tracking",
		State:              "HEATING",
		AdjustedConfidence: 0.58,
		Score:              91,
	}, symbolMeta{LastPrice: 0.1234, Move24h: 18, VolumeUSD: 2_000_000})
	if !strings.Contains(msg, "Starter now:</b> YES") || !strings.Contains(msg, "Soft blockers:") {
		t.Fatalf("expected starter guidance and soft blockers, got %q", msg)
	}
}

func TestSanitizeSnapshotPriceRejectsOutlier(t *testing.T) {
	got, guarded := sanitizeSnapshotPrice(100, 160)
	if !guarded || got != 100 {
		t.Fatalf("expected outlier to be guarded back to ref, got %.2f guarded=%v", got, guarded)
	}
}

func TestActivateManualManagementAdoptsRecoveredPositionForMonitoring(t *testing.T) {
	now := time.Now().UTC()
	mgr := &liveExecManager{
		positions:    map[string]*livePosition{},
		stopPct:      2.0,
		minStopPct:   0.1,
		maxStopPct:   10.0,
		tp1R:         1.2,
		tp2R:         2.0,
		tp3R:         3.0,
		tp1Frac:      0.50,
		tp2Frac:      0.30,
		tp3Frac:      0.20,
		reportLoc:    time.UTC,
		ladderCfg:    ladderConfig{StarterUSDT: 100},
		entryTimeout: time.Minute,
	}
	req := manualManageRequest{
		Key:         positionLookupKey("BTCUSDT", "BUY"),
		Fingerprint: manualManageFingerprint("BTCUSDT", "BUY", 1.25, 100),
		Symbol:      "BTCUSDT",
		Side:        "BUY",
		Qty:         1.25,
		Entry:       100,
		Margin:      100,
		Leverage:    5,
	}

	p, err := mgr.activateManualManagement(req, now, "REMOTE_POSITION_MONITORED")
	if err != nil {
		t.Fatalf("expected recovered position to be adopted for monitoring, got %v", err)
	}
	if p == nil {
		t.Fatal("expected managed recovered position")
	}
	if !p.Managed {
		t.Fatal("expected recovered position to be managed")
	}
	if !p.ProtectionPending {
		t.Fatal("expected recovered position protection to be pending")
	}
	if strings.TrimSpace(p.EntrySource) != manualEntrySourceManaged {
		t.Fatalf("expected managed entry source, got %q", p.EntrySource)
	}
	if strings.TrimSpace(p.EntryReason) != manualEntryReasonManaged {
		t.Fatalf("expected managed entry reason, got %q", p.EntryReason)
	}
	if got := mgr.positions["BTCUSDT"]; got == nil || !got.Managed {
		t.Fatal("expected recovered position stored in manager as managed")
	}
}
