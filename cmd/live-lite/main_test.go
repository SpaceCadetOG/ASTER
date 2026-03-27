package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/features"
	"go-machine/internal/inplay"
	"go-machine/internal/market"
	"go-machine/internal/strategies"
	"go-machine/internal/types"
)

func TestInHourWindow(t *testing.T) {
	if !inHourWindow(0, 0, 1) {
		t.Fatalf("expected 00 hour to be in 00-01")
	}
	if inHourWindow(1, 0, 1) {
		t.Fatalf("expected 01 hour to be out of 00-01")
	}
	if !inHourWindow(23, 22, 2) {
		t.Fatalf("expected overnight window to include 23")
	}
	if !inHourWindow(1, 22, 2) {
		t.Fatalf("expected overnight window to include 01")
	}
}

func TestActiveMaintenanceWindow(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	now := time.Date(2026, 3, 3, 16, 10, 0, 0, loc)
	w := maintenanceWindow{Name: "EOD", Enabled: true, StartHour: 16, EndHour: 18, ForceFlat: true}
	active, ok := activeMaintenanceWindow(now, true, w)
	if !ok || active.Name != "EOD" || !active.ForceFlat {
		t.Fatalf("expected EOD maintenance active at 16:10")
	}
}

func TestInMinuteWindow(t *testing.T) {
	if !inMinuteWindow(22, 15, 22, 0, 23, 30) {
		t.Fatalf("expected 22:15 in 22:00-23:30")
	}
	if !inMinuteWindow(23, 29, 22, 0, 23, 30) {
		t.Fatalf("expected 23:29 in 22:00-23:30")
	}
	if inMinuteWindow(23, 30, 22, 0, 23, 30) {
		t.Fatalf("expected 23:30 out of 22:00-23:30")
	}
	if !inMinuteWindow(0, 30, 22, 0, 1, 0) {
		t.Fatalf("expected 00:30 in 22:00-01:00")
	}
}

func TestActiveMaintenanceWindowMinutePrecision(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	now := time.Date(2026, 3, 3, 16, 20, 0, 0, loc)
	w := maintenanceWindow{Name: "EOD", Enabled: true, StartHour: 16, StartMin: 0, EndHour: 18, EndMin: 0, ForceFlat: true}
	active, ok := activeMaintenanceWindow(now, true, w)
	if !ok || active.Name != "EOD" {
		t.Fatalf("expected EOD maintenance active at 16:20")
	}
	after := time.Date(2026, 3, 3, 18, 5, 0, 0, loc)
	_, ok = activeMaintenanceWindow(after, true, w)
	if ok {
		t.Fatalf("expected no maintenance at 18:05")
	}
}

func TestRealizedFromFill(t *testing.T) {
	pnl, pct := realizedFromFill("BUY", 100, 105, 2)
	if pnl <= 0 || pct <= 0 {
		t.Fatalf("expected long profit, got pnl=%.4f pct=%.4f", pnl, pct)
	}
	pnl, pct = realizedFromFill("SELL", 100, 95, 2)
	if pnl <= 0 || pct <= 0 {
		t.Fatalf("expected short profit, got pnl=%.4f pct=%.4f", pnl, pct)
	}
}

func TestPaperStateSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "paper_state.json")
	p := &paperTrader{
		enabled:   true,
		startBal:  1000,
		balance:   1123.45,
		reserve:   50,
		stateFile: path,
		positions: map[string]*paperPosition{
			"BTCUSDT": {Symbol: "BTCUSDT", Side: "BUY", Entry: 100, Qty: 1.25, OpenedAt: time.Now().UTC()},
		},
		dayStats: map[string]*paperDayStats{
			"2026-03-03": {Trades: 2, Wins: 1, Losses: 1, Net: 12.3, Reasons: map[string]int{"TP1": 1, "SL": 1}},
		},
	}
	if err := p.save(); err != nil {
		t.Fatalf("save error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected state file: %v", err)
	}
	q := &paperTrader{
		enabled:   true,
		stateFile: path,
		positions: map[string]*paperPosition{},
		dayStats:  map[string]*paperDayStats{},
	}
	if err := q.load(); err != nil {
		t.Fatalf("load error: %v", err)
	}
	if q.balance != p.balance {
		t.Fatalf("balance mismatch got=%.2f want=%.2f", q.balance, p.balance)
	}
	if len(q.positions) != 1 {
		t.Fatalf("positions mismatch got=%d", len(q.positions))
	}
	if q.positions["BTCUSDT"] == nil || q.positions["BTCUSDT"].Qty <= 0 {
		t.Fatalf("missing restored position")
	}
	if q.dayStats["2026-03-03"] == nil || q.dayStats["2026-03-03"].Trades != 2 {
		t.Fatalf("day stats not restored")
	}
}

func TestParseSymbolMinutesMap(t *testing.T) {
	m := parseSymbolMinutesMap("BTCUSDT:480, ETHUSDT:240, bad, SOLUSDT:abc")
	if len(m) != 2 {
		t.Fatalf("expected 2 valid entries, got %d", len(m))
	}
	if m["BTCUSDT"] != 480*time.Minute {
		t.Fatalf("btc interval mismatch: %v", m["BTCUSDT"])
	}
	if m["ETHUSDT"] != 240*time.Minute {
		t.Fatalf("eth interval mismatch: %v", m["ETHUSDT"])
	}
}

func TestReserveLockGateHalfReserveMath(t *testing.T) {
	g := &reserveLockGate{
		enabled:     true,
		lossPct:     40,  // lock when 40% of reserve is gone
		recoveryPct: 100, // unlock only at full reserve recovery
	}
	// Account baseline 100, reserve target 50, usable target 50.
	g.ensureTarget(100, 50)

	// At base=79, reserve-equivalent is 29 (< 30), should lock.
	if !g.block(79) {
		t.Fatalf("expected lock at base 79")
	}
	// Stays locked until base recovers to 100 (full reserve recovery).
	if !g.block(95) {
		t.Fatalf("expected still locked below full recovery")
	}
	if g.block(100) {
		t.Fatalf("expected unlock at full recovery")
	}
}

func TestAdjustBracketParamsCapsAndSoften(t *testing.T) {
	t.Setenv("LIVE_TP1_MAX_R", "2.5")
	t.Setenv("LIVE_TP2_MAX_R", "4.0")
	t.Setenv("LIVE_TP3_MAX_R", "6.0")
	t.Setenv("LIVE_STOP_WIDEN_MULT", "1.25")
	t.Setenv("LIVE_SOFTEN_CONF_MAX", "0.65")

	stop, tp1, tp2, tp3 := adjustBracketParams(
		"failed_auction_magnet",
		"A+",
		inplay.StateInPlay,
		0.62,
		0.0,
		0.01, // 1%
		15.0, // unrealistic
		30.0, // unrealistic
		50.0, // unrealistic
		0.0025,
		0.08,
	)
	if stop <= 0.01 {
		t.Fatalf("expected widened stop, got %.4f", stop)
	}
	if tp1 > 2.5 || tp2 > 4.0 || tp3 > 6.0 {
		t.Fatalf("tp caps not enforced tp1=%.2f tp2=%.2f tp3=%.2f", tp1, tp2, tp3)
	}
}

func TestOrderbookSupportsEntry(t *testing.T) {
	ob := aster.OrderBook{
		Bids: [][2]float64{{100, 10}, {99.9, 8}, {99.8, 6}},
		Asks: [][2]float64{{100.1, 5}, {100.2, 4}, {100.3, 3}},
	}
	if !orderbookSupportsEntry(ob, "BUY", 3, 1.05, 20) {
		t.Fatalf("expected buy to pass orderbook filter")
	}
	if orderbookSupportsEntry(ob, "SELL", 3, 1.20, 20) {
		t.Fatalf("expected sell to fail with weak ask imbalance")
	}
}

