# Flash-Loan AVNT/USDC Arbitrage — Builder Notes (Base)

This doc is a copy-ready blueprint for executing **Aave v3 flash-loan** (or **Uniswap v3 flash-swap**) arbitrage between **Aerodrome** and **Uniswap v3** on **Base**. It includes clear pseudocode, guardrails, numeric examples, and the profit-split auto-compound logic back into **Aave**.

---

## 🔧 Prereqs (fill these before coding)
- **RPCs**: Base public + private (Flashbots/MEV-Blocker for Base or similar)
- **Contracts (addresses on Base)**:  
  - `USDC=` `0x...`  
  - `AVNT=` `0x...`  
  - `AAVE_POOL=` `0x...` (Aave v3 Pool)  
  - `AERODROME_ROUTER=` `0x...`  
  - `UNISWAP_V3_ROUTER=` `0x...`  
  - `UNISWAP_V3_QUOTER=` `0x...`  
  - *(optional for flash-swap)* `UNISWAP_V3_POOL_USDC_AVNT=` `0x...`
- **Params** (tune in keeper config):  
  - `loanAmountUSDC` (start small; e.g., `1_000`)  
  - `uniFeeTier` (`3000` = 0.3% typical)  
  - `aavePremiumBps` (e.g., `9` = 0.09%)  
  - `slippageBpsAero`, `slippageBpsUni` (e.g., `20` = 0.20%)  
  - `gasBufferUSDC` (e.g., `3`)  
  - `minProfitUSDC` (e.g., `50`)

---

## 🔺 Flash Loan Arb Pseudocode (USDC ↔ AVNT)

```pseudo
function arbTrade(loanAmountUSDC):

    # 1. Flash loan from Aave
    borrow loanAmountUSDC of USDC from Aave Pool

    # 2. First swap (cheap side)
    avntBought = swap USDC → AVNT on Aerodrome
        guard: avntBought >= minExpectedAvnt (slippage check)

    # 3. Second swap (expensive side)
    usdcOut = swap AVNT → USDC on Uniswap v3
        guard: usdcOut >= minExpectedUsdc (slippage check)

    # 4. Repay flash loan
    repay = loanAmountUSDC + flashLoanFee
    if usdcOut < repay:
        revert("Trade not profitable")

    # 5. Keep the profit
    profit = usdcOut - repay
    store profit USDC in contract

    return profit
```

---

## 🖥️ Off-Chain Keeper Logic (Pseudocode)

```pseudo
loop every few seconds:

    # 1. Fetch quotes
    priceAero = getQuote(USDC -> AVNT on Aerodrome)
    priceUni  = getQuote(AVNT -> USDC on Uniswap v3)

    # 2. Simulate cycle
    avntOut = quote(USDC_in = loanAmount, route=USDC->AVNT on Aero)
    usdcOut = quote(AVNT_in = avntOut, route=AVNT->USDC on Uni)

    # 3. Compute profit
    repay = loanAmount + aaveFlashFee
    netProfit = usdcOut - repay - estGasCost

    # 4. Check threshold
    if netProfit > minProfit:
        build tx: call ArbExecutor.executeArb(loanAmount, minAvntOut, minUsdcOut)
        send tx through private RPC
        log("Arb executed with profit:", netProfit)
    else:
        log("No arb opportunity")
```

**Guards**
- Always enforce `minExpectedAvnt` & `minExpectedUsdc` (slippage).  
- Only fire if **netProfit ≥ minProfitUSDC** (after **Aave premium + gas + buffers**).  
- Submit via **private RPC** to reduce copycats.  
- Log every attempt (quotes, mins, realized).

---

## 📊 Numeric Example (Your Observed Prices)

**Quoted**  
- Loan: **10,000 USDC**  
- Aerodrome: **$2.22** → **4,504.5 AVNT**  
- Uniswap v3: **$2.25** → **10,135 USDC**  
- Aave fee: **~0.05–0.09%** (use exact from Aave)  
- Repay (0.05%): **10,005 USDC** → Profit ≈ **130 USDC** (pre-gas, no slippage)

**Conservative (fees + 0.2% slippage both legs)**  
- After fees+slip: **USDC out ≈ 10,036.4**  
- Repay (0.09%): **10,009.0**  
- Gas buffer: **3.0**  
- **Net ≈ 24.4 USDC**

**Optimistic (fees only)**  
- USDC out ≈ **10,076.7**  
- Repay (0.09%): **10,009.0**  
- Gas buffer: **3.0**  
- **Net ≈ 64.7 USDC**

> Keeper should require net ≥ **$50–$100** for safety. Bigger spreads happen during volatility/new listings.

---

## 🔁 Profit Split + Auto-Compound (Aave)

After successful arb (post-repay):  
- Compute `profitUSDC = currentUSDC - 0` (contract should hold only profit).  
- Split in **basis points** (default **90/10**):
  - `toAave = profit * 9000 / 10000` → `AavePool.supply(USDC, toAave, owner, 0)` (mints **aUSDC** to your EOA)  
  - `toWallet = profit - toAave` → `transfer(owner, toWallet)`

**Why**: Your base vault **snowballs**, while 10% keeps the bot liquid for ops/gas.

---

## ⚡ Optional: Uniswap v3 Flash-Swap Path

When you prefer to borrow from the **Uni pool** instead of Aave:  
- Call Pool `flash(recipient, amount0, amount1, data)` borrowing **USDC or AVNT**.  
- In `uniswapV3FlashCallback`, do **Aero buy → Uni sell**, repay pool + fee, then **_profit split + supply_** as above.  
- Same price guards and min-profit threshold apply.

---

## ✅ Runbook / Checklist

1. **Fill Base addresses** (USDC, AVNT, Aave Pool, Aerodrome Router, Uniswap Router & Quoter, optional Pool).  
2. **Keeper thresholds**: `aavePremiumBps`, `slippageBps`, `gasBufferUSDC`, `minProfitUSDC`.  
3. **Private RPC** configured.  
4. Start with **small `loanAmountUSDC`** on mainnet; verify min-outs, logs, & Aave repay.  
5. Confirm **aUSDC accrues** in your EOA after each win (90% split).  
6. Scale loan size gradually once ≥20 good runs.

---

## 🧪 Quick sanity formula

```
effectiveSpread%  ≈ (askUni - bidAero) / bidAero
costs%            ≈ uniFee + aeroFee + slippageBoth + aavePremium + gas%
netEdge%          ≈ effectiveSpread% - costs%
expectedProfit    ≈ loanAmountUSDC * netEdge%
```

Fire only when `expectedProfit ≥ minProfitUSDC` with buffer.

---

## 🧰 Troubleshooting

- **Reverts on minOut**: widen slippage a bit or reduce loan size (price impact).  
- **No profit after repay**: raise `minProfitUSDC`; spreads too tight.  
- **Being copied**: always send through **private RPC**, and randomize timing.  
- **Dust in contract**: run `rescue()` / `sweepUSDC()` periodically.

---

Happy arbing. Keep defense first, compound the wins, and let the vault snowball. 🚀
