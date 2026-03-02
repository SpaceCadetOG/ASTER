# Aster Exec Smoke Tests

Credentials are loaded in this order:
1. `ASTER_API_KEY` / `ASTER_API_SECRET` environment variables
2. YAML file from `ASTER_CONFIG`
3. `~/.aster.yaml`

Supported auth configuration:
- `aster_auth_mode: hmac|auto` (`agent` reserved in this build)
- HMAC fields: `aster_api_key`, `aster_api_secret`
- Agent fields (`aster_user`, `aster_signer`, `aster_private_key`) are kept for forward compatibility.

Base URL overrides:
- `ASTER_BASE_URL` applies globally to `RESTAuth`
- `EXEC_BASE_URL` applies to `cmd/exec` only (overrides per run)
- Futures V3 default paths are used first (`/fapi/v3/...`) with fallback to older versions where needed.

Example YAML:

```yaml
aster_api_key: YOUR_KEY
aster_api_secret: YOUR_SECRET
```

See full template in [`yaml.example`](/Users/victorogbebor/2026/go-machine/yaml.example).

## Balance

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=balance go run ./cmd/exec
```

Short form from repo root:

```bash
make exec-balance
```

Example with explicit base URL:

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com EXEC_ACTION=balance go run ./cmd/exec
```

Testnet candidates (depends on Aster deployment):

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex-testnet.com EXEC_ACTION=balance go run ./cmd/exec
```

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://www.asterdex-testnet.com EXEC_ACTION=balance go run ./cmd/exec
```

## Account summary

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=account EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
```

Short form:

```bash
make exec-account EXEC_SYMBOL=BTCUSDT
```

## Open orders

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=open_orders EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
```

Short form:

```bash
make exec-open-orders EXEC_SYMBOL=BTCUSDT
```

## Position risk

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=position EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
```

Short form:

```bash
make exec-position EXEC_SYMBOL=BTCUSDT
```

## Dry-run place (safe)

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=place EXEC_SYMBOL=ETHUSDT EXEC_SIDE=BUY EXEC_KIND=LIMIT EXEC_AT=mid EXEC_OFFSET_BPS=-2 EXEC_USD=80 DRY_RUN=1 EXEC_DEBUG=1 go run ./cmd/exec
```

Short form:

```bash
make exec-place EXEC_SYMBOL=ETHUSDT SIDE=BUY KIND=LIMIT USD=80 DRY_RUN=1 EXEC_AT=mid EXEC_OFFSET_BPS=-2
```

## Real place (sends live order)

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=place EXEC_SYMBOL=ETHUSDT EXEC_SIDE=BUY EXEC_KIND=LIMIT EXEC_AT=mid EXEC_OFFSET_BPS=-2 EXEC_USD=80 DRY_RUN=0 EXEC_DEBUG=1 go run ./cmd/exec
```

Short form:

```bash
make exec-place EXEC_SYMBOL=ETHUSDT SIDE=BUY KIND=LIMIT USD=80 DRY_RUN=0 EXEC_AT=mid EXEC_OFFSET_BPS=-2
```

## Cancel one order

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=cancel EXEC_SYMBOL=ETHUSDT EXEC_ORDER_ID=123456789 DRY_RUN=0 go run ./cmd/exec
```

Short form:

```bash
make exec-cancel EXEC_SYMBOL=ETHUSDT ORDER_ID=123456789
```

## Cancel all orders for a symbol

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=cancel_all EXEC_SYMBOL=ETHUSDT DRY_RUN=0 go run ./cmd/exec
```

Short form:

```bash
make exec-cancel-all EXEC_SYMBOL=ETHUSDT
```

## Order status

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=status EXEC_SYMBOL=ETHUSDT EXEC_ORDER_ID=123456789 go run ./cmd/exec
```

Short form:

```bash
make exec-status EXEC_SYMBOL=ETHUSDT ORDER_ID=123456789
```
