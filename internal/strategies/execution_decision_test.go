package strategies

import (
	"testing"

	"go-machine/internal/features"
	"go-machine/internal/risk"
)

func TestExecutionDecisionPreservesSignalGeometry(t *testing.T) {
	sig := Signal{
		Active:     true,
		Name:       "x",
		Side:       features.SideLong,
		Entry:      100,
		Stop:       99,
		TP1:        101,
		TP2:        102,
		TP3:        103,
		Confidence: 0.7,
	}
	dec := NewExecutionDecision("btcusdt", sig, risk.Decision{Approved: true}, PreflightVerdict{}, AdmissionSummary{}, "test")
	if dec.Symbol != "BTCUSDT" {
		t.Fatalf("unexpected symbol: %s", dec.Symbol)
	}
	if dec.Signal.Name != sig.Name || dec.Entry != sig.Entry || dec.Stop != sig.Stop {
		t.Fatalf("signal geometry not preserved")
	}
	if len(dec.Targets) != 3 || dec.Targets[0].Price != sig.TP1 || dec.Targets[2].Price != sig.TP3 {
		t.Fatalf("targets not preserved: %+v", dec.Targets)
	}
	if !dec.Approved {
		t.Fatalf("expected approved decision")
	}
}

func TestExecutionDecisionRejectPropagation(t *testing.T) {
	sig := Signal{
		Name:         "x",
		Side:         features.SideShort,
		RejectReason: "signal_reject",
	}
	dec := NewExecutionDecision("ethusdt", sig, risk.Decision{Approved: false, RejectReason: "risk_reject"}, PreflightVerdict{
		Checked:  true,
		Approved: false,
		Reason:   "preflight_reject",
	}, AdmissionSummary{}, "test")
	if dec.Approved {
		t.Fatalf("expected rejected decision")
	}
	if dec.RejectReason != "signal_reject" {
		t.Fatalf("expected primary reject reason preserved, got %s", dec.RejectReason)
	}
	if len(dec.Rejects) != 3 {
		t.Fatalf("expected all reject reasons, got %+v", dec.Rejects)
	}
	if dec.RiskDecision.RejectReason != "risk_reject" {
		t.Fatalf("risk decision not preserved")
	}
}

func TestExecutionDecisionQualityBlockDisablesApproval(t *testing.T) {
	sig := Signal{
		Active: true,
		Name:   "x",
		Side:   features.SideLong,
		Entry:  100,
		Stop:   99,
		TP1:    101,
	}
	dec := NewExecutionDecision("solusdt", sig, risk.Decision{Approved: true}, PreflightVerdict{
		Checked:  true,
		Approved: true,
		Quality: EntryQualityAccumulator{
			BlockReason: "quality_score_too_low",
		},
	}, AdmissionSummary{}, "test")
	if dec.Approved {
		t.Fatalf("expected quality block to disable approval")
	}
	if dec.RejectReason != "quality_score_too_low" {
		t.Fatalf("expected quality_score_too_low reject reason, got %q", dec.RejectReason)
	}
	if len(dec.Rejects) != 1 || dec.Rejects[0] != "quality_score_too_low" {
		t.Fatalf("expected quality reject to propagate, got %+v", dec.Rejects)
	}
}

func TestExecutionDecisionInactiveSignalProducesReject(t *testing.T) {
	dec := NewExecutionDecision("xrpusdt", Signal{
		Active: false,
		Name:   "x",
		Side:   features.SideLong,
	}, risk.Decision{Approved: true}, PreflightVerdict{
		Checked:  true,
		Approved: true,
	}, AdmissionSummary{}, "test")
	if dec.Approved {
		t.Fatalf("expected inactive signal to be rejected")
	}
	if dec.RejectReason != "signal_inactive" {
		t.Fatalf("expected signal_inactive reject reason, got %q", dec.RejectReason)
	}
	if len(dec.Rejects) != 1 || dec.Rejects[0] != "signal_inactive" {
		t.Fatalf("expected signal_inactive reject list, got %+v", dec.Rejects)
	}
}

func TestExecutionDecisionRiskRejectWithoutReasonStillDisablesApproval(t *testing.T) {
	sig := Signal{
		Active: true,
		Name:   "x",
		Side:   features.SideShort,
		Entry:  100,
		Stop:   101,
		TP1:    99,
	}
	dec := NewExecutionDecision("adausdt", sig, risk.Decision{Approved: false}, PreflightVerdict{
		Checked:  true,
		Approved: true,
	}, AdmissionSummary{}, "test")
	if dec.Approved {
		t.Fatalf("expected risk reject to disable approval")
	}
	if dec.RejectReason != "risk_reject" {
		t.Fatalf("expected risk_reject fallback reason, got %q", dec.RejectReason)
	}
	if len(dec.Rejects) != 1 || dec.Rejects[0] != "risk_reject" {
		t.Fatalf("expected risk_reject in rejects, got %+v", dec.Rejects)
	}
}
