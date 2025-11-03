package aster

import "strings"

// NormSymbol converts "BTCUSDT" -> "BTC-USD". Leaves already dashed forms alone.
func NormSymbol(sym string) string {
	if strings.Contains(sym, "-") {
		return sym
	}
	if strings.HasSuffix(sym, "USDT") {
		return strings.TrimSuffix(sym, "USDT") + "-USD"
	}
	if strings.HasSuffix(sym, "USD") {
		return strings.TrimSuffix(sym, "USD") + "-USD"
	}
	return sym
}
