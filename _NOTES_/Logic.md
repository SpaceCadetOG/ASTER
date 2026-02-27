Scanners:
    Use Sparklines:
    → Scan all markets before each session, rank top trending (price + volume + OI)
    - Detect trend direction of BTC over last 24h
    - Compare volume spikes between tickers

Rank tickers by funding rate shifts
    Use Candles:
    → Pull last 50–100 candles for selected tickers and run technical analysis or entry signals


Spread
- 💡 Why It Matters:
A tight spread (small number) means high liquidity and active participation — easier and cheaper to enter/exit trades.
A wide spread indicates low liquidity or uncertain conditions — more slippage and risk.
🧠 Use Cases:
Avoid trades with high spreads unless you're expecting major movement.
Use as a filter: only trade if spread < $5 or < 0.05%.

Impalcence
💡 Why It Matters:
> 0.5 → More bids than asks (buy pressure).
< 0.5 → More asks than bids (sell pressure).
Close to 1.0 = heavy buyer dominance.
Close to 0.0 = heavy seller dominance.
🧠 Use Cases:
Use to confirm entry direction. For example:
    If your strategy gives a long signal, confirm that imbalance > 0.6.
    If short, check for imbalance < 0.4.
    Use to detect spoofing or fake walls (sudden imbalance shifts).
    Integration with trade execution logic or alerts

example:
    Spread = 2.35
    Imbalance = 0.73
        🔸 There’s a decent amount of buy pressure (imbalance > 0.7)
        🔸 The spread is narrow, indicating healthy liquidity
        ✅ Might be a good time to consider a long entry


- INDEX vs MARK PRICE
    - Mark - SL, wick out protection
    - index - TP - avg true markt price
    - mark based ema

    if mp <= sl
        close pos
    elif ip >= tp
        close pos