package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestComputeGrowthWindowsUsesNearestEarlierSnapshot(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	records := []accountSnapshotRecord{
		{AccountSummary: AccountSummary{Timestamp: now.Add(-31 * 24 * time.Hour), PerpEquity: 70}},
		{AccountSummary: AccountSummary{Timestamp: now.Add(-8 * 24 * time.Hour), PerpEquity: 90}},
		{AccountSummary: AccountSummary{Timestamp: now.Add(-25 * time.Hour), PerpEquity: 100}},
		{AccountSummary: AccountSummary{Timestamp: now.Add(-23 * time.Hour), PerpEquity: 105}},
	}
	current := accountSnapshotRecord{AccountSummary: AccountSummary{Timestamp: now, PerpEquity: 130}}
	windows := computeGrowthWindows(now, records, current, func(r accountSnapshotRecord) float64 {
		return r.PerpEquity
	})
	if len(windows) != 3 {
		t.Fatalf("expected 3 growth windows, got %d", len(windows))
	}
	if !windows[0].Available || windows[0].StartEquity != 100 {
		t.Fatalf("expected 24h window to use 25h snapshot, got %+v", windows[0])
	}
	if windows[1].StartEquity != 90 {
		t.Fatalf("expected 7d start equity 90, got %+v", windows[1])
	}
	if windows[2].StartEquity != 70 {
		t.Fatalf("expected 30d start equity 70, got %+v", windows[2])
	}
}

func TestComputeGrowthWindowsReportsInsufficientHistory(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	current := accountSnapshotRecord{AccountSummary: AccountSummary{Timestamp: now, PerpEquity: 130}}
	windows := computeGrowthWindows(now, nil, current, func(r accountSnapshotRecord) float64 {
		return r.PerpEquity
	})
	for _, window := range windows {
		if window.Available {
			t.Fatalf("expected insufficient history window, got %+v", window)
		}
	}
}

func TestAppendAndLoadAccountSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account_snapshots.jsonl")
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	if err := appendAccountSnapshot(path, accountSnapshotRecord{
		AccountSummary: AccountSummary{Timestamp: now, PerpEquity: 120},
	}); err != nil {
		t.Fatalf("appendAccountSnapshot error: %v", err)
	}
	if err := appendAccountSnapshot(path, accountSnapshotRecord{
		AccountSummary: AccountSummary{Timestamp: now.Add(time.Minute), PerpEquity: 121},
	}); err != nil {
		t.Fatalf("appendAccountSnapshot second error: %v", err)
	}
	records, err := loadAccountSnapshots(path)
	if err != nil {
		t.Fatalf("loadAccountSnapshots error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 snapshot records, got %d", len(records))
	}
	if records[0].PerpEquity != 120 || records[1].PerpEquity != 121 {
		t.Fatalf("unexpected snapshot records: %+v", records)
	}
}

func TestApplyEntryBlockFromHealthLockedUsesHysteresis(t *testing.T) {
	m := &liveExecManager{}

	m.applyEntryBlockFromHealthLocked(accountReport{Health: "degraded", HealthDetail: "stale_account"})
	if !m.entryBlockActive {
		t.Fatalf("expected degraded health to block entries immediately")
	}
	if m.entryBlockReason == "" {
		t.Fatalf("expected degraded block reason to be set")
	}
	if m.healthyAccountReads != 0 {
		t.Fatalf("expected healthy reads reset on unhealthy report")
	}

	m.applyEntryBlockFromHealthLocked(accountReport{Health: "healthy"})
	if !m.entryBlockActive {
		t.Fatalf("expected one healthy read to keep block active")
	}
	if m.healthyAccountReads != 1 {
		t.Fatalf("expected first healthy read to increment hysteresis counter, got %d", m.healthyAccountReads)
	}

	m.applyEntryBlockFromHealthLocked(accountReport{Health: "healthy"})
	if m.entryBlockActive {
		t.Fatalf("expected second consecutive healthy read to clear entry block")
	}
	if m.entryBlockReason != "" {
		t.Fatalf("expected clear reason after hysteresis unlock, got %q", m.entryBlockReason)
	}
}
