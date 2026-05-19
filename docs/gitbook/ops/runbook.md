# Runbook and Operations

## Local run (live dry-run)

```bash
cd /Users/victorogbebor/2026/go-machine
go run ./cmd/live
```

## Backtest run

```bash
cd /Users/victorogbebor/2026/go-machine
go run ./cmd/backtest
```

## Scanner run

```bash
go run ./cmd/long
go run ./cmd/short
```

## Foreground operator run

```bash
cd /Users/victorogbebor/2026/go-machine
bash scripts/run_live_logged.sh
```

This loads `/opt/aster/env/live.env` unless `ASTER_ENV_FILE` is overridden.

## Health checks

- Live:
  - `curl http://localhost:8787/healthz`
  - `curl http://localhost:8787/api/status`
- Long scanner:
  - `curl http://localhost:8080/health`
- Short scanner:
  - `curl http://localhost:8081/health`

## State files

Common persisted files under `out/`:
- `paper_state.json`
- `paper_trades.csv`
- `paper_equity.csv`
- `live_exec_state.json`
- `live_trades.csv`
- `payout_state.json`
- `payouts.csv`

On a dedicated host, set:
- `LIVE_STATE_DIR=/opt/aster/state`

## Logs

Local default:
- `logs/`

Host recommended:
- `/home/traderbot/aster-logs`

Set with:

```bash
ASTER_LOG_DIR=/home/traderbot/aster-logs
```

Typical files:
- `live-YYYY-MM-DD.log`
- `long-YYYY-MM-DD.log`
- `short-YYYY-MM-DD.log`

## Maintenance and payout timing

Operational baseline is defined by env (Chicago time):
- maintenance windows (`LIVE_MAINT1_*`, `LIVE_MAINT2_*`)
- pre-EOD exit window (`LIVE_PRE_EOD_EXIT_*`)
- payout SLA window (`LIVE_PAYOUT_*`)

## Host deployment note

The old Pi tmux/systemd orchestration layer was removed from this repo.

Current repo guidance is limited to:
- direct `go run ./cmd/...` usage
- `scripts/run_live_logged.sh` for a logged foreground `cmd/live` session
- `scripts/deploy_pi.sh` as a host binary/env-example refresh helper

## Funds maintenance

The live runtime can maintain the perp account automatically:
- target: `LIVE_PERP_BAL_TARGET_USDT`
- floor: `LIVE_PERP_BAL_FLOOR_USDT`
- cadence: `LIVE_FUNDS_MAINTENANCE_SEC`

Current recommended defaults:
- target: `$200`
- floor: `$150`
- check every `60s`
- sweep minimum: `LIVE_SWEEP_MIN_USDT=0.01`

Behavior:
- if perp available is above target, sweep the difference to spot
- if perp available is below floor, top up back toward target
