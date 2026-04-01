# Aster Exec Smoke Tests

`cmd/exec` now treats Aster Pro / Futures V3 agent auth as the primary live path.

Credentials are loaded in this order:
1. environment variables
2. YAML file from `ASTER_CONFIG`
3. `~/.aster.yaml`

Supported auth configuration:
- `ASTER_AUTH_MODE=agent` => primary live path
- `ASTER_AUTH_MODE=hmac` => legacy fallback only
- `ASTER_AUTH_MODE=auto` => prefers agent when `ASTER_USER`, `ASTER_SIGNER`, `ASTER_PRIVATE_KEY`, and `ASTER_CHAIN_ID` are present

Agent auth fields:
- `ASTER_USER` / `aster_user` = main wallet address
- `ASTER_SIGNER` / `aster_signer` = approved API wallet / agent
- `ASTER_PRIVATE_KEY` / `aster_private_key` = signer private key
- `ASTER_CHAIN_ID` / `aster_chain_id` = `1666` for mainnet

Base URL overrides:
- `ASTER_BASE_URL` applies globally to `RESTAuth`
- `EXEC_BASE_URL` applies to `cmd/exec` only
- Mainnet default: `https://fapi.asterdex.com`

Example mainnet agent YAML:

```yaml
aster_auth_mode: agent
aster_base_url: https://fapi.asterdex.com
aster_user: 0xYOUR_MAIN_WALLET
aster_signer: 0xYOUR_APPROVED_API_WALLET
aster_private_key: 0xYOUR_SIGNER_PRIVATE_KEY
aster_chain_id: 1666
```

Legacy HMAC fallback is still supported explicitly:

```yaml
aster_auth_mode: hmac
aster_base_url: https://fapi.asterdex.com
aster_api_key: YOUR_LEGACY_API_KEY
aster_api_secret: YOUR_LEGACY_API_SECRET
```

Do not mix agent and HMAC creds in live mode unless you are intentionally testing fallback.

See full template in [`yaml.example`](/Users/victorogbebor/2026/go-machine/yaml.example).

## Auth sanity check

Runs:
- `/fapi/v3/ping`
- `/fapi/v3/time`
- `/fapi/v3/agent`
- `/fapi/v3/account`
- `/fapi/v3/balance`
- `/fapi/v3/openOrders`

```bash
ASTER_CONFIG=~/.aster.yaml \
EXEC_BASE_URL=https://fapi.asterdex.com \
EXEC_ACTION=auth_check \
go run ./cmd/exec
```

Healthy agent output now includes:
- resolved auth summary (`auth_mode`, `auth_source`, `chain_id`, masked `user`, masked `signer`)
- route-by-route results
- `classification`
- `last_trace` with canonical `msg`, sent querystring, and signature mode when auth fails

Likely classifications:
- `signer_private_key_mismatch`
- `agent_not_authorized`
- `canonical_querystring_mismatch`
- `signature_encoding_mismatch`
- `route_not_supported`
- `legacy_hmac_path_selected_unexpectedly`

## Balance

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=balance go run ./cmd/exec
```

## Account summary

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=account EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
```

## Open orders

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=open_orders EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
```

## Position risk

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_ACTION=position EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
```

## Dry-run place

```bash
ASTER_CONFIG=~/.aster.yaml \
EXEC_ACTION=place \
EXEC_SYMBOL=ETHUSDT \
EXEC_SIDE=BUY \
EXEC_KIND=LIMIT \
EXEC_AT=mid \
EXEC_OFFSET_BPS=-2 \
EXEC_USD=80 \
DRY_RUN=1 \
EXEC_DEBUG=1 \
go run ./cmd/exec
```

## Real place

```bash
ASTER_CONFIG=~/.aster.yaml \
EXEC_ACTION=place \
EXEC_SYMBOL=ETHUSDT \
EXEC_SIDE=BUY \
EXEC_KIND=LIMIT \
EXEC_AT=mid \
EXEC_OFFSET_BPS=-2 \
EXEC_USD=80 \
DRY_RUN=0 \
EXEC_DEBUG=1 \
go run ./cmd/exec
```

## Notes

- `GET /fapi/v3/*` agent routes sign the canonical querystring as `Message.msg`.
- The exact canonical string signed is also the exact string sent to the server before appending `signature`.
- `cmd/exec` and `live-lite` both fail fast if agent mode is selected but required fields are missing or the private key does not derive to `ASTER_SIGNER`.
