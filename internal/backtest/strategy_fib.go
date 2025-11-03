// go-machine/internal/backtest/strategy_fib.go
package backtest

import (
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/types"
)

func runFib(client *aster.Client, symbol string, tf types.TF, from, to time.Time, cfg Config) ([]Trade, error) {
	// 1) Load frames
	biasTF := tfFromString(cfg.BiasTF) // e.g., 15m
	// Use 5m as setup frame if the main tf is larger; else use provided tf
	setupTF := tf
	if tfMinutes(tf) > 5 {
		setupTF = types.TF5m
	}
	entryTF := tfFromString(cfg.EntryTf) // usually 1m

	htf, err := loadOHLCV(client, symbol, biasTF, from, to, 200)
	if err != nil || len(htf) < 20 {
		return nil, nil
	}
	mtf, err := loadOHLCV(client, symbol, setupTF, from, to, 200)
	if err != nil || len(mtf) < 20 {
		return nil, nil
	}
	ltf, err := loadOHLCV(client, symbol, entryTF, from, to, 400)
	if err != nil || len(ltf) < 20 {
		return nil, nil
	}

	// 2) 15m bias using EMA9/20 + VWAP
	closeHTF := make([]float64, len(htf))
	for i := range htf {
		closeHTF[i] = htf[i].C
	}
	ema9 := ema(closeHTF, max(2, cfg.EMAShort))
	ema20 := ema(closeHTF, max(2, cfg.EMALong))
	vw := vwap(htf)

	longBias, shortBias := false, false
	if len(ema9) > 0 && len(ema20) > 0 && len(vw) > 0 {
		last := len(ema9) - 1
		longBias = (ema9[last] > ema20[last]) && (htf[last].C >= vw[last])
		shortBias = (ema9[last] < ema20[last]) && (htf[last].C <= vw[last])
	}
	if !longBias && !shortBias {
		return nil, nil // no trade if no bias
	}

	// 3) 5m swing: use last N bars for swing detection
	loIdx, hiIdx := lastSwing(mtf, 120)
	if loIdx < 0 || hiIdx < 0 || loIdx == hiIdx {
		return nil, nil
	}

	var swingLow, swingHigh float64
	var side string

	// Choose direction by bias, NOT by which index comes first (fixes earlier "wrong side" issue)
	if longBias && !shortBias {
		side = "long"
		if mtf[loIdx].L >= mtf[hiIdx].H {
			return nil, nil // degenerate
		}
		swingLow, swingHigh = mtf[loIdx].L, mtf[hiIdx].H
	} else if shortBias && cfg.AllowShorts {
		side = "short"
		if mtf[hiIdx].H <= mtf[loIdx].L {
			return nil, nil
		}
		swingLow, swingHigh = mtf[loIdx].L, mtf[hiIdx].H
	} else {
		return nil, nil
	}

	// Fib levels for LONG: measured low -> high
	// Entry zone: 0.5..0.618 with small buffer
	var entryMin, entryMax, stop, tp1 float64
	switch side {
	case "long":
		f50 := swingLow + 0.5*(swingHigh-swingLow)
		f618 := swingLow + 0.618*(swingHigh-swingLow)
		entryMin, entryMax = f50, f618
		stop = f618 * (1.0 - 0.008) // ~0.8% buffer
		tp1 = swingHigh             // 1.0
	case "short":
		// For shorts, measure high->low; "entry" is a pullback to 0.5..0.618
		f50 := swingHigh - 0.5*(swingHigh-swingLow)
		f618 := swingHigh - 0.618*(swingHigh-swingLow)
		// Order entryMin <= entryMax
		if f50 > f618 {
			f50, f618 = f618, f50
		}
		entryMin, entryMax = f50, f618
		stop = f618 * (1.0 + 0.008)
		tp1 = swingLow
	}

	// R:R gate to TP1 >= cfg.RRMin
	// Use mid of the zone for entry estimate
	entryEst := (entryMin + entryMax) / 2.0
	if rr(entryEst, stop, tp1, side) < clamp(cfg.RRMin, 1.0, 10.0) {
		return nil, nil
	}

	// 4) 1m trigger inside zone with volume + (optionally VWAP reclaim)
	// Prepare 1m helpers
	volAvg := volMA(ltf, max(2, cfg.VolLookback))
	vw1 := vwap(ltf)
	// Find first 1m bar in zone that also satisfies trigger
	entryIdx := -1
	for i := 1; i < len(ltf); i++ {
		price := ltf[i].C
		inZone := (price >= entryMin && price <= entryMax)
		if !inZone {
			continue
		}
		// Trigger:
		ok := false
		switch side {
		case "long":
			ok = bullishEngulfing(ltf, i) && (ltf[i].V > volAvg[i])
			if ok && cfg.VWAPConfirm && vw1[i] > 0 {
				ok = price >= vw1[i]
			}
		case "short":
			ok = bearishEngulfing(ltf, i) && (ltf[i].V > volAvg[i])
			if ok && cfg.VWAPConfirm && vw1[i] > 0 {
				ok = price <= vw1[i]
			}
		}
		if ok {
			entryIdx = i
			entryEst = ltf[i].C // refine to actual bar
			break
		}
	}
	if entryIdx < 0 {
		return nil, nil
	}

	// Exit: for now, exit at TP1 touch or at window end, whichever comes first.
	exitPx := entryEst
	exitT := ltf[len(ltf)-1].T
	for j := entryIdx; j < len(ltf); j++ {
		switch side {
		case "long":
			if ltf[j].H >= tp1 {
				exitPx = tp1
				exitT = ltf[j].T
				j = len(ltf)
			} else {
				exitPx = ltf[j].C
				exitT = ltf[j].T
			}
		case "short":
			if ltf[j].L <= tp1 {
				exitPx = tp1
				exitT = ltf[j].T
				j = len(ltf)
			} else {
				exitPx = ltf[j].C
				exitT = ltf[j].T
			}
		}
	}

	tr := Trade{
		Symbol:    symbol,
		Side:      side,
		EntryTime: ltf[entryIdx].T,
		ExitTime:  exitT,
		Entry:     entryEst,
		Exit:      exitPx,
	}
	return []Trade{tr}, nil
}
