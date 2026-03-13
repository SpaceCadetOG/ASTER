package market

import "testing"

func TestDirectionalDayAlignment(t *testing.T) {
	if got := directionalDayAlignment("long", 10); got <= 0 {
		t.Fatalf("expected positive long day alignment, got %.2f", got)
	}
	if got := directionalDayAlignment("short", -10); got <= 0 {
		t.Fatalf("expected positive short day alignment, got %.2f", got)
	}
	if got := directionalDayAlignment("long", -5); got >= 0 {
		t.Fatalf("expected negative long misalignment, got %.2f", got)
	}
}

func TestDirectionalFundingAdjustment(t *testing.T) {
	if got := directionalFundingAdjustment("long", 0.05); got >= 0 {
		t.Fatalf("expected positive funding to hurt longs, got %.2f", got)
	}
	if got := directionalFundingAdjustment("long", -0.05); got <= 0 {
		t.Fatalf("expected negative funding to help longs, got %.2f", got)
	}
	if got := directionalFundingAdjustment("short", -0.05); got >= 0 {
		t.Fatalf("expected negative funding to hurt shorts, got %.2f", got)
	}
}

func TestFragilityPenaltyHitsThinExtremeMover(t *testing.T) {
	day := 2.0
	oi := 40_000.0
	m := Market{
		Symbol:    "LOBSTER-USD",
		Change24h: 120,
		DayUTC24h: &day,
		VolumeUSD: 1_270_000,
		OIUSD:     &oi,
	}
	if got := fragilityPenalty("long", m); got <= 0 {
		t.Fatalf("expected thin extreme mover penalty, got %.2f", got)
	}
}

func TestBaseScorePrefersAlignedMove(t *testing.T) {
	dayGood := 12.0
	dayBad := -6.0
	oi := 400_000.0
	funding := -0.01
	base := Market{
		Symbol:      "OGN-USD",
		Change24h:   35,
		VolumeUSD:   5_000_000,
		OIUSD:       &oi,
		FundingRate: &funding,
	}
	good := base
	good.DayUTC24h = &dayGood
	bad := base
	bad.DayUTC24h = &dayBad
	if baseLongRawScore(good) <= baseLongRawScore(bad) {
		t.Fatalf("expected aligned DayUTC to score better")
	}
}
