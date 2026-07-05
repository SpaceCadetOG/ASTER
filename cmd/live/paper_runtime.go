package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"go-machine/adapters/aster"
	flowfeed "go-machine/internal/flow"
	"go-machine/internal/inplay"
	"go-machine/internal/risk"
	"go-machine/internal/stats"
	"go-machine/internal/strategies"
)

type runtimeOperatingMode string

const (
	runtimeModePaper runtimeOperatingMode = "paper"
	runtimeModeLive  runtimeOperatingMode = "live"
)

func parseRuntimeOperatingMode(raw string) runtimeOperatingMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(runtimeModePaper):
		return runtimeModePaper
	case string(runtimeModeLive), "live_auto", "manual_only":
		return runtimeModeLive
	default:
		return ""
	}
}

func surfacedRuntimeMode(mode runtimeOperatingMode) string {
	return string(mode)
}

func surfacedPaperLabel(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "paper"
	}
	return value
}

func surfacedPaperState(raw string) string {
	return strings.TrimSpace(raw)
}

func selectTopRuntimeCandidate(cands []candidate) (candidate, bool) {
	if len(cands) == 0 {
		return candidate{}, false
	}
	ordered := append([]candidate(nil), cands...)
	sortCandidatesForRuntime(ordered)
	for _, cand := range ordered {
		if isExecutableStrategy(cand.Strat) {
			return cand, true
		}
	}
	return ordered[0], true
}

func sortCandidatesForRuntime(cands []candidate) {
	sort.SliceStable(cands, func(i, j int) bool {
		leftExec := isExecutableStrategy(cands[i].Strat)
		rightExec := isExecutableStrategy(cands[j].Strat)
		if leftExec != rightExec {
			return leftExec
		}
		if cands[i].FinalRank != cands[j].FinalRank {
			return cands[i].FinalRank > cands[j].FinalRank
		}
		if cands[i].Entry.CurrentScore == cands[j].Entry.CurrentScore {
			if cands[i].Entry.ScoreSlope == cands[j].Entry.ScoreSlope {
				return cands[i].Entry.Rank < cands[j].Entry.Rank
			}
			return cands[i].Entry.ScoreSlope > cands[j].Entry.ScoreSlope
		}
		return cands[i].Entry.CurrentScore > cands[j].Entry.CurrentScore
	})
}

type paperDecisionCtx struct {
	Now                 time.Time
	LocalMaintNow       time.Time
	Candidate           candidate
	MetaBySymbol        map[string]symbolMeta
	EntryDepth          map[string]aster.OrderBook
	Paper               *paperTrader
	CurrentEntries      map[string]inplay.Entry
	SessionChurns       map[string]*sessionChurn
	RiskShell           risk.Config
	RiskFallbackStopPct float64
	RiskHoldHours       float64
	LeverageMode        string
	LeverageFixed       int
	LeverageMin         int
	MaxLeverage         int
	EffectiveReserve    float64
	EffectiveMargin     float64
	AvailableUSDT       float64
	OBFilterEnable      bool
	OBLevels            int
	OBImbMin            float64
	OBMaxSpreadBps      float64
	MaxOpenPos          int
	MaxOpenPerSide      int
	EventLog            *stats.EventLogger
}

func buildPaperExecutionDecision(ctx paperDecisionCtx) strategies.ExecutionDecision {
	preflightReason := paperBaselineHardRejectReason(ctx)
	return buildSharedRuntimeDecision(sharedRuntimeDecisionContext{
		Candidate:             ctx.Candidate,
		MetaBySymbol:          ctx.MetaBySymbol,
		RiskShell:             ctx.RiskShell,
		RiskFallbackStopPct:   ctx.RiskFallbackStopPct,
		RiskHoldHours:         ctx.RiskHoldHours,
		LeverageMode:          ctx.LeverageMode,
		LeverageFixed:         ctx.LeverageFixed,
		LeverageMin:           ctx.LeverageMin,
		MaxLeverage:           ctx.MaxLeverage,
		EffectiveMargin:       ctx.EffectiveMargin,
		PreflightRejectReason: preflightReason,
		PreflightSource:       "paper",
	}).ExecutionDecision
}

func paperRiskDecision(ctx paperDecisionCtx) risk.Decision {
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(ctx.Candidate.Entry.Symbol)))
	meta := ctx.MetaBySymbol[raw]
	entryPx := ctx.Candidate.Sig.Entry
	if entryPx <= 0 {
		entryPx = meta.LastPrice
	}
	stopPx := ctx.Candidate.Sig.Stop
	if stopPx <= 0 && entryPx > 0 {
		d := ctx.RiskFallbackStopPct / 100.0
		if strings.EqualFold(ctx.Candidate.Side, "BUY") {
			stopPx = entryPx * (1 - d)
		} else {
			stopPx = entryPx * (1 + d)
		}
	}
	lev := computeLeverage(ctx.Candidate, ctx.LeverageMode, ctx.LeverageFixed, ctx.LeverageMin, ctx.MaxLeverage)
	topBookUSD := maxFloat(ctx.Candidate.DepthBid, ctx.Candidate.DepthAsk)
	dec := risk.Approve(ctx.RiskShell, risk.Input{
		Symbol:            raw,
		Session:           strings.ToUpper(strings.TrimSpace(ctx.Candidate.SessionLabel)),
		Side:              strings.ToUpper(strings.TrimSpace(ctx.Candidate.Side)),
		Entry:             entryPx,
		Stop:              stopPx,
		Leverage:          float64(maxInt(1, lev)),
		NotionalUSD:       ctx.EffectiveMargin * float64(maxInt(1, lev)),
		FundingRate:       meta.FundingRate,
		HoldHours:         ctx.RiskHoldHours,
		SpreadBps:         ctx.Candidate.SpreadBps,
		TopBookUSD:        topBookUSD,
		BookImbalance:     ctx.Candidate.BookImbalance,
		RecentSlippageBps: 0,
		VenueHealthy:      meta.LastPrice > 0,
	})
	return softenPaperRiskDecision(dec)
}

func softenPaperRiskDecision(dec risk.Decision) risk.Decision {
	switch strings.TrimSpace(dec.RejectReason) {
	case "spread_too_wide",
		"depth_too_thin",
		"depth_imbalance_thin",
		"venue_unhealthy",
		"slippage_anomaly",
		"liq_buffer_violation",
		"funding_too_expensive",
		"hourly_entry_cap",
		"symbol_lockout":
		dec.Approved = true
		dec.RejectReason = ""
	}
	return dec
}

