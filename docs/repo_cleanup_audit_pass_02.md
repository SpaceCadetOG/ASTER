# Repo Cleanup Audit Pass 02

Date: 2026-05-18  
Repository: `SpaceCadetOG/ASTER`

## 1. Executive Summary

Cleanup Pass 1 removed the obvious dead runtime surfaces. This audit focuses on the ambiguous remainder: `data/`, `reports/`, `ui/scanner-dashboard/`, `_NOTES_/`, surviving scripts, Makefile targets, and lingering local/Pi-era docs.

High-confidence conclusions:

- `data/` is mixed. A small subset is acting as reproducible example input for `cmd/backtest` and `cmd/manualreplay`, while the rest is local analysis residue or undocumented sample material.
- `reports/` is not an active product surface. Most of it is historical analysis output and already ignored for new files. Only a tiny tracked subset has a case for preservation.
- `ui/scanner-dashboard/` is a real Next.js app pointed at current scanner/live APIs, not a dead stub, but it is not integrated into the main repo workflow and currently carries ignored `.next/` build output.
- `_NOTES_/` is entirely scratch/historical material. None of it appears to be part of the current ASTER architecture.
- The `Makefile` still contains useful `exec-*`, `test`, `fmt`, and `backtest` targets, but its scanner run/dev/build/deploy assumptions are partly stale after Pass 1.
- Remaining docs still over-index on Pi/local-host framing. Several should be updated or archived before a GCP migration, even if they are not deleted immediately.
- Several surviving scripts are now only thin local conveniences or legacy holdovers. A second patch can likely remove more of them safely.

Assumption used in this audit:

- This report evaluates the repo as it exists after Cleanup Pass 1 changes in the working tree, even though some Pass 1 deletions are not yet committed.

## 2. `data/` Audit Table

| Path | Status | Apparent purpose | Referenced by | Classification | Notes |
| --- | --- | --- | --- | --- | --- |
| `data/ASTERUSDT_1m.csv` | tracked | 1m candle sample | `cmd/backtest` naming convention, `Makefile backtest` default symbol set | KEEP AS REQUIRED FIXTURE | Default backtest symbols include `ASTERUSDT` |
| `data/BNBUSDT_1m.csv` | tracked | 1m candle sample | `cmd/backtest` naming convention, `Makefile backtest` default symbol set | KEEP AS REQUIRED FIXTURE | Default backtest symbols include `BNBUSDT` |
| `data/BTCUSDT_1m.csv` | tracked | 1m candle sample | `cmd/backtest` naming convention, `Makefile backtest` default symbol set | KEEP AS REQUIRED FIXTURE | Default backtest symbols include `BTCUSDT` |
| `data/ETHUSDT_1m.csv` | tracked | 1m candle sample | `cmd/backtest` naming convention, `Makefile backtest` default symbol set | KEEP AS REQUIRED FIXTURE | Default backtest symbols include `ETHUSDT` |
| `data/HYPEUSDT_1m.csv` | tracked | 1m candle sample | `cmd/backtest` naming convention, `Makefile backtest` default symbol set | KEEP AS REQUIRED FIXTURE | Default backtest symbols include `HYPEUSDT` |
| `data/SOLUSDT_1m.csv` | tracked | 1m candle sample | `cmd/backtest` naming convention, `Makefile backtest` default symbol set | KEEP AS REQUIRED FIXTURE | Default backtest symbols include `SOLUSDT` |
| `data/scanner.jsonl` | tracked | scanner snapshot example | `cmd/backtest` default scanner path | KEEP AS REQUIRED FIXTURE | Default `cmd/backtest` path is `data/scanner.jsonl` |
| `data/POWERUSDT_1m.csv` | tracked | sample candle input for one symbol-specific backtest scenario | only generic `cmd/backtest` file naming convention | MOVE TO `docs/examples` | Not used by defaults or tests; better as a documented example than root runtime data |
| `data/POWERUSDT_whales.jsonl` | tracked | whale tape example for POWER | only generic `cmd/backtest` whale path convention | MOVE TO `docs/examples` | Not referenced by docs/tests; likely example material rather than core fixture |
| `data/ASTEROIDUSDT_1m.csv` | untracked | manual replay candle input | `docs/manual-replay.md`, `docs/HOWTO-manual-replay.md` | MOVE TO `docs/examples` | Current docs reference it, but it is not committed |
| `data/CHIPUSDT_1m.csv` | untracked | manual replay candle input | `docs/manual-replay.md`, `docs/HOWTO-manual-replay.md` | MOVE TO `docs/examples` | Current docs reference it, but it is not committed |
| `data/RAVEUSDT_1m.csv` | untracked | manual replay candle input | `docs/manual-replay.md`, `docs/HOWTO-manual-replay.md` | MOVE TO `docs/examples` | Current docs reference it, but it is not committed |
| `data/SPKUSDT_1m_2026-04-23.csv` | untracked | replay input for SPK analysis | only untracked `reports/spk_*` files | GENERATED RUNTIME ARTIFACT | No active code/docs reference |
| `data/SPKUSDT_1m_2026-04-23.json` | untracked | raw candle capture for SPK analysis | only untracked `reports/spk_*` files | GENERATED RUNTIME ARTIFACT | Analysis residue |
| `data/SPKUSDT_1m_2026-04-23_full.csv` | untracked | extended replay input for SPK analysis | only untracked `reports/spk_*` files | GENERATED RUNTIME ARTIFACT | Analysis residue |
| `data/SPKUSDT_1m_2026-04-23_full.json` | untracked | raw extended candle capture for SPK analysis | only untracked `reports/spk_*` files | GENERATED RUNTIME ARTIFACT | Analysis residue |
| `data/scanner_chip.jsonl` | untracked | CHIP-specific scanner capture | no active code/docs refs | REMOVE CANDIDATE | Local analysis input, not documented |

