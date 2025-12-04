// go-machine/internal/backtest/engine.go
package backtest

import (
	"fmt"
	"os"
	"sort"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/types"
)

type mbar = bar // legacy alias

func Run(client *aster.Client, symbols []string, tf types.TF, from, to time.Time, cfg Config) (Result, error) {
	// defaults
	if cfg.InitialEquity <= 0 {
		cfg.InitialEquity = 10000
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 2
	}
	if cfg.PerTradeFrac <= 0 || cfg.PerTradeFrac > 1 {
		cfg.PerTradeFrac = 0.50
	}
	if cfg.FeeBps <= 0 {
		cfg.FeeBps = 4
	}
	if cfg.SlipBps <= 0 {
		cfg.SlipBps = 5
	}
	if cfg.RRMin <= 0 {
		cfg.RRMin = 2.0
	}
	if cfg.EMAShort <= 0 {
		cfg.EMAShort = 9
	}
	if cfg.EMALong <= 0 {
		cfg.EMALong = 20
	}
	if cfg.VolLookback <= 0 {
		cfg.VolLookback = 20
	}
	if cfg.BiasTF == "" {
		cfg.BiasTF = "15m"
	}
	if cfg.EntryTf == "" {
		cfg.EntryTf = "1m"
	}

	res := Result{
		Start:     from,
		End:       to,
		TF:        tf.String(),
		Equity0:   cfg.InitialEquity,
		EquityN:   cfg.InitialEquity,
		Trades:    []Trade{},
		DailyEQ:   []DailyPoint{},
		PerSymbol: map[string]SymbolStat{},
	}

	debug := os.Getenv("BACKTEST_DEBUG") == "1"

	openPositions := 0
	equity := cfg.InitialEquity
	perTradeCash := equity * cfg.PerTradeFrac

	for _, sym := range symbols {
		if openPositions >= cfg.MaxConcurrent {
			break
		}

		plans, err := runStrategy(client, sym, tf, from, to, cfg)
		if err != nil || len(plans) == 0 {
			if debug && err != nil {
				fmt.Printf("[DEBUG] %s: strategy error: %v\n", sym, err)
			}
			continue
		}

		// take first plan for now
		tr := plans[0]
		if tr.Entry <= 0 || tr.Exit <= 0 {
			continue
		}

		qty := perTradeCash / tr.Entry
		notionalIn := tr.Entry * qty
		notionalOut := tr.Exit * qty

		feeIn := (cfg.FeeBps / 10000.0) * notionalIn
		feeOut := (cfg.FeeBps / 10000.0) * notionalOut
		slipIn := (cfg.SlipBps / 10000.0) * notionalIn
		slipOut := (cfg.SlipBps / 10000.0) * notionalOut
		costs := feeIn + feeOut + slipIn + slipOut

		var gross float64
		if tr.Side == "long" {
			gross = (tr.Exit - tr.Entry) * qty
		} else { // short
			gross = (tr.Entry - tr.Exit) * qty
		}
		net := gross - costs
		retPct := 100 * (net / perTradeCash)

		tr.Qty = qty
		tr.GrossPnL = gross
		tr.Fees = costs
		tr.NetPnL = net
		tr.ReturnPct = retPct

		res.Trades = append(res.Trades, tr)
		equity += net
		openPositions++

		// rollup
		st := res.PerSymbol[sym]
		st.Symbol = sym
		st.Trades++
		st.Gross += gross
		st.Net += net
		if net >= 0 {
			st.Wins++
		} else {
			st.Losses++
		}
		if st.Trades == 1 {
			st.Best, st.Worst = net, net
		} else {
			if net > st.Best {
				st.Best = net
			}
			if net < st.Worst {
				st.Worst = net
			}
		}
		st.AvgPNL = st.Net / float64(st.Trades)
		if st.Trades > 0 {
			st.WinRate = 100 * float64(st.Wins) / float64(st.Trades)
		}
		res.PerSymbol[sym] = st
	}

	res.EquityN = equity
	fillMetrics(&res)
	buildDailyEQ(&res)
	return res, nil
}

// ===== metrics & helpers (unchanged) =====

func exposureApprox(trs []Trade, start, end time.Time) float64 {
	if len(trs) == 0 {
		return 0
	}
	// crude: number of trades divided by total minutes (assume avg 15min per trade)
	const avgMin = 15.0
	total := end.Sub(start).Minutes()
	if total <= 0 {
		return 0
	}
	return float64(len(trs)) * avgMin / total
}

func profitFactor(trs []Trade) float64 {
	var gain, loss float64
	for _, t := range trs {
		if t.NetPnL >= 0 {
			gain += t.NetPnL
		} else {
			loss += -t.NetPnL
		}
	}
	if loss == 0 {
		if gain > 0 {
			return 999
		}
		return 0
	}
	return gain / loss
}

func tfMinutes(tf types.TF) float64 {
	switch tf {
	case types.TF1m:
		return 1
	case types.TF5m:
		return 5
	case types.TF15m:
		return 15
	case types.TF1h:
		return 60
	case types.TF4h:
		return 240
	default:
		return 5
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func tern[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
