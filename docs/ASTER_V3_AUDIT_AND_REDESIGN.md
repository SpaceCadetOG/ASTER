# ASTER Perps Bot: Quant Audit + Redesign Spec (V3)

Date: 2026-03-05
Timezone baseline: America/Chicago

## 1. Current System Overview

### Architecture (as implemented)
- Scanners:
  - `cmd/long`, `cmd/short`, in-play tracker state machine in `internal/inplay/tracker.go`.
- TA + confluence:
  - Feature engine in `internal/features/engine.go`.
  - VP engine in `internal/features/volume_profile.go`.
  - Institutional PA blocks in `internal/levels/*` and `internal/strategies/*`.
- Strategy routing:
  - Router and gating in `internal/strategies/router.go`.
  - Signal schema in `internal/strategies/types.go`.
- Paper/live execution:
  - Unified runtime in `cmd/live-lite/main.go`.
  - Exchange execution adapter in `adapters/aster/execution_rest.go`.
- Backtesting:
  - Engine in `internal/backtest/engine.go`.
  - CLI in `cmd/backtest/main.go`.
- Ops/reporting:
  - Maintenance windows, payout manager, state persistence, Telegram commands in `cmd/live-lite/main.go`.

### Evidence table
| Claim | Supporting file/metric | Confidence | Failure mode / caveat |
|---|---|---:|---|
| Session-tagged digest/status exists | `cmd/live-lite/main.go` (`sessionTag`, hourly digest output paths) | 0.95 | Formatting still dense in long tables |
| Pre-EOD sweep exists | `cmd/live-lite/main.go` (`ApplyPreEODExit`, `LIVE_PRE_EOD_EXIT_*`) | 0.95 | Can close good trades early if momentum proxy noisy |
| Daily payout flow exists with state machine | `cmd/live-lite/main.go` (`payoutManager`, states `IDLE/PENDING_CLOSE/DONE/DONE_FALLBACK`) | 0.95 | Live payout action is alert/debit flow, not on-chain transfer |
| Telegram commands exist | `cmd/live-lite/main.go` (`/help`, `/status`, `/balance`, `/positions`, `/pause`, `/resume`, `/close SYMBOL`, `/closeall`) | 0.95 | No auth layer beyond chat-id allowlist |
| Fill journaling exists | `cmd/live-lite/main.go` (`LIVE_TRADES_FILE`, logFill/sendFillReceipt) | 0.90 | Funding impact and modeled slippage not complete in live journals |
| Stale timer-exit architecture is absent | `rg` on `cmd/live-lite/main.go` shows no stale-exit path | 0.90 | Momentum/pre-EOD exits can still behave similarly if thresholds too tight |
| Backtest costs modeled partially | `internal/backtest/engine.go` has fee/slippage bps | 0.90 | Funding, liquidation distance, L2 depth slippage missing |
| Current backtests show weak edge on sample set | `out/backtests/summary.json` (e.g., PF < 1, negative Sharpe/AvgR) | 0.92 | Sample-specific; still a valid warning |

## 2. Working Components (with evidence)

1. Runtime controls and safety rails are materially improved.
- Evidence: maintenance windows + force-flat + warmup + reserve lock + cooldowns in `cmd/live-lite/main.go`.
- Why it works: deterministic scheduler + explicit entry gating + persistent state files (`out/paper_state.json`, `out/live_exec_state.json`, `out/payout_state.json`).

2. Paper/live operational parity improved at decision layer.
- Evidence: same candidate enrichment/router path in `enrichCandidate()` and shared signal/risk policy objects.
- Caveat: live fills and paper fills still differ in market microstructure realism.

3. VP helper primitives are usable for strategy logic.
- Evidence: `LevelAtHeaviestInRange`, `FirstSignificantOpposingLevel`, nearest HVN/LVN fields in `internal/features/volume_profile.go`.

4. Command + status surface is now serviceable.
- Evidence: Telegram handlers and `/api/status` endpoints in `cmd/live-lite/main.go`.

## 3. Broken Components (root cause + fix)