func paperPreflightVerdict(ctx paperDecisionCtx) strategies.PreflightVerdict {
	reasons := []string{}
	if reason := paperBaselineHardRejectReason(ctx); reason != "" {
		reasons = append(reasons, reason)
	}
	quality := buildPaperQualityLogOnly(ctx.Candidate)
	approved := len(reasons) == 0
	verdict := strategies.PreflightVerdict{
		Checked:  true,
		Approved: approved,
		Source:   "paper",
		Reasons:  reasons,
		Quality:  quality,
	}
	if !approved {
		verdict.Reason = reasons[0]
	}
	return verdict
}

func paperBaselineHardRejectReason(ctx paperDecisionCtx) string {
	p := ctx.Paper
	if p == nil || !p.enabled {
		return "paper_disabled"
	}
	raw := strings.ToUpper(aster.RawSymbol(ctx.Candidate.Entry.Symbol))
	if raw == "" {
		return "empty_symbol"
	}
	if meta := ctx.MetaBySymbol[raw]; meta.LastPrice <= 0 {
		return "paper_price_unavailable"
	}
	if paperHasOpenSymbol(p, raw) {
		return "symbol_already_open"
	}
	return ""
}

func paperHasOpenSymbol(p *paperTrader, raw string) bool {
	if p == nil {
		return false
	}
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "" {
		return false
	}
	_, exists := p.positions[raw]
	return exists
}

func buildPaperQualityLogOnly(c candidate) strategies.EntryQualityAccumulator {
	acc := buildEntryQualityAccumulator(c, nil)
	acc.BlockReason = ""
	acc.HardBlockReasons = nil
	return acc
}

type recentProofOutcomeStats struct {
	NoWeakCount     int
	GoodStrongCount int
}

type proofOutcomeMemory struct {
	mu    sync.RWMutex
	byDay map[string]map[string]recentProofOutcomeStats
}

var runtimeProofMemory = &proofOutcomeMemory{byDay: map[string]map[string]recentProofOutcomeStats{}}

func proofMemoryDay(ts time.Time) string {
	return ts.UTC().Format("2006-01-02")
}

func proofMemoryKey(symbol, side, strategy, setup string) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "|" +
		strings.ToUpper(strings.TrimSpace(side)) + "|" +
		strings.ToLower(strings.TrimSpace(strategy)) + "|" +
		strings.ToLower(strings.TrimSpace(setup))
}

func candidateProofMemoryKey(c candidate) string {
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	return proofMemoryKey(raw, c.Side, firstNonEmpty(strings.TrimSpace(c.StrategyID), strings.TrimSpace(c.Strat)), c.SetupFamily)
}

func rebuildRuntimeProofMemory(ledger map[string]paperClosedTradeRecord) {
	runtimeProofMemory.mu.Lock()
	defer runtimeProofMemory.mu.Unlock()
	runtimeProofMemory.byDay = map[string]map[string]recentProofOutcomeStats{}
	for _, rec := range ledger {
		recordRuntimeProofMemoryLocked(rec)
	}
}

func recordRuntimeProofMemory(rec paperClosedTradeRecord) {
	runtimeProofMemory.mu.Lock()
	defer runtimeProofMemory.mu.Unlock()
	recordRuntimeProofMemoryLocked(rec)
}

func recordRuntimeProofMemoryLocked(rec paperClosedTradeRecord) {
	day := proofMemoryDay(rec.Exit.ExitTs)
	if runtimeProofMemory.byDay[day] == nil {
		runtimeProofMemory.byDay[day] = map[string]recentProofOutcomeStats{}
	}
	key := proofMemoryKey(rec.Symbol, rec.Side, rec.Identity.Strategy, rec.Identity.SetupFamily)
	stats := runtimeProofMemory.byDay[day][key]
	if weakEntryOutcome(rec.Exit.EntryOutcomeLabel) {
		stats.NoWeakCount++
	} else {
		stats.GoodStrongCount++
	}
	runtimeProofMemory.byDay[day][key] = stats
}

func recentRuntimeProofStats(now time.Time, c candidate) recentProofOutcomeStats {
	runtimeProofMemory.mu.RLock()
	defer runtimeProofMemory.mu.RUnlock()
	if runtimeProofMemory.byDay == nil {
		return recentProofOutcomeStats{}
	}
	return runtimeProofMemory.byDay[proofMemoryDay(now)][candidateProofMemoryKey(c)]
}

func hasEntryScoreBreakdownData(b EntryScoreBreakdown) bool {
	return b.FinalScore > 0 || b.TrendScore > 0 || b.LocationScore > 0 || b.TriggerScore > 0 || b.FlowScore > 0 || b.RiskRewardScore > 0 || b.PenaltyScore != 0 || len(b.PenaltyReasons) > 0 ||
		strings.TrimSpace(b.TrendLabel) != "" || strings.TrimSpace(b.LocationLabel) != "" || strings.TrimSpace(b.TriggerLabel) != "" || strings.TrimSpace(b.FlowLabel) != "" || strings.TrimSpace(b.RiskRewardLabel) != ""
}

func normalizeEntryScoreBreakdown(b EntryScoreBreakdown) EntryScoreBreakdown {
	if b.FinalScore > 0 {
		return b
	}
	componentTotal := b.TrendScore + b.LocationScore + b.TriggerScore + b.FlowScore + b.RiskRewardScore + b.PenaltyScore
	if componentTotal > 0 || (b.PenaltyScore < 0 && (b.TrendScore > 0 || b.LocationScore > 0 || b.TriggerScore > 0 || b.FlowScore > 0 || b.RiskRewardScore > 0)) {
		b.FinalScore = clamp(componentTotal, 0, 100)
	}
	return b
}

func candidateHasProofInputs(c candidate) bool {
	return hasEntryScoreBreakdownData(c.EntryScoreBreakdown)
}

func resolvedEntryScoreBreakdown(c candidate) EntryScoreBreakdown {
	breakdown := normalizeEntryScoreBreakdown(c.EntryScoreBreakdown)
	if hasEntryScoreBreakdownData(breakdown) {
		return breakdown
	}
	breakdown = normalizeEntryScoreBreakdown(scoreEntryBreakdown(c))
	if hasEntryScoreBreakdownData(breakdown) {
		return breakdown
	}
	if c.CombinedScore > 0 {
		breakdown.FinalScore = clamp(c.CombinedScore*100.0, 0, 100)
	}
	return breakdown
}

