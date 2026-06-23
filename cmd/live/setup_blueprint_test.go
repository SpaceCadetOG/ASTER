package main

import (
	"testing"
	"time"
)

func TestResolveSetupBlueprintForStrategy(t *testing.T) {
	c := candidate{Strat: "vp_accumulation"}
	bp, ok := resolveSetupBlueprint(c, testNow())
	if !ok {
		t.Fatalf("expected blueprint for vp_accumulation")
	}
	if bp.SetupFamily != "deep_pullback_reclaim" {
		t.Fatalf("expected deep_pullback_reclaim, got %q", bp.SetupFamily)
	}
	if bp.SetupSource != "volume_profile" {
		t.Fatalf("expected volume_profile source, got %q", bp.SetupSource)
	}
	if bp.TradeHorizon != "swing" {
		t.Fatalf("expected swing horizon, got %q", bp.TradeHorizon)
	}
}

func TestResolveSetupBlueprintFallsBackFromEntryStyle(t *testing.T) {
	c := candidate{}
	c.Entry.EntryStyle = "breakout_hold_long"
	bp, ok := resolveSetupBlueprint(c, testNow())
	if !ok {
		t.Fatalf("expected blueprint from breakout_hold_long")
	}
	if bp.SetupFamily != "breakout_retest" {
		t.Fatalf("expected breakout_retest, got %q", bp.SetupFamily)
	}
	if bp.SetupSource != "technical_analysis" {
		t.Fatalf("expected technical_analysis source, got %q", bp.SetupSource)
	}
}

func TestEnsureCandidateSetupFamilyAnnotatesBlueprintFields(t *testing.T) {
	c := candidate{Strat: "vwap_confluence"}
	ensureCandidateSetupFamily(&c, testNow())
	if c.SetupFamily != "micro_pullback_continuation" {
		t.Fatalf("expected setup family from blueprint, got %q", c.SetupFamily)
	}
	if c.SetupSource != "vwap" {
		t.Fatalf("expected vwap source, got %q", c.SetupSource)
	}
	if c.TradeHorizon != "intraday" {
		t.Fatalf("expected intraday horizon, got %q", c.TradeHorizon)
	}
}

func TestChooseExitProfileUsesSwingBlueprint(t *testing.T) {
	c := candidate{Strat: "pd_levels_retest"}
	ensureCandidateSetupFamily(&c, testNow())
	if got := chooseExitProfile(c); got != "SWING" {
		t.Fatalf("expected SWING profile, got %q", got)
	}
}

func TestChooseExitProfileUsesSwingBlueprintForMultipleNodes(t *testing.T) {
	c := candidate{Strat: "multiple_nodes"}
	ensureCandidateSetupFamily(&c, testNow())
	if got := chooseExitProfile(c); got != "SWING" {
		t.Fatalf("expected SWING profile for multiple_nodes, got %q", got)
	}
}

func TestStrategyFamilyUsesBlueprint(t *testing.T) {
	c := candidate{Strat: "od"}
	if got := strategyFamily(c); got != "ignite" {
		t.Fatalf("expected ignite family from blueprint, got %q", got)
	}
}

func testNow() time.Time {
	return mustParseTime("2026-06-22T00:00:00Z")
}

func mustParseTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		panic(err)
	}
	return t
}
