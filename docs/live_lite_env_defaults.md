# Live-Lite Environment Knobs

Generated from `/Users/victorogbebor/2026/go-machine/cmd/live-lite/main.go` on 2026-03-14.

Total unique env knobs referenced by `live-lite`: **407**

Notes:
- Defaults shown exactly as the code uses them, with a few expression-based defaults normalized for readability.
- `LIVE_PAPER_*` knobs only affect the paper simulator that runs inside `live-lite` when `LIVE_DRY_RUN=true` and `LIVE_PAPER_ENABLE=true`.
- Shared exit knobs are read by both the live executor and the paper engine.

## Core Runtime And Sizing

- `LIVE_ALLOW_SHORTS`: default `true`
- `LIVE_DRY_RUN`: default `true`
- `LIVE_ENABLE_LIVE_TRADING`: default `false`
- For cautious real-money validation, prefer starting with `scripts/run_live_lite_safe_logged.sh` instead of the generic launcher.
- `LIVE_ENTRY_OFFSET_BPS`: default `2`
- `LIVE_GRADE_TOP_N`: default `6`
- `LIVE_KILL_CLOSE_POSITIONS`: default `false`
- `LIVE_LEVERAGE_FIXED`: default `3`
- `LIVE_LEVERAGE_MIN`: default `1`
- `LIVE_MAX_LEVERAGE`: default `3`
- `LIVE_MAX_OPEN_PER_SIDE`: default `0`
- `LIVE_MAX_OPEN_POS`: default `5`
- `LIVE_MIN_AVAILABLE_USDT`: default `LIVE_RESERVE_USDT + LIVE_TRADE_MARGIN_USDT`
- `LIVE_RESERVE_PCT`: default `50.0`
- `LIVE_RESERVE_USDT`: default `5`
- `LIVE_SCAN_SEC`: default `30`
- `LIVE_SHOW_ACCOUNT`: default `true`
- `LIVE_STRATEGY_TOP_N`: default `3`
- `LIVE_TRADE_MARGIN_MAX_USDT`: default `200.0`
- `LIVE_TRADE_MARGIN_MIN_USDT`: default `5.0`
- `LIVE_TRADE_MARGIN_PCT`: default `10.0`
- `LIVE_TRADE_MARGIN_USDT`: default `100`
- `LIVE_TRADE_SLOTS`: default `5`

## In-Play Tracker And Ranking

- `INPLAY_DROP_SCANS`: default `2`
- `INPLAY_FALL_SCANS`: default `2`
- `INPLAY_HISTORY_N`: default `5`
- `INPLAY_MIN_VOL_USD`: default `1_000_000`
- `INPLAY_RISE_N`: default `3`
- `INPLAY_TTL_MIN`: default `30`
- `LIVE_B_NEAR_A_ONLY`: default `true`
- `LIVE_B_NEAR_A_SCORE_MIN`: default `92.0`
- `LIVE_RANK_MIN_COMPLETENESS`: default `0.0`
- `LIVE_RANK_MIN_CONFIDENCE`: default `0.0`
- `LIVE_RANK_MIN_SCORE`: default `75.0`
- `LIVE_RANK_SORT_COMPLETENESS_WEIGHT`: default `0.6`
- `LIVE_RANK_SORT_CONF_WEIGHT`: default `1.0`
- `LIVE_RANK_SORT_RELIABILITY_WEIGHT`: default `1.0`
- `LIVE_RANK_SORT_USE_COMPLETENESS`: default `true`
- `LIVE_RANK_SORT_USE_CONFIDENCE`: default `true`
- `LIVE_RANK_SORT_USE_RELIABILITY`: default `false`
- `LIVE_RANK_USE_CONTINUOUS`: default `false`
- `RANK_ENABLE_RELIABILITY`: default `false`
- `RANK_ENABLE_STALENESS_PENALTY`: default `true`
- `RANK_ENABLE_STATE_DECAY`: default `true`
- `RANK_RELIABILITY_MAX_BONUS`: default `4`
- `RANK_RELIABILITY_MAX_PENALTY`: default `8`
- `RANK_STALE_IMPULSE_MIN`: default `20`
- `RANK_STATE_DECAY_MIN`: default `25`

## Candidate Quality And Memory

