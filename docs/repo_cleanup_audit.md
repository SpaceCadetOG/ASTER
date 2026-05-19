# Repo Cleanup Audit

Date: 2026-05-18  
Repository: `SpaceCadetOG/ASTER`

## 1. Executive Summary

This repository still compiles end-to-end, but it contains multiple generations of operational surface area:

- The confirmed current production direction is the `cmd/live` runtime plus scanner/flow sidecars.
- The current documented production deployment path is still Raspberry Pi / Linux host oriented and centered on `systemd` + `tmux` wrapper orchestration.
- The repo also contains older standalone service units, old Makefile deployment targets, experimental commands, historical notes, generated artifacts, and a standalone UI that do not cleanly match the current runtime.

High-confidence conclusions:

- The modern runtime surface is `cmd/live`, `cmd/long`, `cmd/short`, `cmd/tape`, `cmd/whale`, `cmd/liqs`, `cmd/oflow`, with `cmd/exec` and `cmd/stats` as active operational tools.
- The Pi deployment path is still the primary operational path in docs and GitHub Actions, via `scripts/deploy_pi.sh` and `systemd/aster-modules-tmux.service`.
- The repo tracks several files that look like generated or machine-local artifacts and should not remain in git during a GCP migration, especially the root `live` binary, `.DS_Store`, `codebase.txt`, and runtime log/data artifacts.
- Several commands and packages appear orphaned or legacy: `cmd/bt-fetch`, `cmd/rr`, `cmd/lat-ch1`, `engine/`, and `internal/tools` outside of `cmd/rr`.

## 2. Repository Inventory

### Inventory commands run

```bash
git status --short
git ls-files
find . -maxdepth 3 -type f | sort
GOCACHE=$(pwd)/.gocache go list ./cmd/...
GOCACHE=$(pwd)/.gocache go list ./...
GOCACHE=$(pwd)/.gocache go test ./... -run TestDoesNotExist
```

### Working tree observations

`git status --short` shows both tracked and untracked operational artifacts already present locally:

- Modified tracked files: `.DS_Store`, `live`, `systemd/env/live_test_2026-04-25_small.env`
- Untracked runtime/report artifacts under `data/`, `reports/`, `third_party/`, and stray `.DS_Store` files

That is strong evidence the repo is currently mixing source, deploy config, local datasets, generated outputs, and machine-local noise.

### Top-level directory summary

| Path | Apparent purpose | Notes |
| --- | --- | --- |
| `adapters/` | Exchange adapter code | Active; `adapters/aster` is used by live/runtime tooling |
| `cmd/` | Go executable entrypoints | Mixed current, supporting, and legacy commands |
| `config/` | Config examples / YAML configs | Mixed; some examples align with current auth model, others look older |
| `data/` | Candle files, scanner outputs, whale outputs | Mixed committed sample data and generated runtime data |
| `docs/` | Runtime docs, audits, plans, notes | Mixed current docs and stale/historical material |
| `engine/` | Legacy Go package | No confirmed active imports found |
| `internal/` | Current application logic | Main active code surface |
| `logs/` | Runtime logs | Generated artifacts; not ideal for git tracking |
| `out/` | Backtest/replay outputs | Generated artifacts |
| `reports/` | Historical analysis / exported reports | Mostly generated or ad hoc |
| `scripts/` | Deployment and ops scripts | Mixed current Pi runtime helpers and stale/manual helpers |
| `systemd/` | Linux service units and env templates | Mixed current tmux-wrapper path and stale standalone units |
| `ui/` | Standalone scanner dashboard app | Not integrated into the main deploy/runtime path |
| `_NOTES_/` | Scratch notes / historical notes | Not part of current runtime |
| `third_party/` | Imported external material | Currently untracked and likely not part of shipping runtime |

## 3. Current Confirmed Runtime Surface

### Confirmed runtime executables

Based on `README.md`, `docs/gitbook/architecture/overview.md`, `docs/pi_ops.md`, `systemd/`, `scripts/`, GitHub Actions, and direct code inspection:

- `cmd/live`
- `cmd/long`
- `cmd/short`
- `cmd/tape`
- `cmd/whale`
- `cmd/liqs`
- `cmd/oflow`

### Confirmed operational support commands

- `cmd/exec`
- `cmd/stats`
- `cmd/backtest`
- `cmd/manualreplay`
- `cmd/vp`

### Confirmed active deployment/orchestration surface

