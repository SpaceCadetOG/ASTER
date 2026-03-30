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

func TestManualProtectionFailureBudgetDegradesAfterRetries(t *testing.T) {
	p := &livePosition{
		Symbol:           "ONUSDT",
		Side:             "LONG",
		EntrySource:      manualEntrySourceManaged,
		EntryReason:      manualEntryReasonManaged,
		ManualManageState: manualManageStatePendingProtection,
	}
	now := time.Now().UTC()
	for i := 0; i < manualProtectionRetryBudget()-1; i++ {
		if degraded := recordManualProtectionFailure(p, now, "exchange_immediate_trigger_retry_failed"); degraded {
			t.Fatalf("degraded too early on attempt %d", i+1)
		}
	}
	if strings.TrimSpace(p.ManualManageState) != manualManageStatePendingProtection {
		t.Fatalf("expected pending protection before budget exhausted, got %s", p.ManualManageState)
	}
	if degraded := recordManualProtectionFailure(p, now, "exchange_immediate_trigger_retry_failed"); !degraded {
		t.Fatal("expected degraded after exhausting retry budget")
	}
	if strings.TrimSpace(p.ManualManageState) != manualManageStateDegraded {
		t.Fatalf("expected degraded state, got %s", p.ManualManageState)
	}
}

func TestManualAssistResponseIncludesStarterAndBlockers(t *testing.T) {
	msg := manualAssistResponse("SIRENUSDT", "LONG", operatorDecision{
		Symbol:             "SIRENUSDT",
		Side:               "LONG",
		Strategy:           "heating_starter_entry",
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