func TestOrderbookEntryDecisionReasons(t *testing.T) {
	empty := aster.OrderBook{}
	ok, reason, _, _ := orderbookEntryDecision(empty, "BUY", 5, 1.1, 10)
	if ok || reason != "orderbook_empty" {
		t.Fatalf("expected empty reason, got ok=%v reason=%s", ok, reason)
	}

	wide := aster.OrderBook{
		Bids: [][2]float64{{100, 10}},
		Asks: [][2]float64{{102, 10}},
	}
	ok, reason, spread, _ := orderbookEntryDecision(wide, "BUY", 5, 1.1, 10)
	if ok || reason != "orderbook_spread" || spread <= 10 {
		t.Fatalf("expected spread reject, got ok=%v reason=%s spread=%.2f", ok, reason, spread)
	}

	imb := aster.OrderBook{
		Bids: [][2]float64{{100, 1}},
		Asks: [][2]float64{{100.05, 100}},
	}
	ok, reason, _, ratio := orderbookEntryDecision(imb, "BUY", 1, 1.1, 20)
	if ok || reason != "orderbook_imbalance" || ratio >= 1.1 {
		t.Fatalf("expected imbalance reject, got ok=%v reason=%s ratio=%.3f", ok, reason, ratio)
	}
}

func TestContinuationGuardReasonBlocksExhaustion(t *testing.T) {
	t.Setenv("LIVE_LATE_ENTRY_REQUIRE_UTC1H_RESET", "0")
	cfg := entryQualityConfig{BlockContExhaustion: true, DayUTCMaturityBrake: true, DayUTCMaturityPct: 25, RequireFreshPullback: true}
	c := candidate{
		Side:         "BUY",
		Strat:        "continuation_fast",
		TriggerState: "OF_EXHAUSTION",
		DayUTC24h:    31,
		LastClose:    0.99,
		SessionVWAP:  1.01,
		EMA9:         1.02,
		Entry:        inplay.Entry{EntryStyle: "continuation_fast"},
	}
	if got := continuationGuardReason(c, cfg); got != "continuation_exhausted" {
		t.Fatalf("expected continuation_exhausted, got %q", got)
	}
}

func TestDirectionalConflictRejectReasonBlocksContradictoryContinuation(t *testing.T) {
	t.Setenv("LIVE_DIRECTIONAL_CONFLICT_BLOCK_ENABLE", "1")
	t.Setenv("LIVE_DIRECTIONAL_CONFLICT_BLOCK_PCT", "3.0")
	c := candidate{
		Side:      "BUY",
		Strat:     "continuation_fast",
		DayUTC24h: -4.8,
		Entry:     inplay.Entry{Symbol: "UAIUSDT", EntryStyle: "pullback_long"},
	}
	if got := directionalConflictRejectReason(c); got != "directional_dayutc_conflict" {
		t.Fatalf("expected directional_dayutc_conflict, got %q", got)
	}
}

func TestMarkProtectionPendingDoesNotResetManageAlertCooldown(t *testing.T) {
	now := time.Date(2026, 3, 26, 22, 1, 0, 0, time.UTC)
	prev := now.Add(-1 * time.Minute)
	p := &livePosition{
		Symbol:              "SIRENUSDT",
		Side:                "SHORT",
		LastManageFailAt:    prev,
		LastManageFailCause: "exchange_immediate_trigger_retry_failed",
	}

	markProtectionPending(p, now, "exchange_immediate_trigger_retry_failed")

	if !p.ProtectionPending {
		t.Fatalf("expected protection pending")
	}
	if !p.LastManageFailAt.Equal(prev) {
		t.Fatalf("expected manage alert timestamp to stay unchanged, got=%v want=%v", p.LastManageFailAt, prev)
	}
	if p.LastManageFailCause != "exchange_immediate_trigger_retry_failed" {
		t.Fatalf("expected manage fail cause preserved, got %q", p.LastManageFailCause)
	}
	if !p.ProtectionRetryAfter.After(now) {
		t.Fatalf("expected retry-after to be scheduled after now")
	}
}

func TestChurnRejectReasonLocksRepeatedStops(t *testing.T) {
	t.Setenv("LIVE_SYMBOL_QUICK_LOSS_LOCK_MIN", "0")
	mem := map[string]*sessionChurn{}
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	c := candidate{
		Side:      "BUY",
		Strat:     "continuation_fast",
		DayUTC24h: 28,
		Entry:     inplay.Entry{Symbol: "LYNUSDT", EntryStyle: "continuation_fast"},
	}
	markSessionStop(mem, now.Add(-20*time.Minute), "LYNUSDT", "BUY", 5, -3.1, 28)
	markSessionStop(mem, now.Add(-10*time.Minute), "LYNUSDT", "BUY", 6, -2.8, 31)
	if got := churnRejectReason(mem, now, c); got != "extended_reentry_lock" {
		t.Fatalf("expected extended_reentry_lock, got %q", got)
	}
}

func TestContinuationGuardReasonBlocksLateShortWithoutReset(t *testing.T) {
	t.Setenv("LIVE_LATE_ENTRY_DAYUTC_BRAKE_PCT", "25")
	t.Setenv("LIVE_CONT_FAST_LATE_MIN_SLOPE", "0.16")
	cfg := entryQualityConfig{DayUTCMaturityBrake: true, DayUTCMaturityPct: 25, RequireFreshPullback: true}
	c := candidate{
		Side:      "SELL",
		Strat:     "continuation_fast",
		DayUTC24h: -37.0,
		Entry:     inplay.Entry{EntryStyle: "continuation_fast", ScoreSlope: 0.04},
	}
	if got := continuationGuardReason(c, cfg); got != "late_extension_no_reset" {
		t.Fatalf("expected late_extension_no_reset, got %q", got)
	}
	c.RetestHold = true
	c.SetupFamily = "breakout_retest"
	if got := continuationGuardReason(c, cfg); got != "" {
		t.Fatalf("expected reset/retest to clear late-entry block, got %q", got)
	}
}

func TestContinuationGuardAllowsLeaderPullbackWithoutUtc1hReset(t *testing.T) {
	t.Setenv("LIVE_LATE_ENTRY_DAYUTC_BRAKE_PCT", "25")
	t.Setenv("LIVE_LATE_ENTRY_LEADER_SCORE_MIN", "96")
	t.Setenv("LIVE_LATE_ENTRY_LEADER_SLOPE_MIN", "0.14")
	t.Setenv("LIVE_LATE_ENTRY_LEADER_RANK_MAX", "1.5")
	cfg := entryQualityConfig{DayUTCMaturityBrake: true, DayUTCMaturityPct: 25, RequireFreshPullback: true}
	c := candidate{
		Side:        "BUY",
		Strat:       "continuation_fast",
		SetupFamily: "micro_pullback_continuation",
		DayUTC24h:   27.0,
		Entry: inplay.Entry{
			EntryStyle:   "pullback_long",
			CurrentScore: 99.0,
			ScoreSlope:   0.22,
			Rank:         1,
			State:        inplay.StateInPlay,
		},
	}
	if got := continuationGuardReason(c, cfg); got != "" {
		t.Fatalf("expected leader pullback override, got %q", got)
	}
}

func TestChurnRejectReasonQuickLossLockExtremeSymbol(t *testing.T) {
	t.Setenv("LIVE_CHURN_LOCK_ENABLE", "1")
	t.Setenv("LIVE_SYMBOL_QUICK_LOSS_LOCK_COUNT", "1")
	t.Setenv("LIVE_SYMBOL_QUICK_LOSS_LOCK_MIN", "60")
	t.Setenv("LIVE_SYMBOL_QUICK_LOSS_DAYUTC_PCT", "25")
	mem := map[string]*sessionChurn{}
	now := time.Date(2026, 3, 20, 15, 0, 0, 0, time.UTC)
	c := candidate{
		Side:      "SELL",
		Strat:     "continuation_fast",
		DayUTC24h: -31.0,
		Entry:     inplay.Entry{Symbol: "LYNUSDT", EntryStyle: "pullback_short"},
	}
	markSessionStop(mem, now.Add(-10*time.Minute), c.Entry.Symbol, c.Side, 4, -3.5, c.DayUTC24h)
	if got := churnRejectReason(mem, now, c); got != "quick_loss_symbol_lock" {
		t.Fatalf("expected quick_loss_symbol_lock, got %q", got)
	}
	c.RetestHold = true
	if got := churnRejectReason(mem, now, c); got != "" {
		t.Fatalf("expected structure reset to clear quick loss lock, got %q", got)
	}
}

