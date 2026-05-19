# How To: Manual Trade Replay

This guide shows how to replay manual trades from candle CSV files and generate side-by-side reports.

## Prerequisites

- Go installed (`go version`)
- Repo cloned locally
- Candle CSV files in `docs/examples/manual-replay/` using:
  - `timestamp,open,high,low,close,volume`
  - timestamp can be unix seconds or unix milliseconds

## 1) Go to repo

```bash
cd /Users/victorogbebor/2026/go-machine
```

## 2) Verify CLI flags

```bash
GOCACHE=$(pwd)/.gocache go run ./cmd/manualreplay --help
```

## 3) Replay one trade

Example: CHIP manual trade.

```bash
GOCACHE=$(pwd)/.gocache go run ./cmd/manualreplay \
  --symbol CHIPUSDT \
  --side BUY \
  --entry-ts "2026-04-21 19:36:52" \
  --entry-price 0.05793 \
  --qty 400 \
  --candles docs/examples/manual-replay/CHIPUSDT_1m.csv \
  --duration-min 1406 \
  --tz America/Chicago \
  --out-dir out/manual_replay_chip
```

Notes:
- `--entry-ts` accepts:
  - unix seconds
  - unix milliseconds
  - RFC3339
  - `YYYY-MM-DD HH:MM[:SS]`
- If you prefer notional sizing:
  - use `--notional 23.17` and omit `--qty`

## 4) Replay multiple trades side-by-side

Create input CSV (example):

```csv
label,symbol,side,entry_ts,entry_price,notional,candles,duration_min
CHIP canonical,CHIPUSDT,BUY,2026-04-21 19:36:52,0.05793,23.17,docs/examples/manual-replay/CHIPUSDT_1m.csv,1406
RAVE ref,RAVEUSDT,BUY,2026-04-20 21:19:22,1.203131,23.17,docs/examples/manual-replay/RAVEUSDT_1m.csv,1406
ASTEROID ref,ASTEROIDUSDT,BUY,2026-04-20 23:06:24,0.000464,23.17,docs/examples/manual-replay/ASTEROIDUSDT_1m.csv,1406
```

Run:

```bash
GOCACHE=$(pwd)/.gocache go run ./cmd/manualreplay \
  --trades-csv docs/examples/manual-replay/manual_replay_trades_rave_asteroid_chip.csv \
  --tz America/Chicago \
  --out-dir out/manual_replay_rave_asteroid_chip
```

## 5) Output files

Each run writes:
- `replay_summary.json`
- `replay_summary.csv`
- `replay_summary.md`

Example directory:
- `out/manual_replay_rave_asteroid_chip/`

## 6) Common issues

- `operation not permitted` under Go build cache:
  - run with `GOCACHE=$(pwd)/.gocache` (as shown above)
- `no candles in range`:
  - check `entry_ts`, `to_ts`/`duration_min`, and candle file coverage
- empty or wrong output:
  - confirm side is `BUY|SELL` (or `LONG|SHORT`)
  - confirm candle CSV has valid numeric values
