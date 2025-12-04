// go-machine/cmd/backtest/main.go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/backtest"
	"go-machine/internal/types"
)

func main() {
	var (
		symbolsCSV string
		tfStr      string
		fromStr    string
		toStr      string
		outCSV     string
		strategy   string
		allowShort bool
	)

	flag.StringVar(&symbolsCSV, "symbols", "BTCUSDT,ETHUSDT", "comma-separated symbols")
	flag.StringVar(&tfStr, "tf", "5m", "timeframe (1m,5m,15m,1h,4h)")
	flag.StringVar(&fromStr, "from", "", "start date (YYYY-MM-DD)")
	flag.StringVar(&toStr, "to", "", "end date (YYYY-MM-DD)")
	flag.StringVar(&outCSV, "csv", "", "optional: write trades to CSV")
	flag.StringVar(&strategy, "strategy", "fib", "strategy: fib | ema9")
	flag.BoolVar(&allowShort, "shorts", true, "enable short entries")
	flag.Parse()

	if fromStr == "" || toStr == "" {
		fmt.Fprintln(os.Stderr, "error: -from and -to are required (YYYY-MM-DD)")
		os.Exit(1)
	}

	tf, ok := types.ParseTF(tfStr)
	if !ok {
		fmt.Fprintf(os.Stderr, "bad -tf %q\n", tfStr)
		os.Exit(1)
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad -from: %v\n", err)
		os.Exit(1)
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bad -to: %v\n", err)
		os.Exit(1)
	}
	if !to.After(from) {
		fmt.Fprintln(os.Stderr, "error: -to must be after -from")
		os.Exit(1)
	}

	symbols := strings.Split(symbolsCSV, ",")
	for i := range symbols {
		symbols[i] = strings.TrimSpace(symbols[i])
	}

	client := aster.New("")
	cfg := backtest.Config{
		InitialEquity: 10000,
		MaxConcurrent: 2,
		PerTradeFrac:  0.5,
		FeeBps:        4,
		SlipBps:       5,

		Strategy:    strategy, // "fib" or "ema9"
		RRMin:       2.0,
		EMAShort:    9,
		EMALong:     20,
		VolLookback: 20,
		VWAPConfirm: true,
		OnePerSwing: true,
		BiasTF:      "15m",
		AllowShorts: allowShort,
		EntryTf:     "1m",
	}

	fmt.Printf("Backtest %s → %s  TF=%s  Symbols=%v\n",
		from.Format("2006-01-02"), to.Format("2006-01-02"), tf, symbols)

	res, runErr := backtest.Run(client, symbols, tf, from, to, cfg)
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "backtest error: %v\n", runErr)
		os.Exit(1)
	}

	fmt.Println("Backtest completed successfully.")

	if outCSV != "" {
		if err := backtest.WriteCSV(outCSV, res.Trades); err != nil {
			fmt.Println("CSV write error:", err)
		} else {
			fmt.Println("Wrote trades to:", outCSV)
		}
	}

	b, jerr := json.MarshalIndent(res, "", "  ")
	if jerr == nil {
		fmt.Println("\n=== Backtest Result (debug JSON) ===")
		fmt.Println(string(b))
	} else {
		log.Println("json:", jerr)
	}
}