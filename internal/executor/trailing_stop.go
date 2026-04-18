package executor

import (
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
	Symbol               string
	Side                 TrailSide
	EntryPrice           float64
	InitialStop          float64
	TacticalStop         float64
	HardStop             float64
	InitialRisk          float64
	BreakevenMoved       bool
	Last15mClosedCandle  time.Time
	AdvancedReady        bool
	HitTP1               bool
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
		Symbol:      symbol,
		Side:        side,
		EntryPrice:  entryPrice,
		InitialStop: initialStop,
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
