package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/inplay"
	"go-machine/internal/strategies"
)

func TestStarterTradeCanEnterWithoutCandidateReadyBlocking(t *testing.T) {
	now := time.Now().UTC()
	cands := applyCandidateLifecycle([]candidate{testCleanupCandidate("BTCUSDT", "BUY")}, now, map[string]candidateMemory{}, candidateLifecycleConfig{
		Enable:        true,
		ArmScans:      2,
		ReadyScans:    3,
		ReadyMinScore: 95,
		ReadyMinSlope: 0.8,
		ExpireAfter:   15 * time.Minute,
	})
	if len(cands) != 1 {
		t.Fatalf("expected one candidate, got %d", len(cands))
	}
	cand := cands[0]
	if cand.LifecycleStage == "READY" {
		t.Fatalf("expected lifecycle stage below READY, got %s", cand.LifecycleStage)
	}
	if strings.TrimSpace(cand.RejectReason) != "" {
		t.Fatalf("expected no reject reason for non-ready starter, got %q", cand.RejectReason)
	}

	paper := testCleanupPaperTrader(t)
	pos, err := paper.MaybeEnter(now, cand, 0, 50, 2, map[string]symbolMeta{
		"BTCUSDT": {LastPrice: 100, VolumeUSD: 5_000_000},
	}, map[string]aster.OrderBook{}, map[string]inplay.Entry{})
	if err != nil {
		t.Fatalf("MaybeEnter error: %v", err)
	}
	if pos == nil {
		t.Fatalf("expected paper position to open")
	}
}

func TestRemovedReentryAndPyramidGatesNoLongerBlock(t *testing.T) {
	now := time.Now().UTC()
	cand := testCleanupCandidate("BTCUSDT", "BUY")
	cand.Entry.EntryStyle = ""
	cand.Entry.MetaState = "exhaustion"
	cand.Entry.ExhaustionRisk = 9
	cand.Entry.TimeInStateMin = 120
	cand.Entry.CurrentScore = 40
	cand.Entry.ScoreSlope = -0.5
	cand.LastClose = 150
	cand.SessionVWAP = 90
	cand.EMA9 = 92
	cand.DayUTC24h = 40
	cand.UTC4hPct = 12
	cand.UTC1hPct = 8
	cand.ExtensionATR = 5

	active := &livePosition{
		Symbol:                "BTCUSDT",
		Side:                  "BUY",
		State:                 execPartialTP1,
		RemainingQty:          1,
		EntrySource:           "BOT",
		Managed:               true,
		Protected:             true,
		AddLockedUntilConfirm: true,
		StarterOnly:           true,
		DeployedMargin:        10,
	}
	mgr := &liveExecManager{
		positions: map[string]*livePosition{"BTCUSDT": active},
		ladderCfg: ladderConfig{
			StarterUSDT:  10,
			StepUSDT:     5,
			MaxAdds:      2,
			MaxTotalUSDT: 50,
		},
	}
	plan := resolveLadderPlan(now, cand, mgr, map[string]symbolMeta{})
	if plan.RejectReason != "" {
		t.Fatalf("expected removed add gate set to allow plan, got %q", plan.RejectReason)
	}
	if !plan.IsAdd {
		t.Fatalf("expected add plan to proceed")
	}

	closed := &livePosition{
		Symbol:              "BTCUSDT",
		Side:                "BUY",
		State:               execClosed,
		ClosedAt:            now.Add(-5 * time.Minute),
		CloseReason:         "SOFT_EXIT",
		RunnerCaptureFailed: true,
		CombinedScore:       99,
	}
	mgr.positions = map[string]*livePosition{"BTCUSDT": closed}
	mgr.reentryCfg = reentryConfig{
		Enable:       true,
		MaxPerSymbol: 3,
		Cooldown:     time.Hour,
		SizeUSDT:     7,
	}
	plan = resolveLadderPlan(now, cand, mgr, map[string]symbolMeta{})
	if plan.RejectReason != "" {
		t.Fatalf("expected removed reentry gate set to allow plan, got %q", plan.RejectReason)
	}
	if !plan.IsReentry {
		t.Fatalf("expected structured reentry to proceed without removed gates")
	}
	if plan.MarginUSDT != 7 {
		t.Fatalf("expected reentry size to be preserved, got %.2f", plan.MarginUSDT)
	}
}

