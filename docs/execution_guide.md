# Execution Guide (`cmd/exec`)

This guide covers the tested CLI flow for placing and canceling futures orders on Aster mainnet.

## 1) Prerequisites

- Repo path: `/Users/victorogbebor/2026/go-machine`
- Credentials file: `/Users/victorogbebor/2026/go-machine/.aster.yaml`
- Mainnet base URL: `https://fapi.asterdex.com`

Minimum `.aster.yaml` for mainnet HMAC:

```yaml
aster_auth_mode: hmac
aster_base_url: https://fapi.asterdex.com
aster_api_key: YOUR_API_KEY
aster_api_secret: YOUR_API_SECRET
```

## 2) Check Balance

USDT only:

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=balance \
EXEC_ASSETS=USDT \
go run ./cmd/exec
```

Check best bid/ask snapshot:

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=orderbook \
EXEC_SYMBOL=ETHUSDT \
go run ./cmd/exec
```

Quote sizing preview before place:

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=quote \
EXEC_SYMBOL=ETHUSDT \
EXEC_SIDE=BUY \
EXEC_KIND=LIMIT \
EXEC_AT=mid \
EXEC_OFFSET_BPS=-200 \
EXEC_USD=35 \
EXEC_MIN_NOTIONAL=20 \
go run ./cmd/exec
```

## 3) Place a LIMIT Order (Dry Run)

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=place \
EXEC_SYMBOL=ETHUSDT \
EXEC_SIDE=BUY \
EXEC_KIND=LIMIT \
EXEC_AT=mid \
EXEC_OFFSET_BPS=-200 \
EXEC_USD=35 \
EXEC_MIN_NOTIONAL=20 \
DRY_RUN=1 \
EXEC_DEBUG=1 \
go run ./cmd/exec
```

## 4) Place a Real LIMIT Order

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=place \
EXEC_SYMBOL=ETHUSDT \
EXEC_SIDE=BUY \
EXEC_KIND=LIMIT \
EXEC_AT=mid \
EXEC_OFFSET_BPS=-200 \
EXEC_USD=35 \
EXEC_MIN_NOTIONAL=20 \
DRY_RUN=0 \
EXEC_DEBUG=1 \
go run ./cmd/exec
```

Save either:
- numeric `orderId`, or
- string `clientOrderId`

from the returned JSON.

## 5) Cancel Order

### By numeric `orderId`

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=cancel \
EXEC_SYMBOL=ETHUSDT \
EXEC_ORDER_ID=123456789 \
DRY_RUN=0 \
go run ./cmd/exec
```

### By string `clientOrderId`

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=cancel \
EXEC_SYMBOL=ETHUSDT \
EXEC_CLIENT_ORDER_ID=YOUR_CLIENT_ORDER_ID \
DRY_RUN=0 \
go run ./cmd/exec
```

## 6) Check Order Status

### By numeric `orderId`

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=status \
EXEC_SYMBOL=ETHUSDT \
EXEC_ORDER_ID=123456789 \
go run ./cmd/exec
```

### By string `clientOrderId`

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=status \
EXEC_SYMBOL=ETHUSDT \
EXEC_CLIENT_ORDER_ID=YOUR_CLIENT_ORDER_ID \
go run ./cmd/exec
```

## 7) Useful Shortcuts (Makefile)

```bash
make exec-balance EXEC_ASSETS=USDT
make exec-account EXEC_SYMBOL=ETHUSDT
make exec-open-orders EXEC_SYMBOL=ETHUSDT
make exec-place EXEC_SYMBOL=ETHUSDT SIDE=BUY KIND=LIMIT USD=35 DRY_RUN=1 EXEC_AT=mid EXEC_OFFSET_BPS=-200
make exec-cancel EXEC_SYMBOL=ETHUSDT ORDER_ID=123456789
make exec-status EXEC_SYMBOL=ETHUSDT ORDER_ID=123456789
```

## 8) Emergency Flatten

Cancels all open orders for the symbol and closes open position with reduce-only market order.

Dry run:

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=flatten \
EXEC_SYMBOL=ETHUSDT \
DRY_RUN=1 \
EXEC_DEBUG=1 \
go run ./cmd/exec
```

Live:

```bash
ASTER_CONFIG=/Users/victorogbebor/2026/go-machine/.aster.yaml \
ASTER_AUTH_MODE=hmac \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=flatten \
EXEC_SYMBOL=ETHUSDT \
DRY_RUN=0 \
EXEC_DEBUG=1 \
go run ./cmd/exec
```

## 9) Common Errors

- `qty <= 0 after rounding`
  - Increase `EXEC_USD`, or set `EXEC_MIN_NOTIONAL` high enough for symbol step size.

- `Precision is over the maximum defined for this asset`
  - Ensure latest code is used (symbol meta matching + trimmed numeric formatting fixes are included).

- `Invalid API-key, IP, or permissions`
  - Verify key/secret, API permissions, and IP whitelist (including VPN egress IP).

## 10) Live-Lite Runtime Ops (Dual Maintenance)

- Margin mode enforced per trade: `LIVE_MARGIN_TYPE=ISOLATED`
- Maintenance windows (`America/Chicago`):
  - `22:00-23:30`: block new entries, keep risk management active
  - `16:00-18:00`: force flat at `16:00`, then block entries until `18:00`
- Hourly digest defaults:
  - `LIVE_TG_HOURLY_ENABLE=1`
  - `LIVE_TG_DIGEST_MIN=60`
  - `LIVE_TG_TRADE_UPDATE_MIN=60`
  - `LIVE_TG_COMMANDS_ENABLE=1`
  - EOD daily dispatch default at `18:00 CT`:
    - `LIVE_TG_DAILY_REPORT_HOUR=18`
    - `LIVE_TG_DAILY_REPORT_MIN=0`
    - `LIVE_TG_DAILY_REPORT_DAY_OFFSET=0`

