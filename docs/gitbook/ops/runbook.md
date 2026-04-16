# Runbook and Operations

## Local run (live-lite dry-run)

```bash
cd /Users/victorogbebor/2026/go-machine
go run ./cmd/live-lite
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

## Health checks

- Live-lite:
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

## Maintenance and payout timing

Operational baseline is defined by env (Chicago time):
- maintenance windows (`LIVE_MAINT1_*`, `LIVE_MAINT2_*`)
- pre-EOD exit window (`LIVE_PRE_EOD_EXIT_*`)
- payout SLA window (`LIVE_PAYOUT_*`)

## Pi deployment

Use existing docs and scripts:
- [`docs/pi_ops.md`](../pi_ops.md)
- `scripts/deploy_pi.sh`
- `scripts/auto_update_aster.sh`
- `systemd/aster-*.service`
