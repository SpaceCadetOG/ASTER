package main

import (
	"log"
	"strings"
	"time"

	"go-machine/internal/inplay"
	"go-machine/internal/strategies"
)

type SimpleEntryDecision struct {
	Allowed bool
	Side    string
	Reason  string
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

	dec := decideSimpleEntryNow(c, currentAccountHealth())
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
	side := simpleEntrySide(c.Side)
	if side == "" {
		return SimpleEntryDecision{Allowed: false, Reason: "side_unknown"}
	}
	if reason, blocked := entriesBlockedByAccountHealth(acct); blocked {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: reason}
	}
	if !simpleEntryLeaderEligible(c) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "not_top_leader"}
	}
	if !simpleStateAllowed(c.Entry.State, side) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "state_not_allowed"}
	}
	if c.Entry.CurrentScore < simpleMinScore(side) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "low_score"}
	}
	if c.Entry.ScoreSlope < simpleMinSlope(side) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "weak_slope"}
	}
	if reason := simpleOperationalBlockReason(c); reason != "" {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: reason}
	}
	if c.ExtensionATR >= envFloat("LIVE_TRUE_EXTENSION_ATR", 2.25) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "extended"}
	}
	if candidateSpikeCandle(c) || c.Entry.ExhaustionRisk >= envFloat("LIVE_TRUE_EXHAUSTION_RISK", 5.5) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "exhausted"}
	}
	if c.SpreadBps > envFloat("LIVE_MAX_SPREAD_BPS", envFloat("LIVE_OB_MAX_SPREAD_BPS", 10)) {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: "spread_too_wide"}
	}
	if reason := directionalConflictRejectReason(c); reason != "" {
		return SimpleEntryDecision{Allowed: false, Side: side, Reason: reason}
	}
	return SimpleEntryDecision{Allowed: true, Side: side, Reason: "entry_now_" + strings.ToLower(side)}
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

func simpleOperationalBlockReason(c candidate) string {
	reason := strings.ToLower(strings.TrimSpace(firstNonEmpty(c.RejectReason, c.Sig.RejectReason)))
	if reason == "" {
		return ""
	}
	switch {
	case strings.Contains(reason, "cooldown"):
		return "symbol_cooldown_active"
	case strings.Contains(reason, "stopout"):
		return "symbol_stopout_lock_active"
	case strings.Contains(reason, "position_conflict"), strings.Contains(reason, "already_in_position"), strings.Contains(reason, "in_position"):
		return "position_conflict"
	case strings.Contains(reason, "daily_loss"):
		return "daily_loss_limit"
	case strings.Contains(reason, "order_limit"), strings.Contains(reason, "orders_per"):
		return "order_limit_exceeded"
	default:
		return ""
	}
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