Recommended env overrides in `/opt/aster/env/live-lite.env`:

```bash
LIVE_MARGIN_TYPE=ISOLATED
LIVE_ENFORCE_MARGIN_TYPE=1
LIVE_MULTI_ASSET_MODE=0
LIVE_STATE_DIR=/opt/aster/state
LIVE_FEE_PROFILE=pro
LIVE_FEE_DISCOUNT_PCT=0
LIVE_RESERVE_MODE=fixed
LIVE_RESERVE_USDT=5
LIVE_TRADE_MARGIN_MODE=fixed
LIVE_TRADE_MARGIN_USDT=10
LIVE_TRADE_MARGIN_MAX_USDT=50
LIVE_MAX_OPEN_POS=1
LIVE_RESERVE_LOCK_ENABLE=0
LIVE_RESERVE_LOCK_LOSS_PCT=40
LIVE_RESERVE_LOCK_RECOVERY_PCT=100
LIVE_STALE_ENABLE=1
LIVE_STALE_MAX_AGE_MIN=180
LIVE_MIN_STOP_PCT=0.25
LIVE_MAX_STOP_PCT=8.00
LIVE_MIN_RR_TP1=0.80
LIVE_BE_LOCK_BPS=5
LIVE_MAINT_ENABLE=1
LIVE_MAINT_TZ=America/Chicago
LIVE_MAINT1_START_HOUR=22
LIVE_MAINT1_START_MIN=0
LIVE_MAINT1_END_HOUR=23
LIVE_MAINT1_END_MIN=30
LIVE_MAINT2_START_HOUR=16
LIVE_MAINT2_START_MIN=0
LIVE_MAINT2_END_HOUR=18
LIVE_MAINT2_END_MIN=0
LIVE_MAINT2_FORCE_FLAT=1
LIVE_PAPER_STATE_FILE=out/paper_state.json
LIVE_PAPER_OB_LEVELS=20
LIVE_PAPER_FEE_BPS=4.0
LIVE_PAPER_FEE_MAKER_BPS=0.5
LIVE_PAPER_FEE_TAKER_BPS=4.0
LIVE_PAPER_FUNDING_INTERVAL_MIN=480
LIVE_PAPER_FUNDING_INTERVALS=BTCUSDT:480,ETHUSDT:480
LIVE_PAPER_OPEN_COST_MODE=aster
LIVE_PAYOUT_ENABLE=1
LIVE_PAYOUT_MODE=telegram_alert
LIVE_PAYOUT_CYCLE_DAYS=1
LIVE_PAYOUT_TZ=America/Chicago
LIVE_PAYOUT_ANCHOR_HOUR=16
LIVE_PAYOUT_ANCHOR_MIN=0
LIVE_PAYOUT_DEADLINE_MIN=15
LIVE_PAYOUT_ONLY_IF_FORCE_FLAT=1
LIVE_PAYOUT_MIN_USDT=1.0
LIVE_PAYOUT_KEEP_USDT=0
LIVE_PAYOUT_NOTIFY_TELEGRAM=1
LIVE_PAYOUT_STATE_FILE=out/payout_state.json
LIVE_PAYOUT_LEDGER_FILE=out/payouts.csv
LIVE_TRADES_FILE=out/live_trades.csv
LIVE_TG_COMMANDS_ENABLE=1
LIVE_TG_DAILY_REPORT_HOUR=18
LIVE_TG_DAILY_REPORT_MIN=0
LIVE_TG_DAILY_REPORT_DAY_OFFSET=0
LIVE_TG_DAILY_RECEIPT_ENABLE=1
LIVE_TG_DAILY_RECEIPT_LIMIT=25
LIVE_TG_DAILY_LIVE_RECEIPT_ENABLE=1
LIVE_TG_DAILY_LIVE_RECEIPT_LIMIT=25
LIVE_TG_FILL_RECEIPT_ENABLE=1
```

Paper continuity:

- Paper trader state is persisted to `LIVE_PAPER_STATE_FILE`.
- Restarting `cmd/live-lite` restores open paper positions, balance, and day stats from that file.
- Paper fills now use live orderbook depth and regime-aware slippage; funding is applied per symbol each funding interval.
- `LIVE_MULTI_ASSET_MODE=1` forces cross margin behavior (entry margin type set to `CROSSED`).
- Payout cycle defaults to daily and executes at `16:00 CT` with a hard deadline by `16:15 CT`.
- In paper mode payout is auto-debited; in live mode payout is Telegram-notified for manual withdraw.
- Payout withdraws only profit above trading base; set `LIVE_PAYOUT_KEEP_USDT` to keep a fixed top-up base.
- End-of-day receipt can include all trades and results via `LIVE_TG_DAILY_RECEIPT_ENABLE`.
- Daily report/receipts are dispatched after EOD by default at `18:00 CT` (configure with `LIVE_TG_DAILY_REPORT_*`).
- Live fills are journaled to `LIVE_TRADES_FILE`; enable second per-fill Telegram receipt with `LIVE_TG_FILL_RECEIPT_ENABLE=1`.
- To avoid state resets across different working directories, set `LIVE_STATE_DIR` to a stable absolute path (example: `/opt/aster/state`).
- Telegram command handlers:
  - `/help`
  - `/status`
  - `/balance`
  - `/positions`
  - `/pause`
  - `/resume`
  - `/forceflat`
