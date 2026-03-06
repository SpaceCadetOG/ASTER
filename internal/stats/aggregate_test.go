package stats

import (
	"testing"
	"time"
)

func TestAggregate(t *testing.T) {
	now := time.Now().UTC()
	es := []Event{
		{Timestamp: now, Type: "POSITION_CLOSE", Symbol: "BTCUSDT", Strategy: "vwap", PnLUSD: 10, RiskR: 1.2, Simulated: true},
		{Timestamp: now.Add(time.Minute), Type: "POSITION_CLOSE", Symbol: "BTCUSDT", Strategy: "vwap", PnLUSD: -5, RiskR: -1.0, Simulated: true},
		{Timestamp: now.Add(2 * time.Minute), Type: "POSITION_CLOSE", Symbol: "ETHUSDT", Strategy: "vp", PnLUSD: 8, RiskR: 0.9, Simulated: true},
	}
	r := Aggregate(es)
	if r.TotalTrades != 3 || r.Wins != 2 || r.Losses != 1 {
		t.Fatalf("unexpected top-level stats: %+v", r)
	}
	if r.ProfitFac <= 1 {
		t.Fatalf("expected PF > 1, got %.2f", r.ProfitFac)
	}
	if len(r.ByStrategy) == 0 || len(r.BySymbol) == 0 {
		t.Fatalf("expected breakdown rows")
	}
}
