package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	w1 := maintenanceWindow{Name: "M1", StartHour: 0, EndHour: 1}
	w2 := maintenanceWindow{Name: "M2", StartHour: 16, EndHour: 18, ForceFlat: true}
	w, ok := activeMaintenanceWindow(now, true, w1, w2)
	if !ok || w.Name != "M2" || !w.ForceFlat {
		t.Fatalf("expected M2 active at 16:10")
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

func TestShouldCloseStaleLive(t *testing.T) {
	now := time.Now()
	p := &livePosition{CreatedAt: now.Add(-4 * time.Hour), MaxFavorableR: 0.1}
	if !shouldCloseStaleLive(p, now, 3*time.Hour, 0.25, 0.75) {
		t.Fatalf("expected stale close")
	}
	p.MaxFavorableR = 0.9
	if shouldCloseStaleLive(p, now, 3*time.Hour, 0.25, 0.75) {
		t.Fatalf("expected grace to skip stale close")
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
