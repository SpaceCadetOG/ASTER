package strategies

import (
	"strings"
	"sync"
	"time"

	"go-machine/internal/data"
	"go-machine/internal/features"
	"go-machine/internal/risk"
)

type RouterConfig struct {
	MinGrade                 string
	MinScore                 float64
	MinWhaleDelta            float64
	AllowWarmup              bool
	WarmupSlopeMin           float64
	MaxOne                   bool
	ScannerScoreScale        float64
	EnableVPSetups           bool
	MinVPConfidence          float64
	RequireFlowConfluence    bool
	UseVPReversal            bool
	EnableInstitutionalPA    bool
	MinConfluenceScore       float64
	UseSessionRegimeRisk     bool
	RiskShell                *risk.RiskShell
	RiskShellEnabled         bool
	StrategyWeight           float64
	FlowWeight               float64
	StructureWeight          float64
	ContinuationDayUTCPct    float64
	ContinuationReset1hPct   float64
	ContinuationLateSlopeMin float64
	RiskPolicy               RiskPolicyConfig
}

type Candidate struct {
	Signal Signal
	Score  float64
}

type Router struct {
	cfg   RouterConfig
	strat []Strategy
}

var (
	defaultShellOnce sync.Once
	defaultShell     *risk.RiskShell
)

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
	if cfg.ContinuationDayUTCPct <= 0 {
		cfg.ContinuationDayUTCPct = 25.0
	}
	if cfg.ContinuationLateSlopeMin <= 0 {
		cfg.ContinuationLateSlopeMin = 0.16
	}
	if cfg.RiskPolicy.StopMode == "" {
		cfg.RiskPolicy = DefaultRiskPolicy()
	}
	if !cfg.RiskShellEnabled {
		cfg.RiskShell = nil
	} else if cfg.RiskShell == nil {
		defaultShellOnce.Do(func() {
			defaultShell = risk.NewRiskShell(risk.DefaultConfig())
		})
		cfg.RiskShell = defaultShell
	}
	base := []Strategy{
		LSR{},
		BOSPB{},
		OBR{},
		FVGC{},
		FailedAuction{},
		OpenDrive{},
		VolumeClusters{},
		MultipleNodes{},
		TradesFilter{},
		StackedImbalances{},
		UnfinishedBusiness{},
	}
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
		if reason := r.continuationMaturityReject(ctx, sig); reason != "" {
			sig.RejectReason = reason
			sig.Reasons = append(sig.Reasons, reason)
			continue
		}
		sig = ApplyRiskPolicy(sig, ctx.Snapshot, r.cfg.RiskPolicy)
		if r.cfg.RiskShellEnabled && r.cfg.RiskShell != nil {
			side := "BUY"
			if sig.Side == features.SideShort {
				side = "SELL"
			}
			dec := r.cfg.RiskShell.Approve(risk.Input{
				Symbol:            ctx.Symbol,
				Session:           string(data.CurrentRegimeCT(sig.Ts)),
				Side:              side,
				Entry:             sig.Entry,
				Stop:              sig.Stop,
				NotionalUSD:       ctx.NotionalUSD,
				FundingRate:       ctx.FundingRate,
				HoldHours:         2,
				SpreadBps:         ctx.SpreadBps,
				TopBookUSD:        ctx.TopBookUSD,
				EstSlippageBps:    ctx.EstSlippageBps,
				RecentSlippageBps: ctx.RecentSlippageBps,
				VenueHealthy:      ctx.VenueHealthy || !ctx.VenueHealthKnown,
				RecordEntry:       false,
				EntriesLastHour:   ctx.EntriesLastHour,
				SymbolStopouts90m: ctx.SymbolStopouts90m,
			})
			if !dec.Approved {
				sig.RejectReason = dec.RejectReason
				sig.Reasons = append(sig.Reasons, "risk_"+dec.RejectReason)
				continue
			}
		}
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

func (r *Router) continuationMaturityReject(ctx Context, sig Signal) string {
	if sig.Side == features.SideLong || ctx.DayUTCPct == 0 {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(sig.Name))
	if strings.Contains(name, "reversal") || strings.Contains(name, "flip") {
		return ""
	}
	if hasAnyTag(sig.Tags, "reversal", "failed_auction", "role_flip") {
		return ""
	}
	if ctx.DayUTCPct > -r.cfg.ContinuationDayUTCPct {
		return ""
	}
	reset := hasAnyTag(sig.Tags, "pullback", "retest", "confluence")
	if !reset {
		return "late_extension_no_reset"
	}
	if ctx.ScoreSlope < r.cfg.ContinuationLateSlopeMin && !hasAnyTag(sig.Tags, "retest") {
		return "late_cycle_short_weak_slope"
	}
	return ""
}

