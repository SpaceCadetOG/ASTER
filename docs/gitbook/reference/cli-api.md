# CLI API

## Main commands

- `cmd/live`: integrated paper/live runtime and trade manager.
- `cmd/backtest`: historical simulation runner.
- `cmd/long`: long scanner + web API/UI.
- `cmd/short`: short scanner + web API/UI.
- `cmd/exec`: execution utility (balance/place/cancel/status/flatten).
- `cmd/oflow`, `cmd/tape`, `cmd/whale`, `cmd/liqs`: market microstructure sidecars.
- `cmd/vp`: VP inspection command.
- `cmd/stats`: JSONL event-log performance aggregation.

## `cmd/live` behavior

- Polls scanner candidates every `LIVE_SCAN_SEC`.
- Applies strategy ranking and no-trade gates.
- Applies risk shell hard gate.
- Executes via paper trader (`LIVE_DRY_RUN=1`) or REST execution manager.
- Sends Telegram digests/receipts and hosts status API.
- Emits structured event logs (`SIGNAL`, `GATE_DECISION`, `INTENT`, `ORDER_*`, `POSITION_*`, `METRICS_SNAPSHOT`).

## `cmd/stats` usage

- `go run ./cmd/stats -log logs/events.jsonl -from 2026-03-01 -to 2026-03-05`
- Optional: `-csv out/stats_report.csv`

## `cmd/exec` actions

Action chosen by `EXEC_ACTION` env:
- `auth_check`
- `balance`
- `orderbook`
- `quote`
- `place`
- `cancel`
- `status`
- `flatten`

See existing deep examples in [`docs/execution_guide.md`](../execution_guide.md).
