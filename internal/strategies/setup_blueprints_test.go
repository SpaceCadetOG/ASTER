package strategies

import "testing"

func TestResolveSetupBlueprintPromotedStrategy(t *testing.T) {
	bp, ok := ResolveSetupBlueprint("vwap_confluence", "", "")
	if !ok {
		t.Fatalf("expected blueprint for promoted strategy")
	}
	if bp.SetupFamily != "micro_pullback_continuation" {
		t.Fatalf("expected micro_pullback_continuation, got %q", bp.SetupFamily)
	}
	if bp.SetupSource != "vwap" {
		t.Fatalf("expected vwap source, got %q", bp.SetupSource)
	}
	if bp.TradeHorizon != "intraday" {
		t.Fatalf("expected intraday horizon, got %q", bp.TradeHorizon)
	}
}

func TestResolveSetupBlueprintFallsBackFromEntryStyle(t *testing.T) {
	bp, ok := ResolveSetupBlueprint("", "", "breakout_hold_long")
	if !ok {
		t.Fatalf("expected blueprint from entry style")
	}
	if bp.SetupFamily != "breakout_retest" {
		t.Fatalf("expected breakout_retest, got %q", bp.SetupFamily)
	}
}