func TestSideDominanceRejectReasonBlocksWeakerOppositeCandidate(t *testing.T) {
	t.Setenv("LIVE_SIDE_DOMINANCE_ENABLE", "1")
	t.Setenv("LIVE_SIDE_DOMINANCE_MIN_STRONGER", "2")
	t.Setenv("LIVE_SIDE_DOMINANCE_MIN_RANK_GAP", "8")
	t.Setenv("LIVE_SIDE_DOMINANCE_MAX_SCORE_ALLOW", "96")
	t.Setenv("LIVE_SIDE_DOMINANCE_CONFLICT_DAYUTC_PCT", "2.5")
	c := candidate{
		Side:      "BUY",
		Strat:     "continuation_fast",
		DayUTC24h: -4.8,
		FinalRank: 146.91,
		Entry:     inplay.Entry{Symbol: "UAIUSDT", CurrentScore: 93.89},
	}
	ranked := []candidate{
		{Side: "SELL", Strat: "continuation_fast", FinalRank: 170.0, Entry: inplay.Entry{Symbol: "PIPPINUSDT", CurrentScore: 106.33}},
		{Side: "SELL", Strat: "continuation_fast", FinalRank: 158.0, Entry: inplay.Entry{Symbol: "RIVERUSDT", CurrentScore: 100.95}},
		c,
	}
	if got := sideDominanceRejectReason(c, ranked); got != "side_dominance_block" {
		t.Fatalf("expected side_dominance_block, got %q", got)
	}
}

func TestActiveWinnerRejectReasonPrefersOpenWinner(t *testing.T) {
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	execMgr := &liveExecManager{positions: map[string]*livePosition{
		"DEGOUSDT": {
			Symbol:                "DEGOUSDT",
			Side:                  "SELL",
			State:                 execOpen,
			EntryPrice:            0.70,
			RemainingQty:          10,
			LastMark:              0.66,
			Sponsored:             true,
			LastConfluenceRefresh: now.Add(-2 * time.Minute),
			EntrySource:           "BOT",
		},
	}}
	longCurrent := map[string]inplay.Entry{
		"LYNUSDT": {Symbol: "LYNUSDT", CurrentScore: 96},
	}
	shortCurrent := map[string]inplay.Entry{
		"DEGOUSDT": {Symbol: "DEGOUSDT", CurrentScore: 108},
	}
	meta := map[string]symbolMeta{
		"DEGOUSDT": {LastPrice: 0.66},
		"LYNUSDT":  {LastPrice: 0.09},
	}
	c := candidate{
		Side:  "BUY",
		Conf:  0.72,
		Entry: inplay.Entry{Symbol: "LYNUSDT", CurrentScore: 95},
	}
	if got := activeWinnerRejectReason(now, c, execMgr, nil, meta, longCurrent, shortCurrent); got != "active_winner_stronger" {
		t.Fatalf("expected active_winner_stronger, got %q", got)
	}
}

func TestComputeEntryScoreBreakdown(t *testing.T) {
	c := candidate{
		Entry: inplay.Entry{
			Symbol:         "BTCUSDT",
			CurrentGrade:   "A",
			CurrentScore:   92,
			ScoreSlope:     0.18,
			State:          inplay.StateInPlay,
			Rank:           88,
			Momentum:       true,
			TimeInStateMin: 4,
		},
		Side:           "BUY",
		Strat:          "continuation_fast",
		Conf:           0.72,
		VolumeRatio:    1.9,
		SpreadBps:      5,
		DepthBid:       80000,
		DepthAsk:       76000,
		BookImbalance:  0.25,
		LastClose:      100,
		SessionVWAP:    99.4,
		EMA9:           99.6,
		FundingRate:    0.0001,
		LifecycleStage: "READY",
	}
	cfg := entryQualityConfig{
		ScoreWeightDiscovery: 0.35,
		ScoreWeightTrigger:   0.40,
		ScoreWeightExecution: 0.25,
	}
	d, trig, execScore, combo, reasons := computeEntryScoreBreakdown(c, cfg)
	if d <= 0 || trig <= 0 || execScore <= 0 || combo <= 0 {
		t.Fatalf("expected positive scores got d=%.2f trig=%.2f exec=%.2f combo=%.2f", d, trig, execScore, combo)
	}
	if len(reasons) != 0 {
		t.Fatalf("expected no reject reasons, got %v", reasons)
	}
}

func TestCloseFromRemoteSnapshotClosesWithoutREST(t *testing.T) {
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	m := &liveExecManager{}
	p := &livePosition{
		Symbol:       "BTCUSDT",
		Side:         "BUY",
		State:        execOpen,
		CreatedAt:    now.Add(-5 * time.Minute),
		EntryPrice:   100,
		RemainingQty: 2,
	}
	changed, err := m.closeFromRemoteSnapshot(now, p, 101, "POSITION_FLAT_REMOTE")
	if err != nil {
		t.Fatalf("closeFromRemoteSnapshot error: %v", err)
	}
	if !changed {
		t.Fatalf("expected closeFromRemoteSnapshot to report change")
	}
	if p.State != execClosed || p.CloseReason != "POSITION_FLAT_REMOTE" {
		t.Fatalf("unexpected close state: %+v", p)
	}
	if p.RemainingQty != 0 {
		t.Fatalf("expected remaining qty to be zero, got %.4f", p.RemainingQty)
	}
}

func TestActiveRecentRejectSymbolsPrunesExpired(t *testing.T) {
	now := time.Now().UTC()
	mem := map[string]recentRejectMemory{
		"BTCUSDT": {Symbol: "BTCUSDT", ExpiresAt: now.Add(30 * time.Second)},
		"ETHUSDT": {Symbol: "ETHUSDT", ExpiresAt: now.Add(-time.Second)},
	}
	got := activeRecentRejectSymbols(now, mem)
	if len(got) != 1 || got[0] != "BTCUSDT" {
		t.Fatalf("unexpected active reject symbols: %v", got)
	}
	if _, ok := mem["ETHUSDT"]; ok {
		t.Fatalf("expected expired symbol to be pruned")
	}
}

func TestShouldExitOnMomentumFade(t *testing.T) {
	mv := momentumView{
		Long:  &inplay.Entry{State: inplay.StateCooling, ScoreSlope: -0.02},
		Short: &inplay.Entry{State: inplay.StateInPlay, ScoreSlope: 0.10},
	}
	if !shouldExitOnMomentumFade("BUY", mv, 0.0) {
		t.Fatalf("expected BUY momentum fade exit")
	}
	mv2 := momentumView{
		Short: &inplay.Entry{State: inplay.StateCooling, ScoreSlope: -0.04},
		Long:  &inplay.Entry{State: inplay.StateInPlay, ScoreSlope: 0.03},
	}
	if !shouldExitOnMomentumFade("SELL", mv2, 0.0) {
		t.Fatalf("expected SELL momentum fade exit")
	}
}

func TestBuildMomentumIndexUsesRawSymbol(t *testing.T) {
	longs := []inplay.Entry{{Symbol: "POWER-USD", State: inplay.StateInPlay, ScoreSlope: 0.1}}
	shorts := []inplay.Entry{{Symbol: "PIPPINUSDT", State: inplay.StateCooling, ScoreSlope: -0.1}}
	idx := buildMomentumIndex(longs, shorts)
	if idx["POWERUSDT"].Long == nil {
		t.Fatalf("expected POWERUSDT long index")
	}
	if idx["PIPPINUSDT"].Short == nil {
		t.Fatalf("expected PIPPINUSDT short index")
	}
}

func TestPreEODExitReason(t *testing.T) {
	mvFade := momentumView{
		Long: &inplay.Entry{State: inplay.StateCooling, ScoreSlope: -0.01},
	}
	if got := preEODExitReason("BUY", mvFade, 1.25, 0.30); got != "PRE_EOD_MOMENTUM_FADE" {
		t.Fatalf("expected momentum fade reason, got %s", got)
	}
	mvStable := momentumView{
		Long: &inplay.Entry{State: inplay.StateInPlay, ScoreSlope: 0.03},
	}
	if got := preEODExitReason("BUY", mvStable, 0.20, 0.30); got != "PRE_EOD_WEAK_PNL" {
		t.Fatalf("expected weak pnl reason, got %s", got)
	}
	if got := preEODExitReason("BUY", mvStable, 0.90, 0.30); got != "" {
		t.Fatalf("expected no exit reason, got %s", got)
	}
}