### Data-specific findings

- `cmd/backtest` defaults depend on the naming convention `data/<SYMBOL>_<TF>.csv`, and the current default symbol list in `Makefile` matches six tracked candle files exactly.
- `cmd/backtest` also defaults to `data/scanner.jsonl`, so removing that file without changing code or docs would break the out-of-the-box example path.
- Manual replay docs currently depend on three untracked files: `CHIPUSDT_1m.csv`, `RAVEUSDT_1m.csv`, and `ASTEROIDUSDT_1m.csv`. That is the clearest current mismatch in this area.

### Data directory recommendation

- Preserve the six default backtest candle CSVs plus `scanner.jsonl` until `cmd/backtest` defaults are redesigned.
- Move the manual replay candle inputs into a committed examples location such as `docs/examples/manual-replay/` or `testdata/manualreplay/`.
- Move `POWERUSDT_1m.csv` and `POWERUSDT_whales.jsonl` into a committed examples location if they are meant to remain reusable.
- Ignore and remove SPK/scanner-local captures unless you explicitly want to preserve that analysis workflow.

## 3. `reports/` Audit Table

| Path | Status | Apparent purpose | Linked from docs? | Classification | Notes |
| --- | --- | --- | --- | --- | --- |
| `reports/manual_replay_trades_rave_asteroid_chip.csv` | tracked | reusable trade input sheet for manual replay | Yes, `docs/manual-replay.md` and `docs/HOWTO-manual-replay.md` | KEEP IN DOCS | This is the strongest case for staying committed |
| `reports/manual_replay_comparison_rave_asteroid_chip_2026-04-22.md` | tracked | historical comparison write-up | No direct active links | MOVE TO `docs/archive` | Valuable as history, not runtime docs |
| `reports/system_patch_checklist.md` | tracked | historical patch checklist | No active links | MOVE TO `docs/archive` | Historical engineering artifact |
| `reports/trade_giveback_2026-04-20.csv` | tracked | historical trade analysis export | No active links | MOVE TO `docs/archive` | Historical analysis input, not current runtime material |
| `reports/apr23_vs_apr24_comparison.md` | untracked, ignored | historical audit note | No | MOVE TO `docs/archive` | Durable narrative but not active output |
| `reports/apr24_compact_audit.md` | untracked, ignored | historical audit note | No | MOVE TO `docs/archive` | Durable narrative but not active output |
| `reports/apr24_symbol_timelines.md` | untracked, ignored | historical review note | No | MOVE TO `docs/archive` | Useful only as archive |
| `reports/apr25_katusdt_incident_report.md` | untracked, ignored | incident report | No | MOVE TO `docs/archive` | Better as archived incident documentation |
| `reports/bot_decision_strategies_explained.txt` | untracked, ignored | strategy explainer snapshot | No | MOVE TO `docs/archive` | Human-authored analysis, not runtime doc |
| `reports/system_memo_apr26_29.md` | untracked, ignored | historical system memo | No | MOVE TO `docs/archive` | Historical architecture/ops commentary |
| `reports/chip_bar_by_bar_replay_2026-04-21_22.md` | untracked, ignored | replay output | No | GENERATED OUTPUT | Derived analysis |
| `reports/chip_manual_replay_snapshots_2026-04-21_22.csv` | untracked, ignored | replay export | No | GENERATED OUTPUT | Derived analysis |
| `reports/codebase_snapshot_2026-04-21.txt` | untracked, ignored | code snapshot dump | No | GENERATED OUTPUT | Same class as `codebase.txt` removed in Pass 1 |
| `reports/log_extract_2026-04-23_live-2026-04-23_focus.txt` | untracked, ignored | log extraction | No | GENERATED OUTPUT | Derived from runtime logs |
| `reports/log_extract_2026-04-23_live-latest_focus.txt` | untracked, ignored | log extraction | No | GENERATED OUTPUT | Derived from runtime logs |
| `reports/log_extract_2026-04-23_live-noise_focus.txt` | untracked, ignored | log extraction | No | GENERATED OUTPUT | Derived from runtime logs |
| `reports/logs_2026-04-23_chip_audit.md` | untracked, ignored | analysis note from logs | No | GENERATED OUTPUT | Derived local review artifact |
| `reports/logs_2026-04-23_chip_extract.txt` | untracked, ignored | extracted log slice | No | GENERATED OUTPUT | Derived local review artifact |
| `reports/logs_2026-04-23_spk_extract.txt` | untracked, ignored | extracted log slice | No | GENERATED OUTPUT | Derived local review artifact |
| `reports/overnight_trade_optimal_exits_2026-04-20_21.csv` | untracked, ignored | analysis export | No | GENERATED OUTPUT | Derived analysis |
| `reports/review2_all_paper_entries_2026-04-23.txt` | untracked, ignored | entry extraction | No | GENERATED OUTPUT | Derived analysis |
| `reports/review2_all_paper_exits_2026-04-23.csv` | untracked, ignored | exit extraction | No | GENERATED OUTPUT | Derived analysis |
| `reports/review2_trade_gradecard_2026-04-23.csv` | untracked, ignored | grading output | No | GENERATED OUTPUT | Derived analysis |
| `reports/spk_2026-04-23_bar_by_bar_review.md` | untracked, ignored | replay review | No | GENERATED OUTPUT | Depends on untracked SPK data |
| `reports/spk_2026-04-23_exits.csv` | untracked, ignored | replay export | No | GENERATED OUTPUT | Derived analysis |
| `reports/spk_2026-04-23_replay_trades.csv` | untracked, ignored | replay input sheet | No | GENERATED OUTPUT | Derived analysis |
| `reports/spk_2026-04-23_replay_trades_full.csv` | untracked, ignored | replay input sheet | No | GENERATED OUTPUT | Derived analysis |
| `reports/logs_2026-05-03_12_from_pi/aster-live-journal-2026-05-03_12.log` | untracked, ignored | raw journal log | No | GENERATED OUTPUT | Runtime log |
| `reports/logs_2026-05-03_12_from_pi/live_trade_events_2026-05-03_12.log` | untracked, ignored | raw event log | No | GENERATED OUTPUT | Runtime log |
| `reports/logs_2026-05-03_12_from_pi/may03_12_logs.tgz` | untracked, ignored | archived logs | No | GENERATED OUTPUT | Raw runtime artifact |
| `reports/logs_2026-05-03_12_from_pi/opt/aster/logs/*` | untracked, ignored | copied runtime logs | No | GENERATED OUTPUT | Raw runtime artifacts |

