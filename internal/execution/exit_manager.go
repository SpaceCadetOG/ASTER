package execution

import (
	"math"
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
}

type Manager struct {
	cfg Config
}

type ProtectInput struct {
	Side              string
	Entry             float64
	Stop              float64
	Mark              float64
	MFER              float64
	MAER              float64
	BarsHeld          int
	StallBars         int
	WeakFlow          bool
	NearFriction      bool
	LiqSpike          bool
	UnrealizedPct     float64
	Sponsored         bool
	HitTP1            bool
	HitTP2            bool
	HitTP3            bool
	WeakSponsorStreak int
}

type ProtectDecision struct {
	Reason         string
	MoveStopToBE   bool
	TightenStop    bool
	TightenToPrice float64
	PartialExitPct float64
	FullExit       bool
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
	dec := ProtectDecision{}
	if in.Entry <= 0 || in.Stop <= 0 || in.Mark <= 0 {
		return dec
	}
	if in.LiqSpike && in.UnrealizedPct > 0 {
		dec.PartialExitPct = m.cfg.LiqSpikePartialPct
		dec.Reason = "LIQ_SPIKE_PARTIAL"
	}
	if in.HitTP1 && !in.Sponsored && in.WeakSponsorStreak >= m.cfg.UnsponsoredWeakStreak {
		dec.MoveStopToBE = true
		dec.TightenStop = true
		dec.TightenToPrice = tightenToR(in.Side, in.Entry, in.Stop, m.cfg.UnsponsoredTightenR)
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
		dec.FullExit = true
		dec.Reason = "PROFIT_GIVEBACK"
		return dec
	}
	if in.BarsHeld >= m.cfg.NoFollowThroughBars &&
		in.MFER < m.cfg.NoFollowThroughMinMFER &&
		in.MAER >= m.cfg.NoFollowThroughMinMAER &&
		!(in.Sponsored && in.BarsHeld <= m.cfg.SponsorshipGraceMin) {
		dec.FullExit = true
		dec.Reason = "NO_FOLLOW_THROUGH"
		return dec
	}
	if in.WeakFlow && in.MFER >= m.cfg.WeakFlowArmBER {
		dec.MoveStopToBE = true
		dec.Reason = "WEAK_FLOW_BE"
	}
	if in.StallBars >= m.cfg.StallBarsForTighten && in.NearFriction {
		dec.TightenStop = true
		dec.TightenToPrice = tightenToR(in.Side, in.Entry, in.Stop, m.cfg.StallTightenToR)
		if dec.Reason == "" {
			dec.Reason = "STALL_NEAR_FRICTION"
		}
	}
	return dec
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