func TestInPreEODEntryBlockStillCalculatesWindow(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	eod := maintenanceWindow{Name: "EOD", StartHour: 16, StartMin: 0, EndHour: 18, EndMin: 0}
	now := time.Date(2026, 3, 22, 15, 30, 0, 0, loc)
	if !inPreEODEntryBlock(now, eod, 60) {
		t.Fatalf("expected helper to identify pre-EOD block window")
	}
}

func TestFilterBlockedScored(t *testing.T) {
	rows := []market.Scored{
		{Market: market.Market{Symbol: "XAUUSDT"}},
		{Market: market.Market{Symbol: "BTCUSDT"}},
		{Market: market.Market{Symbol: "REM-USD"}},
	}
	blocked := map[string]struct{}{
		"XAUUSDT": {},
		"REMUSDT": {},
	}
	out := filterBlockedScored(rows, blocked)
	if len(out) != 1 || out[0].Symbol != "BTCUSDT" {
		t.Fatalf("unexpected filtered rows: %+v", out)
	}
}

func TestPaperLossStreakLock(t *testing.T) {
	now := time.Now().UTC()
	p := &paperTrader{
		enabled:       true,
		balance:       1000,
		positions:     map[string]*paperPosition{},
		dayStats:      map[string]*paperDayStats{},
		lastExitAt:    map[string]time.Time{},
		lastExitLoss:  map[string]bool{},
		lossStreak:    map[string]int{},
		lockUntil:     map[string]time.Time{},
		maxLossStreak: 1,
		lossLock:      60 * time.Minute,
	}
	pos := &paperPosition{
		Symbol:   "BARDUSDT",
		Side:     "BUY",
		Entry:    1.50,
		Qty:      100,
		OpenedAt: now.Add(-10 * time.Minute),
	}
	p.positions["BARDUSDT"] = pos
	p.exitPortion(now, pos, "SL", 1.45, pos.Qty, symbolMeta{LastPrice: 1.45}, aster.OrderBook{})
	if !p.lockUntil["BARDUSDT"].After(now) {
		t.Fatalf("expected BARDUSDT lock after loss streak")
	}
}

func TestResolvePaperFeeProfile(t *testing.T) {
	mk, tk := resolvePaperFeeProfile("pro")
	if mk != 0.5 || tk != 4.0 {
		t.Fatalf("pro profile mismatch: maker=%.2f taker=%.2f", mk, tk)
	}
	mk, tk = resolvePaperFeeProfile("vip")
	if mk != 0.3 || tk != 3.0 {
		t.Fatalf("vip profile mismatch: maker=%.2f taker=%.2f", mk, tk)
	}
	mk, tk = resolvePaperFeeProfile("unknown")
	if mk != 0.5 || tk != 4.0 {
		t.Fatalf("default profile mismatch: maker=%.2f taker=%.2f", mk, tk)
	}
}

func TestPayoutNextCloseAfterAtAnchor(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	pm := &payoutManager{
		enabled:    true,
		cycleDays:  7,
		anchorHour: 16,
		anchorMin:  0,
		loc:        loc,
	}
	nowLocal := time.Date(2026, 3, 3, 10, 30, 0, 0, loc)
	closeAt := pm.nextCloseAfter(nowLocal)
	if closeAt.Location().String() != loc.String() {
		t.Fatalf("expected local close location")
	}
	if closeAt.Hour() != 16 || closeAt.Minute() != 0 {
		t.Fatalf("expected 16:00 close got %s", closeAt.Format("15:04"))
	}
	if !closeAt.After(nowLocal.Add(7*24*time.Hour)) && !closeAt.Equal(nowLocal.Add(7*24*time.Hour)) {
		t.Fatalf("expected close >= now+7d")
	}
}

func TestPayoutFallbackRunsOnceAndRollsCycle(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	closeLocal := time.Date(2026, 3, 10, 16, 0, 0, 0, loc)
	nowLocal := time.Date(2026, 3, 10, 16, 16, 0, 0, loc)
	pm := &payoutManager{
		enabled:       true,
		mode:          "telegram_alert",
		onlyIfFlat:    true,
		cycleDays:     7,
		anchorHour:    16,
		anchorMin:     0,
		deadlineMin:   15,
		minPayoutUSDT: 1.0,
		loc:           loc,
		state: payoutState{
			CycleID:        "2026-03-10@1600",
			CycleStart:     closeLocal.AddDate(0, 0, -7).UTC(),
			CycleEnd:       closeLocal.UTC(),
			StartEquity:    100,
			CycleCloseDate: closeLocal.Format("2006-01-02"),
			RunState:       payoutPendingClose,
			DeadlineAt:     closeLocal.Add(15 * time.Minute).UTC(),
		},
	}
	paper := &paperTrader{
		enabled:   true,
		balance:   110,
		positions: map[string]*paperPosition{},
	}
	ms := &maintenanceState{
		LastStartDay: map[string]string{},
		LastEndDay:   map[string]string{},
		FlatDoneDay:  map[string]string{},
		HookDoneDay:  map[string]string{},
	}
	eod := maintenanceWindow{Name: "M2", StartHour: 16, EndHour: 18, ForceFlat: true}
	pm.maybeRun(nowLocal.UTC(), nowLocal, eod, ms, paper, map[string]symbolMeta{}, accountSnapshot{}, nil, nil)
	if pm.state.LastAction != "FALLBACK_PAPER_DEBIT" {
		t.Fatalf("expected fallback action got %s", pm.state.LastAction)
	}
	if pm.state.LastPayoutAmt <= 0 {
		t.Fatalf("expected payout amount > 0")
	}
	balAfter := paper.balance
	pm.maybeRun(nowLocal.UTC(), nowLocal, eod, ms, paper, map[string]symbolMeta{}, accountSnapshot{}, nil, nil)
	if paper.balance != balAfter {
		t.Fatalf("expected idempotent second run")
	}
}

func TestMergeLiveAccountSnapshotTracksBotAndManual(t *testing.T) {
	now := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	m := &liveExecManager{
		reportLoc:   time.UTC,
		dayRealized: map[string]float64{"2026-03-19": 1.25},
		positions: map[string]*livePosition{
			"BTCUSDT": {
				Symbol:      "BTCUSDT",
				Side:        "SELL",
				State:       execOpen,
				CreatedAt:   now.Add(-15 * time.Minute),
				EntrySource: "BOT",
				EntryReason: "continuation_fast",
				StopPrice:   102,
			},
		},
		marketStates: map[string]*aster.MarketState{},
	}
	acct := accountSnapshot{
		AvailableUSDT: 100,
		Positions: []positionView{
			{Symbol: "BTCUSDT", Side: "SHORT", Margin: 20, SizeAbs: 0.01, Entry: 100, Mark: 99, Unreal: 1.0, Leverage: 5},
			{Symbol: "ETHUSDT", Side: "LONG", Margin: 30, SizeAbs: 0.02, Entry: 2000, Mark: 2010, Unreal: 0.2, Leverage: 4},
		},
	}
	got := m.mergeLiveAccountSnapshot(now, acct)
	if got.OpenCount != 2 {
		t.Fatalf("expected 2 open positions, got %d", got.OpenCount)
	}
	if got.BotCount != 1 || got.ManualCount != 1 {
		t.Fatalf("expected bot/manual counts 1/1, got %d/%d", got.BotCount, got.ManualCount)
	}
	if got.Positions[0].Symbol != "ETHUSDT" && got.Positions[0].Symbol != "BTCUSDT" {
		t.Fatalf("unexpected position ordering: %+v", got.Positions)
	}
	var btc, eth *liveAccountPosition
	for i := range got.Positions {
		switch got.Positions[i].Symbol {
		case "BTCUSDT":
			btc = &got.Positions[i]
		case "ETHUSDT":
			eth = &got.Positions[i]
		}
	}
	if btc == nil || eth == nil {
		t.Fatalf("expected both BTC and ETH positions: %+v", got.Positions)
	}
	if btc.Source != "BOT" {
		t.Fatalf("expected BTC source BOT, got %s", btc.Source)
	}
	if btc.StopPrice != 102 {
		t.Fatalf("expected BTC stop copied from local position, got %.4f", btc.StopPrice)
	}
	if eth.Source != "MANUAL" {
		t.Fatalf("expected ETH source MANUAL, got %s", eth.Source)
	}
}

