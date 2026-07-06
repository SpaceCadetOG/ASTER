package main

import (
	"fmt"
	"strings"
	"time"

	"go-machine/adapters/aster"
	exitmgr "go-machine/internal/execution"
	"go-machine/internal/inplay"
	"go-machine/internal/risk"
	"go-machine/internal/stats"
	"go-machine/internal/strategies"
)

type sharedTradePlan struct {
	Symbol          string
	Side            string
	Strategy        string
	StrategyID      string
	SetupFamily     string
	SetupSource     string
	TradeHorizon    string
	EntryPrice      float64
	StopPrice       float64
	TP1Price        float64
	TP2Price        float64
	TP3Price        float64
	StopReason      string
	StopDistancePct float64
}

type sharedEntryDecision struct {
	Allowed             bool
	HardBlockReasons    []string
	QualityFlags        []string
	BlockReason         string
	ProjectedProofClass EntryOutcome
	ResolvedStrategy    string
	TradePlan           sharedTradePlan
	ExecutionDecision   strategies.ExecutionDecision
}

type sharedRuntimeDecisionContext struct {
	Candidate             candidate
	MetaBySymbol          map[string]symbolMeta
	RiskShell             risk.Config
	RiskFallbackStopPct   float64
	RiskHoldHours         float64
	LeverageMode          string
	LeverageFixed         int
	LeverageMin           int
	MaxLeverage           int
	EffectiveMargin       float64
	PreflightRejectReason string
	PreflightSource       string
}

type sharedTradePlanConfig struct {
	MinStopPct    float64
	MaxStopPct    float64
	MinTP1RR      float64
	RiskOnMargin  bool
	RiskMarginPct float64
	TP1R          float64
	TP2R          float64
	TP3R          float64
	HybridStopCfg exitmgr.HybridStopConfig
	FrontRunner   interface {
		FrontRunTarget(side string, target float64, frictions ...float64) float64
	}
}

type sharedTrailContext struct {
	EntryReason           string
	EntryVolumeUSD        float64
	ExitProfile           string
	Sponsored             bool
	HitTP1                bool
	WeakSponsorStreak     int
	LastConfluenceRefresh time.Time
}

type liveRuntimeDispatchHooks struct {
	Adapter func(candidate, float64, float64, int, ladderPlan) error
}

type liveRuntimeDispatchResult struct {
	Decision     strategies.ExecutionDecision
	TradePlan    sharedTradePlan
	LadderPlan   ladderPlan
	Attempted    bool
	Entered      bool
	RejectReason string
}

func paperExecutionAdapter(paper *paperTrader, now time.Time, c candidate, entryBps, margin float64, leverage int, meta map[string]symbolMeta, depth map[string]aster.OrderBook, current map[string]inplay.Entry) (*paperPosition, error) {
	if paper == nil {
		return nil, fmt.Errorf("paper_disabled")
	}
	return paper.MaybeEnter(now, c, entryBps, margin, leverage, meta, depth, current)
}

func liveExecutionAdapter(execMgr *liveExecManager, c candidate, entryBps, margin float64, leverage int, plan ladderPlan) error {
	if execMgr == nil {
		return fmt.Errorf("execution manager not ready")
	}
	return execMgr.PlaceEntry(c, entryBps, margin, leverage, plan)
}

func liveBypassesDecisionReject(decision strategies.ExecutionDecision) bool {
	reason := strings.TrimSpace(firstNonEmpty(
		decision.RejectReason,
		decision.Preflight.Reason,
		decision.Quality.BlockReason,
	))
	switch reason {
	case "quality_score_too_low", "symbol_setup_failed_proof_recently":
		return true
	default:
		return false
	}
}

