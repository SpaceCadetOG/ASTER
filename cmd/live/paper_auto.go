package main

import (
	"fmt"
	"sort"
	"strings"
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
	runtimeModeManualOnly runtimeOperatingMode = "manual_only"
	runtimeModePaperAuto  runtimeOperatingMode = "paper_auto"
	runtimeModeLiveAuto   runtimeOperatingMode = "live_auto"
)

func parseRuntimeOperatingMode(raw string) runtimeOperatingMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(runtimeModePaperAuto):
		return runtimeModePaperAuto
	case string(runtimeModeLiveAuto):
		return runtimeModeLiveAuto
	default:
		return runtimeModeManualOnly
	}
}

func surfacedRuntimeMode(mode runtimeOperatingMode) string {
	if mode == runtimeModePaperAuto {
		return "paper"
	}
	return string(mode)
}

func surfacedPaperLabel(raw string) string {
	value := strings.TrimSpace(raw)
	if strings.EqualFold(value, string(runtimeModePaperAuto)) {
		return "paper"
	}
	if value == "" {
		return "paper"
	}
	return value
}

func surfacedPaperState(raw string) string {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(value), "paper_auto_") {
		return "paper_" + strings.TrimPrefix(value, "paper_auto_")
	}
	return value
}

func selectTopRuntimeCandidate(cands []candidate) (candidate, bool) {
	if len(cands) == 0 {
		return candidate{}, false
	}
	ordered := append([]candidate(nil), cands...)
	sortCandidatesForRuntime(ordered)
	return ordered[0], true
}

func sortCandidatesForRuntime(cands []candidate) {
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Entry.CurrentScore == cands[j].Entry.CurrentScore {
			if cands[i].Entry.ScoreSlope == cands[j].Entry.ScoreSlope {
				return cands[i].Entry.Rank < cands[j].Entry.Rank
			}
			return cands[i].Entry.ScoreSlope > cands[j].Entry.ScoreSlope
		}
		return cands[i].Entry.CurrentScore > cands[j].Entry.CurrentScore
	})
}

