package execution

import (
	"math"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	FrontRunPct            float64
	NoFollowThroughBars    int
	NoFollowThroughMinMFER float64
	NoFollowThroughMinMAER float64
	ProfitLockArmR         float64
	ProfitGivebackPct      float64
	SponsoredGivebackPct   float64
	WeakFlowArmBER         float64
	LiqSpikePartialPct     float64
	StallBarsForTighten    int
	StallTightenToR        float64
	SponsorshipGraceMin    int
	UnsponsoredTightenR    float64
	UnsponsoredWeakStreak  int
	TightenAfterConfirm    bool
	RequireStructureLoss   bool
	ProfitLockTightenR     float64
	StarterStabilizeBars   int
	MinUPnLPctForBE        float64
}

type Manager struct {
	cfg Config
}

type ProtectInput struct {
	Side               string
	Entry              float64
	Stop               float64
	Mark               float64
	MFER               float64
	MAER               float64
	BarsHeld           int
	StallBars          int
	WeakFlow           bool
	NearFriction       bool
	LiqSpike           bool
	UnrealizedPct      float64
	Sponsored          bool
	HitTP1             bool
	HitTP2             bool
	HitTP3             bool
	WeakSponsorStreak  int
	EntryReason        string
	EntryStrategyID    string
	StarterEntry       bool
	AdvancedReady      bool
	HTFTrendState      string
	HTFTrendPersistent bool
	HTFTrendFailed     bool
	HTFCaution         bool
	TriggerRef         string
	ComputedStop       float64
	SubmittedStop      float64
	AcceptedStop       float64
	LegalityAdjusted   bool
	WinnerLifecycle    string
	TrailingActive     bool
	MatureTrend        bool
	RealInvalidation   bool
}

type ProtectDecision struct {
	CurrentWinnerLifecycle string
	Reason                 string
	MoveStopToBE           bool
	TightenStop            bool
	TightenToPrice         float64
	PartialExitPct         float64
	FullExit               bool
	ImmediateExit          bool
	ExitNowReason          string
	ComputedStop           float64
	SubmittedStop          float64
	AcceptedStop           float64
	TriggerRef             string
	LegalityAdjusted       bool
	HTFTrendState          string
	HTFPersistent          bool
	HTFFailed              bool
	HTFCaution             bool
	WinnerLifecycle        string
}

func NewManager(cfg Config) *Manager {
	if cfg.FrontRunPct <= 0 {
		cfg.FrontRunPct = 0.001
	}
	if cfg.NoFollowThroughBars <= 0 {
		cfg.NoFollowThroughBars = 10
	}
	if cfg.NoFollowThroughMinMFER <= 0 {
		cfg.NoFollowThroughMinMFER = 0.20
	}
	if cfg.NoFollowThroughMinMAER <= 0 {
		cfg.NoFollowThroughMinMAER = 0.80
	}
	if cfg.ProfitLockArmR <= 0 {
		cfg.ProfitLockArmR = 0.60
	}
	if cfg.ProfitGivebackPct <= 0 {
		cfg.ProfitGivebackPct = 0.25
	}
	if cfg.SponsoredGivebackPct <= 0 {
		cfg.SponsoredGivebackPct = 0.10
	}
	if cfg.WeakFlowArmBER <= 0 {
		cfg.WeakFlowArmBER = 0.45
	}
	if cfg.LiqSpikePartialPct <= 0 || cfg.LiqSpikePartialPct > 1 {
		cfg.LiqSpikePartialPct = 0.35
	}
	if cfg.StallBarsForTighten <= 0 {
		cfg.StallBarsForTighten = 3
	}
	if cfg.StallTightenToR <= 0 {
		cfg.StallTightenToR = 0.20
	}
	if cfg.SponsorshipGraceMin <= 0 {
		cfg.SponsorshipGraceMin = 45
	}
	if cfg.UnsponsoredTightenR <= 0 {
		cfg.UnsponsoredTightenR = 0.18
	}
	if cfg.UnsponsoredWeakStreak <= 0 {
		cfg.UnsponsoredWeakStreak = 2
	}
	if cfg.ProfitLockTightenR <= 0 {
		cfg.ProfitLockTightenR = 0.35
	}
	if cfg.StarterStabilizeBars <= 0 {
		cfg.StarterStabilizeBars = 6
	}
	if cfg.MinUPnLPctForBE <= 0 {
		cfg.MinUPnLPctForBE = 5.0
	}
	return &Manager{cfg: cfg}
}