func dispatchLiveRuntimeDecision(now time.Time, c candidate, meta map[string]symbolMeta, execMgr *liveExecManager, riskShell risk.Config, riskFallbackStopPct, riskHoldHours float64, leverageMode string, leverageFixed, leverageMin, leverageMax int, effectiveMargin, entryBps float64, hooks liveRuntimeDispatchHooks) liveRuntimeDispatchResult {
	result := liveRuntimeDispatchResult{
		Decision: buildSharedRuntimeDecision(sharedRuntimeDecisionContext{
			Candidate:           c,
			MetaBySymbol:        meta,
			RiskShell:           riskShell,
			RiskFallbackStopPct: riskFallbackStopPct,
			RiskHoldHours:       riskHoldHours,
			LeverageMode:        leverageMode,
			LeverageFixed:       leverageFixed,
			LeverageMin:         leverageMin,
			MaxLeverage:         leverageMax,
			EffectiveMargin:     effectiveMargin,
			PreflightSource:     "live",
		}).ExecutionDecision,
	}
	emitEntryProofCheck(now, c, result.Decision, "live_dispatch")
	if !result.Decision.Approved {
		if !liveBypassesDecisionReject(result.Decision) {
			result.RejectReason = firstNonEmpty(result.Decision.RejectReason, "not_approved")
			return result
		}
		fmt.Printf("live dispatch advisory: bypassing decision reject=%s symbol=%s side=%s\n",
			firstNonEmpty(result.Decision.RejectReason, "not_approved"),
			strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol))),
			strings.ToUpper(strings.TrimSpace(c.Side)))
	}
	entryRef := result.Decision.Entry
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if entryRef <= 0 {
		entryRef = meta[raw].LastPrice
	}
	if entryRef <= 0 {
		result.RejectReason = "live_price_unavailable"
		return result
	}
	if execMgr == nil {
		result.RejectReason = "execution manager not ready"
		return result
	}
	sharedPlan, err := buildSharedTradePlan(c, entryRef, c.VolumeUSD, sharedTradePlanConfig{
		MinStopPct:    execMgr.minStopPct,
		MaxStopPct:    execMgr.maxStopPct,
		MinTP1RR:      execMgr.minTP1RR,
		RiskOnMargin:  execMgr.riskOnMargin,
		RiskMarginPct: execMgr.riskMarginPct,
		TP1R:          execMgr.tp1R,
		TP2R:          execMgr.tp2R,
		TP3R:          execMgr.tp3R,
		HybridStopCfg: execMgr.hybridStopCfg,
		FrontRunner:   execMgr.exitManager,
	})
	if err != nil {
		result.RejectReason = err.Error()
		return result
	}
	result.TradePlan = sharedPlan
	result.LadderPlan = resolveLadderPlan(now, c, execMgr, meta)
	if result.LadderPlan.RejectReason != "" {
		result.RejectReason = result.LadderPlan.RejectReason
		return result
	}
	adapter := hooks.Adapter
	if adapter == nil {
		adapter = func(c candidate, entryBps, margin float64, leverage int, plan ladderPlan) error {
			return liveExecutionAdapter(execMgr, c, entryBps, margin, leverage, plan)
		}
	}
	result.Attempted = true
	if err := adapter(c, entryBps, effectiveMargin, computeLeverage(c, leverageMode, leverageFixed, leverageMin, leverageMax), result.LadderPlan); err != nil {
		result.RejectReason = strings.TrimSpace(err.Error())
		return result
	}
	result.Entered = true
	return result
}