### Reports-specific findings

- `reports/` is already effectively treated as disposable for new files: `git status --ignored` shows the untracked reports under ignore rules.
- Only four files are still tracked under `reports/`, and only one of them is actively referenced by current docs: `manual_replay_trades_rave_asteroid_chip.csv`.
- Most remaining report files are clearly generated outputs or review notes that belong in an archive, not the source root.

### Reports directory recommendation

- Treat `reports/` as non-source by default.
- Preserve `manual_replay_trades_rave_asteroid_chip.csv` as a committed example input, but consider moving it under `docs/examples/` or `docs/archive/examples/`.
- Move any durable write-ups you care about into `docs/archive/`.
- Remove the rest from git and keep `reports/` ignored.

## 4. `ui/scanner-dashboard/` Audit

### What it is

- Framework: Next.js `14.2.24`
- UI stack: React `18.3.1`, TypeScript `5.8.2`
- Package manager evidence: `package-lock.json` exists
- Build artifacts: ignored `.next/` tree exists locally

### Current integration status

- Active repo references are minimal: the dashboard is mostly self-contained and referenced only by its own README plus the cleanup audit docs.
- It consumes current scanner/live endpoints, not stale deleted ones:
  - `SCANNER_LONG_URL` -> default `http://127.0.0.1:8080`
  - `SCANNER_SHORT_URL` -> default `http://127.0.0.1:8081`
  - `SCANNER_LIVE_URL` -> default `http://127.0.0.1:8787`
  - `SCANNER_OFLOW_URL` -> default `http://127.0.0.1:8090`
  - `SCANNER_TAPE_URL` -> default `http://127.0.0.1:8091`
  - `SCANNER_WHALE_URL` -> default `http://127.0.0.1:8092`
  - `SCANNER_LIQS_URL` -> default `http://127.0.0.1:8093`
