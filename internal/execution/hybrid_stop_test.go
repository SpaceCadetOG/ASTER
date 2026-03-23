package execution

import "testing"

func TestComputeHybridStopLongUsesStructureAndBuffer(t *testing.T) {
	cfg := DefaultHybridStopConfig()
	cfg.Enabled = true
	in := HybridStopInput{
		Side:         "BUY",
		Entry:        100,
		SignalStop:   98.5,
		StructureLow: 99.0,
		SessionVWAP:  99.2,
		EMA9:         99.4,
		ATR:          0.5,
		TargetPrice:  102.0,
		Template:     StopTemplateReclaimPullback,
	}
	res := ComputeHybridStop(cfg, in)
	if res.Rejected {
		t.Fatalf("unexpected reject: %+v", res)
	}
	if res.StopPrice >= 98.5 {
		t.Fatalf("expected sweep-buffered stop below signal stop, got %.4f", res.StopPrice)
	}
	if res.StopReason == "" {
		t.Fatalf("expected stop reason")
	}
}

func TestComputeHybridStopClampsWideStops(t *testing.T) {
	cfg := DefaultHybridStopConfig()
	cfg.Enabled = true
	cfg.MaxWidthPct = 1.0
	in := HybridStopInput{
		Side:          "SELL",
		Entry:         100,
		SignalStop:    103.0,
		StructureHigh: 104.0,
		ATR:           0.2,
		TargetPrice:   99.0,
		Template:      StopTemplateReversalExhaustion,
	}
	res := ComputeHybridStop(cfg, in)
	if res.Rejected {
		t.Fatalf("unexpected wide-stop rejection: %+v", res)
	}
	if res.StopDistancePct > cfg.MaxWidthPct+1e-6 {
		t.Fatalf("expected clamped stop width <= %.2f, got %.4f", cfg.MaxWidthPct, res.StopDistancePct)
	}
}

func TestComputeHybridStopRejectsPoorRR(t *testing.T) {
	cfg := DefaultHybridStopConfig()
	cfg.Enabled = true
	cfg.MinRRToTP1 = 1.5
	in := HybridStopInput{
		Side:         "BUY",
		Entry:        100,
		SignalStop:   99.0,
		StructureLow: 99.2,
		ATR:          0.1,
		TargetPrice:  100.8,
		Template:     StopTemplateContinuationImpulse,
	}
	res := ComputeHybridStop(cfg, in)
	if !res.Rejected || res.RejectReason != "hybrid_stop_rr_too_low" {
		t.Fatalf("expected rr rejection, got %+v", res)
	}
}
