# Paper Runtime UTC Analysis Plan

This is the repeatable post-reset workflow for paper-runtime review.

## Reset Window

- Trading review window: `00:00 UTC` to `23:59:59 UTC`
- On the user's schedule, `19:00 CDT` is `00:00 UTC` during daylight savings.

## Goals

1. Measure baseline performance for the full UTC day.
2. Identify which setups, sessions, and exit behaviors are helping or hurting.
3. Separate entry problems from exit problems.
4. Turn the findings into concrete `entry`, `exit`, `TP`, and `SL` adjustments.

## Data Sources

- Primary: `out/paper_closed_trades.jsonl`
- Legacy fallback: extracted trade CSVs under `out/logs_*`
- Context: runtime logs for rejected candidates, quality flags, and missed opportunities

## Report Command

Closed-trade ledger:

```bash
GOCACHE=$(pwd)/.gocache go run ./cmd/paperreport \
  --closed-trades-jsonl out/paper_closed_trades.jsonl \
  --out-dir out/paper_report_latest
```

Legacy CSV fallback:

```bash
GOCACHE=$(pwd)/.gocache go run ./cmd/paperreport \
  --trades-csv out/logs_YYYY-MM-DD_to_YYYY-MM-DD/trades_*.csv \
  --out-dir out/paper_report_legacy
```

## What To Review First

1. Overall:
   - trade count
   - win rate
   - net realized
   - average hold
2. By setup:
   - `micro_pullback_continuation`
   - `breakout_retest`
   - `deep_pullback_reclaim`
   - `none`
3. By session:
   - `ASIA_BREAKOUT`
   - `LONDON_OPEN`
   - `NY_OPEN`
   - `NY_EXPAND`
   - `UTC_OFF_HOURS`
4. By exit reason and exit improvement action.

## Entry Diagnosis Rules

- `resolve_setup_before_entry`
  - setup was unresolved / `none`
- `enter_earlier_or_expire_candidate`
  - candidate was stale
- `wait_for_pullback_after_extension`
  - entry was too extended
- `restrict_off_hours_autonomy`
  - weak off-hours trade quality
- `prefer_closer_to_vwap_or_reclaim`
  - entry too far from VWAP / location

## Exit Diagnosis Rules

- `let_runner_breathe_after_partial`
  - the market kept going materially after exit
- `hold_secondary_target_longer`
  - there was more room after exit, but not enough for a full redesign
- `tighten_after_initial_proof`
  - trade showed proof, then round-tripped into a loser
- `entry_failed_fast_no_exit_issue`
  - stop-out was mostly an entry quality problem, not an exit logic problem

## Decision Order After Each Report

1. Remove low-quality entry lanes first.
2. Tighten stale/extended entry rules second.
3. Adjust runner/partial behavior only after the entry set is cleaner.
4. Re-check whether stronger exit freedom increases churn or actually improves net PnL.

## Current Bias Based On Baseline

- Prefer:
  - `micro_pullback_continuation`
  - `breakout_retest`
  - clean VP/VWAP/location continuation
- Penalize or block:
  - `setup=none`
  - stale candidates
  - heavily extended entries
  - weak `UTC_OFF_HOURS` autonomy

