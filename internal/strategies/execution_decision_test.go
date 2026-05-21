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