1. Edge quality is weak after current cost assumptions.
- Evidence: `out/backtests/summary.json` shows multiple symbols with `profit_factor < 1`, `avg_r < 0`, negative Sharpe.
- Root cause:
  - Overtrading frequency.
  - Simplified triggers (some strategies mostly candle heuristics).
  - No full funding/liquidation realism in backtest.
- Fix:
  - Add strict location+trigger+risk-shell handshake (hard fail if any missing).
  - Add per-hour cap/symbol lockout/cooldown cluster.
  - Disable strategies that fail OOS expectancy after costs.

2. Perps microstructure model is incomplete.
- Evidence:
  - Funding exists in live-paper accrual path but not fully in backtest PnL accounting (`internal/backtest/engine.go`).
  - No liquidation-distance rejection in entry pipeline.
  - Orderbook filter is entry-side only (`orderbookSupportsEntry`), no exit-quality gate.
- Root cause: strategy-first evolution, risk-shell not centralized.
- Fix: implement centralized `RiskShellApprove()` called by backtest and live-lite before any entry.

3. Session multiplier can distort signal quality.
- Evidence: `UseSessionRegimeRisk` + `SessionRiskMultiplier()` in `router.go`, `session_clock.go`, and scanner labels in `internal/sessions/sessions.go`.
- Root cause: score scaling by regime can promote weak raw setups in low-quality contexts.
- Fix: remove multiplicative boost from ranking path; keep regime as gate and risk-size modifier only.

4. Strategy implementations are uneven in depth vs book fidelity.
- Evidence:
  - Example simplistic logic: `internal/strategies/vwap_confluence.go`, `vp_accumulation.go`, `failed_auction_magnet.go`.
- Root cause: minimum viable heuristics vs full OF+VP+VWAP confirmation stack.
- Fix: formalize each strategy with required context/location/trigger/invalidation and enforce all fields.

## 4. Missing Modules (P0/P1/P2)

### P0
1. Central perps risk shell API:
- `liq_distance >= 2.5 * stop_distance` reject.
- Funding carry projection and reject/reduce logic.
- Spread/depth/slippage hard gates.

2. Backtest realism parity:
- Funding cashflows by hold intervals.
- Slippage model by liquidity tier/session/regime.
- Monte Carlo perturbation (spread/slippage/latency/partial fill).

3. No-trade gate audit trail:
- Persist gate reason at candidate rejection (for paper/live and backtest).

### P1
1. Order-flow confirmation engine hardening:
- Cumulative delta divergence and absorption with deterministic thresholds.
- OI change confirmation where feed exists.

2. Strategy governance:
- Strategy-level disable flags based on rolling expectancy decay.

### P2
1. Hyperliquid portability adapter layer.
2. Dashboard/UI (real-time feed via websocket from `/api/status`).

## 5. Redesigned Architecture (Current -> Target Mapping)

1. Data Layer
- Current: `adapters/aster/*`, `internal/features/*` consume OHLCV + whales.
- Target add:
  - funding history cache service (`internal/data/funding_store.go`)
  - liquidity tier classifier (`internal/market/liquidity_tier.go`)
  - optional OI/liquidation adapters (`adapters/aster/oi.go`, `adapters/aster/liquidations.go`).

2. Regime Engine
- Current: `internal/data/session_clock.go` + in-play states.
- Target add:
  - `internal/regime/classifier.go` with trend/balanced/chop/vol-expansion/post-liq tags.

3. VP Engine
- Current: good base in `internal/features/volume_profile.go`.
- Target add:
  - session-profile and event-leg profile APIs with deterministic significance filters.

4. VWAP Engine
- Current: rolling VWAP helpers exist, strategy uses ad-hoc calculation.
- Target add:
  - anchored VWAP service (`internal/indicators/anchored_vwap.go`) with anchor registry.

5. OF Engine
- Current: whales + score slope proxies.
- Target add:
  - explicit OF signals struct (`internal/flow/signals.go`): absorption, aggression flip, delta div, cluster score.

