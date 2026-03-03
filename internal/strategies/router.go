package strategies

import (
	"strings"

	"go-machine/internal/features"
)

type RouterConfig struct {
	MinGrade          string
	MinScore          float64
	MinWhaleDelta     float64
	AllowWarmup       bool
	WarmupSlopeMin    float64
	MaxOne            bool
	ScannerScoreScale float64
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
	return &Router{cfg: cfg, strat: []Strategy{LSR{}, BOSPB{}, OBR{}, FVGC{}, FailedAuction{}, OpenDrive{}}}
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
		candScore := sig.Confidence * scoreNorm * whaleBoost
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
