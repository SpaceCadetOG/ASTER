# Risk Shell API

Source: `internal/risk/shell.go`

## Purpose

Hard-gate entries before execution to prevent structurally bad trades.

## Public API

```go
func DefaultConfig() Config
func Approve(cfg Config, in Input) Decision
```

## Config

```go
type Config struct {
  Enabled              bool
  MinLiqBufferMult     float64
  MaxFundingCostR      float64
  MaxSpreadBps         float64
  MinBookImbalance     float64
  MaxRecentSlippageBps float64
}
```

## Input

```go
type Input struct {
  Side              string // BUY/SELL
  Entry, Stop       float64
  Leverage          float64
  NotionalUSD       float64
  FundingRate       float64
  HoldHours         float64
  SpreadBps         float64
  BookImbalance     float64
  RecentSlippageBps float64
  VenueHealthy      bool
}
```

## Decision

```go
type Decision struct {
  Approved      bool
  RejectReason  string
  LiqBufferOK   bool
  LiqBufferMult float64
  FundingCostR  float64
}
```

## Standard reject reasons
- `venue_unhealthy`
- `invalid_risk_input`
- `spread_too_wide`
- `depth_too_thin`
- `slippage_anomaly`
- `invalid_stop_distance`
- `liq_buffer_violation`
- `funding_too_expensive`

## Integration points
- `cmd/live/main.go`: pre-entry gate before paper/live place.
- `internal/backtest/engine.go`: pre-entry gate in simulation loop.