func (m *Manager) FrontRunTarget(side string, target float64, frictions ...float64) float64 {
	if target <= 0 {
		return target
	}
	out := target
	for _, f := range frictions {
		if f <= 0 {
			continue
		}
		if strings.EqualFold(side, "BUY") {
			if f < out {
				out = minFloat(out, f*(1-m.cfg.FrontRunPct))
			}
		} else {
			if f > out {
				out = maxFloat(out, f*(1+m.cfg.FrontRunPct))
			}
		}
	}
	return out
}

func (m *Manager) EvaluateProtect(in ProtectInput) ProtectDecision {
	currentR := currentRMultiple(in.Side, in.Entry, in.Stop, in.Mark)
	currentLifecycle := NormalizeWinnerLifecycle(in.WinnerLifecycle)
	dec := ProtectDecision{
		CurrentWinnerLifecycle: string(currentLifecycle),
		HTFTrendState:          in.HTFTrendState,
		HTFPersistent:          in.HTFTrendPersistent,
		HTFFailed:              in.HTFTrendFailed,
		HTFCaution:             in.HTFCaution,
		ComputedStop:           in.ComputedStop,
		SubmittedStop:          in.SubmittedStop,
		AcceptedStop:           in.AcceptedStop,
		TriggerRef:             strings.ToLower(strings.TrimSpace(in.TriggerRef)),
		LegalityAdjusted:       in.LegalityAdjusted,
		WinnerLifecycle: string(ResolveWinnerLifecycle(
			currentLifecycle,
			WinnerLifecycleInput{
				MaxR:             in.MFER,
				CurrentR:         currentR,
				ProofObserved:    winnerProofObserved(in.MFER, currentR, in.AdvancedReady, in.HitTP1, in.HitTP2, in.HitTP3),
				MatureTrend:      in.MatureTrend,
				TrailingActive:   in.TrailingActive,
				RealInvalidation: in.RealInvalidation,
				WinnerReversion:  false,
			},
		)),
	}
	softManageOnly := envBoolXM("LIVE_EXIT_SOFT_SIGNALS_MANAGE_ONLY", true)
	softExitManageOnly := softManageOnly || isStarterOrProof(currentLifecycle) || isWinnerLockedOrBetter(currentLifecycle)
	earlySoftHoldMinutes := envIntExit("LIVE_EXIT_EARLY_SOFT_HOLD_MIN", 20)
	earlySoftProtected := isStarterOrProof(currentLifecycle) &&
		((earlySoftHoldMinutes > 0 && in.BarsHeld < earlySoftHoldMinutes && in.MFER > 0) || in.MFER > 0)
	if in.Entry <= 0 || in.Stop <= 0 || in.Mark <= 0 {
		return dec
	}
	if in.AcceptedStop <= 0 {
		dec.AcceptedStop = in.Stop
	}
	if in.SubmittedStop <= 0 {
		dec.SubmittedStop = in.Stop
	}
	if in.ComputedStop <= 0 {
		dec.ComputedStop = in.Stop
	}
	beLockR := envFloatExit("LIVE_EXIT_BE_LOCK_R", 0.5)
	if in.MFER >= beLockR {
		dec.MoveStopToBE = true
		dec.TightenStop = true
		dec.TightenToPrice = breakEvenPlus(in.Side, in.Entry, in.Stop, 0.00)
		dec.Reason = strongerExitReason(dec.Reason, "instant_be_lock")
	}
	earlyTrailR := envFloatExit("LIVE_EXIT_EARLY_TRAIL_R", 1.0)
	if in.MFER >= earlyTrailR {
		frac := envFloatExit("LIVE_EXIT_EARLY_TRAIL_LOCK_FRAC", 0.30)
		switch {
		case in.MFER >= envFloatExit("LIVE_EXIT_LATE_TRAIL_LOCK_R", 3.0):
			frac = envFloatExit("LIVE_EXIT_LATE_TRAIL_LOCK_FRAC", 0.70)
		case in.MFER >= envFloatExit("LIVE_EXIT_PEAK_LOCK_R", 2.0):
			frac = envFloatExit("LIVE_EXIT_PEAK_LOCK_FRAC", 0.50)
		}
		if frac <= 0 || frac > 1 {
			frac = 0.30
		}
		trailStop := trailLockFromMaxR(in.Side, in.Entry, in.Stop, in.Mark, in.MFER, frac)
		if strings.EqualFold(in.Side, "BUY") {
			dec.TightenStop = true
			dec.TightenToPrice = maxFloat(dec.TightenToPrice, trailStop)
		} else {
			if dec.TightenToPrice <= 0 {
				dec.TightenToPrice = trailStop
			} else {
				dec.TightenToPrice = minFloat(dec.TightenToPrice, trailStop)
			}
			dec.TightenStop = true
		}
		dec.Reason = strongerExitReason(dec.Reason, "early_profit_trail")
	}
	plan := exitPlanForStrategy(firstNonEmptyExit(in.EntryStrategyID, in.EntryReason))
	if starterInitialManageOnly(in, m.cfg.StarterStabilizeBars) {
		// Keep starter trades simple at first attach: let initial stop stand, no early trailing/tightening.
		dec.Reason = "STARTER_INITIAL_PROTECT_ONLY"
		return dec
	}
	wp := EvaluateWinnerProtection(
		firstNonEmptyExit(in.EntryStrategyID, in.EntryReason),
		in.Side,
		in.Entry,
		in.Stop,
		in.Mark,
		in.MFER,
		in.HitTP1 || in.HitTP2 || in.HitTP3,
		in.AdvancedReady || in.HitTP1,
	)
	if wp.MoveStop {
		dec.MoveStopToBE = true
		dec.TightenStop = true
		if strings.EqualFold(in.Side, "BUY") {
			dec.TightenToPrice = maxFloat(dec.TightenToPrice, wp.NewStop)
		} else {
			if dec.TightenToPrice <= 0 {
				dec.TightenToPrice = wp.NewStop
			} else {
				dec.TightenToPrice = minFloat(dec.TightenToPrice, wp.NewStop)
			}
		}
		dec.Reason = strongerExitReason(dec.Reason, wp.Reason)
	}
	if wp.TakePartial && wp.PartialFraction > 0 {
		dec.PartialExitPct = wp.PartialFraction
		if dec.Reason == "" {
			dec.Reason = wp.Reason
		}
	}
	if in.LiqSpike && in.UnrealizedPct > 0 {
		dec.PartialExitPct = m.cfg.LiqSpikePartialPct
		dec.Reason = "LIQ_SPIKE_PARTIAL"
	}
	if in.HitTP1 && !in.Sponsored && in.WeakSponsorStreak >= m.cfg.UnsponsoredWeakStreak {
		dec.MoveStopToBE = true
		dec.TightenStop = true
		tightenR := m.cfg.UnsponsoredTightenR
		if plan == "avwap_hold" || plan == "level_hold" {
			tightenR *= 0.8
		}
		dec.TightenToPrice = tightenToR(in.Side, in.Entry, in.Stop, tightenR)
		if dec.Reason == "" {
			dec.Reason = "RUNNER_UNSPONSORED_TIGHTEN"
		}
	}
	// Lock gains when a trade was clearly profitable but gives back too much while weak.
	profitGivebackPct := m.cfg.ProfitGivebackPct
	if in.Sponsored {
		profitGivebackPct = m.cfg.SponsoredGivebackPct
	}
	if in.MFER >= m.cfg.ProfitLockArmR &&
		in.WeakFlow &&
		in.UnrealizedPct <= profitGivebackPct &&
		!(in.Sponsored && !in.HitTP3) {
		if in.HTFTrendPersistent && !in.HTFTrendFailed {
			dec.MoveStopToBE = true
			dec.TightenStop = true
			tightR := envFloatXM("LIVE_MOMENTUM_FADE_TIGHTEN_R", 0.06)
			if in.HTFCaution {
				tightR = envFloatXM("LIVE_MOMENTUM_FADE_TIGHTEN_R_CAUTION", 0.05)
			}
			dec.TightenToPrice = tightenToR(in.Side, in.Entry, in.Stop, tightR)
			dec.Reason = "PROFIT_GIVEBACK_TIGHTEN"
			return dec
		}
		if earlyContinuationProtect(in) {
			dec.MoveStopToBE = true
			dec.TightenStop = true
			tightR := m.cfg.ProfitLockTightenR
			if in.MFER < 0.75 {
				tightR = envFloatXM("LIVE_PROFIT_GIVEBACK_TIGHTEN_R_EARLY", 0.05)
			}
			dec.TightenToPrice = tightenToR(in.Side, in.Entry, in.Stop, tightR)
			dec.Reason = "PROFIT_GIVEBACK_TIGHTEN"
			return dec
		}
		if softExitManageOnly && !in.HTFTrendFailed {
			dec.MoveStopToBE = true
			dec.TightenStop = true
			tightR := envFloatXM("LIVE_MOMENTUM_FADE_TIGHTEN_R", 0.06)
			if in.HTFCaution {
				tightR = envFloatXM("LIVE_MOMENTUM_FADE_TIGHTEN_R_CAUTION", 0.05)
			}
			dec.TightenToPrice = tightenToR(in.Side, in.Entry, in.Stop, tightR)
			dec.Reason = "PROFIT_GIVEBACK_TIGHTEN"
			return dec
		}
		if isWinnerLockedOrBetter(currentLifecycle) || earlySoftProtected {
			dec.MoveStopToBE = true
			dec.TightenStop = true
			dec.TightenToPrice = tightenToR(in.Side, in.Entry, in.Stop, envFloatXM("LIVE_MOMENTUM_FADE_TIGHTEN_R", 0.06))
			dec.Reason = "PROFIT_GIVEBACK_TIGHTEN"
			return dec
		}
		dec.FullExit = true
		dec.Reason = "PROFIT_GIVEBACK"
		return dec
	}
	if in.WeakFlow && in.MFER >= m.cfg.WeakFlowArmBER {
		if in.HTFTrendPersistent && !in.HTFTrendFailed {
			dec.MoveStopToBE = true
			dec.TightenStop = true
			tightR := envFloatXM("LIVE_MOMENTUM_FADE_TIGHTEN_R", 0.06)
			if in.HTFCaution {
				tightR = envFloatXM("LIVE_MOMENTUM_FADE_TIGHTEN_R_CAUTION", 0.05)
			}
			dec.TightenToPrice = tightenToR(in.Side, in.Entry, in.Stop, tightR)
			dec.Reason = firstNonEmptyExit(dec.Reason, "MOMENTUM_FADE_TIGHTEN")
			return dec
		}
		if in.HitTP1 || in.UnrealizedPct >= m.cfg.MinUPnLPctForBE {
			dec.MoveStopToBE = true
			dec.Reason = "WEAK_FLOW_BE"
		}
	}
	if in.StallBars >= m.cfg.StallBarsForTighten && in.NearFriction {
		dec.TightenStop = true
		dec.TightenToPrice = tightenToR(in.Side, in.Entry, in.Stop, m.cfg.StallTightenToR)
		if dec.Reason == "" {
			dec.Reason = "STALL_NEAR_FRICTION"
		}
	}
	if mustProtectAfterProof(in.MFER) && !dec.MoveStopToBE && !dec.TightenStop {
		dec.MoveStopToBE = true
		dec.TightenStop = true
		dec.TightenToPrice = breakEvenPlus(in.Side, in.Entry, in.Stop, 0.02)
		dec.Reason = firstNonEmptyExit(dec.Reason, "proof_r_protect")
	}
	if in.MFER > 0.25 && in.UnrealizedPct < 0 &&
		!dec.MoveStopToBE && !dec.TightenStop && !dec.FullExit && !dec.ImmediateExit {
		dec.MoveStopToBE = true
		dec.Reason = firstNonEmptyExit(dec.Reason, "PROTECT_BE_FALLBACK")
	}
	if dec.FullExit && in.RealInvalidation {
		dec.WinnerLifecycle = string(ResolveWinnerLifecycle(
			NormalizeWinnerLifecycle(dec.WinnerLifecycle),
			WinnerLifecycleInput{
				MaxR:             in.MFER,
				CurrentR:         currentR,
				ProofObserved:    winnerProofObserved(in.MFER, currentR, in.AdvancedReady, in.HitTP1, in.HitTP2, in.HitTP3),
				MatureTrend:      in.MatureTrend,
				TrailingActive:   in.TrailingActive,
				RealInvalidation: true,
			},
		))
	}
	return dec
}

