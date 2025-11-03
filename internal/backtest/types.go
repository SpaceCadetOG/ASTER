// go-machine/internal/backtest/types.go
package backtest

import "time"

type Config struct {
	InitialEquity float64
	MaxConcurrent int
	PerTradeFrac  float64
	FeeBps        float64
	SlipBps       float64

	// Strategy selection & tuning
	Strategy    string  // "fib" or "ema9"
	RRMin       float64 // e.g. 2.0 for 2:1 minimum
	EMAShort    int     // default 9
	EMALong     int     // default 20
	VolLookback int     // e.g. 20
	VWAPConfirm bool    // require VWAP reclaim/below
	OnePerSwing bool    // only 1 trade per recent swing
	BiasTF      string  // "15m" by default
	AllowShorts bool    // enable short entries (default true)
	EntryTf     string  // "1m" for ema triggers
}

type Result struct {
	Start     time.Time
	End       time.Time
	TF        string
	Equity0   float64
	EquityN   float64
	Trades    []Trade
	DailyEQ   []DailyPoint
	PerSymbol map[string]SymbolStat
	Metrics   Metrics
}

type Trade struct {
	Symbol    string
	Side      string // "long" or "short"
	EntryTime time.Time
	ExitTime  time.Time
	Entry     float64
	Exit      float64
	Qty       float64
	GrossPnL  float64
	Fees      float64
	NetPnL    float64
	ReturnPct float64
}

type DailyPoint struct {
	Day    time.Time
	Equity float64
}

type SymbolStat struct {
	Symbol  string
	Trades  int
	Gross   float64
	Net     float64
	Wins    int
	Losses  int
	Best    float64
	Worst   float64
	AvgPNL  float64
	WinRate float64
}

type Metrics struct {
	NetPNL       float64
	WinRate      float64
	AvgTrade     float64
	ProfitFactor float64
	MaxDD        float64
	Sharpe       float64
	Exposure     float64 // fraction of time in market
	NumTrades    int
}
