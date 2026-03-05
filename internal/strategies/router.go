package strategies

import (
	"strings"

	"go-machine/internal/data"
	"go-machine/internal/features"
)

type RouterConfig struct {
	MinGrade                  string
	MinScore                  float64
	MinWhaleDelta             float64
	AllowWarmup               bool
	WarmupSlopeMin            float64
	MaxOne                    bool
	ScannerScoreScale         float64
	EnableVPSetups            bool
	MinVPConfidence           float64
	RequireFlowConfluence     bool
	RejectIfTargetTooClosePct float64
	UseVPReversal             bool
	EnableInstitutionalPA     bool
	MinConfluenceScore        float64
	UseSessionRegimeRisk      bool
	AllowDeadZoneOnlyAPlus    bool
	RiskPolicy                RiskPolicyConfig
}

type Candidate struct {
	Signal Signal
	Score  float64
}

type Router struct {
	cfg   RouterConfig
	strat []Strategy
}

func NewRouter(cfg RouterConfig) *Router {
	if cfg.MinGrade == "" {
		cfg.MinGrade = "B"
	}
	if cfg.ScannerScoreScale <= 0 {
		cfg.ScannerScoreScale = 100
	}
	if cfg.MinVPConfidence <= 0 {
		cfg.MinVPConfidence = 0.55
	}
	if cfg.MinConfluenceScore <= 0 {
		cfg.MinConfluenceScore = 0.58
	}
	if cfg.RiskPolicy.StopMode == "" {
		cfg.RiskPolicy = DefaultRiskPolicy()
	}
	base := []Strategy{LSR{}, BOSPB{}, OBR{}, FVGC{}, FailedAuction{}, OpenDrive{}}
	if cfg.EnableVPSetups {
		base = append(base, VPAccumulation{}, VPTrendRetest{}, VPRejection{})
		if cfg.UseVPReversal {
			base = append(base, VPReversal{})
		}
	}
	if cfg.EnableInstitutionalPA {
		base = append(base,
			DailyOpenSR{},
			PDLevelsRetest{},
			FailedAuctionMagnetStrategy{},
			VWAPConfluenceStrategy{},
		)
	}
	return &Router{cfg: cfg, strat: base}
}

func (r *Router) Eval(ctx Context) []Candidate {
	if gradeValue(ctx.ScannerGrade) < gradeValue(r.cfg.MinGrade) {
		if !(r.cfg.AllowWarmup && ctx.ScoreSlope >= r.cfg.WarmupSlopeMin) {
			return nil
		}
	}
	if ctx.ScannerScore < r.cfg.MinScore {
		return nil
	}
	if ctx.Snapshot.Flow.WhaleDeltaCum < r.cfg.MinWhaleDelta {
		// still allow if strong sweep/structure trigger
		if ctx.Snapshot.Sweep == nil {
			return nil
		}
	}
	out := make([]Candidate, 0, len(r.strat))
	for _, s := range r.strat {
		sig := s.Eval(ctx)
		if !sig.Active {
			continue
		}
		sig = ApplyRiskPolicy(sig, ctx.Snapshot, r.cfg.RiskPolicy)
		if sig.VPSetup != "" && sig.Confidence < r.cfg.MinVPConfidence {
			continue
		}
		if r.cfg.RequireFlowConfluence && sig.VPSetup != "" {
			if sig.Side == features.SideLong && ctx.Snapshot.Flow.WhaleDelta1m < 0 {
				continue
			}
			if sig.Side == features.SideShort && ctx.Snapshot.Flow.WhaleDelta1m > 0 {
				continue
			}
		}
		if r.cfg.RejectIfTargetTooClosePct > 0 && sig.Entry > 0 && sig.TP1 > 0 {
			distPct := 100.0 * abs((sig.TP1-sig.Entry)/sig.Entry)
			if distPct < r.cfg.RejectIfTargetTooClosePct {
				sig.RejectReason = "target_too_close"
				continue
			}
		}
		if sig.Confidence < r.cfg.MinConfluenceScore {
			sig.RejectReason = "below_min_confluence"
			continue
		}
		if r.cfg.AllowDeadZoneOnlyAPlus && data.IsMaintenanceRegime(data.CurrentRegimeCT(sig.Ts)) && gradeValue(ctx.ScannerGrade) < gradeValue("A") {
			sig.RejectReason = "dead_zone_non_a_grade"
			continue
		}
		scoreNorm := ctx.ScannerScore / r.cfg.ScannerScoreScale
		if scoreNorm < 0 {
			scoreNorm = 0
		}
		if scoreNorm > 1.5 {
			scoreNorm = 1.5
		}
		whaleBoost := 1.0
		if ctx.Snapshot.Flow.WhaleDelta1m > 0 && sig.Side == features.SideLong {
			whaleBoost = 1.15
		}
		if ctx.Snapshot.Flow.WhaleDelta1m < 0 && sig.Side == features.SideShort {
			whaleBoost = 1.15
		}
		sessionMult := 1.0
		if r.cfg.UseSessionRegimeRisk {
			sessionMult = data.SessionRiskMultiplier(sig.Ts, sig.Confidence)
			sig.RegimeTag = string(data.CurrentRegimeCT(sig.Ts))
		}
		candScore := sig.Confidence * scoreNorm * whaleBoost * sessionMult
		out = append(out, Candidate{Signal: sig, Score: candScore})
	}
	if len(out) <= 1 || !r.cfg.MaxOne {
		return out
	}
	best := out[0]
	for i := 1; i < len(out); i++ {
		if out[i].Score > best.Score {
			best = out[i]
		}
	}
	return []Candidate{best}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func gradeValue(g string) int {
	switch strings.ToUpper(strings.TrimSpace(g)) {
	case "A+":
		return 5
	case "A":
		return 4
	case "B":
		return 3
	case "C":
		return 2
	case "D":
		return 1
	default:
		return 0
	}
}
