package aster

import "strings"

// NormSymbol converts exchange symbols to internal dashed form.
//
//	"BTCUSDT" -> "BTC-USD"
//	"ETHBTC"  -> "ETH-BTC"
//	"SOLUSDT" -> "SOL-USD"
//
// Leaves already-dashed forms alone.
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
	// BTC-margined: ETHBTC -> ETH-BTC
	if strings.HasSuffix(sym, "BTC") && sym != "BTC" {
		return strings.TrimSuffix(sym, "BTC") + "-BTC"
	}
	if strings.HasSuffix(sym, "ETH") && sym != "ETH" {
		return strings.TrimSuffix(sym, "ETH") + "-ETH"
	}
	return sym
}

// RawSymbol converts internal dashed form back to exchange symbol.
//
//	"BTC-USD"  -> "BTCUSDT"
//	"ETH-BTC"  -> "ETHBTC"
//	"SOL-USD"  -> "SOLUSDT"
//
// Leaves already-raw forms alone.
func RawSymbol(sym string) string {
	if sym == "" {
		return sym
	}
	if strings.Contains(sym, "-") {
		if strings.HasSuffix(sym, "-USD") {
			return strings.TrimSuffix(sym, "-USD") + "USDT"
		}
		if strings.HasSuffix(sym, "-BTC") {
			return strings.TrimSuffix(sym, "-BTC") + "BTC"
		}
		if strings.HasSuffix(sym, "-ETH") {
			return strings.TrimSuffix(sym, "-ETH") + "ETH"
		}
		// fallback: remove dashes
		return strings.ReplaceAll(sym, "-", "")
	}
	return sym
}

// MatchesQuoteAsset checks if a raw exchange symbol ends with one of the given quote assets.
func MatchesQuoteAsset(sym string, quoteAssets []string) bool {
	for _, q := range quoteAssets {
		if strings.HasSuffix(sym, q) {
			return true
		}
	}
	return false
}
