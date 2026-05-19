# Legacy Host Ops

This page documents the current interim host workflow while ASTER is still
transitioning toward a GCP deployment model. It is no longer the source of
truth for a permanent Pi-specific orchestration stack.

## Scope

Use this page if you need to:
- build the current binaries on a local or dedicated Linux host
- refresh the `/opt/aster` binary/env layout with `scripts/deploy_pi.sh`
- run the core commands manually during the migration window

The old tmux/autoupdate/systemd orchestration layer was removed from the repo
in Cleanup Pass 1.

## Compile-only verification

```bash
GOCACHE=/tmp/go-build go test ./... -run TestDoesNotExist
```

## Manual command checks

```bash
go run ./cmd/long
go run ./cmd/short
go run ./cmd/tape
go run ./cmd/whale
```

Exchange/account checks:

```bash
ASTER_CONFIG=~/.aster.yaml ASTER_AUTH_MODE=agent EXEC_BASE_URL=https://fapi.asterdex.com EXEC_ACTION=auth_check go run ./cmd/exec
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com EXEC_ACTION=balance go run ./cmd/exec
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com EXEC_ACTION=open_orders EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com EXEC_ACTION=position EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
```

Live dry-run:

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com LIVE_DRY_RUN=1 LIVE_SHOW_ACCOUNT=1 LIVE_ACCOUNT_ASSETS= go run ./cmd/live
```

## Interim host helper

```bash
scripts/deploy_pi.sh
```

This helper is intentionally kept for now as a temporary bridge. It refreshes
host binaries and env examples, but it no longer installs or enables the old
tmux/autoupdate/systemd orchestration.

## Logged foreground run

```bash
cd /Users/victorogbebor/2026/go-machine
bash scripts/run_live_logged.sh
```

## Recommended host paths

- env: `/opt/aster/env/live.env`
- state: `/opt/aster/state`
- logs: `/home/traderbot/aster-logs`

Set for stable state persistence:

```bash
LIVE_STATE_DIR=/opt/aster/state
```

Set for off-repo runtime logs:

```bash
ASTER_LOG_DIR=/home/traderbot/aster-logs
```

## Maintenance and payout timing

Operational timing remains env-driven:
- maintenance windows: `LIVE_MAINT1_*`, `LIVE_MAINT2_*`
- pre-EOD exit window: `LIVE_PRE_EOD_EXIT_*`
- payout/report windows: `LIVE_PAYOUT_*`, `LIVE_TG_*`
