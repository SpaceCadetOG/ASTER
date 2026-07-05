package mlschema

import (
	"math"
	"strings"
)

func WinFromRealizedR(realizedR float64) bool {
	return realizedR > 0
}

func StopMovedToBE(stopType string) bool {
	stopType = strings.ToLower(strings.TrimSpace(stopType))
	return stopType == "breakeven" || stopType == "profit_lock"
}

func ReversalAfterExit(realizedR, eodR float64, stoppedThenReclaim bool) bool {
	return !stoppedThenReclaim && realizedR < 0 && eodR < realizedR
}

func NormalizeSide(side string) string {
	side = strings.ToUpper(strings.TrimSpace(side))
	switch side {
	case "SELL", "SHORT":
		return "SELL"
	default:
		return "BUY"
	}
}

func SafeDiv(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return num / den
}

func Rounded(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}
