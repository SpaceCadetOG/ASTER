# Repo Cleanup Patch 02

Cleanup Pass 2 focused on the remaining ambiguous repo surfaces before GCP
infrastructure work begins.

## Files removed

- `_NOTES_/`
- `docs/developer_handoff.md`
- `data/SPKUSDT_1m_2026-04-23.csv`
- `data/SPKUSDT_1m_2026-04-23.json`
- `data/SPKUSDT_1m_2026-04-23_full.csv`
- `data/SPKUSDT_1m_2026-04-23_full.json`
- `data/scanner_chip.jsonl`
- `scripts/tmux_aster.sh`
- `scripts/reconcile_on_boot.sh`
- `scripts/restart_aster_stack.sh`
- `scripts/stop_bot.sh`
- `scripts/start_bot.sh`
- `scripts/maintenance_midnight.sh`
- `scripts/maintenance_eod.sh`
- `ui/scanner-dashboard/.next/` local build artifacts

## Files moved into examples

Manual replay examples moved to `docs/examples/manual-replay/`:
- `ASTEROIDUSDT_1m.csv`
- `CHIPUSDT_1m.csv`
- `RAVEUSDT_1m.csv`
- `manual_replay_trades_rave_asteroid_chip.csv`

Optional backtest examples moved to `docs/examples/backtest/`:
- `POWERUSDT_1m.csv`
- `POWERUSDT_whales.jsonl`

## Files moved into archive

Archived under `docs/archive/`:
- `reports/manual_replay_comparison_rave_asteroid_chip_2026-04-22.md`
- `reports/system_patch_checklist.md`
- `reports/trade_giveback_2026-04-20.csv`
- `docs/guerilla_strategy.md`
- `docs/telegram_refactor_codex_prompt.md`

## Scripts removed

Removed the remaining stale local-orchestration and maintenance helpers:
- `scripts/tmux_aster.sh`
- `scripts/reconcile_on_boot.sh`
- `scripts/restart_aster_stack.sh`
- `scripts/stop_bot.sh`
- `scripts/start_bot.sh`
- `scripts/maintenance_midnight.sh`
- `scripts/maintenance_eod.sh`

Kept intentionally:
- `scripts/deploy_pi.sh`
- `scripts/run_live_logged.sh`
- `scripts/run_long_logged.sh`
- `scripts/run_short_logged.sh`
- `scripts/stream_to_rotating_log.sh`
- `scripts/smoke.sh`

## Makefile changes

- removed the stale `deploy` target
- replaced misleading serial `run`, `dev`, and `devq` targets with:
  - `run-long`
  - `run-short`
  - `dev-long`
  - `dev-short`
  - `devq-long`
  - `devq-short`
- renamed the narrow scanner build target to `build-scanners`
- kept `smoke`, `test`, `fmt`, `backtest`, and all `exec-*` targets

## Docs updated

Updated:
- `docs/manual-replay.md`
- `docs/HOWTO-manual-replay.md`
- `docs/pi_ops.md`
- `docs/live_env_defaults.md`
- `docs/gitbook/README.md`
- `docs/gitbook/guides/quickstart.md`
- `docs/gitbook/guides/developer-guide.md`
- `docs/gitbook/ops/runbook.md`
- `systemd/env/live.env.example`

These updates remove deleted-script references, point examples at the new
`docs/examples/...` paths, and reframe host/Pi material as interim local or
legacy host operations rather than the future deployment model.

## Maintenance hook env handling

The maintenance window envs remain documented, but `LIVE_MAINT1_HOOK` and
`LIVE_MAINT2_HOOK` did not appear in the current Go runtime during this pass.

Current handling:
- env examples no longer mention hook path variables
- repo docs no longer mention repo-shipped maintenance hook scripts
- the remaining documented maintenance settings are the time-window controls
  such as `LIVE_MAINT1_*`, `LIVE_MAINT2_*`, and related EOD timing knobs

## Preserved intentionally

Dashboard source preserved:
- `ui/scanner-dashboard/`

Backtest fixtures preserved:
- `data/ASTERUSDT_1m.csv`
- `data/BNBUSDT_1m.csv`
- `data/BTCUSDT_1m.csv`
- `data/ETHUSDT_1m.csv`
- `data/HYPEUSDT_1m.csv`
- `data/SOLUSDT_1m.csv`
- `data/scanner.jsonl`

Also preserved:
- `cmd/rr`

## Deferred after Pass 2

- decide whether `ui/scanner-dashboard/` becomes part of the future operator UI
- decide whether `docs/archive/` should stay in the main docs tree long-term
- revisit any remaining local/host wording once the GCP deployment model exists
- evaluate whether `scripts/deploy_pi.sh` should be renamed or replaced after
  the migration is complete
