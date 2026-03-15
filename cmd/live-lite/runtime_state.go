package main

import (
	"fmt"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/inplay"
)

type triggerLifecycleConfig struct {
	Enable             bool
	ArmScans           int
	ReadyScans         int
	ReversalReadyScans int
	InvalidateScans    int
	ExpireAfter        time.Duration
	ArmedBoost         float64
	ReadyBoost         float64
	InvalidPenalty     float64
}

type triggerMemory struct {
	RawState     string
	Family       string
	Scans        int
	InvalidScans int
	LastSeen     time.Time
	StateSince   time.Time
}

type sponsorshipSnapshot struct {
	Sponsored bool
	Weak      bool
	Score     float64
	State     inplay.State
	Slope     float64
	OFIZ      float64
}

type orderProgress struct {
	Status   string
	ExecQty  float64
	AvgPx    float64
	OrigQty  float64
	Working  bool
	Filled   bool
	Terminal bool
	Rejected bool
}

func normalizeTriggerLifecycleConfig(cfg triggerLifecycleConfig) triggerLifecycleConfig {
	if cfg.ArmScans < 1 {
		cfg.ArmScans = 1
	}
	if cfg.ReadyScans < cfg.ArmScans {
		cfg.ReadyScans = cfg.ArmScans
	}
	if cfg.ReversalReadyScans < cfg.ReadyScans {
		cfg.ReversalReadyScans = cfg.ReadyScans
	}
	if cfg.InvalidateScans < 1 {
		cfg.InvalidateScans = 1
	}
	if cfg.ExpireAfter <= 0 {
		cfg.ExpireAfter = 20 * time.Minute
	}
	if cfg.ArmedBoost < 0 {
		cfg.ArmedBoost = 0
	}
	if cfg.ReadyBoost < 0 {
		cfg.ReadyBoost = 0
	}
	if cfg.InvalidPenalty <= 0 {
		cfg.InvalidPenalty = 0.18
	}
	return cfg
}

func triggerStateFamily(state, strat string) string {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case string(triggerOFReclaim), string(triggerStackedBid), string(triggerStackedAsk), string(triggerDeltaFlip), string(triggerImpulseCont), string(triggerOFAbsorb):
		return "continuation"
	case string(triggerExhaustion), string(triggerFailReclaim):
		if strategyFamily(candidate{Strat: strat}) == "rev" {
			return "reversal"
		}
		return "fade"
	default:
		return "none"
	}
}

func triggerStageScore(stage string) float64 {
	switch strings.ToUpper(strings.TrimSpace(stage)) {
	case "READY":
		return 1.0
	case "ARMED":
		return 0.72
	case "WATCH":
		return 0.40
	case "INVALIDATED":
		return 0.10
	default:
		return 0.20
	}
}

func reversalStateReady(st inplay.State) bool {
	switch st {
	case inplay.StateCooling, inplay.StateDumping, inplay.StateExhausted:
		return true
	default:
		return false
	}
}

func applyTriggerLifecycle(c candidate, now time.Time, mem map[string]triggerMemory, cfg triggerLifecycleConfig) candidate {
	if !cfg.Enable {
		if strings.TrimSpace(c.TriggerState) == "" || strings.EqualFold(c.TriggerState, string(triggerNone)) {
			c.TriggerStage = "WATCH"
		} else {
			c.TriggerStage = "READY"
			c.TriggerScans = 1
		}
		return c
	}
	cfg = normalizeTriggerLifecycleConfig(cfg)
	key := candidateKey(c)
	m := mem[key]
	if !m.LastSeen.IsZero() && now.Sub(m.LastSeen) > cfg.ExpireAfter {
		m = triggerMemory{}
	}
	rawState := strings.ToUpper(strings.TrimSpace(c.TriggerState))
	family := triggerStateFamily(rawState, c.Strat)
	if rawState == "" {
		rawState = string(triggerNone)
	}
	if rawState == string(triggerNone) || family == "none" {
		m.InvalidScans++
		m.LastSeen = now
		if m.InvalidScans >= cfg.InvalidateScans {
			delete(mem, key)
			c.TriggerStage = "INVALIDATED"
			c.TriggerScans = 0
			c.TriggerStateN = clamp(c.TriggerStateN-cfg.InvalidPenalty, 0, 1)
			if c.RejectReason == "" {
				c.RejectReason = "trigger_not_ready"
			}
			return c
		}
		mem[key] = m
		c.TriggerStage = "WATCH"
		c.TriggerScans = m.Scans
		c.TriggerStateN = clamp(c.TriggerStateN-cfg.InvalidPenalty*0.5, 0, 1)
		return c
	}

	compatible := rawState == m.RawState
	if !compatible && family != "reversal" && family == m.Family && m.RawState != "" {
		compatible = true
	}
	if compatible {
		m.Scans++
	} else {
		m = triggerMemory{
			RawState:   rawState,
			Family:     family,
			Scans:      1,
			StateSince: now,
		}
	}
	m.RawState = rawState
	m.Family = family
	m.LastSeen = now
	m.InvalidScans = 0
	mem[key] = m

	required := cfg.ReadyScans
	if family == "reversal" {
		required = cfg.ReversalReadyScans
	}
	stage := "WATCH"
	if m.Scans >= cfg.ArmScans {
		stage = "ARMED"
	}
	if m.Scans >= required {
		stage = "READY"
	}
	if family == "reversal" && !reversalStateReady(c.Entry.State) {
		if stage == "READY" {
			stage = "ARMED"
		}
		if c.RejectReason == "" {
			c.RejectReason = "reversal_state_not_deteriorated"
		}
	}
	c.TriggerStage = stage
	c.TriggerScans = m.Scans
	switch stage {
	case "READY":
		c.TriggerStateN = clamp(c.TriggerStateN+cfg.ReadyBoost, 0, 1)
	case "ARMED":
		c.TriggerStateN = clamp(c.TriggerStateN+cfg.ArmedBoost, 0, 1)
	default:
		c.TriggerStateN = clamp(c.TriggerStateN-cfg.InvalidPenalty*0.25, 0, 1)
	}
	return c
}