- It exposes internal app routes:
  - `app/api/dashboard/route.ts`
  - `app/api/asset/[symbol]/route.ts`
- It still looks viable as a local browser shell over the current module HTTP APIs.

### Build / viability check

- `node_modules/` is not present locally.
- I did not run `npm install` or a full build because this is audit-only and there was no installed dependency tree.
- The presence of an ignored `.next/` output directory indicates it has been built locally before.

### Value judgment

Pros:
- Already speaks the current scanner/live APIs.
- Uses environment variables rather than hardcoded deployment IPs.
- Could be a seed for a future GCP/web operator dashboard.

Cons:
- Not integrated into CI, root docs, or repo workflows.
- Carries local generated `.next/` output in the working tree.
- Still reads like a sidecar experiment rather than an adopted product surface.

### Classification

- `ui/scanner-dashboard/`: KEEP AND MODERNIZE LATER
- `ui/scanner-dashboard/.next/`: GENERATED OUTPUT, keep ignored

## 5. `_NOTES_/` Audit Table

| Path | Subject | Relevance to current ASTER | Duplicates current docs? | Classification | Notes |
| --- | --- | --- | --- | --- | --- |
| `_NOTES_/Logic.md` | generic spread / imbalance / mark-vs-index notes | Low | Partially duplicated by strategy/risk docs at a much lower quality | REMOVE CANDIDATE | Scratchpad quality |
| `_NOTES_/ProjectLayout.md` | generic Python crypto bot layout | None | No | REMOVE CANDIDATE | Not this repo, not this language |
| `_NOTES_/Timeline.md` | generic build timeline | None | No | REMOVE CANDIDATE | Historical planning stub |
| `_NOTES_/avantis_arb.md` | Base flash-loan arb concept | None | No | REMOVE CANDIDATE | Unrelated to ASTER perps bot |
| `_NOTES_/avantis_resource.md` | hypothetical DEX Python snippets / links | None | No | REMOVE CANDIDATE | Unrelated and low signal |
| `_NOTES_/githubstuff.md` | ad hoc Git push notes for other repo names/remotes | None | No | REMOVE CANDIDATE | Not part of ASTER docs |

