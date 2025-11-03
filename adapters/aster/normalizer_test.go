package aster

import "testing"

func TestNormSymbol(t *testing.T) {
	if NormSymbol("BTCUSDT") != "BTC-USD" {
		t.Fatal("BTCUSDT -> BTC-USD expected")
	}
	if NormSymbol("ASTER-USD") != "ASTER-USD" {
		t.Fatal("already dashed should remain")
	}
	if NormSymbol("ETHUSD") != "ETH-USD" {
		t.Fatal("ETHUSD -> ETH-USD expected")
	}
}
