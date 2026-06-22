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

	out := captureStdout(t, func() {
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
		out := captureStdout(t, func() {
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
		out := captureStdout(t, func() {
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
		out := captureStdout(t, func() {
			paper.CheckExit(now, map[string]symbolMeta{"BTCUSDT": {LastPrice: 100.4}}, map[string]aster.OrderBook{}, map[string]inplay.Entry{}, map[string]inplay.Entry{}, map[string]momentumView{}, map[string]flowMetrics{})
		})
		if !strings.Contains(out, "reason=TRAIL_STOP") {
			t.Fatalf("expected TRAIL_STOP exit, got:\n%s", out)
		}
	})
}

func TestPaperPositionMarginUsedTracksRemainingQty(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	paper.balance = 1000
	paper.reserve = 100
	paper.positions["BTCUSDT"] = &paperPosition{
		Symbol:     "BTCUSDT",
		Side:       "BUY",
		Entry:      100,
		Qty:        0.5,
		InitialQty: 1.0,
		Margin:     10,
		Leverage:   5,
		OpenedAt:   time.Now().UTC().Add(-5 * time.Minute),
	}

	pos := paper.positions["BTCUSDT"]
	if got := paperPositionMarginUsed(pos); got != 5 {
		t.Fatalf("expected remaining margin used 5.00, got %.2f", got)
	}
	if got := paper.freeForEntries(); got != 895 {
		t.Fatalf("expected free balance 895.00, got %.2f", got)
	}

	table := paper.PositionsTable(map[string]symbolMeta{"BTCUSDT": {LastPrice: 101}})
	if !strings.Contains(table, "$5.00") {
		t.Fatalf("expected positions table to show current margin used, got:\n%s", table)
	}
}

func TestWinnerProofRDefaultsToOneR(t *testing.T) {
	t.Setenv("LIVE_EXIT_PROOF_R", "")
	if got := winnerProofR(); got != 1.0 {
		t.Fatalf("expected default proof R of 1.0, got %.2f", got)
	}
}

func TestPaperLossLocksApplyAfterRepeatedDamage(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	pos := &paperPosition{
		Symbol:          "BTCUSDT",
		EntryStrategyID: "continuation_fast",
		EntryStyle:      "pullback_long",
	}

	paper.registerPaperLoss(now, pos)
	if reason := paper.symbolLossBlockReason("BTCUSDT", now.Add(30*time.Minute), candidate{Strat: "continuation_fast"}); reason != "" {
		t.Fatalf("expected no lock after one loss, got %q", reason)
	}

	paper.registerPaperLoss(now.Add(45*time.Minute), pos)
	if reason := paper.symbolLossBlockReason("BTCUSDT", now.Add(46*time.Minute), candidate{Strat: "continuation_fast"}); reason != "symbol_loss_cooldown" {
		t.Fatalf("expected symbol_loss_cooldown, got %q", reason)
	}
	if reason := paper.symbolLossBlockReason("BTCUSDT", now.Add(170*time.Minute), candidate{Strat: "continuation_fast"}); reason != "symbol_setup_loss_lock" {
		t.Fatalf("expected symbol_setup_loss_lock after symbol cooldown expires, got %q", reason)
	}
}

func TestPaperDayLossLockActivatesAfterThreeLosses(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	pos := &paperPosition{
		Symbol:          "BTCUSDT",
		EntryStrategyID: "continuation_fast",
	}
	base := time.Date(2026, 6, 21, 1, 0, 0, 0, time.UTC)
	paper.registerPaperLoss(base, pos)
	paper.registerPaperLoss(base.Add(2*time.Hour), pos)
	paper.registerPaperLoss(base.Add(4*time.Hour), pos)

	if reason := paper.symbolLossBlockReason("BTCUSDT", base.Add(4*time.Hour+time.Minute), candidate{Strat: "continuation_fast"}); reason != "symbol_day_loss_lock" {
		t.Fatalf("expected symbol_day_loss_lock, got %q", reason)
	}
}

func TestPhase2ShortFastProtectionTightensAfterPoint75R(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Now().UTC()
	paper.positions["GUAUSDT"] = &paperPosition{
		Symbol:             "GUAUSDT",
		Side:               "SELL",
		Entry:              100,
		Qty:                1,
		InitialQty:         1,
		Margin:             75,
		Leverage:           5,
		Stop:               102,
		OriginalStop:       102,
		TP1:                98,
		TP2:                97,
		TP3:                96,
		OpenedAt:           now.Add(-6 * time.Minute),
		ShortBucket:        "failed_bounce_short",
		DirectShortAllowed: true,
	}

	paper.CheckExit(now, map[string]symbolMeta{
		"GUAUSDT": {LastPrice: 98.4},
	}, map[string]aster.OrderBook{}, map[string]inplay.Entry{}, map[string]inplay.Entry{}, map[string]momentumView{}, map[string]flowMetrics{})

	pos := paper.positions["GUAUSDT"]
	if pos == nil {
		t.Fatalf("expected position to remain open")
	}
	if !(pos.Stop < 102) {
		t.Fatalf("expected fast short protection to tighten stop, got %.4f", pos.Stop)
	}
}

func captureStdout(t *testing.T, fn func()) string {
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
