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
	"go-machine/internal/strategies"
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

func TestPaperBaselineTPDefaults(t *testing.T) {
	t.Setenv("LIVE_PAPER_TP1_R", "")
	t.Setenv("LIVE_PAPER_TP2_R", "")
	t.Setenv("LIVE_PAPER_TP3_R", "")
	t.Setenv("LIVE_PAPER_TP_RATCHET_ONLY", "")
	t.Setenv("LIVE_BE_LOCK_BPS", "")
	t.Setenv("LIVE_PAPER_BE_ARM_R", "")
	t.Setenv("LIVE_PAPER_EXIT_PROTECT_AFTER_PROOF", "")
	t.Setenv("LIVE_EXIT_PROTECT_AFTER_PROOF", "")

	paper := newPaperTrader(true, 0, 5)
	if paper.tp1R != 1.0 || paper.tp2R != 2.0 || paper.tp3R != 3.0 {
		t.Fatalf("expected 1/2/3R paper defaults, got %.2f/%.2f/%.2f", paper.tp1R, paper.tp2R, paper.tp3R)
	}
	if paper.tpRatchetOnly {
		t.Fatalf("expected paper ratchet-only disabled by default")
	}
	if paper.beLockBps != 0 {
		t.Fatalf("expected paper BE lock default 0, got %.2f", paper.beLockBps)
	}
	if got := beArmThreshold(envFloat("LIVE_PAPER_BE_ARM_R", 1.50), 1.0); got != 1.5 {
		t.Fatalf("expected paper BE arm threshold 1.50R, got %.2f", got)
	}
	if paperProtectAfterProofEnabled() {
		t.Fatalf("expected paper protect-after-proof disabled by default")
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
	if reason := paper.symbolLossBlockReason("BTCUSDT", now.Add(46*time.Minute), candidate{Strat: "continuation_fast"}); reason != "" {
		t.Fatalf("expected advisory-only loss tracking after repeated losses, got %q", reason)
	}
	if reason := paper.symbolLossBlockReason("BTCUSDT", now.Add(170*time.Minute), candidate{Strat: "continuation_fast"}); reason != "" {
		t.Fatalf("expected no setup loss lock after cooldown window, got %q", reason)
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

	if reason := paper.symbolLossBlockReason("BTCUSDT", base.Add(4*time.Hour+time.Minute), candidate{Strat: "continuation_fast"}); reason != "" {
		t.Fatalf("expected no day loss lock, got %q", reason)
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

func TestClassifyEntryOutcome(t *testing.T) {
	cases := []struct {
		r    float64
		want EntryOutcome
	}{
		{0.10, EntryOutcomeNoProof},
		{0.30, EntryOutcomeWeakProof},
		{1.00, EntryOutcomeGoodProof},
		{1.80, EntryOutcomeStrongProof},
	}
	for _, tc := range cases {
		if got := classifyEntryOutcome(tc.r); got != tc.want {
			t.Fatalf("classifyEntryOutcome(%.2f) = %s, want %s", tc.r, got, tc.want)
		}
	}
}

func TestScoreEntryBreakdownPrefersAlignedStructuredEntry(t *testing.T) {
	good := candidate{
		Side:              "BUY",
		DayUTC24h:         12,
		UTC4hPct:          4,
		UTC1hPct:          1.5,
		LastClose:         101,
		SessionVWAP:       100.9,
		EMA9:              100.7,
		ExtensionATR:      0.45,
		DistanceToVWAPPct: 0.10,
		VolumeRatio:       1.8,
		OFIZ:              0.45,
		ReclaimHold:       true,
		TriggerState:      string(triggerOFReclaim),
		Sig: strategies.Signal{
			Entry: 101,
			Stop:  99.5,
			TP1:   104.5,
		},
		Entry: inplay.Entry{
			EntryStyle: "pullback_long",
			State:      inplay.StateInPlay,
			ScoreSlope: 0.22,
		},
	}
	good.EntryTiming = classifyEntryTiming(good)
	goodScore := scoreEntryBreakdown(good)

	bad := candidate{
		Side:              "SELL",
		DayUTC24h:         18,
		UTC4hPct:          5,
		UTC1hPct:          -0.5,
		LastClose:         100,
		SessionVWAP:       98.5,
		EMA9:              99.2,
		ExtensionATR:      1.7,
		DistanceToVWAPPct: 1.5,
		VolumeRatio:       0.9,
		OFIZ:              0.25,
		TriggerState:      "",
		Sig: strategies.Signal{
			Entry: 100,
			Stop:  103,
			TP1:   101,
		},
		Entry: inplay.Entry{
			EntryStyle: "avoid_chase",
			State:      inplay.StateCooling,
			ScoreSlope: -0.02,
		},
	}
	bad.EntryTiming = classifyEntryTiming(bad)
	badScore := scoreEntryBreakdown(bad)

	if goodScore.FinalScore <= badScore.FinalScore {
		t.Fatalf("expected structured aligned entry to outscore chase entry, got good=%.2f bad=%.2f", goodScore.FinalScore, badScore.FinalScore)
	}
	if goodScore.TrendLabel == "direct_conflict" {
		t.Fatalf("expected good setup to avoid direct conflict label")
	}
	if badScore.FinalScore >= 70 {
		t.Fatalf("expected chase/conflict setup to stay below enter threshold, got %.2f", badScore.FinalScore)
	}
}

func TestUpdateLiveMarketContactTelemetryCapturesInitialWindows(t *testing.T) {
	now := time.Now().UTC()
	pos := &livePosition{
		Side:          "BUY",
		EntryPrice:    100,
		EntryFilledAt: now.Add(-65 * time.Second),
		MaxFavorableR: 0.9,
		MaxAdverseR:   0.2,
	}
	updateLiveMarketContactTelemetry(now, pos, 101.2)
	if pos.Price5sAfterFill <= 0 || pos.Price60sAfterFill <= 0 {
		t.Fatalf("expected early contact windows to be captured, got %+v", pos)
	}
	if pos.PostFillDirection60s != "YES" {
		t.Fatalf("expected positive 60s direction, got %q", pos.PostFillDirection60s)
	}
}

func TestClassifyLiveMarketContact(t *testing.T) {
	t.Run("immediate rejection", func(t *testing.T) {
		pos := &livePosition{
			Side:                "BUY",
			Move30sAfterFillBps: -18,
			MFEFirst30sR:        0.10,
		}
		if got := classifyLiveMarketContact(pos, "POSITION_FLAT_REMOTE"); got != "immediate_rejection" {
			t.Fatalf("expected immediate_rejection, got %q", got)
		}
	})

	t.Run("late entry exhaustion", func(t *testing.T) {
		pos := &livePosition{
			Side:                   "BUY",
			EntryCandlePositionPct: 0.92,
			MaxFavorableR:          0.20,
		}
		if got := classifyLiveMarketContact(pos, "POSITION_FLAT_REMOTE"); got != "late_entry_exhaustion" {
			t.Fatalf("expected late_entry_exhaustion, got %q", got)
		}
	})

	t.Run("clean continuation", func(t *testing.T) {
		pos := &livePosition{
			Side:                "BUY",
			Move60sAfterFillBps: 15,
			MFEFirst60sR:        1.1,
		}
		if got := classifyLiveMarketContact(pos, "TP1_HIT"); got != "clean_continuation" {
			t.Fatalf("expected clean_continuation, got %q", got)
		}
	})
}

func TestScoreEntryBreakdownPenalizesLateContinuationLong(t *testing.T) {
	early := candidate{
		Side:              "BUY",
		Strat:             "pullback_reclaim",
		SetupFamily:       "micro_pullback_continuation",
		DayUTC24h:         9,
		UTC4hPct:          2,
		UTC1hPct:          0.8,
		LastClose:         101,
		SessionVWAP:       100.8,
		EMA9:              100.7,
		ExtensionATR:      0.45,
		DistanceToVWAPPct: 0.18,
		VolumeRatio:       1.7,
		OFIZ:              0.32,
		ReclaimHold:       true,
		TriggerState:      string(triggerOFReclaim),
		Sig: strategies.Signal{
			Entry: 101,
			Stop:  99.7,
			TP1:   103.4,
		},
		Entry: inplay.Entry{
			EntryStyle: "pullback_long",
			State:      inplay.StateInPlay,
			ScoreSlope: 0.24,
		},
		EntryTiming: "mid",
	}
	late := early
	late.EntryTiming = "late"
	late.DistanceToVWAPPct = 1.15
	late.ExtensionATR = 1.20
	late.VolumeRatio = 1.05
	late.OFIZ = 0.05

	earlyScore := scoreEntryBreakdown(early)
	lateScore := scoreEntryBreakdown(late)
	if lateScore.FinalScore >= earlyScore.FinalScore {
		t.Fatalf("expected late continuation long to score lower, got late=%.2f early=%.2f", lateScore.FinalScore, earlyScore.FinalScore)
	}
	if !strings.Contains(strings.Join(lateScore.PenaltyReasons, ","), "late_continuation_long") {
		t.Fatalf("expected late continuation penalty reasons, got %+v", lateScore.PenaltyReasons)
	}
}

func TestScoreEntryBreakdownSoftensHighAlphaAltShortPenalties(t *testing.T) {
	alt := candidate{
		Entry: inplay.Entry{
			Symbol:       "TACUSDT",
			CurrentGrade: "A+",
			CurrentScore: 99,
			EntryStyle:   "momentum_ignite_short",
		},
		Side:            "SELL",
		Strat:           "impulse_breakout",
		SetupFamily:     "reset_impulse_breakout",
		DayUTC24h:       -35.8,
		UTC4hPct:        -12.3,
		UTC1hPct:        -5.0,
		LastClose:       0.002853,
		VolumeUSD:       1_660_000,
		ExtensionATR:    2.0,
		TriggerState:    string(triggerImpulseCont),
		VolumeRatio:     1.7,
		OFIZ:            -0.4,
		ClosedBreakHold: true,
		Sig: strategies.Signal{
			Entry: 0.002853,
			Stop:  0.003153,
			TP1:   0.002553,
		},
	}
	major := alt
	major.Entry.Symbol = "ETHUSDT"
	major.VolumeUSD = 396_000_000
	major.ExtensionATR = 2.0

	altScore := scoreEntryBreakdown(alt)
	majorScore := scoreEntryBreakdown(major)
	if altScore.FinalScore <= majorScore.FinalScore {
		t.Fatalf("expected high-alpha alt short to retain higher score, got alt=%.2f major=%.2f", altScore.FinalScore, majorScore.FinalScore)
	}
	if !strings.Contains(strings.Join(altScore.PenaltyReasons, ","), "late_all_red_short_alt_softened") {
		t.Fatalf("expected softened alt short penalty reason, got %+v", altScore.PenaltyReasons)
	}
}

func TestPaperSameSetupCooldownBlocksWeakRepeat(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Date(2026, 6, 29, 1, 0, 0, 0, time.UTC)
	paper.closedTradeLedger["t1"] = paperClosedTradeRecord{
		TradeID: "t1",
		Symbol:  "SYNUSDT",
		Side:    "BUY",
		Identity: paperClosedTradeIdentity{
			SetupFamily: "micro_pullback_continuation",
		},
		Exit: paperClosedTradeExit{
			ExitTs:            now.Add(-10 * time.Minute),
			NetPnL:            -0.40,
			EntryOutcomeLabel: EntryOutcomeWeakProof,
		},
	}
	reason := paper.sameSymbolSetupCooldownReason("SYNUSDT", now, candidate{
		Side:        "BUY",
		SetupFamily: "micro_pullback_continuation",
		Strat:       "pullback_reclaim",
	})
	if !strings.Contains(reason, "same_setup_cooldown") {
		t.Fatalf("expected same setup cooldown, got %q", reason)
	}
}

func TestPaperSameSymbolCooldownBlocksClusteredWeakRepeats(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Date(2026, 6, 29, 2, 0, 0, 0, time.UTC)
	paper.closedTradeLedger["t1"] = paperClosedTradeRecord{
		TradeID: "t1",
		Symbol:  "SYNUSDT",
		Side:    "BUY",
		Identity: paperClosedTradeIdentity{
			SetupFamily: "micro_pullback_continuation",
		},
		Exit: paperClosedTradeExit{
			ExitTs:            now.Add(-45 * time.Minute),
			NetPnL:            -0.30,
			EntryOutcomeLabel: EntryOutcomeNoProof,
		},
	}
	paper.closedTradeLedger["t2"] = paperClosedTradeRecord{
		TradeID: "t2",
		Symbol:  "SYNUSDT",
		Side:    "BUY",
		Identity: paperClosedTradeIdentity{
			SetupFamily: "reset_impulse_breakout",
		},
		Exit: paperClosedTradeExit{
			ExitTs:            now.Add(-12 * time.Minute),
			NetPnL:            -0.20,
			EntryOutcomeLabel: EntryOutcomeWeakProof,
		},
	}
	reason := paper.sameSymbolSetupCooldownReason("SYNUSDT", now, candidate{
		Side:        "BUY",
		SetupFamily: "breakout_retest",
		Strat:       "impulse_breakout",
	})
	if !strings.Contains(reason, "same_symbol_side_cooldown") {
		t.Fatalf("expected same symbol side cooldown, got %q", reason)
	}
}

func TestPaperMaybeEnterRejectsWhenAtMaxOpen(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	paper.maxOpen = 1
	paper.positions["BTCUSDT"] = &paperPosition{
		Symbol:     "BTCUSDT",
		Side:       "BUY",
		Entry:      100,
		Qty:        1,
		InitialQty: 1,
		Margin:     10,
		Leverage:   5,
		OpenedAt:   time.Now().UTC().Add(-5 * time.Minute),
	}

	_, err := paper.MaybeEnter(time.Now().UTC(), candidate{
		Side:          "BUY",
		Strat:         "impulse_breakout",
		StrategyID:    "impulse_breakout",
		SetupFamily:   "reset_impulse_breakout",
		Conf:          0.9,
		CombinedScore: 0.9,
		Entry: inplay.Entry{
			Symbol:       "ETHUSDT",
			CurrentGrade: "A",
			CurrentScore: 96,
			ScoreSlope:   0.20,
			State:        inplay.StateInPlay,
			EntryStyle:   "breakout_hold_long",
		},
		ClosedBreakHold: true,
		LastClose:       100,
		SessionVWAP:     99.7,
		EMA9:            99.8,
		VolumeRatio:     1.5,
		EntryScoreBreakdown: EntryScoreBreakdown{
			TrendScore:    20,
			LocationScore: 18,
			TriggerScore:  16,
			FlowScore:     12,
			FinalScore:    88,
			TrendLabel:    "scored",
		},
		Sig: strategies.Signal{
			Active: true,
			Name:   "impulse_breakout",
			Entry:  100,
			Stop:   98.8,
			TP1:    101.4,
			TP2:    102.6,
			TP3:    103.8,
		},
	}, 0, 10, 5, map[string]symbolMeta{
		"ETHUSDT": {LastPrice: 100},
		"BTCUSDT": {LastPrice: 101},
	}, map[string]aster.OrderBook{}, map[string]inplay.Entry{})
	if err == nil || !strings.Contains(err.Error(), "max_open_reached") {
		t.Fatalf("expected max_open_reached error, got %v", err)
	}
	if len(paper.positions) != 1 {
		t.Fatalf("expected open position count to remain capped, got %d", len(paper.positions))
	}
	if _, ok := paper.positions["ETHUSDT"]; ok {
		t.Fatalf("unexpected ETHUSDT position opened despite max-open cap")
	}
}

func TestRecordClosedTradeUsesAggregateStopOutcome(t *testing.T) {
	paper := testCleanupPaperTrader(t)
	now := time.Now().UTC()
	pos := &paperPosition{
		TradeID:          "paper-test-aggregate-stop",
		Symbol:           "SYNUSDT",
		Side:             "BUY",
		OpenedAt:         now.Add(-10 * time.Minute),
		Entry:            100,
		InitialQty:       10,
		Qty:              0,
		Margin:           10,
		Leverage:         5,
		Stop:             100,
		OriginalStop:     95,
		OriginalTP1:      105,
		OriginalTP2:      110,
		OriginalTP3:      115,
		Realized:         0.50,
		GrossRealized:    0.52,
		FeesRealized:     0.02,
		MaxFavorableR:    1.10,
		MaxAdverseR:      0.20,
		HitTP1:           true,
		EntryReason:      "pullback_reclaim",
		EntrySetupFamily: "micro_pullback_continuation",
		EntryStyle:       "pullback_long",
		EntryTiming:      "mid",
	}

	paper.recordClosedTrade(now, pos, 99.5, "SL", 10, symbolMeta{})
	rec, ok := paper.closedTradeLedger[pos.TradeID]
	if !ok {
		t.Fatalf("expected closed trade record")
	}
	if rec.Exit.StopOutType != "profit_lock" {
		t.Fatalf("expected aggregate stop_out_type=profit_lock, got %q", rec.Exit.StopOutType)
	}
	if rec.Exit.ExitReason != "Protected Stop" {
		t.Fatalf("expected normalized exit reason Protected Stop, got %q", rec.Exit.ExitReason)
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
