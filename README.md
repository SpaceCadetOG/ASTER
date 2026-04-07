# go-machine

Autonomous crypto-perps trading stack (scanner -> confluence -> paper/live execution).

## Architecture Layers

- `internal/discovery`: hot-token discovery universe (volume spike + volatility + movers).
- `internal/gate`: hard entry quality gate (grade/score/slope/volume/MTF/regime).
- `internal/throttle`: symbol cooldown + intent dedupe.
- `internal/risk`: pre-trade hard gates (liq buffer, funding, spread, book imbalance).
- `cmd/live`: orchestration loop (paper/live parity, maintenance windows, digests).
- `internal/stats` + `cmd/stats`: event-log aggregation and performance reports.

## Runtime Flow

1. Fetch market rows.
2. Build discovery universe (optional).
3. Track in-play state + rank strategy candidates.
4. Apply gate + throttle + risk checks.
5. Emit trade intent and execute (paper/live).
6. Persist structured events to JSONL.

Safe default: if gate/throttle/risk fails, no trade intent is emitted.

## Config

Primary runtime controls use `LIVE_*` env vars (see `systemd/env/live.env.example`).

A versioned schema sample is provided at `config/trading.yaml` for:
- `discovery`
- `gates`
- `throttle`
- `events`

## Run

Paper:

```bash
go run ./cmd/live
```

Live:

```bash
LIVE_DRY_RUN=0 LIVE_ENABLE_LIVE_TRADING=1 go run ./cmd/live
```

Manual trades may be imported for tracking. Bot-managed status is granted only after protection is successfully attached; unprotected manual-managed positions block new entries and can be force-closed by the safety layer.

## Stats CLI

Aggregate JSONL event logs:

```bash
go run ./cmd/stats -log logs/events.jsonl -from 2026-03-01 -to 2026-03-05
```

Optional CSV:

```bash
go run ./cmd/stats -log logs/events.jsonl -csv out/stats_report.csv
```

Example output:

- total trades, wins/losses, win rate
- profit factor, expectancy, max drawdown
- avg/median R
- per-strategy and per-symbol breakdowns
