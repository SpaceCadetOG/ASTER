# Telegram Command API

Source: `cmd/live/main.go` (`handleCommand`)

## Supported commands

- `/help` or `/start`
  - Show command guide.
- `/status`
  - Runtime snapshot: mode, top candidate, in-play counts, execution state.
- `/balance`
  - Live account balances (all assets), available USDT, equity view, open positions.
- `/positions`
  - Open positions summary.
  - In dry-run, returns paper positions table.
- `/pause`
  - Create pause file and block new entries.
- `/resume`
  - Remove pause file and resume entries.
- `/close SYMBOL`
  - Force close one symbol (live + paper paths).
- `/closeall`
  - Force close all open positions (live + paper paths).

## Delivery behavior

- Telegram messages are deduplicated by message text over `LIVE_TG_DEDUPE_SEC`.
- Preformatted messages use HTML `<pre>` payloads.
- Commands are filtered by configured chat id if set.
