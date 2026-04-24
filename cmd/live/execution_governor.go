package main

import (
	"strconv"
	"strings"
	"time"

	"go-machine/adapters/aster"
)

type executionGovernorRecord struct {
	Kind            string    `json:"kind"`
	Symbol          string    `json:"symbol"`
	Side            string    `json:"side"`
	Bucket          string    `json:"bucket,omitempty"`
	SetupFamily     string    `json:"setupFamily,omitempty"`
	StrategyID      string    `json:"strategyId,omitempty"`
	WinnerLifecycle string    `json:"winnerLifecycle,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	Score           float64   `json:"score,omitempty"`
	HoldMin         float64   `json:"holdMin,omitempty"`
	Loss            bool      `json:"loss,omitempty"`
	OccurredAt      time.Time `json:"occurredAt"`
}

type executionGovernorPositionView struct {
	Symbol          string
	Side            string
	Bucket          string
	SetupFamily     string
	WinnerLifecycle string
	Score           float64
	Margin          float64
	Active          bool
}

type executionGovernorDecision struct {
	Symbol                  string
	Side                    string
	Bucket                  string
	Reason                  string
	SymbolEntriesWindowMin  int
	SymbolEntries           int
	SymbolLossesWindowMin   int
	SymbolLosses            int
	SoftExitWindowMin       int
	SoftExitCount           int
	BucketEntriesWindowMin  int
	BucketEntries           int
	BucketActiveCount       int
	BucketHasActiveWinner   bool
	SuppressingWinnerSymbol string
	SuppressingWinnerState  string
}

func executionGovernorEnabled() bool {
	return envBool("LIVE_EXEC_CAP_ENABLE", true)
}

func executionGovernorHistoryHours() time.Duration {
	hours := envInt("LIVE_EXEC_CAP_HISTORY_HOURS", 24)
	if hours <= 0 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
}

func executionGovernorHistoryMax() int {
	n := envInt("LIVE_EXEC_CAP_HISTORY_MAX", 500)
	if n <= 0 {
		n = 500
	}
	return n
}

func executionGovernorSymbolWindow() time.Duration {
	mins := envInt("LIVE_EXEC_CAP_SYMBOL_WINDOW_MIN", 180)
	if mins <= 0 {
		mins = 180
	}
	return time.Duration(mins) * time.Minute
}

func executionGovernorBucketWindow() time.Duration {
	mins := envInt("LIVE_EXEC_CAP_BUCKET_WINDOW_MIN", 60)
	if mins <= 0 {
		mins = 60
	}
	return time.Duration(mins) * time.Minute
}

func executionGovernorCandidateScore(c candidate) float64 {
	return maxFloat(c.Entry.CurrentScore, c.CombinedScore)
}

func executionGovernorPositionScore(score, conf float64) float64 {
	return maxFloat(score, conf*100.0)
}

func executionGovernorRecordScore(rec executionGovernorRecord) float64 {
	if rec.Score > 0 {
		return rec.Score
	}
	return 0
}

func executionGovernorIsMajorSymbol(symbol string) bool {
	raw := canonicalSymbolBase(symbol)
	if raw == "" {
		return false
	}
	for _, part := range strings.Split(envStr("LIVE_EXEC_CAP_MAJOR_SYMBOLS", "BTC,ETH,SOL,XRP,BNB"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), raw) {
			return true
		}
	}
	return false
}

func executionGovernorBucket(symbol, side, strat, setupFamily, regimeTag string) string {
	rawSide := normalizePositionSide(side)
	if executionGovernorIsMajorSymbol(symbol) {
		return "majors"
	}
	setup := strings.ToLower(strings.TrimSpace(setupFamily))
	strat = strings.ToLower(strings.TrimSpace(strat))
	regime := strings.ToLower(strings.TrimSpace(regimeTag))
	switch {
	case strings.HasPrefix(strat, "reset_impulse_") || strings.Contains(setup, "reset_impulse"):
		return "reset_impulse"
	case strings.Contains(setup, "reversal") || strings.Contains(strat, "reversal") || strings.Contains(strat, "flip") || strings.Contains(regime, "unwind"):
		if rawSide == "SHORT" {
			return "microcap_unwind"
		}
		return "reversal_reclaim"
	case rawSide == "SHORT":
		return "microcap_momentum_short"
	default:
		return "microcap_momentum_long"
	}
}

func executionGovernorBucketForCandidate(c candidate) string {
	return executionGovernorBucket(c.Entry.Symbol, c.Side, c.Strat, c.SetupFamily, c.Sig.RegimeTag)
}

func executionGovernorBucketForLivePosition(p *livePosition) string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.ExecBucket) != "" {
		return p.ExecBucket
	}
	return executionGovernorBucket(p.Symbol, p.Side, p.EntryReason, p.EntrySetupFamily, p.RegimeTag)
}

func executionGovernorBucketForPaperPosition(p *paperPosition) string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.ExecBucket) != "" {
		return p.ExecBucket
	}
	return executionGovernorBucket(p.Symbol, p.Side, p.EntryReason, p.EntrySetupFamily, "")
}

func normalizeExecutionGovernorRecord(rec executionGovernorRecord) executionGovernorRecord {
	rec.Kind = strings.ToUpper(strings.TrimSpace(rec.Kind))
	rec.Symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(rec.Symbol)))
	rec.Side = normalizePositionSide(rec.Side)
	rec.Bucket = strings.TrimSpace(rec.Bucket)
	rec.SetupFamily = strings.TrimSpace(rec.SetupFamily)
	rec.StrategyID = strings.TrimSpace(rec.StrategyID)
	rec.WinnerLifecycle = strings.TrimSpace(rec.WinnerLifecycle)
	rec.Reason = strings.ToUpper(strings.TrimSpace(rec.Reason))
	return rec
}

func trimExecutionGovernorRecords(records []executionGovernorRecord, now time.Time) []executionGovernorRecord {
	if len(records) == 0 {
		return nil
	}
	cutoff := now.Add(-executionGovernorHistoryHours())
	maxRecords := executionGovernorHistoryMax()
	trimmed := make([]executionGovernorRecord, 0, len(records))
	for _, rec := range records {
		rec = normalizeExecutionGovernorRecord(rec)
		if rec.Symbol == "" || rec.Side == "" || rec.OccurredAt.IsZero() {
			continue
		}
		if rec.OccurredAt.Before(cutoff) {
			continue
		}
		trimmed = append(trimmed, rec)
	}
	if len(trimmed) > maxRecords {
		trimmed = append([]executionGovernorRecord(nil), trimmed[len(trimmed)-maxRecords:]...)
	}
	return trimmed
}

func executionGovernorRecentExit(records []executionGovernorRecord, symbol, side string) (executionGovernorRecord, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	side = normalizePositionSide(side)
	var best executionGovernorRecord
	found := false
	for _, rec := range records {
		if rec.Kind != "EXIT" || rec.Symbol != symbol || rec.Side != side {
			continue
		}
		if !found || rec.OccurredAt.After(best.OccurredAt) {
			best = rec
			found = true
		}
	}
	return best, found
}

func executionGovernorEntryCount(records []executionGovernorRecord, symbol, side, bucket string, since time.Time) int {
	symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	side = normalizePositionSide(side)
	n := 0
	for _, rec := range records {
		if rec.Kind != "ENTRY" || rec.OccurredAt.Before(since) || rec.Side != side {
			continue
		}
		if symbol != "" && rec.Symbol != symbol {
			continue
		}
		if bucket != "" && rec.Bucket != bucket {
			continue
		}
		n++
	}
	return n
}

func executionGovernorSoftExitCount(records []executionGovernorRecord, symbol, side string, since time.Time) int {
	symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	side = normalizePositionSide(side)
	n := 0
	for _, rec := range records {
		if rec.Kind != "EXIT" || rec.OccurredAt.Before(since) || rec.Symbol != symbol || rec.Side != side {
			continue
		}
		if isSoftChurnExit(rec.Reason) {
			n++
		}
	}
	return n
}

func executionGovernorLossCount(records []executionGovernorRecord, symbol, side string, since time.Time) int {
	symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	side = normalizePositionSide(side)
	n := 0
	for _, rec := range records {
		if rec.Kind != "EXIT" || !rec.Loss || rec.OccurredAt.Before(since) || rec.Symbol != symbol || rec.Side != side {
			continue
		}
		n++
	}
	return n
}

func executionGovernorWinnerState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "winner_locked", "runner", "late_trail":
		return true
	default:
		return false
	}
}

func executionGovernorDifferentSetup(current, previous string) bool {
	current = strings.TrimSpace(current)
	previous = strings.TrimSpace(previous)
	return current != "" && previous != "" && !strings.EqualFold(current, previous)
}

func executionGovernorCanOverrideRecentExit(c candidate, rec executionGovernorRecord) bool {
	if hasFreshStructureReset(c) {
		return true
	}
	if executionGovernorDifferentSetup(c.SetupFamily, rec.SetupFamily) {
		return true
	}
	baseScore := executionGovernorRecordScore(rec)
	if baseScore <= 0 {
		baseScore = 88.0
	}
	delta := envFloat("LIVE_EXEC_CAP_SOFT_OVERRIDE_SCORE_DELTA", 7.5)
	return executionGovernorCandidateScore(c) >= baseScore+delta
}

func executionGovernorCanOverrideWinner(c candidate, pos executionGovernorPositionView) bool {
	if hasFreshStructureReset(c) {
		return true
	}
	if executionGovernorDifferentSetup(c.SetupFamily, pos.SetupFamily) && executionGovernorCandidateScore(c) >= pos.Score {
		return true
	}
	baseScore := pos.Score
	if baseScore <= 0 {
		baseScore = envFloat("LIVE_EXEC_CAP_WINNER_PRIORITY_BASE_SCORE", 92.0)
	}
	delta := envFloat("LIVE_EXEC_CAP_WINNER_OVERRIDE_SCORE_DELTA", 5.0)
	return executionGovernorCandidateScore(c) >= baseScore+delta
}

func executionGovernorRecentExitRejectReason(now time.Time, c candidate, rec executionGovernorRecord) string {
	if rec.OccurredAt.IsZero() || executionGovernorCanOverrideRecentExit(c, rec) {
		return ""
	}
	reason := strings.ToUpper(strings.TrimSpace(rec.Reason))
	failedCooldown := time.Duration(envInt("LIVE_EXEC_CAP_FAILED_WINNER_COOLDOWN_MIN", 90)) * time.Minute
	if failedCooldown > 0 &&
		(now.Sub(rec.OccurredAt) < failedCooldown) &&
		(strings.Contains(reason, "WINNER_REVERSION_BLOCK") || strings.EqualFold(strings.TrimSpace(rec.WinnerLifecycle), "failed")) {
		return "exec_cap_symbol_failed_winner"
	}
	quickHold := envFloat("LIVE_EXEC_CAP_QUICK_STOP_MAX_HOLD_MIN", 20.0)
	quickCooldown := time.Duration(envInt("LIVE_EXEC_CAP_QUICK_STOP_COOLDOWN_MIN", 45)) * time.Minute
	if quickCooldown > 0 &&
		now.Sub(rec.OccurredAt) < quickCooldown &&
		isStopCloseReason(reason) &&
		rec.HoldMin > 0 &&
		rec.HoldMin <= quickHold {
		return "exec_cap_symbol_quick_stop"
	}
	softCooldown := time.Duration(envInt("LIVE_EXEC_CAP_SYMBOL_SOFT_COOLDOWN_MIN", 30)) * time.Minute
	if softCooldown > 0 && now.Sub(rec.OccurredAt) < softCooldown && isSoftChurnExit(reason) {
		return "exec_cap_symbol_soft_churn"
	}
	return ""
}

func executionGovernorDecisionFromState(now time.Time, c candidate, margin float64, positions []executionGovernorPositionView, records []executionGovernorRecord) executionGovernorDecision {
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	side := normalizePositionSide(c.Side)
	decision := executionGovernorDecision{
		Symbol:                 raw,
		Side:                   side,
		Bucket:                 executionGovernorBucketForCandidate(c),
		SymbolEntriesWindowMin: int(executionGovernorSymbolWindow() / time.Minute),
		SymbolLossesWindowMin:  int(executionGovernorSymbolWindow() / time.Minute),
		SoftExitWindowMin:      envInt("LIVE_EXEC_CAP_SYMBOL_SOFT_COOLDOWN_MIN", 30),
		BucketEntriesWindowMin: int(executionGovernorBucketWindow() / time.Minute),
	}
	if !executionGovernorEnabled() || raw == "" || side == "" {
		return decision
	}
	records = trimExecutionGovernorRecords(records, now)
	candidateBucket := decision.Bucket
	symbolWindow := executionGovernorSymbolWindow()
	decision.SymbolEntries = executionGovernorEntryCount(records, raw, side, "", now.Add(-symbolWindow))
	decision.SymbolLosses = executionGovernorLossCount(records, raw, side, now.Add(-symbolWindow))
	if decision.SoftExitWindowMin > 0 {
		decision.SoftExitCount = executionGovernorSoftExitCount(records, raw, side, now.Add(-time.Duration(decision.SoftExitWindowMin)*time.Minute))
	}
	decision.BucketEntries = executionGovernorEntryCount(records, "", side, candidateBucket, now.Add(-executionGovernorBucketWindow()))
	if maxEntries := envInt("LIVE_EXEC_CAP_SYMBOL_MAX_ENTRIES", 3); maxEntries > 0 && decision.SymbolEntries >= maxEntries {
		decision.Reason = "exec_cap_symbol_window"
		return decision
	}
	if rec, ok := executionGovernorRecentExit(records, raw, side); ok {
		if reason := executionGovernorRecentExitRejectReason(now, c, rec); reason != "" {
			decision.Reason = reason
			return decision
		}
	}
	if maxLosses := envInt("LIVE_EXEC_CAP_SYMBOL_MAX_LOSSES", 2); maxLosses > 0 && !hasFreshStructureReset(c) && decision.SymbolLosses >= maxLosses {
		decision.Reason = "exec_cap_symbol_loss_cluster"
		return decision
	}
	activeBucketCount := 0
	activeBucketMargin := 0.0
	var bestWinner *executionGovernorPositionView
	for i := range positions {
		pos := positions[i]
		if !pos.Active || pos.Side != side || pos.Bucket != candidateBucket {
			continue
		}
		activeBucketCount++
		activeBucketMargin += maxFloat(pos.Margin, 0)
		if !executionGovernorWinnerState(pos.WinnerLifecycle) {
			continue
		}
		if bestWinner == nil || pos.Score > bestWinner.Score {
			bestWinner = &positions[i]
		}
	}
	decision.BucketActiveCount = activeBucketCount
	if bestWinner != nil {
		decision.BucketHasActiveWinner = true
		decision.SuppressingWinnerSymbol = bestWinner.Symbol
		decision.SuppressingWinnerState = bestWinner.WinnerLifecycle
	}
	if bestWinner != nil && !executionGovernorCanOverrideWinner(c, *bestWinner) {
		decision.Reason = "exec_cap_bucket_winner_priority"
		return decision
	}
	if maxActive := envInt("LIVE_EXEC_CAP_BUCKET_MAX_ACTIVE", 2); maxActive > 0 && activeBucketCount >= maxActive {
		decision.Reason = "exec_cap_bucket_active"
		return decision
	}
	if maxMargin := envFloat("LIVE_EXEC_CAP_BUCKET_MAX_DIRECTIONAL_MARGIN_USDT", 0); maxMargin > 0 && activeBucketMargin+maxFloat(margin, 0) > maxMargin {
		decision.Reason = "exec_cap_bucket_margin"
		return decision
	}
	if maxEntries := envInt("LIVE_EXEC_CAP_BUCKET_MAX_NEW_ENTRIES", 3); maxEntries > 0 && decision.BucketEntries >= maxEntries {
		decision.Reason = "exec_cap_bucket_window"
		return decision
	}
	return decision
}

func executionGovernorRejectReasonFromState(now time.Time, c candidate, margin float64, positions []executionGovernorPositionView, records []executionGovernorRecord) string {
	return executionGovernorDecisionFromState(now, c, margin, positions, records).Reason
}

func executionGovernorLogSide(side string) string {
	switch normalizePositionSide(side) {
	case "LONG":
		return "BUY"
	case "SHORT":
		return "SELL"
	default:
		return strings.ToUpper(strings.TrimSpace(side))
	}
}

func executionGovernorRejectLogLine(dec executionGovernorDecision) string {
	hasWinner := "false"
	if dec.BucketHasActiveWinner {
		hasWinner = "true"
	}
	return strings.TrimSpace(
		"EXEC_GOV_REJECT " +
			"symbol=" + dec.Symbol + " " +
			"side=" + executionGovernorLogSide(dec.Side) + " " +
			"bucket=" + firstNonEmpty(dec.Bucket, "unknown") + " " +
			"reason=" + dec.Reason + " " +
			"symbol_entries_" + strconv.Itoa(dec.SymbolEntriesWindowMin) + "m=" + strconv.Itoa(dec.SymbolEntries) + " " +
			"symbol_losses_" + strconv.Itoa(dec.SymbolLossesWindowMin) + "m=" + strconv.Itoa(dec.SymbolLosses) + " " +
			"soft_exits_" + strconv.Itoa(dec.SoftExitWindowMin) + "m=" + strconv.Itoa(dec.SoftExitCount) + " " +
			"bucket_active=" + strconv.Itoa(dec.BucketActiveCount) + " " +
			"bucket_entries_" + strconv.Itoa(dec.BucketEntriesWindowMin) + "m=" + strconv.Itoa(dec.BucketEntries) + " " +
			"bucket_has_active_winner=" + hasWinner + " " +
			"suppressing_winner_symbol=" + firstNonEmpty(dec.SuppressingWinnerSymbol, "-") + " " +
			"suppressing_winner_state=" + firstNonEmpty(dec.SuppressingWinnerState, "-"))
}

func (m *liveExecManager) governorPositionViews() []executionGovernorPositionView {
	if m == nil || len(m.positions) == 0 {
		return nil
	}
	out := make([]executionGovernorPositionView, 0, len(m.positions))
	for _, p := range m.positions {
		if p == nil {
			continue
		}
		out = append(out, executionGovernorPositionView{
			Symbol:          strings.ToUpper(strings.TrimSpace(aster.RawSymbol(p.Symbol))),
			Side:            normalizePositionSide(p.Side),
			Bucket:          executionGovernorBucketForLivePosition(p),
			SetupFamily:     strings.TrimSpace(p.EntrySetupFamily),
			WinnerLifecycle: strings.TrimSpace(p.WinnerLifecycle),
			Score:           executionGovernorPositionScore(p.CombinedScore, p.EntryConf),
			Margin:          maxFloat(p.DeployedMargin, p.Margin),
			Active:          m.isActive(p) && p.RemainingQty > 0,
		})
	}
	return out
}

func (p *paperTrader) governorPositionViews() []executionGovernorPositionView {
	if p == nil || len(p.positions) == 0 {
		return nil
	}
	out := make([]executionGovernorPositionView, 0, len(p.positions))
	for _, pos := range p.positions {
		if pos == nil {
			continue
		}
		out = append(out, executionGovernorPositionView{
			Symbol:          strings.ToUpper(strings.TrimSpace(aster.RawSymbol(pos.Symbol))),
			Side:            normalizePositionSide(pos.Side),
			Bucket:          executionGovernorBucketForPaperPosition(pos),
			SetupFamily:     strings.TrimSpace(pos.EntrySetupFamily),
			WinnerLifecycle: strings.TrimSpace(pos.WinnerLifecycle),
			Score:           executionGovernorPositionScore(pos.CombinedScore, pos.EntryConf),
			Margin:          maxFloat(pos.Margin, 0),
			Active:          pos.Qty > 0,
		})
	}
	return out
}

func (m *liveExecManager) executionGovernorDecision(now time.Time, c candidate, margin float64) executionGovernorDecision {
	if m == nil {
		return executionGovernorDecision{}
	}
	return executionGovernorDecisionFromState(now, c, margin, m.governorPositionViews(), m.governorRecords)
}

func (m *liveExecManager) executionGovernorRejectReason(now time.Time, c candidate, margin float64) string {
	return m.executionGovernorDecision(now, c, margin).Reason
}

func (p *paperTrader) executionGovernorDecision(now time.Time, c candidate, margin float64) executionGovernorDecision {
	if p == nil {
		return executionGovernorDecision{}
	}
	return executionGovernorDecisionFromState(now, c, margin, p.governorPositionViews(), p.governorRecords)
}

func (p *paperTrader) executionGovernorRejectReason(now time.Time, c candidate, margin float64) string {
	return p.executionGovernorDecision(now, c, margin).Reason
}

func appendExecutionGovernorRecord(records []executionGovernorRecord, rec executionGovernorRecord, now time.Time) []executionGovernorRecord {
	rec = normalizeExecutionGovernorRecord(rec)
	if rec.Symbol == "" || rec.Side == "" {
		return trimExecutionGovernorRecords(records, now)
	}
	if rec.OccurredAt.IsZero() {
		rec.OccurredAt = now
	}
	records = append(records, rec)
	return trimExecutionGovernorRecords(records, now)
}

func (m *liveExecManager) recordExecutionGovernorEntry(now time.Time, c candidate) {
	if m == nil {
		return
	}
	m.governorRecords = appendExecutionGovernorRecord(m.governorRecords, executionGovernorRecord{
		Kind:        "ENTRY",
		Symbol:      c.Entry.Symbol,
		Side:        c.Side,
		Bucket:      executionGovernorBucketForCandidate(c),
		SetupFamily: c.SetupFamily,
		StrategyID:  firstNonEmpty(strings.TrimSpace(c.StrategyID), strings.TrimSpace(c.Strat)),
		Score:       executionGovernorCandidateScore(c),
		OccurredAt:  now,
	}, now)
}

func (p *paperTrader) recordExecutionGovernorEntry(now time.Time, c candidate) {
	if p == nil {
		return
	}
	p.governorRecords = appendExecutionGovernorRecord(p.governorRecords, executionGovernorRecord{
		Kind:        "ENTRY",
		Symbol:      c.Entry.Symbol,
		Side:        c.Side,
		Bucket:      executionGovernorBucketForCandidate(c),
		SetupFamily: c.SetupFamily,
		StrategyID:  firstNonEmpty(strings.TrimSpace(c.StrategyID), strings.TrimSpace(c.Strat)),
		Score:       executionGovernorCandidateScore(c),
		OccurredAt:  now,
	}, now)
}

func (m *liveExecManager) recordExecutionGovernorExit(now time.Time, p *livePosition, reason string) {
	if m == nil || p == nil {
		return
	}
	m.governorRecords = appendExecutionGovernorRecord(m.governorRecords, executionGovernorRecord{
		Kind:            "EXIT",
		Symbol:          p.Symbol,
		Side:            p.Side,
		Bucket:          executionGovernorBucketForLivePosition(p),
		SetupFamily:     p.EntrySetupFamily,
		StrategyID:      firstNonEmpty(strings.TrimSpace(p.EntryStrategyID), strings.TrimSpace(p.EntryReason)),
		WinnerLifecycle: p.WinnerLifecycle,
		Reason:          reason,
		Score:           executionGovernorPositionScore(p.CombinedScore, p.EntryConf),
		HoldMin:         maxFloat(0, now.Sub(p.CreatedAt).Minutes()),
		Loss:            p.RealizedPnL < 0,
		OccurredAt:      now,
	}, now)
}

func (p *paperTrader) recordExecutionGovernorExit(now time.Time, pos *paperPosition, reason string, net float64) {
	if p == nil || pos == nil {
		return
	}
	p.governorRecords = appendExecutionGovernorRecord(p.governorRecords, executionGovernorRecord{
		Kind:            "EXIT",
		Symbol:          pos.Symbol,
		Side:            pos.Side,
		Bucket:          executionGovernorBucketForPaperPosition(pos),
		SetupFamily:     pos.EntrySetupFamily,
		StrategyID:      firstNonEmpty(strings.TrimSpace(pos.EntryStrategyID), strings.TrimSpace(pos.EntryReason)),
		WinnerLifecycle: pos.WinnerLifecycle,
		Reason:          reason,
		Score:           executionGovernorPositionScore(pos.CombinedScore, pos.EntryConf),
		HoldMin:         maxFloat(0, now.Sub(pos.OpenedAt).Minutes()),
		Loss:            net < 0,
		OccurredAt:      now,
	}, now)
}
