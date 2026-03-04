# VP Strategy Integration

This document maps the book-style volume profile logic into the current stack.

## Implemented setup families

- `vp_accumulation`
  - Find heaviest volume in a recent rotation range.
  - Trade first revisit in impulse direction.
- `vp_trend`
  - In non-range trend, find in-trend heavy-volume cluster.
  - Trade trend-direction retest.
- `vp_rejection`
  - Detect strong rejection candle region.
  - Trade revisit of heaviest rejection volume.
- `vp_reversal`
  - Detect decisive failure across key VP level (POC role flip proxy).
  - Trade retest in reversed direction.
- `daily_open_sr`
  - 07:00 America/Chicago anchor.
  - Acceptance + retest around daily open.
- `pd_levels_retest`
  - Prior-day/prior-week levels from NY17 day partition (16:00 Chicago).
  - Breach -> acceptance -> retest logic.
- `failed_auction_magnet`
  - Detect failed auction magnet and trade pull toward unresolved level.
- `vwap_confluence`
  - VWAP bias and role-flip (reclaim/loss) confluence.

## Risk policy

Shared resolver in `internal/strategies/risk_policy.go`:

- Stop mode:
  - `fixed`: volatility-like percent stop
  - `vp`: significant VP zone-derived stop
  - `hybrid`: wider protective of fixed and VP
- Target mode:
  - `rr`: classic R-multiple targets
  - `vp`: first opposing significant VP level (front-run adjusted)
  - `hybrid`: VP target when minimum R threshold is met

## Explainability fields

Signals and runtime records include:

- `vp_setup`
- `vp_level`
- `vp_target_level`
- `vp_stop_mode`
- `vp_target_mode`
- `reject_reason`

## Runtime controls

Backtest:

- `BT_STOP_MODE`
- `BT_TARGET_MODE`
- `BT_VP_MIN_TARGET_PCT`
- `BT_EVENT_LOCKOUT_MIN`
- `BT_MAX_CORRELATED_POS`

Live-lite:

- `LIVE_STOP_MODE`
- `LIVE_TARGET_MODE`
- `LIVE_VP_MIN_TARGET_PCT`
- `LIVE_EVENT_LOCKOUT_MIN`
- `LIVE_MAX_CORRELATED_USD_EXPOSURE`
- `LIVE_CORR_GROUPS`

Session regime (automatic):

- Timezone: `America/Chicago`
- Day reset: `16:00` (NY17 equivalent)
- US morning context: `07:00`
- Regimes:
  - `ASIA` 19:00-02:00
  - `EU` 02:00-10:00
  - `US` 07:00-16:00
  - `ASIA_EU_OVERLAP` 02:00-03:00
  - `EU_US_OVERLAP` 07:00-10:00
- Router applies regime-aware score/risk multiplier and emits `regime_tag` in traces.

## Notes

- Event lockout is a lightweight placeholder gate.
- Correlation control is static-group based in this iteration.
- Flexible profile behavior is approximated from candle-derived volume-at-price bins.
