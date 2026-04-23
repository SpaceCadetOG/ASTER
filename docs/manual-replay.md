# Manual Trade Replay Utility

`cmd/manualreplay` replays a fixed manual trade against candle history.

It is pure Go and runs the same on macOS and Raspberry Pi:

```bash
go run ./cmd/manualreplay --help
```

## 1) Single trade replay

```bash
go run ./cmd/manualreplay \
  --symbol CHIPUSDT \
  --side BUY \
  --entry-ts "2026-04-21 19:36:52" \
  --entry-price 0.05793 \
  --qty 400 \
  --candles data/CHIPUSDT_1m.csv \
  --duration-min 1406 \
  --tz America/Chicago \
  --out-dir out/manual_replay_chip
```

Notes:
- `--entry-ts` accepts unix sec/ms, RFC3339, or `YYYY-MM-DD HH:MM[:SS]`.
- If you prefer capital-based sizing, use `--notional` instead of `--qty`.
- If both are set, `--qty` wins.

## 2) Multi-trade side-by-side replay

Use `--trades-csv` with headers:

- required: `symbol,side,entry_ts,entry_price,candles`
- optional: `label,qty,notional,to_ts,duration_min`

Example:

```csv
label,symbol,side,entry_ts,entry_price,notional,candles,duration_min
CHIP canonical,CHIPUSDT,BUY,2026-04-21 19:36:52,0.05793,23.17,data/CHIPUSDT_1m.csv,1406
RAVE ref,RAVEUSDT,BUY,2026-04-20 21:19:22,1.203131,23.17,data/RAVEUSDT_1m.csv,1406
ASTEROID ref,ASTEROIDUSDT,BUY,2026-04-20 23:06:24,0.000464,23.17,data/ASTEROIDUSDT_1m.csv,1406
```

Run:

```bash
go run ./cmd/manualreplay \
  --trades-csv reports/manual_replay_trades_rave_asteroid_chip.csv \
  --tz America/Chicago \
  --out-dir out/manual_replay_rave_asteroid_chip
```

Outputs:
- `replay_summary.json`
- `replay_summary.csv`
- `replay_summary.md`

All timestamps in outputs are UTC RFC3339 for deterministic automation.

