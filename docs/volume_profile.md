# Volume Profile Notes (POC / Value Area)

This adds a practical Volume Profile workflow based on the chapter concepts:

- volume at price matters more than volume at time for level discovery
- POC (point of control) is the most traded price in the sampled range
- VAH / VAL define the value area (default 70% of total volume)
- HVN / LVN can be used as reaction/acceptance and rejection/fast-move zones

## New CLI

Run:

```bash
go run ./cmd/vp
```

Environment overrides:

- `VP_SYMBOL` default `BTCUSDT`
- `VP_TF` default `5m`
- `VP_N` default `300`
- `VP_BINS` default `72`
- `VP_VALUE_PCT` default `0.70`
- `VP_HVN_N` default `5`
- `VP_LVN_N` default `5`
- `VP_JSON=1` for JSON output

Example:

```bash
VP_SYMBOL=ETHUSDT VP_TF=1m VP_N=500 VP_BINS=96 go run ./cmd/vp
```

## How to read output

- `context=INSIDE_VALUE`: market is accepted inside current profile range.
- `context=ABOVE_VAH`: potential continuation or value migration higher.
- `context=BELOW_VAL`: potential continuation lower or failed auction below value.
- `distPOC` (bp): distance from current price to POC in basis points.
- `shape`: coarse profile shape (`D` balanced, `P` top-heavy, `b` bottom-heavy).
- `POCshare`: concentration of total volume at POC.
- `VAwidth`: value area width as a percent of POC price.

Use with existing tape/whale/scanner:

1. If scanner/flow is bullish and price reclaims POC from below, bias long.
2. If scanner weakens and price loses POC + stays below VAL, bias short.
3. Treat HVN as likely pause/rotation zones; LVN as likely rejection/travel zones.

## Strategy integration (backtest + live-lite)

Volume profile rules are now integrated into router-driven strategy selection:

- `vp_accumulation`: rotation + impulse + first revisit of the heavy-volume level.
- `vp_trend`: trend continuation retest at in-trend heavy-volume level.
- `vp_rejection`: strong rejection zone retest at heaviest rejection volume.
- `vp_reversal`: decisive failure through key level, then role-flip retest.

Risk policy modes:

- stop mode: `fixed`, `vp`, `hybrid`
- target mode: `rr`, `vp`, `hybrid`

Backtest env knobs:

- `BT_STOP_MODE` default `hybrid`
- `BT_TARGET_MODE` default `hybrid`
- `BT_VP_MIN_TARGET_PCT` default `0.10`
- `BT_EVENT_LOCKOUT_MIN` default `0`
- `BT_MAX_CORRELATED_POS` default `1`

Live-lite env knobs:

- `LIVE_STOP_MODE` default `hybrid`
- `LIVE_TARGET_MODE` default `hybrid`
- `LIVE_VP_MIN_TARGET_PCT` default `0.10`
- `LIVE_EVENT_LOCKOUT_MIN` default `0`
- `LIVE_MAX_CORRELATED_USD_EXPOSURE` default `0` (disabled)
- `LIVE_CORR_GROUPS` default empty
  - format: `BTCUSDT,ETHUSDT;SOLUSDT,AVAXUSDT`

## Implementation notes

Current profile engine uses candle quote-volume and allocates each candle's
volume across its `[low..high]` range bins (uniform allocation).
This is a robust approximation when full tick-level volume-at-price is not available.
