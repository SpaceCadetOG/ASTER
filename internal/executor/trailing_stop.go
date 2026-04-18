package executor

import (
	"math"
	"strings"
	"time"

	"go-machine/internal/features"
)

const (
	DefaultHardStopPct = 0.04
)

type TrailSide string

const (
	SideBuy  TrailSide = "BUY"
	SideSell TrailSide = "SELL"
)

type TrailConfig struct {
	HardStopPct float64
}

type TrailState struct {
	Symbol              string
	Side                TrailSide
	EntryPrice          float64
	InitialStop         float64
	TacticalStop        float64
	HardStop            float64
	InitialRisk         float64
	BreakevenMoved      bool
	Last15mClosedCandle time.Time
	AdvancedReady       bool
	HitTP1              bool
}

type TrailUpdate struct {
	TacticalStopUpdated bool
	BreakevenMoved      bool
	Reason              string
	CurrentTacticalStop float64
	CurrentHardStop     float64
}

func NewTrailState(symbol string, side TrailSide, entryPrice, initialStop float64, cfg TrailConfig) TrailState {
	hardPct := cfg.HardStopPct
	if hardPct <= 0 {
		hardPct = DefaultHardStopPct
	}
	st := TrailState{
		Symbol:       symbol,
		Side:         side,
		EntryPrice:   entryPrice,
		InitialStop:  initialStop,
		TacticalStop: initialStop,
	}
	if strings.EqualFold(string(side), "BUY") {
		st.HardStop = entryPrice * (1 - hardPct)
	} else {
		st.HardStop = entryPrice * (1 + hardPct)
	}
	if entryPrice > 0 && initialStop > 0 {
		if strings.EqualFold(string(side), "BUY") {
			st.InitialRisk = entryPrice - initialStop
		} else {
			st.InitialRisk = initialStop - entryPrice
		}
	}
	return st
}

// UpdateTrail enforces protected trailing behavior:
// 1) Tactical stop updates ONLY on new 15m candle closes.
// 2) Hard stop remains fixed at -4% from entry (or +4% for shorts).
// 3) Stop moves to breakeven once 1R is reached.
func UpdateTrail(st *TrailState, closed15m features.Candle, ema20 float64) TrailUpdate {
	if st == nil {
		return TrailUpdate{Reason: "nil_state"}
	}
	upd := TrailUpdate{
		CurrentTacticalStop: st.TacticalStop,
		CurrentHardStop:     st.HardStop,
		Reason:              "no_change",
	}
	if closed15m.Ts.IsZero() || !closed15m.Ts.After(st.Last15mClosedCandle) {
		return upd
	}
	st.Last15mClosedCandle = closed15m.Ts

	// Breakeven move at >= 1R.
	if !st.BreakevenMoved && st.InitialRisk > 0 && closed15m.C > 0 {
		if reachedOneR(st, closed15m.C) {
			st.BreakevenMoved = true
			if strings.EqualFold(string(st.Side), "BUY") {
				if st.EntryPrice > st.TacticalStop {
					st.TacticalStop = st.EntryPrice
					upd.TacticalStopUpdated = true
				}
			} else {
				if st.EntryPrice < st.TacticalStop || st.TacticalStop <= 0 {
					st.TacticalStop = st.EntryPrice
					upd.TacticalStopUpdated = true
				}
			}
			upd.BreakevenMoved = true
			upd.Reason = "breakeven_1r"
		}
	}

	// Tactical trailing to EMA20, 15m close only, and only after stabilization.
	if (st.AdvancedReady || st.HitTP1) && ema20 > 0 {
		if strings.EqualFold(string(st.Side), "BUY") {
			if ema20 > st.TacticalStop {
				st.TacticalStop = ema20
				upd.TacticalStopUpdated = true
				upd.Reason = "tactical_trail_ema20_close"
			}
		} else {
			if st.TacticalStop <= 0 || ema20 < st.TacticalStop {
				st.TacticalStop = ema20
				upd.TacticalStopUpdated = true
				upd.Reason = "tactical_trail_ema20_close"
			}
		}
	}

	upd.CurrentTacticalStop = st.TacticalStop
	upd.CurrentHardStop = st.HardStop
	return upd
}