func candidateProofRoomR(c candidate) float64 {
	entry := firstPositive(c.Sig.Entry, c.LastClose)
	stop := c.Sig.Stop
	if entry <= 0 || stop <= 0 || entry == stop {
		return 0
	}
	risk := math.Abs(entry - stop)
	if risk <= 0 {
		return 0
	}
	targets := []float64{}
	if c.Sig.TP1 > 0 {
		targets = append(targets, c.Sig.TP1)
	}
	if c.Sig.VPTargetLevel > 0 {
		targets = append(targets, c.Sig.VPTargetLevel)
	}
	best := 0.0
	for _, target := range targets {
		move := 0.0
		if strings.EqualFold(c.Side, "BUY") {
			move = target - entry
		} else {
			move = entry - target
		}
		if move <= 0 {
			continue
		}
		if best <= 0 || move < best {
			best = move
		}
	}
	if best <= 0 {
		return 0
	}
	return best / risk
}

func executionProofRejectReason(now time.Time, c candidate) string {
	if !isExecutableStrategy(firstNonEmpty(strings.TrimSpace(c.StrategyID), strings.TrimSpace(c.Strat))) {
		return ""
	}
	if c.Sig.Entry <= 0 || c.Sig.Stop <= 0 || c.Sig.TP1 <= 0 {
		return ""
	}
	breakdown := resolvedEntryScoreBreakdown(c)
	if !hasEntryScoreBreakdownData(breakdown) {
		return ""
	}
	quality := buildEntryQualityAccumulator(c, nil)
	return strings.TrimSpace(quality.BlockReason)
}

func emitEntryProofCheck(now time.Time, c candidate, decision strategies.ExecutionDecision, executionPath string) {
	breakdown := resolvedEntryScoreBreakdown(c)
	proof := projectedProofOutcome(c)
	proofRoom := candidateProofRoomR(c)
	stats := recentRuntimeProofStats(now, c)
	fmt.Printf(
		"ENTRY_PROOF_CHECK symbol=%s side=%s strategy=%s setup=%s final_score=%.1f projected_proof=%s proof_room_r=%.2f trigger_score=%.1f flow_score=%.1f location_score=%.1f trend_score=%.1f recent_symbol_setup_no_weak_count=%d quality_block_reason=%s decision_approved=%t rejects=%s execution_path=%s\n",
		strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol))),
		strings.ToUpper(strings.TrimSpace(c.Side)),
		firstNonEmpty(strings.TrimSpace(c.StrategyID), strings.TrimSpace(c.Strat), "unknown"),
		firstNonEmpty(strings.TrimSpace(c.SetupFamily), "unknown"),
		breakdown.FinalScore,
		proof,
		proofRoom,
		breakdown.TriggerScore,
		breakdown.FlowScore,
		breakdown.LocationScore,
		breakdown.TrendScore,
		stats.NoWeakCount,
		firstNonEmpty(strings.TrimSpace(decision.Quality.BlockReason), "none"),
		decision.Approved,
		strings.Join(decision.Rejects, "|"),
		firstNonEmpty(strings.TrimSpace(executionPath), "unknown"),
	)
}

type paperEnterFunc func(time.Time, candidate, float64, float64, int, map[string]symbolMeta, map[string]aster.OrderBook, map[string]inplay.Entry) (*paperPosition, error)

type paperDispatchHooks struct {
	Paper paperEnterFunc
	Live  func() error
}

type paperDispatchResult struct {
	Attempted    bool
	Entered      bool
	RejectReason string
	Position     *paperPosition
}

type shortPhase2Context struct {
	Bucket                string
	FilterReason          string
	RequireConfirmation   string
	DirectShortAllowed    bool
	SizeMultiplier        float64
	Pct24hAtEntry         float64
	Pct4hAtEntry          float64
	Pct1hAtEntry          float64
	BounceFromLocalLowPct float64
	FailedBounceConfirmed bool
	PostPumpBreakdown     bool
	LateChaseBlocked      bool
}

func dispatchPaperDecision(mode runtimeOperatingMode, now time.Time, decision strategies.ExecutionDecision, c candidate, entryBps, margin float64, leverage int, meta map[string]symbolMeta, depth map[string]aster.OrderBook, current map[string]inplay.Entry, hooks paperDispatchHooks) paperDispatchResult {
	if mode != runtimeModePaper {
		return paperDispatchResult{}
	}
	emitEntryProofCheck(now, c, decision, "paper_dispatch")
	if !decision.Approved {
		return paperDispatchResult{RejectReason: firstNonEmpty(decision.RejectReason, "not_approved")}
	}
	if hooks.Paper == nil {
		return paperDispatchResult{RejectReason: "paper_entry_unavailable"}
	}
	pos, err := hooks.Paper(now, c, entryBps, margin, leverage, meta, depth, current)
	if err != nil {
		return paperDispatchResult{
			Attempted:    true,
			RejectReason: strings.TrimSpace(err.Error()),
		}
	}
	return paperDispatchResult{
		Attempted: true,
		Entered:   pos != nil,
		Position:  pos,
	}
}