func TestLiveAccountSnapshotRespectsLimit(t *testing.T) {
	m := &liveExecManager{
		liveAccount: liveAccountSnapshot{
			Positions: []liveAccountPosition{
				{Symbol: "A"},
				{Symbol: "B"},
				{Symbol: "C"},
			},
		},
	}
	got := m.LiveAccountSnapshot(2)
	if len(got.Positions) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(got.Positions))
	}
	if got.Positions[0].Symbol != "A" || got.Positions[1].Symbol != "B" {
		t.Fatalf("unexpected limited ordering: %+v", got.Positions)
	}
}

func TestLivePositionBySymbolNormalizesRawSymbol(t *testing.T) {
	m := &liveExecManager{
		liveAccount: liveAccountSnapshot{
			Positions: []liveAccountPosition{
				{Symbol: "BTCUSDT"},
			},
		},
	}
	if _, ok := m.LivePositionBySymbol("BTC"); !ok {
		t.Fatalf("expected BTC to resolve to BTCUSDT position")
	}
	if _, ok := m.LivePositionBySymbol("ETHUSDT"); ok {
		t.Fatalf("did not expect ETHUSDT position")
	}
}

func TestPendingManualRequestNormalizesSymbol(t *testing.T) {
	now := time.Now().UTC()
	m := &liveExecManager{
		manualConfirm:  true,
		manualRequests: map[string]manualManageRequest{},
	}
	if !m.queueManualManagementRequest("LYNUSDT", "BUY", 10, 0.125, 5, 5, now) {
		t.Fatalf("expected pending manual management request to be queued")
	}
	req, ok := m.pendingManualRequest("LYN")
	if !ok {
		t.Fatalf("expected pending request lookup by raw symbol")
	}
	if req.Symbol != "LYNUSDT" || req.Side != "LONG" {
		t.Fatalf("unexpected pending request: %+v", req)
	}
}

func TestHandleCommandManageDecline(t *testing.T) {
	now := time.Now().UTC()
	m := &liveExecManager{
		manualConfirm:  true,
		manualRequests: map[string]manualManageRequest{},
	}
	_ = m.queueManualManagementRequest("LYNUSDT", "BUY", 10, 0.125, 5, 5, now)
	ctx := &telegramCommandCtx{execMgr: m}
	resp := ctx.handleCommand("", "/manage LYN n")
	if !strings.Contains(resp, "MANAGE DECLINED") {
		t.Fatalf("expected decline response, got %s", resp)
	}
	req, ok := m.manualRequests[positionLookupKey("LYNUSDT", "BUY")]
	if !ok || req.Status != "DECLINED" {
		t.Fatalf("expected declined request persisted, got %+v", req)
	}
}

func TestHandleCommandSingleLetterRequiresSymbolWhenMultiplePending(t *testing.T) {
	now := time.Now().UTC()
	m := &liveExecManager{
		manualConfirm:  true,
		manualRequests: map[string]manualManageRequest{},
	}
	_ = m.queueManualManagementRequest("LYNUSDT", "BUY", 10, 0.125, 5, 5, now)
	_ = m.queueManualManagementRequest("BTCUSDT", "SELL", 1, 70000, 10, 3, now)
	ctx := &telegramCommandCtx{execMgr: m}
	resp := ctx.handleCommand("", "y")
	if !strings.Contains(resp, "/manage SYMBOL y") {
		t.Fatalf("expected explicit /manage guidance, got %s", resp)
	}
}

func TestEvaluateSwingHoldKeepsPersistentLeader(t *testing.T) {
	t.Setenv("LIVE_SWING_HOLD_MIN_STATE_MIN", "20")
	t.Setenv("LIVE_SWING_HOLD_MIN_SLOPE", "0.02")
	t.Setenv("LIVE_SWING_HOLD_MIN_SCORE", "88")
	t.Setenv("LIVE_SWING_HOLD_MIN_DAYUTC_PCT", "5")
	t.Setenv("LIVE_SWING_HOLD_MAX_SCORE_OFF_PEAK_PCT", "18")
	t.Setenv("LIVE_SWING_HOLD_MIN_MFE_R", "0.8")
	t.Setenv("LIVE_SWING_HOLD_MIN_SCORE_TOTAL", "0.58")
	mv := momentumView{
		Long: &inplay.Entry{
			Symbol:          "LYNUSDT",
			State:           inplay.StateInPlay,
			CurrentScore:    94,
			ScoreSlope:      0.18,
			TimeInStateMin:  34,
			DayUTCPct:       16,
			ScoreOffPeakPct: 8,
		},
	}
	score, reason, hold := evaluateSwingHold("BUY", mv, true, true, 1.4, true, false, false)
	if !hold {
		t.Fatalf("expected swing hold, got hold=%v score=%.2f reason=%s", hold, score, reason)
	}
}

func TestEvaluateSwingHoldRejectsMissingScannerSupport(t *testing.T) {
	score, reason, hold := evaluateSwingHold("BUY", momentumView{}, false, false, 0.2, false, false, false)
	if hold || reason != "scanner_missing" || score != 0 {
		t.Fatalf("expected scanner_missing rejection, got hold=%v score=%.2f reason=%s", hold, score, reason)
	}
}

func TestDayUTCResetProgressUses1900Anchor(t *testing.T) {
	t.Setenv("LIVE_DAYUTC_RESET_TZ", "America/Chicago")
	t.Setenv("LIVE_DAYUTC_RESET_HOUR", "19")
	t.Setenv("LIVE_DAYUTC_RESET_MIN", "0")
	t.Setenv("LIVE_DAYUTC_RESET_RAMP_MIN", "90")
	t.Setenv("LIVE_DAYUTC_RESET_WEIGHT_FLOOR", "0.35")
	loc, _ := time.LoadLocation("America/Chicago")
	justAfterReset := time.Date(2026, 3, 19, 19, 5, 0, 0, loc)
	later := time.Date(2026, 3, 19, 21, 0, 0, 0, loc)
	early := dayUTCResetProgress(justAfterReset)
	full := dayUTCResetProgress(later)
	if early >= full {
		t.Fatalf("expected early reset progress %.2f to be less than later %.2f", early, full)
	}
	if early < 0.35 || full != 1 {
		t.Fatalf("unexpected progress values early=%.2f full=%.2f", early, full)
	}
}

func TestQualifiesResetImpulseLong(t *testing.T) {
	t.Setenv("LIVE_ENABLE_RESET_IMPULSE", "1")
	t.Setenv("LIVE_RESET_IMPULSE_WINDOW_MIN", "45")
	t.Setenv("LIVE_RESET_IMPULSE_MAX_STATE_MIN", "25")
	t.Setenv("LIVE_RESET_IMPULSE_MIN_SCORE", "72")
	t.Setenv("LIVE_RESET_IMPULSE_MIN_SLOPE", "0.08")
	t.Setenv("LIVE_RESET_IMPULSE_MIN_VOL_RATIO", "1.20")
	t.Setenv("LIVE_RESET_IMPULSE_MIN_DAYUTC_PCT", "8.0")
	t.Setenv("LIVE_DAYUTC_RESET_TZ", "America/Chicago")
	t.Setenv("LIVE_DAYUTC_RESET_HOUR", "19")
	t.Setenv("LIVE_DAYUTC_RESET_MIN", "0")
	loc, _ := time.LoadLocation("America/Chicago")
	now := time.Date(2026, 3, 19, 19, 18, 0, 0, loc)
	c := candidate{
		Side:         "BUY",
		TriggerState: string(triggerImpulseCont),
		TriggerStage: "READY",
		DayUTC24h:    18.0,
		VolumeRatio:  1.55,
		LastClose:    1.25,
		SessionVWAP:  1.20,
		EMA9:         1.22,
		Entry: inplay.Entry{
			State:          inplay.StateInPlay,
			TimeInStateMin: 7,
			CurrentScore:   91,
			ScoreSlope:     0.19,
		},
	}
	name, conf, _ := qualifiesResetImpulse(c, now)
	if name != "reset_impulse_long" {
		t.Fatalf("expected reset_impulse_long, got %q", name)
	}
	if conf < 0.62 {
		t.Fatalf("expected meaningful confidence, got %.2f", conf)
	}
}

func TestContinuationGuardAllowsResetImpulse(t *testing.T) {
	cfg := entryQualityConfig{
		BlockContExhaustion:  true,
		DayUTCMaturityBrake:  true,
		DayUTCMaturityPct:    25,
		RequireFreshPullback: true,
	}
	c := candidate{
		Side:         "BUY",
		Strat:        "reset_impulse_long",
		TriggerState: string(triggerExhaustion),
		DayUTC24h:    32,
	}
	if reason := continuationGuardReason(c, cfg); reason != "" {
		t.Fatalf("expected reset impulse to bypass continuation guard, got %q", reason)
	}
}

