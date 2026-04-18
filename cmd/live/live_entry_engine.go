package main

import (
	"fmt"
	"log"
	"strings"
	"time"

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

var liveEntryAccountHealthProvider = func() accountHealthSummary {
	return accountHealthSummary{State: "healthy"}
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

func choosePrimaryLiveSignal(c candidate, now time.Time) candidate {
	if reason, blocked := hardBlockEntry(c); blocked {
		c.Strat = "none"
		c.Conf = 0
		c.RejectReason = reason
		logSimpleDecision(c, false, reason)
		return c
	}

	dec := decideSimpleEntryNowAt(c, currentAccountHealth(), now)
	if dec.Allowed {
		signal := "entry_now_" + strings.ToLower(strings.TrimSpace(dec.Side))
		c.Strat = signal
		c.Conf = clamp(0.50+min(0.30, max(0, c.Entry.CurrentScore-85.0)*0.01+max(0, c.Entry.ScoreSlope-0.20)*0.80), 0, 0.92)
		c.Sig = strategies.Signal{
			Active:       true,
			Name:         signal,
			Side:         toFeatureSide(c.Side),
			Confidence:   c.Conf,
			RejectReason: "",
			Reasons:      []string{dec.Reason},
			Tags:         []string{"entry_now", "starter_only"},
		}
		c.Sig = applySignalRiskGeometry(c, signal)
		c.RejectReason = ""
		logSimpleDecision(c, true, dec.Reason)
		return c
	}

	c.Strat = "none"
	c.Conf = 0
	c.RejectReason = "no_simple_entry"
	logSimpleDecision(c, false, dec.Reason)
	return c
}

func decideSimpleEntryNow(c candidate, acct accountHealthSummary) SimpleEntryDecision {
	return decideSimpleEntryNowAt(c, acct, time.Now().UTC())
}

func decideSimpleEntryNowAt(c candidate, acct accountHealthSummary, now time.Time) SimpleEntryDecision {
	side := simpleEntrySide(c.Side)
	marketTs := candidateMarketSnapshotAt(c, now)
	accountTs := accountSnapshotAt(acct, now)
	if side == "" {
		return SimpleEntryDecision{Allowed: false, Reason: "side_unknown", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if dataAge := now.Sub(marketTs); dataAge > time.Duration(envInt("LIVE_SIMPLE_MAX_DATA_AGE_SEC", 3))*time.Second {
		return SimpleEntryDecision{
			Allowed:           false,
			Side:              side,
			Reason:            "stale_data_skew",
			MarketSnapshotTs:  marketTs,
			AccountSnapshotTs: accountTs,
		}
	}
	if reason, blocked := entriesBlockedByAccountHealth(acct); blocked {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: reason, MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if !simpleEntryLeaderEligible(c) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "not_top_leader", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
	}
	if confluenceScorePct, ok := candidateConfluenceScorePct(c); ok {
		if confluenceScorePct < envFloat("LIVE_CONFLUENCE_MIN_SCORE", 70.0) {
			return SimpleEntryDecision{Allowed: false, Side: side, Reason: "confluence_below_min", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
		}
		if confluenceScorePct >= envFloat("LIVE_CONFLUENCE_WATCH_MIN_SCORE", 70.0) &&
			confluenceScorePct < envFloat("LIVE_CONFLUENCE_AUTO_ENTRY_MIN_SCORE", 85.0) {
			return SimpleEntryDecision{Allowed: false, Side: side, Reason: "watchlist_wait_orderflow", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
		}
	}
	if !simpleStateAllowed(c.Entry.State, side) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "state_not_allowed", MarketSnapshotTs: marketTs, AccountSnapshotTs: accountTs}
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
	return SimpleEntryDecision{
		Allowed:           true,
		Side:              side,
		Reason:            "entry_now_" + strings.ToLower(side),
		MarketSnapshotTs:  marketTs,
		AccountSnapshotTs: accountTs,
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
	return c.CombinedScore >= minCombined
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

func logSimpleDecision(c candidate, allowed bool, reason string) {
	if !shouldLogSimpleDecision(c) {
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
}

func shouldLogSimpleDecision(c candidate) bool {
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
	if summary.State == "degraded" && envBool("LIVE_PAPER_BLOCK_ON_DEGRADED_HEALTH", false) {
		return "account_health_degraded", true
	}
	if summary.State == "partial" && envBool("LIVE_PAPER_BLOCK_ON_PARTIAL_HEALTH", false) {
		return "account_health_partial", true
	}
	if summary.SignedUserDataBackoff && envBool("LIVE_PAPER_BLOCK_ON_SIGNED_BACKOFF", false) {
		return "signed_user_data_backoff", true
	}
	return "", false
}

func simpleOperationalBlockReason(c candidate) string {
	blockers := []string{
		"account_health_failed",
		"account_health_degraded",
		"signed_user_data_backoff",
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
