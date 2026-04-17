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

func baseSimplePaperLongCandidate() candidate {
	c := baseSimpleLongCandidate()
	c.Entry.CurrentGrade = "A"
	c.Entry.CurrentScore = 92
	c.Entry.ScoreSlope = 0.04 // advisory only for paper path
	c.Entry.State = inplay.StateInPlay
	c.DayUTC24h = 7.2
	c.VolumeRatio = 1.25
	c.FinalRank = 96
	c.Entry.Rank = 2.0
	return c
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

func TestDecideSimplePaperEntryNowStrongLeaderPasses(t *testing.T) {
	dec := decideSimplePaperEntryNow(baseSimplePaperLongCandidate(), accountHealthSummary{State: "healthy"})
	if !dec.Allowed || dec.Side != "LONG" || dec.Reason != "paper_entry_now_long" {
		t.Fatalf("expected allowed paper long entry, got %+v", dec)
	}
}

func TestDecideSimplePaperEntryNowEliteBalancedStillAllowed(t *testing.T) {
	c := baseSimplePaperLongCandidate()
	c.Entry.CurrentScore = 101
	c.Entry.State = inplay.StateBalanced
	c.Entry.ScoreSlope = -0.03
	dec := decideSimplePaperEntryNow(c, accountHealthSummary{State: "healthy"})
	if !dec.Allowed {
		t.Fatalf("expected elite balanced leader to stay tradable, got %+v", dec)
	}
	if !dec.PullbackPreferred {
		t.Fatalf("expected pullback preference for >=100 score, got %+v", dec)
	}
}

func TestDecideSimplePaperEntryNowLowScoreFails(t *testing.T) {
	c := baseSimplePaperLongCandidate()
	c.Entry.CurrentScore = 80
	dec := decideSimplePaperEntryNow(c, accountHealthSummary{State: "healthy"})
	if dec.Allowed || dec.Reason != "low_score" {
		t.Fatalf("expected low_score reject, got %+v", dec)
	}
}

func TestDecideSimplePaperEntryNowResetMoveFails(t *testing.T) {
	c := baseSimplePaperLongCandidate()
	c.DayUTC24h = 3.2
	dec := decideSimplePaperEntryNow(c, accountHealthSummary{State: "healthy"})
	if dec.Allowed || dec.Reason != "reset_move_below_threshold" {
		t.Fatalf("expected reset threshold reject, got %+v", dec)
	}
}

func TestDecideSimplePaperEntryNowDownsideShortPasses(t *testing.T) {
	c := baseSimplePaperLongCandidate()
	c.Side = "SELL"
	c.Entry.State = inplay.StateCooling
	c.Entry.CurrentGrade = "A"
	c.DayUTC24h = -8.4
	dec := decideSimplePaperEntryNow(c, accountHealthSummary{State: "healthy"})
	if !dec.Allowed || dec.Side != "SHORT" || dec.Reason != "paper_entry_now_short" {
		t.Fatalf("expected allowed downside short, got %+v", dec)
	}
}

func TestDecideSimplePaperEntryNowSpreadRelaxedForPaperValidation(t *testing.T) {
	c := baseSimplePaperLongCandidate()
	c.SpreadBps = 14.0 // > live default(10), but within paper relaxed cap
	dec := decideSimplePaperEntryNow(c, accountHealthSummary{State: "healthy"})
	if !dec.Allowed || dec.Reason != "paper_entry_now_long" {
		t.Fatalf("expected relaxed paper spread to allow entry, got %+v", dec)
	}
}

func TestDecideSimplePaperEntryNowSpreadStillHardFailsWhenExtreme(t *testing.T) {
	c := baseSimplePaperLongCandidate()
	c.SpreadBps = 45.0
	dec := decideSimplePaperEntryNow(c, accountHealthSummary{State: "healthy"})
	if dec.Allowed || dec.Reason != "spread_too_wide" {
		t.Fatalf("expected extreme spread to still block, got %+v", dec)
	}
}

func TestDecideSimplePaperEntryNowAccountHealthBlocked(t *testing.T) {
	dec := decideSimplePaperEntryNow(baseSimplePaperLongCandidate(), accountHealthSummary{State: "degraded"})
	if dec.Allowed || dec.Reason != "account_health_degraded" {
		t.Fatalf("expected degraded block, got %+v", dec)
	}
}

func TestDecideSimplePaperEntryNowPartialHealthAllowedByDefault(t *testing.T) {
	dec := decideSimplePaperEntryNow(baseSimplePaperLongCandidate(), accountHealthSummary{State: "partial"})
	if !dec.Allowed || dec.Reason != "paper_entry_now_long" {
		t.Fatalf("expected partial health to stay tradable in paper mode by default, got %+v", dec)
	}
}

func TestDecideSimplePaperEntryNowPartialHealthCanBeBlockedByFlag(t *testing.T) {
	t.Setenv("LIVE_PAPER_BLOCK_ON_PARTIAL_HEALTH", "1")
	dec := decideSimplePaperEntryNow(baseSimplePaperLongCandidate(), accountHealthSummary{State: "partial"})
	if dec.Allowed || dec.Reason != "account_health_partial" {
		t.Fatalf("expected partial health block with flag, got %+v", dec)
	}
}
