# Strategy and Signal API

## Router contract
Source: `internal/strategies/router.go`

`Router.Eval(ctx)` returns `[]Candidate` where each candidate has:
- `Signal`: normalized trade signal
- `Score`: ranking value used to pick the top candidate

Router gates include:
- minimum scanner grade and score
- whale delta gate with sweep exception
- VP confidence minimum
- target-too-close rejection
- confluence minimum
- optional dead-zone gating

## Strategy catalog in current runtime

Router may include these strategies depending on config:
- Legacy: `lsr`, `bos_pb`, `ob_r`, `fvg_c`, `fa`, `od`
- Volume Profile: `vp_accumulation`, `vp_trend`, `vp_rejection`, `vp_reversal`
- Institutional PA/VWAP: `daily_open_sr`, `pd_levels_retest`, `failed_auction_magnet`, `vwap_confluence`

## Risk policy transform
Source: `internal/strategies/risk_policy.go`

The policy can override stop/target geometry after setup generation.

Supported modes:
- Stop: `fixed`, `vp`, `hybrid`
- Target: `rr`, `vp`, `hybrid`

Applied via:
- `ApplyRiskPolicy(sig, snapshot, cfg)`

## Signal quality fields for explainability

The project uses these fields for logs and digests:
- `RejectReason`
- `Reasons[]`
- `Confluence map`
- `RegimeTag`
- `SignalSource[]`

These fields are persisted into backtest trade exports (`trades.csv`) and live receipts where available.
