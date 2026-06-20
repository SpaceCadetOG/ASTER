# Paper Runtime Validation Handoff

## Objective

Use the historical `live-lite` to `live` paper-runtime lineage as the source of truth for bringing the GCP runtime back to a stable autonomous paper-trading state.

This is not a live-trading task.

This is not a strategy redesign task.

This is not a paper-evidence run start.

The goal is to validate that GCP `cmd/live` behaves like the known-good historical paper runtime and that the read-only operator surfaces match that behavior closely enough to retire the Pi later.

## Historical Reference Commits

These commits define the paper/live-lite lineage that should guide validation:

1. `55e2f6b8` — `paper trader`
2. `0fbe2661` — `live-lite: add persistent live execution state machine, bracket exits, trailing stops, reconcile loop, kill-switch, and status API`
3. `804da3ca` — `live-lite: print paper active positions in account-style table`
4. `4e7b0391` — `live-lite: align paper/live cost model with aster docs`
5. `a1cb768f` — `live-lite: fix tp3 progression and improve telegram paper tables`
6. `c63ac0e6` — `Upgrade live-lite execution and paper exits`
7. `288c038b` — `Rename live-lite to live and add balance maintenance`

## Architectural Intent

Treat the current `cmd/live` binary as the successor to `live-lite`.

Paper validation should be framed as:

- paper engine enabled
- autonomous paper entries allowed
- paper lifecycle managed internally
- status API reflecting current runtime truth
- dashboard acting as a read-only operator surface

Do not introduce new operator/runtime concepts beyond what is required to restore that historical behavior.

## Guardrails

Do not:

- enable live trading
- enable `live`
- submit real orders
- add dashboard trade controls
- change scanner/ranking semantics
- change strategy behavior unless needed to restore historical paper runtime behavior
- start the official paper evidence run as part of this task
- sunset the Pi during this task

Keep:

- `LIVE_DRY_RUN=1`
- `LIVE_ENABLE_LIVE_TRADING=0`

## What Must Be Validated On GCP

### Runtime

On the Taiwan execution VM, validate that `cmd/live`:

- starts cleanly under systemd
- stays healthy over time
- makes autonomous paper decisions
- opens paper positions
- manages open paper positions
- evaluates and executes paper exits
- emits decision/open/close telemetry

### Status API

Validate that:

- `/healthz` returns OK
- `/api/status` returns valid JSON quickly
- status reports `dry_run=true`
- status reports `live_enabled=false`
- paper payload is present even when arrays are empty
- status does not block when paper state is empty or stale

### UI

On the dashboard path `ui/scanner-dashboard`, validate that the read-only operator UI shows:

- current paper positions
- recent paper closes
- recent paper decisions/rejects
- runtime mode and safety posture
- honest degraded or empty states when data is unavailable

The UI should resemble the historical operator/console view, not invent a new workflow.

## Required Runtime Settings

For safe GCP paper validation:

```env
LIVE_RUNTIME_MODE=paper
LIVE_DRY_RUN=1
LIVE_ENABLE_LIVE_TRADING=0
LIVE_PAPER_ENABLE=1
LIVE_SHOW_ACCOUNT=0
LIVE_USERDATA_STREAM_ENABLE=0
```

Note:

- internal runtime code now uses `paper`
- surfaced status/UI should be treated as `paper`
- no live execution path should be reachable

## Acceptance Criteria

This task is complete when all of the following are true:

1. `cmd/live` runs autonomously in paper mode on the execution VM.
2. Paper entries are actually opened without manual operator intervention.
3. Paper exits are actually closed by lifecycle logic.
4. `/api/status` returns quickly and includes paper runtime data.
5. The Cloud Run dashboard shows read-only paper positions, recent closes, and recent decisions.
6. No live trading is enabled.
7. No official paper evidence run is started during setup/repair.

## Pi Sunset Criteria

Do not sunset the Pi until the GCP runtime proves parity with the historical paper-runtime lineage.

The Pi can be retired only after:

1. Taiwan `cmd/live` has remained stable through a meaningful autonomous paper validation window.
2. Paper positions and closes behave correctly on GCP.
3. Status API and dashboard agree with runtime behavior.
4. Systemd-managed services recover cleanly across restart/reboot.
5. Archive/log flow is confirmed on GCP.
6. There is no remaining operational dependency on Pi env files, Pi launch behavior, or Pi-only runtime observations.

## Recommended Next Tasks

1. Pull the latest repo on the execution VM and restart `aster-live.service`.
2. Verify `paper` status fields and dashboard reconnect behavior.
3. Watch a full unattended paper session on GCP.
4. Compare runtime behavior against the historical live-lite lineage above.
5. Only after successful validation, open a separate decision on Pi shutdown timing.

## Summary For PM

Use the `live-lite` to `live` paper-runtime lineage as the acceptance baseline.

The next task is not “add more modes.”

The next task is:

- restore and validate the historical paper runtime behavior on GCP
- confirm the dashboard reflects it read-only
- keep the Pi alive until GCP proves parity
