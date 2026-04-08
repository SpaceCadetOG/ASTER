# Developer Guide

## What this system is

ASTER is a scanner-driven perp trading system centered on `cmd/live`.

Today the production shape is:
- scanner-first
- fixed-size entries
- no pyramiding by default
- Telegram-operated
- Pi-deployed through tmux/systemd

The bot is designed to:
- rank markets with `cmd/long` and `cmd/short`
- trade the best live setups through `cmd/live`
- manage manual handoffs through Telegram
- maintain the perp balance around a target account size

## Runtime entry points

Main commands:
- `cmd/live`: production paper/live runtime
- `cmd/long`: long scanner
- `cmd/short`: short scanner
- `cmd/backtest`: historical simulation
- `cmd/exec`: direct exchange/auth/account checks

## Current live model

### Sizing

The current recommended live model is fixed-size:
- starter: `$50`
- re-entry: `$50`
- no adds
- max open positions: `4`
- max open per side: `4`

This is driven by env such as:
- `LIVE_ENTRY_STARTER_USDT=50`
- `LIVE_REENTRY_SIZE_USDT=50`
- `LIVE_FIXED_SIZE_NO_ADD=1`
- `LIVE_PYRAMID_MAX_ADDS=0`

### Leverage

Bot-native live trades start from configured leverage and can step down if the
exchange rejects the requested leverage.

Recommended default:
- `LIVE_LEVERAGE_MODE=fixed`
- `LIVE_LEVERAGE_FIXED=10`
- `LIVE_MAX_LEVERAGE=10`

Manual-approved trades also try to align to configured live leverage on handoff.

### Profit capture

Recent live behavior is tuned to:
- lock profit earlier once a trade is clearly working
- hold runners through healthier pullbacks
- tighten only when deterioration is real

The design goal is:
- avoid turning a strong winner into a small realized win
- still keep the bot disciplined on reversals

## How a bot trade flows

1. `cmd/long` and `cmd/short` produce ranked boards.
2. `cmd/live` consumes those boards and filters opportunities.
3. The risk shell rejects unsafe or low-quality candidates.
4. A starter order is placed.
5. The stop engine calculates a legal protective stop.
6. The trade is managed with:
   - stop updates
   - runner logic
   - re-entry logic
   - reduce-only limit exits
7. Realized profit can be swept from perp to spot.

## How manual trade management works

### Detection

If the operator opens a trade manually on exchange, `cmd/live` imports it.

### Approval

When the operator sends:

```text
/manage SYMBOL y
```

the bot should:
- adopt the live position
- mark it as bot-managed
- align leverage toward live config
- immediately attempt to attach a real stop
- continue managing it like a bot-native trade

### Commands

Important Telegram controls:
- `/positions`
- `/manage SYMBOL y`
- `/manage SYMBOL n`
- `/protect SYMBOL`
- `/scanner`
- `/longs`
- `/shorts`

Base symbols are resolved where supported, so commands like `/protect BULLA`
should resolve against `BULLAUSDT`.

## Funds maintenance

The runtime can maintain the perp account automatically.

Recommended settings:
- `LIVE_FUNDS_MANAGER_ENABLE=1`
- `LIVE_PERP_BAL_TARGET_USDT=200`
- `LIVE_PERP_BAL_FLOOR_USDT=150`
- `LIVE_FUNDS_MAINTENANCE_SEC=60`
- `LIVE_SWEEP_MIN_USDT=0.01`

Expected behavior:
- if perp available is above `$200`, sweep the difference to spot
- if perp available is below `$150`, top up back toward `$200`

## Pi deployment model

### Normal deploy

Use:

```bash
scripts/deploy_pi.sh
```

This is also what the GitHub Actions Pi deploy workflow uses.

### Runtime layout

The Pi normally uses:
- `aster-modules-tmux`
- `aster-autoupdate.timer`

The foreground operator script is:

```bash
scripts/run_live_logged.sh
```

### Logs

Use a dedicated Pi log directory outside the repo. Recommended:

```bash
ASTER_LOG_DIR=/home/traderbot/aster-logs
```

That keeps runtime logs separate from the checked-out repo.

### State

Use a stable state directory on the Pi:

```bash
LIVE_STATE_DIR=/opt/aster/state
```

This prevents cwd-dependent state drift across restarts.

## Common development tasks

### Run tests

```bash
GOCACHE=$(pwd)/.gocache go test ./cmd/live
```

### Run local paper live

```bash
LIVE_DRY_RUN=1 go run ./cmd/live
```

### Start scanners locally

```bash
go run ./cmd/long
go run ./cmd/short
```

### Validate exchange auth

```bash
ASTER_CONFIG=/etc/aster/.aster.yaml \
ASTER_AUTH_MODE=agent \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=auth_check \
go run ./cmd/exec
```

## High-value files for developers

- `/Users/victorogbebor/2026/go-machine/cmd/live/main.go`
- `/Users/victorogbebor/2026/go-machine/cmd/live/main_test.go`
- `/Users/victorogbebor/2026/go-machine/systemd/env/live.env.example`
- `/Users/victorogbebor/2026/go-machine/scripts/run_live_logged.sh`
- `/Users/victorogbebor/2026/go-machine/scripts/deploy_pi.sh`
- `/Users/victorogbebor/2026/go-machine/docs/pi_ops.md`

## Current operator assumptions

- the bot is trusted to trade its own setups
- manual trades can be handed off and managed like bot trades
- BTC, ETH, and SOL are tradable unless explicitly placed in
  `LIVE_CONTEXT_ONLY_SYMBOLS`
- exits are limit-based, protection is stop-based
- the main remaining optimization area is late-stage runner capture
