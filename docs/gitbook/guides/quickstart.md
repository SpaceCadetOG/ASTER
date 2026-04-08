# Quickstart Guides

## 1) Start scanners

```bash
cd /Users/victorogbebor/2026/go-machine
go run ./cmd/long
go run ./cmd/short
```

- Long UI/API: `http://localhost:8080`
- Short UI/API: `http://localhost:8081`

## 2) Start live (paper)

```bash
cd /Users/victorogbebor/2026/go-machine
LIVE_DRY_RUN=1 go run ./cmd/live
```

Status API:
- `http://localhost:8787/healthz`
- `http://localhost:8787/api/status`

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

## 5) Promote to live mode

Only after paper validation:

```bash
LIVE_DRY_RUN=0 LIVE_ENABLE_LIVE_TRADING=1 go run ./cmd/live
```

Use isolated margin defaults and keep risk shell enabled.

## 6) Pi operator start

On the Pi, the normal operator entrypoint is:

```bash
cd /home/traderbot/actions-runner/_work/ASTER/ASTER/scripts
bash run_live_logged.sh
```

That script:
- loads `/opt/aster/env/live.env` by default
- writes rotating logs to `ASTER_LOG_DIR` if set
- launches the `cmd/live` runtime

Recommended Pi overrides:
- `ASTER_LOG_DIR=/home/traderbot/aster-logs`
- `LIVE_STATE_DIR=/opt/aster/state`

## 7) Current live operating model

The current recommended live setup is:
- fixed trade size: `$50`
- re-entry size: `$50`
- no adds
- max open positions: `4`
- target perp balance: `$200`
- floor: `$150`
- fixed leverage default: `10x`
