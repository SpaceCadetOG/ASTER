# CLI API

## Main commands

- `cmd/live-lite`: integrated paper/live-lite runtime and trade manager.
- `cmd/backtest`: historical simulation runner.
- `cmd/long`: long scanner + web API/UI.
- `cmd/short`: short scanner + web API/UI.
- `cmd/exec`: execution utility (balance/place/cancel/status/flatten).
- `cmd/oflow`, `cmd/tape`, `cmd/whale`, `cmd/liqs`: market microstructure sidecars.
- `cmd/vp`: VP inspection command.

## `cmd/live-lite` behavior

- Polls scanner candidates every `LIVE_SCAN_SEC`.
- Applies strategy ranking and no-trade gates.
- Applies risk shell hard gate.
- Executes via paper trader (`LIVE_DRY_RUN=1`) or REST execution manager.
- Sends Telegram digests/receipts and hosts status API.

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
