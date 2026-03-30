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
		t.Fatalf("expected wide-stop clamp, got %+v", res)
	}
	if res.StopDistancePct > cfg.MaxWidthPct+1e-9 {
		t.Fatalf("expected stop width <= %.2f, got %+v", cfg.MaxWidthPct, res)
	}
}

func TestComputeHybridStopAllowsPoorRR(t *testing.T) {
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
	if res.Rejected {
		t.Fatalf("expected rr-low plan to continue, got %+v", res)
	}
}

func TestComputeHybridStopClampsWideEliteStarter(t *testing.T) {
	cfg := DefaultHybridStopConfig()
	cfg.Enabled = true
	cfg.MaxWidthPct = 8.0
	cfg.SoftRejectEnable = true
	cfg.SoftRejectMaxWidthPct = 12.0
	in := HybridStopInput{
		Side:           "SELL",
		Entry:          100,
		SignalStop:     109.0,
		StructureHigh:  110.0,
		ATR:            0.2,
		TargetPrice:    86.0,
		Template:       StopTemplateReversalExhaustion,
		EliteCandidate: true,
	}
	res := ComputeHybridStop(cfg, in)
	if res.Rejected {
		t.Fatalf("expected usable clamped stop, got %+v", res)
	}
	if res.StopPrice <= 0 {
		t.Fatalf("expected usable stop price, got %+v", res)
	}
	if res.StopDistancePct > cfg.MaxWidthPct+1e-9 {
		t.Fatalf("expected stop width <= %.2f, got %+v", cfg.MaxWidthPct, res)
	}
}

func TestComputeHybridStopAllowsElitePoorRR(t *testing.T) {
	cfg := DefaultHybridStopConfig()
	cfg.Enabled = true
	cfg.MinRRToTP1 = 1.5
	cfg.SoftRejectEnable = true
	cfg.SoftRejectMinRRToTP1 = 0.65
	in := HybridStopInput{
		Side:           "BUY",
		Entry:          100,
		SignalStop:     99.0,
		StructureLow:   99.2,
		ATR:            0.1,
		TargetPrice:    100.8,
		Template:       StopTemplateContinuationImpulse,
		EliteCandidate: true,
	}
	res := ComputeHybridStop(cfg, in)
	if res.Rejected {
		t.Fatalf("expected rr-low elite plan to continue, got %+v", res)
	}
}

func TestComputeHybridStopClampsAbsurdWideEliteStop(t *testing.T) {
	cfg := DefaultHybridStopConfig()
	cfg.Enabled = true
	cfg.MaxWidthPct = 8.0
	cfg.SoftRejectEnable = true
	cfg.SoftRejectMaxWidthPct = 12.0
	in := HybridStopInput{
		Side:           "SELL",
		Entry:          100,
		SignalStop:     116.0,
		StructureHigh:  117.0,
		ATR:            0.2,
		TargetPrice:    96.0,
		Template:       StopTemplateReversalExhaustion,
		EliteCandidate: true,
	}
	res := ComputeHybridStop(cfg, in)
	if res.Rejected {
		t.Fatalf("expected absurd width to clamp, got %+v", res)
	}
	if res.StopDistancePct > cfg.MaxWidthPct+1e-9 {
		t.Fatalf("expected stop width <= %.2f, got %+v", cfg.MaxWidthPct, res)
	}
}
