package main

import "time"

type SimpleEntryDecision struct {
	Allowed           bool
	Side              string
	Reason            string
	MarketSnapshotTs  time.Time
	AccountSnapshotTs time.Time
}

type SimplePaperDecision struct {
	Allowed           bool
	Side              string
	Reason            string
	PullbackPreferred bool
}

func currentAccountHealth() accountHealthSummary {
	return accountHealthSummary{State: "healthy"}
}

func setLiveEntryAccountHealthProvider(fn func() accountHealthSummary) {
	_ = fn
}

func entriesBlockedByPaperAccountHealth(summary accountHealthSummary) (string, bool) {
	_ = summary
	return "", false
}

func decideSimpleEntryNow(c candidate, acct accountHealthSummary) SimpleEntryDecision {
	_ = acct
	side := "LONG"
	if c.Side == "SELL" {
		side = "SHORT"
	}
	return SimpleEntryDecision{Allowed: true, Side: side, Reason: "manual_mode"}
}

func decideSimplePaperEntryNow(c candidate, acct accountHealthSummary) SimplePaperDecision {
	_ = c
	_ = acct
	return SimplePaperDecision{Allowed: true, Side: "LONG", Reason: "manual_mode"}
}
