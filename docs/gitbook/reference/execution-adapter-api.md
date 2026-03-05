# Execution Adapter API (Aster)

Primary sources:
- `adapters/aster/rest_auth.go`
- `adapters/aster/execution_rest.go`
- `adapters/aster/execution.go`

## RESTAuth

`RESTAuth` handles signing, auth mode, and REST calls.

Auth modes:
- `hmac`
- `agent`

Key methods:
- `NewRESTAuthWithConfig(cfg)`
- `SyncTime()`, `ServerTime()`, `Ping()`
- `PlaceOrder(vals)`
- `CancelOrder(symbol, orderID)`
- `CancelAllOrders(symbol)`
- `GetOrder(symbol, orderID)`
- `PositionRisk(symbol)`
- `ChangeLeverage(symbol, lev)`
- `ChangeMarginType(symbol, marginType)`
- `SymbolMeta(symbol, useCache)`
- `RoundPrice(symbol, price)`, `RoundQty(symbol, qty)`

## SymbolMeta contract

```go
type SymbolMeta struct {
  Symbol string
  TickSize float64
  StepSize float64
  MinQty float64
  MinNotional float64
  QtyPrecision int
  PricePrecision int
}
```

## Higher-level Trader wrapper

`adapters/aster/execution.go` provides convenience wrappers:
- `EnterLimitUSD(...)`
- `EnterMarketUSD(...)`
- `ClosePositionMarket()`
- `ClosePositionLimit(...)`
- `Bracket(...)`
- `Flatten()`
- `WaitForFill(...)`

## Margin mode

Current live standard is isolated margin, enforced in live-lite execution path.
