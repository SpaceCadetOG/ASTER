# ASTER Unified Operator Portal

`ui/scanner-dashboard` is the only ASTER frontend app and the canonical Cloud Run UI.

It preserves the existing dark operator shell while adding richer scanner storytelling, live hotlist drilldown, runtime status, paper placeholders, asset detail, and backend health views.

## Product Areas
- Overview
- Scanners
- Live Hotlist / In-Play
- Runtime
- Paper
- Asset Detail
- Health

All browser-side calls stay behind Next API routes so the client does not call private backend URLs directly.

## Data Sources
- `SCANNER_LONG_URL` (default `http://127.0.0.1:8080`)
  - expects `/api/status` and `/api/confluence`
- `SCANNER_SHORT_URL` (default `http://127.0.0.1:8081`)
  - expects `/api/status` and `/api/confluence`
- `SCANNER_LIVE_URL` (default `http://127.0.0.1:8787`)
  - expects `/api/status`
- `SCANNER_OFLOW_URL` or `OFLOW_URL` (default `http://127.0.0.1:8090`)
  - expects `/api/status`, optionally `/api/asset?symbol=...`
- `SCANNER_TAPE_URL` or `TAPE_URL` (default `http://127.0.0.1:8091`)
  - expects `/api/status`, optionally `/api/asset?symbol=...`
- `SCANNER_WHALE_URL` or `WHALE_URL` (default `http://127.0.0.1:8092`)
  - expects `/api/status`, optionally `/api/asset?symbol=...`
- `SCANNER_LIQS_URL` or `LIQS_URL` (default `http://127.0.0.1:8093`)
  - expects `/api/status`, optionally `/api/asset?symbol=...`

Production behavior is real data only. If an upstream backend is unavailable, the UI shows explicit unavailable, stale, or disconnected state and does not invent rows.

## Run
```bash
cd /Users/victorogbebor/2026/go-machine/ui/scanner-dashboard
npm install
npm run dev
```

Open: `http://localhost:3000`

## Cloud Run Notes

The Cloud Run service name remains `scanner-dashboard`.

This frontend is intentionally read-only:

- do not expose execution controls
- do not inject exchange credentials into the browser
- do not add live enable, submit, close, pause, or resume buttons

Cloud Run private backend access to the Go APIs is a later deployment step.

## Module Status Ports
- `cmd/oflow` -> `OFLOW_HTTP_ADDR` (default `:8090`)
- `cmd/tape` -> `TAPE_HTTP_ADDR` (default `:8091`)
- `cmd/whale` -> `WHALE_HTTP_ADDR` (default `:8092`)
- `cmd/liqs` -> `LIQS_HTTP_ADDR` (default `:8093`)