- `scripts/deploy_pi.sh`
- `scripts/start_tmux_modules.sh`
- `scripts/tmux_module_runner.sh`
- `systemd/aster-modules-tmux.service`
- `systemd/aster-autoupdate.service`
- `systemd/aster-autoupdate.timer`
- `systemd/env/*.env.example` for live/scanners/flow modules

### Compile and package confirmation

`go list ./cmd/...` currently resolves these commands:

- `cmd/backtest`
- `cmd/bt-fetch`
- `cmd/exec`
- `cmd/lat-ch1`
- `cmd/liqs`
- `cmd/live`
- `cmd/long`
- `cmd/manualreplay`
- `cmd/oflow`
- `cmd/rr`
- `cmd/short`
- `cmd/stats`
- `cmd/tape`
- `cmd/vp`
- `cmd/whale`

`go test ./... -run TestDoesNotExist` passed, so the repo is still structurally buildable despite the operational drift.

## 4. Commands / Packages Table

| Command / package | Classification | Why |
| --- | --- | --- |
| `cmd/live` | ACTIVE / CURRENTLY REQUIRED | Primary runtime in [README.md](/Users/victorogbebor/2026/go-machine/README.md:7), [docs/gitbook/architecture/overview.md](/Users/victorogbebor/2026/go-machine/docs/gitbook/architecture/overview.md:22), active env template [systemd/env/live.env.example](/Users/victorogbebor/2026/go-machine/systemd/env/live.env.example:1) |
| `cmd/long` | ACTIVE / CURRENTLY REQUIRED | Scanner runtime referenced by docs, Makefile dev targets, and tmux module launcher |
| `cmd/short` | ACTIVE / CURRENTLY REQUIRED | Scanner runtime referenced by docs, Makefile dev targets, and tmux module launcher |
| `cmd/tape` | ACTIVE / CURRENTLY REQUIRED | Started by tmux wrapper and documented as sidecar flow module |
| `cmd/whale` | ACTIVE / CURRENTLY REQUIRED | Started by tmux wrapper and documented as sidecar flow module |
| `cmd/liqs` | ACTIVE / CURRENTLY REQUIRED | Started by tmux wrapper and documented as sidecar flow module |
| `cmd/oflow` | ACTIVE / CURRENTLY REQUIRED | Started by tmux wrapper and documented as sidecar flow module |
| `cmd/exec` | SUPPORTING / MANUAL TOOL | Current operational tool for auth/balance/order checks in docs and Makefile |
| `cmd/stats` | SUPPORTING / MANUAL TOOL | Referenced in README and docs for JSONL performance aggregation |
| `cmd/backtest` | SUPPORTING / MANUAL TOOL | Documented in gitbook and Makefile, but not part of live runtime |
| `cmd/manualreplay` | SUPPORTING / MANUAL TOOL | Explicit docs exist; not part of live runtime |
| `cmd/vp` | SUPPORTING / MANUAL TOOL | Explicit docs exist; analytical helper, not live runtime |
| `cmd/bt-fetch` | LEGACY CANDIDATE | Builds, but no meaningful references found in README/docs/scripts/systemd/workflows |
| `cmd/rr` | LEGACY CANDIDATE | Standalone risk/PnL helper with no meaningful external references found |
| `cmd/lat-ch1` | LEGACY CANDIDATE | Referenced only by translation/planning docs, not by runtime/deploy surface |
| `adapters/aster` | ACTIVE / CURRENTLY REQUIRED | Core exchange adapter used by runtime and `cmd/exec` |
| `internal/*` runtime packages used by active commands | ACTIVE / CURRENTLY REQUIRED | Used by `cmd/live`, scanners, and sidecars |
| `internal/backtest` | SUPPORTING / MANUAL TOOL | Used by `cmd/backtest`; not live runtime |
| `internal/dev` | LEGACY / NARROW TOOLING | Only tied to Makefile dev watcher usage |
| `internal/tools` | LEGACY CANDIDATE | Only confirmed use is `cmd/rr` |
| `internal/structure` | MEDIUM-CONFIDENCE REMOVE CANDIDATE | Present with tests, but not in active runtime dependency graph |
| `engine/` | HIGH-CONFIDENCE REMOVE CANDIDATE | No confirmed imports from active/runtime commands |

### Notable drift inside active commands

