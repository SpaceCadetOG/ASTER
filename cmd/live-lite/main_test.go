package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/inplay"
	"go-machine/internal/market"
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
	cfg := entryQualityConfig{BlockContExhaustion: true, DayUTCMaturityBrake: true, DayUTCMaturityPct: 25, RequireFreshPullback: true}
	c := candidate{
		Side:         "BUY",
		Strat:        "continuation_fast",
		TriggerState: "OF_EXHAUSTION",
		DayUTC24h:    31,
		Entry:        inplay.Entry{EntryStyle: "continuation_fast"},
	}
	if got := continuationGuardReason(c, cfg); got != "continuation_exhausted" {
		t.Fatalf("expected continuation_exhausted, got %q", got)
	}
}

func TestChurnRejectReasonLocksRepeatedStops(t *testing.T) {
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