func shortPhase2ContextForCandidate(c candidate) shortPhase2Context {
	ctx := shortPhase2Context{
		DirectShortAllowed: true,
		SizeMultiplier:     1.0,
		Pct24hAtEntry:      c.DayUTC24h,
		Pct4hAtEntry:       c.UTC4hPct,
		Pct1hAtEntry:       c.UTC1hPct,
	}
	if !strings.EqualFold(c.Side, "SELL") {
		return ctx
	}
	ctx.BounceFromLocalLowPct = maxFloat(0, c.Entry.DrawupFromTroughPct)
	postPumpFreshWindow := c.DayUTC24h > 60 && c.UTC4hPct < 0 && c.UTC4hPct > -15 && c.UTC1hPct < -2 && c.UTC1hPct > -6
	switch {
	case c.DayUTC24h > 60 && c.UTC4hPct < -15 && c.UTC1hPct < -5:
		ctx.Bucket = "post_pump_breakdown"
		ctx.FilterReason = "short_block_post_pump_chase"
		ctx.RequireConfirmation = "failed_bounce"
		ctx.DirectShortAllowed = false
		ctx.SizeMultiplier = 0
		ctx.PostPumpBreakdown = true
	case postPumpFreshWindow:
		ctx.Bucket = "post_pump_fresh_breakdown"
		ctx.FilterReason = "short_post_pump_structure_wait"
		ctx.RequireConfirmation = "just_lost_vwap_or_structure"
		ctx.DirectShortAllowed = false
		ctx.SizeMultiplier = 0
		ctx.PostPumpBreakdown = true
	case c.DayUTC24h < -20 && c.UTC4hPct < -8 && c.UTC1hPct < -3:
		ctx.Bucket = "late_chase_short"
		ctx.FilterReason = "short_block_late_chase"
		ctx.RequireConfirmation = "failed_bounce"
		ctx.DirectShortAllowed = false
		ctx.SizeMultiplier = 0
		ctx.LateChaseBlocked = true
	case c.DayUTC24h < 0 && c.DayUTC24h > -20 && c.UTC4hPct < 0 && c.UTC1hPct < 0 && c.UTC1hPct > -3:
		ctx.Bucket = "fresh_breakdown_short"
		ctx.FilterReason = "short_allowed_fresh_breakdown"
		ctx.DirectShortAllowed = true
		ctx.SizeMultiplier = 1.0
	default:
		ctx.Bucket = "unclassified_short"
		ctx.FilterReason = "short_context_unclassified"
	}
	if postPumpFreshWindow && shortJustLostVWAPOrStructure(c) {
		ctx.DirectShortAllowed = true
		ctx.SizeMultiplier = 0.75
		ctx.FilterReason = "short_allowed_post_pump_breakdown"
		ctx.RequireConfirmation = ""
	}
	if (ctx.Bucket == "late_chase_short" || ctx.Bucket == "post_pump_breakdown") && shortFailedBounceConfirmed(c, ctx) {
		ctx.Bucket = "failed_bounce_short"
		ctx.FilterReason = "short_allowed_failed_bounce"
		ctx.RequireConfirmation = ""
		ctx.DirectShortAllowed = true
		ctx.SizeMultiplier = 1.0
		ctx.FailedBounceConfirmed = true
	}
	return ctx
}

func annotatePaperPositionFromDecision(pos *paperPosition, c candidate, decision strategies.ExecutionDecision) {
	if pos == nil {
		return
	}
	pos.EntryMode = string(runtimeModePaper)
	pos.EntryConfluenceScore = decision.Signal.ConfluenceScore.TotalScore
	pos.EntrySignalReasons = append([]string(nil), decision.Signal.Reasons...)
	pos.EntrySignalSources = append([]string(nil), decision.Signal.SignalSource...)
	pos.EntryDecisionReasons = append([]string(nil), decision.Signal.Reasons...)
	pos.EntryDecisionRejects = append([]string(nil), decision.Rejects...)
	pos.EntryDecisionReject = decision.RejectReason
	pos.EntryDecisionProof = append([]string(nil), decision.Provenance...)
	if pos.EntrySetupFamily == "" {
		pos.EntrySetupFamily = c.SetupFamily
	}
}

func emitPaperDecisionEvent(log *stats.EventLogger, now time.Time, c candidate, decision strategies.ExecutionDecision) {
	if log == nil {
		return
	}
	allow := decision.Approved
	reasons := append([]string(nil), decision.Rejects...)
	if allow {
		reasons = append(reasons, decision.Signal.Reasons...)
	}
	log.Emit(stats.Event{
		Timestamp:       now,
		Type:            "ENTRY_DECISION",
		Simulated:       true,
		Symbol:          strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol))),
		Side:            strings.ToUpper(strings.TrimSpace(c.Side)),
		Source:          "paper",
		Mode:            string(runtimeModePaper),
		TF:              "1m",
		Strategy:        firstNonEmpty(strings.TrimSpace(decision.Signal.Name), strings.TrimSpace(c.Strat)),
		SetupFamily:     c.SetupFamily,
		Grade:           c.Entry.CurrentGrade,
		State:           string(c.Entry.State),
		TriggerState:    c.TriggerState,
		ExitProfile:     c.ExitProfile,
		ConfluenceScore: decision.Signal.ConfluenceScore.TotalScore,
		StrategyReasons: append([]string(nil), decision.Signal.Reasons...),
		StrategySources: append([]string(nil), decision.Signal.SignalSource...),
		Score:           c.Entry.CurrentScore,
		Slope:           c.Entry.ScoreSlope,
		VolumeRatio:     c.VolumeRatio,
		EntryPx:         decision.Entry,
		Discovery:       c.DiscoveryScore,
		Trigger:         c.TriggerScore,
		Execution:       c.ExecutionScore,
		Combined:        c.CombinedScore,
		StopDistPct:     decisionStopDistancePct(decision),
		Reason:          firstNonEmpty(decision.RejectReason, decision.Signal.Name, c.Strat),
		GateAllow:       &allow,
		GateReasons:     reasons,
	})
}

func emitPaperPositionOpenEvent(log *stats.EventLogger, now time.Time, c candidate, pos *paperPosition, decision strategies.ExecutionDecision) {
	if log == nil || pos == nil {
		return
	}
	allow := true
	log.Emit(stats.Event{
		Timestamp:       now,
		Type:            "POSITION_OPEN",
		Simulated:       true,
		Symbol:          strings.ToUpper(strings.TrimSpace(aster.RawSymbol(pos.Symbol))),
		Side:            strings.ToUpper(strings.TrimSpace(pos.Side)),
		Source:          "paper",
		Mode:            pos.EntryMode,
		TF:              "1m",
		Strategy:        firstNonEmpty(strings.TrimSpace(pos.EntryStrategyID), strings.TrimSpace(pos.EntryReason)),
		SetupFamily:     pos.EntrySetupFamily,
		Grade:           pos.EntryGrade,
		State:           string(pos.EntryState),
		TriggerState:    pos.EntryTrigger,
		ExitProfile:     pos.ExitProfile,
		ConfluenceScore: pos.EntryConfluenceScore,
		StrategyReasons: append([]string(nil), pos.EntrySignalReasons...),
		StrategySources: append([]string(nil), pos.EntrySignalSources...),
		Score:           c.Entry.CurrentScore,
		Slope:           c.Entry.ScoreSlope,
		VolumeRatio:     c.VolumeRatio,
		EntryPx:         pos.Entry,
		Discovery:       pos.DiscoveryScore,
		Trigger:         pos.TriggerScore,
		Execution:       pos.ExecutionScore,
		Combined:        pos.CombinedScore,
		StopDistPct:     pos.StopDistancePct,
		Reason:          firstNonEmpty(decision.Signal.Name, pos.EntryReason),
		GateAllow:       &allow,
		GateReasons:     append([]string(nil), decision.Provenance...),
	})
}

func decisionStopDistancePct(decision strategies.ExecutionDecision) float64 {
	if decision.Entry <= 0 || decision.Stop <= 0 {
		return 0
	}
	return abs(decision.Entry-decision.Stop) / decision.Entry * 100.0
}