- `LIVE_CANDIDATE_ARM_SCANS`: default `2`
- `LIVE_CANDIDATE_EXPIRE_MIN`: default `20`
- `LIVE_CANDIDATE_MEMORY_ENABLE`: default `true`
- `LIVE_CANDIDATE_READY_MIN_SCORE`: default `65`
- `LIVE_CANDIDATE_READY_MIN_SLOPE`: default `0.01`
- `LIVE_CANDIDATE_READY_SCANS`: default `3`
- `LIVE_META_GATE_ENABLE`: default `true`
- `LIVE_META_MIN_QUALITY`: default `0.58`
- `LIVE_META_MIN_QUALITY_CONT`: default `envFloat("LIVE_META_MIN_QUALITY", 0.58`
- `LIVE_META_MIN_QUALITY_IGNITE`: default `min(envFloat("LIVE_META_MIN_QUALITY", 0.58`
- `LIVE_META_MIN_QUALITY_REV`: default `min(envFloat("LIVE_META_MIN_QUALITY", 0.58`
- `LIVE_MIN_ENTRY_CONF`: default `0.55`
- `LIVE_MIN_ENTRY_CONF_CONT`: default `envFloat("LIVE_MIN_ENTRY_CONF", 0.55`
- `LIVE_MIN_ENTRY_CONF_IGNITE`: default `min(envFloat("LIVE_MIN_ENTRY_CONF", 0.55`
- `LIVE_MIN_ENTRY_CONF_REV`: default `min(envFloat("LIVE_MIN_ENTRY_CONF", 0.55`
- `LIVE_PERSISTENCE_OVERRIDE_ENABLE`: default `true`
- `LIVE_PERSISTENCE_OVERRIDE_MIN_QUALITY`: default `0.55`
- `LIVE_PERSISTENCE_OVERRIDE_MIN_SCANS`: default `3`
- `LIVE_PERSISTENCE_OVERRIDE_MIN_SCORE`: default `85.0`
- `LIVE_REQUIRE_STRATEGY_MATCH`: default `true`

## Order Book Risk Shell And Flow

- `LIVE_EVENT_LOCKOUT_MIN`: default `0`
- `LIVE_ENABLE_OFI`: default `true`
- `LIVE_EXPECTED_HOLD_HOURS`: default `8.0`
- `LIVE_FLOW_FEED_TTL_SEC`: default `300`
- `LIVE_CONT_FAST_MIN_OFI_Z`: default `0.35`
- `LIVE_IGNITE_MIN_OFI_Z`: default `0.60`
- `LIVE_MAX_CORRELATED_USD_EXPOSURE`: default `0`
- `LIVE_MAX_FUNDING_COST_R`: default `0.20`
- `LIVE_MAX_RECENT_SLIPPAGE_BPS`: default `15`
- `LIVE_MAX_SPREAD_BPS`: default `LIVE_OB_MAX_SPREAD_BPS (10)`
- `LIVE_MIN_BOOK_IMBALANCE`: default `LIVE_OB_IMBALANCE_MIN (1.10)`
- `LIVE_MIN_LIQ_BUFFER_MULT`: default `2.5`
- `LIVE_OB_FILTER_ENABLE`: default `false`
- `LIVE_OB_IMBALANCE_MIN`: default `1.10`
- `LIVE_OB_LEVELS`: default `5`
- `LIVE_OB_MAX_SPREAD_BPS`: default `10`
- `LIVE_OFI_EWMA_ALPHA`: default `0.05`
- `LIVE_OFI_MIN_SAMPLES`: default `8`
- `LIVE_REVERSAL_TOP_LONG_N`: default `5`
- `LIVE_REV_LONG_MIN_OFI_Z`: default `0.80`
- `LIVE_REV_SHORT_MAX_OFI_Z`: default `-0.80`
- `LIVE_REVERSAL_VOL_SPIKE_MIN`: default `3.0`
- `LIVE_RISK_SHELL_ENABLE`: default `true`
- `LIVE_VP_MIN_TARGET_PCT`: default `0.10`
- `LIVE_PRIORITY_WATCH_ENABLE`: default `true`
- `LIVE_PRIORITY_WATCH_EVERY_SEC`: default `1`
- `LIVE_PRIORITY_WATCH_TTL_MIN`: default `15`
- `LIVE_WATCH_SEC`: default `3`
- `LIVE_WATCHER_SEC`: default `LIVE_WATCH_SEC (3)`
- `WATCH_BOOK_LEVELS`: default `5`
- `WATCH_ENABLE`: default `true`
- `WATCH_MAX_CANDIDATES`: default `20`