## 6. Makefile Target Table

| Target | Commands | Post-Pass-1 status | Classification | Notes |
| --- | --- | --- | --- | --- |
| `smoke` | `./scripts/smoke.sh` | Works if scanners are already running and `curl`/`jq` exist | KEEP | Still useful as lightweight scanner API smoke test |
| `run` | `go run ./cmd/long/main.go` then `go run ./cmd/short/main.go` | Misleading; first command blocks so second never starts | UPDATE | Should become explicit single-process targets or parallel helper |
| `dev` | watcher over long then watcher over short | Misleading; first watcher blocks | UPDATE | Same issue as `run` |
| `devq` | quiet watcher over long then quiet watcher over short | Misleading; first watcher blocks | UPDATE | Same issue as `run` |
| `build` | builds `go-machine-long` and `go-machine-short` | Works | UPDATE | Useful, but too narrow for current repo runtime |
| `test` | `go test ./... -v` | Works | KEEP | Current and useful |
| `fmt` | `go fmt ./...` | Works | KEEP | Current and useful |
| `clean` | removes `go-machine-long`, `go-machine-short`, `bin` | Works | UPDATE | Root build artifact set has changed; cleanup scope is narrow |
| `backtest` | `go run ./cmd/backtest` with default envs | Works | KEEP | Still useful and aligned with retained command |
| `deploy` | `scp` scanner binaries, restart `traderbot` and `traderbot-short` services | Stale | REMOVE CANDIDATE | Old VM/service names, not current architecture |
| `exec-balance` | `go run ./cmd/exec` balance | Works | KEEP | Useful current operational shortcut |
| `exec-account` | `go run ./cmd/exec` account | Works | KEEP | Useful current operational shortcut |
| `exec-open-orders` | `go run ./cmd/exec` open orders | Works | KEEP | Useful current operational shortcut |
| `exec-position` | `go run ./cmd/exec` position | Works | KEEP | Useful current operational shortcut |
| `exec-place` | `go run ./cmd/exec` place | Works | KEEP | Useful current operational shortcut |
| `exec-cancel` | `go run ./cmd/exec` cancel | Works | KEEP | Useful current operational shortcut |
| `exec-cancel-all` | `go run ./cmd/exec` cancel all | Works | KEEP | Useful current operational shortcut |
| `exec-status` | `go run ./cmd/exec` status | Works | KEEP | Useful current operational shortcut |

### Makefile findings

- No remaining Makefile target references deleted `cmd/bt-fetch`, `cmd/lat-ch1`, or removed `engine/`.
- The main stale area is operational: `run`, `dev`, `devq`, `build`, `clean`, and especially `deploy` still reflect a much older scanner-centric layout.

## 7. Docs Review Table

