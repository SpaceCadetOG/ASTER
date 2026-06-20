# ASTER Architecture Governance Memo

Date: 2026-05-19

Decision status: Approved direction

## Context

ASTER started from a layered trading-system plan:

1. scanners / live feeds
2. TA / confluence engine
3. backtester
4. paper engine
5. live execution with risk management

The codebase then evolved through a real production history:

- standalone scanner and tooling surfaces
- a `live-lite` execution runtime
- operator-managed trading and Telegram controls
- a larger execution / protection / reconcile stack
- shared strategy, risk, and backtest layers
- `live-lite` renamed to `cmd/live`
- a self-contained `cmd/live` scanner/runtime path
- a current ground-zero / manual-only posture with autonomous entry suppressed

The project is no longer treated as an unfinished early-stage build. The current self-contained `cmd/live` architecture is accepted as the production truth, with targeted boundary restoration favored over rollback.

## Canonical architecture policy

1. `cmd/live` is the canonical production runtime.
   It is intentionally self-contained for live market fetch, scanner-worker passes, in-memory scanner snapshots, watch-set building, candidate selection, enrichment, strategy/risk routing, execution/protection orchestration, reporting, and operator surfaces.

2. `cmd/long` and `cmd/short` are standalone scanner / dashboard / diagnostic products.
   They are real tools and supported products, but they are not required upstream runtime dependencies for `cmd/live`.

3. The current correction strategy is Hybrid Correction, not rollback.
   Keep the self-contained runtime. Restore only the boundaries that improve correctness, testing, maintainability, and backtest/paper/live parity.

## What must be preserved

- `cmd/live` as the canonical production runtime
- self-contained scanning inside `cmd/live`
- the execution / protection stack
- hybrid stops, bracket placement, reconcile loops, protection logic, manual trade adoption, operator controls, and risk hardening
- the current ground-zero / manual-only suppression posture until revalidation is complete

## What must be corrected

- make documentation truthful about the current runtime topology
- identify and name the real shared decision contract
- restore only the most valuable boundaries for correctness and parity
- clarify active vs dormant behavior in the runtime story
- avoid future work that assumes a stale scanner-fed topology

## Active vs implemented

The repo contains autonomous paper/live entry codepaths, but those codepaths must not be described as current active production behavior.

Current active runtime posture:

- `cmd/live` is the active production runtime
- it self-scans and self-ranks
- it publishes status and operator surfaces
- autonomous entry is intentionally suppressed by the current manual-only / ground-zero posture

Implemented but not currently active production behavior:

- autonomous paper entry
- autonomous live entry
- broader paper/live parity restoration work

## Approved forward path

1. Make docs truthful.
2. Audit and name the real shared decision contract.
3. Restore intentional paper-auto wiring only after parity is understood.
4. Extract only correctness-critical seams.
5. Reopen automation in stages:
   manual-only -> paper-auto -> shadow live -> controlled live-auto

## Not doing

- no rollback to a hard scanner-fed process topology
- no requirement that `cmd/live` depend on `cmd/long` or `cmd/short`
- no broad architecture rewrite for its own sake
- no reactivation of autonomous trading by casually flipping mode flags
- no deletion of the execution / protection stack as if it were architectural error

## Governance rules for future changes

1. No architecture redesign without a defined debt target.
2. No scanner/ranking semantic changes without explicit approval.
3. No autonomous live reactivation until paper-auto is explicit, traceable, and test-backed.
4. Every trading-behavior change must identify its layer:
   scanner/discovery, features, strategy/signal contract, risk, paper execution, live execution, protection/reconcile, operator surface, persistence/reliability, or docs/tests only.
5. The current architecture documentation is the reference truth for future work, not stale scanner-fed diagrams.

## Architecture Decision

ASTER will move forward by keeping the current self-contained production runtime, preserving the hard-won execution/protection system, and restoring only the boundaries that improve correctness, testing, maintainability, and backtest/paper/live parity. Future architecture work is seam-cleaning, not rewinding.
