# ASTER Frontend UI

Standalone Next.js frontend for ASTER scanners under [`/Users/victorogbebor/2026/go-machine/frontend/ui`](/Users/victorogbebor/2026/go-machine/frontend/ui).

## What It Preserves

- Long scanner behavior from `cmd/long`
- Short scanner behavior from `cmd/short`
- Scanner rows driven by `/api/status`
- Token detail analytics driven by existing scanner APIs, not embedded HTML
- Live mode context from `cmd/live` status

## What It Adds

- Independent frontend module that can run separately from Go binaries
- Dedicated routes for long scanner, short scanner, and token drilldown
- Mobile-friendly layout
- Token drilldown page with:
  - scanner context
  - long confluence
  - short confluence
  - fusion/structure/patterns/volstats raw backend views
  - `tape` / `whale` / `liqs` / `oflow` module linkage cards
- Trade execution panel with explicit backend contract gap documentation

## Env Contract

Copy `.env.example` to `.env.local` and set:

```bash
SCANNER_LONG_URL=http://127.0.0.1:8080
SCANNER_SHORT_URL=http://127.0.0.1:8081
OFLOW_URL=http://127.0.0.1:8090
TAPE_URL=http://127.0.0.1:8091
WHALE_URL=http://127.0.0.1:8092
LIQS_URL=http://127.0.0.1:8093
LIVE_URL=http://127.0.0.1:8787
```

The frontend also accepts legacy fallback names:

- `SCANNER_OFLOW_URL`
- `SCANNER_TAPE_URL`
- `SCANNER_WHALE_URL`
- `SCANNER_LIQS_URL`
- `SCANNER_LIVE_URL`

## Backend Endpoint Map Used By The Frontend

Long scanner:

- `GET /api/status`
- `GET /api/candles`
- `GET /api/confluence`
- `GET /api/fusion`
- `GET /api/structure`
- `GET /api/patterns`
- `GET /api/volstats`

Short scanner:

- `GET /api/status`
- `GET /api/confluence`

Module services:

- `GET /api/status` from `oflow`, `tape`, `whale`, `liqs`

Live service:

- `GET /api/status`

## Local Run

Start backend services in separate terminals:

```bash
go run ./cmd/long
go run ./cmd/short
go run ./cmd/oflow
go run ./cmd/tape
go run ./cmd/whale
go run ./cmd/liqs
LIVE_DRY_RUN=1 go run ./cmd/live
```

Then run the frontend:

```bash
cd /Users/victorogbebor/2026/go-machine/frontend/ui
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000).

## Known Backend Gaps

- `cmd/live` exposes status only. There is no real HTTP execution endpoint to wire the trade panel.
- `cmd/oflow`, `cmd/tape`, `cmd/whale`, and `cmd/liqs` expose module-level `/api/status` only. There are no per-token analysis endpoints yet.

## Suggested Next Backend Additions

1. `POST /api/trades` in `cmd/live` or a dedicated execution service.
2. `GET /api/token/:symbol` or equivalent per-token endpoints for `oflow`, `tape`, `whale`, and `liqs`.
3. Shared API schema docs so frontend and backend can evolve independently.