func TestClassifySetupFamilyBreakoutRetest(t *testing.T) {
	now := time.Date(2026, 3, 21, 20, 0, 0, 0, time.UTC)
	c := candidate{
		Side:         "BUY",
		Strat:        "continuation_fast",
		RetestHold:   true,
		ResetRebreak: true,
		Entry:        inplay.Entry{EntryStyle: "breakout_hold_long"},
	}
	if got := classifySetupFamily(c, now); got != "breakout_retest" {
		t.Fatalf("expected breakout_retest, got %q", got)
	}
}

func TestClassifySetupFamilyDeepPullback(t *testing.T) {
	now := time.Date(2026, 3, 21, 20, 0, 0, 0, time.UTC)
	c := candidate{
		Side:         "BUY",
		Strat:        "continuation_fast",
		ReclaimHold:  true,
		ExtensionATR: 1.8,
		Entry:        inplay.Entry{EntryStyle: "pullback_long"},
	}
	if got := classifySetupFamily(c, now); got != "deep_pullback_reclaim" {
		t.Fatalf("expected deep_pullback_reclaim, got %q", got)
	}
}

func TestContinuationGuardDoesNotBlockConstructiveExhaustion(t *testing.T) {
	cfg := entryQualityConfig{BlockContExhaustion: true, DayUTCMaturityBrake: false}
	c := candidate{
		Side:         "BUY",
		Strat:        "continuation_fast",
		SetupFamily:  "micro_pullback_continuation",
		TriggerState: string(triggerExhaustion),
		LastClose:    1.08,
		SessionVWAP:  1.05,
		EMA9:         1.06,
		Entry:        inplay.Entry{EntryStyle: "pullback_long", ScoreSlope: 0.08},
	}
	if got := continuationGuardReason(c, cfg); got != "" {
		t.Fatalf("expected no reject, got %q", got)
	}
}

func TestApplyPatternModifiersBullishEngulfingNearReclaim(t *testing.T) {
	now := time.Now().UTC()
	c := candidate{
		Side:        "BUY",
		ReclaimHold: true,
		LastClose:   10.4,
		SessionVWAP: 10.2,
		EMA9:        10.25,
	}
	bars := []features.Candle{
		{Ts: now.Add(-2 * time.Minute), O: 10.30, H: 10.35, L: 10.00, C: 10.10, V: 100},
		{Ts: now.Add(-1 * time.Minute), O: 10.05, H: 10.45, L: 10.00, C: 10.40, V: 160},
	}
	applyPatternModifiers(&c, bars)
	if c.PatternBias <= 0 {
		t.Fatalf("expected bullish pattern bias, got %.3f", c.PatternBias)
	}
}

func TestPullbackContinuationGetsConfidenceWithStructure(t *testing.T) {
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "1")
	t.Setenv("LIVE_CONT_FAST_MIN_SCORE", "65")
	t.Setenv("LIVE_CONT_FAST_MIN_SLOPE", "0.02")
	t.Setenv("LIVE_CONT_FAST_MIN_VOL_RATIO", "1.15")
	t.Setenv("LIVE_CONT_FAST_MIN_OFI_Z", "0.35")
	t.Setenv("LIVE_CONT_FAST_BASE_CONF", "0.58")
	t.Setenv("LIVE_CONT_REQUIRE_STRUCTURE_CONFIRM", "1")
	t.Setenv("LIVE_PULLBACK_CONT_MIN_VOL_RATIO", "0.75")
	t.Setenv("LIVE_PULLBACK_CONT_BASE_CONF", "0.54")
	t.Setenv("LIVE_PULLBACK_CONT_MIN_ABS_OFI_Z", "0.10")
	c := candidate{
		Side:        "BUY",
		SetupFamily: "micro_pullback_continuation",
		LastClose:   10.4,
		SessionVWAP: 10.2,
		EMA9:        10.25,
		VolumeRatio: 0.82,
		OFIZ:        -0.05,
		OFISamples:  10,
		ReclaimHold: true,
		Entry: inplay.Entry{
			EntryStyle:   "pullback_long",
			State:        inplay.StateHeating,
			CurrentScore: 92,
			ScoreSlope:   0.14,
		},
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC))
	if got.Strat != "continuation_fast" {
		t.Fatalf("expected continuation_fast, got %q reject=%q", got.Strat, got.RejectReason)
	}
	if got.Conf <= 0 {
		t.Fatalf("expected positive confidence, got %.3f", got.Conf)
	}
}

func TestPositionLookupKeyNormalizesAsterSymbols(t *testing.T) {
	if got, want := positionLookupKey("LYN-USD", "buy"), "LYN|LONG"; got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
	if got, want := positionLookupKey("LYNUSDT", "BUY"), "LYN|LONG"; got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestClassifyRejectReasonTreatsMissedOpportunityReadyAsSoft(t *testing.T) {
	if got := classifyRejectReason("missed_opportunity_ready"); got != rejectClassSoftConfirm {
		t.Fatalf("expected soft classification, got %q", got)
	}
}

func TestMergeLiveAccountSnapshotMatchesBotPositionByCanonicalSymbol(t *testing.T) {
	m := &liveExecManager{
		positions: map[string]*livePosition{
			"LYN-USD": {
				Symbol:      "LYN-USD",
				Side:        "BUY",
				State:       execOpen,
				CreatedAt:   time.Now().UTC().Add(-5 * time.Minute),
				EntrySource: "BOT",
				EntryReason: "TEST",
				StopPrice:   0.1234,
			},
		},
	}
	now := time.Now().UTC()
	snap := m.mergeLiveAccountSnapshot(now, accountSnapshot{
		AvailableUSDT: 50,
		Positions: []positionView{
			{Symbol: "LYNUSDT", Side: "BUY", SizeAbs: 100, Entry: 0.12, Mark: 0.13, Unreal: 1, Leverage: 5, Margin: 10},
		},
	})
	if len(snap.Positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(snap.Positions))
	}
	if snap.Positions[0].Source != "BOT" {
		t.Fatalf("expected BOT source, got %s", snap.Positions[0].Source)
	}
}

func TestApplySimpleContinuationFallbackEliteSoftRejectUsesStarter(t *testing.T) {
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "1")
	t.Setenv("LIVE_CONT_FAST_MIN_SCORE", "65")
	t.Setenv("LIVE_CONT_FAST_MIN_SLOPE", "0.02")
	t.Setenv("LIVE_CONT_FAST_MIN_VOL_RATIO", "1.15")
	t.Setenv("LIVE_CONT_FAST_MIN_OFI_Z", "0.35")
	t.Setenv("LIVE_CONT_FAST_BASE_CONF", "0.58")
	t.Setenv("LIVE_CONT_REQUIRE_STRUCTURE_CONFIRM", "0")
	t.Setenv("LIVE_STARTER_FINAL_RANK_MIN", "0.72")
	t.Setenv("LIVE_STARTER_MIN_VOL_RATIO", "0.80")
	t.Setenv("LIVE_STARTER_ALLOW_BELOW_VWAP_EMA_SOFT", "1")
	c := candidate{
		Side:          "BUY",
		CombinedScore: 0.83,
		VolumeRatio:   0.86,
		OFIZ:          0.42,
		OFISamples:    12,
		LastClose:     10.10,
		SessionVWAP:   10.20,
		EMA9:          10.25,
		Entry: inplay.Entry{
			Symbol:       "LYNUSDT",
			CurrentGrade: "A",
			State:        inplay.StateInPlay,
			CurrentScore: 94,
			ScoreSlope:   0.14,
		},
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC))
	if got.Strat != "continuation_fast_starter" {
		t.Fatalf("expected continuation_fast_starter, got %q reject=%q", got.Strat, got.RejectReason)
	}
	if got.Conf <= 0 {
		t.Fatalf("expected starter confidence, got %.3f", got.Conf)
	}
}

