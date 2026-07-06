package main

import (
	"testing"

	"go-machine/adapters/aster"
)

func TestWidenStopPctForVolatility(t *testing.T) {
	t.Setenv("LIVE_VOLATILE_ATR_PCT_MODERATE", "0.03")
	t.Setenv("LIVE_VOLATILE_ATR_PCT_HIGH", "0.05")
	t.Setenv("LIVE_VOLATILE_STOP_MULT_MODERATE", "1.25")
	t.Setenv("LIVE_VOLATILE_STOP_MULT_HIGH", "1.60")
	t.Setenv("LIVE_VOLATILE_MAX_VOLUME_USD", "75000000")

	got := widenStopPctForVolatility(0.02, 0.04, 20_000_000)
	if got <= 0.02 {
		t.Fatalf("expected volatility widening, got %.6f", got)
	}
}

func TestNormalizeProtectiveStopToTickHonorsSide(t *testing.T) {
	meta := aster.SymbolMeta{TickSize: 0.1, PricePrecision: 4}
	longStop := normalizeProtectiveStopToTick("BUY", 12.37, meta)
	shortStop := normalizeProtectiveStopToTick("SELL", 12.37, meta)
	if longStop != 12.3 {
		t.Fatalf("expected long stop floored to tick, got %.4f", longStop)
	}
	if shortStop != 12.4 {
		t.Fatalf("expected short stop ceiled to tick, got %.4f", shortStop)
	}
}

func TestValidateStopOrderLegalityHonorsProtectiveSide(t *testing.T) {
	meta := aster.SymbolMeta{
		TickSize:       0.1,
		PricePrecision: 4,
		StepSize:       0.1,
		QtyPrecision:   4,
		MinQty:         0.1,
	}
	qty, stop, reason := validateStopOrderLegality(meta, 1.0, 12.4, "SELL")
	if reason != "" {
		t.Fatalf("expected short protective stop to be legal, got %q", reason)
	}
	if qty != 1.0 || stop != 12.4 {
		t.Fatalf("expected unchanged short stop, got qty=%.4f stop=%.4f", qty, stop)
	}
	qty, stop, reason = validateStopOrderLegality(meta, 1.0, 12.3, "BUY")
	if reason != "" {
		t.Fatalf("expected long protective stop to be legal, got %q", reason)
	}
	if qty != 1.0 || stop != 12.3 {
		t.Fatalf("expected unchanged long stop, got qty=%.4f stop=%.4f", qty, stop)
	}
}

func TestLifecycleSoftExitsCanHardClose(t *testing.T) {
	if lifecycleSoftExitsCanHardClose("winner_locked") {
		t.Fatal("expected winner_locked soft exits to stay managed")
	}
	if lifecycleSoftExitsCanHardClose("runner") {
		t.Fatal("expected runner soft exits to stay managed")
	}
	if !lifecycleSoftExitsCanHardClose("proof_armed") {
		t.Fatal("expected proof_armed to remain eligible for early-phase exit handling")
	}
}
