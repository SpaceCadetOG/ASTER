package stats

import (
	"fmt"
	"math"
	"sort"
	"strings"
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

type CountRow struct {
	Name  string
	Count int
}

type Report struct {
	Simulated      bool
	TotalTrades    int
	Wins           int
	Losses         int
	WinRate        float64
	AvgWin         float64
	AvgLoss        float64
	Expectancy     float64
	ProfitFac      float64
	MaxDrawdown    float64
	AvgR           float64
	MedianR        float64
	Signals        int
	Entries        int
	Rejects        int
	Missed         int
	LeaderMiss     int
	WatchToEntry   float64
	AvgHoldMin     float64
	AvgMFER        float64
	AvgMAER        float64
	Fees           float64
	Slippage       float64
	FundingPnL     float64
	AvgLoopMs      float64
	CacheHits      int64
	CacheMisses    int64
	CacheHitRate   float64
	CacheEvictions int64
	ByStrategy     []Row
	BySymbol       []Row
	ByReject       []CountRow
	ByExit         []CountRow
	ByMissCat      []CountRow
}

func Aggregate(events []Event) Report {
	var r Report
	strat := map[string]*Row{}
	sym := map[string]*Row{}
	rejects := map[string]int{}
	exits := map[string]int{}
	missCats := map[string]int{}
	pnlSeq := make([]float64, 0, 256)
	rs := make([]float64, 0, 256)
	grossWin := 0.0
	grossLoss := 0.0
	winSum := 0.0
	lossSum := 0.0
	holdSum := 0.0
	holdN := 0
	mfeSum := 0.0
	mfeN := 0
	maeSum := 0.0
	maeN := 0
	loopSum := 0.0
	loopN := 0
	for _, e := range events {
		if e.Simulated {
			r.Simulated = true
		}
		switch e.Type {
		case "SIGNAL":
			r.Signals++
		case "POSITION_OPEN":
			r.Entries++
		case "ORDER_FILL":
			if e.Reason == "entry_accepted" {
				r.Entries++
			}
		case "GATE_DECISION":
			if e.GateAllow != nil && !*e.GateAllow {
				r.Rejects++
				for _, gr := range e.GateReasons {
					if gr == "" {
						continue
					}
					rejects[gr]++
				}
			}
		case "MISSED_OPPORTUNITY":
			r.Missed++
			cat := e.MissCategory
			if cat == "" {
				cat = "uncategorized"
			}
			missCats[cat]++
			if e.Discovery >= 0.85 || e.Score >= 90 {
				r.LeaderMiss++
			}
		case "METRICS_SNAPSHOT":
			if e.LoopMs > 0 {
				loopSum += e.LoopMs
				loopN++
			}
			r.CacheHits += e.CacheHits
			r.CacheMisses += e.CacheMisses
			r.CacheEvictions += e.CacheEvictions
		}
		if e.Type != "POSITION_CLOSE" {
			continue
		}
		r.TotalTrades++
		pnl := e.PnLUSD
		r.Fees += e.Fees
		r.Slippage += math.Abs(e.Slippage)
		if strings.EqualFold(e.Reason, "FUNDING") {
			r.FundingPnL += pnl
		}
		if e.HoldMin > 0 {
			holdSum += e.HoldMin
			holdN++
		}
		if e.MFER != 0 {
			mfeSum += e.MFER
			mfeN++
		}
		if e.MAER != 0 {
			maeSum += e.MAER
			maeN++
		}
		pnlSeq = append(pnlSeq, pnl)
		if e.RiskR != 0 {
			rs = append(rs, e.RiskR)
		}
		exitReason := e.Reason
		if exitReason == "" {
			exitReason = "UNKNOWN"
		}
		exits[exitReason]++
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
	if holdN > 0 {
		r.AvgHoldMin = holdSum / float64(holdN)
	}
	if mfeN > 0 {
		r.AvgMFER = mfeSum / float64(mfeN)
	}
	if maeN > 0 {
		r.AvgMAER = maeSum / float64(maeN)
	}
	if loopN > 0 {
		r.AvgLoopMs = loopSum / float64(loopN)
	}
	if totalCache := r.CacheHits + r.CacheMisses; totalCache > 0 {
		r.CacheHitRate = (100.0 * float64(r.CacheHits)) / float64(totalCache)
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
	if r.Signals > 0 {
		r.WatchToEntry = (100.0 * float64(r.Entries)) / float64(r.Signals)
	}
	r.ByStrategy = mapRows(strat)
	r.BySymbol = mapRows(sym)
	r.ByReject = countRows(rejects)
	r.ByExit = countRows(exits)
	r.ByMissCat = countRows(missCats)
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

func countRows(m map[string]int) []CountRow {
	out := make([]CountRow, 0, len(m))
	for k, v := range m {
		out = append(out, CountRow{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Name < out[j].Name
		}
		return out[i].Count > out[j].Count
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
	return fmt.Sprintf("stats mode=%s trades=%d wins=%d losses=%d winRate=%.2f%% PF=%.2f expectancy=%.2f maxDD=%.2f avgR=%.2f medianR=%.2f signals=%d entries=%d conv=%.2f%% missed=%d avgHold=%.1fm", sim, r.TotalTrades, r.Wins, r.Losses, r.WinRate, r.ProfitFac, r.Expectancy, r.MaxDrawdown, r.AvgR, r.MedianR, r.Signals, r.Entries, r.WatchToEntry, r.Missed, r.AvgHoldMin)
}
