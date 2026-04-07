# Data Models

## Core feature candle
Source: `internal/features/types.go`

```go
type Candle struct {
  Ts time.Time
  O, H, L, C, V float64
}
```

## Snapshot model
Source: `internal/features/types.go`

```go
type Snapshot struct {
  Candle    Candle
  Structure StructureState
  Pools     []LiquidityPool
  Sweep     *SweepEvent
  FVGs      []FVGZone
  OBs       []OBZone
  Flow      FlowState
  Anchors   AnchorLevels
  VP        VolumeProfile
}
```

## VolumeProfile model
Source: `internal/features/types.go`

Key fields:
- `POCPrice`, `VAH`, `VAL`
- `HVNs`, `LVNs`, `Bins`
- `NearestHVNAbove/Below`, `NearestLVNAbove/Below`
- `FirstOpposingVolumeDistPct`

## Strategy signal model
Source: `internal/strategies/types.go`

```go
type Signal struct {
  Active bool
  Name string
  Side features.Side
  Entry, Stop, TP1, TP2 float64
  Confidence float64
  Tags []string
  Invalidation string
  VPSetup string
  VPLevel float64
  VPTargetLevel float64
  StopMode, TargetMode string
  RejectReason string
  Reasons []string
  Confluence map[string]float64
  RegimeTag string
  SignalSource []string
  Ts time.Time
}
```

## Live status snapshot
Source: `cmd/live/main.go` (`liveStatus`)

Key fields exposed by `/api/status`:
- dry-run/live mode flags
- in-play counts
- top candidate details
- open/pending execution counts
- payout cycle summary

