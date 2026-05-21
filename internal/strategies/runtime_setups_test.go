package strategies

import (
	"testing"
	"time"

	"go-machine/internal/features"
)

func TestApplySharedInvalidationsPreservesReason(t *testing.T) {
	ctx := Context{
		Runtime: &RuntimeSignalContext{
			CandidateState: "cooling",
			LastClose:      98,
			SessionVWAP:    100,
			EMA9:           99,
		},
	}
	sig := Signal{
		Active: true,
		Name:   "fa",
		Side:   features.SideLong,
		Ts:     time.Now(),
	}
	out := ApplySharedInvalidations(ctx, sig)
	if out.Active {
		t.Fatalf("expected invalidated signal")
	}
	if out.RejectReason != "VWAP_EMA_LONG_INVALIDATION" {
		t.Fatalf("unexpected reject reason: %s", out.RejectReason)
	}
}

func TestEvaluateRuntimeSignalExhaustionFlipShortParity(t *testing.T) {
	ctx := Context{
		EntryStyle: "reversal_watch_short",
		MetaState:  "long_exhausting",
		Candles: []features.Candle{
			{H: 110, C: 100, V: 10},
			{H: 108, C: 99, V: 12},
		},
		Runtime: &RuntimeSignalContext{
			RequestedStrategy:     "exhaustion_flip_short",
			Side:                  features.SideShort,
			LastClose:             99,
			SessionVWAP:           100,
			EMA9:                  101,
			FastSlope:             -0.2,
			SlowSlope:             -0.1,
			OFIZ:                  -1.1,
			OFISamples:            10,
			FailedReclaimCount:    1,
			FailedBounceCount:     1,
			BarsSincePeak:         0,
			DrawdownFromPeakPct:   -7,
			IntradayReversalScore: 3,
		},
	}
	sig, handled := EvaluateRuntimeSignal(ctx)
	if !handled {
		t.Fatalf("expected handled signal")
	}
	if !sig.Active || sig.Name != "exhaustion_flip_short" || sig.Side != features.SideShort {
		t.Fatalf("unexpected signal: %+v", sig)
	}
	if sig.Entry != 99 || sig.Stop <= sig.Entry || sig.TP1 >= sig.Entry {
		t.Fatalf("unexpected geometry: %+v", sig)
	}
}

func TestEvaluateRuntimeSignalMomentumReversalShortRejectPreserved(t *testing.T) {
	ctx := Context{
		Candles: []features.Candle{
			{V: 10},
			{V: 10},
			{V: 10},
		},
		Runtime: &RuntimeSignalContext{
			RequestedStrategy: "mom_reversal_short",
			Side:              features.SideShort,
			LastClose:         101,
			EMA9:              100,
		},
	}
	sig, handled := EvaluateRuntimeSignal(ctx)
	if !handled {
		t.Fatalf("expected handled strategy")
	}
	if sig.RejectReason != "mom_reversal_short_not_ready" {
		t.Fatalf("unexpected reject reason: %s", sig.RejectReason)
	}
}
