# HTTP API

## Scanner APIs (`cmd/long`, `cmd/short`)

Default ports:
- long: `:8080`
- short: `:8081`

Endpoints:
- `GET /` HTML dashboard
- `GET /status` scanner status HTML
- `GET /api/inplay` JSON in-play list
- `GET /health` health JSON
- `GET /api/candles`
- `GET /api/pivots`
- `GET /api/structure`
- `GET /api/patterns`
- `GET /api/volstats`
- `GET /api/confluence`
- `GET /api/fusion`

Typical query params:
- `symbol` (e.g. `BTCUSDT`)
- `tf` (e.g. `5m`)
- `n`, plus endpoint-specific tuning params.

## Live-lite status API (`cmd/live-lite`)

Default bind:
- `LIVE_STATUS_ADDR=:8787`

Endpoints:
- `GET /healthz` -> `{ "ok": true }`
- `GET /api/status` -> JSON runtime snapshot
- `GET /status` -> plain-text snapshot

`/api/status` includes:
- mode flags (`dry_run`, `live_enabled`)
- top candidate metadata and reject reason
- in-play counts
- execution snapshot (open/pending/partial)
- payout cycle metadata