func runPaperLifecycle(now time.Time, paper *paperTrader, meta map[string]symbolMeta, depth map[string]aster.OrderBook, longCurrent, shortCurrent map[string]inplay.Entry, mom map[string]momentumView, flow map[string]flowMetrics, ext map[string]flowfeed.ExternalSignal) {
	if paper == nil || !paper.enabled {
		return
	}
	paper.ApplyFunding(now, meta, longCurrent, shortCurrent)
	paper.ApplyMomentumExit(now, mom, meta, depth, ext)
	paper.CheckExit(now, meta, depth, longCurrent, shortCurrent, mom, flow)
}

func applyPaperDecisionStatus(st *liveStatus, decision strategies.ExecutionDecision, dispatch paperDispatchResult) {
	if st == nil {
		return
	}
	st.Mode = surfacedRuntimeMode(runtimeModePaper)
	switch {
	case dispatch.Entered:
		st.ModeState = "paper_entered"
		st.TopDecision = "paper_entered"
		st.TopDecisionWhy = firstNonEmpty(decision.Signal.Name, "paper_entry") + " | stage=opened"
		st.TopRejectReason = ""
	case dispatch.Attempted && dispatch.RejectReason != "":
		st.ModeState = "paper_candidate_rejected"
		st.TopDecision = "paper_candidate_rejected"
		st.TopDecisionWhy = "stage=maybe_enter_failed reason=" + dispatch.RejectReason
		st.TopRejectReason = dispatch.RejectReason
	case !decision.Approved:
		st.ModeState = "paper_candidate_rejected"
		st.TopDecision = "paper_candidate_rejected"
		reason := paperDecisionRejectReason(candidate{}, decision)
		st.TopDecisionWhy = "stage=decision_rejected reason=" + reason
		st.TopRejectReason = reason
	case dispatch.Attempted:
		st.ModeState = "paper_entry_attempted"
		st.TopDecision = "paper_entry_attempted"
		st.TopDecisionWhy = firstNonEmpty(decision.Signal.Name, "paper_entry") + " | stage=maybe_enter_called"
		st.TopRejectReason = ""
	default:
		st.ModeState = "paper_enabled"
		st.TopDecision = "paper_enabled"
		st.TopDecisionWhy = firstNonEmpty(decision.Signal.Name, "ready") + " | stage=decision_approved_dispatch_pending"
		st.TopRejectReason = ""
	}
}

func paperLogDecision(c candidate, decision strategies.ExecutionDecision, dispatch paperDispatchResult) {
	sym := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	switch {
	case !decision.Approved:
		quality := decision.Quality
		reason := paperDecisionRejectReason(c, decision)
		fmt.Printf("live: paper reject %s side=%s strat=%s stage=decision_rejected reason=%s unresolved_source=%s quality_flags=%s penalty_total=%.2f score_before=%.2f score_after_penalties=%.2f min_score=%.2f hard_block_reasons=%s block_reason=%s\n",
			sym,
			c.Side,
			c.Strat,
			reason,
			firstNonEmpty(unresolvedStrategySource(c), "n/a"),
			strings.Join(quality.QualityFlags, "|"),
			quality.PenaltyTotal,
			quality.ScoreBefore,
			quality.ScoreAfterPenalties,
			quality.MinScore,
			strings.Join(quality.HardBlockReasons, "|"),
			firstNonEmpty(quality.BlockReason, reason, "not_approved"),
		)
	case dispatch.Attempted && dispatch.Entered:
		fmt.Printf("live: paper entered %s side=%s strat=%s stage=opened conf=%.2f\n",
			sym, c.Side, c.Strat, c.Conf)
	case dispatch.Attempted && dispatch.RejectReason != "":
		fmt.Printf("live: paper attempt_failed %s side=%s strat=%s stage=maybe_enter_failed reason=%s\n",
			sym, c.Side, c.Strat, dispatch.RejectReason)
	default:
		fmt.Printf("live: paper entry_attempted %s side=%s strat=%s stage=maybe_enter_called conf=%.2f\n",
			sym, c.Side, c.Strat, c.Conf)
	}
}

func paperDecisionRejectReason(c candidate, decision strategies.ExecutionDecision) string {
	if !decision.Signal.Active {
		return paperInactiveSignalReason(c, decision)
	}
	if rr := strings.TrimSpace(decision.Signal.RejectReason); rr != "" {
		if strings.EqualFold(rr, "below_min_confluence") {
			return "confluence_reject:" + rr
		}
		return "signal_reject:" + rr
	}
	if !decision.RiskDecision.Approved {
		return "risk_reject:" + firstNonEmpty(strings.TrimSpace(decision.RiskDecision.RejectReason), "unknown")
	}
	if decision.Preflight.Checked && !decision.Preflight.Approved {
		return "preflight_reject:" + firstNonEmpty(strings.TrimSpace(decision.Preflight.Reason), "unknown")
	}
	if rr := strings.TrimSpace(decision.RejectReason); rr != "" {
		return "decision_reject:" + rr
	}
	return "not_approved"
}

func paperInactiveSignalReason(c candidate, decision strategies.ExecutionDecision) string {
	if rr := strings.TrimSpace(decision.Signal.RejectReason); rr != "" {
		return "inactive_signal_reject:" + rr
	}
	source := unresolvedSourceFromReason(c.RejectReason)
	switch source {
	case "feature_cache_nil", "feature_bars_insufficient":
		return "inactive_features_unavailable:" + source
	case "runtime_signal_rejected", "mom_reversal_runtime_rejected":
		return "inactive_runtime_not_ready:" + source
	case "router_rejected":
		return "inactive_router_rejected"
	case "inertia_kill":
		return "inactive_state_inertia_kill"
	case "setup_family_unmapped", "continuation_fallback_unmapped", "blank_strategy",
		"explicit_none", "explicit_no_strategy", "explicit_unknown", "explicit_unresolved":
		return "inactive_unresolved_execution:" + source
	}
	if !isExecutableStrategy(c.Strat) {
		if source = unresolvedStrategySource(c); source != "" {
			return "inactive_unresolved_execution:" + source
		}
		return "inactive_unresolved_execution"
	}
	if strings.TrimSpace(c.RejectReason) != "" {
		return "inactive_candidate_reject:" + strings.TrimSpace(strings.Split(c.RejectReason, "|")[0])
	}
	if rr := strings.TrimSpace(decision.RejectReason); rr != "" {
		return "inactive_decision_reject:" + rr
	}
	return "inactive_no_signal"
}