func TestResolveLadderPlanAllowsWinnerAdd(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"LYNUSDT": {
				Symbol:         "LYNUSDT",
				Side:           "BUY",
				State:          execOpen,
				EntrySource:    "BOT",
				EntryPrice:     1.00,
				RemainingQty:   100,
				DeployedMargin: 10,
			},
		},
		ladderCfg: loadLadderConfig(10),
	}
	meta := map[string]symbolMeta{
		"LYNUSDT": {LastPrice: 1.02},
	}
	c := candidate{
		Side:        "BUY",
		Strat:       "continuation_fast",
		LastClose:   1.02,
		SessionVWAP: 1.01,
		EMA9:        1.01,
		ReclaimHold: true,
		Entry:       inplay.Entry{Symbol: "LYNUSDT", State: inplay.StateInPlay},
		Sig: strategies.Signal{
			Entry: 1.02,
			TP1:   1.06,
		},
	}
	plan := resolveLadderPlan(time.Date(2026, 3, 25, 10, 30, 0, 0, time.UTC), c, execMgr, meta)
	if !plan.IsAdd {
		t.Fatalf("expected winner add plan, got %+v", plan)
	}
	if plan.MarginUSDT != 10 {
		t.Fatalf("expected 10 usdt add, got %.2f", plan.MarginUSDT)
	}
}

func TestResolveLadderPlanRejectsLoserAdd(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"LYNUSDT": {
				Symbol:         "LYNUSDT",
				Side:           "BUY",
				State:          execOpen,
				EntrySource:    "BOT",
				EntryPrice:     1.00,
				RemainingQty:   100,
				DeployedMargin: 10,
			},
		},
		ladderCfg: loadLadderConfig(10),
	}
	meta := map[string]symbolMeta{
		"LYNUSDT": {LastPrice: 0.99},
	}
	c := candidate{
		Side:        "BUY",
		Strat:       "continuation_fast",
		LastClose:   0.99,
		SessionVWAP: 0.98,
		EMA9:        0.98,
		ReclaimHold: true,
		Entry:       inplay.Entry{Symbol: "LYNUSDT", State: inplay.StateInPlay},
		Sig: strategies.Signal{
			Entry: 0.99,
			TP1:   1.03,
		},
	}
	plan := resolveLadderPlan(time.Date(2026, 3, 25, 10, 30, 0, 0, time.UTC), c, execMgr, meta)
	if plan.IsAdd || plan.RejectReason == "" {
		t.Fatalf("expected loser add reject, got %+v", plan)
	}
}

func TestResolveLadderPlanAllowsManualManagedCatchUpAddAfterReconstructedTPHits(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"SIRENUSDT": {
				Symbol:            "SIRENUSDT",
				Side:              "SELL",
				State:             execOpen,
				EntrySource:       "MANUAL",
				EntryReason:       "manual_managed_live",
				ManageAnchorPrice: 1.0825,
				EntryPrice:        1.2437,
				RemainingQty:      180,
				DeployedMargin:    20,
				HitTP1:            true,
				HitTP2:            true,
				HitTP3:            true,
			},
		},
		ladderCfg: loadLadderConfig(10),
	}
	meta := map[string]symbolMeta{
		"SIRENUSDT": {LastPrice: 1.0825},
	}
	c := candidate{
		Side:        "SELL",
		Strat:       "continuation_fast",
		LastClose:   1.0825,
		SessionVWAP: 1.18,
		EMA9:        1.12,
		DayUTC24h:   -43.0,
		RetestHold:  true,
		Entry: inplay.Entry{
			Symbol:       "SIRENUSDT",
			State:        inplay.StateInPlay,
			EntryStyle:   "pullback_short",
			CurrentScore: 98,
			ScoreSlope:   0.22,
			Momentum:     true,
		},
		Sig: strategies.Signal{
			Entry: 1.0825,
			TP1:   1.03,
		},
	}
	plan := resolveLadderPlan(time.Date(2026, 3, 26, 16, 22, 0, 0, time.UTC), c, execMgr, meta)
	if !plan.IsAdd {
		t.Fatalf("expected manual catch-up add plan, got %+v", plan)
	}
	if plan.MarginUSDT != 10 {
		t.Fatalf("expected 10 usdt add, got %.2f", plan.MarginUSDT)
	}
}

func TestManualWouldAddCapitalAllowsManualManagedCatchUpDespiteTP3(t *testing.T) {
	p := &livePosition{
		Symbol:                "SIRENUSDT",
		Side:                  "SELL",
		State:                 execOpen,
		EntrySource:           "MANUAL",
		EntryReason:           "manual_managed_live",
		ManageAnchorPrice:     1.0825,
		EntryPrice:            1.2437,
		RemainingQty:          180,
		HitTP3:                true,
		AddLockedUntilConfirm: false,
		StarterOnly:           false,
	}
	if !manualWouldAddCapital(p, 1.0825, 0.75) {
		t.Fatalf("expected manual catch-up add eligibility despite TP3")
	}
}

func TestTrailCandidateConfirmedFromBarsShortRequiresCloseBelowLevel(t *testing.T) {
	t.Setenv("LIVE_TRAIL_CONFIRM_BARS", "1")
	t.Setenv("LIVE_TRAIL_RETEST_ENABLE", "1")
	candidateAt := time.Date(2026, 3, 26, 22, 10, 20, 0, time.UTC)
	bars := []types.Candle{
		{T: time.Date(2026, 3, 26, 22, 10, 0, 0, time.UTC), O: 1.18, H: 1.19, L: 1.15, C: 1.16},
		{T: time.Date(2026, 3, 26, 22, 11, 0, 0, time.UTC), O: 1.16, H: 1.17, L: 1.12, C: 1.13},
	}
	if !trailCandidateConfirmedFromBars(false, bars, candidateAt, 1.15) {
		t.Fatalf("expected short trail confirmation on close below level")
	}
}

func TestTrailCandidateConfirmedFromBarsShortRejectsFailedCloseBelowLevel(t *testing.T) {
	t.Setenv("LIVE_TRAIL_CONFIRM_BARS", "1")
	t.Setenv("LIVE_TRAIL_RETEST_ENABLE", "1")
	candidateAt := time.Date(2026, 3, 26, 22, 10, 20, 0, time.UTC)
	bars := []types.Candle{
		{T: time.Date(2026, 3, 26, 22, 11, 0, 0, time.UTC), O: 1.14, H: 1.18, L: 1.13, C: 1.155},
	}
	if trailCandidateConfirmedFromBars(false, bars, candidateAt, 1.15) {
		t.Fatalf("expected no short trail confirmation when close reclaims level")
	}
}

func TestSessionPhaseUTCUsesUTCWindows(t *testing.T) {
	if got := sessionPhaseUTC(time.Date(2026, 3, 25, 1, 30, 0, 0, time.UTC)); got != sessionAsiaDev {
		t.Fatalf("expected asia dev, got %s", got)
	}
	if got := sessionPhaseUTC(time.Date(2026, 3, 25, 7, 30, 0, 0, time.UTC)); got != sessionLondonOpen {
		t.Fatalf("expected london open precedence at 07:30 UTC, got %s", got)
	}
	if got := sessionPhaseUTC(time.Date(2026, 3, 25, 21, 0, 0, 0, time.UTC)); got != sessionUTCOffHours {
		t.Fatalf("expected off hours, got %s", got)
	}
}

