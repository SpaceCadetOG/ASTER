package structure

import "go-machine/internal/types"

// SwingHighIdx returns indices where High[k] > all neighbors ±lookback
func SwingHighIdx(c []types.Candle, lookback int) []int {
	idx := []int{}
	for i := lookback; i < len(c)-lookback; i++ {
		high := c[i].H
		isHigh := true
		for j := i - lookback; j <= i+lookback; j++ {
			if c[j].H > high {
				isHigh = false
				break
			}
		}
		if isHigh {
			idx = append(idx, i)
		}
	}
	return idx
}

// SwingLowIdx symmetric to SwingHighIdx
func SwingLowIdx(c []types.Candle, lookback int) []int {
	idx := []int{}
	for i := lookback; i < len(c)-lookback; i++ {
		low := c[i].L
		isLow := true
		for j := i - lookback; j <= i+lookback; j++ {
			if c[j].L < low {
				isLow = false
				break
			}
		}
		if isLow {
			idx = append(idx, i)
		}
	}
	return idx
}
