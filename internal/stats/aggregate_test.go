package stats

import (
	"testing"
	"time"
)

func TestAggregate(t *testing.T) {
	now := time.Now().UTC()
	es := []Event{
		{Timestamp: now, Type: "SIGNAL", Symbol: "BTCUSDT", Simulated: true},
		{Timestamp: now, Type: "POSITION_OPEN", Symbol: "BTCUSDT", Simulated: true},
		{Timestamp: now, Type: "GATE_DECISION", Symbol: "ETHUSDT", Simulated: true, GateAllow: boolPtr(false), GateReasons: []string{"meta_quality"}},
		{Timestamp: now, Type: "MISSED_OPPORTUNITY", Symbol: "SOLUSDT", Simulated: true, MissCategory: "architecture_miss", Discovery: 0.9},
		{Timestamp: now, Type: "POSITION_CLOSE", Symbol: "BTCUSDT", Strategy: "vwap", Reason: "TP2", PnLUSD: 10, RiskR: 1.2, HoldMin: 14, MFER: 2.1, MAER: 0.4, Simulated: true},
		{Timestamp: now.Add(time.Minute), Type: "POSITION_CLOSE", Symbol: "BTCUSDT", Strategy: "vwap", Reason: "SL", PnLUSD: -5, RiskR: -1.0, HoldMin: 8, MFER: 0.3, MAER: 1.0, Simulated: true},
		{Timestamp: now.Add(2 * time.Minute), Type: "POSITION_CLOSE", Symbol: "ETHUSDT", Strategy: "vp", Reason: "FUNDING", PnLUSD: 8, RiskR: 0.9, HoldMin: 20, MFER: 1.8, MAER: 0.5, Fees: 0.4, Slippage: 0.2, Simulated: true},
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
	if r.Signals != 1 || r.Entries != 1 || r.Rejects != 1 || r.Missed != 1 {
		t.Fatalf("expected signal/entry/reject/miss counts, got %+v", r)
	}
	if r.AvgHoldMin <= 0 || r.AvgMFER <= 0 || r.AvgMAER <= 0 {
		t.Fatalf("expected lifecycle stats, got %+v", r)
	}
	if r.LeaderMiss != 1 {
		t.Fatalf("expected leader miss count, got %d", r.LeaderMiss)
	}
	if len(r.ByReject) == 0 || len(r.ByExit) == 0 || len(r.ByMissCat) == 0 {
		t.Fatalf("expected reject/exit/miss breakdowns")
	}
}

func boolPtr(v bool) *bool { return &v }