func classifySponsorship(side, symbol string, mom map[string]momentumView, flow map[string]flowMetrics) sponsorshipSnapshot {
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	mv := mom[raw]
	var e *inplay.Entry
	if strings.EqualFold(side, "BUY") {
		e = mv.Long
	} else {
		e = mv.Short
	}
	out := sponsorshipSnapshot{}
	if e != nil {
		out.State = e.State
		out.Slope = e.ScoreSlope
		score := 0.0
		score += 0.45 * clamp(e.CurrentScore/100.0, 0, 1)
		if strings.EqualFold(side, "BUY") {
			score += 0.20 * clamp(e.ScoreSlope/0.40, 0, 1)
		} else {
			score += 0.20 * clamp((-e.ScoreSlope)/0.40, 0, 1)
		}
		switch e.State {
		case inplay.StateHeating:
			score += 0.12
		case inplay.StateInPlay:
			score += 0.18
		case inplay.StatePumping:
			score += 0.22
		case inplay.StateBalanced:
			score += 0.05
		case inplay.StateCooling, inplay.StateDumping, inplay.StateExhausted:
			score -= 0.08
		}
		if e.Momentum {
			score += 0.10
		}
		out.Score = score
	}
	if fm, ok := flow[raw]; ok {
		out.OFIZ = fm.OFIZ
		if strings.EqualFold(side, "BUY") {
			out.Score += 0.15 * clamp(fm.OFIZ/1.20, 0, 1)
		} else {
			out.Score += 0.15 * clamp((-fm.OFIZ)/1.20, 0, 1)
		}
	}
	out.Score = clamp(out.Score, 0, 1)
	minScore := envFloat("LIVE_RUNNER_SPONSOR_SCORE_MIN", 0.68)
	alignedFlow := true
	if out.OFIZ != 0 {
		if strings.EqualFold(side, "BUY") {
			alignedFlow = out.OFIZ >= -envFloat("LIVE_RUNNER_SPONSOR_OFI_FLOOR", 0.10)
		} else {
			alignedFlow = out.OFIZ <= envFloat("LIVE_RUNNER_SPONSOR_OFI_FLOOR", 0.10)
		}
	}
	out.Sponsored = out.Score >= minScore && alignedFlow
	out.Weak = !out.Sponsored
	if e != nil {
		switch e.State {
		case inplay.StateCooling, inplay.StateDumping, inplay.StateExhausted:
			out.Weak = true
		}
	}
	return out
}

func updatePaperSponsorship(pos *paperPosition, snap sponsorshipSnapshot) {
	if pos == nil {
		return
	}
	pos.Sponsored = snap.Sponsored
	pos.SponsorshipScore = snap.Score
	if snap.Sponsored {
		pos.StrongSponsorStreak++
		pos.WeakSponsorStreak = 0
		return
	}
	pos.WeakSponsorStreak++
	pos.StrongSponsorStreak = 0
}

func updateLiveSponsorship(pos *livePosition, snap sponsorshipSnapshot) {
	if pos == nil {
		return
	}
	pos.Sponsored = snap.Sponsored
	pos.SponsorshipScore = snap.Score
	if snap.Sponsored {
		pos.StrongSponsorStreak++
		pos.WeakSponsorStreak = 0
		return
	}
	pos.WeakSponsorStreak++
	pos.StrongSponsorStreak = 0
}

func parseOrderProgress(o map[string]any) orderProgress {
	status := strings.ToUpper(strings.TrimSpace(fmt.Sprint(o["status"])))
	execQty := mapFloat(o["executedQty"])
	avgPx := mapFloat(o["avgPrice"])
	if avgPx <= 0 {
		avgPx = mapFloat(o["price"])
	}
	origQty := mapFloat(o["origQty"])
	out := orderProgress{
		Status:  status,
		ExecQty: execQty,
		AvgPx:   avgPx,
		OrigQty: origQty,
	}
	switch status {
	case "FILLED":
		out.Filled = true
		out.Working = false
	case "PARTIALLY_FILLED":
		out.Working = true
	case "NEW", "PENDING_NEW":
		out.Working = true
	case "CANCELED", "EXPIRED":
		out.Terminal = true
	case "REJECTED":
		out.Terminal = true
		out.Rejected = true
	default:
		out.Working = false
	}
	return out
}
