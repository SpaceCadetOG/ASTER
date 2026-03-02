# Backtest Guide (V1 Orderflow Replay)

`cmd/backtest` replays candles and optionally scanner/whale logs.

## Input Files

1. Candles CSV (required), example:
- `data/POWERUSDT_1m.csv`
- columns: `timestamp,open,high,low,close,volume`

2. Scanner JSONL (optional), example line:
```json
{"ts":1708847100,"symbol":"POWERUSDT","score":79.7,"conf":0.71,"grade":"A"}
```

3. Whale JSONL (optional), example line:
```json
{"ts":1708847105,"symbol":"POWERUSDT","usd":52000,"side":"BUY"}
```

## Run

Minimal (candles only):

```bash
BT_CANDLES=data/POWERUSDT_1m.csv BT_SYMBOL=POWERUSDT go run ./cmd/backtest
```

With scanner + whales:

```bash
BT_CANDLES=data/POWERUSDT_1m.csv \
BT_SCANNER=data/scanner.jsonl \
BT_WHALES=data/POWERUSDT_whales.jsonl \
BT_SYMBOL=POWERUSDT \
BT_START_BALANCE=10000 \
BT_SCORE_MIN=75 \
BT_CONF_MIN=0.65 \
BT_WHALE_DELTA_MIN=50000 \
BT_BUY_PCT_MIN=55 \
BT_WHALE_WINDOW_SEC=30 \
BT_RISK_PCT=0.01 \
BT_TP_R=2 \
BT_SL_R=1 \
go run ./cmd/backtest
```

## Strategy Logic (V1)

Long entry:
- scanner score >= `BT_SCORE_MIN` (if scanner file provided)
- scanner conf >= `BT_CONF_MIN` (if scanner file provided)
- whale delta >= `BT_WHALE_DELTA_MIN` (if whale file provided)
- whale buy% >= `BT_BUY_PCT_MIN` (if whale file provided)

Exit:
- TP at `BT_TP_R * BT_RISK_PCT`
- SL at `BT_SL_R * BT_RISK_PCT`
- whale delta flip negative (if whale file provided)

## Output Metrics

- Total Trades
- Win Rate
- Profit Factor
- Max Drawdown
- Expectancy (avg trade return)
- Sharpe (trade-return based)