- [cmd/long/main.go](/Users/victorogbebor/2026/go-machine/cmd/long/main.go:193) still hardcodes `http://34.174.250.99:8081`
- [cmd/short/main.go](/Users/victorogbebor/2026/go-machine/cmd/short/main.go:188) still hardcodes `http://34.174.250.99:8080`

Those are strong stale-environment indicators for a future GCP migration patch.

## 5. Scripts Table

| Script | What it does | References | Valid for current runtime? | Classification |
| --- | --- | --- | --- | --- |
| `scripts/deploy_pi.sh` | Builds binaries, installs units/scripts/envs under `/opt/aster`, enables tmux wrapper/autoupdate | GitHub Actions deploy workflow, docs, `start_bot.sh` | Yes, current Pi deploy path | KEEP |
| `scripts/start_tmux_modules.sh` | Starts tmux sessions for `live`, `long`, `short`, `tape`, `whale`, `liqs`, `oflow` | `aster-modules-tmux.service` | Yes, core current runtime wrapper | KEEP |
| `scripts/tmux_module_runner.sh` | Creates/restarts individual tmux-backed module sessions with env sourcing | `start_tmux_modules.sh` | Yes | KEEP |
| `scripts/auto_update_aster.sh` | Pulls latest code, rebuilds, restarts wrapper service | `aster-autoupdate.service`, docs | Yes, current Pi automation path | KEEP |
| `scripts/restart_aster_stack.sh` | Stops legacy services and ensures `aster-modules-tmux` is up | `start_bot.sh`, docs | Mostly yes, but still carries legacy-service handling | UPDATE |
| `scripts/start_bot.sh` | Interactive helper to deploy and restart stack | docs | Yes, but Pi-specific and operator-oriented | UPDATE |
| `scripts/stop_bot.sh` | Stops legacy services and kills tmux session | docs only | Partially stale; default session name does not match current per-module tmux layout | UPDATE |
| `scripts/tmux_aster.sh` | Creates a human operator tmux workspace | docs | Useful manually, but not part of required runtime | UPDATE |
| `scripts/maintenance_midnight.sh` | Pulls latest code and runs `reconcile_on_boot.sh` | Referenced by `LIVE_MAINT1_HOOK` | Thin but still wired into env template | UPDATE |
| `scripts/maintenance_eod.sh` | Pulls latest code and runs `reconcile_on_boot.sh` | Referenced by `LIVE_MAINT2_HOOK` | Thin but still wired into env template | UPDATE |
| `scripts/reconcile_on_boot.sh` | Prints reconciliation/state/session info | maintenance scripts, docs | Very thin; name implies more than it does | REMOVE CANDIDATE |
| `scripts/run_live_logged.sh` | Runs `cmd/live` with env sourcing and rotating log pipe | docs | Manual helper still plausible | KEEP |
| `scripts/run_live_safe_logged.sh` | Safe live-profile wrapper around `run_live_logged.sh` | docs | Manual helper; still plausible | UNKNOWN |
| `scripts/run_live_balanced_logged.sh` | Balanced live-profile wrapper around `run_live_logged.sh` | docs | Manual helper; still plausible | UNKNOWN |
| `scripts/run_live_one_trade_logged.sh` | One-trade live-profile wrapper | No meaningful repo references found | Possibly obsolete | REMOVE CANDIDATE |
| `scripts/run_long_logged.sh` | Logged wrapper for scanner long | no major deploy refs | Plausible but not current main runtime path | UNKNOWN |
| `scripts/run_short_logged.sh` | Logged wrapper for scanner short | no major deploy refs | Plausible but not current main runtime path | UNKNOWN |
| `scripts/stream_to_rotating_log.sh` | Shared logging helper used by `run_*_logged.sh` wrappers | run wrappers | Valid if wrappers stay | KEEP |
| `scripts/smoke.sh` | Curl-based smoke test for long/short scanner endpoints | Makefile | Still valid for scanner-only checks | UNKNOWN |
| `scripts/audit_live.sh` | Greps/parses live logs for quick operator audit | no strong refs | Manual only; likely obsolete | REMOVE CANDIDATE |
| `scripts/generate_agent_handoff.sh` | Produces markdown handoff from logs and `cmd/stats` | no strong refs | Useful support utility, but not runtime | UNKNOWN |

## 6. systemd Table