func TestManagedPositionUnprotectedNoLongerBlocksStarter(t *testing.T) {
	now := time.Now().UTC()
	cand := testCleanupCandidate("BTCUSDT", "BUY")
	mgr := &liveExecManager{
		positions: map[string]*livePosition{
			"ETHUSDT": {
				Symbol:            "ETHUSDT",
				Side:              "BUY",
				State:             execOpen,
				RemainingQty:      1,
				Managed:           true,
				EntryReason:       manualEntryReasonManaged,
				ManualManageState: manualManageStateCritical,
				UpdatedAt:         now.Add(-time.Minute),
			},
		},
		ladderCfg: ladderConfig{StarterUSDT: 10},
	}
	plan := resolveLadderPlan(now, cand, mgr, map[string]symbolMeta{})
	if plan.RejectReason != "" {
		t.Fatalf("expected starter plan not to be blocked by managed protection state, got %q", plan.RejectReason)
	}
	if plan.IsAdd || plan.IsReentry {
		t.Fatalf("expected plain starter plan, got %+v", plan)
	}
}

func TestSafetyGatesStillBlock(t *testing.T) {
	now := time.Now().UTC()
	cand := testCleanupCandidate("BTCUSDT", "BUY")

	t.Run("symbol_active_opposite_side", func(t *testing.T) {
		mgr := &liveExecManager{
			positions: map[string]*livePosition{
				"BTCUSDT": {Symbol: "BTCUSDT", Side: "SELL", State: execOpen, RemainingQty: 1, Managed: true, Protected: true},
			},
			ladderCfg: ladderConfig{StarterUSDT: 10},
		}
		if got := resolveLadderPlan(now, cand, mgr, nil).RejectReason; got != "symbol_active_opposite_side" {
			t.Fatalf("expected symbol_active_opposite_side, got %q", got)
		}
	})

	t.Run("pending_add_order", func(t *testing.T) {
		mgr := &liveExecManager{
			positions: map[string]*livePosition{
				"BTCUSDT": {Symbol: "BTCUSDT", Side: "BUY", State: execOpen, RemainingQty: 1, EntrySource: "BOT", Managed: true, Protected: true, PendingAddOrderID: 99},
			},
			ladderCfg: ladderConfig{StarterUSDT: 10},
		}
		if got := resolveLadderPlan(now, cand, mgr, nil).RejectReason; got != "pending_add_order" {
			t.Fatalf("expected pending_add_order, got %q", got)
		}
	})
}

func testCleanupCandidate(symbol, side string) candidate {
	return candidate{
		Entry: inplay.Entry{
			Symbol:       symbol,
			CurrentGrade: "A",
			CurrentScore: 75,
			ScoreSlope:   0.1,
			State:        inplay.StateHeating,
			Rank:         1,
			Momentum:     true,
		},
		Side:        side,
		Strat:       "cleanup_test",
		StrategyID:  "cleanup_test",
		SetupFamily: "cleanup",
		Conf:        0.8,
		LastClose:   100,
		SessionVWAP: 100,
		EMA9:        100,
		VolumeUSD:   5_000_000,
		VolumeRatio: 1.5,
		Sig: strategies.Signal{
			Active: true,
			Name:   "cleanup_test",
			Entry:  100,
			Stop:   99,
			TP1:    101.5,
			TP2:    102,
			TP3:    103,
		},
	}
}

func testCleanupPaperTrader(t *testing.T) *paperTrader {
	t.Helper()
	dir := t.TempDir()
	return &paperTrader{
		enabled:           true,
		balance:           1_000,
		feeBps:            0,
		stopPct:           1,
		tp1R:              1.2,
		tp2R:              2.0,
		tp3R:              3.0,
		minStopPct:        0.1,
		maxStopPct:        10,
		minTP1RR:          0.1,
		stateFile:         filepath.Join(dir, "paper_state.json"),
		tradesCSV:         filepath.Join(dir, "paper_trades.csv"),
		closedTradesJSONL: filepath.Join(dir, "paper_closed_trades.jsonl"),
		equityCSV:         filepath.Join(dir, "paper_equity.csv"),
		maxOpen:           5,
		positions:         map[string]*paperPosition{},
		closedTradeLedger: map[string]paperClosedTradeRecord{},
		postExitTrackers:  map[string]*paperPostExitTracker{},
		reportLoc:         time.UTC,
		dayStats:          map[string]*paperDayStats{},
		lastFundKey:       map[string]string{},
		lastExitAt:        map[string]time.Time{},
		lastExitLoss:      map[string]bool{},
		lastHarvestAt:     map[string]time.Time{},
		symbolTradeDay:    map[string]string{},
		symbolTradeCount:  map[string]int{},
		lossStreak:        map[string]int{},
		lockUntil:         map[string]time.Time{},
		stopTriggerRef:    "mark",
		tpTriggerRef:      "mark",
		markLastModel:     "mark",
		openCostMode:      "simple",
	}
}
