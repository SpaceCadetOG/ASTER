# Data Layer Service

`cmd/datalayer` is a read-only internal Go service that exposes normalized market, account, and flow data for the bot and dashboard.

## Architecture

- `cmd/datalayer`
  - bootstrap, env parsing, graceful shutdown
- `internal/datalayer/httpapi`
  - HTTP routing and request parsing
- `internal/datalayer/service`
  - runtime orchestration, aggregation, normalization
- `internal/datalayer/cache`
  - in-memory latest snapshots, TTL entries, recent-event rings
- `internal/datalayer/types`
  - stable JSON payload shapes

## Runtime Model

- Prices:
  - broad market coverage comes from periodic Aster REST refreshes
  - tracked symbols are enriched with live `bookTicker` and shallow book state from websocket streams
- Orderbook:
  - prefers in-memory stream state for tracked symbols
  - falls back to REST depth snapshots
- Candles:
  - fetched on demand through existing Aster candle loaders
  - cached by `symbol + timeframe + limit`
- Account:
  - prefers `adapters/aster/user_stream.go` in-memory state when fresh
  - falls back to signed REST account, balance, position, and open-order calls
- Fills:
  - pulled from signed `userTrades` REST calls for tracked symbols
- Liquidations:
  - consumes the Aster `!forceOrder@arr` liquidation feed
- Whales and Orderflow:
  - consume tracked-symbol `aggTrade` streams
  - use rolling `internal/flow.Window` summaries plus recent-event buffers

The service is best-effort by design. If a feed is unavailable, endpoints stay up and return partial data with `meta.partial`, `meta.stale`, and `meta.error` when appropriate.

## Endpoints

- `GET /health`
- `GET /api/prices`
- `GET /api/price/{symbol}`
- `GET /api/orderbook/{symbol}`
- `GET /api/candles/{symbol}?tf=1m&limit=200`
- `GET /api/account`
- `GET /api/account/positions`
- `GET /api/fills?symbol=&limit=`
- `GET /api/liquidations`
- `GET /api/whales`
- `GET /api/orderflow/{symbol}`

Responses use repo-native normalized symbols like `BTC-USD`, while path and query inputs accept raw exchange symbols such as `BTCUSDT`.

## Env Vars

- `DATALAYER_ADDR`
  - default: `:8095`
- `DATALAYER_SYMBOLS`
  - default: `BTCUSDT,ETHUSDT,SOLUSDT`
- `DATALAYER_PRICE_REFRESH_SEC`
  - default: `15`
- `DATALAYER_CANDLE_TTL_SEC`
  - default: `15`
- `DATALAYER_ACCOUNT_REFRESH_SEC`
  - default: `15`
- `DATALAYER_USERDATA_MAX_STALE_SEC`
  - default: `120`
- `DATALAYER_MARKET_LEVELS`
  - default: `20`
- `DATALAYER_MARKET_SPEED`
  - default: `100ms`
- `DATALAYER_MARKET_TRADES`
  - default: `50`
- `DATALAYER_EVENT_BUFFER`
  - default: `50`
- `DATALAYER_WHALE_MIN_USD`
  - default: `500`
- `DATALAYER_LIQ_MIN_USD`
  - default: `500`
- `DATALAYER_OFLOW_LARGE_USD`
  - default: `50`
- `DATALAYER_WHALE_WINDOW_SEC`
  - default: `30`
- `DATALAYER_LIQ_WINDOW_SEC`
  - default: `60`
- `DATALAYER_OFLOW_WINDOW_SEC`
  - default: `20`
- `DATALAYER_ORDERBOOK_LIMIT`
  - default: `20`
- `DATALAYER_CANDLE_LIMIT`
  - default: `200`

Auth is optional. If account endpoints should be populated, provide either the repo’s existing HMAC vars or agent-wallet vars:

- `ASTER_API_KEY`
- `ASTER_API_SECRET`
- `ASTER_AUTH_MODE`
- `ASTER_USER`
- `ASTER_SIGNER`
- `ASTER_PRIVATE_KEY`
- `ASTER_CHAIN_ID`
- `ASTER_BASE_URL`

## Run

```bash
go run ./cmd/datalayer
```

## Curl Examples

```bash
curl -s http://127.0.0.1:8095/health | jq
curl -s http://127.0.0.1:8095/api/prices | jq
curl -s http://127.0.0.1:8095/api/price/BTCUSDT | jq
curl -s "http://127.0.0.1:8095/api/candles/BTCUSDT?tf=1m&limit=50" | jq
curl -s http://127.0.0.1:8095/api/account | jq
curl -s "http://127.0.0.1:8095/api/fills?symbol=BTCUSDT&limit=20" | jq
curl -s http://127.0.0.1:8095/api/liquidations | jq
curl -s http://127.0.0.1:8095/api/orderflow/BTCUSDT | jq
```
