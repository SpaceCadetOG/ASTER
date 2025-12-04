// go-machine/internal/backtest/indicators.go
package backtest

import (
	"math"
	"time"
)

type ohlcv struct {
	T time.Time
	O float64
	H float64
	L float64
	C float64
	V float64
}

func ema(series []float64, period int) []float64 {
	if period <= 1 || len(series) == 0 {
		return append([]float64(nil), series...)
	}
	out := make([]float64, len(series))
	k := 2.0 / (float64(period) + 1.0)
	out[0] = series[0]
	for i := 1; i < len(series); i++ {
		out[i] = series[i]*k + out[i-1]*(1.0-k)
	}
	return out
}

func vwap(bars []ohlcv) []float64 {
	out := make([]float64, len(bars))
	var pv, vol float64
	for i, b := range bars {
		tp := (b.H + b.L + b.C) / 3.0
		pv += tp * b.V
		vol += b.V
		if vol == 0 {
			out[i] = 0
		} else {
			out[i] = pv / vol
		}
	}
	return out
}

func volMA(bars []ohlcv, n int) []float64 {
	if n < 1 {
		n = 1
	}
	out := make([]float64, len(bars))
	var sum float64
	for i := 0; i < len(bars); i++ {
		sum += bars[i].V
		if i >= n {
			sum -= bars[i-n].V
		}
		win := i + 1
		if win > n {
			win = n
		}
		out[i] = sum / float64(win)
	}
	return out
}

// simple pivot swing (highest high & lowest low over a lookback window)
func lastSwing(bars []ohlcv, lookback int) (loIdx int, hiIdx int) {
	if len(bars) == 0 {
		return -1, -1
	}
	start := 0
	if lookback > 0 && lookback < len(bars) {
		start = len(bars) - lookback
	}
	loIdx, hiIdx = start, start
	for i := start; i < len(bars); i++ {
		if bars[i].L < bars[loIdx].L {
			loIdx = i
		}
		if bars[i].H > bars[hiIdx].H {
			hiIdx = i
		}
	}
	return loIdx, hiIdx
}

func bullishEngulfing(bars []ohlcv, i int) bool {
	// needs i>=1
	if i < 1 || i >= len(bars) {
		return false
	}
	// previous red: close<open, current green: close>open and close>prevOpen, open<prevClose
	p := bars[i-1]
	c := bars[i]
	return p.C < p.O && c.C > c.O && c.C >= p.O && c.O <= p.C
}

func bearishEngulfing(bars []ohlcv, i int) bool {
	if i < 1 || i >= len(bars) {
		return false
	}
	p := bars[i-1]
	c := bars[i]
	return p.C > p.O && c.C < c.O && c.C <= p.O && c.O >= p.C
}

func rr(entry, stop, target float64, side string) float64 {
	// return R multiple to target from entry relative to stop
	switch side {
	case "long":
		risk := entry - stop
		reward := target - entry
		if risk <= 0 {
			return 0
		}
		return reward / risk
	case "short":
		risk := stop - entry
		reward := entry - target
		if risk <= 0 {
			return 0
		}
		return reward / risk
	default:
		return 0
	}
}

func clamp(x, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, x))
}