package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"go-machine/adapters/aster"
	exitmgr "go-machine/internal/execution"
	flowpkg "go-machine/internal/flow"
	"go-machine/internal/indicators"
	"go-machine/internal/inplay"
	"go-machine/internal/strategies"
)

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

type UnifiedEntryDecision struct {
	Simple  SimpleEntryDecision
	Entry   strategies.EntryDecision
	Context strategies.StrategyContext
}

type EntryPosture string

const (
	PostureAttackNow      EntryPosture = "ATTACK_NOW"
	PostureStarterNow     EntryPosture = "STARTER_NOW"
	PostureWaitPullback   EntryPosture = "WAIT_FOR_PULLBACK"
	PostureWaitReclaim    EntryPosture = "WAIT_FOR_RECLAIM"
	PostureBlockExhausted EntryPosture = "BLOCK_EXHAUSTED"
	PostureBlockWeakFlow  EntryPosture = "BLOCK_WEAK_FLOW"
	PostureBlockRisk      EntryPosture = "BLOCK_RISK"
)

var liveEntryAccountHealthProvider = func() accountHealthSummary {
	return accountHealthSummary{State: "healthy"}
}

var (
	reentryGuardMu      sync.Mutex
	reentryGuardBySym   = map[string]exitmgr.ReentryRecord{}
	reentryGuardEnabled = true
	simpleDecisionLogMu  sync.Mutex
	simpleDecisionLogMem = map[string]simpleDecisionLogState{}
	waveSlopeMu          sync.Mutex
	waveSlopeStateBySym  = map[string]waveSlopeState{}
)

type simpleDecisionLogState struct {
	Reason       string
	Allowed      bool
	Score        float64
	Slope        float64
	State        string
	SpreadBps    float64
	ExtensionATR float64
	ExpiresAt    time.Time
}

type waveSlopeState struct {
	LastSlope   float64
	RisingStreak int
	LastSeen    time.Time
}

func setLiveEntryAccountHealthProvider(fn func() accountHealthSummary) {
	if fn == nil {
		liveEntryAccountHealthProvider = func() accountHealthSummary { return accountHealthSummary{State: "healthy"} }
		return
	}
	liveEntryAccountHealthProvider = fn
}

func currentAccountHealth() accountHealthSummary {
	return liveEntryAccountHealthProvider()
}

func DecideEntry(ctx strategies.StrategyContext, router *strategies.DefaultRouter, confirmer strategies.ConfirmationEngine) strategies.EntryDecision {
	if router == nil {
		router = strategies.NewDefaultRouter(confirmer)
	}
	intents := router.Route(ctx)
	if len(intents) == 0 {
		return strategies.EntryDecision{
			Allowed:      false,
			RejectReason: "no_strategy_intent",
			RejectCodes:  []string{"no_strategy_intent"},
			FinalScore:   0,
		}
	}
	for _, intent := range intents {
		if intent == nil {
			continue
		}
		cfx := strategies.ScoreConfluenceForIntent(ctx, intent)
		intent.Score += cfx.Score
		intent.ReasonCodes = append(intent.ReasonCodes, cfx.Reasons...)
	}
	best := chooseBestIntent(intents)
	if best == nil {
		return strategies.EntryDecision{
			Allowed:      false,
			RejectReason: "no_best_intent",
			RejectCodes:  []string{"no_best_intent"},
			FinalScore:   0,
		}
	}
	hardBlocks := evaluateHardBlocks(ctx, best)
	if len(hardBlocks) > 0 {
		return strategies.EntryDecision{
			Allowed:      false,
			Intent:       best,
			RejectReason: strings.Join(hardBlocks, ","),
			RejectCodes:  append([]string(nil), hardBlocks...),
			FinalScore:   best.Score,
			HardBlocks:   append([]string(nil), hardBlocks...),
		}
	}
	okFlow, flowBlocks := confirmIntent(ctx, best)
	if !okFlow {
		return strategies.EntryDecision{
			Allowed:      false,
			Intent:       best,
			RejectReason: strings.Join(flowBlocks, ","),
			RejectCodes:  append([]string(nil), flowBlocks...),
			FinalScore:   best.Score,
		}
	}
	if confirmer != nil {
		ok, confirmBlocks := confirmer.Confirm(ctx, best)
		if !ok {
			return strategies.EntryDecision{
				Allowed:      false,
				Intent:       best,
				RejectReason: strings.Join(confirmBlocks, ","),
				RejectCodes:  append([]string(nil), confirmBlocks...),
				FinalScore:   best.Score,
			}
		}
	}
	return strategies.EntryDecision{
		Allowed:    true,
		Intent:     best,
		FinalScore: best.Score,
	}
}

func confirmIntent(ctx strategies.StrategyContext, intent *strategies.EntryIntent) (bool, []string) {
	if intent == nil {
		return false, []string{"nil_intent"}
	}
	needsFlow := false
	for _, req := range intent.RequiresConfirm {
		if req == "flow_confirm" {
			needsFlow = true
			break
		}
	}
	if !needsFlow {
		return true, nil
	}
	if !envBool("LIVE_FLOW_CONFIRM_ENABLE", true) {
		return true, nil
	}
	switch intent.Side {
	case strategies.SideLong:
		if (ctx.Flow.StackedImbalanceBull || ctx.Flow.AbsorptionBull || ctx.Flow.DeltaDivBull) &&
			ctx.Flow.Confidence >= envFloat("LIVE_FLOW_MIN_CONFIDENCE", 0.55) {
			return true, nil
		}
		return false, []string{"flow_confirm_missing"}
	case strategies.SideShort:
		if (ctx.Flow.StackedImbalanceBear || ctx.Flow.AbsorptionBear || ctx.Flow.DeltaDivBear) &&
			ctx.Flow.Confidence >= envFloat("LIVE_FLOW_MIN_CONFIDENCE", 0.55) {
			return true, nil
		}
		return false, []string{"flow_confirm_missing"}
	default:
		return false, []string{"flow_confirm_missing"}
	}
}

func buildAnchoredVWAPSnapshot(candles []indicators.Candle, events []indicators.MarketEvent, markPrice float64) indicators.AnchoredVWAPSnapshot {
	anchor := indicators.SelectPrimaryAnchor(candles, events)
	return indicators.ComputeAnchoredVWAP(candles, anchor, markPrice)
}

func buildFlowSnapshot(trades []flowpkg.Trade, book flowpkg.OrderBook, candles []flowpkg.Candle) strategies.FlowSnapshot {
	snap := flowpkg.BuildFlowSnapshot(trades, book, candles)
	return strategies.FlowSnapshot{
		Delta:                snap.Delta,
		CumDelta:             snap.CumDelta,
		DeltaDivBull:         snap.DeltaDivBull,
		DeltaDivBear:         snap.DeltaDivBear,
		AbsorptionBull:       snap.AbsorptionBull,
		AbsorptionBear:       snap.AbsorptionBear,
		StackedImbalanceBull: snap.StackedImbalanceBull,
		StackedImbalanceBear: snap.StackedImbalanceBear,
		UnfinishedBusinessUp: snap.UnfinishedBusinessUp,
		UnfinishedBusinessDn: snap.UnfinishedBusinessDn,
		Confidence:           snap.Confidence,
		Summary:              snap.Summary,
	}
}

func syntheticAVWAPCandles(c candidate, markPrice float64, now time.Time) []indicators.Candle {
	if markPrice <= 0 {
		return nil
	}
	baseVol := c.VolumeRatio
	if baseVol <= 0 {
		baseVol = 1
	}
	slope := c.SlowSlope
	if slope > 1 {
		slope = 1
	}
	if slope < -1 {
		slope = -1
	}
	step := slope * 0.0008
	out := make([]indicators.Candle, 0, 12)
	for i := 0; i < 12; i++ {
		k := float64(i - 11)
		closePx := markPrice * (1.0 + step*k)
		if closePx <= 0 {
			closePx = markPrice
		}
		openPx := closePx * (1.0 - step*0.5)
		highPx := max(closePx, openPx) * 1.0006
		lowPx := min(closePx, openPx) * 0.9994
		out = append(out, indicators.Candle{
			Time:   now.Add(time.Duration(i-11) * time.Minute),
			Open:   openPx,
			High:   highPx,
			Low:    lowPx,
			Close:  closePx,
			Volume: baseVol + float64(i)*0.05,
		})
	}
	return out
}

func syntheticFlowTrades(c candidate, markPrice float64, now time.Time) []flowpkg.Trade {
	if markPrice <= 0 {
		return nil
	}
	size := max(1.0, mathAbs(c.OFIRaw))
	isBuy := strings.EqualFold(strings.TrimSpace(c.Side), "BUY")
	return []flowpkg.Trade{
		{Time: now.Add(-2 * time.Second), Price: markPrice * 0.999, Size: size * 0.6, IsBuyAgg: isBuy, IsSellAgg: !isBuy},
		{Time: now.Add(-1 * time.Second), Price: markPrice, Size: size * 0.4, IsBuyAgg: isBuy, IsSellAgg: !isBuy},
	}
}