## Telegram Reporting And Maintenance

- `ALLOW_DEAD_SESSION_TRADING`: default `false`
- `LIVE_ASIA_MIN_SLOPE`: default `0.01`
- `LIVE_ASIA_QUALITY_ENABLE`: default `false`
- `LIVE_ASIA_STRONG_CONF_MIN`: default `0.60`
- `LIVE_EVENTS_ENABLE`: default `true`
- `LIVE_EVENTS_STDOUT`: default `false`
- `LIVE_MAINT2_END_HOUR`: default `18`
- `LIVE_MAINT2_END_MIN`: default `0`
- `LIVE_MAINT2_FORCE_FLAT`: default `true`
- `LIVE_MAINT2_HOOK_TIMEOUT_SEC`: default `900`
- `LIVE_MAINT2_START_HOUR`: default `16`
- `LIVE_MAINT2_START_MIN`: default `0`
- `LIVE_MAINT_ENABLE`: default `true`
- `LIVE_MAINT_EOD_ENABLE`: default `true`
- `LIVE_MAINT_WARMUP_MIN`: default `0`
- `LIVE_PRE_EOD_ENTRY_BLOCK_MIN`: default `60`
- `LIVE_PRE_EOD_EXIT_ENABLE`: default `true`
- `LIVE_PRE_EOD_EXIT_END_HOUR`: default `16`
- `LIVE_PRE_EOD_EXIT_END_MIN`: default `0`
- `LIVE_PRE_EOD_EXIT_MIN_HOLD_MIN`: default `0`
- `LIVE_PRE_EOD_EXIT_UPNL_PCT_MAX`: default `0.30`
- `LIVE_PURE_MODE`: default `true`
- `LIVE_RECON_SEC`: default `10`
- `LIVE_REQUIRE_PAPER_DAYS`: default `0`
- `LIVE_TG_COMMANDS_ENABLE`: default `true`
- `LIVE_TG_DAILY_LIVE_RECEIPT_ENABLE`: default `true`
- `LIVE_TG_DAILY_LIVE_RECEIPT_LIMIT`: default `25`
- `LIVE_TG_DAILY_RECEIPT_ENABLE`: default `true`
- `LIVE_TG_DAILY_RECEIPT_LIMIT`: default `25`
- `LIVE_TG_DAILY_REPORT_DAY_OFFSET`: default `0`
- `LIVE_TG_DIGEST_MIN`: default `60`
- `LIVE_TG_EOD_REPORT_HOUR`: default `16`
- `LIVE_TG_EOD_REPORT_MIN`: default `0`
- `LIVE_TG_FILL_RECEIPT_ENABLE`: default `true`
- `LIVE_TG_HOURLY_ENABLE`: default `true`
- `LIVE_TG_LIST_LIMIT`: default `12`
- `LIVE_TG_PRE_US_REPORT_HOUR`: default `8`
- `LIVE_TG_PRE_US_REPORT_MIN`: default `0`
- `LIVE_TG_SOD_REPORT_HOUR`: default `18`
- `LIVE_TG_SOD_REPORT_MIN`: default `0`
- `LIVE_TG_SUGGEST_TTL_MIN`: default `15`
- `LIVE_TG_TRADE_TOP_N`: default `5`
- `LIVE_TG_TRADE_UPDATE_MIN`: default `60`
- `LIVE_TG_VERBOSE`: default `false`
- `LIVE_THROTTLE_DEDUPE_WINDOW_SECONDS`: default `120`
- `LIVE_THROTTLE_SYMBOL_FLIP_SIDE_COOLDOWN_SECONDS`: default `120`
- `LIVE_THROTTLE_SYMBOL_SAME_SIDE_COOLDOWN_SECONDS`: default `envInt("LIVE_THROTTLE_SYMBOL_COOLDOWN_SECONDS", 300`
- `POST_SL_COOLDOWN_MIN`: default `30`

## Discovery And Market Gate

