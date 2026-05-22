# Live Engine Recovery Handoff

## Purpose

This branch plugs the pre-ground-zero live entry engine back into the current
runtime start without discarding the current paper-runtime surface.

## Restored on current main

- Revert the May 12 scanner-only strip so the real live entry engine and
  execution governor replace their stubs.
- Revert the later operator-command removal so Telegram manage/trade handling is
  available again.
- Keep `manual_only` as the default mode.
- Route explicit `LIVE_RUNTIME_MODE=paper` to paper validation.
- Route explicit `LIVE_RUNTIME_MODE=live_auto` to the recovered live engine only
  when live flags are also active.
- Restore the safe, one-trade, balanced, and April small-account launch profiles.
- Add `systemd/env/live.recovery.env.example` as a small paper-first env surface.

## Success anchors used for comparison

| Commit | Commit time | Comparison role |
| --- | --- | --- |
| `05ffb16d` | `2026-03-11 14:30:20 -0500` | Early simpler live-lite/paper baseline. |
| `d95fab90` | `2026-03-27 13:17:29 -0500` | Late-March operator-positive window. |
| `118f971d` | `2026-04-01 18:13:27 -0500` | Strong early-April manual-import comparison window. |
| `dbce6284` | `2026-04-08 19:42:03 -0500` | Protection/manage comparison state before the April paper win window. |
| `e1ef6707` | `2026-04-26 19:17:01 -0500` | Late-April managed-stop safety state nearest the pre-collapse engine family. |

`c41137f0` is the restore boundary: it is the last pre-collapse engine commit
before `ccd5cc44` stripped auto-live down to scanner/manual mode.

## Start path

Paper recovery:

```bash
cp systemd/env/live.recovery.env.example /opt/aster/env/live.recovery.env
bash scripts/run_live_logged.sh --env /opt/aster/env/live.recovery.env
```

Small live recovery after paper comparison:

1. Change `LIVE_RUNTIME_MODE=live_auto`, `LIVE_DRY_RUN=0`, and
   `LIVE_ENABLE_LIVE_TRADING=1` in `/opt/aster/env/live.recovery.env`.
2. Start the guarded live profile:

```bash
bash scripts/run_live_safe_logged.sh --env /opt/aster/env/live.recovery.env
```

Do not expand the recovery env from the full `live.env.example` until a test or
log comparison justifies each extra knob.

## GCP next pass

- Deploy this branch to a non-production validation target first.
- Validate `/healthz`, `/api/status`, Telegram mode/status output, paper entries,
  live operator commands, and state persistence under `LIVE_STATE_DIR`.
- Compare decision logs against the five anchors by behavior class: entry quality,
  protection/exit handling, operator/manual import handling, sizing, and env
  complexity.