func syntheticFlowBook(markPrice float64) flowpkg.OrderBook {
	if markPrice <= 0 {
		return flowpkg.OrderBook{}
	}
	return flowpkg.OrderBook{
		Bids: []flowpkg.BookLevel{
			{Price: markPrice * 0.9995, Size: 120},
			{Price: markPrice * 0.9990, Size: 70},
			{Price: markPrice * 0.9985, Size: 40},
		},
		Asks: []flowpkg.BookLevel{
			{Price: markPrice * 1.0005, Size: 110},
			{Price: markPrice * 1.0010, Size: 65},
			{Price: markPrice * 1.0015, Size: 35},
		},
	}
}

func syntheticFlowCandles(markPrice float64, now time.Time) []flowpkg.Candle {
	if markPrice <= 0 {
		return nil
	}
	return []flowpkg.Candle{
		{Time: now.Add(-2 * time.Minute), Open: markPrice * 0.999, High: markPrice * 1.002, Low: markPrice * 0.998, Close: markPrice * 1.001, Volume: 100},
		{Time: now.Add(-1 * time.Minute), Open: markPrice * 1.001, High: markPrice * 1.003, Low: markPrice * 0.999, Close: markPrice, Volume: 120},
	}
}

func chooseBestIntent(intents []*strategies.EntryIntent) *strategies.EntryIntent {
	if len(intents) == 0 {
		return nil
	}
	best := intents[0]
	for _, it := range intents[1:] {
		if it == nil {
			continue
		}
		if best == nil || it.Score > best.Score {
			best = it
		}
	}
	return best
}

func evaluateHardBlocks(ctx strategies.StrategyContext, intent *strategies.EntryIntent) []string {
	var out []string
	if isSpreadTooWide(ctx) {
		out = append(out, "spread_too_wide")
	}
	if isLiquidityInsufficient(ctx) {
		out = append(out, "insufficient_liquidity")
	}
	if isOrderLegalityBlocked(ctx, intent) {
		out = append(out, "order_legality_block")
	}
	return out
}

func isSpreadTooWide(ctx strategies.StrategyContext) bool {
	maxSpread := envFloat("LIVE_MAX_SPREAD_BPS", envFloat("LIVE_OB_MAX_SPREAD_BPS", 10))
	return ctx.SpreadBps > maxSpread
}

func isLiquidityInsufficient(ctx strategies.StrategyContext) bool {
	// Thin adapter for Slice A; live engine still uses deeper checks downstream.
	return ctx.MarkPrice <= 0
}

func isOrderLegalityBlocked(ctx strategies.StrategyContext, intent *strategies.EntryIntent) bool {
	if intent == nil {
		return true
	}
	if intent.TriggerPrice <= 0 || intent.StopPrice <= 0 {
		return true
	}
	return false
}

func strategyContextFromCandidate(c candidate, now time.Time) strategies.StrategyContext {
	mark := c.LastClose
	if mark <= 0 {
		mark = c.SessionVWAP
	}
	if mark <= 0 {
		mark = 1
	}
	side := strings.ToUpper(strings.TrimSpace(c.Side))
	trend5 := "flat"
	trend15 := "flat"
	if c.FastSlope > 0 {
		trend5 = "up"
	} else if c.FastSlope < 0 {
		trend5 = "down"
	}
	if c.SlowSlope > 0 {
		trend15 = "up"
	} else if c.SlowSlope < 0 {
		trend15 = "down"
	}
	impulseUp := side == "BUY" && c.Entry.Momentum
	impulseDown := side == "SELL" && c.Entry.Momentum
	avwapCandles := syntheticAVWAPCandles(c, mark, now)
	avwapSnap := buildAnchoredVWAPSnapshot(avwapCandles, nil, mark)
	flowSnap := buildFlowSnapshot(syntheticFlowTrades(c, mark, now), syntheticFlowBook(mark), syntheticFlowCandles(mark, now))
	return strategies.StrategyContext{
		Symbol:         strings.ToUpper(strings.TrimSpace(c.Entry.Symbol)),
		Now:            now,
		MarkPrice:      mark,
		IndexPrice:     mark,
		LastPrice:      mark,
		SpreadBps:      c.SpreadBps,
		VolumeRatio:    c.VolumeRatio,
		OIChangePct:    c.OFIZ,
		CandidateScore: c.Entry.CurrentScore,
		SessionVWAP:    c.SessionVWAP,
		WeeklyVWAP:     c.SessionVWAP,
		VWAPDistBps:    pctBps(mark, c.SessionVWAP),
		AnchoredVWAP:   avwapSnap,
		AVWAPLabel:     avwapSnap.Anchor.Label,
		Flow: flowSnap,
		Trend: strategies.TrendSnapshot{
			TF5mDir:         trend5,
			TF15mDir:        trend15,
			Slope5m:         c.FastSlope,
			Slope15m:        c.SlowSlope,
			Compression:     c.Entry.State == inplay.StateBalanced || c.Entry.State == inplay.StateHeating,
			ImpulseUp:       impulseUp,
			ImpulseDown:     impulseDown,
			BreakoutLevel:   mark,
			BreakdownLevel:  mark,
			CompressionHigh: mark * 1.001,
			CompressionLow:  mark * 0.999,
		},
		MarketRegime: c.SessionLabel,
		Raw:          c,
	}
}

func pctBps(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	return ((a - b) / a) * 10000.0
}

func buildUnknownIntent(c candidate, ctx strategies.StrategyContext, now time.Time) *strategies.EntryIntent {
	side := strategies.SideLong
	if strings.EqualFold(strings.TrimSpace(c.Side), "SELL") {
		side = strategies.SideShort
	}
	entry := c.LastClose
	if entry <= 0 {
		entry = ctx.MarkPrice
	}
	stop := c.LastClose
	if stop <= 0 {
		stop = entry
	}
	return &strategies.EntryIntent{
		Strategy:     strategies.StrategyUnknown,
		Symbol:       ctx.Symbol,
		Side:         side,
		Timeframe:    "1m",
		Confidence:   c.Conf,
		Score:        c.Entry.CurrentScore,
		TriggerPrice: entry,
		Invalidation: stop,
		StopPrice:    stop,
		Targets:      nil,
		TimeStopBars: 0,
		ReasonCodes:  []string{firstNonEmpty(c.Strat, "legacy_entry")},
		Features: map[string]float64{
			"candidate_score": c.Entry.CurrentScore,
			"volume_ratio":    c.VolumeRatio,
			"spread_bps":      c.SpreadBps,
		},
		CreatedAt: now,
	}
}

func decideUnifiedEntryAt(c candidate, acct accountHealthSummary, now time.Time) UnifiedEntryDecision {
	simple := decideSimpleEntryNowLegacyAt(c, acct, now)
	ctx := strategyContextFromCandidate(c, now)
	entry := DecideEntry(ctx, strategies.NewDefaultRouter(nil), nil)
	openAllStrategies := envBool("LIVE_OPEN_ALL_STRATEGIES", false)
	requireSimpleGate := envBool("LIVE_REQUIRE_SIMPLE_GATE", !openAllStrategies)
	if !simple.Allowed && requireSimpleGate {
		entry.Allowed = false
		if simple.Reason != "" {
			entry.RejectReason = simple.Reason
			entry.RejectCodes = append(entry.RejectCodes, simple.Reason)
		}
		return UnifiedEntryDecision{Simple: simple, Entry: entry, Context: ctx}
	}
	if entry.Intent == nil {
		entry.Intent = buildUnknownIntent(c, ctx, now)
	}
	entry.Allowed = true
	if entry.FinalScore <= 0 {
		entry.FinalScore = c.Entry.CurrentScore
	}
	return UnifiedEntryDecision{Simple: simple, Entry: entry, Context: ctx}
}

