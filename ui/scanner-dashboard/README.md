# ASTER Scanner Dashboard (Next.js)

Tabbed browser UI for existing scanner stack, without rewriting trading logic.

## Tabs
- Overview
- In-Play
- Long Confluence
- Short Confluence
- Asset Detail

All asset rows are clickable and open detail context.

## Data Sources
- `SCANNER_LONG_URL` (default `http://127.0.0.1:8080`)
  - expects `/api/status` and `/api/confluence`
- `SCANNER_SHORT_URL` (default `http://127.0.0.1:8081`)
  - expects `/api/status` and `/api/confluence`
- `SCANNER_LIVE_URL` (default `http://127.0.0.1:8787`)
  - expects `/api/status`
- `SCANNER_OFLOW_URL` (default `http://127.0.0.1:8090`)
  - expects `/api/status`
- `SCANNER_TAPE_URL` (default `http://127.0.0.1:8091`)
  - expects `/api/status`
- `SCANNER_WHALE_URL` (default `http://127.0.0.1:8092`)
  - expects `/api/status`
- `SCANNER_LIQS_URL` (default `http://127.0.0.1:8093`)
  - expects `/api/status`

`SCANNER_USE_MOCK=true` forces mock data.

## Run
```bash
cd /Users/victorogbebor/2026/go-machine/ui/scanner-dashboard
npm install
npm run dev
```

Open: `http://localhost:3000`

## Cloud Run Notes

The dashboard is currently intended for a read-only Cloud Run deploy.

Safest first deploy:

- set `SCANNER_USE_MOCK=true`
- do not inject exchange credentials
- do not expose execution controls

Cloud Run private backend access to the Go APIs is a later deployment step.

## Module Status Ports
- `cmd/oflow` -> `OFLOW_HTTP_ADDR` (default `:8090`)
- `cmd/tape` -> `TAPE_HTTP_ADDR` (default `:8091`)
- `cmd/whale` -> `WHALE_HTTP_ADDR` (default `:8092`)
- `cmd/liqs` -> `LIQS_HTTP_ADDR` (default `:8093`)
