package stats

import "testing"

func TestEvaluateReadiness(t *testing.T) {
	r := Report{
		TotalTrades: 60,
		Expectancy:  0.05,
		ProfitFac:   1.02,
		MaxDrawdown: 42,
		WatchToEntry: 0.5,
		LeaderMiss:  20,
		FundingPnL:  -25,
		Slippage:    30,
	}
	out := EvaluateReadiness(r, DefaultReadinessConfig())
	if out.Ready {
		t.Fatalf("expected not ready")
	}
	if len(out.Reasons) < 3 {
		t.Fatalf("expected multiple reasons, got %+v", out)
	}
}
