# Guerilla Strategy

This document captures the current strategy spec derived from the strongest
March 2026 trades:

- `guerilla_long_runner`: rare, high-conviction long leader pyramids
- `guerilla_short_sniper`: default short continuation / failed-bounce entries
- `guerilla_long_sniper`: default long continuation / failed-breakdown entries
- `guerilla_short_runner`: rare, high-conviction short leader pyramids

The main lesson from the trade review is simple:

- winners came from strong names with smart entries
- losers mostly came from late entries, not terrible exits
- the bot should optimize for sniper entry location before it optimizes exits

## Core Principles

- Default bias is short. Longs must clear a much higher bar.
- Entry quality matters more than squeezing the perfect exit.
- Do not chase breakouts on first touch.
- Do not short the floor unless the tape is in true unwind mode.
- Long pyramids may add only while green. Never average down.
- Separate runner exits from scalp / no-follow-through exits.

## Prior-Hour Range Filter

Every candidate should compute:

- `high1h`
- `low1h`
- `range1h = high1h - low1h`
- `rangeLoc1h = (mark - low1h) / max(range1h, tiny)`

Interpretation:

- `rangeLoc1h ~= 0.00`: price is near the prior-hour low
- `rangeLoc1h ~= 1.00`: price is near the prior-hour high

Strategy use:

- longs should usually enter in the upper half of the prior-hour range, but not
  on a blind overextension above the high
- shorts should usually enter off a bounce, not on a fresh hourly low

## Guerilla Long Runner

This is the SIREN-style trade.

### Candidate Requirements

- top long leader only
- `grade >= B`
- `score >= 95`
- `slope >= 0.50`
- `dayUTC >= +15%`
- `entry_style` in:
  - `pullback_long`
  - `breakout_hold_long`
  - `momentum_ignite_long`
- structure must show one of:
  - `break_hold`
  - `reclaim_hold`
  - `retest_hold`

### Entry Filters

- Base entry allowed only when `0.65 <= rangeLoc1h <= 1.02`
- Reject first-touch breakout candles
- Require a pullback or reclaim hold after the breakout impulse
- Reject if the entry is more than `0.50%` above `high1h` unless runner override

### Runner Override

Allow a slightly extended entry when all of the following are true:

- `score >= 110`
- `dayUTC >= +20%`
- still the top leader for at least 2 consecutive scans
- structure is `break_hold` or `reclaim_hold`
- the pullback low still held above the prior-hour high

### Position Sizing

- use fixed units
- tranche 1 opens the position
- max 4 tranches
- add only when:
  - current blended position is green
  - symbol is still the top long leader
  - structure reasserts after a pullback / reclaim

### Exit Profile

- no early `NO_FOLLOW_THROUGH` exit unless the runner never confirms
- do not arm an aggressive trail immediately
- keep holding while:
  - score remains elevated
  - slope remains positive
  - structure is intact
  - same-side sponsorship or confluence refresh is active
- full exit on:
  - structure loss plus momentum fade
  - leader status loss for multiple scans
  - catastrophic blended stop breach

### Risk Model

Do not use the default `hybrid_stop_too_wide` gate for this branch. Use a
runner-specific stop based on:

- blended entry
- prior pullback low / reclaim level
- max capital-at-risk per full pyramid

## Guerilla Short Sniper

This is the LYN / PIPPIN / DEGO template.

## Guerilla Long Sniper

This mirrors the short sniper but for long failed-breakdown entries.

### Candidate Requirements

- top long candidate, usually top 1–4
- `score >= 78`
- `slope >= 0.06`
- `dayUTC >= +6%`
- `vol_ratio >= 1.10`
- at least one failed breakdown / reclaim

### Entry Filters

- structure confirms near VWAP/EMA
- OFI supportive or improving
- avoid `avoid_chase` unless failure count is strong

## Guerilla Short Runner

This mirrors the long runner but for rare short leaders.

### Candidate Requirements

- top short leader only
- `score >= 105`
- `slope >= 0.28`
- `dayUTC <= -12%`
- `vol_ratio >= 1.50`
- strong ask wall or consumption pressure

### Position Sizing

- allow pyramids while green and still top leader

### Candidate Requirements

- top short candidate, usually top 1 or top 2
- `grade >= A`
- `score >= 100`
- `dayUTC <= -6%`
- `entry_style` in:
  - `pullback_short`
  - `breakout_hold_short`
  - `leader_unwind_short`
- require at least one:
  - `failed_bounce_count >= 1`
  - `failed_reclaim_count >= 1`
  - strong unwind / ask-pressure context

### Entry Filters

- Preferred range location: `0.25 <= rangeLoc1h <= 0.65`
- Reject if `rangeLoc1h < 0.15`
- Only allow a fresh-low short if all are true:
  - `score >= 120`
  - `dayUTC <= -10%`
  - `failed_bounce_count >= 2`

### Bounce-Fail Structure

Look for:

- strong down impulse already in place
- 10% to 30% bounce of the latest downswing
- reclaim attempt fails at VWAP, prior breakdown, or local pivot
- lower high forms and price rolls back over

### Position Sizing

- 1 core unit by default
- optional 1 add only if:
  - first unit is green
  - bounce fails again
  - same symbol remains among the top short leaders

### Exit Profile

- if the trade does not progress quickly enough, allow `NO_FOLLOW_THROUGH`
- if same-side confluence remains strong after confirmation, convert to runner
- trail should tighten more slowly when sponsorship / confluence refresh is live
- avoid pre-funding exits when the short is still clearly sponsored

## Bad Trade Filters Learned From OOS Review

### BANANAS31 Long

- lost because entry chased above the prior-hour high
- only had a tiny positive window before immediate failure
- rule added: no blind first-touch breakout long above `high1h`

### SOL Short

- lost because entry sold near the prior-hour low instead of into a bounce
- had a small profitable window, then mean-reverted
- rule added: no short entry at the floor without true unwind conditions

## Suggested Implementation Order

1. Add prior-hour range metrics and impulse retrace metrics to candidates.
2. Add `guerilla_long_runner` and `guerilla_short_sniper` as explicit strategy
   branches.
3. Allow same-symbol pyramids only for approved long-runner adds.
4. Add a runner-specific stop model that bypasses the default hybrid stop gate.
5. Split exits into `runner`, `scalp`, and `no_follow_through` profiles.
6. Extend the backtester to support:
   - same-symbol adds
   - blended entry tracking
   - prior-hour location filters
   - strategy-specific exit profiles

## Existing Code Hooks

Useful current hooks already exist in the live path:

- candidate context and logs in `cmd/live/main.go`
- structure tagging for `break_hold`, `reclaim_hold`, and `retest_hold`
- confluence refresh and swing hold logic
- trail profile multipliers
- same-symbol open guard
- hybrid stop rejection

The strategy should be implemented by extending those hooks rather than
building a separate execution path from scratch.
