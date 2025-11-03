package backtest

// import (
// 	"fmt"
// 	"io"
// )

// func PrintReport(w io.Writer, r Result) {
// 	m := r.Metrics
// 	fmt.Fprintf(w, "Backtest %s → %s  TF=%s  Symbols=%v\n",
// 		r.From.UTC().Format("2006-01-02"),
// 		r.To.UTC().Format("2006-01-02"),
// 		r.TF, r.Symbols,
// 	)
// 	fmt.Fprintf(w, "Trades=%d  Wins=%d  Losses=%d  WinRate=%.1f%%\n",
// 		m.Trades, m.Wins, m.Losses, m.WinRatePct,
// 	)
// 	fmt.Fprintf(w, "NetPnL=%.2f  AvgTrade=%.2f%%  Best=%.2f%%  Worst=%.2f%%\n",
// 		m.NetPnL, m.AvgTradePct, m.BestTradePct, m.WorstTradePct,
// 	)
// 	fmt.Fprintf(w, "MaxDD=%.2f%%  Sharpe=%.2f  Sortino=%.2f  EquityEnd=%.2f\n",
// 		m.MaxDDPct, m.Sharpe, m.Sortino, m.EquityEnd,
// 	)
// }
