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
	StrategyWeight            float64
	FlowWeight                float64
	StructureWeight           float64
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
	if cfg.StrategyWeight <= 0 {
		cfg.StrategyWeight = 0.50
	}
	if cfg.FlowWeight <= 0 {
		cfg.FlowWeight = 0.30
	}
	if cfg.StructureWeight <= 0 {
		cfg.StructureWeight = 0.20
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
		con := r.scoreConfluence(ctx, sig)
		sig.ConfluenceScore = con
		if sig.Confluence == nil {
			sig.Confluence = map[string]float64{}
		}
		sig.Confluence["strategy"] = con.StrategyScore
		sig.Confluence["flow"] = con.FlowScore
		sig.Confluence["structure"] = con.StructureScore
		sig.Confluence["total"] = con.TotalScore
		sig.Reasons = append(sig.Reasons, con.Reasons...)
		if !con.Approved {
			sig.RejectReason = "below_min_confluence"
			continue
		}
		if r.cfg.AllowDeadZoneOnlyAPlus && data.CurrentRegimeCT(sig.Ts) == data.RegimeDead && gradeValue(ctx.ScannerGrade) < gradeValue("A") {
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
		if r.cfg.UseSessionRegimeRisk {
			sig.RegimeTag = string(data.CurrentRegimeCT(sig.Ts))
		}
		candScore := con.TotalScore * scoreNorm * whaleBoost
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

func (r *Router) scoreConfluence(ctx Context, sig Signal) ConfluenceScore {
	sc := clamp01(sig.Confidence)
	fs := 0.5
	reasons := make([]string, 0, 4)
	if ctx.Snapshot.Flow.VolumeSpike {
		fs += 0.2
		reasons = append(reasons, "flow_volume_spike")
	}
	if sig.Side == features.SideLong && ctx.Snapshot.Flow.WhaleDelta1m > 0 {
		fs += 0.2
		reasons = append(reasons, "flow_whale_aligned")
	}
	if sig.Side == features.SideShort && ctx.Snapshot.Flow.WhaleDelta1m < 0 {
		fs += 0.2
		reasons = append(reasons, "flow_whale_aligned")
	}
	if sig.Side == features.SideLong && ctx.Snapshot.Flow.WhaleDelta1m < 0 {
		fs -= 0.2
		reasons = append(reasons, "flow_whale_against")
	}
	if sig.Side == features.SideShort && ctx.Snapshot.Flow.WhaleDelta1m > 0 {
		fs -= 0.2
		reasons = append(reasons, "flow_whale_against")
	}
	if fs < 0 {
		fs = 0
	}
	if fs > 1 {
		fs = 1
	}
	ss := 0.5
	if sig.Side == features.SideLong && ctx.Snapshot.Structure.Trend == features.TrendBull {
		ss += 0.25
		reasons = append(reasons, "structure_trend_aligned")
	}
	if sig.Side == features.SideShort && ctx.Snapshot.Structure.Trend == features.TrendBear {
		ss += 0.25
		reasons = append(reasons, "structure_trend_aligned")
	}
	if ctx.Snapshot.Sweep != nil && ctx.Snapshot.Sweep.Strength > 0 {
		ss += 0.15
		reasons = append(reasons, "structure_sweep_active")
	}
	if ctx.Snapshot.VP.POCShare > 0.35 {
		ss += 0.10
		reasons = append(reasons, "structure_vp_concentration")
	}
	if ss < 0 {
		ss = 0
	}
	if ss > 1 {
		ss = 1
	}
	total := sc*r.cfg.StrategyWeight + fs*r.cfg.FlowWeight + ss*r.cfg.StructureWeight
	return ConfluenceScore{
		TotalScore:     total,
		StrategyScore:  sc,
		FlowScore:      fs,
		StructureScore: ss,
		Reasons:        reasons,
		Approved:       total >= r.cfg.MinConfluenceScore,
	}
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
