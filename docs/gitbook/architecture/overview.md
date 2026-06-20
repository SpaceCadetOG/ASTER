# Architecture Overview

## System map

```mermaid
flowchart LR
  A["Aster market data"] --> B["cmd/live scanner worker"]
  B --> C["In-memory scanner snapshot / watch set"]
  C --> D["Candidate selection / enrichment"]
  D --> E["Strategy router"]
  E --> F["Risk shell"]
  F --> G["Paper/live execution machinery"]
  G --> H["Protection / reconcile / exits"]
  H --> I["Status / Telegram / persistence"]
  J["cmd/long"] --> K["Standalone scanner / dashboard / diagnostics"]
  L["cmd/short"] --> M["Standalone scanner / dashboard / diagnostics"]
  N["cmd/backtest"] --> O["Backtest engine"]
  O --> P["out/backtests/*"]
```

## Major modules

- `cmd/live`: the production runtime. It owns scanning intake, candidate
  selection, paper/live execution machinery, manual trade adoption, Telegram
  controls, status serving, and perp balance maintenance.
- `cmd/long`, `cmd/short`: standalone ranking scanners and JSON/HTML surfaces.
- `adapters/aster`: exchange integration, balances, positions, leverage,
  order placement, cancel/replace, and signed account actions.
- `internal/features`: snapshot generation used by scanners and live.
- `internal/strategies`: setup scoring, persistence, continuation, and
  directional opportunity shaping.
- `internal/risk`: the hard gate before any live order reaches the exchange.
- `internal/notify`: Telegram-facing formatting for status, fills, digests,
  scanner snapshots, and operator commands.
- `internal/backtest`: event-loop simulation and report generation.

## How the live system works

### 1. `cmd/live` ranks the market

`cmd/live` fetches markets directly, runs its own scanner worker, computes its
own long/short rankings, and maintains in-memory scanner state for downstream
runtime decisions.

`cmd/long` and `cmd/short` remain useful standalone scanner/dashboard products,
but they are not required upstream runtime dependencies for `cmd/live`.

### 2. `cmd/live` chooses and enriches candidates

`cmd/live` filters candidates through:
- grade and score thresholds
- persistence and reject memory
- stop geometry and expected hold quality
- risk shell checks
- available margin, open-position limits, and symbol cooldowns

In the current architecture, scanner state, watch-set construction, candidate
selection, enrichment, and strategy/risk routing all happen inside the
canonical `cmd/live` runtime.

The current operating model is fixed-size and no-add by default:
- starter size: `$50`
- re-entry size: `$50`
- no pyramiding
- max concurrent positions: `4`
- max per side: `4`

### 3. Execution and protection are preserved

Bot-native trades start from configured leverage and can step down if exchange
or risk constraints reject the requested leverage. Live exits are reduce-only
limit orders. Protection is stop-based, with trailing and profit-lock logic on
top.

Key current behavior:
- profit protection arms once the trade is clearly working
- runners now hold through healthier pullbacks
- exits rely more on real deterioration than on mild cooling

The execution/protection stack is part of the accepted production architecture,
not a mistake to be rolled back.

### 4. Manual trades can be adopted

Manual trades detected on exchange are imported first. When the operator sends
`/manage SYMBOL y`, the bot:
- adopts the existing live position
- promotes it into a bot-managed manual state
- aligns leverage toward the configured live leverage
- forces the first protection attach immediately
- continues retry/backoff only if the exchange rejects the stop

The intended standard is:
- `/manage SYMBOL y` means the bot owns the trade
- if the stop is legal, a real stop should appear on exchange immediately

### 5. Telegram and status are the operator surfaces

Telegram is used for:
- `/status`, `/balance`, `/positions`
- `/manage SYMBOL y|n`
- `/protect SYMBOL`
- `/scanner`, `/longs`, `/shorts`
- pause/resume and close controls

Some operator commands related to autonomous trading are intentionally
restricted by the current ground-zero / manual-only posture.

### 6. Funds are maintained automatically

The live runtime can maintain the perp account on a clock:
- target perp balance: `$200`
- floor: `$150`
- funds check: every `60s`

Behavior:
- if perp available is above target, sweep the excess to spot
- if perp available is below floor, top back up toward target

## Runtime posture

- `cmd/live` is the canonical production runtime.
- `cmd/long` and `cmd/short` are standalone scanner / dashboard / diagnostic
  products.
- Autonomous entry logic exists in the codebase, but the current active
  production posture is manual-only / ground-zero mode.
- Re-enabling autonomous paper/live entry is a future staged revalidation task,
  not current production behavior.

## Runtime invariants

- Timezone baseline: `America/Chicago` for operations and reporting.
- State is persisted under `out/` locally or `LIVE_STATE_DIR` on a dedicated host.
- The repo no longer ships a prescribed tmux/systemd orchestration layer; run
  commands directly or through your own host/container supervisor.
- Logging is environment-driven via `ASTER_LOG_DIR`.
