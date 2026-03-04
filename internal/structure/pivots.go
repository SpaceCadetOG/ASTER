package structure

import "go-machine/internal/types"

// SwingHighIdx returns indices where High[k] > all neighbors ±lookback
func SwingHighIdx(c []types.Candle, lookback int) []int {
	if len(c) == 0 || lookback < 0 {
		return nil
	}
	idx := []int{}
	for i := lookback; i < len(c); i++ {
		high := c[i].H
		isHigh := true
		lo := i - lookback
		if lo < 0 {
			lo = 0
		}
		hi := i + lookback
		if hi >= len(c) {
			hi = len(c) - 1
		}
		for j := lo; j <= hi; j++ {
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
	if len(c) == 0 || lookback < 0 {
		return nil
	}
	idx := []int{}
	for i := lookback; i < len(c); i++ {
		low := c[i].L
		isLow := true
		lo := i - lookback
		if lo < 0 {
			lo = 0
		}
		hi := i + lookback
		if hi >= len(c) {
			hi = len(c) - 1
		}
		for j := lo; j <= hi; j++ {
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