func trailLockFromMaxR(side string, entry, stop, mark, maxR, frac float64) float64 {
	risk := math.Abs(entry - stop)
	if risk <= 0 {
		return trailLockFromPeak(side, entry, mark, frac)
	}
	if frac <= 0 || frac > 1 {
		frac = 0.30
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		peak := entry + risk*maxR
		locked := entry + (peak-entry)*frac
		if mark > 0 && locked > mark {
			return mark
		}
		return locked
	}
	peak := entry - risk*maxR
	locked := entry - (entry-peak)*frac
	if mark > 0 && locked < mark {
		return mark
	}
	return locked
}

func exitPlanForStrategy(strategyID string) string {
	switch strings.ToLower(strings.TrimSpace(strategyID)) {
	case "impulse_continuation":
		return "fast_runner"
	case "anchored_vwap_pullback":
		return "avwap_hold"
	case "vp_retest":
		return "level_hold"
	default:
		return "default"
	}
}

func starterInitialManageOnly(in ProtectInput, stabilizeBars int) bool {
	if stabilizeBars <= 0 {
		stabilizeBars = 6
	}
	if !(in.StarterEntry || IsStarterEntryReason(in.EntryReason)) {
		return false
	}
	if in.AdvancedReady || in.HitTP1 {
		return false
	}
	return in.BarsHeld < stabilizeBars
}