- `LIVE_DISCOVERY_ENABLE`: default `true`
- `LIVE_DISCOVERY_LOOKBACK_MIN`: default `60`
- `LIVE_DISCOVERY_MIN_VOLATILITY`: default `0`
- `LIVE_DISCOVERY_MIN_VOLUME_RATIO`: default `1.5`
- `LIVE_DISCOVERY_REFRESH_SEC`: default `60`
- `LIVE_DISCOVERY_TOP_N`: default `10`
- `LIVE_GATE_EMA_FAST`: default `8`
- `LIVE_GATE_EMA_SLOW`: default `20`
- `LIVE_GATE_MIN_SCORE`: default `75`
- `LIVE_GATE_MIN_SLOPE`: default `0.15`
- `LIVE_GATE_MIN_VOLUME_RATIO`: default `1.5`
- `LIVE_GATE_MTF_USE_15M`: default `false`
- `LIVE_GATE_REGIME_MIN_ATR_PCT`: default `0.8`
- `LIVE_GATE_REQUIRE_MTF`: default `true`
- `LIVE_GATE_REQUIRE_REGIME`: default `false`
- `LIVE_GATE_REQUIRE_VOLUME_SPIKE`: default `true`

## Paper Engine

- `LIVE_PAPER_BE_ARM_R`: default `1.10`
- `LIVE_PAPER_ENABLE`: default `true`
- `LIVE_PAPER_FEE_BPS`: default `profile taker fee`
- `LIVE_PAPER_FEE_MAKER_BPS`: default `profile maker fee`
- `LIVE_PAPER_FEE_TAKER_BPS`: default `LIVE_PAPER_FEE_BPS`
- `LIVE_PAPER_FUNDING_INTERVAL_MIN`: default `480`
- `LIVE_PAPER_HARVEST_REENTRY_LOCK_MIN`: default `120`
- `LIVE_PAPER_HARVEST_REENTRY_MAX_STATE_MIN`: default `12.0`
- `LIVE_PAPER_HARVEST_REENTRY_MIN_SLOPE`: default `0.45`
- `LIVE_PAPER_LOSS_COOLDOWN_MIN`: default `0`
- `LIVE_PAPER_OB_LEVELS`: default `20`
- `LIVE_PAPER_PRE_FUNDING_EXIT_ENABLE`: default `true`
- `LIVE_PAPER_PRE_FUNDING_EXIT_MAX_UPNL`: default `2.5`
- `LIVE_PAPER_PRE_FUNDING_EXIT_MIN_AGE_MIN`: default `90`
- `LIVE_PAPER_PRE_FUNDING_EXIT_MIN_MFE_R`: default `1.2`
- `LIVE_PAPER_PRE_FUNDING_EXPENSIVE_RATE`: default `0.0008`
- `LIVE_PAPER_SLOT_REPLACE_ENABLE`: default `true`
- `LIVE_PAPER_SLOT_REPLACE_KEEP_MFE_R`: default `1.50`
- `LIVE_PAPER_SLOT_REPLACE_MAX_UPNL`: default `4.0`
- `LIVE_PAPER_SLOT_REPLACE_MIN_AGE_MIN`: default `90`
- `LIVE_PAPER_SLOT_REPLACE_MIN_CONF`: default `0.66`
- `LIVE_PAPER_SLOT_REPLACE_MIN_SCORE_GAP`: default `8.0`
- `LIVE_PAPER_SLOT_REPLACE_MIN_SLOPE`: default `0.10`
- `LIVE_PAPER_START_BALANCE`: default `1000`
- `LIVE_PAPER_STOP_PCT`: default `3.0`
- `LIVE_PAPER_SYMBOL_LOSS_LOCK_MIN`: default `5`
- `LIVE_PAPER_SYMBOL_MAX_LOSS_STREAK`: default `3`
- `LIVE_PAPER_SYMBOL_MAX_TRADES_PER_DAY`: default `2`
- `LIVE_PAPER_SYMBOL_REENTRY_EXCEPTION_CONF`: default `0.78`
- `LIVE_PAPER_SYMBOL_REENTRY_EXCEPTION_SLOPE`: default `0.85`
- `LIVE_PAPER_TP1_FRAC`: default `0.20`
- `LIVE_PAPER_TP1_R`: default `1.20`
- `LIVE_PAPER_TP2_FRAC`: default `0.15`
- `LIVE_PAPER_TP2_R`: default `2.50`
- `LIVE_PAPER_TP3_FRAC`: default `0.15`
- `LIVE_PAPER_TP3_R`: default `4.00`
- `LIVE_PAPER_TRAIL_AFTER_TP`: default `3`
- `LIVE_PAPER_TRAIL_STOP_PCT`: default `1.50`
- `LIVE_PAPER_TRAIL_STOP_PCT_TP3`: default `3.25`
- `PAPER_STRESS_BPS_ROUNDTRIP`: default `0`