func (r *Router) scoreConfluence(ctx Context, sig Signal) ConfluenceScore {
	flowSignal := OrderFlowSignal{
		CumulativeDelta: ctx.Snapshot.Flow.WhaleDeltaCum,
		DeltaRising: (sig.Side == features.SideLong && ctx.Snapshot.Flow.WhaleDelta1m > 0) ||
			(sig.Side == features.SideShort && ctx.Snapshot.Flow.WhaleDelta1m < 0),
	}
	w := CalculateConfluenceScore(ctx, sig.Side, flowSignal)
	total := w.Score / 100.0
	minRequired := r.cfg.MinConfluenceScore
	if minRequired > 1 {
		minRequired = minRequired / 100.0
	}
	reasons := []string{
		"confluence_v2",
		"tier:" + string(w.Tier),
	}
	if w.StackedFlow > 0 {
		reasons = append(reasons, "stacked_imbalance_boost")
	}
	return ConfluenceScore{
		TotalScore:     total,
		StrategyScore:  (w.Trend + w.Fibonacci + w.VWAP) / 50.0,
		FlowScore:      (w.OrderFlow + w.StackedFlow) / 35.0,
		StructureScore: w.Volume / 25.0,
		Reasons:        reasons,
		Approved:       total >= minRequired && (minRequired < 0.70 || w.Approved),
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
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

// DefaultRouter is an additive intent router used by Slice A.
// It does not replace the legacy Router/Eval path.
type DefaultRouter struct {
	confirmation ConfirmationEngine
	strategies   []DetectStrategy
}

func NewDefaultRouter(confirm ConfirmationEngine) *DefaultRouter {
	return &DefaultRouter{
		confirmation: confirm,
		strategies: []DetectStrategy{
			ImpulseContinuationStrategy{},
			AnchoredVWAPPullbackStrategy{},
			VPRetestStrategy{},
		},
	}
}

func (r *DefaultRouter) Route(ctx StrategyContext) []*EntryIntent {
	intents := make([]*EntryIntent, 0, len(r.strategies))
	for _, strat := range r.strategies {
		if strat == nil {
			continue
		}
		intent, ok := strat.Detect(ctx)
		if !ok || intent == nil {
			continue
		}
		intents = append(intents, intent)
	}
	return intents
}

// SignalToEntryIntent wraps a legacy Signal into an explicit EntryIntent so
// downstream systems always see a strategy id instead of an anonymous setup.
func SignalToEntryIntent(sig Signal, ctx StrategyContext) *EntryIntent {
	if !sig.Active {
		return nil
	}
	side := SideLong
	if sig.Side == features.SideShort {
		side = SideShort
	}
	reasons := make([]string, 0, len(sig.Reasons)+1)
	if sig.Name != "" {
		reasons = append(reasons, sig.Name)
	}
	reasons = append(reasons, sig.Reasons...)
	intent := &EntryIntent{
		Strategy:        strategyIDFromSignal(sig.Name),
		Symbol:          ctx.Symbol,
		Side:            side,
		Timeframe:       "1m",
		Confidence:      sig.Confidence,
		Score:           ctx.CandidateScore,
		TriggerPrice:    sig.Entry,
		Invalidation:    sig.Stop,
		StopPrice:       sig.Stop,
		TimeStopBars:    0,
		ReasonCodes:     reasons,
		RequiresConfirm: []string{},
		Features:        map[string]float64{},
		CreatedAt:       time.Now().UTC(),
	}
	if sig.TP1 > 0 {
		intent.Targets = append(intent.Targets, Target{Label: "tp1", Price: sig.TP1, Size: 0.50})
	}
	if sig.TP2 > 0 {
		intent.Targets = append(intent.Targets, Target{Label: "tp2", Price: sig.TP2, Size: 0.30})
	}
	if sig.TP3 > 0 {
		intent.Targets = append(intent.Targets, Target{Label: "tp3", Price: sig.TP3, Size: 0.20})
	}
	return intent
}

func strategyIDFromSignal(name string) StrategyID {
	low := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(low, "impulse"), strings.Contains(low, "breakout"), strings.Contains(low, "entry_now"):
		return StrategyImpulseContinuation
	case strings.Contains(low, "vwap"):
		return StrategyAnchoredVWAPPullback
	case strings.Contains(low, "vp"), strings.Contains(low, "volume_profile"), strings.Contains(low, "retest"):
		return StrategyVPRetest
	default:
		return StrategyUnknown
	}
}
