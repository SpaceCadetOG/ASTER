package market

import "fmt"

const (
	MIN_CHANGE  = 5.0
	MIN_VOL_USD = 1_000_000
)

func Eligible(m Market) (bool, string) {
	if m.Change24h < MIN_CHANGE {
		return false, fmt.Sprintf("Δ%% < %.1f", MIN_CHANGE)
	}
	if m.VolumeUSD < MIN_VOL_USD {
		return false, fmt.Sprintf("Vol < $%.0f", float64(MIN_VOL_USD))
	}
	return true, ""
}