## Shared Stops Targets And Exit Manager

- `LIVE_ATR_LEN`: default `14`
- `LIVE_BE_ARM_R`: default `1.10`
- `LIVE_BE_LOCK_BPS`: default `5`
- `LIVE_ENFORCE_MARGIN_TYPE`: default `true`
- `LIVE_ENTRY_TIMEOUT_SEC`: default `90`
- `LIVE_EXIT_LIQ_SPIKE_PARTIAL_PCT`: default `0.35`
- `LIVE_EXIT_NO_FT_BARS`: default `36`
- `LIVE_EXIT_NO_FT_MIN_MAE_R`: default `0.80`
- `LIVE_EXIT_NO_FT_MIN_MFE_R`: default `1.00`
- `LIVE_EXIT_PROFIT_GIVEBACK_PCT`: default `0.55`
- `LIVE_EXIT_PROFIT_LOCK_ARM_R`: default `1.40`
- `LIVE_EXIT_SPONSOR_FADE_HOLD_MIN`: default `120`
- `LIVE_EXIT_SPONSOR_GIVEBACK_PCT`: default `0.28`
- `LIVE_EXIT_SPONSOR_MIN_SCORE`: default `70.0`
- `LIVE_EXIT_SPONSOR_MIN_SLOPE`: default `0.02`
- `LIVE_EXIT_STALL_BARS`: default `3`
- `LIVE_EXIT_STALL_TIGHTEN_TO_R`: default `0.20`
- `LIVE_EXIT_WEAK_FLOW_BE_R`: default `1.20`
- `LIVE_FEE_DISCOUNT_PCT`: default `0`
- `LIVE_FUNDING_INTERVAL_MIN`: default `480`
- `LIVE_MAX_STOP_PCT`: default `8.0`
- `LIVE_MIN_RR_TP1`: default `0.8`
- `LIVE_MIN_STOP_PCT`: default `0.25`
- `LIVE_MOMENTUM_EXIT_ENABLE`: default `true`
- `LIVE_MOMENTUM_EXIT_MIN_HOLD_MIN`: default `35`
- `LIVE_MOMENTUM_EXIT_MIN_MFE_R`: default `1.75`
- `LIVE_MOMENTUM_EXIT_MIN_UPNL_PCT`: default `0.25`
- `LIVE_MOMENTUM_EXIT_SLOPE_MAX`: default `0.0`
- `LIVE_MULTI_ASSET_MODE`: default `false`
- `LIVE_PRE_FUNDING_EXIT_ENABLE`: default `true`
- `LIVE_PRE_FUNDING_EXIT_MAX_UPNL`: default `2.5`
- `LIVE_PRE_FUNDING_EXIT_MIN_AGE_MIN`: default `90`
- `LIVE_PRE_FUNDING_EXIT_MIN_MFE_R`: default `1.2`
- `LIVE_PRE_FUNDING_EXIT_WINDOW_MIN`: default `30`
- `LIVE_PRE_FUNDING_EXPENSIVE_RATE`: default `0.0008`
- `LIVE_RECOVERY_ATR_MULT`: default `1.5`
- `LIVE_RECOVERY_FORCE_FLAT_ON_STOP_FAIL`: default `true`
- `LIVE_RECOVERY_STOP_RETRIES`: default `3`
- `LIVE_RECOVERY_STOP_RETRY_SEC`: default `1`
- `LIVE_RISK_MARGIN_PCT`: default `5.0`
- `LIVE_RISK_ON_MARGIN_ENABLE`: default `true`
- `LIVE_STOP_ENGINE_V2_ENABLE`: default `true`
- `LIVE_STOP_ATR_MULT_CONT`: default `1.8`
- `LIVE_STOP_ATR_MULT_IGNITE`: default `1.4`
- `LIVE_STOP_ATR_MULT_REV`: default `1.2`
- `LIVE_STOP_PCT`: default `3.0`
- `LIVE_STOP_TEMPLATE_MODE`: default `setup`
- `LIVE_STOP_TRIGGER_REF`: default `mark`
- `LIVE_TP1_FRAC`: default `0.20`
- `LIVE_TP1_R`: default `1.20`
- `LIVE_TP2_FRAC`: default `0.15`
- `LIVE_TP2_R`: default `2.50`
- `LIVE_TP3_FRAC`: default `0.15`
- `LIVE_TP3_R`: default `4.00`
- `LIVE_TP_TRIGGER_REF`: default `mark`
- `LIVE_TRIGGER_PRICE_PROTECT`: default `true`
- `LIVE_TP_FRONT_RUN_PCT`: default `0.001`
- `LIVE_TRAIL_AFTER_TP`: default `3`
- `LIVE_TRAIL_ATR_MULT_CONT`: default `2.6`
- `LIVE_TRAIL_ATR_MULT_REV`: default `1.9`
- `LIVE_TRAIL_PCT_MIN`: default `1.0`
- `LIVE_TRAIL_SPONSORED_POST_TP3_MULT`: default `1.25`
- `LIVE_TRAIL_STEP_BPS`: default `10.0`
- `LIVE_TRAIL_STOP_PCT`: default `1.50`
- `LIVE_TRAIL_STOP_PCT_TP3`: default `3.25`