func buildEntryQualityAccumulator(c candidate, rejects []string) strategies.EntryQualityAccumulator {
	scoreBefore := c.CombinedScore
	if scoreBefore <= 0 {
		scoreBefore = clamp(c.Conf, 0, 1)
	}
	minScore := runtimeMinQualityForCandidate(c)
	acc := strategies.EntryQualityAccumulator{
		ScoreBefore:         clamp(scoreBefore, 0, 1),
		ScoreAfterPenalties: clamp(scoreBefore, 0, 1),
		MinScore:            clamp(minScore, 0, 1),
	}
	for _, reason := range rejects {
		addEntryQualityReason(&acc, c, reason)
	}
	addEntryQualityHeuristics(&acc, c)
	if candidateHasProofInputs(c) {
		breakdown := resolvedEntryScoreBreakdown(c)
		if breakdown.FinalScore <= 0 && c.CombinedScore <= 0 {
			appendQualityPenalty(&acc, "entry_score_zero", 1.0)
		}
		switch projectedProofOutcome(c) {
		case EntryOutcomeNoProof:
			appendQualityPenalty(&acc, "projected_no_proof", 1.0)
		case EntryOutcomeWeakProof:
			appendQualityPenalty(&acc, "projected_weak_proof", 1.0)
		}
		proofRoomR := candidateProofRoomR(c)
		if proofRoomR > 0 && proofRoomR < 1.0 {
			appendQualityPenalty(&acc, "insufficient_proof_room", 1.0)
		}
		proofStats := recentRuntimeProofStats(time.Now().UTC(), c)
		if proofStats.NoWeakCount >= 2 && proofStats.GoodStrongCount == 0 {
			appendHardBlock(&acc, "symbol_setup_failed_proof_recently")
		}
		if strings.EqualFold(firstNonEmpty(strings.TrimSpace(c.StrategyID), strings.TrimSpace(c.Strat)), "impulse_breakout") &&
			(strings.EqualFold(strings.TrimSpace(c.EntryTiming), "late") || containsString(acc.QualityFlags, "avoid_chase") || containsString(acc.QualityFlags, "late_chase_fading_impulse") || containsString(acc.QualityFlags, "late_chase_rapid_expansion")) {
			appendQualityPenalty(&acc, "late_impulse_chase", 1.0)
		}
		if strings.EqualFold(firstNonEmpty(strings.TrimSpace(c.StrategyID), strings.TrimSpace(c.Strat)), "exhaustion_reversal") &&
			strings.EqualFold(strings.TrimSpace(c.SetupFamily), "reversal_exhaustion") &&
			projectedProofOutcome(c) != EntryOutcomeStrongProof {
			appendQualityPenalty(&acc, "exhaustion_reversal_requires_strong_proof", 1.0)
		}
		if strings.EqualFold(firstNonEmpty(strings.TrimSpace(c.StrategyID), strings.TrimSpace(c.Strat)), "pullback_reclaim") {
			if (breakdown.FlowScore > 0 || breakdown.TriggerScore > 0) && (breakdown.FlowScore < 8 || breakdown.TriggerScore < 12) {
				appendQualityPenalty(&acc, "pullback_flow_trigger_unconfirmed", 1.0)
			}
		}
	}
	acc.ScoreAfterPenalties = clamp(acc.ScoreBefore-acc.PenaltyTotal, 0, 1)
	switch {
	case containsString(acc.HardBlockReasons, "symbol_setup_failed_proof_recently"):
		acc.BlockReason = "symbol_setup_failed_proof_recently"
	case len(acc.HardBlockReasons) > 0:
		acc.BlockReason = "hard_safety_block"
	case acc.ScoreAfterPenalties < acc.MinScore:
		acc.BlockReason = "quality_score_too_low"
	}
	return acc
}

func projectedProofOutcome(c candidate) EntryOutcome {
	breakdown := resolvedEntryScoreBreakdown(c)
	switch {
	case breakdown.FinalScore >= 85 &&
		(breakdown.LocationScore == 0 || breakdown.LocationScore >= 17) &&
		(breakdown.TriggerScore == 0 || breakdown.TriggerScore >= 14) &&
		(breakdown.FlowScore == 0 || breakdown.FlowScore >= 10):
		return EntryOutcomeStrongProof
	case breakdown.FinalScore >= 72 &&
		(breakdown.LocationScore == 0 || breakdown.LocationScore >= 14) &&
		(breakdown.TriggerScore == 0 || breakdown.TriggerScore >= 12) &&
		(breakdown.FlowScore == 0 || breakdown.FlowScore >= 8):
		return EntryOutcomeGoodProof
	case breakdown.FinalScore >= 58:
		return EntryOutcomeWeakProof
	default:
		return EntryOutcomeNoProof
	}
}

func addEntryQualityReason(acc *strategies.EntryQualityAccumulator, c candidate, reason string) {
	if acc == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	if qualityFlag, penalty, ok := classifyQualityPenaltyReason(c, reason); ok {
		appendQualityPenalty(acc, qualityFlag, penalty)
		return
	}
	appendHardBlock(acc, reason)
}

