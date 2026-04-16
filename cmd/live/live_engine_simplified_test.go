package main

import (
	"strings"
	"testing"
	"time"

	"go-machine/internal/inplay"
)

func baseSimpleLongCandidate() candidate {
	return candidate{
		Side:        "BUY",
		SpreadBps:   4,
		FinalRank:   92,
		LastClose:   101.0,
		VolumeRatio: 0.60, // intentionally low: no longer a hard routing blocker
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			State:        inplay.StateInPlay,
			CurrentScore: 92,
			ScoreSlope:   0.26,
			Rank:         2.0,
		},
	}
}

func withHealthyAccountProvider(t *testing.T) {
	t.Helper()
	setLiveEntryAccountHealthProvider(func() accountHealthSummary {
		return accountHealthSummary{State: "healthy"}
	})
	t.Cleanup(func() {
		setLiveEntryAccountHealthProvider(func() accountHealthSummary {
			return accountHealthSummary{State: "healthy"}
		})
	})
}

func TestDecideSimpleEntryNowStrongLongPasses(t *testing.T) {
	dec := decideSimpleEntryNow(baseSimpleLongCandidate(), accountHealthSummary{State: "healthy"})
	if !dec.Allowed || dec.Side != "LONG" || dec.Reason != "entry_now_long" {
		t.Fatalf("expected allowed long entry_now, got %+v", dec)
	}
}

func TestDecideSimpleEntryNowStrongShortPasses(t *testing.T) {
	c := baseSimpleLongCandidate()
	c.Side = "SELL"
	c.Entry.Symbol = "ETHUSDT"
	c.Entry.State = inplay.StateHeating
	c.Entry.CurrentScore = 91
	c.Entry.ScoreSlope = 0.24
	c.Entry.Rank = 1.5
	dec := decideSimpleEntryNow(c, accountHealthSummary{State: "healthy"})
	if !dec.Allowed || dec.Side != "SHORT" || dec.Reason != "entry_now_short" {
		t.Fatalf("expected allowed short entry_now, got %+v", dec)
	}
}