func tightenToR(side string, entry, stop, r float64) float64 {
	risk := math.Abs(entry - stop)
	if risk <= 0 || r <= 0 {
		return stop
	}
	if strings.EqualFold(side, "BUY") {
		return entry - risk*r
	}
	return entry + risk*r
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func exitReasonPriority(reason string) int {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "":
		return 0
	case "INSTANT_BE_LOCK", "PROTECT_BE_FALLBACK":
		return 10
	case "MOMENTUM_FADE_TIGHTEN", "PROFIT_GIVEBACK_TIGHTEN":
		return 20
	case "EARLY_PROFIT_TRAIL":
		return 30
	default:
		return 15
	}
}

func strongerExitReason(current, next string) string {
	if exitReasonPriority(next) >= exitReasonPriority(current) {
		return next
	}
	return current
}

func firstNonEmptyExit(v ...string) string {
	for _, x := range v {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func mustProtectAfterProof(maxR float64) bool {
	return envBoolExit("LIVE_EXIT_PROTECT_AFTER_PROOF", true) &&
		maxR >= envFloatExit("LIVE_EXIT_PROOF_R", 0.5)
}

func trailLockFromPeak(side string, entry, mark, frac float64) float64 {
	if frac <= 0 || frac > 1 {
		frac = 0.30
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		if mark <= entry {
			return entry
		}
		return entry + (mark-entry)*frac
	}
	if mark >= entry {
		return entry
	}
	return entry - (entry-mark)*frac
}

func earlyContinuationProtect(in ProtectInput) bool {
	if in.AdvancedReady {
		return true
	}
	if in.HitTP1 || in.HitTP2 || in.HitTP3 {
		return true
	}
	minR := envFloatXM("LIVE_EARLY_CONTINUATION_MIN_R", 0.25)
	if minR > 0 && in.MFER >= minR {
		return true
	}
	if in.MFER > 0 && in.WeakFlow {
		return true
	}
	return false
}

func envFloatXM(key string, def float64) float64 {
	v := os.Getenv(key)
	if strings.TrimSpace(v) == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envBoolXM(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envFloatExit(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envBoolExit(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envIntExit(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