## Strategy Routing: Ignite Continuation Reversal Exhaustion

- `LIVE_BREAK_RETEST_MAX_BARS`: default `3`
- `LIVE_CONFLUENCE_FLOW_WEIGHT`: default `0.30`
- `LIVE_CONFLUENCE_STRATEGY_WEIGHT`: default `0.50`
- `LIVE_CONFLUENCE_STRUCTURE_WEIGHT`: default `0.20`
- `LIVE_CONT_CONFIRM_BARS`: default `2`
- `LIVE_CONT_FAST_APLUS_MAX_STATE_MIN`: default `28.0`
- `LIVE_CONT_FAST_BASE_CONF`: default `0.58`
- `LIVE_CONT_FAST_LATE_MIN_SCORE`: default `90.0`
- `LIVE_CONT_FAST_LATE_MIN_SLOPE`: default `0.16`
- `LIVE_CONT_FAST_LATE_MIN_VOL_RATIO`: default `1.35`
- `LIVE_CONT_FAST_MAX_STATE_MIN`: default `18.0`
- `LIVE_CONT_FAST_MIN_SCORE`: default `65.0`
- `LIVE_CONT_FAST_MIN_SLOPE`: default `0.02`
- `LIVE_CONT_FAST_MIN_VOL_RATIO`: default `1.15`
- `LIVE_CONT_REQUIRE_STRUCTURE_CONFIRM`: default `true`
- `LIVE_LEADER_UNWIND_OPPOSING_MAX_SLOPE`: default `0.10`
- `LIVE_LEADER_UNWIND_SHORT_MAX_STATE_MIN`: default `30.0`
- `LIVE_LEADER_UNWIND_SHORT_MIN_ABS_OFI_Z`: default `0.15`
- `LIVE_LEADER_UNWIND_SHORT_MIN_BEAR_SCORE`: default `3.0`
- `LIVE_LEADER_UNWIND_SHORT_MIN_DAYUTC_PCT`: default `-20.0`
- `LIVE_LEADER_UNWIND_SHORT_MIN_SCORE`: default `88.0`
- `LIVE_LEADER_UNWIND_SHORT_MIN_SCORE_GAP`: default `12.0`
- `LIVE_LEADER_UNWIND_SHORT_MIN_SLOPE`: default `0.35`
- `LIVE_LEADER_UNWIND_SHORT_MIN_VOL_RATIO`: default `0.80`
- `LIVE_LEADER_UNWIND_SHORT_RANK_BOOST`: default `12.0`
- `LIVE_ENABLE_CONTINUATION_FAST`: default `true`
- `LIVE_ENABLE_INSTITUTIONAL_PA`: default `false`
- `LIVE_ENABLE_MOMENTUM_IGNITE`: default `true`
- `LIVE_ENABLE_MOMENTUM_REVERSAL`: default `true`
- `LIVE_ENABLE_VP_SETUPS`: default `false`
- `LIVE_EXHAUSTION_AVOID_CHASE_RISK`: default `4.5`
- `LIVE_EXHAUSTION_BASE_CONF`: default `0.60`
- `LIVE_EXHAUSTION_LONG_BASE_CONF`: default `0.60`
- `LIVE_EXHAUSTION_LONG_BULL_SCORE_MIN`: default `4.5`
- `LIVE_EXHAUSTION_LONG_MIN_DRAWUP_PCT`: default `6.0`
- `LIVE_EXHAUSTION_LONG_MIN_SLOPE`: default `0.50`
- `LIVE_EXHAUSTION_LONG_MIN_STOP_PCT`: default `0.02`
- `LIVE_EXHAUSTION_LONG_STOP_LOOKBACK_BARS`: default `12`
- `LIVE_EXHAUSTION_LONG_STOP_MULT`: default `LIVE_EXHAUSTION_STOP_MULT (1.08)`
- `LIVE_EXHAUSTION_LONG_STOP_PAD_PCT`: default `0.0035`
- `LIVE_EXHAUSTION_LONG_TP1_R`: default `0.8`
- `LIVE_EXHAUSTION_LONG_TP2_R`: default `1.6`
- `LIVE_EXHAUSTION_LONG_TP3_R`: default `2.4`
- `LIVE_EXHAUSTION_MIN_DRAWDOWN_PCT`: default `-8.0`
- `LIVE_EXHAUSTION_MIN_SLOPE`: default `-0.75`
- `LIVE_EXHAUSTION_MIN_STOP_PCT`: default `0.02`
- `LIVE_EXHAUSTION_REVERSAL_SCORE_MIN`: default `5.5`
- `LIVE_EXHAUSTION_STARTER_MARGIN_FRAC`: default `0.50`
- `LIVE_EXHAUSTION_STOP_LOOKBACK_BARS`: default `12`
- `LIVE_EXHAUSTION_STOP_MULT`: default `1.08`
- `LIVE_EXHAUSTION_STOP_PAD_PCT`: default `0.0035`
- `LIVE_EXHAUSTION_TP1_R`: default `0.8`
- `LIVE_EXHAUSTION_TP2_R`: default `1.6`
- `LIVE_EXHAUSTION_TP3_R`: default `2.4`
- `LIVE_IGNITE_BASE_CONF`: default `0.56`
- `LIVE_IGNITE_HEATING_MAX_STATE_MIN`: default `14.0`
- `LIVE_IGNITE_INPLAY_MAX_STATE_MIN`: default `8.0`
- `LIVE_IGNITE_MIN_SCORE`: default `60.0`
- `LIVE_IGNITE_MIN_SLOPE`: default `0.08`
- `LIVE_IGNITE_MIN_VOL_RATIO`: default `1.00`
- `LIVE_IGNITE_STARTER_MARGIN_FRAC`: default `0.65`
- `LIVE_IGNITE_STOP_MULT`: default `1.10`
- `LIVE_IGNITE_TP1_R`: default `1.0`
- `LIVE_IGNITE_TP2_R`: default `2.4`
- `LIVE_IGNITE_TP3_R`: default `4.2`
- `LIVE_LATE_ENTRY_DAYUTC_BRAKE_PCT`: default `25`
- `LIVE_LATE_ENTRY_REQUIRE_UTC1H_RESET`: default `true`
- `LIVE_LATE_ENTRY_UTC1H_RESET_MIN_PCT`: default `0.8`
- `LIVE_MIN_CONFLUENCE_SCORE`: default `0.52`
- `LIVE_MIN_VP_CONFIDENCE`: default `0.55`
- `LIVE_RECLAIM_HOLD_BARS`: default `1`
- `LIVE_REQUIRE_LOCATION_HANDSHAKE`: default `false`
- `LIVE_REQUIRE_ORDERFLOW_HANDSHAKE`: default `false`
- `LIVE_REVERSAL_MIN_COMPLETENESS`: default `0.0`
- `LIVE_REVERSAL_MIN_CONFIDENCE`: default `0.0`
- `LIVE_REVERSAL_MIN_SCORE`: default `72.0`
- `LIVE_REVERSAL_MIN_STATE_MIN`: default `1.0`
- `LIVE_REVERSAL_SHORT_COOLING_MIN`: default `3.0`
- `LIVE_REVERSAL_SHORT_MIN_SLOPE`: default `0.25`
- `LIVE_REVERSAL_SLOPE_MIN`: default `0.15`
- `LIVE_SIMPLE_MODE`: default `true`
- `LIVE_USE_VP_REVERSAL`: default `false`

