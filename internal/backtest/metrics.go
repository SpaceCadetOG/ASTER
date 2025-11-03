// go-machine/internal/backtest/metrics.go
package backtest

import (
	"math"
	"time"
)

// buildDailyEQ creates a minimal equity curve (start → end).
// Expand later to a true day-by-day curve if needed.
func buildDailyEQ(res *Result) {
	res.DailyEQ = []DailyPoint{
		{Day: res.Start, Equity: res.Equity0},
		{Day: res.End, Equity: res.EquityN},
	}
}

// fillMetrics rolls up PnL, win-rate, PF, DD, Sharpe (basic), exposure, etc.
func fillMetrics(res *Result) {
	var net, winsAmt, lossAmt float64
	var wins, losses int
	best := math.Inf(-1)
	worst := math.Inf(1)

	for _, t := range res.Trades {
		net += t.NetPnL
		if t.NetPnL >= 0 {
			wins++
			winsAmt += t.NetPnL
		} else {
			losses++
			lossAmt += -t.NetPnL
		}
		if t.NetPnL > best {
			best = t.NetPnL
		}
		if t.NetPnL < worst {
			worst = t.NetPnL
		}
	}
	n := len(res.Trades)

	winRate := 0.0
	avgTrade := 0.0
	if n > 0 {
		winRate = 100 * float64(wins) / float64(n)
		avgTrade = net / float64(n)
	}

	// Profit Factor
	pf := 0.0
	if lossAmt == 0 {
		if winsAmt > 0 {
			pf = 999
		}
	} else {
		pf = winsAmt / lossAmt
	}

	// Max drawdown & Sharpe — simple placeholders based on the 2-point equity curve
	maxDD := 0.0
	sharpe := 0.0
	if len(res.DailyEQ) >= 2 {
		e0 := res.DailyEQ[0].Equity
		en := res.DailyEQ[len(res.DailyEQ)-1].Equity
		if e0 > 0 {
			ret := (en - e0) / e0 // total period return
			// fake daily volatility to avoid div-by-zero in tiny tests
			vol := 0.000001
			sharpe = ret / vol
		}
		// With only 2 points we can’t infer drawdown properly; keep 0 for now.
		maxDD = 0
	}

	// Exposure: total time in market across trades ÷ test period
	exp := 0.0
	total := res.End.Sub(res.Start)
	if total > 0 {
		var held time.Duration
		for _, t := range res.Trades {
			et := t.ExitTime
			if et.Before(t.EntryTime) {
				et = t.EntryTime
			}
			// clamp to test window
			if t.EntryTime.Before(res.Start) {
				t.EntryTime = res.Start
			}
			if et.After(res.End) {
				et = res.End
			}
			if et.After(t.EntryTime) {
				held += et.Sub(t.EntryTime)
			}
		}
		exp = float64(held) / float64(total)
	}

	res.Metrics = Metrics{
		NetPNL:       net,
		WinRate:      winRate,
		AvgTrade:     avgTrade,
		ProfitFactor: pf,
		MaxDD:        maxDD,
		Sharpe:       sharpe,
		Exposure:     exp,
		NumTrades:    n,
	}
}
