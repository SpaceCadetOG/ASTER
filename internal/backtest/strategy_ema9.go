// go-machine/internal/backtest/strategy_ema9.go
package backtest

import (
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/types"
)

func runEMA9(client *aster.Client, symbol string, tf types.TF, from, to time.Time, cfg Config) ([]Trade, error) {
	// Frames
	biasTF := tfFromString(cfg.BiasTF) // 15m
	setupTF := types.TF5m              // confirmation on 5m
	entryTF := tfFromString(cfg.EntryTf)

	htf, err := loadOHLCV(client, symbol, biasTF, from, to, 300)
	if err != nil || len(htf) < 30 {
		return nil, nil
	}
	mtf, err := loadOHLCV(client, symbol, setupTF, from, to, 600)
	if err != nil || len(mtf) < 60 {
		return nil, nil
	}
	ltf, err := loadOHLCV(client, symbol, entryTF, from, to, 1200)
	if err != nil || len(ltf) < 120 {
		return nil, nil
	}

	// 15m bias
	cHTF := make([]float64, len(htf))
	for i := range htf {
		cHTF[i] = htf[i].C
	}
	emaS := ema(cHTF, max(2, cfg.EMAShort))
	emaL := ema(cHTF, max(2, cfg.EMALong))
	vwH := vwap(htf)
	longBias, shortBias := false, false
	if len(emaS) > 0 && len(emaL) > 0 && len(vwH) > 0 {
		i := len(emaS) - 1
		longBias = (emaS[i] > emaL[i]) && (htf[i].C >= vwH[i])
		shortBias = (emaS[i] < emaL[i]) && (htf[i].C <= vwH[i])
	}
	if !longBias && !shortBias {
		return nil, nil
	}

	// 5m confirmation: EMA9 cross & close above EMA9 for longs (opposite for shorts)
	cMTF := make([]float64, len(mtf))
	for i := range mtf {
		cMTF[i] = mtf[i].C
	}
	ema9 := ema(cMTF, max(2, cfg.EMAShort))
	vwM := vwap(mtf)

	confirmLong := false
	confirmShort := false
	if len(ema9) > 1 {
		i := len(ema9) - 1
		confirmLong = longBias && (mtf[i].C > ema9[i]) && (!cfg.VWAPConfirm || mtf[i].C >= vwM[i])
		confirmShort = shortBias && cfg.AllowShorts && (mtf[i].C < ema9[i]) && (!cfg.VWAPConfirm || mtf[i].C <= vwM[i])
	}
	if !confirmLong && !confirmShort {
		return nil, nil
	}
	side := "long"
	if confirmShort {
		side = "short"
	}

	// 1m trigger: engulfing + volume > avg; vwap confirm if requested
	volAvg := volMA(ltf, max(2, cfg.VolLookback))
	vwL := vwap(ltf)
	entryIdx := -1
	for i := 1; i < len(ltf); i++ {
		ok := false
		switch side {
		case "long":
			ok = bullishEngulfing(ltf, i) && (ltf[i].V > volAvg[i])
			if ok && cfg.VWAPConfirm && vwL[i] > 0 {
				ok = ltf[i].C >= vwL[i]
			}
		case "short":
			ok = bearishEngulfing(ltf, i) && (ltf[i].V > volAvg[i])
			if ok && cfg.VWAPConfirm && vwL[i] > 0 {
				ok = ltf[i].C <= vwL[i]
			}
		}
		if ok {
			entryIdx = i
			break
		}
	}
	if entryIdx < 0 {
		return nil, nil
	}
	entry := ltf[entryIdx].C
	entryT := ltf[entryIdx].T

	// Stop: for long under last 1m swing low (simple: min of previous N lows), for short over last swing high.
	// Keep it simple: use previous candle low/high as micro stop; enforce RR >= 1 quickly to avoid tiny stops.
	var stop float64
	if side == "long" {
		stop = ltf[entryIdx-1].L
	} else {
		stop = ltf[entryIdx-1].H
	}

	// Exit rule: opposite 5m condition or end of window; also TP at +2R if hit intra-run.
	exit := entry
	exitT := ltf[len(ltf)-1].T
	risk := entry - stop
	if side == "short" {
		risk = stop - entry
	}
	tp2R := entry + 2*risk
	if side == "short" {
		tp2R = entry - 2*risk
	}

	// walk forward from entry
	for j := entryIdx + 1; j < len(ltf); j++ {
		price := ltf[j].C
		// take TP2R if reached
		if side == "long" && price >= tp2R {
			exit = tp2R
			exitT = ltf[j].T
			break
		}
		if side == "short" && price <= tp2R {
			exit = tp2R
			exitT = ltf[j].T
			break
		}
		exit = price
		exitT = ltf[j].T
	}

	tr := Trade{
		Symbol:    symbol,
		Side:      side,
		EntryTime: entryT,
		ExitTime:  exitT,
		Entry:     entry,
		Exit:      exit,
	}
	return []Trade{tr}, nil
}
