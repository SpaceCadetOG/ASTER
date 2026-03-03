package main

import (
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
	w2 := maintenanceWindow{Name: "M2", StartHour: 16, EndHour: 17, ForceFlat: true}
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
