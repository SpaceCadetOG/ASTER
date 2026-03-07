package aster

import "testing"

func TestFormatPrecision(t *testing.T) {
	if got := formatPrecision(1.234567, 2); got != "1.23" {
		t.Fatalf("unexpected precision output: %s", got)
	}
	if got := formatPrecision(1.2, -1); got == "" {
		t.Fatalf("expected formatted string")
	}
}

func TestUpdateBracketRequiresSymbol(t *testing.T) {
	r := &RESTAuth{}
	_, _, err := r.UpdateBracket(BracketUpdate{})
	if err == nil {
		t.Fatalf("expected symbol required error")
	}
}

func TestUpdateBracketNoopWithoutTargets(t *testing.T) {
	r := &RESTAuth{}
	stop, tp, err := r.UpdateBracket(BracketUpdate{Symbol: "BTCUSDT"})
	if err != nil {
		t.Fatalf("expected noop success, got err=%v", err)
	}
	if len(stop) != 0 || len(tp) != 0 {
		t.Fatalf("expected empty outputs on noop update")
	}
}