| File | Purpose | Launch target | Exists? | Current architecture fit | Classification |
| --- | --- | --- | --- | --- | --- |
| `systemd/aster-modules-tmux.service` | Main wrapper service for module tmux sessions | `/opt/aster/scripts/start_tmux_modules.sh` | Yes | Matches current Pi runtime docs and deploy flow | KEEP |
| `systemd/aster-autoupdate.service` | Oneshot update/build/restart service | `/opt/aster/scripts/auto_update_aster.sh` | Yes | Matches current Pi automation path | KEEP |
| `systemd/aster-autoupdate.timer` | Schedules autoupdate service | `aster-autoupdate.service` | Yes | Matches current Pi automation path | KEEP |
| `systemd/aster-live.service` | Standalone live service | `/opt/aster/bin/live` | Yes | Current docs prefer wrapper instead; carries `Conflicts=go-machine.service` legacy marker | REMOVE CANDIDATE |
| `systemd/aster-tape.service` | Standalone tape sidecar | `/opt/aster/bin/tape` | Yes | Wrapper now owns this role | REMOVE CANDIDATE |
| `systemd/aster-whale.service` | Standalone whale sidecar | `/opt/aster/bin/whale` | Yes | Wrapper now owns this role | REMOVE CANDIDATE |
| `systemd/aster-liqs.service` | Standalone liqs sidecar | `/opt/aster/bin/liqs` | Yes | Wrapper now owns this role | REMOVE CANDIDATE |
| `systemd/aster-oflow.service` | Standalone oflow sidecar | `/opt/aster/bin/oflow` | Yes | Wrapper now owns this role | REMOVE CANDIDATE |
| `systemd/env/live.env.example` | Primary live env template | consumed by live runtime | Yes | Current and important | KEEP |
| `systemd/env/long.env.example` | Scanner env template | consumed by `cmd/long` | Yes | Current if scanners remain separate | KEEP |
| `systemd/env/short.env.example` | Scanner env template | consumed by `cmd/short` | Yes | Current if scanners remain separate | KEEP |
| `systemd/env/tape.env.example` | Tape env template | consumed by `cmd/tape` | Yes | Current if sidecars remain separate | KEEP |
| `systemd/env/whale.env.example` | Whale env template | consumed by `cmd/whale` | Yes | Current if sidecars remain separate | KEEP |
| `systemd/env/liqs.env.example` | Liqs env template | consumed by `cmd/liqs` | Yes | Current if sidecars remain separate | KEEP |
| `systemd/env/oflow.env.example` | Oflow env template | consumed by `cmd/oflow` | Yes | Current if sidecars remain separate | KEEP |
| `systemd/env/live_test_2026-04-25_small.env` | Historical one-off test profile | manual | Yes | Looks like a dated operational artifact, not a reusable template | REMOVE CANDIDATE |
| `systemd/README.md` | Pi/Linux install docs | documentation | Yes | Mostly current, but Pi/tmux specific | UPDATE |

## 7. Documentation Drift Audit

| File | Status | Why |
| --- | --- | --- |
| `README.md` | MATCHES CURRENT RUNTIME | Describes `cmd/live`, Aster adapter, stats, and current flow correctly |
| `docs/gitbook/architecture/overview.md` | MOSTLY CURRENT | Accurately describes production runtime and Pi wrapper model |
| `docs/pi_ops.md` | CURRENT BUT PI-SPECIFIC | Matches current deployment practice, but should later be reframed for GCP |
| `docs/execution_guide.md` | CURRENT | Matches `cmd/exec` |
| `docs/aster_exec.md` | CURRENT | Matches current Aster agent-auth path and `yaml.example` |
| `docs/manual-replay.md` | CURRENT | Matches `cmd/manualreplay` |
| `docs/HOWTO-manual-replay.md` | CURRENT | Matches `cmd/manualreplay` |
| `docs/volume_profile.md` | CURRENT | Matches `cmd/vp` |
| `docs/live_env_defaults.md` | UPDATE | Content is useful, but “Live-Lite Environment Knobs” wording is stale |
| `docs/live_lite_env_defaults.md` | OUTDATED | Explicitly references nonexistent `cmd/live-lite/main.go` |
| `docs/gitbook/ops/runbook.md` | UPDATE | Mostly current architecture, but still tightly Pi/self-hosted-runner oriented |
| `docs/gitbook/guides/quickstart.md` | UPDATE | Contains backtest and runtime guidance, but still assumes Pi-style local deployment |
| `docs/gitbook/guides/developer-guide.md` | UPDATE | Useful, but includes host-specific paths and old deployment assumptions |
| `docs/ASTER_V3_AUDIT_AND_REDESIGN.md` | HISTORICAL / KEEP | Valuable architecture history, not runtime documentation |
| `docs/telegram_refactor_codex_prompt.md` | HISTORICAL / MOVE OR REMOVE | Prompt/task artifact, not durable runtime documentation |
| `docs/perps_go_translation_plan.md` | HISTORICAL / MOVE OR REMOVE | Planning doc, not runtime documentation |
| `docs/learn_algo_trading_go_port.md` | HISTORICAL / MOVE OR REMOVE | Translation note tied to `cmd/lat-ch1` |
| `_NOTES_/...` | HISTORICAL / MOVE OR REMOVE | Scratch notes, not operational docs |
| `Momentum_Fib_Trade_Logic.md` | LIKELY STALE | Strategy note not connected to confirmed runtime surface |
| `yaml.example` | CURRENT | Matches agent-auth examples referenced by `docs/aster_exec.md` |
| `config/example.yaml` | POSSIBLY STALE | Older config style; does not appear to be the primary current auth/runtime path |
| `codebase.txt` | GENERATED ARTIFACT | Monolithic dump, not maintainable documentation |
| `ui/scanner-dashboard/README.md` | UNKNOWN | Documents a standalone UI not referenced by current deploy/runtime surface |

