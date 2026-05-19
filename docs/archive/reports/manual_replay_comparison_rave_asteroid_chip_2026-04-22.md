# Manual Replay Comparison (RAVE + ASTEROID + CHIP)

Replay method: fixed manual-entry replay over 1406 minutes (~23h26m) per trade, with equal notional sizing (`23.17 USDT`) and 1m candles.

## Inputs
- CHIP canonical: BUY, entry `2026-04-21 19:36:52 CDT`, entry price `0.05793`
- RAVE ref: BUY, entry `2026-04-20 21:19:22 CDT`, entry price `1.203131`
- ASTEROID ref: BUY, entry `2026-04-20 23:06:24 CDT`, entry price `0.000464`

## Side-by-side summary

| Label | Symbol | Peak PnL (USDT) | End PnL (USDT) | Giveback (USDT) | Peak % | End % |
|---|---|---:|---:|---:|---:|---:|
| CHIP canonical | CHIPUSDT | 25.2898 | 21.6901 | 3.5997 | 109.15 | 93.61 |
| RAVE ref | RAVEUSDT | 27.5254 | 4.5407 | 22.9847 | 118.80 | 19.60 |
| ASTEROID ref | ASTEROIDUSDT | 1.5680 | -2.4968 | 4.0647 | 6.77 | -10.78 |

## Interpretation
- CHIP is the clean hold-through-consolidation archetype: large sustained gains with relatively low giveback.
- RAVE shows a high peak but large giveback over the full replay horizon, so late-stage protection/trailing quality dominates outcomes.
- ASTEROID behaves as a weak/fragile trend continuation in this window; early management and invalidation handling matter more than long hold bias.

## Files
- Trades input: `/Users/victorogbebor/2026/go-machine/docs/examples/manual-replay/manual_replay_trades_rave_asteroid_chip.csv`
- Replay outputs:
  - `/Users/victorogbebor/2026/go-machine/out/manual_replay_rave_asteroid_chip/replay_summary.md`
  - `/Users/victorogbebor/2026/go-machine/out/manual_replay_rave_asteroid_chip/replay_summary.csv`
  - `/Users/victorogbebor/2026/go-machine/out/manual_replay_rave_asteroid_chip/replay_summary.json`