func choosePrimaryLiveSignal(c candidate, now time.Time) candidate {
	if reason, blocked := hardBlockEntry(c); blocked {
		c.Strat = "none"
		c.Conf = 0
		c.RejectReason = reason
		logSimpleDecision(c, false, reason)
		return c
	}

	dec := decideUnifiedEntryAt(c, currentAccountHealth(), now)
	openAllStrategies := envBool("LIVE_OPEN_ALL_STRATEGIES", false)
	requireSimpleGate := envBool("LIVE_REQUIRE_SIMPLE_GATE", !openAllStrategies)
	if dec.Entry.Allowed && (dec.Simple.Allowed || !requireSimpleGate) {
		posture, postureReason := chooseEntryPosture(c, dec, currentAccountHealth())
		c.EntryPosture = string(posture)
		c.EntryPostureReason = postureReason
		logEntryPosture(c, dec, posture, postureReason)
		if envBool("LIVE_POSTURE_GOV_ENABLE", false) {
			switch posture {
			case PostureWaitPullback, PostureWaitReclaim:
				c.Strat = "none"
				c.Conf = 0
				c.RejectReason = firstNonEmpty(postureReason, "posture_wait")
				logSimpleDecision(c, false, c.RejectReason)
				logEntryReject(dec.Entry, dec.Context)
				return c
			case PostureBlockExhausted, PostureBlockWeakFlow, PostureBlockRisk:
				c.Strat = "none"
				c.Conf = 0
				c.RejectReason = firstNonEmpty(postureReason, "posture_block")
				logSimpleDecision(c, false, c.RejectReason)
				logEntryReject(dec.Entry, dec.Context)
				return c
			}
		}
		signal := "entry_now_" + strings.ToLower(strings.TrimSpace(dec.Simple.Side))
		if openAllStrategies {
			signal = strategySignalName(dec)
		}
		c.Strat = signal
		if dec.Entry.Intent != nil {
			c.StrategyID = string(dec.Entry.Intent.Strategy)
			c.SetupFamily = c.StrategyID
		}
		c.Conf = clamp(0.50+min(0.30, max(0, c.Entry.CurrentScore-85.0)*0.01+max(0, c.Entry.ScoreSlope-0.20)*0.80), 0, 0.92)
		c.Sig = strategies.Signal{
			Active:       true,
			Name:         signal,
			Side:         toFeatureSide(c.Side),
			Confidence:   c.Conf,
			RejectReason: "",
			Reasons:      []string{firstNonEmpty(dec.Simple.Reason, signal)},
			Tags:         []string{"router_entry"},
		}
		if strings.TrimSpace(c.StrategyID) != "" {
			c.Sig.SignalSource = append(c.Sig.SignalSource, c.StrategyID)
		}
		c.Sig = applySignalRiskGeometry(c, signal)
		c.RejectReason = ""
		if posture == PostureStarterNow {
			c.Sig.Tags = append(c.Sig.Tags, "posture:starter_now")
		} else if posture == PostureAttackNow {
			c.Sig.Tags = append(c.Sig.Tags, "posture:attack_now")
		}
		logSimpleDecision(c, true, dec.Simple.Reason)
		logEntryAllow(dec.Entry, dec.Context)
		return c
	}

	c.Strat = "none"
	c.Conf = 0
	c.RejectReason = "no_simple_entry"
	logSimpleDecision(c, false, firstNonEmpty(strings.TrimSpace(dec.Simple.Reason), strings.TrimSpace(dec.Entry.RejectReason), "no_simple_entry"))
	logEntryReject(dec.Entry, dec.Context)
	return c
}

func chooseEntryPosture(c candidate, dec UnifiedEntryDecision, acct accountHealthSummary) (EntryPosture, string) {
	if !dec.Simple.Allowed || !dec.Entry.Allowed {
		return PostureBlockRisk, "simple_or_entry_not_allowed"
	}
	if reason := simpleOperationalBlockReason(c); reason != "" {
		return PostureBlockRisk, reason
	}
	if candidateSpikeCandle(c) || c.Entry.ExhaustionRisk >= envFloat("LIVE_TRUE_EXHAUSTION_RISK", 5.5) {
		if c.ReclaimHold || c.RetestHold {
			return PostureWaitReclaim, "exhausted_wait_reclaim"
		}
		return PostureBlockExhausted, "exhausted"
	}
	if c.SpreadBps > envFloat("LIVE_MAX_SPREAD_BPS", envFloat("LIVE_OB_MAX_SPREAD_BPS", 10)) {
		return PostureBlockRisk, "spread_too_wide"
	}
	flowWeak := c.TriggerScore < envFloat("LIVE_POSTURE_MIN_TRIGGER_SCORE", 0.50) && c.ExecutionScore < envFloat("LIVE_POSTURE_MIN_EXEC_SCORE", 0.18)
	if flowWeak {
		return PostureBlockWeakFlow, "weak_flow"
	}
	if isEliteLeaderPosture(c) {
		if c.ReclaimHold || c.ClosedBreakHold || c.RetestHold || waveImpulseTrigger(strings.ToUpper(strings.TrimSpace(c.TriggerState))) {
			if c.TriggerScore >= envFloat("LIVE_POSTURE_ATTACK_TRIGGER_SCORE", 0.62) && c.CombinedScore >= envFloat("LIVE_POSTURE_ATTACK_COMBO_SCORE", 0.62) {
				return PostureAttackNow, "elite_leader_attack"
			}
			return PostureStarterNow, "elite_leader_starter"
		}
		return PostureWaitReclaim, "elite_wait_reclaim"
	}
	if isBGradePosture(c) {
		if c.ReclaimHold || c.ClosedBreakHold || c.RetestHold {
			return PostureStarterNow, "b_grade_structure_starter"
		}
		return PostureWaitPullback, "b_grade_wait_pullback"
	}
	if c.ReclaimHold || c.ClosedBreakHold {
		return PostureStarterNow, "structure_starter"
	}
	return PostureWaitReclaim, "default_wait_reclaim"
}

func strategySignalName(dec UnifiedEntryDecision) string {
	side := strings.ToLower(strings.TrimSpace(dec.Simple.Side))
	if dec.Entry.Intent == nil {
		if side == "" {
			side = "long"
		}
		return "entry_now_" + side
	}
	if side == "" {
		if dec.Entry.Intent.Side == strategies.SideShort {
			side = "short"
		} else {
			side = "long"
		}
	}
	switch dec.Entry.Intent.Strategy {
	case strategies.StrategyImpulseContinuation:
		return "continuation_fast"
	case strategies.StrategyAnchoredVWAPPullback:
		if side == "short" {
			return "pullback_short"
		}
		return "pullback_long"
	case strategies.StrategyVPRetest:
		return "vp_retest"
	default:
		return "entry_now_" + side
	}
}

func isEliteLeaderPosture(c candidate) bool {
	grade := strings.ToUpper(strings.TrimSpace(c.Entry.CurrentGrade))
	if grade != "A+" && grade != "A" {
		return false
	}
	topRank := c.Entry.Rank > 0 && c.Entry.Rank <= envFloat("LIVE_POSTURE_ELITE_MAX_RANK", 3.0)
	strongSlope := c.Entry.ScoreSlope >= envFloat("LIVE_POSTURE_ELITE_MIN_SLOPE", 0.18)
	strongDay := c.DayUTC24h >= waveDayUTCThreshold(c, "LONG")
	return topRank && strongSlope && strongDay
}

func isBGradePosture(c candidate) bool {
	grade := strings.ToUpper(strings.TrimSpace(c.Entry.CurrentGrade))
	return grade == "B" || grade == "B+"
}

func logEntryPosture(c candidate, dec UnifiedEntryDecision, posture EntryPosture, reason string) {
	if !envBool("LIVE_POSTURE_LOG_ENABLE", true) {
		return
	}
	log.Printf("ENTRY_POSTURE symbol=%s side=%s grade=%s rank=%.2f final_rank=%.2f score=%.2f slope=%.3f state=%s dayUTC=%+.2f trigger=%s strat=%s posture=%s reason=%s spread_bps=%.2f recent_exit=%s cooldown=%t",
		strings.ToUpper(strings.TrimSpace(c.Entry.Symbol)),
		firstNonEmpty(strings.ToUpper(strings.TrimSpace(dec.Simple.Side)), strings.ToUpper(strings.TrimSpace(c.Side))),
		strings.ToUpper(strings.TrimSpace(c.Entry.CurrentGrade)),
		c.Entry.Rank,
		c.FinalRank,
		c.Entry.CurrentScore,
		c.Entry.ScoreSlope,
		strings.ToLower(strings.TrimSpace(string(c.Entry.State))),
		c.DayUTC24h,
		strings.ToUpper(strings.TrimSpace(c.TriggerState)),
		strings.TrimSpace(c.Strat),
		string(posture),
		firstNonEmpty(strings.TrimSpace(reason), "none"),
		c.SpreadBps,
		strings.ToUpper(strings.TrimSpace(c.RejectReason)),
		strings.Contains(strings.ToLower(strings.TrimSpace(c.RejectReason)), "cooldown"),
	)
}

func decideSimpleEntryNow(c candidate, acct accountHealthSummary) SimpleEntryDecision {
	return decideSimpleEntryNowAt(c, acct, time.Now().UTC())
}

func decideSimpleEntryNowAt(c candidate, acct accountHealthSummary, now time.Time) SimpleEntryDecision {
	dec := decideUnifiedEntryAt(c, acct, now)
	out := dec.Simple
	if strings.TrimSpace(out.Reason) == "" {
		out.Reason = firstNonEmpty(strings.TrimSpace(dec.Entry.RejectReason), "no_simple_entry")
	}
	return out
}