### Documentation duplication / drift themes

- `README.md` and `docs/gitbook/architecture/overview.md` are aligned and reinforce each other well.
- Operational docs are split across `docs/pi_ops.md`, `systemd/README.md`, and `docs/gitbook/ops/runbook.md`, with overlapping Pi-centric deployment guidance.
- “live-lite” terminology is stale and no longer matches the active `cmd/live` naming.
- Historical planning and scratch-note material lives alongside runtime docs without clear separation.

## 8. Non-Source Artifact Audit

| File / area | Assessment | Recommendation |
| --- | --- | --- |
| `.DS_Store` at repo root | Machine-local artifact | Delete from git and gitignore |
| `docs/gitbook/.DS_Store` | Machine-local artifact | Delete and gitignore |
| `live` at repo root | Built binary artifact | Delete from git and gitignore |
| `logs/events.jsonl` | Generated runtime log | Remove from git; keep path gitignored; optionally provide sample fixture elsewhere |
| `data/*.csv`, `data/*.json`, `data/*.jsonl` | Mixed sample and generated datasets | Split into committed fixtures/examples vs generated runtime data |
| `out/` | Generated outputs | Gitignore |
| `reports/` | Historical generated reports | Move selected reports to docs if needed; otherwise keep out of source control |
| `third_party/` | Imported external material | Keep only if intentionally vendored; otherwise remove |
| `codebase.txt` | Generated monolithic dump | Delete from git |
| `wave_window_analysis_2026-05-01.txt` | One-off analysis artifact | Remove or move to archival docs |
| `systemd/env/live_test_2026-04-25_small.env` | Dated one-off env file | Remove or move under a clearly named examples/archive folder |

## 9. Import / Reference Audit for Possible Dead Code

### Active dependency graph observations

Comparing all packages from `go list ./...` against the dependency closure of the active runtime commands shows the active code surface is concentrated in:

- `adapters/aster`
- `internal/api`
- `internal/data`
- `internal/discovery`
- `internal/execution`
- `internal/executor`
- `internal/features`
- `internal/flow`
- `internal/gate`
- `internal/indicators`
- `internal/inplay`
- `internal/levels`
- `internal/market`
- `internal/notify`
- `internal/ratelimit`
- `internal/reliability`
- `internal/risk`
- `internal/sessions`
- `internal/stats`
- `internal/status`
- `internal/strategies`
- `internal/ta`
- `internal/throttle`
- `internal/types`
- `internal/ws`

### Orphan / low-reference candidates

| Package / file area | Evidence | Candidate status |
| --- | --- | --- |
| `engine/` | No confirmed imports from current commands or runtime docs | HIGH-CONFIDENCE REMOVE CANDIDATE |
| `internal/tools` | Confirmed use only from `cmd/rr` | HIGH-CONFIDENCE REMOVE CANDIDATE if `cmd/rr` is dropped |
| `internal/structure` | Not in active runtime dependency graph | MEDIUM-CONFIDENCE REMOVE CANDIDATE |
| `internal/dev` | Narrow Makefile watcher support only | MEDIUM-CONFIDENCE REMOVE CANDIDATE |
| `cmd/bt-fetch` | Builds but no meaningful repo references found | HIGH-CONFIDENCE REMOVE CANDIDATE |
| `cmd/rr` | Builds but no meaningful repo references found | HIGH-CONFIDENCE REMOVE CANDIDATE |
| `cmd/lat-ch1` | Referenced only by translation/planning docs | HIGH-CONFIDENCE REMOVE CANDIDATE |