| Path | Classification | Why |
| --- | --- | --- |
| `README.md` | KEEP CURRENT | Still accurately summarizes the core runtime |
| `docs/aster_exec.md` | KEEP CURRENT | Aligned with current Aster auth/signing path |
| `docs/execution_guide.md` | KEEP CURRENT | Still useful current command guidance |
| `docs/volume_profile.md` | KEEP CURRENT | Still aligned with `cmd/vp` |
| `docs/vp_strategy.md` | KEEP CURRENT | Still relevant strategy/supporting doc |
| `docs/gitbook/architecture/overview.md` | KEEP CURRENT | Pass 1 removed the worst orchestration drift; still valid |
| `docs/gitbook/reference/backtest-api.md` | KEEP CURRENT | Matches retained `cmd/backtest` |
| `docs/gitbook/reference/cli-api.md` | KEEP CURRENT | Matches retained command set |
| `docs/gitbook/reference/data-models.md` | KEEP CURRENT | Reference doc, no obvious drift found |
| `docs/gitbook/reference/execution-adapter-api.md` | KEEP CURRENT | Reference doc, still aligned with adapter role |
| `docs/gitbook/reference/http-api.md` | KEEP CURRENT | Still aligned with HTTP surfaces |
| `docs/gitbook/reference/risk-shell-api.md` | KEEP CURRENT | Reference doc, no obvious drift found |
| `docs/gitbook/reference/strategy-signal-api.md` | KEEP CURRENT | Reference doc, no obvious drift found |
| `docs/gitbook/reference/telegram-api.md` | KEEP CURRENT | Still relevant while Telegram remains in runtime |
| `docs/gitbook/SUMMARY.md` | KEEP CURRENT | Table of contents file |
| `docs/HOWTO-manual-replay.md` | UPDATE | References untracked candle files and a Pi framing in title |
| `docs/manual-replay.md` | UPDATE | References untracked candle files and reproducibility depends on local artifacts |
| `docs/live_env_defaults.md` | UPDATE | Current, but still references maintenance-hook knobs tied to local scripts |
| `docs/pi_ops.md` | UPDATE | Honest after Pass 1, but should likely become `local_host_ops.md` or legacy/local-only |
| `docs/gitbook/guides/developer-guide.md` | UPDATE | Still contains Pi/local-host wording and `deploy_pi.sh` emphasis |
| `docs/gitbook/guides/quickstart.md` | UPDATE | Still has a stale Pi operator section and wrong `bash run_live_logged.sh` path |
| `docs/gitbook/ops/runbook.md` | UPDATE | More accurate after Pass 1, but still strongly local-host oriented |
| `docs/gitbook/README.md` | UPDATE | Still says the bot “maintains the perp account on the Pi” and mentions Pi deployment |
| `docs/gitbook/reference/environment.md` | UPDATE | Still centralizes env examples under `systemd/env/*`; not wrong, but local-host specific |
| `docs/ASTER_V3_AUDIT_AND_REDESIGN.md` | ARCHIVE | Valuable historical design document, not current operator doc |
| `docs/guerilla_strategy.md` | ARCHIVE | Not referenced by code; `docs/live_env_defaults.md` explicitly says guerilla lanes were removed from live routing |
| `docs/telegram_refactor_codex_prompt.md` | ARCHIVE | Prompt artifact, not durable runtime documentation |
| `docs/repo_cleanup_audit.md` | KEEP CURRENT | Cleanup history from Pass 1; useful during migration cleanup |
| `docs/repo_cleanup_patch_01.md` | KEEP CURRENT | Cleanup history record |
| `docs/developer_handoff.md` | REMOVE CANDIDATE | Empty file |

## 8. Scripts Review Table