## Safety Limits And Kill Switches

- `LIVE_ALLOW_TESTNET`: default `false`
- `LIVE_MAX_DAILY_LOSS_PCT`: default `0`
- `LIVE_MAX_ORDERS_PER_DAY`: default `6`
- `LIVE_MAX_ORDERS_PER_HOUR`: default `2`
- `LIVE_ORDER_COOLDOWN_SEC`: default `180`
- `LIVE_SYMBOL_COOLDOWN_FLIP_SIDE_SEC`: default `120`
- `LIVE_SYMBOL_COOLDOWN_SAME_SIDE_SEC`: default `envInt("LIVE_SYMBOL_COOLDOWN_SEC", 900`
- `LIVE_SYMBOL_QUICK_LOSS_DAYUTC_PCT`: default `25.0`
- `LIVE_SYMBOL_QUICK_LOSS_LOCK_COUNT`: default `1`
- `LIVE_SYMBOL_QUICK_LOSS_LOCK_MIN`: default `60`
- `LIVE_SYMBOL_STOPOUT_COUNT`: default `3`
- `LIVE_SYMBOL_STOPOUT_LOCK_MIN`: default `5`
- `LIVE_SYMBOL_STOPOUT_WINDOW_MIN`: default `60`

## Payout And Reserve Lock

