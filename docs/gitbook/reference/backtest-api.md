# Backtest API

Sources:
- `internal/backtest/engine.go`
- `cmd/backtest/main.go`

## CLI entry

Run:
```bash
go run ./cmd/backtest
```

## Config model

`internal/backtest.Config` includes:
- strategy + symbol/timeframe
- fees/slippage
- margin/leverage/reserve
- VP risk mode controls
- event lockout + correlated cap
- funding + risk-shell controls

## Engine flow (`Run`)

1. Build feature snapshot per bar.
2. Evaluate strategies (router or named strategy).
3. Risk-shell approval for pending entry.
4. Simulated entry/exit with fee/slippage.
5. Funding impact applied on exit.
6. Write trade/report artifacts.

## Output artifacts

Per symbol/strategy:
- `out/backtests/<symbol>/<strategy>/trades.csv`
- `out/backtests/<symbol>/<strategy>/report.json`

Batch:
- `out/backtests/summary.json`

## `trades.csv` fields (core)
- `symbol,strategy,side`
- `entry_ts,exit_ts,entry,exit`
- `stop,tp1,tp2,qty`
- `pnl,r,reason,hold_mins`
- `fees,slippage,funding_impact,liq_buffer_ok`
- signal explainability fields (`vp_*`, `reject_reason`, `regime_tag`, `signal_reasons`, `signal_source`)