6. Strategy Engine
- Current: router evaluates mixed strategies.
- Target add:
  - strict signal contract: `location_ok && regime_ok && trigger_ok && risk_ok`.
  - reject reasons must be explicit and logged.

7. Risk Shell
- Current: spread/orderbook entry filter + reserve gates + cooldowns.
- Target add:
  - unified risk shell package used by backtest and live-lite.

8. Execution/Trade Mgmt
- Current: stops/TP/trailing/BE implemented, reconciliation exists.
- Target add:
  - stop confirmation ack timeout.
  - exit quality filter for illiquid slippage spikes.

9. Backtest/Validation
- Current: single-path event loop, fixed bps fee/slippage.
- Target add:
  - cost stack parity + walk-forward + MC stress + per-bucket reporting.

## 6. Strategy Implementation Spec (All listed strategies)

Threshold defaults (tunable):
- `MinConfluenceScore=0.62`
- `RiskPerTrade=0.75% equity`
- `MaxDailyLoss=3R`
- `MaxConsecutiveLoss=4`
- `PerHourEntryCap=2`
- `SymbolStopoutLock=45m after 2 SL in 90m`

1. OF Volume Clusters
- Context: trend or balanced; liquidity tier != low unless A+.
- Location: repeated high executed-volume zone.
- Trigger: absorption + aggression flip at zone.
- Invalidation: accepted close beyond zone + OI confirms continuation.
- Exits: 1R / 2R / 3R + runner trail.
- Perps gates: funding against hold horizon -> reduce size/skip.

2. OF Multi-HVN participation
- Context: rotational regime.
- Location: between adjacent HVNs in same profile.
- Trigger: edge rejection with delta flip.
- Invalidation: acceptance outside lane.
- Exits: mean/POC then opposite HVN.

3. OF trade-size/speed filter
- Context: any, but required for entries in low-liq symbols.
- Trigger: large-print rate > threshold and directional dominance.
- Invalidation: flow normalization.

4. OF stacked imbalance
- Context: breakout/retest continuation.
- Trigger: multi-bar one-sided delta with price acceptance.
- Invalidation: delta divergence + reclaim failure.

5. OF unfinished business
- Context: impulse leaving low-participation zone.
- Trigger: revisit + absorption.
- Invalidation: fast acceptance through inefficiency.

6. VP balanced rotation
- Context: D-profile/balanced shape.
- Location: VAH/VAL extremes.
- Trigger: OF rejection.
- Invalidation: acceptance outside value.
- Exits: POC then opposite VA edge.

7. VP rejection-leg retest
- Context: strong rejection leg.
- Location: heaviest event-leg volume.
- Trigger: first retest + OF confirmation.
- Invalidation: decisive breach of rejection extreme.

8. VP HVN edge defense
- Context: accepted value node.
- Location: HVN edge.
- Trigger: defense prints + non-expansion.
- Invalidation: clean acceptance through edge.

9. VP LVN break/flip
- Context: trend expansion.
- Location: LVN breach/retest.
- Trigger: stacked imbalance on break, then retest hold.
- Invalidation: re-accept inside prior value.

10. VWAP pullback continuation
- Context: trending + sloped VWAP.
- Location: daily/session/anchored VWAP pullback.
- Trigger: counterflow absorption then reclaim.
- Invalidation: VWAP role failure.

11. VWAP band mean reversion
- Context: flat VWAP + balanced VP.
- Location: ±1 band extremes.
- Trigger: fade confirmation.
- Invalidation: band walk continuation.

12. Anchored VWAP event retests
- Context: breakout, liquidation cascade, trend origin.
- Location: anchored VWAP retest.
- Trigger: alignment with OF and VP node.
- Invalidation: sustained acceptance wrong side.

## 7. Risk Management Framework

Mandatory hard rejects:
1. Liquidation buffer:
- Compute approximate liquidation distance from leverage/margin model.
- Reject if `liq_distance < 2.5 * stop_distance`.

2. Funding filter:
- Reject when projected funding carry over expected hold > 0.25R.