| Script | Current role after Pass 1 | Classification | Why |
| --- | --- | --- | --- |
| `scripts/run_live_logged.sh` | real foreground launcher with env loading and rotating logs | KEEP | Still useful as a local/manual run convenience |
| `scripts/stream_to_rotating_log.sh` | shared rotating log helper | KEEP | Still directly used by `run_live_logged.sh`, `run_long_logged.sh`, `run_short_logged.sh` |
| `scripts/smoke.sh` | scanner API smoke test | KEEP | Still used by Makefile and useful for local verification |
| `scripts/run_long_logged.sh` | logged wrapper for `cmd/long` | UPDATE | Useful, but undocumented and narrow |
| `scripts/run_short_logged.sh` | logged wrapper for `cmd/short` | UPDATE | Useful, but undocumented and narrow |
| `scripts/deploy_pi.sh` | builds binaries and syncs env examples to `/opt/aster` | UPDATE | Still useful locally, but name and `/opt/aster` semantics are pre-GCP |
| `scripts/maintenance_midnight.sh` | `git fetch/pull` + reconcile hook | REMOVE CANDIDATE | Old host mutation behavior tied to local maintenance hooks |
| `scripts/maintenance_eod.sh` | `git fetch/pull` + reconcile hook | REMOVE CANDIDATE | Same as above |
| `scripts/reconcile_on_boot.sh` | prints state-file existence and `tmux ls` | REMOVE CANDIDATE | Thin leftover from removed orchestration model |
| `scripts/start_bot.sh` | wrapper around `deploy_pi.sh` then printed guidance | REMOVE CANDIDATE | Adds little beyond calling `deploy_pi.sh` directly |
| `scripts/restart_aster_stack.sh` | prints manual guidance only | REMOVE CANDIDATE | No longer restarts anything |
| `scripts/stop_bot.sh` | prints manual guidance only | REMOVE CANDIDATE | No longer stops anything |
| `scripts/tmux_aster.sh` | full tmux dashboard for deleted orchestration style | REMOVE CANDIDATE | Most obvious remaining dead script from old Pi model |

### Script findings

- `systemd/env/live.env.example` still points `LIVE_MAINT1_HOOK` and `LIVE_MAINT2_HOOK` at the maintenance scripts. That means those scripts are not isolated dead files; their removal should be paired with env-template cleanup.
- `tmux_aster.sh` is now the clearest remaining orchestration orphan. It still builds a full multi-pane tmux view around a model that was intentionally removed in Pass 1.

## 9. High-Confidence Remove Candidates

- `_NOTES_/Logic.md`
- `_NOTES_/ProjectLayout.md`
- `_NOTES_/Timeline.md`
- `_NOTES_/avantis_arb.md`
- `_NOTES_/avantis_resource.md`
- `_NOTES_/githubstuff.md`
- `scripts/tmux_aster.sh`
- `scripts/reconcile_on_boot.sh`
- `scripts/restart_aster_stack.sh`
- `scripts/stop_bot.sh`
- `docs/developer_handoff.md`
- `reports/` generated outputs other than deliberately preserved examples/archive write-ups
- `ui/scanner-dashboard/.next/` local build artifacts
- `data/SPKUSDT_1m_2026-04-23.csv`
- `data/SPKUSDT_1m_2026-04-23.json`
- `data/SPKUSDT_1m_2026-04-23_full.csv`
- `data/SPKUSDT_1m_2026-04-23_full.json`
- `data/scanner_chip.jsonl`

## 10. Medium-Confidence Remove Candidates

- `scripts/start_bot.sh`
- `scripts/deploy_pi.sh` if you decide the repo should stop shipping any `/opt/aster` helper at all before GCP
- `scripts/maintenance_midnight.sh`
- `scripts/maintenance_eod.sh`
- `scripts/run_long_logged.sh`
- `scripts/run_short_logged.sh`
- `docs/pi_ops.md` after it is either renamed or replaced by local/GCP guidance
- `docs/guerilla_strategy.md`
- `docs/telegram_refactor_codex_prompt.md`
- tracked `reports/system_patch_checklist.md`
- tracked `reports/trade_giveback_2026-04-20.csv`
- tracked `data/POWERUSDT_1m.csv`
- tracked `data/POWERUSDT_whales.jsonl`

## 11. Move / Archive Recommendations

