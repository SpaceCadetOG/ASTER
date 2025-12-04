# 🧠 Momentum-Fibonacci Trade Logic Specification

This file defines the **exact trade logic** that ties together:
- The Momentum Scanner (Δ%, volume, funding)
- The Fibonacci-based trade planner
- Your PnL/ROE/R:R calculator and risk manager

Use this as a structured, repeatable decision tree for both manual and automated trading.

---

## 🧭 0) Pre-Filters (Scanner → Candidates)

A coin becomes **tradable** only if all conditions are true:

| Metric | Condition | Reason |
|---------|------------|--------|
| Δ 24h | ≥ 25% and ≤ 90% | Ensures volatility but not exhaustion |
| Volume (24h) | ≥ $10M | Avoid illiquid wicks |
| Funding | ≤ 0.25% | Avoid overcrowded longs |
| Momentum Score | ≥ 70 | Confirms trend strength |
| Spread | Tight | Reliable fills |

---

## ⚙️ 1) Trend Bias

Use **15m or 1h** chart:
- **EMA9 > EMA21** and price > VWAP → **Bias = LONG**
- Else → no long trades.

---

## 🧩 2) Define Active Swing (Structure Anchor)

On **5m (or 15m)** chart:
- Identify latest impulse: **SwingLow → SwingHigh**
- Compute Fibonacci: **0.382, 0.5, 0.618, 1.0, 1.272, 1.618**

```go
fib := ComputeFibLevels(swingLow, swingHigh)
```

---

## 🧱 3) Build Plan From Fibonacci Confluence

| Element | Logic |
|----------|--------|
| **Entry Zone** | 0.5–0.618 if overlaps with demand zone or EMA cluster |
| **Stop-Loss** | Below 0.618 by 0.5–1% buffer or below structure low |
| **Targets** | TP1 = 1.0, TP2 = 1.272, TP3 = 1.618 |
| **R:R Requirement** | ≥ 2:1 to TP1 or skip trade |

```go
plan := NewFibPlanLong(fib, entryBias=0.0, stopBufferPct=0.8)
require(plan.RRToTP1 >= 2.0)
```

---

## 💰 4) Position Sizing (Hard Risk)

- Account risk = 1% (example)
- Risk% = `(Entry - Stop) / Entry`
- Qty = `(AccountRisk$ * Leverage) / (Entry * Risk%)`

Never exceed max leverage or margin limits.

---

## 🎯 5) Execution (1m Precision Inside 5m Setup)

**Only enter inside the 0.5–0.618 zone** once confirmation appears.

**Triggers:**
- ✅ 1m bullish engulfing close + rising volume
- ✅ VWAP reclaim inside zone + 5m candle closes above EMA9

**Orders:**
- Limit or market entry
- OCO:
  - Stop at planned SL
  - TP1 (50% size) at Fib 1.0
  - TP2 (25–35%) at Fib 1.272
  - Trail remaining to Fib 1.618

---

## 🧠 6) Management Rules

| Condition | Action |
|------------|--------|
| Price hits 1× risk | Move SL → Breakeven |
| TP1 hit | Take 50%, move SL → BE |
| TP2 hit | Take 25–35%, tighten trail |
| Price between TP2–TP3 | Trail under EMA9 (15m) − 1% |

**Trail logic**
- < +25% profit → trail under last 5m swing low − 0.3–0.5%
- 25–60% profit → trail = EMA9 (5m) − 0.5%
- > 60% profit → trail = EMA9 (15m) − 1%

Update only **on candle close**.

---

## 🔄 7) Retracement Management

| Type | Behavior | Action |
|------|-----------|--------|
| **A. Shallow** | Holds 0.382 or EMA9 (5m) | Hold & trail |
| **B. Moderate** | Tags 0.5–0.618 with volume rebound | Add (≤ 50%) |
| **C. Deep** | Closes below 0.618 & EMA21 (15m) | Exit & wait for reclaim |

Re-entry only if:
- EMA9 (15m) reclaimed,
- Volume increases,
- Candle closes above prior LH.

---

## 📊 8) Order Book & Liquidity

| Observation | Meaning | Action |
|--------------|----------|--------|
| Bids cluster at 0.5–0.618 | Strong demand | Safer to fill longs |
| Asks build near 1.0/1.272 | Take-profits ahead | Scale out partials |
| Bid walls vanish | Likely dip | Watch stop or trail |
| Ask walls vanish | Vol breakout | Let winner run |

---

## ⛔ 9) Exit Rules

Exit all if:
- Close below EMA9 (15m) **and** below trailing stop
- Funding > 0.30% & momentum stalls
- Two consecutive lower highs (5m)
- News/liquidity event breaks structure

---

## 🔁 10) Re-entry Protocol

- Wait for reclaim of EMA9 (15m)
- Confirm volume + OB bid return
- Re-enter ½ size → add full after break of prior LH

---

## 📉 11) Short Symmetry

For shorts:
- Fib drawn **high → low**
- Entry zone = retrace to 0.5–0.618
- Stop above 0.618
- Targets = 0.0 / −0.272 / −0.618
- EMAs flipped
- OB focus on ask walls

---

## 💻 12) Pseudo-Code Overview

```go
if !PassesScannerFilters(sym) { return NO_TRADE }
if !IsLongBias(ema9_15m, ema21_15m, vwap_15m) { return NO_TRADE }

low, high := DetectSwing5m(sym)
fib := ComputeFibLevels(low, high)
plan := NewFibPlanLong(fib, 0.0, 0.8)
if plan.RRToTP1 < 2.0 { return NO_TRADE }

wait until PriceInZone(plan.EntryZone) && OneMinTriggerOK() && FiveMinStructureOK()

qty := PositionSizeUSD(accountRisk$, leverage, plan.Entry, plan.Stop)
PlaceEntry(sym, qty, plan.Entry)
PlaceOCO(sym, stop=plan.Stop, tp1=fib.High, tp2=fib.E1272)

for each closed candle {
    UpdateTrail(sym, profit%, ema9_5m, ema9_15m, lastSwings)
    if HitTP1 { TakePartial(50%); MoveStopToBE() }
    if ExitConditions() { CloseAll(); break }
}
```

---

## 📈 13) Example (COAI-USD)

| Stage | Price | Action | R:R |
|--------|--------|---------|-----|
| Entry | 4.10 | Fib zone 0.5–0.618 | – |
| Stop | 3.80 | Below structure | – |
| TP1 | 5.33 | Previous high | 4.1:1 |
| TP2 | 5.96 | Fib 1.272 | 6.2:1 |
| TP3 | 6.60 | Fib 1.618 | 8.3:1 |

With 5× leverage:
- TP1 = +30% ROE  
- TP2 = +45% ROE  
- TP3 = +60%+ ROE (depending on trail).

---

## ❌ 14) What NOT To Do

| Mistake | Why It’s Fatal |
|----------|----------------|
| Entering at exact 0.618 | That’s where stop-hunts live |
| Adding on vertical candles | You’re buying exhaustion |
| Updating trail intra-bar | You’ll get wicked out |
| Trading illiquid moves | Can’t manage risk accurately |

---

## ✅ 15) TL;DR Decision Flow

```text
SCAN ✅ → Trend ✅ → Swing ✅ → Fib Plan ✅
→ Entry zone hit + 1m trigger ✅
→ Place OCO (SL + TP1/TP2)
→ Manage via trail per phase
→ Exit on structure break
```

---

**Result:**  
One unified playbook combining **momentum scanner + Fibonacci structure + risk management**, executable by both human and algorithmic systems.

---

*Built from live COAI-USD trade analysis (October 2025).*
