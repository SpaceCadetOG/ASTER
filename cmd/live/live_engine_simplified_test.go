package main

import (
	"strings"
	"testing"
	"time"

	"go-machine/internal/inplay"
)

func baseIgniteCandidate() candidate {
	return candidate{
		Side:        "BUY",
		LastClose:   101.0,
		SessionVWAP: 100.0,
		EMA9:        100.2,
		VolumeRatio: 1.5,
		OFIZ:        0.8,
		OFISamples:  12,
		Entry: inplay.Entry{
			Symbol:         "BTCUSDT",
			EntryStyle:     "momentum_ignite_long",
			State:          inplay.StateHeating,
			TimeInStateMin: 3,
			Momentum:       true,
			CurrentScore:   92,
			ScoreSlope:     0.20,
		},
	}
}

func TestChoosePrimaryLiveSignalOrderHardBlockFirst(t *testing.T) {
	t.Setenv("LIVE_ENABLE_MOMENTUM_IGNITE", "1")
	c := baseIgniteCandidate()
	c.ExtensionATR = 3.0
	got := choosePrimaryLiveSignal(c, time.Now().UTC())
	if got.RejectReason != "extended" {
		t.Fatalf("expected hard block to win first, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestChoosePrimaryLiveSignalOrderIgniteBeforeOthers(t *testing.T) {
	t.Setenv("LIVE_ENABLE_MOMENTUM_IGNITE", "1")
	t.Setenv("LIVE_IGNITE_MIN_SCORE", "60")
	t.Setenv("LIVE_IGNITE_MIN_SLOPE", "0.05")
	t.Setenv("LIVE_IGNITE_MIN_VOL_RATIO", "1.00")
	t.Setenv("LIVE_IGNITE_MIN_OFI_Z", "0.10")
	got := choosePrimaryLiveSignal(baseIgniteCandidate(), time.Now().UTC())
	if got.Strat != "momentum_ignite_long" {
		t.Fatalf("expected ignite to win routing order, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestChoosePrimaryLiveSignalOrderContinuationBeforeStarter(t *testing.T) {
	t.Setenv("LIVE_ENABLE_MOMENTUM_IGNITE", "0")
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "1")
	c := candidate{
		Side:        "BUY",
		LastClose:   101.0,
		SessionVWAP: 100.0,
		EMA9:        100.1,
		VolumeRatio: 1.4,
		OFIZ:        0.8,
		OFISamples:  12,
		ReclaimHold: true,
		Entry: inplay.Entry{
			Symbol:       "BTCUSDT",
			State:        inplay.StateInPlay,
			CurrentScore: 88,
			ScoreSlope:   0.12,
		},
	}
	got := choosePrimaryLiveSignal(c, time.Now().UTC())
	if got.Strat != "continuation_fast" {
		t.Fatalf("expected continuation_fast when ignite is disabled, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestChoosePrimaryLiveSignalStarterPassesSoftRejects(t *testing.T) {
	t.Setenv("LIVE_ENABLE_MOMENTUM_IGNITE", "0")
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "1")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_LONG_STARTER", "1")
	t.Setenv("LIVE_CONT_FAST_MIN_VOL_RATIO", "2.00") // force continuation fast reject by vol_ratio soft fail
	c := candidate{
		Side:        "BUY",
		LastClose:   100.5,
		SessionVWAP: 100.0,
		EMA9:        100.1,
		DayUTC24h:   24,
		VolumeUSD:   10_000_000,
		VolumeRatio: 0.75,
		OFIZ:        0.05,
		OFISamples:  12,
		Entry: inplay.Entry{
			Symbol:         "ETHUSDT",
			State:          inplay.StateInPlay,
			TimeInStateMin: 8,
			CurrentScore:   105,
			ScoreSlope:     0.20,
			Rank:           1.8,
			EntryStyle:     "pullback_long",
			Momentum:       true,
		},
	}
	got := choosePrimaryLiveSignal(c, time.Now().UTC())
	if got.Strat != "impulsive_long_starter" {
		t.Fatalf("expected starter path to pass soft rejects, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestChoosePrimaryLiveSignalNoPath(t *testing.T) {
	t.Setenv("LIVE_ENABLE_MOMENTUM_IGNITE", "0")
	t.Setenv("LIVE_ENABLE_CONTINUATION_FAST", "0")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_LONG_STARTER", "0")
	t.Setenv("LIVE_ENABLE_IMPULSIVE_SHORT_STARTER", "0")
	got := choosePrimaryLiveSignal(candidate{Side: "BUY", Entry: inplay.Entry{Symbol: "BTCUSDT"}}, time.Now().UTC())
	if got.RejectReason != "no_live_entry_path" {
		t.Fatalf("expected no_live_entry_path, got strat=%q reject=%q", got.Strat, got.RejectReason)
	}
}

func TestSessionLabelMetadataOnlyDoesNotChangeRouting(t *testing.T) {
	t.Setenv("LIVE_ENABLE_MOMENTUM_IGNITE", "1")
	t.Setenv("LIVE_IGNITE_MIN_SCORE", "60")
	t.Setenv("LIVE_IGNITE_MIN_SLOPE", "0.05")
	t.Setenv("LIVE_IGNITE_MIN_VOL_RATIO", "1.00")
	t.Setenv("LIVE_IGNITE_MIN_OFI_Z", "0.10")
	a := baseIgniteCandidate()
	b := a
	a.SessionLabel = "ASIA_DEV"
	b.SessionLabel = "NY_OPEN"
	gotA := choosePrimaryLiveSignal(a, time.Now().UTC())
	gotB := choosePrimaryLiveSignal(b, time.Now().UTC())
	if gotA.Strat != gotB.Strat {
		t.Fatalf("expected session label to be metadata only, got %q vs %q", gotA.Strat, gotB.Strat)
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