## 10. High-Confidence Remove Candidates

- `live` (tracked built binary)
- `.DS_Store`
- `docs/gitbook/.DS_Store`
- `codebase.txt`
- `cmd/bt-fetch`
- `cmd/rr`
- `cmd/lat-ch1`
- `engine/`
- `systemd/aster-live.service`
- `systemd/aster-tape.service`
- `systemd/aster-whale.service`
- `systemd/aster-liqs.service`
- `systemd/aster-oflow.service`
- `systemd/env/live_test_2026-04-25_small.env`
- `scripts/run_live_one_trade_logged.sh`
- `scripts/audit_live.sh`

## 11. Medium-Confidence Remove Candidates

- `internal/tools` if `cmd/rr` is removed
- `internal/structure`
- `internal/dev`
- `scripts/reconcile_on_boot.sh`
- `ui/scanner-dashboard/`
- `_NOTES_/`
- `Momentum_Fib_Trade_Logic.md`
- `docs/telegram_refactor_codex_prompt.md`
- `docs/perps_go_translation_plan.md`
- `docs/learn_algo_trading_go_port.md`
- committed runtime data/log artifacts under `data/` and `logs/`

## 12. Needs Human Confirmation

- Whether `cmd/tape`, `cmd/whale`, `cmd/liqs`, and `cmd/oflow` remain part of the desired GCP runtime, or whether some flow modules should be folded into `cmd/live`
- Whether `cmd/long` and `cmd/short` remain separate long-lived processes in GCP, or become internalized / replaced
- Whether `cmd/backtest`, `cmd/manualreplay`, and `cmd/vp` are still valuable in-repo research tools for the team
- Whether the standalone `ui/scanner-dashboard/` is a future operator dashboard or abandoned material
- Which committed files under `data/` are intentional fixtures versus generated operational residue
- Whether Pi-specific scripts should be kept temporarily during migration or removed immediately after GCP cutover

## 13. Proposed Deletion Plan for a Follow-up Patch

### Phase 1: Safe artifact cleanup

- Remove tracked binaries and machine-local files
- Remove monolithic generated dumps
- Add or tighten `.gitignore` for logs, outputs, caches, OS artifacts, and local report data

### Phase 2: Remove clearly obsolete runtime surfaces

- Remove `cmd/bt-fetch`, `cmd/rr`, `cmd/lat-ch1`
- Remove `engine/`
- Remove stale standalone `systemd/aster-*.service` units that are superseded by `aster-modules-tmux.service`
- Remove dated one-off env profiles under `systemd/env/`

### Phase 3: Separate history from runtime docs

- Move planning notes and scratch docs into an explicit archive folder or remove them
- Rewrite Pi-specific operational docs into a GCP migration/operator guide
- Eliminate `live-lite` terminology and stale path references

### Phase 4: Re-audit ambiguous surfaces

- Decide fate of `ui/scanner-dashboard/`
- Decide whether sidecars remain separate runtime units in GCP
- Decide which research tools stay in the main repo versus move to `tools/`, `experiments/`, or an archive repo

## 14. Concise Terminal Summary

- Active commands: `cmd/live`, `cmd/long`, `cmd/short`, `cmd/tape`, `cmd/whale`, `cmd/liqs`, `cmd/oflow`, with `cmd/exec` and `cmd/stats` as active support tools
- Active scripts: `deploy_pi.sh`, `start_tmux_modules.sh`, `tmux_module_runner.sh`, `auto_update_aster.sh`, plus current manual/operator wrappers
- Active systemd units: `aster-modules-tmux.service`, `aster-autoupdate.service`, `aster-autoupdate.timer`, current `env/*.env.example` templates
- Safe remove candidates: root `live`, `.DS_Store`, `codebase.txt`, `cmd/bt-fetch`, `cmd/rr`, `cmd/lat-ch1`, `engine/`, standalone `aster-*.service` sidecar units, dated env artifact `live_test_2026-04-25_small.env`
- Needs confirmation: sidecar future on GCP, scanner process future, dashboard fate, committed data fixtures vs generated residue
