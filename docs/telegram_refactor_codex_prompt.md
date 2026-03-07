# Telegram Refactor Prompt (Codex-Ready)

Refactor the Telegram subsystem out of `cmd/live-lite/main.go` into `internal/notify/telegram.go`.

Important:
- Treat this as a refactor/design task, not a description of the current package layout.
- Preserve current runtime behavior unless explicitly improved.
- Keep existing Telegram command behavior working.

## Current State

Telegram is currently implemented inline inside `cmd/live-lite/main.go`.

Main pieces:
- `newTelegramSink()`
- `telegramSink.Sendf()`
- `telegramSink.send()`
- `telegramSink.sendToChat()`
- `telegramSink.getUpdates()`
- `telegramCommandCtx.run()`
- `telegramCommandCtx.handleCommand()`

Current config loading priority:
- bot token:
  - `LIVE_TG_BOT_TOKEN`
  - fallback `TELEGRAM_BOT_TOKEN`
- chat id:
  - `LIVE_TG_CHAT_ID`
  - fallback `TELEGRAM_CHAT_ID`

Current supporting config:
- `LIVE_TG_TIMEOUT_SEC`
- `LIVE_TG_DEDUPE_SEC`
- scheduling/report keys:
  - `LIVE_TG_HOURLY_ENABLE`
  - `LIVE_TG_HOURLY_TZ`
  - `LIVE_TG_DIGEST_MIN`
  - `LIVE_TG_TRADE_UPDATE_MIN`
  - `LIVE_TG_EOD_REPORT_HOUR`
  - `LIVE_TG_EOD_REPORT_MIN`
  - `LIVE_TG_SOD_REPORT_HOUR`
  - `LIVE_TG_SOD_REPORT_MIN`
  - `LIVE_TG_PRE_US_REPORT_HOUR`
  - `LIVE_TG_PRE_US_REPORT_MIN`
  - `LIVE_TG_DAILY_REPORT_DAY_OFFSET`
  - `LIVE_TG_DAILY_RECEIPT_ENABLE`
  - `LIVE_TG_DAILY_RECEIPT_LIMIT`
  - `LIVE_TG_DAILY_LIVE_RECEIPT_ENABLE`
  - `LIVE_TG_DAILY_LIVE_RECEIPT_LIMIT`
  - `LIVE_TG_FILL_RECEIPT_ENABLE`
  - `LIVE_TG_COMMANDS_ENABLE`

Current command behavior:
- `/help`
- `/start`
- `/status`
- `/balance`
- `/positions`
- `/pause`
- `/resume`
- `/close SYMBOL`
- `/closeall`

Current command handling facts:
- `/status` reads from `liveLiteStatusStore`
- `/pause` writes `LIVE_PAUSE_FILE`
- `/resume` removes `LIVE_PAUSE_FILE`
- `/close SYMBOL` calls `execMgr.ForceCloseSymbol(...)` and/or `paper.ForceCloseSymbol(...)`
- `/closeall` calls `execMgr.ForceCloseAll(...)` and `paper.ForceCloseAll(...)`
- command access is restricted to the configured Telegram chat ID
- updates are read via long-polling `getUpdates()`

Current message categories:
- startup / boot reconcile
- hourly digest / in-play digest
- SOD / EOD / Pre-US / maintenance window reports
- paper enter / exit / receipts
- live submit / fill / TP / SL / trail / force-close receipts
- payout notifications
- kill-switch / maintenance / safety notifications

## Refactor Goal

Move Telegram into a dedicated package:
- `internal/notify/telegram.go`

Introduce an interface so the rest of the bot does not depend on Telegram-specific code.

Suggested interface:

```go
package notify

type TradeEvent struct {
    Symbol     string
    Side       string
    Price      float64
    Confluence float64
    Setup      string
    Reason     string
}

type Service interface {
    Sendf(format string, args ...any)
    SendTrade(event TradeEvent, isEntry bool)
    Stop()
}
```

## Required Refactor

1. Extract Telegram sink
Move these responsibilities out of `cmd/live-lite/main.go`:
- config loading for Telegram credentials
- message sending
- dedupe behavior
- long polling for commands
- direct Telegram HTTP calls

2. Make delivery asynchronous
Current `send()` is synchronous HTTP.
Refactor to use a buffered channel and worker goroutine so trading logic does not block on Telegram API latency.

Suggested approach:
- `msgChan chan outboundMessage`
- worker goroutine drains the queue
- preserve dedupe behavior
- preserve timeout behavior
- support graceful shutdown

3. Preserve HTML preformatted messages
Current `tgPre()` wraps content in `<pre>...</pre>` and sends with `parse_mode=HTML`.
Retain equivalent behavior.

4. Separate transport from command execution
Refactor so Telegram package handles:
- polling
- chat filtering
- raw message receipt
- response sending

But main trading code still owns command semantics.

Suggested shape:
- `notify.Telegram.Listen(ctx, handler func(chatID string, text string) string)`

This allows:
- Telegram package to stay transport-focused
- trading engine to stay command/business-logic focused

5. Keep the current command set working
Do not regress:
- `/status`
- `/balance`
- `/positions`
- `/pause`
- `/resume`
- `/close SYMBOL`
- `/closeall`

6. Add richer trade notifications
Add `SendTrade(...)` helper for structured trade notifications.

Examples of new metadata to support:
- strategy/setup name
- confluence score
- reason/context string
- entry vs exit type

This will be used later for "Golden Loop" notifications.

Example output format:
- entry:
  - symbol
  - setup
  - price
  - confluence %
  - context
- exit:
  - symbol
  - reason
  - price
  - PnL context if available

## Message Trigger Map To Preserve

These existing call sites in `cmd/live-lite/main.go` should continue to emit notifications after refactor:
- startup message
- boot reconcile complete
- hourly digest
- SOD report
- EOD paper report
- daily receipts
- maintenance start / end
- maintenance hook complete / error
- paper enter / paper exit
- dry run intent
- order placed / order error
- live entry submitted / filled / timed out
- TP1 / TP2 / TP3 hit
- stop hit
- trail move
- forced close
- momentum exit
- pre-EOD exit
- payout messages
- orphan recovery / emergency stop attached

## Engineering Notes

- Preserve current dedupe semantics from `telegramSink.Sendf()`
- Preserve current chat ID enforcement
- Preserve current env/YAML config fallback behavior
- Add tests where practical for:
  - dedupe
  - async queue behavior
  - command dispatch
  - HTML preformatted formatting
  - disabled mode when token/chat ID missing

## Deliverable

Produce:
- `internal/notify/telegram.go`
- minimal integration changes in `cmd/live-lite/main.go`
- no behavior regressions in runtime messaging
- asynchronous delivery so Telegram latency does not stall execution
