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
- `/pause` and `/resume`
- `/close SYMBOL`
- `/closeall`

## 5) Promote to live mode

Only after paper validation:

```bash
LIVE_DRY_RUN=0 LIVE_ENABLE_LIVE_TRADING=1 go run ./cmd/live
```

Use isolated margin defaults and keep risk shell enabled.
