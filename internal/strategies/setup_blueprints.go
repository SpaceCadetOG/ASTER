package strategies

import "strings"

type SetupBlueprint struct {
	SetupFamily    string
	SetupSource    string
	TradeHorizon   string
	StrategyFamily string
}

var setupBlueprints = map[string]SetupBlueprint{
	"lsr":                         {SetupFamily: "breakout_retest", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"bos_pb":                      {SetupFamily: "breakout_retest", SetupSource: "market_structure", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"ob_r":                        {SetupFamily: "deep_pullback_reclaim", SetupSource: "market_structure", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"fvg_c":                       {SetupFamily: "micro_pullback_continuation", SetupSource: "market_structure", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"fa":                          {SetupFamily: "reversal_exhaustion", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "rev"},
	"failed_auction_magnet":       {SetupFamily: "reversal_exhaustion", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "rev"},
	"od":                          {SetupFamily: "reset_impulse_breakout", SetupSource: "order_flow", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"volume_clusters":             {SetupFamily: "deep_pullback_reclaim", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"multiple_nodes":              {SetupFamily: "breakout_retest", SetupSource: "order_flow", TradeHorizon: "swing", StrategyFamily: "cont"},
	"trades_filter":               {SetupFamily: "micro_pullback_continuation", SetupSource: "order_flow", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"stacked_imbalances":          {SetupFamily: "reset_impulse_breakout", SetupSource: "order_flow", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"unfinished_business":         {SetupFamily: "breakout_retest", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"vwap_confluence":             {SetupFamily: "micro_pullback_continuation", SetupSource: "vwap", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"daily_open_sr":               {SetupFamily: "breakout_retest", SetupSource: "vwap", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"pd_levels_retest":            {SetupFamily: "deep_pullback_reclaim", SetupSource: "vwap", TradeHorizon: "swing", StrategyFamily: "cont"},
	"vp_accumulation":             {SetupFamily: "deep_pullback_reclaim", SetupSource: "volume_profile", TradeHorizon: "swing", StrategyFamily: "cont"},
	"vp_trend":                    {SetupFamily: "micro_pullback_continuation", SetupSource: "volume_profile", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"vp_rejection":                {SetupFamily: "breakout_retest", SetupSource: "volume_profile", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"vp_reversal":                 {SetupFamily: "reversal_exhaustion", SetupSource: "volume_profile", TradeHorizon: "intraday_swing", StrategyFamily: "rev"},
	"momentum_ignite_long":        {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"momentum_ignite_short":       {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"reset_impulse_long":          {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"reset_impulse_short":         {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"impulse_long":                {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"impulse_short":               {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"continuation_fast":           {SetupFamily: "micro_pullback_continuation", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"micro_pullback_continuation": {SetupFamily: "micro_pullback_continuation", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"breakout_retest":             {SetupFamily: "breakout_retest", SetupSource: "technical_analysis", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"deep_pullback_reclaim":       {SetupFamily: "deep_pullback_reclaim", SetupSource: "technical_analysis", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"reset_impulse_breakout":      {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"reversal_exhaustion":         {SetupFamily: "reversal_exhaustion", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "rev"},
}

func SetupBlueprintForLabel(label string) (SetupBlueprint, bool) {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return SetupBlueprint{}, false
	}
	bp, ok := setupBlueprints[label]
	return bp, ok
}

func ResolveSetupBlueprint(strategy, setupFamily, entryStyle string) (SetupBlueprint, bool) {
	for _, label := range []string{strategy, setupFamily, entryStyle} {
		if bp, ok := SetupBlueprintForLabel(label); ok {
			return bp, true
		}
	}
	switch strings.ToLower(strings.TrimSpace(entryStyle)) {
	case "breakout_hold_long", "breakout_hold_short":
		return setupBlueprints["breakout_retest"], true
	case "pullback_long", "pullback_short":
		return setupBlueprints["micro_pullback_continuation"], true
	case "reversal_watch_long", "reversal_watch_short", "leader_unwind_short":
		return setupBlueprints["reversal_exhaustion"], true
	}
	return SetupBlueprint{}, false
}
