package main

import "time"

type executionGovernorRecord struct {
	OccurredAt time.Time `json:"occurredAt"`
}

type executionGovernorDecision struct {
	Reason string
}

func executionGovernorBucketForCandidate(c candidate) string {
	_ = c
	return ""
}

func executionGovernorRejectLogLine(dec executionGovernorDecision) string {
	_ = dec
	return ""
}

func (m *liveExecManager) executionGovernorDecision(now time.Time, c candidate, margin float64) executionGovernorDecision {
	_ = now
	_ = c
	_ = margin
	return executionGovernorDecision{}
}

func (m *liveExecManager) recordExecutionGovernorEntry(now time.Time, c candidate) {
	_ = now
	_ = c
}

func (m *liveExecManager) recordExecutionGovernorClose(now time.Time, p *livePosition) {
	_ = now
	_ = p
}

func (p *paperTrader) executionGovernorDecision(now time.Time, c candidate, margin float64) executionGovernorDecision {
	_ = now
	_ = c
	_ = margin
	return executionGovernorDecision{}
}

func (p *paperTrader) recordExecutionGovernorEntry(now time.Time, c candidate) {
	_ = now
	_ = c
}

func (p *paperTrader) recordExecutionGovernorClose(now time.Time, pos *paperPosition) {
	_ = now
	_ = pos
}

func (p *paperTrader) recordExecutionGovernorExit(now time.Time, pos *paperPosition, reason string, pnl ...float64) {
	_ = now
	_ = pos
	_ = reason
	_ = pnl
}

func (m *liveExecManager) recordExecutionGovernorExit(now time.Time, p *livePosition, reason string, pnl ...float64) {
	_ = now
	_ = p
	_ = reason
	_ = pnl
}

func trimExecutionGovernorRecords(records []executionGovernorRecord, now time.Time) []executionGovernorRecord {
	_ = now
	return records
}