func applyLiveDecisionStatus(st *liveStatus, c candidate, dispatch liveRuntimeDispatchResult) {
	if st == nil {
		return
	}
	st.Mode = surfacedRuntimeMode(runtimeModeLive)
	switch {
	case dispatch.Entered:
		st.ModeState = "live_entered"
		st.TopDecision = "live_entered"
		st.TopDecisionWhy = firstNonEmpty(dispatch.Decision.Signal.Name, c.Strat, "live_entry") + " | stage=submitted"
		st.TopRejectReason = ""
	case dispatch.Attempted && dispatch.RejectReason != "":
		st.ModeState = "live_candidate_rejected"
		st.TopDecision = "live_candidate_rejected"
		st.TopDecisionWhy = "stage=live_submit_failed reason=" + dispatch.RejectReason
		st.TopRejectReason = dispatch.RejectReason
	case dispatch.RejectReason != "":
		st.ModeState = "live_candidate_rejected"
		st.TopDecision = "live_candidate_rejected"
		st.TopDecisionWhy = "stage=decision_rejected reason=" + dispatch.RejectReason
		st.TopRejectReason = dispatch.RejectReason
	case dispatch.Attempted:
		st.ModeState = "live_entry_attempted"
		st.TopDecision = "live_entry_attempted"
		st.TopDecisionWhy = firstNonEmpty(dispatch.Decision.Signal.Name, c.Strat, "live_entry") + " | stage=submit_called"
		st.TopRejectReason = ""
	default:
		st.ModeState = "live_enabled"
		st.TopDecision = "live_enabled"
		st.TopDecisionWhy = firstNonEmpty(dispatch.Decision.Signal.Name, c.Strat, "ready") + " | stage=decision_approved_dispatch_pending"
		st.TopRejectReason = ""
	}
}

func emitLiveDecisionEvent(log *stats.EventLogger, now time.Time, c candidate, decision strategies.ExecutionDecision) {
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
		Simulated:       false,
		Symbol:          strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol))),
		Side:            strings.ToUpper(strings.TrimSpace(c.Side)),
		Source:          "live",
		Mode:            string(runtimeModeLive),
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

func buildSharedRuntimeDecision(ctx sharedRuntimeDecisionContext) sharedEntryDecision {
	riskDec := sharedRuntimeRiskDecision(ctx)
	preflight := sharedRuntimePreflightVerdict(ctx)
	admission := strategies.AdmissionSummary{
		LifecycleStage: ctx.Candidate.LifecycleStage,
		TriggerStage:   ctx.Candidate.TriggerStage,
		TriggerState:   ctx.Candidate.TriggerState,
		CandidateGrade: ctx.Candidate.Entry.CurrentGrade,
		CandidateScore: ctx.Candidate.Entry.CurrentScore,
		FinalRank:      ctx.Candidate.FinalRank,
	}
	decision := strategies.NewExecutionDecision(
		ctx.Candidate.Entry.Symbol,
		ctx.Candidate.Sig,
		riskDec,
		preflight,
		admission,
		firstNonEmpty(strings.TrimSpace(ctx.PreflightSource), "shared"),
		firstNonEmpty(strings.TrimSpace(ctx.Candidate.Strat), "unknown"),
	)
	return sharedEntryDecision{
		Allowed:             decision.Approved,
		HardBlockReasons:    append([]string(nil), decision.Quality.HardBlockReasons...),
		QualityFlags:        append([]string(nil), decision.Quality.QualityFlags...),
		BlockReason:         decision.Quality.BlockReason,
		ProjectedProofClass: projectedProofOutcome(ctx.Candidate),
		ResolvedStrategy:    firstNonEmpty(strings.TrimSpace(ctx.Candidate.StrategyID), strings.TrimSpace(ctx.Candidate.Strat), "unknown"),
		ExecutionDecision:   decision,
	}
}

