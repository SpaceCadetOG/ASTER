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

## 8) Common Errors

- `qty <= 0 after rounding`
  - Increase `EXEC_USD`, or set `EXEC_MIN_NOTIONAL` high enough for symbol step size.

- `Precision is over the maximum defined for this asset`
  - Ensure latest code is used (symbol meta matching + trimmed numeric formatting fixes are included).

- `Invalid API-key, IP, or permissions`
  - Verify key/secret, API permissions, and IP whitelist (including VPN egress IP).
