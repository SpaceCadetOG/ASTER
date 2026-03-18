package backtest

import (
	"testing"
	"time"
)

func TestRealizeBacktestExitStagesAndRunner(t *testing.T) {
	cfg := Config{
		FeesBps:         0,
		SlipBps:         0,
		FundingRate:     0,
		TP1Frac:         0.35,
		TP2Frac:         0.25,
		TP3Frac:         0.20,
		TrailStopPct:    1.50,
		TrailStopPctTP3: 3.25,
		TrailPctMin:     1.00,
	}
	base := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	pos := &btPosition{
		Trade: Trade{
			Symbol:   "BTCUSDT",
			Strategy: "continuation_fast",
			Side:     "long",
			EntryTs:  base,
			Entry:    100,
			Stop:     99,
			TP1:      101,
			TP2:      102,
			TP3:      103,
			Qty:      10,
		},
		InitialQty:   10,
		RemainingQty: 10,
	}

	if _, closed := realizeBacktestExit(pos, cfg, base.Add(time.Minute), pos.Trade.TP1, backtestTargetQty(pos.InitialQty, cfg.TP1Frac, pos.RemainingQty), "TP1", false); closed {
		t.Fatalf("tp1 partial should not close position")
	}
	if pos.RemainingQty >= 10 || pos.RemainingQty <= 0 {
		t.Fatalf("expected reduced remaining qty after TP1, got %.4f", pos.RemainingQty)
	}

	if _, closed := realizeBacktestExit(pos, cfg, base.Add(2*time.Minute), pos.Trade.TP2, backtestTargetQty(pos.InitialQty, cfg.TP2Frac, pos.RemainingQty), "TP2", false); closed {
		t.Fatalf("tp2 partial should not close position")
	}
	pos.HitTP3 = true
	pos.TrailOn = true
	pos.TrailRef = 104
	pos.TrailStop = backtestCalcTrailStop(cfg, pos, pos.TrailRef, true)
	if pos.TrailStop <= 0 || pos.TrailStop >= pos.TrailRef {
		t.Fatalf("expected valid post-TP3 trail stop, got ref=%.4f stop=%.4f", pos.TrailRef, pos.TrailStop)
	}

	closeTrade, closed := realizeBacktestExit(pos, cfg, base.Add(3*time.Minute), pos.TrailStop, pos.RemainingQty, "TRAIL_STOP", true)
	if !closed {
		t.Fatalf("runner close should finalize trade")
	}
	if closeTrade.Reason != "TRAIL_STOP" {
		t.Fatalf("expected trail stop final reason, got %s", closeTrade.Reason)
	}
	if closeTrade.PnL <= 0 {
		t.Fatalf("expected positive pnl after staged exits, got %.4f", closeTrade.PnL)
	}
	if closeTrade.ExitTs.IsZero() || closeTrade.HoldMins <= 0 {
		t.Fatalf("expected finalized exit metadata, got %+v", closeTrade)
	}
}
