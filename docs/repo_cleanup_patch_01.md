# Repo Cleanup Patch 01

Date: 2026-05-18

## Summary

This patch removes obsolete/generated/superseded repo material ahead of the GCP migration while preserving the current core runtime and the command set explicitly approved to keep.

Notably preserved on purpose:

- `cmd/live`
- `cmd/long`
- `cmd/short`
- `cmd/tape`
- `cmd/whale`
- `cmd/liqs`
- `cmd/oflow`
- `cmd/exec`
- `cmd/stats`
- `cmd/backtest`
- `cmd/manualreplay`
- `cmd/vp`
- `cmd/rr`
- `internal/tools` because `cmd/rr` depends on it

## Files Removed

### Generated / stale artifacts

- `live`
- `.DS_Store`
- `docs/gitbook/.DS_Store`
- `codebase.txt`

### Commands / packages removed

- `cmd/bt-fetch/`
- `cmd/lat-ch1/`
- `engine/`

### Scripts removed

- `scripts/start_tmux_modules.sh`
- `scripts/tmux_module_runner.sh`
- `scripts/auto_update_aster.sh`
- `scripts/run_live_one_trade_logged.sh`
- `scripts/audit_live.sh`
- `scripts/generate_agent_handoff.sh`
- `scripts/run_live_safe_logged.sh`
- `scripts/run_live_balanced_logged.sh`

### systemd / env artifacts removed

- `systemd/aster-modules-tmux.service`
- `systemd/aster-autoupdate.service`
- `systemd/aster-autoupdate.timer`
- `systemd/aster-live.service`
- `systemd/aster-tape.service`
- `systemd/aster-whale.service`
- `systemd/aster-liqs.service`
- `systemd/aster-oflow.service`
- `systemd/env/live_test_2026-04-25_small.env`

### Docs removed

- `docs/perps_go_translation_plan.md`
- `docs/learn_algo_trading_go_port.md`
- `docs/live_lite_env_defaults.md`

## Docs Updated

- `docs/pi_ops.md`
  - removed repo-managed Pi tmux/systemd/autoupdate instructions
  - reframed as manual host/runtime guidance
- `systemd/README.md`
  - now documents that only env templates remain
- `docs/gitbook/ops/runbook.md`
  - removed stale orchestration guidance
- `docs/gitbook/guides/developer-guide.md`
  - removed “Pi deployed through tmux/systemd” wording
- `docs/gitbook/architecture/overview.md`
  - removed tmux wrapper as a runtime invariant
- `docs/live_env_defaults.md`
  - renamed from stale “Live-Lite” wording in content
  - replaced deleted safe/balanced wrapper references with `run_live_logged.sh`
- `docs/execution_guide.md`
  - updated “Live-Lite Runtime Ops” heading to current `cmd/live` wording
- `.github/workflows/deploy-pi.yml`
  - removed repo-managed service restart/status steps

## Scanner URL Handling Decision

The hardcoded scanner cross-links to `34.174.250.99` were still active in the HTML served by:

- `cmd/long/main.go`
- `cmd/short/main.go`

They were replaced with environment-driven bases:

- `SHORT_SCANNER_URL` for the long scanner’s link to the short scanner
- `LONG_SCANNER_URL` for the short scanner’s link to the long scanner

Safe defaults were set to current local ports:

- `http://localhost:8081`
- `http://localhost:8080`

This removes the stale environment-specific IP without introducing a new deployment model.

## Preserved Intentionally

- `cmd/rr` was preserved exactly as requested
- `internal/tools` was preserved because `cmd/rr` imports it
- `scripts/deploy_pi.sh` was preserved, but reduced to a build/env-template helper instead of a tmux/systemd installer
- `scripts/start_bot.sh`, `scripts/restart_aster_stack.sh`, and `scripts/stop_bot.sh` were preserved as lightweight guidance helpers rather than deleted outright

## Deferred To Cleanup Pass 2

- Decide whether `ui/scanner-dashboard/` remains part of the repo
- Decide whether `reports/` should stay in-repo or move out entirely
- Decide which tracked `data/` files are real fixtures versus removable operational residue
- Revisit older Makefile deployment targets and any remaining non-GCP deployment assumptions
- Optionally archive or refresh `docs/repo_cleanup_audit.md` now that part of it is intentionally historical