- `LIVE_PAYOUT_ANCHOR_HOUR`: default `16`
- `LIVE_PAYOUT_ANCHOR_MIN`: default `0`
- `LIVE_PAYOUT_CYCLE_DAYS`: default `1`
- `LIVE_PAYOUT_DEADLINE_MIN`: default `15`
- `LIVE_PAYOUT_ENABLE`: default `true`
- `LIVE_PAYOUT_KEEP_USDT`: default `0`
- `LIVE_PAYOUT_MIN_USDT`: default `1.0`
- `LIVE_PAYOUT_NOTIFY_TELEGRAM`: default `true`
- `LIVE_PAYOUT_ONLY_IF_FORCE_FLAT`: default `true`
- `LIVE_RESERVE_LOCK_ENABLE`: default `false`
- `LIVE_RESERVE_LOCK_LOSS_PCT`: default `40.0`
- `LIVE_RESERVE_LOCK_RECOVERY_PCT`: default `100.0`

## Stop Geometry Helpers

- `LIVE_SOFTEN_CONF_MAX`: default `0.65`
- `LIVE_STOP_STATE_MULT_MAX`: default `1.55`
- `LIVE_STOP_STATE_MULT_MIN`: default `0.88`
- `LIVE_STOP_SWING_VOL_USD`: default `100_000_000`
- `LIVE_STOP_VOL_USD_TIER1`: default `25_000_000`
- `LIVE_STOP_VOL_USD_TIER2`: default `100_000_000`
- `LIVE_STOP_VOL_USD_TIER3`: default `250_000_000`
- `LIVE_STOP_VOL_WIDEN_TIER1`: default `1.11`
- `LIVE_STOP_VOL_WIDEN_TIER2`: default `1.20`
- `LIVE_STOP_VOL_WIDEN_TIER3`: default `1.32`
- `LIVE_STOP_WIDEN_MULT`: default `1.32`
- `LIVE_TP1_MAX_R`: default `2.5`
- `LIVE_TP2_MAX_R`: default `4.0`
- `LIVE_TP3_MAX_R`: default `6.0`

## Inertia Breaker

- `LIVE_INERTIA_BREAKER_ENABLE`: default `true`
- `LIVE_INERTIA_FAST_N`: default `3`
- `LIVE_INERTIA_FAST_SLOPE_MAX`: default `-1.0`
- `LIVE_INERTIA_SCORE_MIN`: default `80`
- `LIVE_INERTIA_SLOW_N`: default `15`
- `LIVE_INERTIA_SLOW_SLOPE_MIN`: default `0.5`