func decideSimpleEntryNowLegacyAt(c candidate, acct accountHealthSummary, now time.Time) SimpleEntryDecision {
	side := simpleEntrySide(c.Side)
	marketTs := candidateMarketSnapshotAt(c, now)
	accountTs := accountSnapshotAt(acct, now)
	if side == "" {
		return SimpleEntryDecision{Allowed: false, Reason: "side_unknown", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	staleData := false
	if dataAge := now.Sub(marketTs); dataAge > time.Duration(envInt("LIVE_SIMPLE_MAX_DATA_AGE_SEC", 3))*time.Second {
		staleData = true
	}
	if reason, blocked := entriesBlockedByAccountHealth(acct); blocked {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: reason, MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if reason := simpleOperationalBlockReason(c); reason != "" {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: reason, MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	confluenceHardGate := envBool("LIVE_SIMPLE_CONFLUENCE_HARD_GATE", false)
	if confluenceScorePct, ok := candidateConfluenceScorePct(c); ok {
		if confluenceHardGate && confluenceScorePct < envFloat("LIVE_CONFLUENCE_MIN_SCORE", 70.0) {
			return SimpleEntryDecision{Allowed: false, Side: side, Reason: "confluence_below_min", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
		}
		if confluenceHardGate && confluenceScorePct >= envFloat("LIVE_CONFLUENCE_WATCH_MIN_SCORE", 70.0) &&
			confluenceScorePct < envFloat("LIVE_CONFLUENCE_AUTO_ENTRY_MIN_SCORE", 85.0) {
			return SimpleEntryDecision{Allowed: false, Side: side, Reason: "watchlist_wait_orderflow", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
		}
	}
	if !simpleStateAllowed(c.Entry.State, side) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "state_not_allowed", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if envBool("LIVE_WAVE_ENTRY_RULES_ENABLE", false) {
		if reason := waveEntryReadinessRejectReason(c, side); reason != "" {
			return SimpleEntryDecision{Allowed: false, Side: side, Reason: reason, MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
		}
	}
	if c.Entry.CurrentScore < simpleMinScore(side) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "low_score", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if c.Entry.ScoreSlope < simpleMinSlope(side) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "weak_slope", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if c.ExtensionATR >= envFloat("LIVE_TRUE_EXTENSION_ATR", 2.25) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "extended", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if candidateSpikeCandle(c) || c.Entry.ExhaustionRisk >= envFloat("LIVE_TRUE_EXHAUSTION_RISK", 5.5) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "exhausted", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if c.SpreadBps > envFloat("LIVE_MAX_SPREAD_BPS", envFloat("LIVE_OB_MAX_SPREAD_BPS", 10)) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "spread_too_wide", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if reason := directionalConflictRejectReason(c); reason != "" {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: reason, MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if block, reason := reentryGuardReject(c, side, now); block {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: reason, MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if !simpleEntryLeaderEligible(c) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "not_top_leader", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	reason := "entry_now_" + strings.ToLower(side)
	if staleData {
		reason = reason + "_stale_warn"
	}
	return SimpleEntryDecision{
		Allowed:           true,
		Side:              side,
		Reason:            reason,
		MarketSnapshotTs:  marketTs,
		AccountSnapshotTs: accountTs,
	}
}

func waveEntryReadinessRejectReason(c candidate, side string) string {
	trigger := strings.ToUpper(strings.TrimSpace(c.TriggerState))
	state := c.Entry.State
	structureOK := c.ReclaimHold || c.ClosedBreakHold || c.RetestHold
	impulseTrigger := waveImpulseTrigger(trigger)
	reclaimTrigger := waveReclaimTrigger(trigger)
	if envBool("LIVE_WAVE_REQUIRE_SLOPE_STREAK", false) && !waveSlopeStreakReady(c) {
		return "slope_streak_not_ready"
	}

	dayUTCThresh := waveDayUTCThreshold(c, side)
	if side == "LONG" {
		if c.DayUTC24h < dayUTCThresh {
			return "dayutc_below_wave_threshold"
		}
		if state == inplay.StateBalanced && !(c.ReclaimHold && reclaimTrigger) {
			return "wait_reclaim_from_balance"
		}
		if trigger == "OF_EXHAUSTION" && !c.ReclaimHold {
			return "wait_pullback_reclaim"
		}
		if state == inplay.StateHeating && !structureOK && !impulseTrigger {
			return "setup_wait_structure"
		}
		if state == inplay.StateInPlay && !structureOK && !impulseTrigger && !reclaimTrigger {
			return "inplay_needs_structure_or_trigger"
		}
		return ""
	}

	// SHORT wave rules mirror long rules with downside dayUTC.
	if c.DayUTC24h > -dayUTCThresh {
		return "dayutc_above_short_wave_threshold"
	}
	if state == inplay.StateBalanced && !(c.ReclaimHold && reclaimTrigger) {
		return "wait_reclaim_from_balance"
	}
	if trigger == "OF_EXHAUSTION" && !c.ReclaimHold {
		return "wait_pullback_reclaim"
	}
	if state == inplay.StateHeating && !structureOK && !impulseTrigger {
		return "setup_wait_structure"
	}
	if state == inplay.StateInPlay && !structureOK && !impulseTrigger && !reclaimTrigger {
		return "inplay_needs_structure_or_trigger"
	}
	return ""
}

func waveSlopeStreakReady(c candidate) bool {
	sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if sym == "" {
		return false
	}
	minSlope := envFloat("LIVE_WAVE_MIN_POSITIVE_SLOPE", 0.05)
	req := envInt("LIVE_WAVE_MIN_RISING_STREAK", 2)
	if req < 1 {
		req = 1
	}
	now := time.Now().UTC()
	waveSlopeMu.Lock()
	defer waveSlopeMu.Unlock()
	st := waveSlopeStateBySym[sym]
	cur := c.Entry.ScoreSlope
	if cur > minSlope && cur >= st.LastSlope {
		st.RisingStreak++
	} else if cur > minSlope {
		st.RisingStreak = 1
	} else {
		st.RisingStreak = 0
	}
	st.LastSlope = cur
	st.LastSeen = now
	waveSlopeStateBySym[sym] = st
	return st.RisingStreak >= req
}

func waveImpulseTrigger(trigger string) bool {
	switch trigger {
	case "OF_IMPULSE_CONT", "OF_DELTA_FLIP", "OF_ABSORB":
		return true
	default:
		return false
	}
}

func waveReclaimTrigger(trigger string) bool {
	switch trigger {
	case "OF_RECLAIM", "OF_DELTA_FLIP", "OF_IMPULSE_CONT":
		return true
	default:
		return false
	}
}

func waveDayUTCThreshold(c candidate, side string) float64 {
	if isWaveMajorSymbol(c.Entry.Symbol) {
		if side == "SHORT" {
			return envFloat("LIVE_WAVE_SHORT_DAYUTC_MAJOR", 10.0)
		}
		return envFloat("LIVE_WAVE_LONG_DAYUTC_MAJOR", 10.0)
	}
	if side == "SHORT" {
		return envFloat("LIVE_WAVE_SHORT_DAYUTC_MICRO", 15.0)
	}
	return envFloat("LIVE_WAVE_LONG_DAYUTC_MICRO", 15.0)
}

func isWaveMajorSymbol(sym string) bool {
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(sym)))
	switch raw {
	case "BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT":
		return true
	default:
		return false
	}
}

func reentryGuardReject(c candidate, side string, now time.Time) (bool, string) {
	if !reentryGuardEnabled {
		return false, ""
	}
	sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if sym == "" {
		return false, ""
	}
	strategyID := strings.TrimSpace(c.StrategyID)
	if strategyID == "" {
		strategyID = strings.TrimSpace(c.Strat)
	}
	reentryGuardMu.Lock()
	rec := reentryGuardBySym[sym]
	reentryGuardMu.Unlock()
	block, reason := exitmgr.ShouldBlockReentry(sym, strategyID, side, rec, now, c.Entry.CurrentScore)
	return block, reason
}

func registerReentryLoss(symbol, strategyID, side string, score float64, now time.Time) {
	sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if sym == "" {
		return
	}
	reentryGuardMu.Lock()
	defer reentryGuardMu.Unlock()
	rec := reentryGuardBySym[sym]
	if !rec.LastLossTime.IsZero() && now.Sub(rec.LastLossTime) > time.Duration(envInt("LIVE_REENTRY_LOSS_COOLDOWN_MIN", 15))*time.Minute {
		rec.LossCount = 0
	}
	rec.LastLossTime = now
	rec.LossCount++
	rec.LastStrategy = strings.TrimSpace(strategyID)
	rec.LastSide = strings.TrimSpace(side)
	rec.LastStopScore = score
	reentryGuardBySym[sym] = rec
}

func registerReentryExit(symbol, strategyID, side, reason string, score, maxFavorR float64, wasLoss bool, now time.Time) {
	sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if sym == "" {
		return
	}
	reentryGuardMu.Lock()
	defer reentryGuardMu.Unlock()
	rec := reentryGuardBySym[sym]
	rec.LastExitTime = now
	rec.LastExitReason = strings.TrimSpace(reason)
	rec.LastExitWasLoss = wasLoss
	rec.LastExitMaxFavorR = maxFavorR
	rec.LastStrategy = strings.TrimSpace(strategyID)
	rec.LastSide = strings.TrimSpace(side)
	rec.LastStopScore = score
	if wasLoss {
		if !rec.LastLossTime.IsZero() && now.Sub(rec.LastLossTime) > time.Duration(envInt("LIVE_REENTRY_LOSS_COOLDOWN_MIN", 15))*time.Minute {
			rec.LossCount = 0
		}
		rec.LastLossTime = now
		rec.LossCount++
	}
	reentryGuardBySym[sym] = rec
}

func isSoftChurnExit(reason string) bool {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "NO_FOLLOW_THROUGH", "NO_FOLLOW_THROUGH_TIGHTEN", "MOMENTUM_FADE", "PROFIT_GIVEBACK":
		return true
	default:
		return false
	}
}

func clearReentryLoss(symbol string) {
	sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if sym == "" {
		return
	}
	reentryGuardMu.Lock()
	defer reentryGuardMu.Unlock()
	delete(reentryGuardBySym, sym)
}

func logEntryReject(decision strategies.EntryDecision, ctx strategies.StrategyContext) {
	strategyID := strategies.StrategyUnknown
	symbol := ctx.Symbol
	trigger := 0.0
	stop := 0.0
	score := decision.FinalScore
	if decision.Intent != nil {
		strategyID = decision.Intent.Strategy
		symbol = decision.Intent.Symbol
		trigger = decision.Intent.TriggerPrice
		stop = decision.Intent.StopPrice
		if score <= 0 {
			score = decision.Intent.Score
		}
	}
	log.Printf("entry_reject symbol=%s strategy_id=%s trigger=%.8f stop=%.8f score=%.2f hard_blocks=%q confirm_blocks=%q spread_bps=%.2f vwap_dist_bps=%.2f flow_summary=%q reject_reason=%q",
		symbol, strategyID, trigger, stop, score, decision.HardBlocks, decision.RejectCodes, ctx.SpreadBps, ctx.VWAPDistBps, ctx.Flow.Summary, firstNonEmpty(decision.RejectReason, "rejected"))
}

func logEntryAllow(decision strategies.EntryDecision, ctx strategies.StrategyContext) {
	if decision.Intent == nil {
		return
	}
	log.Printf("entry_allow symbol=%s strategy_id=%s side=%s trigger=%.8f stop=%.8f score=%.2f spread_bps=%.2f vwap_dist_bps=%.2f flow_summary=%q",
		decision.Intent.Symbol, decision.Intent.Strategy, decision.Intent.Side, decision.Intent.TriggerPrice, decision.Intent.StopPrice, decision.FinalScore, ctx.SpreadBps, ctx.VWAPDistBps, ctx.Flow.Summary)
	if decision.Intent.Strategy == strategies.StrategyAnchoredVWAPPullback {
		log.Printf("entry_avwap symbol=%s strategy_id=%s avwap_label=%q avwap=%.8f dev1_upper=%.8f dev1_lower=%.8f avwap_slope=%.8f",
			ctx.Symbol, decision.Intent.Strategy, ctx.AVWAPLabel, ctx.AnchoredVWAP.VWAP, ctx.AnchoredVWAP.Dev1Upper, ctx.AnchoredVWAP.Dev1Lower, ctx.AnchoredVWAP.Slope)
	}
}

func candidateMarketSnapshotAt(c candidate, now time.Time) time.Time {
	if !c.Entry.LastSeen.IsZero() {
		return c.Entry.LastSeen
	}
	if !c.Entry.StateSince.IsZero() {
		return c.Entry.StateSince
	}
	if !c.Entry.FirstSeen.IsZero() {
		return c.Entry.FirstSeen
	}
	return now
}

func accountSnapshotAt(acct accountHealthSummary, now time.Time) time.Time {
	if !acct.AsOf.IsZero() {
		return acct.AsOf
	}
	return now
}

func candidateConfluenceScorePct(c candidate) (float64, bool) {
	if c.Sig.ConfluenceScore.TotalScore > 0 {
		total := c.Sig.ConfluenceScore.TotalScore
		if total <= 1 {
			total *= 100
		}
		return total, true
	}
	if c.Sig.Confluence == nil {
		return 0, false
	}
	total, ok := c.Sig.Confluence["total"]
	if !ok || total <= 0 {
		return 0, false
	}
	if total <= 1 {
		total *= 100
	}
	return total, true
}

func simpleEntrySide(side string) string {
	if strings.EqualFold(strings.TrimSpace(side), "SELL") {
		return "SHORT"
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		return "LONG"
	}
	return ""
}

func simpleEntryLeaderEligible(c candidate) bool {
	maxRank := envFloat("LIVE_SIMPLE_ENTRY_MAX_RANK", 6.0)
	minFinalRank := envFloat("LIVE_SIMPLE_ENTRY_MIN_FINAL_RANK", 85.0)
	minCombined := envFloat("LIVE_SIMPLE_ENTRY_MIN_COMBINED", 0.70)
	if c.Entry.Rank > 0 && c.Entry.Rank <= maxRank {
		return true
	}
	if c.FinalRank >= minFinalRank {
		return true
	}
	if c.CombinedScore >= minCombined {
		return true
	}
	return simpleEntryActionableOverride(c)
}

func simpleEntryActionableOverride(c candidate) bool {
	side := simpleEntrySide(c.Side)
	if side == "" {
		return false
	}
	if !simpleStateAllowed(c.Entry.State, side) {
		return false
	}
	if c.Entry.CurrentScore < envFloat("LIVE_SIMPLE_ENTRY_ACTIONABLE_SCORE", 92.0) {
		return false
	}
	if c.Entry.ScoreSlope < envFloat("LIVE_SIMPLE_ENTRY_ACTIONABLE_SLOPE", 0.12) {
		return false
	}
	if c.ExtensionATR >= envFloat("LIVE_TRUE_EXTENSION_ATR", 2.25) {
		return false
	}
	if candidateSpikeCandle(c) || c.Entry.ExhaustionRisk >= envFloat("LIVE_TRUE_EXHAUSTION_RISK", 5.5) {
		return false
	}
	if c.SpreadBps > envFloat("LIVE_MAX_SPREAD_BPS", envFloat("LIVE_OB_MAX_SPREAD_BPS", 10)) {
		return false
	}
	if directionalConflictRejectReason(c) != "" {
		return false
	}
	if !candidatePriceConfirmsDirection(c) {
		return false
	}
	minVolRatio := envFloat("LIVE_SIMPLE_ENTRY_ACTIONABLE_MIN_VOL_RATIO", 0.90)
	return c.VolumeRatio >= minVolRatio || c.Entry.Momentum || continuationStructureConfirmed(c) || hasFreshStructureReset(c)
}

func simpleStateAllowed(st inplay.State, side string) bool {
	switch st {
	case inplay.StateInPlay, inplay.StateHeating:
		return true
	case inplay.StateBalanced, inplay.StateCooling, inplay.StateExhausted:
		return false
	case inplay.StateDumping:
		return false
	case inplay.StatePumping:
		return false
	default:
		return false
	}
}

func simpleMinScore(side string) float64 {
	if strings.EqualFold(side, "SHORT") {
		return envFloat("LIVE_SIMPLE_SHORT_MIN_SCORE", 85.0)
	}
	return envFloat("LIVE_SIMPLE_LONG_MIN_SCORE", 85.0)
}

func simpleMinSlope(side string) float64 {
	if strings.EqualFold(side, "SHORT") {
		return envFloat("LIVE_SIMPLE_SHORT_MIN_SLOPE", 0.20)
	}
	return envFloat("LIVE_SIMPLE_LONG_MIN_SLOPE", 0.20)
}

func shouldSuppressSimpleDecisionLog(c candidate, allowed bool, reason string) bool {
	if envBool("LIVE_SIMPLE_DECISION_LOG_ALL", false) || envBool("LIVE_SIMPLE_DECISION_DISABLE_SUPPRESS", false) {
		return false
	}
	if !suppressibleRepeatReject(reason) {
		return false
	}
	ttl := time.Duration(envInt("LIVE_SIMPLE_DECISION_SUPPRESS_TTL_SEC", 45)) * time.Second
	if ttl <= 0 {
		return false
	}
	key := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol))) + "|" + strings.ToUpper(strings.TrimSpace(c.Side)) + "|" + firstNonEmpty(strings.TrimSpace(reason), "none")
	now := time.Now().UTC()
	simpleDecisionLogMu.Lock()
	defer simpleDecisionLogMu.Unlock()
	prev, ok := simpleDecisionLogMem[key]
	if ok {
		if now.After(prev.ExpiresAt) {
			delete(simpleDecisionLogMem, key)
		} else if prev.Allowed == allowed &&
			prev.Reason == strings.TrimSpace(reason) &&
			prev.State == strings.TrimSpace(string(c.Entry.State)) &&
			abs(prev.Score-c.Entry.CurrentScore) < envFloat("LIVE_REPEAT_REJECT_SCORE_DELTA", 2.5) &&
			abs(prev.Slope-c.Entry.ScoreSlope) < envFloat("LIVE_REPEAT_REJECT_SLOPE_DELTA", 0.05) &&
			abs(prev.SpreadBps-c.SpreadBps) < envFloat("LIVE_REPEAT_REJECT_SPREAD_DELTA_BPS", 2.0) &&
			abs(prev.ExtensionATR-c.ExtensionATR) < envFloat("LIVE_REPEAT_REJECT_EXTENSION_DELTA", 0.20) {
			prev.ExpiresAt = now.Add(ttl)
			simpleDecisionLogMem[key] = prev
			return true
		}
	}
	simpleDecisionLogMem[key] = simpleDecisionLogState{
		Reason:       strings.TrimSpace(reason),
		Allowed:      allowed,
		Score:        c.Entry.CurrentScore,
		Slope:        c.Entry.ScoreSlope,
		State:        strings.TrimSpace(string(c.Entry.State)),
		SpreadBps:    c.SpreadBps,
		ExtensionATR: c.ExtensionATR,
		ExpiresAt:    now.Add(ttl),
	}
	return false
}

func logSimpleDecision(c candidate, allowed bool, reason string) {
	if !shouldLogSimpleDecision(c) {
		return
	}
	if shouldSuppressSimpleDecisionLog(c, allowed, reason) {
		return
	}
	side := simpleEntrySide(c.Side)
	if side == "" {
		side = strings.ToUpper(strings.TrimSpace(c.Side))
	}
	acctReason, acctBlocked := entriesBlockedByAccountHealth(currentAccountHealth())
	acctOK := !acctBlocked
	if !acctOK && reason == "" {
		reason = acctReason
	}
	log.Printf("SIMPLE_DECISION sym=%s side=%s score=%.2f slope=%.3f state=%s extended=%d exhausted=%d spread_ok=%d acct_ok=%d allowed=%d reason=%s",
		strings.ToUpper(strings.TrimSpace(c.Entry.Symbol)),
		side,
		c.Entry.CurrentScore,
		c.Entry.ScoreSlope,
		strings.ToLower(strings.TrimSpace(string(c.Entry.State))),
		boolInt(c.ExtensionATR >= envFloat("LIVE_TRUE_EXTENSION_ATR", 2.25)),
		boolInt(candidateSpikeCandle(c) || (c.Entry.ExhaustionRisk >= envFloat("LIVE_TRUE_EXHAUSTION_RISK", 5.5))),
		boolInt(c.SpreadBps <= envFloat("LIVE_MAX_SPREAD_BPS", envFloat("LIVE_OB_MAX_SPREAD_BPS", 10))),
		boolInt(acctOK),
		boolInt(allowed),
		firstNonEmpty(strings.TrimSpace(reason), "no_simple_entry"))
	logDetailedDecision("live", c, side, allowed, reason)
}

func shouldLogSimpleDecision(c candidate) bool {
	if envBool("LIVE_SIMPLE_DECISION_LOG_ALL", false) {
		return true
	}
	if c.Entry.Rank > 0 && c.Entry.Rank <= envFloat("LIVE_SIMPLE_DECISION_LOG_MAX_RANK", 6.0) {
		return true
	}
	return c.FinalRank >= envFloat("LIVE_SIMPLE_DECISION_LOG_MIN_FINAL_RANK", 85.0)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func decideSimplePaperEntryNow(c candidate, acct accountHealthSummary) SimplePaperDecision {
	side := simpleEntrySide(c.Side)
	if side == "" {
		return SimplePaperDecision{Allowed: false, Reason: "side_unknown"}
	}
	if envBool("LIVE_PAPER_SYNC_WITH_LIVE", true) {
		liveDec := decideSimpleEntryNow(c, acct)
		return SimplePaperDecision{
			Allowed:           liveDec.Allowed,
			Side:              liveDec.Side,
			Reason:            firstNonEmpty(liveDec.Reason, "no_simple_entry"),
			PullbackPreferred: c.Entry.CurrentScore >= envFloat("LIVE_PAPER_SIMPLE_PULLBACK_SCORE", 100.0),
		}
	}
	if reason, blocked := entriesBlockedByPaperAccountHealth(acct); blocked {
		return SimplePaperDecision{Allowed: false, Side: side, Reason: reason}
	}
	if reason := simpleOperationalBlockReason(c); reason != "" {
		return SimplePaperDecision{Allowed: false, Side: side, Reason: reason}
	}
	if hardReason, blocked := paperEntryStructureBlock(c); blocked {
		return SimplePaperDecision{Allowed: false, Side: side, Reason: hardReason}
	}
	if !simpleEntryLeaderEligible(c) {
		return SimplePaperDecision{Allowed: false, Side: side, Reason: "not_top_leader"}
	}
	if gradeValue(c.Entry.CurrentGrade) < gradeValue("B") {
		return SimplePaperDecision{Allowed: false, Side: side, Reason: "grade_below_high_b"}
	}
	if c.Entry.CurrentScore < envFloat("LIVE_PAPER_SIMPLE_MIN_SCORE", 85.0) {
		return SimplePaperDecision{Allowed: false, Side: side, Reason: "low_score"}
	}
	if !paperSimpleVolumeOK(c) {
		return SimplePaperDecision{Allowed: false, Side: side, Reason: "volume_not_rising_or_strong"}
	}
	moveMin := envFloat("LIVE_PAPER_SIMPLE_MOVE_MIN_PCT", 3.0)
	dayMove := c.DayUTC24h
	if side == "LONG" {
		if dayMove < moveMin {
			return SimplePaperDecision{Allowed: false, Side: side, Reason: "reset_move_below_threshold"}
		}
		if !paperSimpleLongStateOK(c) {
			return SimplePaperDecision{Allowed: false, Side: side, Reason: "state_not_long_ready"}
		}
		return SimplePaperDecision{
			Allowed:           true,
			Side:              side,
			Reason:            "paper_entry_now_long",
			PullbackPreferred: c.Entry.CurrentScore >= envFloat("LIVE_PAPER_SIMPLE_PULLBACK_SCORE", 100.0),
		}
	}
	if !paperSimpleShortAllowed(c, moveMin) {
		return SimplePaperDecision{Allowed: false, Side: side, Reason: "short_not_ready"}
	}
	return SimplePaperDecision{
		Allowed:           true,
		Side:              side,
		Reason:            "paper_entry_now_short",
		PullbackPreferred: false,
	}
}

func paperSpreadWithinValidationLimit(c candidate) bool {
	liveMax := envFloat("LIVE_MAX_SPREAD_BPS", envFloat("LIVE_OB_MAX_SPREAD_BPS", 10))
	paperMax := envFloat("LIVE_PAPER_MAX_SPREAD_BPS", maxFloat(20.0, liveMax*2.5))
	return c.SpreadBps <= paperMax
}

func paperEntryStructureBlock(c candidate) (string, bool) {
	paperExt := envFloat("LIVE_PAPER_TRUE_EXTENSION_ATR", envFloat("LIVE_TRUE_EXTENSION_ATR", 2.25)*1.45)
	if c.ExtensionATR >= paperExt {
		return "extended", true
	}
	paperExhaust := envFloat("LIVE_PAPER_TRUE_EXHAUSTION_RISK", envFloat("LIVE_TRUE_EXHAUSTION_RISK", 5.5)+1.5)
	if candidateSpikeCandle(c) || c.Entry.ExhaustionRisk >= paperExhaust {
		return "exhausted", true
	}
	if !paperSpreadWithinValidationLimit(c) {
		return "spread_too_wide", true
	}
	if envBool("LIVE_PAPER_BLOCK_ON_DIRECTIONAL_CONFLICT", false) {
		if reason := directionalConflictRejectReason(c); reason != "" {
			return reason, true
		}
	}
	return "", false
}

func entriesBlockedByPaperAccountHealth(summary accountHealthSummary) (string, bool) {
	if summary.State == "failed" {
		return "account_health_failed", true
	}
	if summary.State == "partial" && envBool("LIVE_PAPER_BLOCK_ON_PARTIAL_HEALTH", false) {
		return "account_health_partial", true
	}
	return "", false
}

func simpleOperationalBlockReason(c candidate) string {
	blockers := []string{
		"account_health_failed",
		"symbol_cooldown",
		"symbol_loss_cooldown",
		"stopout_lock",
		"position_conflict",
		"opposite_position_open",
		"max_orders_per_day",
		"max_orders_per_hour",
		"pause_file_active",
		"risk_shell_reject",
		"insufficient_balance",
		"insufficient_usable_balance",
		"available_balance_hard_failure",
		"exec_cap_symbol_loss_cluster",
		"exec_cap_bucket_window",
		"exec_cap",
	}
	reject := strings.ToLower(strings.TrimSpace(c.RejectReason))
	if reject == "" {
		return ""
	}
	for _, token := range blockers {
		if strings.Contains(reject, token) {
			return token
		}
	}
	return ""
}

func isExecutionBlocked(c candidate) bool {
	reject := strings.ToLower(strings.TrimSpace(c.RejectReason))
	if reject == "" {
		return false
	}
	return strings.Contains(reject, "exec_cap") || strings.Contains(reject, "symbol_loss_cluster")
}

func shouldEmitEarlyOpportunity(c candidate, allowed bool) bool {
	if !envBool("LIVE_EARLY_OPPORTUNITY_ALERT_ENABLE", false) {
		return false
	}
	grade := strings.ToUpper(strings.TrimSpace(c.Entry.CurrentGrade))
	if grade != "A" && grade != "A+" {
		return false
	}
	if c.Entry.State != inplay.StateHeating && c.Entry.State != inplay.StateInPlay {
		return false
	}
	if c.Entry.ScoreSlope <= 0 {
		return false
	}
	side := simpleEntrySide(c.Side)
	if side == "" {
		return false
	}
	dayThr := waveDayUTCThreshold(c, side)
	if side == "LONG" && c.DayUTC24h < dayThr {
		return false
	}
	if side == "SHORT" && c.DayUTC24h > -dayThr {
		return false
	}
	if !(c.ReclaimHold || c.ClosedBreakHold) {
		return false
	}
	if isExecutionBlocked(c) {
		return false
	}
	return allowed
}

func paperSimpleVolumeOK(c candidate) bool {
	minStrong := envFloat("LIVE_PAPER_SIMPLE_MIN_VOL_RATIO", 1.0)
	if c.VolumeRatio >= minStrong {
		return true
	}
	// Advisory fallback: keep high-conviction movers tradable even when volume ratio is noisy.
	return c.Entry.Momentum || c.Entry.CurrentScore >= envFloat("LIVE_PAPER_SIMPLE_STRONG_SCORE_FALLBACK", 95.0)
}

func paperSimpleLongStateOK(c candidate) bool {
	switch c.Entry.State {
	case inplay.StateInPlay, inplay.StateHeating:
		return true
	case inplay.StateBalanced:
		// Balanced is advisory only for elite leaders in paper validation mode.
		return c.Entry.CurrentScore >= envFloat("LIVE_PAPER_SIMPLE_BALANCED_ELITE_SCORE", 95.0) && simpleEntryLeaderEligible(c)
	default:
		return false
	}
}

func paperSimpleShortAllowed(c candidate, moveMin float64) bool {
	downsideLeader := c.DayUTC24h <= -moveMin
	weakeningNow := c.Entry.LongDemotionFlag ||
		c.Entry.State == inplay.StateCooling ||
		c.Entry.State == inplay.StateDumping ||
		c.Entry.State == inplay.StateExhausted ||
		(!c.Entry.Momentum && c.Entry.ScoreSlope < 0)
	toppingLong := c.DayUTC24h >= moveMin && weakeningNow
	return downsideLeader || toppingLong
}

func logSimplePaperDecision(c candidate, dec SimplePaperDecision) {
	if !shouldLogSimpleDecision(c) {
		return
	}
	if shouldSuppressSimpleDecisionLog(c, dec.Allowed, dec.Reason) {
		return
	}
	log.Printf("SIMPLE_DECISION sym=%s side=%s score=%.2f slope=%.3f state=%s extended=%d exhausted=%d allowed=%d reason=%s",
		strings.ToUpper(strings.TrimSpace(c.Entry.Symbol)),
		firstNonEmpty(strings.ToUpper(strings.TrimSpace(dec.Side)), simpleEntrySide(c.Side)),
		c.Entry.CurrentScore,
		c.Entry.ScoreSlope,
		strings.ToLower(strings.TrimSpace(string(c.Entry.State))),
		boolInt(c.ExtensionATR >= envFloat("LIVE_TRUE_EXTENSION_ATR", 2.25)),
		boolInt(candidateSpikeCandle(c) || (c.Entry.ExhaustionRisk >= envFloat("LIVE_TRUE_EXHAUSTION_RISK", 5.5))),
		boolInt(dec.Allowed),
		firstNonEmpty(strings.TrimSpace(dec.Reason), "paper_no_simple_entry"))
	logDetailedDecision("paper", c, firstNonEmpty(strings.ToUpper(strings.TrimSpace(dec.Side)), simpleEntrySide(c.Side)), dec.Allowed, dec.Reason)
}

func logDetailedDecision(mode string, c candidate, side string, allowed bool, reason string) {
	if !envBool("LIVE_SIMPLE_DECISION_LOG_VERBOSE", true) {
		return
	}
	log.Printf("DECISION_TRACE mode=%s symbol=%s side=%s grade=%s rank=%.2f final_rank=%.2f score=%.2f slope=%.3f state=%s trigger_state=%s strat=%s disc=%.2f trig=%.2f exec=%.2f combo=%.2f dayUTC=%+.2f spread_bps=%.2f vol_ratio=%.2f ext_atr=%.2f break_hold=%t reclaim_hold=%t retest_hold=%t allowed=%d reason=%s",
		strings.ToLower(strings.TrimSpace(mode)),
		strings.ToUpper(strings.TrimSpace(c.Entry.Symbol)),
		firstNonEmpty(strings.ToUpper(strings.TrimSpace(side)), strings.ToUpper(strings.TrimSpace(c.Side))),
		strings.ToUpper(strings.TrimSpace(c.Entry.CurrentGrade)),
		c.Entry.Rank,
		c.FinalRank,
		c.Entry.CurrentScore,
		c.Entry.ScoreSlope,
		strings.ToLower(strings.TrimSpace(string(c.Entry.State))),
		strings.TrimSpace(c.TriggerState),
		strings.TrimSpace(c.Strat),
		c.DiscoveryScore,
		c.TriggerScore,
		c.ExecutionScore,
		c.CombinedScore,
		c.DayUTC24h,
		c.SpreadBps,
		c.VolumeRatio,
		c.ExtensionATR,
		c.ClosedBreakHold,
		c.ReclaimHold,
		c.RetestHold,
		boolInt(allowed),
		firstNonEmpty(strings.TrimSpace(reason), "no_reason"))
	if shouldEmitEarlyOpportunity(c, allowed) {
		log.Printf("EARLY_OPPORTUNITY symbol=%s side=%s grade=%s state=%s slope=%.3f dayUTC=%+.2f trigger_state=%s structure=%s reclaim_hold=%t break_hold=%t score=%.2f combo=%.2f",
			strings.ToUpper(strings.TrimSpace(c.Entry.Symbol)),
			firstNonEmpty(strings.ToUpper(strings.TrimSpace(side)), strings.ToUpper(strings.TrimSpace(c.Side))),
			strings.ToUpper(strings.TrimSpace(c.Entry.CurrentGrade)),
			strings.ToLower(strings.TrimSpace(string(c.Entry.State))),
			c.Entry.ScoreSlope,
			c.DayUTC24h,
			strings.TrimSpace(c.TriggerState),
			strings.TrimSpace(c.StructureReason),
			c.ReclaimHold,
			c.ClosedBreakHold,
			c.Entry.CurrentScore,
			c.CombinedScore)
	}
}

func hardBlockEntry(c candidate) (string, bool) {
	switch {
	case c.ExtensionATR >= envFloat("LIVE_TRUE_EXTENSION_ATR", 2.25):
		return "extended", true
	case candidateSpikeCandle(c) || (c.Entry.ExhaustionRisk >= envFloat("LIVE_TRUE_EXHAUSTION_RISK", 5.5)):
		return "exhausted", true
	case directionalConflictRejectReason(c) != "":
		return directionalConflictRejectReason(c), true
	case c.SpreadBps > envFloat("LIVE_MAX_SPREAD_BPS", envFloat("LIVE_OB_MAX_SPREAD_BPS", 10)):
		return "spread_too_wide", true
	default:
		return "", false
	}
}

func tryMomentumIgnite(c candidate, now time.Time) (candidate, bool) {
	if !envBool("LIVE_ENABLE_MOMENTUM_IGNITE", true) {
		return c, false
	}
	if resetName, resetConf, resetReasons := qualifiesResetImpulse(c, now); resetName != "" {
		c.Strat = resetName
		c.Conf = resetConf
		c.Sig = strategies.Signal{
			Active:       true,
			Name:         resetName,
			Side:         toFeatureSide(c.Side),
			Confidence:   resetConf,
			RejectReason: "",
			Reasons:      resetReasons,
			Tags:         []string{"reset_impulse"},
		}
		c.Sig = applySignalRiskGeometry(c, resetName)
		c.RejectReason = ""
		return c, true
	}

	igniteMinScore := envFloat("LIVE_IGNITE_MIN_SCORE", 60.0)
	igniteMinSlope := envFloat("LIVE_IGNITE_MIN_SLOPE", 0.08)
	igniteMinVolRatio := envFloat("LIVE_IGNITE_MIN_VOL_RATIO", 1.00)
	igniteMinOFIZ := envFloat("LIVE_IGNITE_MIN_OFI_Z", 0.60)
	igniteBaseConf := envFloat("LIVE_IGNITE_BASE_CONF", 0.56)
	igniteHeatMaxStateMin := envFloat("LIVE_IGNITE_HEATING_MAX_STATE_MIN", 14.0)
	igniteInPlayMaxStateMin := envFloat("LIVE_IGNITE_INPLAY_MAX_STATE_MIN", 8.0)

	structureOK := starterDirectionalContextOK(c)
	freshStateOK := false
	switch c.Entry.State {
	case inplay.StateHeating:
		freshStateOK = c.Entry.TimeInStateMin <= igniteHeatMaxStateMin
	case inplay.StateInPlay:
		freshStateOK = c.Entry.TimeInStateMin <= igniteInPlayMaxStateMin
	}

	styleMatch := false
	igniteName := ""
	if strings.EqualFold(c.Side, "BUY") {
		styleMatch = c.Entry.EntryStyle == "momentum_ignite_long"
		igniteName = "momentum_ignite_long"
	} else {
		styleMatch = c.Entry.EntryStyle == "momentum_ignite_short"
		igniteName = "momentum_ignite_short"
	}
	ofiReady := c.OFISamples < maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8))
	if !ofiReady {
		if strings.EqualFold(c.Side, "BUY") {
			ofiReady = c.OFIZ >= igniteMinOFIZ
		} else {
			ofiReady = c.OFIZ <= -igniteMinOFIZ
		}
	}
	if !(styleMatch && freshStateOK && c.Entry.Momentum && c.Entry.CurrentScore >= igniteMinScore &&
		c.Entry.ScoreSlope >= igniteMinSlope && c.VolumeRatio >= igniteMinVolRatio && ofiReady && structureOK) {
		return c, false
	}

	c.Strat = igniteName
	c.Conf = clamp(igniteBaseConf+min(0.20, max(0.0, c.Entry.ScoreSlope-igniteMinSlope)*0.35+max(0.0, c.VolumeRatio-igniteMinVolRatio)*0.10), 0, 0.86)
	c.Sig = strategies.Signal{Active: true, Name: igniteName, Side: toFeatureSide(c.Side)}
	c.Sig = applySignalRiskGeometry(c, igniteName)
	c.RejectReason = ""
	return c, true
}

func tryContinuationFast(c candidate, _ time.Time) (candidate, bool) {
	if !envBool("LIVE_ENABLE_CONTINUATION_FAST", true) {
		return c, false
	}
	if strings.EqualFold(c.Side, "BUY") && c.Entry.LongDemotionFlag {
		return c, false
	}
	if strings.EqualFold(c.Side, "SELL") && c.Entry.ShortDemotionFlag {
		return c, false
	}

	fastMinScore := envFloat("LIVE_CONT_FAST_MIN_SCORE", 65.0)
	fastMinSlope := envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02)
	fastMinVolRatio := envFloat("LIVE_CONT_FAST_MIN_VOL_RATIO", 1.15)
	fastMinOFIZ := envFloat("LIVE_CONT_FAST_MIN_OFI_Z", 0.35)
	fastBaseConf := envFloat("LIVE_CONT_FAST_BASE_CONF", 0.58)
	structureConfirmOK := continuationStructureConfirmed(c)
	vwapEMAOK := starterDirectionalContextOK(c)
	ofiOK := c.OFISamples < maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8))
	if !ofiOK {
		if strings.EqualFold(c.Side, "BUY") {
			ofiOK = c.OFIZ >= fastMinOFIZ
		} else {
			ofiOK = c.OFIZ <= -fastMinOFIZ
		}
	}
	stateOK := c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay || c.Entry.State == inplay.StatePumping

	if stateOK &&
		c.Entry.CurrentScore >= fastMinScore &&
		c.Entry.ScoreSlope >= fastMinSlope &&
		c.VolumeRatio >= fastMinVolRatio &&
		ofiOK &&
		structureConfirmOK &&
		vwapEMAOK {
		c.Strat = "continuation_fast"
		c.Conf = clamp(fastBaseConf+min(0.22, (c.VolumeRatio-fastMinVolRatio)*0.08+max(0.0, c.Entry.ScoreSlope-fastMinSlope)*0.30), 0, 0.90)
		c.Sig = strategies.Signal{Active: true, Name: "continuation_fast", Side: toFeatureSide(c.Side)}
		c.Sig = applySignalRiskGeometry(c, "continuation_fast")
		c.RejectReason = ""
		return c, true
	}
	return c, false
}

