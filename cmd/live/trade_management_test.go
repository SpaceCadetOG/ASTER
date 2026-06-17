package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/inplay"
)

func TestNoProofTimeoutDoesNotExitTradeThatReachedProof(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Now().UTC()
	paper.positions["BTCUSDT"] = &paperPosition{
		Symbol:     "BTCUSDT",
		Side:       "BUY",
		Entry:      100,
		Qty:        1,
		InitialQty: 1,
		Stop:       99,
		TP1:        105,
		TP2:        106,
		TP3:        107,
		OpenedAt:   now.Add(-11 * time.Minute),
	}

	paper.CheckExit(now, map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 100.20},
	}, map[string]aster.OrderBook{}, map[string]inplay.Entry{}, map[string]inplay.Entry{}, map[string]momentumView{}, map[string]flowMetrics{})

	pos := paper.positions["BTCUSDT"]
	if pos == nil {
		t.Fatalf("expected position to remain open")
	}
	if pos.MaxFavorableR < 0.15 {
		t.Fatalf("expected max favorable R update, got %.4f", pos.MaxFavorableR)
	}
}

func TestWeakPaperTradeNoLongerExitsByNoProofTimeout(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Now().UTC()
	paper.positions["BTCUSDT"] = &paperPosition{
		Symbol:                 "BTCUSDT",
		Side:                   "BUY",
		Entry:                  100,
		Qty:                    1,
		InitialQty:             1,
		Stop:                   99,
		OriginalStop:           99,
		TP1:                    105,
		TP2:                    106,
		TP3:                    107,
		OpenedAt:               now.Add(-11 * time.Minute),
		EntryTiming:            "late",
		EntryStrategyID:        "timeout_test",
		EntryReason:            "timeout_test",
		EntrySetupFamily:       "cleanup",
		EntryDistanceToVWAPPct: 0.8,
	}

	out := captureTradeManagementStdout(t, func() {
		paper.CheckExit(now, map[string]symbolMeta{
			"BTCUSDT": {LastPrice: 100.10},
		}, map[string]aster.OrderBook{}, map[string]inplay.Entry{}, map[string]inplay.Entry{}, map[string]momentumView{}, map[string]flowMetrics{})
	})

	if _, ok := paper.positions["BTCUSDT"]; !ok {
		t.Fatalf("expected weak trade to remain open without no-proof timeout")
	}
	if strings.Contains(out, "NO_PROOF_TIMEOUT") {
		t.Fatalf("expected NO_PROOF_TIMEOUT logs to be removed, got:\n%s", out)
	}
}

func TestNormalTPSLAndTrailBehaviorStillWorks(t *testing.T) {
	now := time.Now().UTC()

	t.Run("sl", func(t *testing.T) {
		paper := testCleanupPaperTrader(t)
		paper.positions["BTCUSDT"] = &paperPosition{
			Symbol:       "BTCUSDT",
			Side:         "BUY",
			Entry:        100,
			Qty:          1,
			InitialQty:   1,
			Stop:         99,
			OriginalStop: 99,
			TP1:          105,
			TP2:          106,
			TP3:          107,
			OpenedAt:     now.Add(-5 * time.Minute),
		}
		out := captureTradeManagementStdout(t, func() {
			paper.CheckExit(now, map[string]symbolMeta{"BTCUSDT": {LastPrice: 98.9}}, map[string]aster.OrderBook{}, map[string]inplay.Entry{}, map[string]inplay.Entry{}, map[string]momentumView{}, map[string]flowMetrics{})
		})
		if !strings.Contains(out, "reason=SL") {
			t.Fatalf("expected SL exit, got:\n%s", out)
		}
	})

	t.Run("tp", func(t *testing.T) {
		paper := testCleanupPaperTrader(t)
		paper.positions["BTCUSDT"] = &paperPosition{
			Symbol:       "BTCUSDT",
			Side:         "BUY",
			Entry:        100,
			Qty:          1,
			InitialQty:   1,
			Stop:         99,
			OriginalStop: 99,
			TP1:          100.4,
			TP2:          101,
			TP3:          102,
			OpenedAt:     now.Add(-2 * time.Minute),
		}
		out := captureTradeManagementStdout(t, func() {
			paper.CheckExit(now, map[string]symbolMeta{"BTCUSDT": {LastPrice: 100.5}}, map[string]aster.OrderBook{}, map[string]inplay.Entry{}, map[string]inplay.Entry{}, map[string]momentumView{}, map[string]flowMetrics{})
		})
		if !strings.Contains(out, "reason=TP1") {
			t.Fatalf("expected TP1 exit, got:\n%s", out)
		}
	})

	t.Run("trail", func(t *testing.T) {
		paper := testCleanupPaperTrader(t)
		paper.positions["BTCUSDT"] = &paperPosition{
			Symbol:       "BTCUSDT",
			Side:         "BUY",
			Entry:        100,
			Qty:          1,
			InitialQty:   1,
			Stop:         99,
			OriginalStop: 99,
			TP1:          105,
			TP2:          106,
			TP3:          107,
			TrailOn:      true,
			TrailRef:     101,
			TrailStop:    100.5,
			OpenedAt:     now.Add(-4 * time.Minute),
		}
		out := captureTradeManagementStdout(t, func() {
			paper.CheckExit(now, map[string]symbolMeta{"BTCUSDT": {LastPrice: 100.4}}, map[string]aster.OrderBook{}, map[string]inplay.Entry{}, map[string]inplay.Entry{}, map[string]momentumView{}, map[string]flowMetrics{})
		})
		if !strings.Contains(out, "reason=TRAIL_STOP") {
			t.Fatalf("expected TRAIL_STOP exit, got:\n%s", out)
		}
	})
}

func captureTradeManagementStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stdout: %v", err)
	}
	_ = r.Close()
	return buf.String()
}
