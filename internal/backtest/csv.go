// go-machine/internal/backtest/csv.go
package backtest

import (
	"encoding/csv"
	"os"
	"strconv"
)

func WriteCSV(trades []Trade, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	_ = w.Write([]string{
		"symbol", "side", "entry_time", "exit_time", "entry", "exit", "qty",
		"gross_pnl", "fees", "net_pnl", "return_pct",
	})
	for _, t := range trades {
		_ = w.Write([]string{
			t.Symbol, t.Side, t.EntryTime.Format("2006-01-02T15:04:05Z07:00"),
			t.ExitTime.Format("2006-01-02T15:04:05Z07:00"),
			formatF(t.Entry), formatF(t.Exit), formatF(t.Qty),
			formatF(t.GrossPnL), formatF(t.Fees), formatF(t.NetPnL), formatF(t.ReturnPct),
		})
	}
	return nil
}

func formatF(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