func sharedRuntimeRiskDecision(ctx sharedRuntimeDecisionContext) risk.Decision {
	raw := strings.ToUpper(strings.TrimSpace(ctx.Candidate.Entry.Symbol))
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

func sharedRuntimePreflightVerdict(ctx sharedRuntimeDecisionContext) strategies.PreflightVerdict {
	quality := buildEntryQualityAccumulator(ctx.Candidate, nil)
	reasons := []string{}
	if reason := strings.TrimSpace(ctx.PreflightRejectReason); reason != "" {
		reasons = append(reasons, reason)
	}
	verdict := strategies.PreflightVerdict{
		Checked:  true,
		Approved: len(reasons) == 0 && strings.TrimSpace(quality.BlockReason) == "",
		Source:   firstNonEmpty(strings.TrimSpace(ctx.PreflightSource), "shared"),
		Reasons:  reasons,
		Quality:  quality,
	}
	if len(reasons) > 0 {
		verdict.Reason = reasons[0]
	} else if !verdict.Approved {
		verdict.Reason = quality.BlockReason
	}
	return verdict
}

func buildSharedTradePlan(c candidate, entryPrice, volumeUSD float64, cfg sharedTradePlanConfig) (sharedTradePlan, error) {
	stopPct := cfg.MinStopPct / 100.0
	if cfg.MaxStopPct > 0 && stopPct <= 0 {
		stopPct = cfg.MaxStopPct / 100.0
	}
	if cfg.RiskOnMargin {
		if riskPct := marginRiskStopPct(1, 1, cfg.RiskMarginPct); riskPct > 0 {
			stopPct = riskPct
		}
	}
	if c.Sig.Entry > 0 && c.Sig.Stop > 0 {
		riskPct := abs(c.Sig.Entry-c.Sig.Stop) / c.Sig.Entry
		if riskPct > 0 {
			stopPct = clamp(riskPct, cfg.MinStopPct/100.0, cfg.MaxStopPct/100.0)
		}
	}
	tp1R := cfg.TP1R
	tp2R := cfg.TP2R
	tp3R := cfg.TP3R
	if c.Sig.Entry > 0 && c.Sig.Stop > 0 {
		baseRisk := abs(c.Sig.Entry - c.Sig.Stop)
		if baseRisk > 0 && c.Sig.TP1 > 0 {
			tp1R = abs(c.Sig.TP1-c.Sig.Entry) / baseRisk
		}
		if baseRisk > 0 && c.Sig.TP2 > 0 {
			tp2R = abs(c.Sig.TP2-c.Sig.Entry) / baseRisk
		}
	}
	stopPct, tp1R, tp2R, tp3R = adjustBracketParams(
		c.Strat,
		c.Entry.CurrentGrade,
		c.Entry.State,
		c.Conf,
		volumeUSD,
		stopPct,
		tp1R,
		tp2R,
		tp3R,
		cfg.MinStopPct/100.0,
		cfg.MaxStopPct/100.0,
	)
	if c.Sig.Stop <= 0 {
		stopPct = clamp(widenStopPctForVolatility(stopPct, c.ATRPct, volumeUSD), cfg.MinStopPct/100.0, cfg.MaxStopPct/100.0)
	}
	stopReason := ""
	stopDistancePct := stopPct * 100.0
	if cfg.HybridStopCfg.Enabled {
		stopRes := exitmgr.ComputeHybridStop(cfg.HybridStopCfg, hybridStopInputForCandidate(c, entryPrice, c.Sig.TP1))
		if stopRes.Rejected {
			return sharedTradePlan{}, fmt.Errorf("%s", stopRes.RejectReason)
		}
		if stopRes.StopPrice > 0 {
			stopPct = clamp(stopRes.StopDistancePct/100.0, cfg.MinStopPct/100.0, cfg.MaxStopPct/100.0)
			stopReason = stopRes.StopReason
			stopDistancePct = stopRes.StopDistancePct
		}
	}
	tp1Pct := stopPct * tp1R
	tp2Pct := stopPct * tp2R
	tp3Pct := stopPct * tp3R
	stop := entryPrice
	tp1 := entryPrice
	tp2 := entryPrice
	tp3 := entryPrice
	if strings.EqualFold(c.Side, "BUY") {
		stop = entryPrice * (1 - stopPct)
		tp1 = entryPrice * (1 + tp1Pct)
		tp2 = entryPrice * (1 + tp2Pct)
		tp3 = entryPrice * (1 + tp3Pct)
	} else {
		stop = entryPrice * (1 + stopPct)
		tp1 = entryPrice * (1 - tp1Pct)
		tp2 = entryPrice * (1 - tp2Pct)
		tp3 = entryPrice * (1 - tp3Pct)
	}
	if cfg.FrontRunner != nil {
		tp1 = cfg.FrontRunner.FrontRunTarget(c.Side, tp1, c.Sig.VPTargetLevel)
		tp2 = cfg.FrontRunner.FrontRunTarget(c.Side, tp2, c.Sig.VPTargetLevel)
		tp3 = cfg.FrontRunner.FrontRunTarget(c.Side, tp3, c.Sig.VPTargetLevel)
	}
	tp1, tp2, tp3 = enforceTPProgression(c.Side, tp1, tp2, tp3)
	stop, tp1, tp2, tp3 = sanitizeBracketGeometry(entryPrice, c.Side, stop, tp1, tp2, tp3)
	if stop <= 0 || tp1 <= 0 || tp2 <= 0 || tp3 <= 0 {
		return sharedTradePlan{}, fmt.Errorf("invalid bracket levels")
	}
	riskDist := abs(entryPrice - stop)
	reward := abs(tp1 - entryPrice)
	if riskDist <= 0 || reward/riskDist < cfg.MinTP1RR {
		return sharedTradePlan{}, fmt.Errorf("tp1 rr below minimum")
	}
	return sharedTradePlan{
		Symbol:          strings.ToUpper(strings.TrimSpace(c.Entry.Symbol)),
		Side:            strings.ToUpper(strings.TrimSpace(c.Side)),
		Strategy:        firstNonEmpty(strings.TrimSpace(c.Strat), "manual"),
		StrategyID:      firstNonEmpty(strings.TrimSpace(c.StrategyID), "unknown"),
		SetupFamily:     c.SetupFamily,
		SetupSource:     c.SetupSource,
		TradeHorizon:    c.TradeHorizon,
		EntryPrice:      entryPrice,
		StopPrice:       stop,
		TP1Price:        tp1,
		TP2Price:        tp2,
		TP3Price:        tp3,
		StopReason:      stopReason,
		StopDistancePct: stopDistancePct,
	}, nil
}

func calcSharedTrailStop(sideBuy bool, ref float64, trailMode string, trailPct, trailPctMin, trailPctTP3 float64, postTP3 bool, atrPct, structureDist float64, ctx sharedTrailContext) float64 {
	if ref <= 0 {
		return 0
	}
	pct := trailPct / 100.0
	if postTP3 && trailPctTP3 > 0 {
		pct = trailPctTP3 / 100.0
	}
	if pct <= 0 {
		pct = 0.01
	}
	dist := ref * pct
	floorDist := ref * (trailPctMin / 100.0)
	atrDist := 0.0
	if atrPct > 0 {
		atrDist = ref * atrPct * trailATRMultForContext(ctx.EntryReason, atrPct, ctx.EntryVolumeUSD)
	}
	switch strings.ToLower(strings.TrimSpace(trailMode)) {
	case "structure":
		if structureDist > 0 {
			dist = maxFloat(floorDist, structureDist)
		}
	case "atr":
		if atrDist > 0 {
			dist = maxFloat(floorDist, atrDist)
		}
	default:
		if atrDist > 0 {
			dist = maxFloat(floorDist, atrDist)
		}
		if structureDist > 0 {
			dist = maxFloat(dist, structureDist)
		}
	}
	dist *= trailProfileMultiplier(ctx.ExitProfile)
	if ctx.Sponsored {
		dist *= envFloat("LIVE_TRAIL_SPONSORED_MULT", 1.15)
	} else if ctx.HitTP1 {
		dist *= envFloat("LIVE_TRAIL_UNSPONSORED_MULT", 0.85)
		if ctx.WeakSponsorStreak >= envInt("LIVE_TRAIL_WEAK_SPONSOR_STREAK", 2) {
			dist *= envFloat("LIVE_TRAIL_WEAK_SPONSOR_MULT", 0.75)
		}
	}
	if confluenceRefreshActive(time.Now().UTC(), ctx.LastConfluenceRefresh) {
		dist *= envFloat("LIVE_TRAIL_CONFLUENCE_REFRESH_MULT", 1.30)
	}
	if postTP3 {
		if ctx.Sponsored {
			dist *= envFloat("LIVE_TRAIL_SPONSORED_POST_TP3_MULT", 1.25)
		} else {
			dist *= envFloat("LIVE_TRAIL_UNSPONSORED_POST_TP3_MULT", 0.95)
		}
	}
	if sideBuy {
		return ref - dist
	}
	return ref + dist
}
