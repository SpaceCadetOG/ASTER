package strategies

import (
	"strings"

	"go-machine/internal/features"
	"go-machine/internal/risk"
)

type PreflightVerdict struct {
	Checked  bool
	Approved bool
	Source   string
	Reason   string
	Reasons  []string
	Quality  EntryQualityAccumulator
}

type AdmissionSummary struct {
	LifecycleStage string
	TriggerStage   string
	TriggerState   string
	CandidateGrade string
	CandidateScore float64
	FinalRank      float64
}

type ExecutionDecision struct {
	Symbol       string
	Side         features.Side
	Signal       Signal
	RiskDecision risk.Decision
	Preflight    PreflightVerdict
	Quality      EntryQualityAccumulator
	Admission    AdmissionSummary
	Approved     bool
	RejectReason string
	Rejects      []string
	Entry        float64
	Stop         float64
	Targets      []Target
	Provenance   []string
}

type EntryQualityAccumulator struct {
	HardBlockReasons    []string
	QualityFlags        []string
	PenaltyTotal        float64
	ScoreBefore         float64
	ScoreAfterPenalties float64
	MinScore            float64
	BlockReason         string
}

func NewExecutionDecision(symbol string, sig Signal, riskDec risk.Decision, preflight PreflightVerdict, admission AdmissionSummary, provenance ...string) ExecutionDecision {
	out := ExecutionDecision{
		Symbol:       strings.ToUpper(strings.TrimSpace(symbol)),
		Side:         sig.Side,
		Signal:       sig,
		RiskDecision: riskDec,
		Preflight:    preflight,
		Quality:      preflight.Quality,
		Admission:    admission,
		Entry:        sig.Entry,
		Stop:         sig.Stop,
		Targets:      signalTargets(sig),
		Provenance:   append([]string(nil), provenance...),
	}
	rejects := make([]string, 0, 4)
	if rr := strings.TrimSpace(sig.RejectReason); rr != "" {
		rejects = append(rejects, rr)
	}
	if !riskDec.Approved {
		if rr := strings.TrimSpace(riskDec.RejectReason); rr != "" {
			rejects = append(rejects, rr)
		}
	}
	if preflight.Checked && !preflight.Approved {
		if rr := strings.TrimSpace(preflight.Reason); rr != "" {
			rejects = append(rejects, rr)
		}
	}
	if rr := strings.TrimSpace(preflight.Quality.BlockReason); rr != "" && !containsDecisionReject(rejects, rr) {
		rejects = append(rejects, rr)
	}
	out.Rejects = rejects
	out.RejectReason = firstDecisionReject(sig.RejectReason, riskDec.RejectReason, preflight.Reason, preflight.Quality.BlockReason)
	out.Approved = sig.Active && strings.TrimSpace(sig.RejectReason) == "" && riskDec.Approved && (!preflight.Checked || preflight.Approved)
	return out
}

func signalTargets(sig Signal) []Target {
	out := make([]Target, 0, 3)
	if sig.TP1 > 0 {
		out = append(out, Target{Label: "tp1", Price: sig.TP1, Size: 0.50})
	}
	if sig.TP2 > 0 {
		out = append(out, Target{Label: "tp2", Price: sig.TP2, Size: 0.30})
	}
	if sig.TP3 > 0 {
		out = append(out, Target{Label: "tp3", Price: sig.TP3, Size: 0.20})
	}
	return out
}

func firstDecisionReject(parts ...string) string {
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			return p
		}
	}
	return ""
}

func containsDecisionReject(in []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, item := range in {
		if strings.TrimSpace(item) == target {
			return true
		}
	}
	return false
}