func addEntryQualityHeuristics(acc *strategies.EntryQualityAccumulator, c candidate) {
	if acc == nil {
		return
	}
	shortCtx := shortPhase2ContextForCandidate(c)
	switch shortCtx.FilterReason {
	case "short_block_late_chase", "short_block_post_pump_chase", "short_post_pump_structure_wait":
		appendHardBlock(acc, shortCtx.FilterReason)
	}
	if blocksMaturePullbackLong(c) {
		appendHardBlock(acc, "mature_pullback_long_needs_reclaim")
	}
	if strings.EqualFold(strings.TrimSpace(c.Entry.EntryStyle), "avoid_chase") || candidateExhaustionActive(c) {
		appendQualityPenalty(acc, "avoid_chase", 0.10)
	}
	if reason := strings.TrimSpace(chaseRejectReason(c, false)); reason != "" {
		if qualityFlag, penalty, ok := classifyQualityPenaltyReason(c, reason); ok {
			appendQualityPenalty(acc, qualityFlag, penalty)
		}
	}
	if conflicting, magnitude := directionallyConflicting(c, envFloat("LIVE_DIRECTIONAL_CONFLICT_BLOCK_PCT", 3.0)); conflicting {
		extremePct := maxFloat(envFloat("LIVE_DIRECTIONAL_CONFLICT_BLOCK_PCT", 3.0)*2.0, envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY_PCT", 2.0)+envFloat("LIVE_DIRECTIONAL_CONFLICT_BLOCK_PCT", 3.0))
		if magnitude >= extremePct {
			appendHardBlock(acc, "directional_dayutc_conflict_extreme")
		} else {
			penalty := envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY", 18.0) / 100.0
			penalty += maxFloat(0.0, magnitude-envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY_PCT", 2.0)) * (envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY_PER_PCT", 2.0) / 100.0)
			appendQualityPenalty(acc, "directional_dayutc_conflict", penalty)
		}
	}
	if isContinuationStrategy(c) && !continuationStructureConfirmed(c) {
		appendQualityPenalty(acc, "weaker_structure", 0.07)
	}
	if isContinuationStrategy(c) && candidateExtendedForBotAdd(c) && !hasFreshStructureReset(c) {
		penalty := envFloat("LIVE_MINOR_EXTENSION_PENALTY", 0.03)
		if gradeValue(c.Entry.CurrentGrade) >= gradeValue("A+") &&
			c.Entry.CurrentScore >= envFloat("LIVE_MINOR_EXTENSION_ELITE_SCORE_MIN", 96.0) &&
			(c.ReclaimHold || c.RetestHold || c.ClosedBreakHold || c.Entry.Momentum) {
			penalty *= envFloat("LIVE_MINOR_EXTENSION_ELITE_PENALTY_FRAC", 0.5)
		}
		appendQualityPenalty(acc, "minor_extension", penalty)
	}
	if weakOFIForCandidate(c) {
		appendQualityPenalty(acc, "weak_ofi", 0.06)
	}
	if weakSlopeForCandidate(c) {
		appendQualityPenalty(acc, "weak_slope", envFloat("LIVE_WEAK_SLOPE_PENALTY", 0.03))
	}
	if c.Sig.ConfluenceScore.TotalScore > 0 && c.Sig.ConfluenceScore.TotalScore < envFloat("LIVE_MIN_CONFLUENCE_SCORE", 0.48) {
		appendQualityPenalty(acc, "imperfect_confluence", 0.08)
	}
}

func timeframeAlignedLong(c candidate) bool {
	minDay := envFloat("LIVE_ALIGN_LONG_DAYUTC_MIN_PCT", 0.0)
	min4h := envFloat("LIVE_ALIGN_LONG_4H_MIN_PCT", 0.0)
	min1h := envFloat("LIVE_ALIGN_LONG_1H_MIN_PCT", 0.0)
	return strings.EqualFold(c.Side, "BUY") && c.DayUTC24h > minDay && c.UTC4hPct > min4h && c.UTC1hPct > min1h
}

func timeframePullbackLong(c candidate) bool {
	return strings.EqualFold(c.Side, "BUY") && c.DayUTC24h > 0 && c.UTC4hPct > 0 && c.UTC1hPct < 0
}

func continuationReclaimConfirmed(c candidate) bool {
	return c.ReclaimHold || c.RetestHold || c.ClosedBreakHold || hasFreshStructureReset(c) || continuationStructureConfirmed(c)
}

func shortJustLostVWAPOrStructure(c candidate) bool {
	if !strings.EqualFold(c.Side, "SELL") {
		return false
	}
	belowVWAP := c.SessionVWAP > 0 && c.LastClose < c.SessionVWAP
	belowEMA := c.EMA9 > 0 && c.LastClose < c.EMA9
	return belowVWAP || belowEMA || c.ClosedBreakHold || c.TriggerState == string(triggerFailReclaim)
}

func shortLowerTimeframeTurnedDown(c candidate) bool {
	switch c.Entry.State {
	case inplay.StateCooling, inplay.StateDumping, inplay.StateExhausted:
		return true
	}
	return c.FastSlope < 0 || c.SlowSlope < 0 || c.Entry.ScoreSlope < 0
}

func blocksMaturePullbackLong(c candidate) bool {
	if !timeframePullbackLong(c) {
		return false
	}
	if continuationReclaimConfirmed(c) {
		return false
	}
	extremeDay := envFloat("LIVE_PULLBACK_LONG_24H_EXTREME_PCT", 60.0)
	flush1h := envFloat("LIVE_PULLBACK_LONG_1H_DROP_PCT", 3.0)
	return c.DayUTC24h >= extremeDay && c.UTC1hPct <= -flush1h
}

func shortBounceFailureConfirmed(c candidate) bool {
	if !strings.EqualFold(c.Side, "SELL") {
		return false
	}
	return c.Entry.FailedBounceCount > 0 || c.Entry.FailedReclaimCount > 0 || c.TriggerState == string(triggerFailReclaim) || c.RetestHold || c.ClosedBreakHold || hasFreshStructureReset(c)
}

func shortFailedBounceConfirmed(c candidate, ctx shortPhase2Context) bool {
	return ctx.BounceFromLocalLowPct >= 2.0 && shortBounceFailureConfirmed(c) && shortLowerTimeframeTurnedDown(c)
}

func blocksAllRedShortChase(c candidate) bool {
	return shortPhase2ContextForCandidate(c).Bucket == "late_chase_short"
}

func classifyQualityPenaltyReason(c candidate, reason string) (string, float64, bool) {
	raw := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case raw == "":
		return "", 0, false
	case strings.Contains(raw, "avoid_chase"):
		return "avoid_chase", 0.10, true
	case strings.Contains(raw, "late_chase_fading_impulse"):
		return "late_chase_fading_impulse", 0.10, true
	case strings.Contains(raw, "late_chase_rapid_expansion"):
		return "late_chase_rapid_expansion", 0.12, true
	case strings.Contains(raw, "late_chase_extended_no_reset"), strings.Contains(raw, "late_extension_no_reset"):
		penalty := envFloat("LIVE_MINOR_EXTENSION_REJECT_PENALTY", 0.05)
		if gradeValue(c.Entry.CurrentGrade) >= gradeValue("A+") &&
			c.Entry.CurrentScore >= envFloat("LIVE_MINOR_EXTENSION_ELITE_SCORE_MIN", 96.0) &&
			(c.ReclaimHold || c.RetestHold || c.ClosedBreakHold || c.Entry.Momentum) {
			penalty *= envFloat("LIVE_MINOR_EXTENSION_ELITE_PENALTY_FRAC", 0.5)
		}
		return "minor_extension", penalty, true
	case strings.Contains(raw, "directional_dayutc_conflict"):
		if conflicting, magnitude := directionallyConflicting(c, envFloat("LIVE_DIRECTIONAL_CONFLICT_BLOCK_PCT", 3.0)); conflicting {
			extremePct := maxFloat(envFloat("LIVE_DIRECTIONAL_CONFLICT_BLOCK_PCT", 3.0)*2.0, envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY_PCT", 2.0)+envFloat("LIVE_DIRECTIONAL_CONFLICT_BLOCK_PCT", 3.0))
			if magnitude >= extremePct {
				return "", 0, false
			}
			penalty := envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY", 18.0) / 100.0
			penalty += maxFloat(0.0, magnitude-envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY_PCT", 2.0)) * (envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY_PER_PCT", 2.0) / 100.0)
			return "directional_dayutc_conflict", penalty, true
		}
		return "directional_dayutc_conflict", 0.10, true
	case strings.Contains(raw, "continuation_no_structure_confirm"), strings.Contains(raw, "structure"):
		return "weaker_structure", 0.07, true
	case strings.Contains(raw, "below_min_confluence"), strings.Contains(raw, "low_conf"), strings.Contains(raw, "conf_"), strings.Contains(raw, "quality"):
		return "imperfect_confluence", 0.08, true
	case rejectReasonHasToken(raw, "ofi") || strings.Contains(raw, "weak_ofi"):
		return "weak_ofi", 0.06, true
	case strings.Contains(raw, "weak_slope"), strings.Contains(raw, "late_cycle_short_weak_slope"):
		return "weak_slope", envFloat("LIVE_WEAK_SLOPE_PENALTY", 0.03), true
	case strings.Contains(raw, "extension"):
		return "minor_extension", envFloat("LIVE_MINOR_EXTENSION_PENALTY", 0.03), true
	default:
		return "", 0, false
	}
}

