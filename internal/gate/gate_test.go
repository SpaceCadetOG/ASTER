package gate

import "testing"

func TestEvaluatePass(t *testing.T) {
	cfg := DefaultConfig()
	d := Evaluate(Input{
		Symbol:      "BTCUSDT",
		Side:        "BUY",
		Grade:       "A",
		Score:       88,
		Slope:       0.35,
		VolumeRatio: 2.1,
		MTF: []MTFSnapshot{
			{TF: "1m", EMAFast: 101, EMASlow: 100},
			{TF: "5m", EMAFast: 202, EMASlow: 200},
		},
	}, cfg)
	if !d.Allow {
		t.Fatalf("expected pass, got %+v", d.Reasons)
	}
}

func TestEvaluateDeny(t *testing.T) {
	cfg := DefaultConfig()
	d := Evaluate(Input{
		Symbol:      "BTCUSDT",
		Side:        "BUY",
		Grade:       "C",
		Score:       64,
		Slope:       0.02,
		VolumeRatio: 1.1,
		MTF:         []MTFSnapshot{{TF: "1m", EMAFast: 99, EMASlow: 100}},
	}, cfg)
	if d.Allow {
		t.Fatalf("expected deny")
	}
	if len(d.Reasons) < 3 {
		t.Fatalf("expected multiple deny reasons, got %+v", d.Reasons)
	}
}
