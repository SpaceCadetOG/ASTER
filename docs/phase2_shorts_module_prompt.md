ASTER Phase 2 Shorts Module

Add this as a second-phase short filter layer before allowing autonomous short entries.

Goal:
Stop the bot from shorting late panic lows while still allowing high-quality shorts when the breakdown is fresh or when a relief bounce fails.

This phase should separate short setups into these buckets:

1. `fresh_breakdown_short`
2. `late_chase_short`
3. `failed_bounce_short`
4. `post_pump_breakdown`
5. `post_pump_fresh_breakdown`

Do not treat all bearish-looking conditions the same.

## 1. Fresh Breakdown Short

This is the only direct short continuation that can be allowed without waiting for a bounce.

Condition example:

- `pct24h < 0`
- `pct24h > -20`
- `pct4h < 0`
- `pct1h < 0`
- `pct1h > -3`

Suggested first rule:

```text
if pct24h < 0
and pct24h > -20
and pct4h < 0
and pct1h < 0
and pct1h > -3:
    short_bucket = "fresh_breakdown_short"
    direct_short_allowed = true
```

Meaning:
The market is bearish, but the bot is not shorting after a massive dump has already happened.

## 2. Late / Chase All-Red Short

This blocks shorts where all timeframes are already heavily dumped.

Condition example:

- `pct24h = -30`
- `pct4h = -12`
- `pct1h = -5`

Suggested first rule:

```text
if pct24h < -20
and pct4h < -8
and pct1h < -3:
    short_bucket = "late_chase_short"
    direct_short_allowed = false
    require_confirmation = "failed_bounce"
```

Reason:
This is likely a late short into panic lows. It may still go lower, but risk/reward is bad and relief bounce risk is high.

Telegram/debug label:
`SHORT_BLOCK_LATE_CHASE`

Human reason:
All-red downside already extended; waiting for bounce failure.

## 3. Failed Bounce Short

This is the preferred short setup after downside extension.

Condition:

- `24h` bearish or post-pump reversal context
- `4h` bearish
- price bounces from local low
- bounce fails near VWAP / prior support / resistance / breakdown level
- `5m` or `15m` turns back down

Suggested logic:

```text
if short_bucket in ["late_chase_short", "post_pump_breakdown"]
and bounce_from_local_low_pct >= 2
and failed_reclaim == true
and lower_timeframe_turn_down == true:
    short_bucket = "failed_bounce_short"
    direct_short_allowed = true
```

Minimum failed-bounce proof options:

- price rejects VWAP
- price fails to reclaim breakdown level
- `5m` lower high forms
- `15m` candle closes back below trigger level
- sell volume expands on rejection

Telegram/debug label:
`SHORT_ALLOWED_FAILED_BOUNCE`

Human reason:
Relief bounce failed; short allowed after rejection.

## 4. Post-Pump Breakdown Short

This handles the special case where the token is still massively green on `24h` but is now violently selling off.

Example:

- `pct24h = +100`
- `pct4h = -35`
- `pct1h = -12`

This is not normal all-red short alignment. It is post-pump breakdown / intraday reversal.

Suggested classification:

```text
if pct24h > 60
and pct4h < -15
and pct1h < -5:
    short_bucket = "post_pump_breakdown"
    direct_short_allowed = false
    require_confirmation = "failed_bounce"
```

Reason:
The token pumped hard all day, then dumped hard. The short can be valid, but the bot should not market-short immediately after a `4h -35% / 1h -12%` flush. It should wait for bounce/failure.

Telegram/debug label:
`SHORT_BLOCK_POST_PUMP_CHASE`

Human reason:
Post-pump dump already extended; waiting for failed bounce.

## 5. Post-Pump Fresh Breakdown Exception

Allow direct short only if the post-pump breakdown is still early.

Example:

- `pct24h = +100`
- `pct4h = -8`
- `pct1h = -4`

Suggested rule:

```text
if pct24h > 60
and pct4h < 0
and pct4h > -15
and pct1h < -2
and pct1h > -6
and just_lost_vwap_or_structure == true:
    short_bucket = "post_pump_fresh_breakdown"
    direct_short_allowed = true
```

Reason:
The token is still up big on the day, but the reversal may be just starting.

Telegram/debug label:
`SHORT_ALLOWED_POST_PUMP_BREAKDOWN`

Human reason:
Post-pump structure break is fresh; short allowed.

## 6. Size / Risk Adjustments

Shorts should not all use the same confidence.

Suggested confidence/risk rules:

```text
fresh_breakdown_short:
    confidence = normal
    size_multiplier = 1.00

failed_bounce_short:
    confidence = high
    size_multiplier = 1.00

post_pump_fresh_breakdown:
    confidence = medium
    size_multiplier = 0.75

post_pump_breakdown waiting for failed bounce:
    confidence = blocked_until_confirmed
    size_multiplier = 0.00

late_chase_short:
    confidence = blocked_until_confirmed
    size_multiplier = 0.00
```

If a late/chase or post-pump breakdown later becomes a confirmed failed-bounce short:

```text
size_multiplier = 0.75 or 1.00 depending on score/confluence
```

## 7. Management for These Shorts

For post-pump and failed-bounce shorts, use faster protection.

Suggested first version:

```text
if short_bucket in ["post_pump_fresh_breakdown", "failed_bounce_short"]:
    if max_r_seen >= 0.75:
        tighten risk
    if max_r_seen >= 1.0:
        stop cannot remain full-risk
```

Reason:
These names can move violently. Do not let a good short become a full loser after meaningful favorable move.

## 8. Ledger Fields to Add

Persist these on the trade plan at entry:

- `short_bucket`
- `short_filter_reason`
- `direct_short_allowed`
- `require_confirmation`
- `pct24h_at_entry`
- `pct4h_at_entry`
- `pct1h_at_entry`
- `bounce_from_local_low_pct`
- `failed_bounce_confirmed`
- `post_pump_breakdown`
- `late_chase_blocked`

These must be frozen at entry and carried into the close ledger.

## 9. Test Cases

Add tests for:

1. `24h -30, 4h -12, 1h -5`
   - bucket = `late_chase_short`
   - direct short blocked
   - requires failed bounce
2. `24h -8, 4h -3, 1h -1.5`
   - bucket = `fresh_breakdown_short`
   - direct short allowed
3. `24h +100, 4h -35, 1h -12`
   - bucket = `post_pump_breakdown`
   - direct short blocked
   - requires failed bounce
4. `24h +100, 4h -8, 1h -4`
   - bucket = `post_pump_fresh_breakdown`
   - direct short allowed only if VWAP/structure break is confirmed
5. Late/chase short becomes allowed after failed bounce confirmation.
6. Post-pump breakdown becomes allowed after failed bounce confirmation.

## 10. Important Design Rule

Do not simply block shorts.

Classify the short context.

The bot should understand the difference between:

- fresh breakdown
- late downside chase
- failed bounce short
- post-pump breakdown

Phase 2 goal:
Reduce bad short entries caused by selling panic lows, while still allowing clean breakdowns and failed-bounce shorts.