type paperAutoDecisionCtx struct {
	Now                 time.Time
	LocalMaintNow       time.Time
	Candidate           candidate
	MetaBySymbol        map[string]symbolMeta
	EntryDepth          map[string]aster.OrderBook
	Paper               *paperTrader
	CurrentEntries      map[string]inplay.Entry
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

func buildPaperAutoExecutionDecision(ctx paperAutoDecisionCtx) strategies.ExecutionDecision {
	riskDec := paperAutoRiskDecision(ctx)
	preflight := paperAutoPreflightVerdict(ctx)
	admission := strategies.AdmissionSummary{
		LifecycleStage: ctx.Candidate.LifecycleStage,
		TriggerStage:   ctx.Candidate.TriggerStage,
		TriggerState:   ctx.Candidate.TriggerState,
		CandidateGrade: ctx.Candidate.Entry.CurrentGrade,
		CandidateScore: ctx.Candidate.Entry.CurrentScore,
		FinalRank:      ctx.Candidate.FinalRank,
	}
	return strategies.NewExecutionDecision(
		ctx.Candidate.Entry.Symbol,
		ctx.Candidate.Sig,
		riskDec,
		preflight,
		admission,
		"paper_auto",
		firstNonEmpty(strings.TrimSpace(ctx.Candidate.Strat), "unknown"),
	)
}

func paperAutoRiskDecision(ctx paperAutoDecisionCtx) risk.Decision {
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
	return risk.Approve(ctx.RiskShell, risk.Input{
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
}

func paperAutoPreflightVerdict(ctx paperAutoDecisionCtx) strategies.PreflightVerdict {
	reasons := make([]string, 0, 3)
	pre := deepQueuePreflight(ctx.Candidate, queueDeepPreflightCtx{
		Now:                 ctx.Now,
		LocalMaintNow:       ctx.LocalMaintNow,
		PureMode:            true,
		OBFilterEnable:      ctx.OBFilterEnable,
		EntryDepth:          ctx.EntryDepth,
		OBLevels:            ctx.OBLevels,
		OBImbMin:            ctx.OBImbMin,
		OBMaxSpreadBps:      ctx.OBMaxSpreadBps,
		RiskShell:           ctx.RiskShell,
		RiskFallbackStopPct: ctx.RiskFallbackStopPct,
		RiskHoldHours:       ctx.RiskHoldHours,
		LeverageMode:        ctx.LeverageMode,
		LeverageFixed:       ctx.LeverageFixed,
		LeverageMin:         ctx.LeverageMin,
		MaxLeverage:         ctx.MaxLeverage,
		EffectiveReserve:    ctx.EffectiveReserve,
		EffectiveMargin:     ctx.EffectiveMargin,
		AvailableUSDT:       ctx.AvailableUSDT,
		MetaBySymbol:        ctx.MetaBySymbol,
		Paper:               ctx.Paper,
		MaxOpenPos:          ctx.MaxOpenPos,
		MaxOpenPerSide:      ctx.MaxOpenPerSide,
	})
	if reason := strings.TrimSpace(pre.RejectReason); reason != "" {
		reasons = append(reasons, reason)
	}
	if reason := strings.TrimSpace(paperAutoPaperRejectReason(ctx)); reason != "" && !containsString(reasons, reason) {
		reasons = append(reasons, reason)
	}
	verdict := strategies.PreflightVerdict{
		Checked:  true,
		Approved: len(reasons) == 0,
		Source:   "paper_auto",
		Reasons:  append([]string(nil), reasons...),
	}
	if len(reasons) > 0 {
		verdict.Reason = reasons[0]
	}
	return verdict
}

func paperAutoPaperRejectReason(ctx paperAutoDecisionCtx) string {
	p := ctx.Paper
	if p == nil || !p.enabled {
		return "paper_disabled"
	}
	raw := strings.ToUpper(aster.RawSymbol(ctx.Candidate.Entry.Symbol))
	if raw == "" {
		return "empty_symbol"
	}
	if len(p.positions) >= p.maxOpen {
		if replacePos, _ := p.slotReplacementCandidate(ctx.Now, ctx.Candidate, ctx.MetaBySymbol, ctx.CurrentEntries); replacePos == nil {
			return "max_paper_positions"
		}
	}
	if p.freeForEntries() < ctx.EffectiveMargin {
		return "insufficient_usable_paper_balance"
	}
	if _, exists := p.positions[raw]; exists {
		return "paper_symbol_already_open"
	}
	if t := p.lockUntil[raw]; !t.IsZero() && ctx.Now.Before(t) {
		return "paper_symbol_lock_active"
	}
	if blocked, reason := p.blocksHarvestReentry(raw, ctx.Now, ctx.Candidate); blocked {
		return reason
	}
	if blocked, reason := p.blocksSymbolTradeBudget(raw, ctx.Now, ctx.Candidate); blocked {
		return reason
	}
	if p.lossCooldown > 0 {
		if t := p.lastExitAt[raw]; !t.IsZero() && p.lastExitLoss[raw] && ctx.Now.Sub(t) < p.lossCooldown {
			return "paper_symbol_loss_cooldown"
		}
	}
	if meta := ctx.MetaBySymbol[raw]; meta.LastPrice <= 0 {
		return "paper_price_unavailable"
	}
	return ""
}

type paperAutoEnterFunc func(time.Time, candidate, float64, float64, int, map[string]symbolMeta, map[string]aster.OrderBook, map[string]inplay.Entry) (*paperPosition, error)

type paperAutoDispatchHooks struct {
	Paper paperAutoEnterFunc
	Live  func() error
}

type paperAutoDispatchResult struct {
	Attempted    bool
	Entered      bool
	RejectReason string
	Position     *paperPosition
}

func dispatchPaperAutoDecision(mode runtimeOperatingMode, now time.Time, decision strategies.ExecutionDecision, c candidate, entryBps, margin float64, leverage int, meta map[string]symbolMeta, depth map[string]aster.OrderBook, current map[string]inplay.Entry, hooks paperAutoDispatchHooks) paperAutoDispatchResult {
	if mode != runtimeModePaperAuto {
		return paperAutoDispatchResult{}
	}
	if !decision.Approved {
		return paperAutoDispatchResult{RejectReason: firstNonEmpty(decision.RejectReason, "not_approved")}
	}
	if hooks.Paper == nil {
		return paperAutoDispatchResult{RejectReason: "paper_entry_unavailable"}
	}
	pos, err := hooks.Paper(now, c, entryBps, margin, leverage, meta, depth, current)
	if err != nil {
		return paperAutoDispatchResult{
			Attempted:    true,
			RejectReason: strings.TrimSpace(err.Error()),
		}
	}
	return paperAutoDispatchResult{
		Attempted: true,
		Entered:   pos != nil,
		Position:  pos,
	}
}

func annotatePaperPositionFromDecision(pos *paperPosition, c candidate, decision strategies.ExecutionDecision) {
	if pos == nil {
		return
	}
	pos.EntryMode = string(runtimeModePaperAuto)
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

func emitPaperAutoDecisionEvent(log *stats.EventLogger, now time.Time, c candidate, decision strategies.ExecutionDecision) {
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
		Source:          "paper_auto",
		Mode:            string(runtimeModePaperAuto),
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

func emitPaperAutoPositionOpenEvent(log *stats.EventLogger, now time.Time, c candidate, pos *paperPosition, decision strategies.ExecutionDecision) {
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
		Source:          "paper_auto",
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

func applyPaperAutoDecisionStatus(st *liveStatus, decision strategies.ExecutionDecision, dispatch paperAutoDispatchResult) {
	if st == nil {
		return
	}
	st.Mode = surfacedRuntimeMode(runtimeModePaperAuto)
	switch {
	case dispatch.Entered:
		st.ModeState = "paper_entered"
		st.TopDecision = "paper_entered"
		st.TopDecisionWhy = firstNonEmpty(decision.Signal.Name, "paper_entry")
		st.TopRejectReason = ""
	case dispatch.Attempted && dispatch.RejectReason != "":
		st.ModeState = "paper_candidate_rejected"
		st.TopDecision = "paper_candidate_rejected"
		st.TopDecisionWhy = dispatch.RejectReason
		st.TopRejectReason = dispatch.RejectReason
	case !decision.Approved:
		st.ModeState = "paper_candidate_rejected"
		st.TopDecision = "paper_candidate_rejected"
		st.TopDecisionWhy = firstNonEmpty(decision.RejectReason, "not_approved")
		st.TopRejectReason = firstNonEmpty(decision.RejectReason, "not_approved")
	case dispatch.Attempted:
		st.ModeState = "paper_entry_attempted"
		st.TopDecision = "paper_entry_attempted"
		st.TopDecisionWhy = firstNonEmpty(decision.Signal.Name, "paper_entry")
		st.TopRejectReason = ""
	default:
		st.ModeState = "paper_enabled"
		st.TopDecision = "paper_enabled"
		st.TopDecisionWhy = firstNonEmpty(decision.Signal.Name, "ready")
		st.TopRejectReason = ""
	}
}

func paperAutoLogDecision(c candidate, decision strategies.ExecutionDecision, dispatch paperAutoDispatchResult) {
	sym := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	switch {
	case !decision.Approved:
		fmt.Printf("live: paper reject %s side=%s strat=%s reason=%s\n",
			sym, c.Side, c.Strat, firstNonEmpty(decision.RejectReason, "not_approved"))
	case dispatch.Attempted && dispatch.Entered:
		fmt.Printf("live: paper entered %s side=%s strat=%s conf=%.2f\n",
			sym, c.Side, c.Strat, c.Conf)
	case dispatch.Attempted && dispatch.RejectReason != "":
		fmt.Printf("live: paper attempt_failed %s side=%s strat=%s reason=%s\n",
			sym, c.Side, c.Strat, dispatch.RejectReason)
	default:
		fmt.Printf("live: paper entry_attempted %s side=%s strat=%s conf=%.2f\n",
			sym, c.Side, c.Strat, c.Conf)
	}
}