func TestApplySimpleContinuationFallbackEarlyDevEntry(t *testing.T) {
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "1")
	t.Setenv("LIVE_STARTER_FINAL_RANK_MIN", "0.72")
	t.Setenv("LIVE_CONT_FAST_MIN_SCORE", "65")
	t.Setenv("LIVE_CONT_FAST_MIN_SLOPE", "0.02")
	t.Setenv("LIVE_CONT_FAST_MIN_OFI_Z", "0.35")
	c := candidate{
		Side:          "BUY",
		CombinedScore: 0.84,
		VolumeRatio:   1.05,
		OFIZ:          0.52,
		OFISamples:    12,
		LastClose:     10.15,
		SessionVWAP:   10.05,
		EMA9:          10.02,
		Entry: inplay.Entry{
			Symbol:       "LYNUSDT",
			CurrentGrade: "A",
			State:        inplay.StateHeating,
			CurrentScore: 92,
			ScoreSlope:   0.18,
			Momentum:     true,
			Rank:         2.0,
		},
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 3, 25, 1, 15, 0, 0, time.UTC))
	if got.Strat != "early_dev_entry" {
		t.Fatalf("expected early_dev_entry, got %q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestResolveLadderPlanAllowsStructuredReentry(t *testing.T) {
	now := time.Date(2026, 3, 25, 9, 30, 0, 0, time.UTC)
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"LYNUSDT": {
				Symbol:       "LYNUSDT",
				Side:         "BUY",
				State:        execClosed,
				ClosedAt:     now.Add(-20 * time.Minute),
				EntrySource:  "BOT",
				ReentryCount: 0,
			},
		},
		ladderCfg:  loadLadderConfig(20),
		reentryCfg: loadReentryConfig(20),
	}
	c := candidate{
		Side:  "BUY",
		Strat: "continuation_fast",
		Entry: inplay.Entry{
			Symbol:       "LYNUSDT",
			State:        inplay.StateInPlay,
			EntryStyle:   "pullback_long",
			CurrentGrade: "A",
			CurrentScore: 90,
			ScoreSlope:   0.10,
		},
		LastClose:       1.02,
		SessionVWAP:     1.00,
		EMA9:            1.01,
		ReclaimHold:     true,
		ClosedBreakHold: true,
		CombinedScore:   0.82,
		VolumeRatio:     1.20,
	}
	plan := resolveLadderPlan(now, c, execMgr, map[string]symbolMeta{"LYNUSDT": {LastPrice: 1.02}})
	if !plan.IsReentry {
		t.Fatalf("expected structured reentry plan, got %+v", plan)
	}
	if plan.MarginUSDT != 20 {
		t.Fatalf("expected 20 usdt reentry, got %.2f", plan.MarginUSDT)
	}
}

func TestSessionEntryRejectReasonBlocksFreshAsiaContinue(t *testing.T) {
	c := candidate{
		Side:  "BUY",
		Strat: "continuation_fast",
		Entry: inplay.Entry{CurrentGrade: "A"},
	}
	reason := sessionEntryRejectReason(time.Date(2026, 3, 25, 5, 30, 0, 0, time.UTC), c, ladderPlan{})
	if reason != "asia_continue_no_fresh_entry" {
		t.Fatalf("expected asia continue fresh entry block, got %q", reason)
	}
}

func TestSessionEntryRejectReasonAllowsOffHoursAGradeEntry(t *testing.T) {
	c := candidate{
		Side:          "BUY",
		Strat:         "continuation_fast",
		CombinedScore: 0.81,
		Conf:          0.58,
		VolumeRatio:   1.10,
		Entry: inplay.Entry{
			Symbol:       "LYNUSDT",
			CurrentGrade: "A",
			State:        inplay.StateInPlay,
			Momentum:     true,
		},
	}
	reason := sessionEntryRejectReason(time.Date(2026, 3, 25, 21, 30, 0, 0, time.UTC), c, ladderPlan{})
	if reason != "" {
		t.Fatalf("expected off-hours A-grade entry to pass, got %q", reason)
	}
}

func TestSessionEntryRejectReasonAllowsOffHoursEntry(t *testing.T) {
	c := candidate{
		Side:          "BUY",
		Strat:         "continuation_fast",
		CombinedScore: 0.62,
		Conf:          0.44,
		VolumeRatio:   0.82,
		Entry: inplay.Entry{
			Symbol:       "LYNUSDT",
			CurrentGrade: "B",
			State:        inplay.StateHeating,
			Momentum:     false,
		},
	}
	reason := sessionEntryRejectReason(time.Date(2026, 3, 25, 21, 30, 0, 0, time.UTC), c, ladderPlan{})
	if reason != "" {
		t.Fatalf("expected off-hours entry to pass, got %q", reason)
	}
}

func TestPostWinCooldownRejectReasonBlocksOppositeAfterBigWin(t *testing.T) {
	now := time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC)
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"LYNUSDT": {
				Symbol:         "LYNUSDT",
				Side:           "BUY",
				State:          execClosed,
				ClosedAt:       now.Add(-10 * time.Minute),
				EntrySource:    "BOT",
				DeployedMargin: 20,
				RealizedPnL:    12,
			},
		},
		postWinCooldownCfg: loadPostWinCooldownConfig(),
	}
	c := candidate{
		Side:  "SELL",
		Entry: inplay.Entry{Symbol: "LYNUSDT"},
	}
	if got := postWinCooldownRejectReason(now, c, execMgr); got != "post_win_opposite_cooldown" {
		t.Fatalf("expected post-win opposite cooldown, got %q", got)
	}
}

func TestLoadSafetyConfigUsesStarterMarginForMinAvailable(t *testing.T) {
	t.Setenv("LIVE_MIN_AVAILABLE_USDT", "")
	t.Setenv("LIVE_ENTRY_STARTER_USDT", "10")
	cfg := loadSafetyConfig(0, 25)
	if cfg.minAvailUSDT != 10 {
		t.Fatalf("expected starter-based min available of 10, got %.2f", cfg.minAvailUSDT)
	}
}

func TestLoadLadderConfigDefaultsStarterToTen(t *testing.T) {
	t.Setenv("LIVE_ENTRY_STARTER_USDT", "")
	cfg := loadLadderConfig(0)
	if cfg.StarterUSDT != 10 {
		t.Fatalf("expected starter default of 10, got %.2f", cfg.StarterUSDT)
	}
}

func TestPersistenceSoftBlockMatchesExpectedReasons(t *testing.T) {
	if !persistenceSoftBlock("wall_not_persistent") {
		t.Fatalf("expected wall_not_persistent to be soft")
	}
	if !persistenceSoftBlock("meta_quality:0.45<0.52") {
		t.Fatalf("expected meta_quality to be soft")
	}
	if !persistenceSoftBlock("continuation_no_structure_confirm") {
		t.Fatalf("expected continuation_no_structure_confirm to be soft")
	}
	if persistenceSoftBlock("min_available_usdt") {
		t.Fatalf("expected min_available_usdt to remain hard")
	}
}

func TestPersistenceStrongRequiresEvidence(t *testing.T) {
	cfg := entryQualityConfig{
		PersistenceSoftOverride: true,
		PersistSoftMinSeen:      3,
		PersistSoftMinTopN:      2,
	}
	c := candidate{
		Strat:                  "persistence_entry",
		CombinedScore:          0.82,
		PersistenceSeenCount:   3,
		PersistenceTopNCount:   2,
		PersistenceVolumeTrend: true,
		PersistenceMomentum:    true,
		Entry: inplay.Entry{
			State: inplay.StateHeating,
		},
	}
	if !persistenceStrong(c, cfg) {
		t.Fatalf("expected strong persistence candidate to qualify")
	}
	c.PersistenceTopNCount = 1
	if persistenceStrong(c, cfg) {
		t.Fatalf("expected insufficient topN evidence to fail")
	}
}

func TestPersistenceSoftBlocksOnly(t *testing.T) {
	if !persistenceSoftBlocksOnly([]string{"continuation_no_structure_confirm", "below_vwap_ema"}) {
		t.Fatalf("expected pure soft blockers to pass")
	}
	if persistenceSoftBlocksOnly([]string{"continuation_no_structure_confirm", "min_available_usdt"}) {
		t.Fatalf("expected mixed soft/hard blockers to fail")
	}
}

func TestLoadWatchConfigDefaultsToOneSecond(t *testing.T) {
	t.Setenv("LIVE_WATCHER_SEC", "")
	t.Setenv("LIVE_WATCH_SEC", "")
	t.Setenv("LIVE_PRIORITY_WATCH_EVERY_SEC", "")
	cfg := loadWatchConfig()
	if cfg.Every != time.Second {
		t.Fatalf("expected watcher every 1s, got %v", cfg.Every)
	}
	if cfg.PriorityEvery != time.Second {
		t.Fatalf("expected priority watcher every 1s, got %v", cfg.PriorityEvery)
	}
}

func TestProtectiveStopValid(t *testing.T) {
	if !protectiveStopValid("BUY", 100, 101, 99.5) {
		t.Fatalf("expected long protective stop to be valid")
	}
	if protectiveStopValid("BUY", 100, 101, 100.5) {
		t.Fatalf("expected long protective stop above entry to be invalid")
	}
	if !protectiveStopValid("SELL", 100, 99, 101) {
		t.Fatalf("expected short protective stop to be valid")
	}
	if protectiveStopValid("SELL", 100, 99, 98.5) {
		t.Fatalf("expected short protective stop below mark to be invalid")
	}
}