func tryImpulsiveStarter(c candidate, _ time.Time) (candidate, bool) {
	fails := make([]string, 0, 6)
	if c.Entry.CurrentScore < envFloat("LIVE_CONT_FAST_MIN_SCORE", 65.0) {
		fails = append(fails, fmt.Sprintf("score:%.2f<%.2f", c.Entry.CurrentScore, envFloat("LIVE_CONT_FAST_MIN_SCORE", 65.0)))
	}
	if c.Entry.ScoreSlope < envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02) {
		fails = append(fails, fmt.Sprintf("slope:%.3f<%.3f", c.Entry.ScoreSlope, envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02)))
	}
	if c.VolumeRatio < envFloat("LIVE_CONT_FAST_MIN_VOL_RATIO", 1.15) {
		fails = append(fails, fmt.Sprintf("vol_ratio:%.2f<%.2f", c.VolumeRatio, envFloat("LIVE_CONT_FAST_MIN_VOL_RATIO", 1.15)))
	}
	if !continuationStructureConfirmed(c) {
		fails = append(fails, firstNonEmpty(c.StructureReason, "continuation_no_structure_confirm"))
	}
	if !starterDirectionalContextOK(c) {
		if strings.EqualFold(c.Side, "BUY") {
			fails = append(fails, "below_vwap_ema")
		} else {
			fails = append(fails, "above_vwap_ema")
		}
	}

	if conf, reasons, ok := qualifiesImpulsiveShortStarter(c, fails); ok && starterSoftRejectsOnly(c) {
		c.Strat = "impulsive_short_starter"
		c.Conf = conf
		c.Sig = strategies.Signal{Active: true, Name: "impulsive_short_starter", Side: toFeatureSide(c.Side), Confidence: conf, Reasons: reasons, Tags: []string{"starter_only"}}
		c.Sig = applySignalRiskGeometry(c, "impulsive_short_starter")
		c.RejectReason = ""
		return c, true
	}
	if conf, reasons, ok := qualifiesImpulsiveLongStarter(c, fails); ok && starterSoftRejectsOnly(c) {
		c.Strat = "impulsive_long_starter"
		c.Conf = conf
		c.Sig = strategies.Signal{Active: true, Name: "impulsive_long_starter", Side: toFeatureSide(c.Side), Confidence: conf, Reasons: reasons, Tags: []string{"starter_only"}}
		c.Sig = applySignalRiskGeometry(c, "impulsive_long_starter")
		c.RejectReason = ""
		return c, true
	}
	return c, false
}

func starterSoftRejectsOnly(c candidate) bool {
	if c.Entry.ScoreSlope <= -0.01 {
		return false
	}
	if hardReason, blocked := hardBlockEntry(c); blocked {
		_ = hardReason
		return false
	}
	return c.Entry.Rank <= envFloat("LIVE_ELITE_STARTER_MAX_RANK", 2.0) ||
		qualifiesEliteStarterCandidate(c) ||
		starterStructureContextOK(c) ||
		candidateDirectionalMovePct(c) >= envFloat("LIVE_STARTER_MIN_DIRECTIONAL_PCT", 3.0)
}
