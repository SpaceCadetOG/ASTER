# System Patch Checklist

## Now
- [x] Winner lifecycle stays in place
- [x] Governor stays tight
- [x] Liquidity-risk filter stays on
- [x] Do not loosen trade frequency yet

## Patch 1
- [x] Align `SIMPLE_DECISION`, `not_top_leader`, and final entry eligibility
- [x] Make repeated `allowed=1` candidates either promote or get one authoritative reject more often
- [x] Stop contradictory "allowed but not actionable" loops at the simple-entry source

## Patch 2
- [x] Add short TTL suppression for repeated unchanged candidate rejects
- [x] Suppress duplicate reevaluation for:
  - `weak_slope`
  - `spread_too_wide`
  - `extended`
  - `not_top_leader`
- [x] Re-open eligibility only on material state change

## Patch 3
- [x] Reduce signed REST pressure further
- [x] Cache account/open-order truth longer
- [x] Prefer WS-backed truth where possible
- [x] Keep cutting `429` and degraded-account cycles

## Validation
- [x] Fewer contradictory leader-vs-simple-entry paths in code
- [x] Targeted regression tests added for entry-stack alignment
- [x] Targeted regression tests added for signed-cache/backoff behavior
- [x] `go test ./internal/execution ./internal/executor ./cmd/live`
- [x] `go test ./...`
- [ ] Observe lower repeated `SIMPLE_DECISION` loops in the next session
- [ ] Observe fewer `allowed=1` followed by `not_top_leader` in the next session
- [ ] Observe fewer signed `429` bursts and degraded-account cycles in the next session

## Do Not Do Yet
- [x] Do not relax governor thresholds first
- [x] Do not widen entry frequency first
- [x] Do not retune stop logic first
