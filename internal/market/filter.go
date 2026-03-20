package market

import "fmt"

const (
	MIN_CHANGE  = 1.5
	MIN_VOL_USD = 1_000_000
)

func Eligible(m Market) (bool, string) {
	move := PrimaryMovePct(m)
	if move < MIN_CHANGE {
		return false, fmt.Sprintf("%s < %.1f", primaryMoveLabel(m), MIN_CHANGE)
	}
	if m.VolumeUSD < MIN_VOL_USD {
		return false, fmt.Sprintf("Vol < $%.0f", float64(MIN_VOL_USD))
	}
	return true, ""
}