- Move `reports/manual_replay_comparison_rave_asteroid_chip_2026-04-22.md` to `docs/archive/`
- Move `reports/system_patch_checklist.md` to `docs/archive/`
- Move `reports/trade_giveback_2026-04-20.csv` to `docs/archive/` if you want to preserve it
- Move durable incident and audit markdowns from `reports/` into `docs/archive/`
- Move manual replay candle inputs into `docs/examples/manual-replay/` or `testdata/manualreplay/`
- Move `POWERUSDT_1m.csv` and `POWERUSDT_whales.jsonl` into `docs/examples/backtest/` or `testdata/backtest/` if you want them preserved as curated examples
- Consider moving `ui/scanner-dashboard/` to a clearer top-level product/apps folder later if you decide it is part of the future GCP operator surface

## 12. What Should Be Preserved For GCP Migration

- `ui/scanner-dashboard/` source tree, if you want a seed for a future operator dashboard
- core backtest example fixtures currently used by defaults:
  - `data/ASTERUSDT_1m.csv`
  - `data/BNBUSDT_1m.csv`
  - `data/BTCUSDT_1m.csv`
  - `data/ETHUSDT_1m.csv`
  - `data/HYPEUSDT_1m.csv`
  - `data/SOLUSDT_1m.csv`
  - `data/scanner.jsonl`
- `reports/manual_replay_trades_rave_asteroid_chip.csv`, because current docs depend on it
- current runtime docs and reference docs that still match the retained command surface

## 13. Proposed Cleanup Pass 2 Deletion Patch

### Phase 1: Safe file hygiene

- Remove `_NOTES_/` entirely
- Remove `docs/developer_handoff.md`
- Remove `ui/scanner-dashboard/.next/` local artifacts if present in working trees
- Remove generated SPK/scanner-local files under `data/`

### Phase 2: Reports normalization

- Keep only the deliberately preserved example inputs or archive write-ups
- Move durable markdown write-ups into `docs/archive/`
- Remove remaining generated `reports/` content from git and continue ignoring it

### Phase 3: Example data normalization

- Move manual replay and optional backtest sample datasets into an explicit examples/testdata structure
- Update `docs/manual-replay.md` and `docs/HOWTO-manual-replay.md` to point at the new committed example paths

### Phase 4: Local-host script pruning

- Remove `tmux_aster.sh`, `reconcile_on_boot.sh`, `restart_aster_stack.sh`, and `stop_bot.sh`
- Decide whether to remove or rename `deploy_pi.sh`, `start_bot.sh`, and the maintenance scripts
- If maintenance scripts are removed, update `systemd/env/live.env.example` to remove or neutralize hook references

### Phase 5: Docs cleanup

- Archive `guerilla_strategy.md` and `telegram_refactor_codex_prompt.md`
- Rewrite `docs/gitbook/README.md` and `docs/gitbook/guides/quickstart.md` so they stop centering the Pi/local-host story
- Reclassify `pi_ops.md` as either legacy/local-host or replace it later with GCP deployment docs

## 14. Concise Terminal Summary

- Safe remove now: `_NOTES_/`, `docs/developer_handoff.md`, `scripts/tmux_aster.sh`, `scripts/reconcile_on_boot.sh`, printed-guidance wrappers, generated SPK/scanner-local files, `ui/scanner-dashboard/.next/`, most ignored `reports/` outputs
- Archive/move: manual replay examples, durable incident/audit markdowns from `reports/`, `guerilla_strategy.md`, `telegram_refactor_codex_prompt.md`, tracked historical reports
- Keep: core runtime docs, `ui/scanner-dashboard/` source, `run_live_logged.sh`, `stream_to_rotating_log.sh`, `smoke.sh`, current backtest default candle fixtures, `data/scanner.jsonl`, `cmd/rr`
- Needs human decision: whether the dashboard is part of future GCP product surface, whether to keep any `/opt/aster` helper scripts, whether `POWERUSDT_*` files are worth preserving as curated examples, and whether `reports/` should be fully emptied except for archive material
