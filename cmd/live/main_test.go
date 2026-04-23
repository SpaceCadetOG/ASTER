package main

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/features"
	flowfeed "go-machine/internal/flow"
	"go-machine/internal/inplay"
	"go-machine/internal/market"
	"go-machine/internal/notify"
	"go-machine/internal/strategies"
	"go-machine/internal/types"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe create: %v", err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String()
}

func TestRestAuthConfigFromConfigSupportsAgentWallet(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".aster.yaml")
	if err := os.WriteFile(cfgPath, []byte(strings.Join([]string{
		"aster_auth_mode: agent",
		"aster_base_url: https://fapi.asterdex.com",
		"aster_user: 0x93b2137D2Fb5B34D8399956658111eAa7B4DB7b6",
		"aster_signer: 0x93b2137D2Fb5B34D8399956658111eAa7B4DB7b6",
		"aster_private_key: 0xabc123",
		"aster_chain_id: 1666",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("ASTER_CONFIG", cfgPath)
	t.Setenv("ASTER_API_KEY", "")
	t.Setenv("ASTER_API_SECRET", "")
	t.Setenv("ASTER_USER", "")
	t.Setenv("ASTER_SIGNER", "")
	t.Setenv("ASTER_PRIVATE_KEY", "")
	t.Setenv("ASTER_AUTH_MODE", "")
	t.Setenv("ASTER_CHAIN_ID", "")
	t.Setenv("ASTER_BASE_URL", "")
	t.Setenv("EXEC_BASE_URL", "")

	cfg, ok := restAuthConfigFromConfig()
	if !ok {
		t.Fatal("expected config to load agent wallet auth")
	}
	if cfg.AuthMode != "agent" {
		t.Fatalf("expected agent auth mode, got %q", cfg.AuthMode)
	}
	if cfg.User != "0x93b2137D2Fb5B34D8399956658111eAa7B4DB7b6" || cfg.Signer != cfg.User {
		t.Fatalf("unexpected user/signer config: user=%q signer=%q", cfg.User, cfg.Signer)
	}
	if cfg.PrivateKey != "0xabc123" {
		t.Fatalf("expected private key from config, got %q", cfg.PrivateKey)
	}
	if cfg.ChainID != 1666 {
		t.Fatalf("expected chain id 1666, got %d", cfg.ChainID)
	}
	if cfg.BaseURL != "https://fapi.asterdex.com" {
		t.Fatalf("expected mainnet base url, got %q", cfg.BaseURL)
	}
}

func TestRestAuthConfigFromConfigSupportsHMACFallback(t *testing.T) {
	t.Setenv("ASTER_CONFIG", "")
	t.Setenv("ASTER_API_KEY", "key123")
	t.Setenv("ASTER_API_SECRET", "sec456")
	t.Setenv("ASTER_USER", "")
	t.Setenv("ASTER_SIGNER", "")
	t.Setenv("ASTER_PRIVATE_KEY", "")
	t.Setenv("ASTER_AUTH_MODE", "")
	t.Setenv("ASTER_CHAIN_ID", "")
	t.Setenv("ASTER_BASE_URL", "")
	t.Setenv("EXEC_BASE_URL", "")

	cfg, ok := restAuthConfigFromConfig()
	if !ok {
		t.Fatal("expected HMAC config to load")
	}
	if cfg.APIKey != "key123" || cfg.APISecret != "sec456" {
		t.Fatalf("unexpected HMAC creds: key=%q secret=%q", cfg.APIKey, cfg.APISecret)
	}
	if cfg.AuthMode != "auto" {
		t.Fatalf("expected auto auth mode before adapter selection, got %q", cfg.AuthMode)
	}
	if cfg.BaseURL != "https://fapi.asterdex.com" {
		t.Fatalf("expected mainnet base url, got %q", cfg.BaseURL)
	}
}

func TestSignedUserDataBackoffActivatesOn429(t *testing.T) {
	signedUserDataBackoffState.mu.Lock()
	signedUserDataBackoffState.until = time.Time{}
	signedUserDataBackoffState.mu.Unlock()
	t.Setenv("LIVE_SIGNED_USERDATA_BACKOFF_SEC", "60")
	now := time.Now().UTC()
	signedUserDataBackoffObserve(now, fmt.Errorf("http 429 GET /fapi/v3/balance:"))
	if err := signedUserDataBackoffCheck(now.Add(5 * time.Second)); err == nil {
		t.Fatal("expected signed user-data backoff to activate after 429")
	}
}

func TestLoadAccountReportConfigUsesSaferRefreshDefault(t *testing.T) {
	t.Setenv("LIVE_ACCOUNT_REFRESH_SEC", "")
	cfg := loadAccountReportConfig()
	if cfg.RefreshEvery < 10*time.Second {
		t.Fatalf("expected account refresh to be clamped, got %v", cfg.RefreshEvery)
	}
}

func TestFetchAccountSnapshotUsesFreshUserDataState(t *testing.T) {
	state := aster.NewUserDataState()
	state.ApplyAccountUpdateTestOnly(asterUserDataUpdateForTest())
	snap, err := fetchAccountSnapshot(nil, state, nil)
	if err != nil {
		t.Fatalf("expected user-data snapshot without rest error, got %v", err)
	}
	if snap.AvailableUSDT <= 0 {
		t.Fatalf("expected available usdt from user-data snapshot, got %.2f", snap.AvailableUSDT)
	}
	if len(snap.Positions) != 1 || snap.Positions[0].Symbol != "BTCUSDT" {
		t.Fatalf("expected one BTCUSDT position from user-data snapshot, got %+v", snap.Positions)
	}
}

func asterUserDataUpdateForTest() aster.UserDataAccountUpdateTestOnly {
	return aster.UserDataAccountUpdateTestOnly{
		Event:     "ACCOUNT_UPDATE",
		EventTime: time.Now().UTC().UnixMilli(),
		Balances: []aster.UserDataBalanceTestOnly{
			{Asset: "USDT", WalletBalance: "100.00", CrossWallet: "80.00", BalanceChange: "0"},
		},
		Positions: []aster.UserDataPositionTestOnly{
			{Symbol: "BTCUSDT", PositionAmt: "0.01", EntryPrice: "80000", UnrealizedPnL: "5.5", IsolatedWallet: "20", PositionSide: "LONG"},
		},
	}
}

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

func TestBlockedNewRiskWindowReturnsForceFlatReason(t *testing.T) {
	t.Setenv("LIVE_MAINT1_ENABLE", "1")
	t.Setenv("LIVE_MAINT1_START_HOUR", "16")
	t.Setenv("LIVE_MAINT1_START_MIN", "0")
	t.Setenv("LIVE_MAINT1_END_HOUR", "18")
	t.Setenv("LIVE_MAINT1_END_MIN", "0")
	loc, _ := time.LoadLocation("America/Chicago")
	now := time.Date(2026, 3, 3, 16, 20, 0, 0, loc)
	window, reason, blocked := blockedNewRiskWindow(now.UTC(), loc)
	if !blocked {
		t.Fatal("expected maintenance window to block new risk")
	}
	if window.ForceFlat {
		t.Fatalf("expected non-force-flat runtime maintenance window, got %+v", window)
	}
	if reason != blockedMaintenanceWindowReason {
		t.Fatalf("expected %s, got %s", blockedMaintenanceWindowReason, reason)
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

func TestPaperMaybeEnterActuallyOpensPosition(t *testing.T) {
	t.Setenv("LIVE_PAPER_ENABLE", "1")
	t.Setenv("LIVE_STOP_ENGINE_V2_ENABLE", "0")
	t.Setenv("LIVE_PAPER_STATE_FILE", filepath.Join(t.TempDir(), "paper_state.json"))

	setLiveEntryAccountHealthProvider(func() accountHealthSummary {
		return accountHealthSummary{State: "healthy"}
	})
	t.Cleanup(func() {
		setLiveEntryAccountHealthProvider(func() accountHealthSummary {
			return accountHealthSummary{State: "healthy"}
		})
	})

	p := newPaperTrader(true, 0, 5)
	if p == nil || !p.enabled {
		t.Fatalf("expected enabled paper trader")
	}

	c := candidate{
		Side:        "BUY",
		SpreadBps:   4,
		FinalRank:   96,
		VolumeRatio: 1.25,
		DayUTC24h:   8.2,
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			State:        inplay.StateInPlay,
			CurrentGrade: "A",
			CurrentScore: 92,
			ScoreSlope:   0.26,
			Rank:         2.0,
			Momentum:     true,
		},
	}

	meta := map[string]symbolMeta{
		"BTCUSDT": {
			LastPrice: 100.0,
			OpenPrice: 95.0,
		},
	}

	pp, err := p.MaybeEnter(
		time.Now().UTC(),
		c,
		0,
		10.0,
		2,
		meta,
		map[string]aster.OrderBook{},
		map[string]inplay.Entry{},
	)
	if err != nil {
		t.Fatalf("expected paper entry success, got err=%v", err)
	}
	if pp == nil {
		t.Fatalf("expected non-nil paper position")
	}
	if pp.Symbol != "BTCUSDT" {
		t.Fatalf("expected BTCUSDT, got %s", pp.Symbol)
	}
	if _, ok := p.positions["BTCUSDT"]; !ok {
		t.Fatalf("expected position persisted in paper trader map")
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

func TestShouldNotifyManageStatusOnStateOrCauseChange(t *testing.T) {
	p := &livePosition{}
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if !shouldNotifyManageStatus(p, notify.ManageStateAttachingProtection, "ORDER_ILLEGAL_TICK_SIZE", now) {
		t.Fatal("expected first manage status notification")
	}
	if shouldNotifyManageStatus(p, notify.ManageStateAttachingProtection, "ORDER_ILLEGAL_TICK_SIZE", now.Add(30*time.Second)) {
		t.Fatal("did not expect duplicate status/cause notification inside cooldown")
	}
	if !shouldNotifyManageStatus(p, notify.ManageStateAttachingProtection, "MARK_UNAVAILABLE", now.Add(31*time.Second)) {
		t.Fatal("expected notification when cause changes")
	}
	if !shouldNotifyManageStatus(p, notify.ManageStateDegraded, "MARK_UNAVAILABLE", now.Add(32*time.Second)) {
		t.Fatal("expected notification when state changes to degraded")
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

func TestRemotePositionForSideMatchesNormalizedLongShortSides(t *testing.T) {
	rows := []map[string]any{
		{
			"positionAmt": 537.0,
			"entryPrice":  0.09393,
			"markPrice":   0.09410,
		},
	}
	view := remotePositionForSide(rows, "LONG")
	if view.QtyAbs != 537.0 {
		t.Fatalf("expected normalized LONG side match, got %+v", view)
	}
	if view.EntryPrice != 0.09393 {
		t.Fatalf("unexpected entry price: %+v", view)
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

func TestQueueManualManagementRequestKeepsSingleRequestAcrossEntryDrift(t *testing.T) {
	now := time.Now().UTC()
	m := &liveExecManager{
		manualConfirm:  true,
		manualRequests: map[string]manualManageRequest{},
	}
	if !m.queueManualManagementRequest("SIRENUSDT", "SELL", 165, 1.2437, 20.5, 10, now) {
		t.Fatalf("expected initial manual request")
	}
	if !m.queueManualManagementRequest("SIRENUSDT", "SELL", 165.2, 1.2441, 20.7, 10, now.Add(30*time.Second)) {
		t.Fatalf("expected drifted manual request to reuse same pending slot")
	}
	if len(m.manualRequests) != 1 {
		t.Fatalf("expected one manual request after drift, got %d", len(m.manualRequests))
	}
	req := m.manualRequests[positionLookupKey("SIRENUSDT", "SELL")]
	if req.Status != manualRequestPending {
		t.Fatalf("expected pending request, got %+v", req)
	}
	if req.Entry != 1.2441 || req.Qty != 165.2 {
		t.Fatalf("expected request metadata to refresh, got %+v", req)
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

func TestHandleCommandManageExistingManagedTradeRearmsProtection(t *testing.T) {
	m := &liveExecManager{
		positions: map[string]*livePosition{
			"4USDT": {
				Symbol:            "4USDT",
				Side:              "SELL",
				State:             execOpen,
				EntrySource:       manualEntrySourceManaged,
				EntryReason:       manualEntryReasonManaged,
				RemainingQty:      100,
				ProtectionPending: true,
			},
		},
	}
	ctx := &telegramCommandCtx{execMgr: m}
	resp := ctx.handleCommand("", "/manage 4USDT y")
	if !strings.Contains(resp, "MANAGE ACTIVE") {
		t.Fatalf("expected active manage response, got %s", resp)
	}
	if !strings.Contains(resp, "already bot-managed") {
		t.Fatalf("expected already-managed guidance, got %s", resp)
	}
}

func TestActivatePassiveManualImportKeepsPendingRequestAndCreatesPassiveLocal(t *testing.T) {
	now := time.Now().UTC()
	req := manualManageRequest{
		Key:         positionLookupKey("SIRENUSDT", "SELL"),
		Fingerprint: manualManageFingerprint("SIRENUSDT", "SELL", 165, 1.2437),
		Symbol:      "SIRENUSDT",
		Side:        "SELL",
		Qty:         165,
		Entry:       1.2437,
		Margin:      20.5,
		Leverage:    10,
		Status:      manualRequestPending,
	}
	m := &liveExecManager{
		manualConfirm:  true,
		manualRequests: map[string]manualManageRequest{req.Key: req},
		positions:      map[string]*livePosition{},
	}
	p, err := m.activatePassiveManualImport(req, now, "MANUAL_PENDING_IMPORT", true)
	if err != nil {
		t.Fatalf("unexpected passive import error: %v", err)
	}
	if !manualPassivePosition(p) {
		t.Fatalf("expected passive manual import, got %+v", p)
	}
	if _, ok := m.manualRequests[req.Key]; !ok {
		t.Fatalf("expected pending request to remain for approval")
	}
}

func TestActivateManualManagementPromotesExistingPassiveImport(t *testing.T) {
	now := time.Now().UTC()
	req := manualManageRequest{
		Key:         positionLookupKey("SIRENUSDT", "SELL"),
		Fingerprint: manualManageFingerprint("SIRENUSDT", "SELL", 165, 1.2437),
		Symbol:      "SIRENUSDT",
		Side:        "SELL",
		Qty:         165,
		Entry:       1.2437,
		Margin:      20.5,
		Leverage:    10,
		Status:      manualRequestApproved,
	}
	passive := &livePosition{
		Symbol:       "SIRENUSDT",
		Side:         "SELL",
		State:        execOpen,
		CreatedAt:    now.Add(-time.Minute),
		UpdatedAt:    now.Add(-time.Minute),
		EntryPrice:   1.2437,
		Qty:          165,
		FilledQty:    165,
		RemainingQty: 165,
		Margin:       20.5,
		Leverage:     10,
		EntryReason:  manualEntryReasonPassive,
		EntrySource:  manualEntrySourcePassive,
	}
	m := &liveExecManager{
		manualConfirm:  true,
		manualRequests: map[string]manualManageRequest{req.Key: req},
		positions:      map[string]*livePosition{"SIRENUSDT": passive},
		stopPct:        2,
		minStopPct:     1,
		maxStopPct:     5,
		tp1R:           1,
		tp2R:           2,
		tp3R:           3,
		tp1Frac:        0.33,
		tp2Frac:        0.33,
		tp3Frac:        0.34,
	}
	p, err := m.activateManualManagement(req, now, "MANUAL_APPROVED")
	if err != nil {
		t.Fatalf("expected manage adoption to proceed while protection is attaching, got err=%v", err)
	}
	if p != passive {
		t.Fatalf("expected in-place promotion of passive position")
	}
	if !manualManagedTrade(p) {
		t.Fatalf("expected manual-managed position, got %+v", p)
	}
	if _, ok := m.manualRequests[req.Key]; ok {
		t.Fatalf("expected manual request to be cleared after promotion")
	}
	if !p.ProtectionPending {
		t.Fatalf("expected promoted manual trade to enter protection-pending state")
	}
}

func TestActivateManualManagementPromotesPassiveImportWithRawSellSide(t *testing.T) {
	now := time.Now().UTC()
	req := manualManageRequest{
		Key:         positionLookupKey("SIRENUSDT", "SELL"),
		Fingerprint: manualManageFingerprint("SIRENUSDT", "SELL", 128, 0.80622),
		Symbol:      "SIRENUSDT",
		Side:        "SHORT",
		Qty:         128,
		Entry:       0.80622,
		Margin:      20.5,
		Leverage:    5,
		Status:      manualRequestApproved,
	}
	passive := &livePosition{
		Symbol:       "SIRENUSDT",
		Side:         "SHORT",
		State:        execOpen,
		CreatedAt:    now.Add(-time.Minute),
		UpdatedAt:    now.Add(-time.Minute),
		EntryPrice:   0.80622,
		Qty:          128,
		FilledQty:    128,
		RemainingQty: 128,
		Margin:       20.5,
		Leverage:     5,
		EntryReason:  manualEntryReasonPassive,
		EntrySource:  manualEntrySourcePassive,
	}
	m := &liveExecManager{
		manualConfirm:  true,
		manualRequests: map[string]manualManageRequest{req.Key: req},
		positions:      map[string]*livePosition{"SIRENUSDT": passive},
		stopPct:        2,
		minStopPct:     1,
		maxStopPct:     5,
		tp1R:           1,
		tp2R:           2,
		tp3R:           3,
		tp1Frac:        0.33,
		tp2Frac:        0.33,
		tp3Frac:        0.34,
	}
	if _, err := m.activateManualManagement(req, now, "MANUAL_APPROVED"); err != nil {
		t.Fatalf("expected approval to continue while protection attach is pending, got err=%v", err)
	}
}

func TestHandleCommandScannerSnapshot(t *testing.T) {
	ctx := &telegramCommandCtx{status: newLiveStatusStore()}
	ctx.status.Set(liveStatus{
		Generated:   time.Date(2026, 3, 27, 12, 34, 56, 0, time.UTC),
		ScannerBias: "short",
		ScannerLongs: []notify.ScanItem{
			{Symbol: "ETHUSDT", Grade: "B", Score: 88},
		},
		ScannerShorts: []notify.ScanItem{
			{Symbol: "SIRENUSDT", Grade: "A+", Score: 118},
			{Symbol: "BLUAIUSDT", Grade: "C", Score: 81},
		},
	})

	resp := ctx.handleCommand("", "/scanner")
	if !strings.Contains(resp, "SCANNER") || !strings.Contains(resp, "SIREN") || !strings.Contains(resp, "ETH") {
		t.Fatalf("expected combined scanner snapshot, got: %s", resp)
	}
}

func TestHandleCommandLongsSnapshot(t *testing.T) {
	ctx := &telegramCommandCtx{status: newLiveStatusStore()}
	ctx.status.Set(liveStatus{
		Generated: time.Date(2026, 3, 27, 12, 34, 56, 0, time.UTC),
		ScannerLongs: []notify.ScanItem{
			{Symbol: "ETHUSDT", Grade: "B", Score: 88},
		},
		ScannerShorts: []notify.ScanItem{
			{Symbol: "SIRENUSDT", Grade: "A+", Score: 118},
		},
	})

	resp := ctx.handleCommand("", "/longs")
	if !strings.Contains(resp, "LONG SCANS") || !strings.Contains(resp, "ETH") || strings.Contains(resp, "SIREN") {
		t.Fatalf("expected longs-only snapshot, got: %s", resp)
	}
}

func TestHandleCommandShortsSnapshot(t *testing.T) {
	ctx := &telegramCommandCtx{status: newLiveStatusStore()}
	ctx.status.Set(liveStatus{
		Generated: time.Date(2026, 3, 27, 12, 34, 56, 0, time.UTC),
		ScannerLongs: []notify.ScanItem{
			{Symbol: "ETHUSDT", Grade: "B", Score: 88},
		},
		ScannerShorts: []notify.ScanItem{
			{Symbol: "SIRENUSDT", Grade: "A+", Score: 118},
		},
	})

	resp := ctx.handleCommand("", "/shorts")
	if !strings.Contains(resp, "SHORT SCANS") || !strings.Contains(resp, "SIREN") || strings.Contains(resp, "ETH") {
		t.Fatalf("expected shorts-only snapshot, got: %s", resp)
	}
}

func TestDisplayEntrySourceAndReason(t *testing.T) {
	if got := displayEntrySource("MANUAL_PASSIVE"); got != "MANUAL" {
		t.Fatalf("expected MANUAL for passive source, got %q", got)
	}
	if got := displayEntrySource("MANUAL_MANAGED"); got != "MANUAL_MANAGED" {
		t.Fatalf("expected MANUAL_MANAGED for managed source, got %q", got)
	}
	if got := displayEntryReason("manual_managed_live"); got != "MANUAL_MANAGED" {
		t.Fatalf("expected MANUAL_MANAGED display reason, got %q", got)
	}
	if got := displayEntryReason("MANUAL_IMPORT"); got != "MANUAL_IMPORT" {
		t.Fatalf("expected MANUAL_IMPORT to stay visible, got %q", got)
	}
}

func TestPassiveManualPositionBySymbolFindsActivePassiveImport(t *testing.T) {
	now := time.Now().UTC()
	m := &liveExecManager{
		positions: map[string]*livePosition{
			"SIRENUSDT": {
				Symbol:       "SIRENUSDT",
				Side:         "SHORT",
				State:        execOpen,
				CreatedAt:    now,
				UpdatedAt:    now,
				EntryReason:  manualEntryReasonPassive,
				EntrySource:  manualEntrySourcePassive,
				RemainingQty: 10,
			},
		},
	}
	if _, ok := m.passiveManualPositionBySymbol("SIREN"); !ok {
		t.Fatalf("expected passive manual position lookup by raw symbol")
	}
}

func TestArmManualProtectionAfterReconstructExpeditesReadyManualTrade(t *testing.T) {
	now := time.Now().UTC()
	p := &livePosition{
		EntryReason:     manualEntryReasonManaged,
		EntrySource:     manualEntrySourceManaged,
		Side:            "SHORT",
		EntryPrice:      1.2437,
		RemainingQty:    165,
		LastMark:        1.0825,
		MaxFavorableR:   1.2,
		ProtectionStage: protectionStageArmed,
	}
	armManualProtectionAfterReconstruct(now, p)
	if !p.ProtectionPending {
		t.Fatalf("expected protection pending")
	}
	if p.ProtectionRetryAfter.Sub(now) > 6*time.Second {
		t.Fatalf("expected ready manual trade to retry quickly, got %s", p.ProtectionRetryAfter.Sub(now))
	}
}

func TestReconstructManualManagedStateEnablesTrailForLateStageWinner(t *testing.T) {
	m := &liveExecManager{
		trailAfterTP: 2,
		trailStopPct: 1.5,
		trailPctMin:  1.0,
	}
	now := time.Now().UTC()
	p := &livePosition{
		Symbol:        "SIRENUSDT",
		Side:          "SHORT",
		EntryReason:   manualEntryReasonManaged,
		EntrySource:   manualEntrySourceManaged,
		EntryPrice:    1.2437,
		RemainingQty:  165,
		FilledQty:     165,
		StopPrice:     1.2444,
		TP1Price:      1.20,
		TP2Price:      1.14,
		TP3Price:      1.08,
		MaxFavorableR: 1.8,
	}
	m.reconstructManualManagedState(now, p, 1.07)
	if !p.TrailOn {
		t.Fatalf("expected reconstructed late-stage winner to enable trail")
	}
	if p.TrailRef <= 0 || p.TrailStop <= 0 {
		t.Fatalf("expected trail state initialized, got ref=%.6f stop=%.6f", p.TrailRef, p.TrailStop)
	}
}

func TestSyncImportedRemotePositionDetectsManualAddMutation(t *testing.T) {
	now := time.Now().UTC()
	p := &livePosition{
		Symbol:       "RLSUSDT",
		Side:         "SHORT",
		EntryReason:  manualEntryReasonManaged,
		EntrySource:  manualEntrySourceManaged,
		Qty:          4183,
		FilledQty:    4183,
		RemainingQty: 4183,
		EntryPrice:   0.006280,
		Margin:       26.0,
		Leverage:     1,
	}
	sync := syncImportedRemotePosition(p, 8359, 0.006195, 52.0, 1, now)
	if !sync.QtyIncreased {
		t.Fatalf("expected qty increase to be detected: %+v", sync)
	}
	if !sync.EntryChanged {
		t.Fatalf("expected blended entry change to be detected: %+v", sync)
	}
	if p.RemainingQty != 8359 || p.EntryPrice != 0.006195 {
		t.Fatalf("expected live position updated from remote truth, got qty=%.0f entry=%.6f", p.RemainingQty, p.EntryPrice)
	}
}

func TestRecalibrateManagedManualPositionResetsAndRearmsProtectionOnManualAdd(t *testing.T) {
	now := time.Now().UTC()
	m := &liveExecManager{
		stopPct:    2,
		minStopPct: 0.25,
		maxStopPct: 8,
		tp1R:       1.2,
		tp2R:       2.5,
		tp3R:       4.0,
		tp1Frac:    0.00,
		tp2Frac:    0.10,
		tp3Frac:    0.10,
		beLockBps:  5,
		ladderCfg: ladderConfig{
			StarterUSDT:  25,
			MinAddPnLPct: 1.5,
		},
	}
	p := &livePosition{
		Symbol:               "RLSUSDT",
		Side:                 "SHORT",
		State:                execOpen,
		EntryReason:          manualEntryReasonManaged,
		EntrySource:          manualEntrySourceManaged,
		ManualManageState:    manualManageStateCritical,
		Managed:              true,
		Protected:            false,
		ProtectionPending:    true,
		ProtectionRetryAfter: now.Add(5 * time.Minute),
		ProtectionRetryCount: 3,
		ProtectionFailCount:  3,
		LastManageFailCause:  "exchange_immediate_trigger_retry_failed",
		LastManageFailAt:     now.Add(-2 * time.Minute),
		Qty:                  8359,
		FilledQty:            8359,
		RemainingQty:         8359,
		EntryPrice:           0.006195,
		LastMark:             0.006202,
		Margin:               52,
		Leverage:             1,
		HitTP1:               true,
		TrailOn:              true,
		TrailRef:             0.0058,
		TrailStop:            0.0060,
		MaxFavorableR:        2.1,
		ProtectionStage:      protectionStageArmed,
		ManageAnchorPrice:    0.006048,
	}
	sync := importedRemoteSync{QtyChanged: true, QtyIncreased: true, EntryChanged: true}

	m.recalibrateManagedManualPosition(p, now, sync)

	if p.ManualManageState != manualManageStatePendingProtection {
		t.Fatalf("expected recalibrated manual trade to return to pending protection, got %s", p.ManualManageState)
	}
	if !p.ProtectionPending {
		t.Fatalf("expected protection to be re-armed after recalibration")
	}
	if p.ProtectionRetryCount != 0 || p.ProtectionFailCount != 0 {
		t.Fatalf("expected stale protection counters cleared, got retries=%d fails=%d", p.ProtectionRetryCount, p.ProtectionFailCount)
	}
	if p.LastManageFailCause != "" {
		t.Fatalf("expected stale manage failure cause cleared, got %q", p.LastManageFailCause)
	}
	if p.HitTP1 || p.HitTP2 || p.HitTP3 {
		t.Fatalf("expected TP hit state reset after recalibration")
	}
	if p.TrailOn {
		t.Fatalf("expected trailing state reset after recalibration")
	}
	if p.ManageAnchorPrice != p.LastMark {
		t.Fatalf("expected manage anchor rebuilt from latest mark, got %.6f want %.6f", p.ManageAnchorPrice, p.LastMark)
	}
	if p.StopPrice <= 0 || p.TP2Price <= 0 || p.TP3Price <= 0 {
		t.Fatalf("expected bracket geometry to be rebuilt, got stop=%.6f tp2=%.6f tp3=%.6f", p.StopPrice, p.TP2Price, p.TP3Price)
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

func TestSafetyRejectBlocksContextOnlySymbolsFromTrading(t *testing.T) {
	t.Setenv("LIVE_CONTEXT_ONLY_ENFORCE", "1")
	now := time.Now().UTC()
	cfg := safetyConfig{
		contextOnlySymbols: map[string]struct{}{
			"BTCUSDT": {},
			"ETHUSDT": {},
			"SOLUSDT": {},
		},
	}
	c := candidate{
		Side: "BUY",
		Entry: inplay.Entry{
			Symbol: "BTCUSDT",
		},
	}
	if got := safetyReject(cfg, c, now, time.Time{}, map[string]time.Time{}, map[string]time.Time{}, map[string]int{}, map[string]int{}, map[string]time.Time{}); got != "context_only_symbol" {
		t.Fatalf("expected context_only_symbol reject, got %q", got)
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
		VolumeRatio: 1.25,
		OFIZ:        0.45,
		OFISamples:  10,
		ReclaimHold: true,
		Entry: inplay.Entry{
			EntryStyle:   "pullback_long",
			State:        inplay.StateHeating,
			CurrentScore: 92,
			ScoreSlope:   0.24,
			Rank:         2.0,
		},
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC))
	if got.Strat != "entry_now_long" {
		t.Fatalf("expected entry_now_long, got %q reject=%q", got.Strat, got.RejectReason)
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

func TestAccountHealthAllowsLiveBoot(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		report := accountReport{
			Generated: time.Now().UTC(),
			Health:    "healthy",
		}
		if !accountHealthAllowsLiveBoot(report) {
			t.Fatal("expected healthy report to allow live boot")
		}
	})

	t.Run("partial optional fields only", func(t *testing.T) {
		report := accountReport{
			Generated: time.Now().UTC(),
			Health:    "partial",
			Summary: AccountSummary{
				MissingFields: []string{"perp_realized_pnl", "spot_equity", "total_equity"},
			},
		}
		if !accountHealthAllowsLiveBoot(report) {
			t.Fatal("expected partial report with optional fields to allow live boot")
		}
	})

	t.Run("partial missing core fields", func(t *testing.T) {
		report := accountReport{
			Generated: time.Now().UTC(),
			Health:    "partial",
			Summary: AccountSummary{
				MissingFields: []string{"perp_available", "spot_equity"},
			},
		}
		if accountHealthAllowsLiveBoot(report) {
			t.Fatal("expected partial report missing core fields to block live boot")
		}
	})
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
	t.Setenv("LIVE_IMPULSIVE_LONG_MIN_DAYUTC_PCT", "0")
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
			ScoreSlope:   0.24,
			EntryStyle:   "pullback_long",
			Momentum:     true,
		},
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC))
	if got.Strat != "entry_now_long" {
		t.Fatalf("expected entry_now_long, got %q reject=%q", got.Strat, got.RejectReason)
	}
	if got.Conf <= 0 {
		t.Fatalf("expected starter confidence, got %.3f", got.Conf)
	}
}

func TestApplySimpleContinuationFallbackEliteReclaimStarterAllowsSoftOFIAndVWAP(t *testing.T) {
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "1")
	t.Setenv("LIVE_CONT_FAST_MIN_SCORE", "65")
	t.Setenv("LIVE_CONT_FAST_MIN_SLOPE", "0.02")
	t.Setenv("LIVE_CONT_FAST_MIN_VOL_RATIO", "1.20")
	t.Setenv("LIVE_CONT_FAST_MIN_OFI_Z", "0.35")
	t.Setenv("LIVE_STARTER_FINAL_RANK_MIN", "0.72")
	t.Setenv("LIVE_STARTER_MIN_VOL_RATIO", "0.80")
	t.Setenv("LIVE_STARTER_ALLOW_BELOW_VWAP_EMA_SOFT", "1")
	t.Setenv("LIVE_STARTER_ALLOW_STRUCTURE_SOFT", "1")
	t.Setenv("LIVE_ELITE_STARTER_MIN_SCORE", "95")
	t.Setenv("LIVE_ELITE_STARTER_MAX_RANK", "2")
	t.Setenv("LIVE_ELITE_STARTER_MIN_OFI_Z", "0.10")
	t.Setenv("LIVE_IMPULSIVE_LONG_MIN_DAYUTC_PCT", "0")
	c := candidate{
		Side:          "BUY",
		CombinedScore: 0.88,
		VolumeRatio:   0.92,
		OFIZ:          -0.06,
		OFISamples:    12,
		LastClose:     10.05,
		SessionVWAP:   10.20,
		EMA9:          10.15,
		ReclaimHold:   true,
		Entry: inplay.Entry{
			Symbol:               "RLSUSDT",
			CurrentGrade:         "A+",
			State:                inplay.StateInPlay,
			CurrentScore:         101,
			ScoreSlope:           0.18,
			Rank:                 1.0,
			FailedBreakdownCount: 1,
			EntryStyle:           "pullback_long",
			Momentum:             true,
		},
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 4, 4, 9, 0, 0, 0, time.UTC))
	if got.RejectReason != "no_simple_entry" {
		t.Fatalf("expected no_simple_entry, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestApplySimpleContinuationFallbackUsesImpulsiveShortStarterWhenImpulseStrong(t *testing.T) {
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "1")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_SHORT_STARTER", "1")
	t.Setenv("LIVE_CONT_FAST_MIN_SCORE", "65")
	t.Setenv("LIVE_CONT_FAST_MIN_SLOPE", "0.02")
	t.Setenv("LIVE_CONT_FAST_MIN_VOL_RATIO", "1.15")
	t.Setenv("LIVE_CONT_FAST_MIN_OFI_Z", "0.35")
	t.Setenv("LIVE_CONT_REQUIRE_STRUCTURE_CONFIRM", "1")
	t.Setenv("LIVE_EXHAUSTION_AVOID_CHASE_RISK", "10")
	t.Setenv("LIVE_ELITE_STARTER_HIGH_EXHAUST_DAYUTC_PCT", "40")
	t.Setenv("LIVE_ELITE_STARTER_MODERATE_EXHAUST_DAYUTC_PCT", "35")
	c := candidate{
		Side:        "SELL",
		VolumeRatio: 0.07,
		VolumeUSD:   13_700_000,
		OFIZ:        -0.02,
		OFISamples:  12,
		LastClose:   1.1196,
		SessionVWAP: 1.1500,
		EMA9:        1.1450,
		DayUTC24h:   -31.9,
		Entry: inplay.Entry{
			Symbol:         "SIRENUSDT",
			CurrentGrade:   "B",
			Rank:           1.2,
			State:          inplay.StateInPlay,
			TimeInStateMin: 12,
			CurrentScore:   113.5,
			ScoreSlope:     0.15,
			EntryStyle:     "pullback_short",
			Momentum:       true,
		},
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 3, 27, 3, 37, 0, 0, time.UTC))
	if got.RejectReason != "no_simple_entry" {
		t.Fatalf("expected no_simple_entry, got %q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestApplySimpleContinuationFallbackUsesImpulsiveShortStarterOnFailedBounce(t *testing.T) {
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "1")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_SHORT_STARTER", "1")
	t.Setenv("LIVE_CONT_FAST_MIN_SCORE", "72")
	t.Setenv("LIVE_CONT_FAST_MIN_SLOPE", "0.04")
	t.Setenv("LIVE_CONT_FAST_MIN_VOL_RATIO", "1.20")
	t.Setenv("LIVE_CONT_FAST_MIN_OFI_Z", "0.25")
	t.Setenv("LIVE_IMPULSIVE_SHORT_MAX_CONTRARY_OFI_Z", "0.10")
	t.Setenv("LIVE_CONT_REQUIRE_STRUCTURE_CONFIRM", "1")
	c := candidate{
		Side:        "SELL",
		VolumeRatio: 0.62,
		VolumeUSD:   9_500_000,
		OFIZ:        0.04,
		OFISamples:  12,
		LastClose:   1.021,
		SessionVWAP: 1.030,
		EMA9:        1.028,
		DayUTC24h:   -22.5,
		Entry: inplay.Entry{
			Symbol:            "STOUSDT",
			CurrentGrade:      "A",
			Rank:              1.5,
			State:             inplay.StateInPlay,
			TimeInStateMin:    10,
			CurrentScore:      98,
			ScoreSlope:        0.12,
			EntryStyle:        "pullback_short",
			Momentum:          true,
			FailedBounceCount: 1,
		},
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 4, 4, 9, 10, 0, 0, time.UTC))
	if got.RejectReason != "no_simple_entry" {
		t.Fatalf("expected no_simple_entry, got %q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestApplySimpleContinuationFallbackUsesImpulsiveLongStarterWhenImpulseStrong(t *testing.T) {
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "1")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_LONG_STARTER", "1")
	t.Setenv("LIVE_CONT_FAST_MIN_SCORE", "65")
	t.Setenv("LIVE_CONT_FAST_MIN_SLOPE", "0.02")
	t.Setenv("LIVE_CONT_FAST_MIN_VOL_RATIO", "1.15")
	t.Setenv("LIVE_CONT_FAST_MIN_OFI_Z", "0.35")
	t.Setenv("LIVE_CONT_REQUIRE_STRUCTURE_CONFIRM", "1")
	t.Setenv("LIVE_EXHAUSTION_AVOID_CHASE_RISK", "10")
	t.Setenv("LIVE_ELITE_STARTER_HIGH_EXHAUST_DAYUTC_PCT", "40")
	t.Setenv("LIVE_ELITE_STARTER_MODERATE_EXHAUST_DAYUTC_PCT", "35")
	c := candidate{
		Side:        "BUY",
		VolumeRatio: 0.09,
		VolumeUSD:   14_500_000,
		OFIZ:        0.03,
		OFISamples:  12,
		LastClose:   1.6441,
		SessionVWAP: 1.60,
		EMA9:        1.59,
		DayUTC24h:   31.9,
		Entry: inplay.Entry{
			Symbol:         "SIRENUSDT",
			CurrentGrade:   "B",
			Rank:           1.2,
			State:          inplay.StateInPlay,
			TimeInStateMin: 12,
			CurrentScore:   113.5,
			ScoreSlope:     0.15,
			EntryStyle:     "pullback_long",
			Momentum:       true,
		},
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 3, 27, 3, 37, 0, 0, time.UTC))
	if got.RejectReason != "no_simple_entry" {
		t.Fatalf("expected no_simple_entry, got %q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestClassifyImpulseQualityTreatsEliteReclaimAsBreakout(t *testing.T) {
	t.Setenv("LIVE_ELITE_STARTER_OFI_TOLERANCE_Z", "0.10")
	c := candidate{
		Side:        "BUY",
		VolumeRatio: 1.02,
		OFIZ:        -0.04,
		OFISamples:  10,
		LastClose:   10.1,
		SessionVWAP: 10.2,
		EMA9:        10.15,
		Entry: inplay.Entry{
			CurrentGrade:         "A+",
			CurrentScore:         99,
			ScoreSlope:           0.09,
			Rank:                 1.0,
			State:                inplay.StateInPlay,
			FailedBreakdownCount: 1,
			EntryStyle:           "pullback_long",
		},
		ReclaimHold: true,
	}
	if got := classifyImpulseQuality(c); got != "elite_breakout" {
		t.Fatalf("expected elite_breakout, got %q", got)
	}
}

func TestClassifyStarterLaneEliteLongReclaimPassesDirtyEarlySetup(t *testing.T) {
	t.Setenv("LIVE_ELITE_STARTER_MIN_SCORE", "92")
	t.Setenv("LIVE_ELITE_STARTER_MAX_RANK", "2")
	t.Setenv("LIVE_ELITE_STARTER_OFI_TOLERANCE_Z", "0.10")
	c := candidate{
		Side:          "BUY",
		CombinedScore: 0.88,
		VolumeRatio:   1.00,
		OFIZ:          -0.06,
		OFISamples:    12,
		LastClose:     10.05,
		SessionVWAP:   10.20,
		EMA9:          10.15,
		ReclaimHold:   true,
		Entry: inplay.Entry{
			CurrentGrade:         "A+",
			CurrentScore:         95,
			ScoreSlope:           0.08,
			Rank:                 1,
			State:                inplay.StateInPlay,
			FailedBreakdownCount: 1,
			EntryStyle:           "pullback_long",
		},
	}
	if got := classifyStarterLane(c, c.Side); got != "elite_starter" {
		t.Fatalf("expected elite_starter, got %q", got)
	}
	t.Setenv("LIVE_IMPULSIVE_LONG_MIN_DAYUTC_PCT", "0")
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 4, 4, 15, 0, 0, 0, time.UTC))
	if got.RejectReason != "no_simple_entry" {
		t.Fatalf("expected no_simple_entry, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestClassifyStarterLaneEliteShortFailedBouncePassesDirtySetup(t *testing.T) {
	t.Setenv("LIVE_ELITE_STARTER_MIN_SCORE", "92")
	t.Setenv("LIVE_ELITE_STARTER_MAX_RANK", "2")
	t.Setenv("LIVE_ELITE_STARTER_OFI_TOLERANCE_Z", "0.10")
	c := candidate{
		Side:          "SELL",
		CombinedScore: 0.90,
		VolumeRatio:   1.01,
		OFIZ:          0.05,
		OFISamples:    12,
		LastClose:     0.98,
		SessionVWAP:   0.95,
		EMA9:          0.955,
		Entry: inplay.Entry{
			CurrentGrade:      "A+",
			CurrentScore:      96,
			ScoreSlope:        0.09,
			Rank:              1,
			State:             inplay.StateInPlay,
			FailedBounceCount: 1,
			EntryStyle:        "pullback_short",
		},
	}
	if got := classifyStarterLane(c, c.Side); got != "elite_starter" {
		t.Fatalf("expected elite_starter, got %q", got)
	}
	t.Setenv("LIVE_IMPULSIVE_SHORT_MIN_DAYUTC_PCT", "0")
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 4, 4, 15, 0, 0, 0, time.UTC))
	if got.RejectReason != "no_simple_entry" {
		t.Fatalf("expected no_simple_entry, got %q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestClassifyStarterLaneRejectsMediocreDirtyContinuation(t *testing.T) {
	c := candidate{
		Side:          "BUY",
		CombinedScore: 0.70,
		VolumeRatio:   0.82,
		OFIZ:          -0.25,
		OFISamples:    12,
		LastClose:     9.9,
		SessionVWAP:   10.1,
		EMA9:          10.05,
		Entry: inplay.Entry{
			CurrentGrade: "B",
			CurrentScore: 74,
			ScoreSlope:   0.03,
			Rank:         4,
			State:        inplay.StateInPlay,
		},
	}
	if got := classifyStarterLane(c, c.Side); got != "reject" {
		t.Fatalf("expected reject, got %q", got)
	}
	got := applySimpleContinuationFallbackAt(c, time.Date(2026, 4, 4, 15, 0, 0, 0, time.UTC))
	if got.Strat != "none" {
		t.Fatalf("expected no strategy for mediocre dirty setup, got %q", got.Strat)
	}
}

func TestClassifyStarterLaneExtendedWaitReset(t *testing.T) {
	c := candidate{
		Side:          "BUY",
		CombinedScore: 0.86,
		VolumeRatio:   1.05,
		OFIZ:          0.04,
		OFISamples:    12,
		LastClose:     12.0,
		SessionVWAP:   11.0,
		EMA9:          11.1,
		ExtensionATR:  1.45,
		Entry: inplay.Entry{
			CurrentGrade: "A",
			CurrentScore: 94,
			ScoreSlope:   0.07,
			Rank:         1,
			State:        inplay.StateInPlay,
		},
	}
	if got := classifyStarterLane(c, c.Side); got != "extended_wait_reset" {
		t.Fatalf("expected extended_wait_reset, got %q", got)
	}
}

func TestClassifyStarterLaneHighExhaustionBlocksStarter(t *testing.T) {
	c := candidate{
		Side:          "BUY",
		CombinedScore: 0.90,
		VolumeRatio:   1.25,
		OFIZ:          0.20,
		OFISamples:    12,
		LastClose:     12.2,
		SessionVWAP:   11.0,
		EMA9:          11.1,
		ExtensionATR:  1.7,
		Entry: inplay.Entry{
			CurrentGrade: "A+",
			CurrentScore: 97,
			ScoreSlope:   0.06,
			Rank:         1,
			State:        inplay.StateInPlay,
		},
	}
	if got := classifyExhaustionRisk(c, c.Side); got != "high" {
		t.Fatalf("expected high exhaustion, got %q", got)
	}
	if got := classifyStarterLane(c, c.Side); got != "reject" {
		t.Fatalf("expected reject for high exhaustion, got %q", got)
	}
}

func TestLeverageRetrySequenceStepsDownForNotionalLimit(t *testing.T) {
	got := leverageRetrySequence(10, 3)
	want := []int{10, 5, 4, 3}
	if len(got) != len(want) {
		t.Fatalf("unexpected sequence len: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected sequence: got=%v want=%v", got, want)
		}
	}
}

func TestLeverageRetrySequenceClampsHighLeverageDown(t *testing.T) {
	got := leverageRetrySequence(20, 3)
	want := []int{20, 10, 5, 4, 3}
	if len(got) != len(want) {
		t.Fatalf("unexpected sequence len: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected sequence: got=%v want=%v", got, want)
		}
	}
}

func TestIsSymbolNotionalLimitError(t *testing.T) {
	err := fmt.Errorf("http 400 POST /fapi/v3/order: {\"code\":-5018,\"msg\":\"You’ve reached the maximum notional value limit for this symbol.\"}")
	if !isSymbolNotionalLimitError(err) {
		t.Fatal("expected -5018 error to be recognized")
	}
	if isSymbolNotionalLimitError(fmt.Errorf("some other error")) {
		t.Fatal("did not expect unrelated error to match")
	}
}

func TestIsIgnorableMarginTypeErrorRecognizes2014(t *testing.T) {
	err := fmt.Errorf("http 400 POST /fapi/v1/marginType: {\"code\":-2014,\"msg\":\"margin type unsupported in this auth mode\"}")
	if !isIgnorableMarginTypeError(err) {
		t.Fatal("expected -2014 margin type error to be ignored")
	}
}

func TestApplyLeverageWithFallbackStepsDownUntilAccepted(t *testing.T) {
	var attempts []int
	got, err := applyLeverageWithFallback(5, 2, func(lev int) error {
		attempts = append(attempts, lev)
		if lev > 4 {
			return fmt.Errorf("rejected")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 4 {
		t.Fatalf("expected accepted leverage 4, got %d", got)
	}
	want := []int{5, 4}
	if len(attempts) != len(want) {
		t.Fatalf("unexpected attempts: got=%v want=%v", attempts, want)
	}
	for i := range want {
		if attempts[i] != want[i] {
			t.Fatalf("unexpected attempts: got=%v want=%v", attempts, want)
		}
	}
}

func TestRRBelowMinimumTreatsEqualityAsAllowed(t *testing.T) {
	if rrBelowMinimum(1.0, 1.0) {
		t.Fatal("expected equal rr to pass minimum check")
	}
	if rrBelowMinimum(0.9999999, 1.0) {
		t.Fatal("expected tiny float noise to pass minimum check")
	}
	if !rrBelowMinimum(0.99, 1.0) {
		t.Fatal("expected meaningfully lower rr to fail minimum check")
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

func TestResolveLadderPlanAllowsBotAddAfterTPHitWhenFreshResetForms(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"SIRENUSDT": {
				Symbol:         "SIRENUSDT",
				Side:           "SELL",
				State:          execOpen,
				EntrySource:    "BOT",
				EntryReason:    "continuation_fast",
				EntryPrice:     1.2437,
				RemainingQty:   180,
				DeployedMargin: 20,
				HitTP1:         true,
			},
		},
		ladderCfg: loadLadderConfig(10),
	}
	meta := map[string]symbolMeta{
		"SIRENUSDT": {LastPrice: 1.0825},
	}
	c := candidate{
		Side:         "SELL",
		Strat:        "continuation_fast",
		LastClose:    1.0825,
		SessionVWAP:  1.18,
		EMA9:         1.12,
		DayUTC24h:    -43.0,
		RetestHold:   true,
		ResetRebreak: true,
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
		t.Fatalf("expected fresh-reset add plan after tp hit, got %+v", plan)
	}
	if plan.MarginUSDT != 10 {
		t.Fatalf("expected 10 usdt add, got %.2f", plan.MarginUSDT)
	}
}

func TestResolveLadderPlanAllowsAddForImpulsiveShortStarter(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"SIRENUSDT": {
				Symbol:         "SIRENUSDT",
				Side:           "SELL",
				State:          execOpen,
				EntrySource:    "BOT",
				EntryReason:    "impulsive_short_starter",
				EntryPrice:     1.20,
				RemainingQty:   100,
				DeployedMargin: 10,
			},
		},
		ladderCfg: loadLadderConfig(10),
	}
	meta := map[string]symbolMeta{
		"SIRENUSDT": {LastPrice: 1.08},
	}
	c := candidate{
		Side:        "SELL",
		Strat:       "impulsive_short_starter",
		Conf:        0.62,
		LastClose:   1.08,
		SessionVWAP: 1.15,
		EMA9:        1.12,
		RetestHold:  true,
		Entry:       inplay.Entry{Symbol: "SIRENUSDT", State: inplay.StateInPlay},
		Sig: strategies.Signal{
			Entry: 1.08,
			TP1:   1.02,
		},
	}
	plan := resolveLadderPlan(time.Date(2026, 3, 27, 3, 45, 0, 0, time.UTC), c, execMgr, meta)
	if plan.IsAdd || plan.RejectReason != "starter_lane_no_adds" {
		t.Fatalf("expected starter lane add block, got %+v", plan)
	}
}

func TestResolveLadderPlanAllowsAddForImpulsiveLongStarter(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"LYNUSDT": {
				Symbol:         "LYNUSDT",
				Side:           "BUY",
				State:          execOpen,
				EntrySource:    "BOT",
				EntryReason:    "impulsive_long_starter",
				EntryPrice:     1.20,
				RemainingQty:   100,
				DeployedMargin: 10,
			},
		},
		ladderCfg: loadLadderConfig(10),
	}
	meta := map[string]symbolMeta{
		"LYNUSDT": {LastPrice: 1.32},
	}
	c := candidate{
		Side:        "BUY",
		Strat:       "impulsive_long_starter",
		Conf:        0.62,
		LastClose:   1.32,
		SessionVWAP: 1.25,
		EMA9:        1.27,
		ReclaimHold: true,
		Entry:       inplay.Entry{Symbol: "LYNUSDT", State: inplay.StateInPlay},
		Sig: strategies.Signal{
			Entry: 1.32,
			TP1:   1.38,
		},
	}
	plan := resolveLadderPlan(time.Date(2026, 3, 27, 3, 45, 0, 0, time.UTC), c, execMgr, meta)
	if plan.IsAdd || plan.RejectReason != "starter_lane_no_adds" {
		t.Fatalf("expected starter lane add block, got %+v", plan)
	}
}

func TestManualWouldAddCapitalStillAllowsGreenImportedTradeAfterTP3(t *testing.T) {
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
		t.Fatalf("expected green imported trade to remain add-eligible despite tp3 history")
	}
}

func TestResolveLadderPlanBlocksImportedManagedAddWhenExtendedWithoutReset(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"SIRENUSDT": {
				Symbol:            "SIRENUSDT",
				Side:              "SELL",
				State:             execOpen,
				EntrySource:       manualEntrySourceManaged,
				EntryReason:       manualEntryReasonManaged,
				ManageAnchorPrice: 1.0825,
				EntryPrice:        1.2437,
				LastMark:          1.0410,
				RemainingQty:      180,
				DeployedMargin:    20,
			},
		},
		ladderCfg: loadLadderConfig(10),
	}
	meta := map[string]symbolMeta{
		"SIRENUSDT": {LastPrice: 1.0410},
	}
	c := candidate{
		Side:            "SELL",
		Strat:           "continuation_fast",
		LastClose:       1.0410,
		SessionVWAP:     1.11,
		EMA9:            1.09,
		DayUTC24h:       -43.0,
		ClosedBreakHold: true,
		ExtensionATR:    1.35,
		Entry: inplay.Entry{
			Symbol:       "SIRENUSDT",
			State:        inplay.StateInPlay,
			EntryStyle:   "pullback_short",
			CurrentScore: 98,
			ScoreSlope:   0.22,
			Momentum:     true,
		},
		Sig: strategies.Signal{
			Entry: 1.0410,
			Stop:  1.0825,
			TP1:   1.0200,
			TP2:   0.9950,
			TP3:   0.9700,
		},
	}
	plan := resolveLadderPlan(time.Date(2026, 3, 26, 16, 22, 0, 0, time.UTC), c, execMgr, meta)
	if plan.IsAdd || plan.RejectReason != "imported_add_extended_wait_reset" {
		t.Fatalf("expected imported extended add rejection, got %+v", plan)
	}
}

func TestResolveLadderPlanAllowsImportedManagedAddAfterFreshReset(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"SIRENUSDT": {
				Symbol:            "SIRENUSDT",
				Side:              "SELL",
				State:             execOpen,
				EntrySource:       manualEntrySourceManaged,
				EntryReason:       manualEntryReasonManaged,
				ManageAnchorPrice: 1.0825,
				EntryPrice:        1.2437,
				LastMark:          1.0640,
				RemainingQty:      180,
				DeployedMargin:    20,
			},
		},
		ladderCfg: loadLadderConfig(10),
	}
	meta := map[string]symbolMeta{
		"SIRENUSDT": {LastPrice: 1.0640},
	}
	c := candidate{
		Side:         "SELL",
		Strat:        "continuation_fast",
		LastClose:    1.0640,
		SessionVWAP:  1.10,
		EMA9:         1.08,
		DayUTC24h:    -43.0,
		RetestHold:   true,
		ResetRebreak: true,
		ExtensionATR: 1.35,
		Entry: inplay.Entry{
			Symbol:       "SIRENUSDT",
			State:        inplay.StateInPlay,
			EntryStyle:   "pullback_short",
			CurrentScore: 98,
			ScoreSlope:   0.22,
			Momentum:     true,
		},
		Sig: strategies.Signal{
			Entry: 1.0640,
			Stop:  1.0980,
			TP1:   1.0300,
			TP2:   0.9900,
			TP3:   0.9500,
		},
	}
	plan := resolveLadderPlan(time.Date(2026, 3, 26, 16, 22, 0, 0, time.UTC), c, execMgr, meta)
	if !plan.IsAdd {
		t.Fatalf("expected imported add after fresh reset, got %+v", plan)
	}
}

func TestResolveLadderPlanBlocksNewEntriesWhenManagedTradeIsUnprotected(t *testing.T) {
	t.Setenv("LIVE_DEGRADED_BLOCK_NEW_ENTRIES", "1")
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"SIRENUSDT": {
				Symbol:            "SIRENUSDT",
				Side:              "SELL",
				State:             execOpen,
				EntrySource:       manualEntrySourceManaged,
				EntryReason:       manualEntryReasonManaged,
				RemainingQty:      100,
				ManualManageState: manualManageStateDegraded,
				ProtectionPending: true,
				Protected:         false,
				StopOrderID:       0,
				ManageAnchorPrice: 1.05,
				DeployedMargin:    20,
			},
		},
		ladderCfg: loadLadderConfig(10),
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
	plan := resolveLadderPlan(time.Date(2026, 4, 4, 16, 0, 0, 0, time.UTC), c, execMgr, nil)
	if plan.RejectReason != "managed_position_unprotected" {
		t.Fatalf("expected managed_position_unprotected, got %+v", plan)
	}
}

func TestDegradedEntryReasonDoesNotUsePartialAccountHealthReason(t *testing.T) {
	now := time.Date(2026, 4, 4, 16, 0, 0, 0, time.UTC)
	mgr := &liveExecManager{
		accountReport: accountReport{
			Generated: now,
			Health:    "partial",
		},
		accountReportCfg:  accountReportConfig{RefreshEvery: time.Hour},
		userDataState:     aster.NewUserDataState(),
		lastReconcileOKAt: now,
	}
	mgr.userDataState.ApplyAccountUpdateTestOnly(asterUserDataUpdateForTest())
	if got := mgr.degradedEntryReason(now, "BTCUSDT"); got == degradedAccountHealthPartialReason {
		t.Fatalf("expected partial health to no longer map to %s, got %q", degradedAccountHealthPartialReason, got)
	}
}

func TestDegradedEntryReasonBlocksStaleUserData(t *testing.T) {
	t.Setenv("LIVE_USERDATA_STREAM_ENABLE", "1")
	now := time.Date(2026, 4, 4, 16, 0, 0, 0, time.UTC)
	mgr := &liveExecManager{
		accountReport: accountReport{
			Generated: now,
			Health:    "healthy",
		},
		accountReportCfg:  accountReportConfig{RefreshEvery: time.Hour},
		lastReconcileOKAt: now,
	}
	if got := mgr.degradedEntryReason(now, "BTCUSDT"); got != degradedUserDataStaleReason {
		t.Fatalf("expected %s, got %q", degradedUserDataStaleReason, got)
	}
}

func TestDegradedModeBlocksFreshEntriesButAllowsExistingRiskManagement(t *testing.T) {
	now := time.Date(2026, 4, 4, 16, 0, 0, 0, time.UTC)
	execMgr := &liveExecManager{
		accountReport: accountReport{
			Generated: now,
			Health:    "partial",
		},
		accountReportCfg: accountReportConfig{RefreshEvery: time.Hour},
	}
	c := candidate{
		Side:  "BUY",
		Entry: inplay.Entry{Symbol: "BTCUSDT"},
	}
	reason := quickCandidateSelectionReject(
		c,
		now,
		false,
		false,
		0,
		now.In(time.Local),
		maintenanceWindow{},
		0,
		nil,
		execMgr,
		safetyConfig{},
		time.Time{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if reason != degradedUserDataStaleReason {
		t.Fatalf("expected fresh-entry block %s, got %q", degradedUserDataStaleReason, reason)
	}

	p := &livePosition{
		Symbol:            "BTCUSDT",
		Side:              "BUY",
		State:             execOpen,
		EntrySource:       manualEntrySourceManaged,
		EntryReason:       manualEntryReasonManaged,
		RemainingQty:      1,
		EntryPrice:        100,
		LastMark:          99.5,
		ManualManageState: manualManageStatePendingProtection,
		Managed:           true,
	}
	execMgr.handleManualProtectionFailure(p, "exchange_immediate_trigger_retry_failed", now)
	if p.State != execOpen {
		t.Fatalf("expected existing position to remain open for protection retries, got state=%s", p.State)
	}
	if !p.ProtectionPending || p.ProtectionRetryAfter.IsZero() {
		t.Fatalf("expected protection maintenance retry scheduling, pending=%v retry_after=%v", p.ProtectionPending, p.ProtectionRetryAfter)
	}
}

func TestRecordOrderLegalityFailureQuarantinesSymbol(t *testing.T) {
	t.Setenv("LIVE_ORDER_LEGALITY_FAIL_LIMIT", "3")
	t.Setenv("LIVE_ORDER_LEGALITY_QUARANTINE_SEC", "1800")
	now := time.Date(2026, 4, 4, 16, 0, 0, 0, time.UTC)
	mgr := &liveExecManager{
		legalityFailCount:    map[string]int{},
		symbolQuarantineTill: map[string]time.Time{},
	}
	mgr.recordOrderLegalityFailure("BTCUSDT", orderIllegalMaxQtyReason, now)
	mgr.recordOrderLegalityFailure("BTCUSDT", orderIllegalMaxQtyReason, now)
	if mgr.symbolQuarantined("BTCUSDT", now.Add(time.Minute)) {
		t.Fatal("expected no quarantine before fail limit")
	}
	mgr.recordOrderLegalityFailure("BTCUSDT", orderIllegalMaxQtyReason, now)
	if !mgr.symbolQuarantined("BTCUSDT", now.Add(time.Minute)) {
		t.Fatal("expected symbol quarantine after repeated legality failures")
	}
}

func TestRecordOrderLegalityFailureLogsStructuredQuarantineVisibility(t *testing.T) {
	t.Setenv("LIVE_ORDER_LEGALITY_FAIL_LIMIT", "3")
	t.Setenv("LIVE_ORDER_LEGALITY_QUARANTINE_SEC", "1800")
	now := time.Date(2026, 4, 4, 16, 0, 0, 0, time.UTC)
	mgr := &liveExecManager{
		legalityFailCount:    map[string]int{},
		symbolQuarantineTill: map[string]time.Time{},
	}

	logLine := captureStdout(t, func() {
		mgr.recordOrderLegalityFailure("BTCUSDT", orderIllegalMaxQtyReason, now)
		mgr.recordOrderLegalityFailure("BTCUSDT", orderIllegalMaxQtyReason, now)
		mgr.recordOrderLegalityFailure("BTCUSDT", orderIllegalMaxQtyReason, now)
	})
	if !strings.Contains(logLine, "LEGALITY_QUARANTINE") {
		t.Fatalf("expected structured legality log marker, got %q", logLine)
	}
	if !strings.Contains(logLine, "symbol=BTCUSDT") ||
		!strings.Contains(logLine, "reason="+orderIllegalMaxQtyReason) ||
		!strings.Contains(logLine, "failure_count=3") ||
		!strings.Contains(logLine, "quarantine_until=") ||
		!strings.Contains(logLine, "fresh_entries_blocked=true") {
		t.Fatalf("expected structured legality visibility fields, got %q", logLine)
	}
}

func TestRecordOrderLegalityFailureWarnsBeforeQuarantine(t *testing.T) {
	t.Setenv("LIVE_ORDER_LEGALITY_FAIL_LIMIT", "3")
	t.Setenv("LIVE_ORDER_LEGALITY_QUARANTINE_SEC", "1800")
	now := time.Date(2026, 4, 4, 16, 0, 0, 0, time.UTC)
	mgr := &liveExecManager{
		legalityFailCount:    map[string]int{},
		symbolQuarantineTill: map[string]time.Time{},
	}

	logLine := captureStdout(t, func() {
		mgr.recordOrderLegalityFailure("BTCUSDT", orderIllegalMaxQtyReason, now)
	})
	if !strings.Contains(logLine, "LEGALITY_WARNING") {
		t.Fatalf("expected legality warning before quarantine, got %q", logLine)
	}
	if !strings.Contains(logLine, "fresh_entries_blocked=false") {
		t.Fatalf("expected fresh entries to stay unblocked before threshold, got %q", logLine)
	}
}

func TestValidateOrderLegalityRejectsQtyAboveMaxWhenClampInvalid(t *testing.T) {
	meta := aster.SymbolMeta{
		StepSize:     1,
		MinQty:       1,
		MaxQty:       0.5,
		MinNotional:  1,
		QtyPrecision: 0,
	}
	_, _, reason := validateOrderLegality(meta, 2, 10)
	if reason != orderIllegalMaxQtyReason {
		t.Fatalf("expected %s, got %q", orderIllegalMaxQtyReason, reason)
	}
}

func TestValidateOrderLegalityRejectsMinNotional(t *testing.T) {
	meta := aster.SymbolMeta{
		StepSize:     0.1,
		MinQty:       0.1,
		MaxQty:       10,
		MinNotional:  50,
		QtyPrecision: 2,
	}
	_, _, reason := validateOrderLegality(meta, 1, 10)
	if reason != orderIllegalMinNotionalReason {
		t.Fatalf("expected %s, got %q", orderIllegalMinNotionalReason, reason)
	}
}

func TestValidateOrderLegalityProtectionPathRejectsIllegalTickSize(t *testing.T) {
	meta := aster.SymbolMeta{
		TickSize:       0.05,
		StepSize:       0.001,
		MinQty:         0.001,
		MaxQty:         100,
		MinNotional:    1,
		PricePrecision: 2,
		QtyPrecision:   3,
	}
	_, _, reason := validateOrderLegality(meta, 1, 100.03)
	if reason != orderIllegalTickSizeReason {
		t.Fatalf("expected %s, got %q", orderIllegalTickSizeReason, reason)
	}
}

func TestInitializeBracketLevelsUsesManageAnchorForImportedManagedTrade(t *testing.T) {
	m := &liveExecManager{
		stopPct:    3.0,
		minStopPct: 1.0,
		maxStopPct: 10.0,
		tp1R:       1.0,
		tp2R:       2.0,
		tp3R:       3.0,
	}
	p := &livePosition{
		Symbol:            "SIRENUSDT",
		Side:              "SELL",
		EntrySource:       manualEntrySourceManaged,
		EntryReason:       manualEntryReasonManaged,
		EntryPrice:        1.2437,
		ManageAnchorPrice: 1.0825,
		FilledQty:         100,
	}
	if err := m.initializeBracketLevels(p); err != nil {
		t.Fatalf("unexpected bracket init error: %v", err)
	}
	if p.StopPrice <= 1.0825 {
		t.Fatalf("expected imported stop to rebuild off manage anchor, got %.4f", p.StopPrice)
	}
	if p.TP1Price >= 1.0825 {
		t.Fatalf("expected imported tp1 to rebuild below manage anchor for short, got %.4f", p.TP1Price)
	}
}

func TestUpdateManagePhasePromotesBotTradeToContinuation(t *testing.T) {
	p := &livePosition{
		EntrySource:   "BOT",
		EntryReason:   "continuation_fast",
		MaxFavorableR: 1.4,
	}
	updateManagePhase(p, false)
	if p.ManagePhase != managePhaseContinuation {
		t.Fatalf("expected continuation phase, got %s", p.ManagePhase)
	}
}

func TestRefreshRunnerReservationKeepsRunnerAfterSizeBuild(t *testing.T) {
	p := &livePosition{
		EntrySource:    "BOT",
		EntryReason:    "continuation_fast",
		ManagePhase:    managePhaseContinuation,
		RemainingQty:   100,
		DeployedMargin: 100,
	}
	refreshRunnerReservation(p, 25)
	if p.RunnerMinQty <= 0 {
		t.Fatalf("expected non-zero runner reservation, got %.4f", p.RunnerMinQty)
	}
}

func TestMarkLivePositionClosedFlagsRunnerCaptureFailure(t *testing.T) {
	p := &livePosition{
		DeployedMargin: 100,
		RealizedPnL:    0.2,
		MaxFavorableR:  3.0,
		CaptureRatio:   0.05,
	}
	markLivePositionClosed(p, time.Now().UTC(), "STOP_HIT")
	if !p.RunnerCaptureFailed {
		t.Fatalf("expected runner capture failure flag")
	}
}

func TestResolveLadderPlanBlocksReentryAfterRunnerCaptureFailureWithoutReset(t *testing.T) {
	execMgr := &liveExecManager{
		reentryCfg: reentryConfig{
			Enable:       true,
			SizeUSDT:     25,
			MaxPerSymbol: 1,
			Cooldown:     5 * time.Minute,
		},
		positions: map[string]*livePosition{
			"STOUSDT": {
				Symbol:              "STOUSDT",
				Side:                "SELL",
				State:               execClosed,
				ClosedAt:            time.Date(2026, 4, 3, 16, 0, 0, 0, time.UTC),
				RunnerCaptureFailed: true,
			},
		},
	}
	c := candidate{
		Side:        "SELL",
		Strat:       "continuation_fast",
		LastClose:   0.47,
		SessionVWAP: 0.49,
		EMA9:        0.48,
		Entry: inplay.Entry{
			Symbol:       "STOUSDT",
			State:        inplay.StateInPlay,
			EntryStyle:   "pullback_short",
			CurrentScore: 98,
			ScoreSlope:   0.22,
			Momentum:     true,
		},
	}
	plan := resolveLadderPlan(time.Date(2026, 4, 3, 16, 10, 0, 0, time.UTC), c, execMgr, nil)
	if plan.RejectReason != "reentry_runner_capture_failed" {
		t.Fatalf("expected reentry runner capture failure block, got %+v", plan)
	}
}

func TestResolveLadderPlanBlocksReentryWhenDisabled(t *testing.T) {
	now := time.Date(2026, 4, 3, 16, 10, 0, 0, time.UTC)
	execMgr := &liveExecManager{
		reentryCfg: reentryConfig{Enable: false},
		positions: map[string]*livePosition{
			"STOUSDT": {
				Symbol:   "STOUSDT",
				Side:     "SELL",
				State:    execClosed,
				ClosedAt: now.Add(-10 * time.Minute),
			},
		},
	}
	c := candidate{
		Side:      "SELL",
		Strat:     "continuation_fast",
		LastClose: 0.47,
		Entry: inplay.Entry{
			Symbol:     "STOUSDT",
			State:      inplay.StateInPlay,
			EntryStyle: "pullback_short",
		},
	}
	plan := resolveLadderPlan(now, c, execMgr, nil)
	if plan.IsReentry || plan.RejectReason != "reentry_disabled" {
		t.Fatalf("expected reentry_disabled block, got %+v", plan)
	}
}

func TestResolveLadderPlanBlocksReentryWhenDisabledEvenWithReset(t *testing.T) {
	now := time.Date(2026, 4, 3, 16, 10, 0, 0, time.UTC)
	execMgr := &liveExecManager{
		reentryCfg: reentryConfig{Enable: false},
		positions: map[string]*livePosition{
			"STOUSDT": {
				Symbol:   "STOUSDT",
				Side:     "SELL",
				State:    execClosed,
				ClosedAt: now.Add(-10 * time.Minute),
			},
		},
	}
	c := candidate{
		Side:       "SELL",
		Strat:      "continuation_fast",
		RetestHold: true,
		LastClose:  0.47,
		Entry: inplay.Entry{
			Symbol:     "STOUSDT",
			State:      inplay.StateInPlay,
			EntryStyle: "pullback_short",
		},
	}
	plan := resolveLadderPlan(now, c, execMgr, nil)
	if plan.IsReentry || plan.RejectReason != "reentry_disabled" {
		t.Fatalf("expected strict reentry_disabled block even with reset, got %+v", plan)
	}
}

func TestManualProtectionConvictionReadyImmediatelyForImportedManagedTrade(t *testing.T) {
	p := &livePosition{
		Symbol:            "SIRENUSDT",
		Side:              "SELL",
		EntrySource:       "MANUAL",
		EntryReason:       "manual_managed_live",
		EntryPrice:        1.2437,
		ManageAnchorPrice: 1.2415,
		LastMark:          1.2415,
	}
	if !manualProtectionConvictionReady(p) {
		t.Fatalf("expected imported/manual-managed trades to require immediate baseline protection")
	}
}

func TestMarkProtectionPendingBacksOffSameCause(t *testing.T) {
	p := &livePosition{}
	now := time.Date(2026, 3, 26, 22, 0, 0, 0, time.UTC)
	markProtectionPending(p, now, "exchange_immediate_trigger_retry_failed")
	first := p.ProtectionRetryAfter
	markProtectionPending(p, now.Add(30*time.Second), "exchange_immediate_trigger_retry_failed")
	if !p.ProtectionRetryAfter.After(first) {
		t.Fatalf("expected retry backoff to extend on same cause")
	}
}

func TestProtectiveStopExchangeSafeRequiresBuffer(t *testing.T) {
	if protectiveStopExchangeSafe("SELL", 1.00, 0.99, 0.991, 0.0001) {
		t.Fatalf("expected short stop too close to mark to be rejected")
	}
	if !protectiveStopExchangeSafe("SELL", 1.00, 0.99, 1.01, 0.0001) {
		t.Fatalf("expected wider short stop to be exchange-safe")
	}
	if protectiveStopExchangeSafe("BUY", 1.00, 1.01, 1.009, 0.0001) {
		t.Fatalf("expected long stop too close to mark to be rejected")
	}
	if !protectiveStopExchangeSafe("BUY", 1.00, 1.01, 0.99, 0.0001) {
		t.Fatalf("expected wider long stop to be exchange-safe")
	}
}

func TestHasLiveProtectiveOrderRequiresRealOrderAndNoPendingProtection(t *testing.T) {
	if hasLiveProtectiveOrder(&livePosition{}) {
		t.Fatal("expected no live protection without stop order")
	}
	if hasLiveProtectiveOrder(&livePosition{StopOrderID: 123, ProtectionPending: true}) {
		t.Fatal("expected pending protection to not count as live")
	}
	if !hasLiveProtectiveOrder(&livePosition{StopOrderID: 123}) {
		t.Fatal("expected active stop order to count as live protection")
	}
}

func TestManualStopRetryCandidatesEscalateBeyondLegacyImmediateTriggerWidths(t *testing.T) {
	candidates := manualStopRetryCandidates("SELL", 1.00, 0.98, 0.0001)
	if len(candidates) != 4 {
		t.Fatalf("expected bounded 3-step widening ladder (+base), got %d candidates: %#v", len(candidates), candidates)
	}
	if candidates[len(candidates)-1] <= 1.006 {
		t.Fatalf("expected final retry candidate to widen materially, got %#v", candidates)
	}
}

func TestNormalizeManualProtectiveStopWidensToExchangeSafeCandidate(t *testing.T) {
	stop, mark, err := normalizeManualProtectiveStop("SIRENUSDT", "SELL", nil, 1.00, 0.99, 0.991, 0.0001)
	if err != nil {
		t.Fatalf("expected normalized manual stop, got err=%v", err)
	}
	if mark != 0.99 {
		t.Fatalf("expected mark to be preserved, got %v", mark)
	}
	if !protectiveStopExchangeSafe("SELL", 1.00, 0.99, stop, 0.0001) {
		t.Fatalf("expected normalized stop to be exchange-safe, got %v", stop)
	}
	if stop <= 0.991 {
		t.Fatalf("expected normalized stop to widen beyond original, got %v", stop)
	}
}

func TestBenignZeroRoundedQtyErrMatchesExchangeZeroQtyMessage(t *testing.T) {
	if !benignZeroRoundedQtyErr(fmt.Errorf("qty must be > 0")) {
		t.Fatal("expected exchange zero-qty message to be treated as benign for partial TP slices")
	}
	if benignZeroRoundedQtyErr(fmt.Errorf("some other error")) {
		t.Fatal("did not expect unrelated error to be treated as benign")
	}
}

func TestProtectiveStopHelpersSupportLongShortAliases(t *testing.T) {
	if !protectiveStopValid("LONG", 1.00, 1.01, 0.99) {
		t.Fatal("expected LONG alias to use long-side protective stop geometry")
	}
	if !protectiveStopExchangeSafe("LONG", 1.00, 1.01, 0.99, 0.0001) {
		t.Fatal("expected LONG alias to accept long-side exchange-safe stop")
	}
	if got := widenedProtectiveStop("LONG", 1.00, 1.01, 0.0001); got >= 1.01 {
		t.Fatalf("expected widened LONG stop below market, got %.6f", got)
	}
	if got := widenedImmediateTriggerStopPct("LONG", 1.00, 1.01, 0.0001, 0.005); got >= 1.01 {
		t.Fatalf("expected widened immediate-trigger LONG stop below market, got %.6f", got)
	}
	if !protectiveStopValid("SHORT", 1.00, 0.99, 1.01) {
		t.Fatal("expected SHORT alias to use short-side protective stop geometry")
	}
	if !protectiveStopExchangeSafe("SHORT", 1.00, 0.99, 1.01, 0.0001) {
		t.Fatal("expected SHORT alias to accept short-side exchange-safe stop")
	}
	if got := widenedProtectiveStop("SHORT", 1.00, 0.99, 0.0001); got <= 0.99 {
		t.Fatalf("expected widened SHORT stop above market, got %.6f", got)
	}
}

func TestChooseProtectiveReferenceUsesAskForShortAndBidForLong(t *testing.T) {
	if got := chooseProtectiveReference("SHORT", 0.0680, 0.0715); got != 0.0715 {
		t.Fatalf("expected short protective reference to use ask, got %.6f", got)
	}
	if got := chooseProtectiveReference("LONG", 0.0680, 0.0715); got != 0.0680 {
		t.Fatalf("expected long protective reference to use bid, got %.6f", got)
	}
}

func TestChooseManagedProtectiveStopKeepsExistingLegalProtectedStop(t *testing.T) {
	got := chooseManagedProtectiveStop("SHORT", 0.06417, 0.06802, 0.064202, 0.06900)
	if math.Abs(got-0.06900) > 1e-9 {
		t.Fatalf("expected legal existing protected stop to be preserved, got %.6f", got)
	}
}

func TestShouldEmergencyForceCloseManagedPositionRequiresRealLoss(t *testing.T) {
	t.Setenv("LIVE_IMPORT_FORCE_CLOSE_ON_PROTECT_FAIL", "1")
	t.Setenv("LIVE_IMPORT_FORCE_CLOSE_MAX_LOSS_PCT", "2.5")
	m := &liveExecManager{}
	p := &livePosition{
		Symbol:            "SIRENUSDT",
		Side:              "SELL",
		State:             execOpen,
		EntrySource:       manualEntrySourceManaged,
		EntryReason:       manualEntryReasonManaged,
		EntryPrice:        0.5900,
		LastMark:          0.59157,
		RemainingQty:      212,
		ManualManageState: manualManageStateCritical,
		Managed:           true,
	}
	if m.shouldEmergencyForceCloseManagedPosition(p, "exchange_immediate_trigger_retry_failed") {
		t.Fatal("expected healthy managed trade to stay open while protection retries continue")
	}
	p.LastMark = 0.6060
	if !m.shouldEmergencyForceCloseManagedPosition(p, "exchange_immediate_trigger_retry_failed") {
		t.Fatal("expected emergency force close once managed loss exceeds threshold")
	}
}

func TestHandleManualProtectionFailureKeepsManagedTradeAliveWhenLossIsSmall(t *testing.T) {
	t.Setenv("LIVE_IMPORT_FORCE_CLOSE_ON_PROTECT_FAIL", "1")
	t.Setenv("LIVE_IMPORT_FORCE_CLOSE_MAX_LOSS_PCT", "2.5")
	p := &livePosition{
		Symbol:            "SIRENUSDT",
		Side:              "SELL",
		State:             execOpen,
		EntrySource:       manualEntrySourceManaged,
		EntryReason:       manualEntryReasonManaged,
		EntryPrice:        0.5900,
		LastMark:          0.59157,
		RemainingQty:      212,
		ManualManageState: manualManageStatePendingProtection,
		Managed:           true,
	}
	m := &liveExecManager{}
	now := time.Date(2026, 4, 4, 22, 20, 0, 0, time.UTC)
	m.handleManualProtectionFailure(p, "exchange_immediate_trigger_retry_failed", now)
	if p.State == execClosed {
		t.Fatal("expected managed trade to remain open while retries continue")
	}
	if p.ManualManageState != manualManageStatePendingProtection {
		t.Fatalf("expected pending protection state during bounded retries, got %s", p.ManualManageState)
	}
	if !p.ProtectionPending {
		t.Fatal("expected protection to remain pending")
	}
	if !p.ProtectionRetryAfter.After(now) {
		t.Fatal("expected retry timer to be scheduled")
	}
}

func TestInitializeBracketLevelsAllowsZeroTP1Fraction(t *testing.T) {
	t.Setenv("LIVE_TP1_FRAC", "0.00")
	m := &liveExecManager{
		stopPct:    2,
		minStopPct: 1,
		maxStopPct: 5,
		tp1R:       1,
		tp2R:       2,
		tp3R:       3,
		tp1Frac:    0.0,
		tp2Frac:    0.10,
		tp3Frac:    0.10,
	}
	p := &livePosition{
		Symbol:      "RLSUSDT",
		Side:        "SELL",
		EntryPrice:  0.006280,
		FilledQty:   4183,
		EntryReason: manualEntryReasonManaged,
		EntrySource: manualEntrySourceManaged,
	}
	if err := m.initializeBracketLevels(p); err != nil {
		t.Fatalf("expected zero TP1 fraction to be allowed, got %v", err)
	}
	if p.TP1Qty != 0 {
		t.Fatalf("expected TP1 qty to stay zero, got %.6f", p.TP1Qty)
	}
}

func TestManualProtectionStatusShowsProtectingWhileCriticalRetryPending(t *testing.T) {
	p := &livePosition{
		EntrySource:       manualEntrySourceManaged,
		EntryReason:       manualEntryReasonManaged,
		ManualManageState: manualManageStateCritical,
		ProtectionPending: true,
		RemainingQty:      100,
	}
	if got := manualProtectionStatus(p); got != "PROTECTING" {
		t.Fatalf("expected PROTECTING, got %q", got)
	}
}

func TestCriticalProtectionStateHelpers(t *testing.T) {
	m := &liveExecManager{
		positions: map[string]*livePosition{
			"SIRENUSDT": {
				Symbol:               "SIRENUSDT",
				Side:                 "SELL",
				State:                execOpen,
				RemainingQty:         120,
				EntrySource:          manualEntrySourceManaged,
				EntryReason:          manualEntryReasonManaged,
				ManualManageState:    manualManageStateCritical,
				ProtectionPending:    true,
				ProtectionRetryCount: 2,
				LastManageFailCause:  "invalid_after_retry",
				Managed:              true,
				Protected:            false,
			},
		},
	}
	if !m.hasCriticalProtectionState() {
		t.Fatal("expected critical protection helper to detect managed critical state")
	}
	rows := m.criticalProtectionSummaryLines(2)
	if len(rows) == 0 {
		t.Fatal("expected critical protection summary lines")
	}
	if !strings.Contains(rows[0], "SIREN") || !strings.Contains(rows[0], "status=PROTECTING") {
		t.Fatalf("unexpected critical summary row: %q", rows[0])
	}
}

func TestHelpCommandGroupedByCategory(t *testing.T) {
	resp := (&telegramCommandCtx{}).handleCommand("", "/help")
	if !strings.Contains(resp, "Market + Status") || !strings.Contains(resp, "Trade + Manual Management") || !strings.Contains(resp, "Runtime Controls") {
		t.Fatalf("expected grouped help categories, got %s", resp)
	}
}

func TestPlaceOrReplaceStopEscalatesWhenConvictionPendingTooLong(t *testing.T) {
	t.Setenv("LIVE_MANUAL_PROTECTION_DEFER_UNTIL_CONVICTION", "1")
	t.Setenv("LIVE_MANUAL_PROTECTION_CONVICTION_TIMEOUT_SEC", "30")
	now := time.Now().UTC()
	p := &livePosition{
		Symbol:            "SIRENUSDT",
		Side:              "SELL",
		State:             execOpen,
		RemainingQty:      50,
		EntrySource:       "BOT",
		EntryReason:       manualEntryReasonManaged,
		Managed:           true,
		ManualManageState: manualManageStatePendingProtection,
		CreatedAt:         now.Add(-2 * time.Minute),
		UpdatedAt:         now.Add(-2 * time.Minute),
	}
	m := &liveExecManager{}
	if err := m.placeOrReplaceStop(p); err != nil {
		t.Fatalf("expected timeout escalation to short-circuit without exchange call, got %v", err)
	}
	if p.ManualManageState != manualManageStateCritical {
		t.Fatalf("expected critical state after timeout escalation, got %s", p.ManualManageState)
	}
	if !p.ProtectionPending {
		t.Fatal("expected protection pending to remain true after escalation")
	}
	if got := strings.TrimSpace(p.LastManageFailCause); got != "awaiting_conviction_timeout" {
		t.Fatalf("expected awaiting_conviction_timeout cause, got %q", got)
	}
}

func TestPlaceOrReplaceStopKeepsPendingWhileAwaitingConvictionWithinTimeout(t *testing.T) {
	t.Setenv("LIVE_MANUAL_PROTECTION_DEFER_UNTIL_CONVICTION", "1")
	t.Setenv("LIVE_MANUAL_PROTECTION_CONVICTION_TIMEOUT_SEC", "900")
	now := time.Now().UTC()
	p := &livePosition{
		Symbol:            "SIRENUSDT",
		Side:              "SELL",
		State:             execOpen,
		RemainingQty:      50,
		EntrySource:       "BOT",
		EntryReason:       manualEntryReasonManaged,
		Managed:           true,
		ManualManageState: manualManageStatePendingProtection,
		CreatedAt:         now.Add(-1 * time.Minute),
		UpdatedAt:         now.Add(-1 * time.Minute),
	}
	m := &liveExecManager{}
	if err := m.placeOrReplaceStop(p); err != nil {
		t.Fatalf("expected defer path without exchange call, got %v", err)
	}
	if p.ManualManageState != manualManageStatePendingProtection {
		t.Fatalf("expected pending protection state, got %s", p.ManualManageState)
	}
	if !p.ProtectionPending || p.ProtectionRetryAfter.IsZero() {
		t.Fatalf("expected pending + retry schedule while awaiting conviction, pending=%v retry_after=%v", p.ProtectionPending, p.ProtectionRetryAfter)
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

func TestEvaluateRunnerExitStateDoesNotTightenHealthyShortRunnerOnReversalWatchAlone(t *testing.T) {
	mv := momentumView{
		Short: &inplay.Entry{
			State:             inplay.StateInPlay,
			ScoreSlope:        0.18,
			Momentum:          true,
			ReversalWatchFlag: true,
			MetaState:         "exhaust_watch",
		},
	}
	got := evaluateRunnerExitState("SELL", mv, flowfeed.ExternalSignal{})
	if got.ExhaustionConfirmed {
		t.Fatalf("expected healthy runner to avoid premature tighten, got %+v", got)
	}
	if got.StructureBroken {
		t.Fatalf("expected healthy runner to remain intact, got %+v", got)
	}
}

func TestEvaluateRunnerExitStateWithFlowKeepsHealthyRunnerWhenFlowSupports(t *testing.T) {
	mv := momentumView{
		Short: &inplay.Entry{
			State:      inplay.StateInPlay,
			ScoreSlope: 0.03,
			Momentum:   true,
		},
	}
	fm := flowMetrics{
		OFISamples:    12,
		OFIZ:          -0.62,
		BookImbalance: 0.72,
	}
	got := evaluateRunnerExitStateWithFlow("SELL", mv, fm, flowfeed.ExternalSignal{})
	if got.StructureBroken || got.ExhaustionConfirmed {
		t.Fatalf("expected supportive flow to keep runner alive, got %+v", got)
	}
}

func TestEvaluateRunnerExitStateWithFlowBreaksRunnerOnFadeAndAdverseFlow(t *testing.T) {
	t.Setenv("LIVE_MOMENTUM_EXIT_SLOPE_MAX", "0.00")
	mv := momentumView{
		Short: &inplay.Entry{
			State:      inplay.StateCooling,
			ScoreSlope: -0.05,
			Momentum:   false,
		},
	}
	fm := flowMetrics{
		OFISamples:    12,
		OFIZ:          0.80,
		BookImbalance: 1.40,
	}
	got := evaluateRunnerExitStateWithFlow("SELL", mv, fm, flowfeed.ExternalSignal{})
	if !got.StructureBroken {
		t.Fatalf("expected adverse flow plus fade to break runner, got %+v", got)
	}
}

func TestEvaluateRunnerExitStateWithFlowKeepsHealthyLongPullbackAlive(t *testing.T) {
	mv := momentumView{
		Long: &inplay.Entry{
			State:             inplay.StateBalanced,
			ScoreSlope:        -0.03,
			Momentum:          false,
			ReversalWatchFlag: true,
			MetaState:         "exhaust_watch",
		},
	}
	fm := flowMetrics{
		OFISamples:    12,
		OFIZ:          0.05,
		BookImbalance: 1.02,
	}
	got := evaluateRunnerExitStateWithFlow("BUY", mv, fm, flowfeed.ExternalSignal{})
	if got.StructureBroken || got.ExhaustionConfirmed {
		t.Fatalf("expected healthy pullback to stay alive, got %+v", got)
	}
}

func TestEvaluateRunnerExitStateWithFlowKeepsTrendAcceptedLongAlive(t *testing.T) {
	mv := momentumView{
		Long: &inplay.Entry{
			State:               inplay.StateCooling,
			ScoreSlope:          -0.20,
			Momentum:            false,
			ReversalWatchFlag:   true,
			MetaState:           "exhaust_watch",
			DrawdownFromPeakPct: -1.6,
			VWAPDistancePct:     2.1,
			EMADistancePct:      1.8,
		},
	}
	fm := flowMetrics{
		OFISamples:    12,
		OFIZ:          0.08,
		BookImbalance: 1.03,
	}
	got := evaluateRunnerExitStateWithFlow("BUY", mv, fm, flowfeed.ExternalSignal{})
	if got.StructureBroken || got.ExhaustionConfirmed {
		t.Fatalf("expected trend-accepted long to stay alive, got %+v", got)
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

func TestSessionTagUsesMajorMarketLabels(t *testing.T) {
	if got := sessionTag(time.Date(2026, 3, 25, 3, 0, 0, 0, time.UTC)); got != "ASIA_BREAK" {
		t.Fatalf("expected ASIA_BREAK at Tokyo lunch, got %s", got)
	}
	if got := sessionTag(time.Date(2026, 3, 25, 8, 0, 0, 0, time.UTC)); got != "ASIA_LONDON_OVERLAP" {
		t.Fatalf("expected ASIA_LONDON_OVERLAP at London open with Asia still active, got %s", got)
	}
	if got := sessionTag(time.Date(2026, 3, 25, 14, 0, 0, 0, time.UTC)); got != "LONDON_US_OVERLAP" {
		t.Fatalf("expected LONDON_US_OVERLAP during London/NY cash overlap, got %s", got)
	}
	if got := sessionTag(time.Date(2026, 3, 25, 20, 30, 0, 0, time.UTC)); got != "US_CLOSE" {
		t.Fatalf("expected US_CLOSE shortly after the cash close, got %s", got)
	}
	if got := sessionTag(time.Date(2026, 3, 25, 22, 0, 0, 0, time.UTC)); got != "OFF_HOURS" {
		t.Fatalf("expected OFF_HOURS after major cash sessions, got %s", got)
	}
}

func TestApplySimpleContinuationFallbackNoLegacyEarlyDevEntry(t *testing.T) {
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
	if got.Strat == "early_dev_entry" {
		t.Fatalf("expected early_dev_entry to be retired, got %q", got.Strat)
	}
}

func TestResolveLadderPlanAllowsStructuredReentry(t *testing.T) {
	t.Setenv("LIVE_REENTRY_ENABLE", "1")
	t.Setenv("LIVE_REENTRY_MAX_PER_SYMBOL", "1")
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

func TestResolveLadderPlanBlocksSecondSymbolWhenOneSymbolOnly(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"BTCUSDT": {
				Symbol:       "BTCUSDT",
				Side:         "BUY",
				State:        execOpen,
				RemainingQty: 1,
				EntrySource:  "BOT",
			},
		},
		ladderCfg: ladderConfig{OneSymbolOnly: true},
	}
	c := candidate{
		Side: "BUY",
		Entry: inplay.Entry{
			Symbol: "ETHUSDT",
		},
	}
	plan := resolveLadderPlan(time.Now().UTC(), c, execMgr, nil)
	if plan.RejectReason != "one_symbol_only_active" {
		t.Fatalf("expected one_symbol_only_active, got %+v", plan)
	}
}

func TestPrefilterCandidatesBeforeExpensiveWorkOneSymbolOnly(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"BTCUSDT": {
				Symbol:       "BTCUSDT",
				Side:         "BUY",
				State:        execOpen,
				RemainingQty: 1,
				EntrySource:  "BOT",
			},
		},
		ladderCfg: ladderConfig{OneSymbolOnly: true},
	}
	in := []candidate{
		{Side: "BUY", Entry: inplay.Entry{Symbol: "BTCUSDT"}},
		{Side: "BUY", Entry: inplay.Entry{Symbol: "ETHUSDT"}},
	}
	filtered, rejected := prefilterCandidatesBeforeExpensiveWork(in, execMgr)
	if len(filtered) != 1 || !strings.EqualFold(filtered[0].Entry.Symbol, "BTCUSDT") {
		t.Fatalf("expected only active symbol candidate before expensive work, got %+v", filtered)
	}
	if got := rejected["ETHUSDT"]; got != "one_symbol_only_active" {
		t.Fatalf("expected ETH one-symbol early reject, got %q", got)
	}
}

func TestManageApprovalBlockedByWindowUsesMaintenanceTimezone(t *testing.T) {
	t.Setenv("LIVE_MAINT_TZ", "America/Chicago")
	t.Setenv("LIVE_MAINT1_ENABLE", "1")
	t.Setenv("LIVE_MAINT1_START_HOUR", "0")
	t.Setenv("LIVE_MAINT1_START_MIN", "0")
	t.Setenv("LIVE_MAINT1_END_HOUR", "1")
	t.Setenv("LIVE_MAINT1_END_MIN", "0")
	origLocal := time.Local
	time.Local = time.UTC
	defer func() { time.Local = origLocal }()

	now := time.Date(2026, 4, 4, 5, 45, 0, 0, time.UTC) // 00:45 America/Chicago
	if _, _, blocked := blockedNewRiskWindow(now, time.Local); blocked {
		t.Fatal("expected UTC local clock to be outside blocked window")
	}

	reason, blocked := manageApprovalBlockedByWindow(now, "MANAGE")
	if !blocked {
		t.Fatal("expected maintenance policy timezone to block manual manage approval")
	}
	if reason != blockedMaintenanceWindowReason {
		t.Fatalf("expected %s, got %q", blockedMaintenanceWindowReason, reason)
	}

	if _, blocked := manageApprovalBlockedByWindow(now, "FORCE_FLAT"); blocked {
		t.Fatal("did not expect force-flat manage action to be blocked")
	}
}

func TestResolveLadderPlanRejectsAddWhenFixedSizeNoAddEnabled(t *testing.T) {
	t.Setenv("LIVE_FIXED_SIZE_NO_ADD", "1")
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"LYNUSDT": {
				Symbol:         "LYNUSDT",
				Side:           "BUY",
				State:          execOpen,
				EntrySource:    "BOT",
				EntryReason:    "continuation_fast",
				RemainingQty:   50,
				DeployedMargin: 50,
				EntryPrice:     1.00,
			},
		},
		ladderCfg: loadLadderConfig(50),
	}
	c := candidate{
		Side:         "BUY",
		Strat:        "continuation_fast",
		LastClose:    1.05,
		SessionVWAP:  1.02,
		EMA9:         1.03,
		ReclaimHold:  true,
		RetestHold:   true,
		ResetRebreak: true,
		Entry: inplay.Entry{
			Symbol:       "LYNUSDT",
			State:        inplay.StateInPlay,
			EntryStyle:   "pullback_long",
			CurrentScore: 95,
			ScoreSlope:   0.20,
			Momentum:     true,
		},
		Sig: strategies.Signal{
			Entry: 1.05,
			Stop:  1.00,
			TP1:   1.12,
			TP2:   1.18,
			TP3:   1.24,
		},
	}
	plan := resolveLadderPlan(time.Date(2026, 4, 7, 14, 0, 0, 0, time.UTC), c, execMgr, map[string]symbolMeta{"LYNUSDT": {LastPrice: 1.05}})
	if plan.RejectReason != "fixed_size_no_add" {
		t.Fatalf("expected fixed_size_no_add reject, got %+v", plan)
	}
}

func TestSessionEntryRejectReasonDoesNotGateFreshEntry(t *testing.T) {
	c := candidate{
		Side:  "BUY",
		Strat: "continuation_fast",
		Entry: inplay.Entry{CurrentGrade: "A"},
	}
	reason := sessionEntryRejectReason(time.Date(2026, 3, 25, 5, 30, 0, 0, time.UTC), c, ladderPlan{})
	if reason != "" {
		t.Fatalf("expected no session-gating reject, got %q", reason)
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

func TestSessionEntryRejectReasonDoesNotGateOffHoursWeakEntry(t *testing.T) {
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
		t.Fatalf("expected no off-hours gating reject, got %q", reason)
	}
}

func TestChaseRejectReasonBlocksObviousChaseAndAllowsStructuredReset(t *testing.T) {
	chase := candidate{
		Side:      "BUY",
		Strat:     "continuation_fast",
		DayUTC24h: 10.0,
		Entry: inplay.Entry{
			State:           inplay.StatePumping,
			BarsSinceTrough: 1,
		},
	}
	if got := chaseRejectReason(chase, false); got == "" {
		t.Fatal("expected obvious chase setup to be rejected")
	}

	reset := chase
	reset.ReclaimHold = true
	reset.RetestHold = true
	if got := chaseRejectReason(reset, true); got != "" {
		t.Fatalf("expected structured reset/retest to bypass chase reject, got %q", got)
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
	t.Setenv("LIVE_STARTER_USDT", "10")
	cfg := loadSafetyConfig(0, 25)
	if cfg.minAvailUSDT != 10 {
		t.Fatalf("expected starter-based min available of 10, got %.2f", cfg.minAvailUSDT)
	}
}

func TestLoadLadderConfigDefaultsStarterToTen(t *testing.T) {
	t.Setenv("LIVE_STARTER_USDT", "")
	t.Setenv("LIVE_ENTRY_STARTER_USDT", "")
	cfg := loadLadderConfig(0)
	if cfg.StarterUSDT != 10 {
		t.Fatalf("expected starter default of 10, got %.2f", cfg.StarterUSDT)
	}
}

func TestLoadLadderConfigPrefersNewSizingEnvNames(t *testing.T) {
	t.Setenv("LIVE_STARTER_USDT", "25")
	t.Setenv("LIVE_ADD_USDT", "25")
	t.Setenv("LIVE_MAX_TOTAL_USDT", "100")
	t.Setenv("LIVE_ENTRY_STARTER_USDT", "10")
	t.Setenv("LIVE_PYRAMID_STEP_USDT", "10")
	t.Setenv("LIVE_PYRAMID_MAX_TOTAL_USDT", "50")
	cfg := loadLadderConfig(20)
	if cfg.StarterUSDT != 25 {
		t.Fatalf("expected starter from LIVE_STARTER_USDT, got %.2f", cfg.StarterUSDT)
	}
	if cfg.StepUSDT != 25 {
		t.Fatalf("expected add step from LIVE_ADD_USDT, got %.2f", cfg.StepUSDT)
	}
	if cfg.MaxTotalUSDT != 100 {
		t.Fatalf("expected max total from LIVE_MAX_TOTAL_USDT, got %.2f", cfg.MaxTotalUSDT)
	}
}

func TestLoadLadderConfigFixedSizeNoAddMode(t *testing.T) {
	t.Setenv("LIVE_ENTRY_STARTER_USDT", "50")
	t.Setenv("LIVE_PYRAMID_STEP_USDT", "25")
	t.Setenv("LIVE_PYRAMID_MAX_TOTAL_USDT", "125")
	t.Setenv("LIVE_PYRAMID_MAX_ADDS", "3")
	t.Setenv("LIVE_FIXED_SIZE_NO_ADD", "1")
	cfg := loadLadderConfig(20)
	if cfg.StarterUSDT != 50 {
		t.Fatalf("expected starter 50, got %.2f", cfg.StarterUSDT)
	}
	if cfg.StepUSDT != 0 || cfg.MaxTotalUSDT != 50 || cfg.MaxAdds != 0 {
		t.Fatalf("expected fixed-size no-add config, got %+v", cfg)
	}
	if !ladderAddsDisabled(cfg) {
		t.Fatal("expected fixed-size ladder to disable adds")
	}
}

func TestContinuationLaneRejectReasonBlocksExtendedNoReset(t *testing.T) {
	t.Setenv("LIVE_ADD_MAX_DIRECTIONAL_PCT", "6")
	c := candidate{
		Side:         "BUY",
		Strat:        "continuation_fast",
		DayUTC24h:    9.0,
		ExtensionATR: 1.5,
		Entry: inplay.Entry{
			State: inplay.StateInPlay,
		},
	}
	if got := continuationLaneRejectReason(c); got == "" {
		t.Fatal("expected continuation lane to reject extended no-reset candidate")
	}
}

func TestContinuationLaneRejectReasonBlocksExhaustionAndFadingImpulse(t *testing.T) {
	exhausted := candidate{
		Side:  "BUY",
		Strat: "continuation_fast",
		Entry: inplay.Entry{
			State:      inplay.StateInPlay,
			EntryStyle: "avoid_chase",
		},
	}
	if got := continuationLaneRejectReason(exhausted); got != "continuation_exhaustion_active" {
		t.Fatalf("expected continuation_exhaustion_active, got %q", got)
	}

	fading := candidate{
		Side:        "BUY",
		Strat:       "continuation_fast",
		LastClose:   99,
		SessionVWAP: 100,
		EMA9:        100,
		Entry: inplay.Entry{
			State:      inplay.StateInPlay,
			ScoreSlope: 0.0,
		},
		TriggerState: string(triggerFailReclaim),
	}
	if got := continuationLaneRejectReason(fading); got != "continuation_impulse_fading" {
		t.Fatalf("expected continuation_impulse_fading, got %q", got)
	}
}

func TestStarterLaneQualityRequiresPersistenceOrReset(t *testing.T) {
	t.Setenv("LIVE_STARTER_PERSIST_MIN_SEEN", "2")
	t.Setenv("LIVE_STARTER_PERSIST_MIN_TOPN", "1")
	c := candidate{
		Strat: "continuation_fast_starter",
		Entry: inplay.Entry{State: inplay.StateInPlay},
	}
	if starterLaneQualityReady(c) {
		t.Fatal("expected starter lane quality to fail without reset/persistence")
	}
	c.PersistenceSeenCount = 2
	c.PersistenceTopNCount = 1
	if !starterLaneQualityReady(c) {
		t.Fatal("expected starter lane quality to pass on persistence evidence")
	}
}

func TestResolveLadderPlanStarterLaneBlocksAdds(t *testing.T) {
	execMgr := &liveExecManager{
		positions: map[string]*livePosition{
			"LYNUSDT": {
				Symbol:       "LYNUSDT",
				Side:         "BUY",
				State:        execOpen,
				EntrySource:  "BOT",
				RemainingQty: 1,
			},
		},
		ladderCfg: loadLadderConfig(10),
	}
	c := candidate{
		Side:  "BUY",
		Strat: "reclaim_long_starter",
		Entry: inplay.Entry{Symbol: "LYNUSDT"},
	}
	plan := resolveLadderPlan(time.Date(2026, 4, 8, 13, 0, 0, 0, time.UTC), c, execMgr, nil)
	if plan.IsAdd || plan.RejectReason != "starter_lane_no_adds" {
		t.Fatalf("expected starter lane no-add rejection, got %+v", plan)
	}
}

func TestQuickCandidateSelectionRejectBlocksMaintenanceWindow(t *testing.T) {
	t.Setenv("LIVE_MAINT1_ENABLE", "1")
	t.Setenv("LIVE_MAINT1_START_HOUR", "0")
	t.Setenv("LIVE_MAINT1_START_MIN", "0")
	t.Setenv("LIVE_MAINT1_END_HOUR", "1")
	t.Setenv("LIVE_MAINT1_END_MIN", "0")
	loc, _ := time.LoadLocation("America/Chicago")
	now := time.Date(2026, 4, 4, 0, 45, 0, 0, time.UTC)
	local := time.Date(2026, 4, 4, 0, 45, 0, 0, loc)
	c := candidate{Side: "BUY", Entry: inplay.Entry{Symbol: "BTCUSDT"}}
	got := quickCandidateSelectionReject(c, now, false, false, 0, local, maintenanceWindow{}, 0, nil, nil, safetyConfig{}, time.Time{}, nil, nil, nil, nil, nil)
	if got != blockedMaintenanceWindowReason {
		t.Fatalf("expected %s, got %q", blockedMaintenanceWindowReason, got)
	}
}

func TestMaintenanceWindowBlocksFreshPromotionButKeepsProtectionHandling(t *testing.T) {
	t.Setenv("LIVE_MAINT1_ENABLE", "1")
	t.Setenv("LIVE_MAINT1_START_HOUR", "0")
	t.Setenv("LIVE_MAINT1_START_MIN", "0")
	t.Setenv("LIVE_MAINT1_END_HOUR", "1")
	t.Setenv("LIVE_MAINT1_END_MIN", "0")
	loc, _ := time.LoadLocation("America/Chicago")
	now := time.Date(2026, 4, 4, 0, 45, 0, 0, time.UTC)
	localMaint := time.Date(2026, 4, 4, 0, 45, 0, 0, loc)
	c := candidate{Side: "BUY", Entry: inplay.Entry{Symbol: "BTCUSDT"}}
	freshReject := quickCandidateSelectionReject(
		c,
		now,
		false,
		false,
		0,
		localMaint,
		maintenanceWindow{},
		0,
		nil,
		nil,
		safetyConfig{},
		time.Time{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if freshReject != blockedMaintenanceWindowReason {
		t.Fatalf("expected fresh promotion blocked by maintenance window, got %q", freshReject)
	}

	mgr := &liveExecManager{}
	p := &livePosition{
		Symbol:            "BTCUSDT",
		Side:              "BUY",
		State:             execOpen,
		EntrySource:       manualEntrySourceManaged,
		EntryReason:       manualEntryReasonManaged,
		RemainingQty:      1,
		EntryPrice:        100,
		LastMark:          99.5,
		ManualManageState: manualManageStatePendingProtection,
		Managed:           true,
	}
	mgr.handleManualProtectionFailure(p, "exchange_immediate_trigger_retry_failed", now)
	if p.State != execOpen {
		t.Fatalf("expected essential protection handling to keep position open, got state=%s", p.State)
	}
	if !p.ProtectionPending || p.ProtectionRetryAfter.IsZero() {
		t.Fatalf("expected essential protection retry scheduling during maintenance, pending=%v retry_after=%v", p.ProtectionPending, p.ProtectionRetryAfter)
	}
}

func TestApplyPnLProtectiveStopLocksMaterialProfitOnceArmed(t *testing.T) {
	t.Setenv("LIVE_PNL_PROTECT_ARM_PCT", "20")
	t.Setenv("LIVE_PNL_PROTECT_LOCK_FRAC", "0.75")
	stop, changed := applyPnLProtectiveStop("SELL", 1.00, 1.02, 0.80, 40.0)
	if !changed {
		t.Fatal("expected pnl protective lock to tighten stop")
	}
	if math.Abs(stop-0.85) > 1e-9 {
		t.Fatalf("expected short stop to lock 75%% of move at 0.85, got %.6f", stop)
	}
}

func TestPerpSweepAmountUsesTargetCeiling(t *testing.T) {
	cfg := fundsManagerConfig{PerpTargetUSDT: 200, PerpFloorUSDT: 150}
	if got := perpSweepAmount(235, cfg); got != 35 {
		t.Fatalf("expected sweep amount 35, got %.2f", got)
	}
	if got := perpSweepAmount(199, cfg); got != 0 {
		t.Fatalf("expected no sweep below target, got %.2f", got)
	}
}

func TestAutoSweepAmountUsesSweepMinNotTopupMin(t *testing.T) {
	cfg := fundsManagerConfig{
		PerpTargetUSDT: 200,
		PerpFloorUSDT:  150,
		TopupMinUSDT:   10,
		SweepMinUSDT:   0.01,
	}
	if got := autoSweepAmount(202.66, cfg); math.Abs(got-2.66) > 1e-9 {
		t.Fatalf("expected sweep amount 2.66, got %.6f", got)
	}
	if got := autoSweepAmount(200.005, cfg); got != 0 {
		t.Fatalf("expected no sweep below sweep minimum, got %.6f", got)
	}
}

func TestPerpTopupTargetUsesFloorGuard(t *testing.T) {
	cfg := fundsManagerConfig{PerpTargetUSDT: 200, PerpFloorUSDT: 150}
	if got := perpTopupTarget(149, cfg); got != 200 {
		t.Fatalf("expected topup target 200 below floor, got %.2f", got)
	}
	if got := perpTopupTarget(151, cfg); got != 0 {
		t.Fatalf("expected no topup above floor, got %.2f", got)
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

func TestCandidateSelectionRankPriorDayBoostTieBreak(t *testing.T) {
	base := candidate{FinalRank: 88.0}
	boosted := base
	boosted.PriorDayLeaderBoost = 0.60
	if candidateSelectionRank(boosted) <= candidateSelectionRank(base) {
		t.Fatalf("expected prior-day boost to improve tie-break rank")
	}
}

func TestPersistenceEligibilityScoreIncludesPriorDayBoost(t *testing.T) {
	c := candidate{
		CombinedScore:        0.38,
		PersistenceSeenCount: 1,
		PersistenceTopNCount: 0,
	}
	base := persistenceEligibilityScore(c)
	c.PriorDayLeaderBoost = 0.55
	boosted := persistenceEligibilityScore(c)
	if boosted <= base {
		t.Fatalf("expected prior-day boost to lift persistence score, base=%.3f boosted=%.3f", base, boosted)
	}
}

func TestStarterLaneQualityReadyAllowsPriorDayLeaderPreference(t *testing.T) {
	c := candidate{
		Strat:               "continuation_fast_starter",
		PriorDayLeaderBoost: 0.60,
		LastClose:           1.05,
		SessionVWAP:         1.02,
		EMA9:                1.02,
		Entry: inplay.Entry{
			State:          inplay.StateInPlay,
			ScoreSlope:     0.08,
			ExhaustionRisk: 1.2,
		},
	}
	if !starterLaneQualityReady(c) {
		t.Fatal("expected prior-day leader preference to satisfy starter quality")
	}
}

func TestAmplifierDoesNotBypassHardBlocks(t *testing.T) {
	c := candidate{
		Entry:               inplay.Entry{Symbol: "BTCUSDT", CurrentGrade: "A"},
		Side:                "BUY",
		Strat:               "elite_starter",
		Conf:                0.92,
		FinalRank:           90,
		PriorDayLeaderBoost: 0.80,
	}
	summary := newEligibilitySummary(c)
	summary.StarterAllowed = true
	addEligibilityBlock(&summary, "max_open_positions")
	chooseFinalDecision(&summary, ladderPlan{})
	if summary.FinalDecision != "reject" {
		t.Fatalf("expected hard blocker to force reject despite amplifier, got %s (%s)", summary.FinalDecision, summary.FinalReason)
	}
	if summary.FinalReason != "max_open_positions" {
		t.Fatalf("expected hard blocker reason to remain primary, got %q", summary.FinalReason)
	}
}
