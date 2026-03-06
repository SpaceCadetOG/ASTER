package stats

import (
	"fmt"
	"math"
	"sort"
)

type Row struct {
	Name      string
	Trades    int
	Wins      int
	Losses    int
	WinRate   float64
	PnL       float64
	AvgPnL    float64
	ProfitFac float64
}

type Report struct {
	Simulated   bool
	TotalTrades int
	Wins        int
	Losses      int
	WinRate     float64
	AvgWin      float64
	AvgLoss     float64
	Expectancy  float64
	ProfitFac   float64
	MaxDrawdown float64
	AvgR        float64
	MedianR     float64
	ByStrategy  []Row
	BySymbol    []Row
}

func Aggregate(events []Event) Report {
	var r Report
	strat := map[string]*Row{}
	sym := map[string]*Row{}
	pnlSeq := make([]float64, 0, 256)
	rs := make([]float64, 0, 256)
	grossWin := 0.0
	grossLoss := 0.0
	winSum := 0.0
	lossSum := 0.0
	for _, e := range events {
		if e.Simulated {
			r.Simulated = true
		}
		if e.Type != "POSITION_CLOSE" {
			continue
		}
		r.TotalTrades++
		pnl := e.PnLUSD
		pnlSeq = append(pnlSeq, pnl)
		if e.RiskR != 0 {
			rs = append(rs, e.RiskR)
		}
		if pnl > 0 {
			r.Wins++
			grossWin += pnl
			winSum += pnl
		} else if pnl < 0 {
			r.Losses++
			grossLoss += math.Abs(pnl)
			lossSum += pnl
		}
		st := e.Strategy
		if st == "" {
			st = "unknown"
		}
		if _, ok := strat[st]; !ok {
			strat[st] = &Row{Name: st}
		}
		accRow(strat[st], pnl)
		sy := e.Symbol
		if sy == "" {
			sy = "unknown"
		}
		if _, ok := sym[sy]; !ok {
			sym[sy] = &Row{Name: sy}
		}
		accRow(sym[sy], pnl)
	}
	if r.Wins+r.Losses > 0 {
		r.WinRate = (100.0 * float64(r.Wins)) / float64(r.Wins+r.Losses)
	}
	if r.Wins > 0 {
		r.AvgWin = winSum / float64(r.Wins)
	}
	if r.Losses > 0 {
		r.AvgLoss = lossSum / float64(r.Losses)
	}
	if r.TotalTrades > 0 {
		r.Expectancy = (winSum + lossSum) / float64(r.TotalTrades)
	}
	if grossLoss > 0 {
		r.ProfitFac = grossWin / grossLoss
	}
	r.MaxDrawdown = maxDrawdown(pnlSeq)
	if len(rs) > 0 {
		tot := 0.0
		for _, x := range rs {
			tot += x
		}
		r.AvgR = tot / float64(len(rs))
		r.MedianR = median(rs)
	}
	r.ByStrategy = mapRows(strat)
	r.BySymbol = mapRows(sym)
	return r
}

func accRow(r *Row, pnl float64) {
	r.Trades++
	r.PnL += pnl
	if pnl > 0 {
		r.Wins++
	} else if pnl < 0 {
		r.Losses++
	}
	if r.Wins+r.Losses > 0 {
		r.WinRate = (100.0 * float64(r.Wins)) / float64(r.Wins+r.Losses)
	}
	r.AvgPnL = r.PnL / float64(r.Trades)
	gw, gl := 0.0, 0.0
	if r.PnL > 0 {
		gw = r.PnL
	} else {
		gl = math.Abs(r.PnL)
	}
	if gl > 0 {
		r.ProfitFac = gw / gl
	}
}

func mapRows(m map[string]*Row) []Row {
	out := make([]Row, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Trades == out[j].Trades {
			return out[i].Name < out[j].Name
		}
		return out[i].Trades > out[j].Trades
	})
	return out
}

func maxDrawdown(pnlSeq []float64) float64 {
	eq := 0.0
	peak := 0.0
	dd := 0.0
	for _, p := range pnlSeq {
		eq += p
		if eq > peak {
			peak = eq
		}
		d := peak - eq
		if d > dd {
			dd = d
		}
	}
	return dd
}

func median(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	cp := append([]float64(nil), x...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

func FormatReport(r Report) string {
	sim := "live"
	if r.Simulated {
		sim = "simulated"
	}
	return fmt.Sprintf("stats mode=%s trades=%d wins=%d losses=%d winRate=%.2f%% PF=%.2f expectancy=%.2f maxDD=%.2f avgR=%.2f medianR=%.2f", sim, r.TotalTrades, r.Wins, r.Losses, r.WinRate, r.ProfitFac, r.Expectancy, r.MaxDrawdown, r.AvgR, r.MedianR)
}
