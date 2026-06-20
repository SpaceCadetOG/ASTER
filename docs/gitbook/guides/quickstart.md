# Quickstart Guides

## 1) Start `cmd/live` in paper/manual mode

```bash
cd /Users/victorogbebor/2026/go-machine
LIVE_DRY_RUN=1 go run ./cmd/live
```

Status API:
- `http://localhost:8787/healthz`
- `http://localhost:8787/api/status`

Current runtime posture:
- `cmd/live` self-scans and does not require `cmd/long` or `cmd/short` to be
  running first
- autonomous entry logic exists in the codebase
- the active production posture is currently manual-only / ground-zero mode

## 2) Optional: start standalone scanners

```bash
cd /Users/victorogbebor/2026/go-machine
go run ./cmd/long
go run ./cmd/short
```

- Long UI/API: `http://localhost:8080`
- Short UI/API: `http://localhost:8081`
- These are standalone scanner/dashboard/diagnostic products, not required
  runtime prerequisites for `cmd/live`

## 3) Run backtest

```bash
cd /Users/victorogbebor/2026/go-machine
BT_SYMBOLS=BTCUSDT,ETHUSDT,SOLUSDT go run ./cmd/backtest
```

Artifacts:
- `out/backtests/<symbol>/<strategy>/trades.csv`
- `out/backtests/<symbol>/<strategy>/report.json`
- `out/backtests/summary.json`

## 4) Use Telegram controls

In your bot chat:
- `/help`
- `/status`
- `/balance`
- `/positions`
- `/scanner`
- `/longs`
- `/shorts`
- `/manage SYMBOL y`
- `/protect SYMBOL`
- `/pause` and `/resume`
- `/close SYMBOL`
- `/closeall`

Note:
- operator surfaces are active
- autonomous entry is not currently documented as active production behavior

## 5) Promote to live mode

Only after paper validation and explicit operational approval:

```bash
LIVE_DRY_RUN=0 LIVE_ENABLE_LIVE_TRADING=1 go run ./cmd/live
```

Use isolated margin defaults and keep risk shell enabled.

This does not imply that autonomous entry is currently an active production
mode. Current posture remains manual-only / ground-zero until staged
revalidation is complete.

## 6) Logged host/manual start

The current logged operator entrypoint is:

```bash
cd /Users/victorogbebor/2026/go-machine
bash scripts/run_live_logged.sh
```

That script:
- loads `/opt/aster/env/live.env` by default
- writes rotating logs to `ASTER_LOG_DIR` if set
- launches the `cmd/live` runtime

Recommended host overrides:
- `ASTER_LOG_DIR=/home/traderbot/aster-logs`
- `LIVE_STATE_DIR=/opt/aster/state`

## 7) Current runtime operating model

The current recommended live setup is:
- fixed trade size: `$50`
- re-entry size: `$50`
- no adds
- max open positions: `4`
- target perp balance: `$200`
- floor: `$150`
- fixed leverage default: `10x`

Architecture note:
- `cmd/live` is the canonical production runtime
- `cmd/long` and `cmd/short` are standalone scanner products