3. Spread/depth:
- Reject if spread > `maxSpreadBpsByTier[session]` or depth imbalance below threshold.

4. Venue health:
- Reject on timeout/retry degradation and stale websocket/orderbook.

5. Slippage anomaly:
- Reject symbol for 30m if rolling realized slippage > 2x modeled.

Position/risk controls:
- Risk per trade `0.75%` (range 0.5%-1%).
- Max concurrent = min(config, exposure cap).
- Exposure cap by correlated group.
- Daily stop = `-3R`; streak brake = 4 losses.

## 8. Implementation Roadmap (Fixed Owner Phases)

1. Scanners
- Add explicit OF state features and liquidity-tier tags in scanner output.
- Keep current state labels; add confidence decomposition fields.

2. TA & Confluence
- Enforce strategy contract (`location/regime/trigger/risk`).
- Remove session multiplier from score ranking path; keep gating only.
- Keep current router/pivot baselines otherwise unchanged.

3. Backtester
- Add funding + liquidation checks + session/tier slippage model.
- Add ablation runner and OOS split mode.
- Add regime/session/liquidity breakdown in artifacts.

4. Paper Engine
- Replay same risk shell and cost model as backtest/live.
- Journal funding impact and slippage estimate per trade row.

5. Live Execution
- Centralize risk-shell checks pre-order.
- Add exit quality filter and post-fill slippage monitor.
- Keep existing maintenance/payout/telegram operations.

## 9. Validation / Acceptance Tests (Pass/Fail)

1. Session boundary tests (Chicago + NY17 anchor): pass existing + add weekend edge cases.
2. Strategy determinism: same candles => same signal/reject reason.
3. Risk-shell reject tests:
- liq buffer violation rejects.
- funding carry violation rejects.
- spread/depth violation rejects.
4. Bracket geometry tests:
- side-correct TP/SL, no invalid/negative prices.
5. Cost accounting tests:
- fee + funding + slippage included and reconciled.
6. Paper/live parity tests:
- same snapshot produces same decision + reason.
7. Restart/reconcile idempotency:
- no duplicate payout/trade journal events after restart.
8. Telegram formatting tests:
- digest and receipt fields include realized/open/net and per-trade reason.

## 10. Deployment Gates + Rollback Policy

### Gate thresholds
1. Backtest gate:
- OOS expectancy > 0.
- Profit factor >= 1.15.
- Max drawdown <= 12%.
- At least 3 regimes and 2 liquidity tiers profitable net costs.

2. Paper gate (7-14 days):
- Net positive after modeled fees/funding/slippage.
- No unresolved reconciliation drift.
- Fill/journal completeness >= 99%.

3. Shadow-live gate:
- Decision parity with paper >= 98% over same window.
- Slippage forecast error within tolerance.

4. Live gate:
- Start at reduced size (25%-40% nominal), auto-scale by rolling Sharpe and drawdown.

### Rollback triggers
- Daily loss breach, streak breach, health degradation, or model drift.
- Action: force-flat + disable entries + send Telegram high-priority alert + revert to last stable config snapshot.

---

## Immediate Code-Level Priorities (next commit set)

1. Add `internal/risk/shell.go` and call it from:
- `cmd/live-lite/main.go` before `PlaceOrder`
- `internal/backtest/engine.go` before scheduling entries

2. Remove score multiplier influence while retaining session tags:
- `internal/strategies/router.go` (`UseSessionRegimeRisk` should not scale candidate score)
- `internal/sessions/sessions.go` (`SCAN_MULT` label can be retired or set fixed 1.00x)

3. Upgrade backtest realism:
- `internal/backtest/engine.go` add funding and liq buffer checks
- emit per-trade `funding_cost` and `liq_buffer_ok`

4. Tighten anti-overtrading controls:
- per-hour cap and stopout lock in candidate acceptance path in `cmd/live-lite/main.go`.

5. Keep unchanged by requirement:
- no stale timer-exit architecture
- no new maintenance-regime router behavior beyond baseline
- no new pivot/swing behavior changes beyond baseline