func rejectReasonHasToken(raw string, token string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	token = strings.ToLower(strings.TrimSpace(token))
	if raw == "" || token == "" {
		return false
	}
	normalized := strings.NewReplacer("|", "_", "-", "_", " ", "_", "/", "_", ":", "_").Replace(raw)
	for _, part := range strings.Split(normalized, "_") {
		if part == token {
			return true
		}
	}
	return false
}

func appendHardBlock(acc *strategies.EntryQualityAccumulator, reason string) {
	reason = strings.TrimSpace(reason)
	if acc == nil || reason == "" || containsString(acc.HardBlockReasons, reason) {
		return
	}
	acc.HardBlockReasons = append(acc.HardBlockReasons, reason)
}

func appendQualityPenalty(acc *strategies.EntryQualityAccumulator, flag string, penalty float64) {
	flag = strings.TrimSpace(flag)
	if acc == nil || flag == "" {
		return
	}
	if !containsString(acc.QualityFlags, flag) {
		acc.QualityFlags = append(acc.QualityFlags, flag)
	}
	acc.PenaltyTotal += math.Max(0, penalty)
}

func runtimeMinQualityForCandidate(c candidate) float64 {
	base := envFloat("LIVE_META_MIN_QUALITY", 0.52)
	shortCtx := shortPhase2ContextForCandidate(c)
	switch {
	case timeframeAlignedLong(c):
		base -= envFloat("LIVE_ALIGN_LONG_QUALITY_RELIEF", 0.04)
	case blocksMaturePullbackLong(c):
		base += envFloat("LIVE_PULLBACK_LONG_QUALITY_PENALTY", 0.08)
	case timeframePullbackLong(c):
		base += envFloat("LIVE_PULLBACK_LONG_QUALITY_PENALTY_SOFT", 0.04)
	case shortCtx.Bucket == "failed_bounce_short":
		base -= envFloat("LIVE_SHORT_FAILED_BOUNCE_QUALITY_RELIEF", 0.03)
	case shortCtx.Bucket == "post_pump_fresh_breakdown":
		base += envFloat("LIVE_POST_PUMP_FRESH_BREAKDOWN_QUALITY_PENALTY", 0.03)
	case strings.EqualFold(c.Side, "SELL") && c.DayUTC24h < 0 && c.UTC4hPct < 0 && c.UTC1hPct < 0:
		base += envFloat("LIVE_ALL_RED_SHORT_QUALITY_PENALTY", 0.06)
	}
	switch strategyFamily(c) {
	case "ignite":
		return clamp(envFloat("LIVE_META_MIN_QUALITY_IGNITE", min(base, 0.50)), 0, 1)
	case "rev":
		return clamp(envFloat("LIVE_META_MIN_QUALITY_REV", min(base, 0.48)), 0, 1)
	default:
		return clamp(envFloat("LIVE_META_MIN_QUALITY_CONT", base), 0, 1)
	}
}

func weakOFIForCandidate(c candidate) bool {
	if c.OFISamples < maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8)) {
		return false
	}
	switch strategyFamily(c) {
	case "ignite":
		threshold := envFloat("LIVE_IGNITE_MIN_OFI_Z", 0.0)
		if strings.EqualFold(c.Side, "BUY") {
			return c.OFIZ < threshold
		}
		return c.OFIZ > -threshold
	case "cont":
		threshold := envFloat("LIVE_CONT_FAST_MIN_OFI_Z", 0.35)
		if strings.EqualFold(c.Side, "BUY") {
			return c.OFIZ < threshold
		}
		return c.OFIZ > -threshold
	default:
		return false
	}
}

func weakSlopeForCandidate(c candidate) bool {
	switch strategyFamily(c) {
	case "ignite":
		threshold := envFloat("LIVE_IGNITE_MIN_SLOPE", 0.08)
		if gradeValue(c.Entry.CurrentGrade) >= gradeValue("A+") &&
			c.Entry.CurrentScore >= envFloat("LIVE_WEAK_SLOPE_ELITE_SCORE_MIN", 95.0) &&
			(c.Entry.Momentum || c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay) {
			threshold *= envFloat("LIVE_WEAK_SLOPE_ELITE_THRESHOLD_FRAC", 0.5)
		}
		if strings.EqualFold(c.Side, "BUY") {
			return c.Entry.ScoreSlope < threshold
		}
		return c.Entry.ScoreSlope > -threshold
	case "cont":
		threshold := envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02)
		if gradeValue(c.Entry.CurrentGrade) >= gradeValue("A+") &&
			c.Entry.CurrentScore >= envFloat("LIVE_WEAK_SLOPE_ELITE_SCORE_MIN", 95.0) &&
			(c.Entry.Momentum || c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay) {
			threshold *= envFloat("LIVE_WEAK_SLOPE_ELITE_THRESHOLD_FRAC", 0.5)
		}
		if strings.EqualFold(c.Side, "BUY") {
			return c.Entry.ScoreSlope < threshold
		}
		return c.Entry.ScoreSlope > -threshold
	default:
		return false
	}
}