func TestChoosePrimaryLiveSignalHardBlockWinsFirst(t *testing.T) {
	withHealthyAccountProvider(t)
	c := baseSimpleLongCandidate()
	c.ExtensionATR = 3.0
	got := choosePrimaryLiveSignal(c, time.Now().UTC())
	if got.RejectReason != "extended" {
		t.Fatalf("expected hard block first, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestChoosePrimaryLiveSignalHeatingStateCanEnter(t *testing.T) {
	withHealthyAccountProvider(t)
	c := baseSimpleLongCandidate()
	c.Entry.State = inplay.StateHeating
	got := choosePrimaryLiveSignal(c, time.Now().UTC())
	if got.Strat != "entry_now_long" {
		t.Fatalf("expected entry_now_long for heating leader, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestChoosePrimaryLiveSignalWeakSlopeFails(t *testing.T) {
	withHealthyAccountProvider(t)
	c := baseSimpleLongCandidate()
	c.Entry.ScoreSlope = 0.05
	got := choosePrimaryLiveSignal(c, time.Now().UTC())
	if got.Strat != "none" || got.RejectReason != "no_simple_entry" {
		t.Fatalf("expected no_simple_entry for weak slope, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestChoosePrimaryLiveSignalLowScoreFails(t *testing.T) {
	withHealthyAccountProvider(t)
	c := baseSimpleLongCandidate()
	c.Entry.CurrentScore = 70
	got := choosePrimaryLiveSignal(c, time.Now().UTC())
	if got.Strat != "none" || got.RejectReason != "no_simple_entry" {
		t.Fatalf("expected no_simple_entry for low score, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestDecideSimpleEntryNowBlocksCooldownStopoutAndPositionConflict(t *testing.T) {
	base := baseSimpleLongCandidate()
	cooldown := base
	cooldown.RejectReason = "symbol_cooldown_active"
	dec := decideSimpleEntryNow(cooldown, accountHealthSummary{State: "healthy"})
	if dec.Allowed || dec.Reason != "symbol_cooldown_active" {
		t.Fatalf("expected cooldown block, got %+v", dec)
	}

	stopout := base
	stopout.RejectReason = "symbol_stopout_lock_active"
	dec = decideSimpleEntryNow(stopout, accountHealthSummary{State: "healthy"})
	if dec.Allowed || dec.Reason != "symbol_stopout_lock_active" {
		t.Fatalf("expected stopout block, got %+v", dec)
	}

	conflict := base
	conflict.RejectReason = "position_conflict_with_open_position"
	dec = decideSimpleEntryNow(conflict, accountHealthSummary{State: "healthy"})
	if dec.Allowed || dec.Reason != "position_conflict" {
		t.Fatalf("expected position conflict block, got %+v", dec)
	}
}

func TestChoosePrimaryLiveSignalExtendedFails(t *testing.T) {
	withHealthyAccountProvider(t)
	c := baseSimpleLongCandidate()
	c.ExtensionATR = 2.5
	got := choosePrimaryLiveSignal(c, time.Now().UTC())
	if got.RejectReason != "extended" {
		t.Fatalf("expected extended hard block, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestChoosePrimaryLiveSignalExhaustedFails(t *testing.T) {
	withHealthyAccountProvider(t)
	c := baseSimpleLongCandidate()
	c.Entry.ExhaustionRisk = 6.5
	got := choosePrimaryLiveSignal(c, time.Now().UTC())
	if got.RejectReason != "exhausted" {
		t.Fatalf("expected exhausted hard block, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestChoosePrimaryLiveSignalDegradedAccountHealthFails(t *testing.T) {
	setLiveEntryAccountHealthProvider(func() accountHealthSummary {
		return accountHealthSummary{State: "degraded"}
	})
	t.Cleanup(func() {
		setLiveEntryAccountHealthProvider(func() accountHealthSummary {
			return accountHealthSummary{State: "healthy"}
		})
	})
	got := choosePrimaryLiveSignal(baseSimpleLongCandidate(), time.Now().UTC())
	if got.Strat != "none" || got.RejectReason != "no_simple_entry" {
		t.Fatalf("expected no_simple_entry when account health blocks entry, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestSessionLabelMetadataOnlyDoesNotChangeSimpleRouting(t *testing.T) {
	withHealthyAccountProvider(t)
	a := baseSimpleLongCandidate()
	b := a
	a.SessionLabel = "ASIA_DEV"
	b.SessionLabel = "NY_OPEN"
	gotA := choosePrimaryLiveSignal(a, time.Now().UTC())
	gotB := choosePrimaryLiveSignal(b, time.Now().UTC())
	if gotA.Strat != gotB.Strat {
		t.Fatalf("expected session labels to be metadata-only, got %q vs %q", gotA.Strat, gotB.Strat)
	}
}

func TestEntriesBlockedByAccountHealth(t *testing.T) {
	if reason, blocked := entriesBlockedByAccountHealth(accountHealthSummary{State: "degraded"}); !blocked || reason != "account_health_degraded" {
		t.Fatalf("expected degraded block, got blocked=%v reason=%q", blocked, reason)
	}
	if reason, blocked := entriesBlockedByAccountHealth(accountHealthSummary{State: "failed"}); !blocked || reason != "account_health_failed" {
		t.Fatalf("expected failed block, got blocked=%v reason=%q", blocked, reason)
	}
	if reason, blocked := entriesBlockedByAccountHealth(accountHealthSummary{State: "healthy", SignedUserDataBackoff: true}); !blocked || reason != "signed_user_data_backoff" {
		t.Fatalf("expected signed-user backoff block, got blocked=%v reason=%q", blocked, reason)
	}
	if reason, blocked := entriesBlockedByAccountHealth(accountHealthSummary{State: "healthy"}); blocked || reason != "" {
		t.Fatalf("expected healthy clear, got blocked=%v reason=%q", blocked, reason)
	}
}

func TestManualApprovalRequiresImmediateLiveProtection(t *testing.T) {
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
	_, err := m.activateManualManagement(req, now, "MANUAL_APPROVED")
	if err == nil || !strings.Contains(err.Error(), "immediate protection attach failed") {
		t.Fatalf("expected immediate protection failure, got err=%v", err)
	}
}

func TestManualStopRetryCandidatesBoundedLadder(t *testing.T) {
	cands := manualStopRetryCandidates("SELL", 1.00, 0.98, 0.0001)
	if len(cands) != 4 {
		t.Fatalf("expected base + 3 retries, got %d (%#v)", len(cands), cands)
	}
}