// UpdateProtectedTrailOn15mClose computes the tactical anchor from closed 15m candles only.
// It intentionally ignores the developing candle to avoid intrabar stop churn/wick-outs.
func UpdateProtectedTrailOn15mClose(st *TrailState, history []features.Candle, atrMult float64) TrailUpdate {
	if st == nil {
		return TrailUpdate{Reason: "nil_state"}
	}
	closed := lastClosedCandles(history, 20)
	if len(closed) < 20 {
		return TrailUpdate{
			CurrentTacticalStop: st.TacticalStop,
			CurrentHardStop:     st.HardStop,
			Reason:              "insufficient_closed_history",
		}
	}
	if atrMult <= 0 {
		atrMult = 1.5
	}
	ema20 := emaFromClosed(closed, 20)
	atr14 := atrFromClosed(closed, 14)
	anchor := ema20
	if atr14 > 0 {
		if strings.EqualFold(string(st.Side), "BUY") {
			anchor = ema20 - atrMult*atr14
		} else {
			anchor = ema20 + atrMult*atr14
		}
	}
	return UpdateTrail(st, closed[len(closed)-1], anchor)
}

func reachedOneR(st *TrailState, mark float64) bool {
	if st == nil || st.EntryPrice <= 0 || st.InitialRisk <= 0 || mark <= 0 {
		return false
	}
	if strings.EqualFold(string(st.Side), "BUY") {
		return (mark - st.EntryPrice) >= st.InitialRisk
	}
	return (st.EntryPrice - mark) >= st.InitialRisk
}

func HardStopTriggered(st TrailState, mark float64) bool {
	if mark <= 0 || st.HardStop <= 0 {
		return false
	}
	if strings.EqualFold(string(st.Side), "BUY") {
		return mark <= st.HardStop
	}
	return mark >= st.HardStop
}

func lastClosedCandles(history []features.Candle, n int) []features.Candle {
	if n <= 0 || len(history) == 0 {
		return nil
	}
	end := len(history)
	if len(history) > n {
		// Treat the latest candle as developing when enough history exists.
		end = len(history) - 1
	}
	if end <= 0 {
		return nil
	}
	start := end - n
	if start < 0 {
		start = 0
	}
	out := make([]features.Candle, 0, end-start)
	for i := start; i < end; i++ {
		c := history[i]
		if c.C <= 0 || c.H <= 0 || c.L <= 0 || c.Ts.IsZero() {
			continue
		}
		out = append(out, c)
	}
	return out
}

func emaFromClosed(candles []features.Candle, period int) float64 {
	if period <= 1 || len(candles) < period {
		return 0
	}
	k := 2.0 / float64(period+1)
	start := candles[0].C
	if start <= 0 {
		return 0
	}
	out := start
	for i := 1; i < len(candles); i++ {
		if candles[i].C <= 0 {
			continue
		}
		out = candles[i].C*k + out*(1-k)
	}
	return out
}

func atrFromClosed(candles []features.Candle, period int) float64 {
	if period <= 1 || len(candles) < period+1 {
		return 0
	}
	start := len(candles) - period
	var sum float64
	for i := start; i < len(candles); i++ {
		prev := candles[i-1].C
		cur := candles[i]
		if prev <= 0 || cur.H <= 0 || cur.L <= 0 {
			return 0
		}
		tr1 := cur.H - cur.L
		tr2 := math.Abs(cur.H - prev)
		tr3 := math.Abs(cur.L - prev)
		sum += max3(tr1, tr2, tr3)
	}
	return sum / float64(period)
}

func max3(a, b, c float64) float64 {
	if b > a {
		a = b
	}
	if c > a {
		a = c
	}
	return a
}
