# ASTER Codex Change Policy

Date: 2026-05-19

This document defines how future implementation requests must be classified before code changes are made.

## Change categories

Every future Codex task must identify which layer it touches:

- scanner/discovery
- features
- strategy/signal contract
- risk
- paper execution
- live execution
- protection/reconcile
- operator/Telegram/status
- persistence/reliability
- docs/tests only

If a request touches more than one layer, it must say which layer is primary and why.

## Allowed changes without architecture approval

- bug fixes
- tests
- documentation truth updates
- narrow risk hardening
- correctness-preserving seam extraction
- observability improvements

These changes should stay scoped to an identified debt, defect, or validation need.

## Changes requiring explicit approval

- scanner/ranking semantics
- changing the production topology
- reintroducing hard dependencies on standalone scanners
- modifying autonomous live activation behavior
- changing strategy ownership boundaries broadly
- deleting or bypassing paper/backtest parity paths
- large package moves or rewrites

If a request implies one of these, Codex should stop and request explicit approval before implementation.

## Required test and validation language

For any trading-behavior change, the request or implementation note must state:

- layer classification
- reason for change
- expected behavior delta
- test impact
- parity impact
- rollout and rollback consideration

Minimum expectation:

- what changes for scanner, strategy, risk, execution, or protection behavior
- what existing or new tests cover it
- whether backtest, paper, and live remain aligned or intentionally diverge

## Rule: no open-ended architecture work

No open-ended “improve the architecture” implementation work is allowed without a defined debt target.

Approved debt targets include:

- stale documentation
- unclear decision ownership
- backtest/paper/live parity gaps
- testability blockers
- correctness-critical seam extraction
- risk hardening with bounded scope

## Operational guidance

- Do not change runtime behavior when the task is documentation-only.
- Do not touch scanner/ranking semantics without explicit approval.
- Do not alter `manualOnlyScannerMode` or autonomous activation posture without an approved revalidation task.
- Prefer targeted corrections over broad rewrites.

## Decision standard

Future changes should be judged by whether they:

- improve correctness
- improve testing
- improve maintainability at an important seam
- improve backtest/paper/live parity
- preserve production runtime stability

If a proposed change does not clearly satisfy one of those goals, it should not proceed as implementation work.
