package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"time"

	"go-machine/internal/stats"
)

func main() {
	logPath := flag.String("log", "logs/events.jsonl", "path to JSONL event log")
	fromS := flag.String("from", "", "start date/time (RFC3339 or YYYY-MM-DD)")
	toS := flag.String("to", "", "end date/time (RFC3339 or YYYY-MM-DD)")
	csvOut := flag.String("csv", "", "optional csv output path")
	flag.Parse()

	from, err := parseTime(*fromS, false)
	if err != nil {
		fmt.Println("invalid -from:", err)
		os.Exit(2)
	}
	to, err := parseTime(*toS, true)
	if err != nil {
		fmt.Println("invalid -to:", err)
		os.Exit(2)
	}
	es, err := stats.LoadEvents(*logPath, from, to)
	if err != nil {
		fmt.Println("load events:", err)
		os.Exit(1)
	}
	r := stats.Aggregate(es)
	fmt.Println(stats.FormatReport(r))
	fmt.Println("by strategy:")
	for _, x := range r.ByStrategy {
		fmt.Printf("- %s trades=%d winRate=%.2f%% pnl=%.2f avg=%.2f\n", x.Name, x.Trades, x.WinRate, x.PnL, x.AvgPnL)
	}
	fmt.Println("by symbol:")
	for _, x := range r.BySymbol {
		fmt.Printf("- %s trades=%d winRate=%.2f%% pnl=%.2f avg=%.2f\n", x.Name, x.Trades, x.WinRate, x.PnL, x.AvgPnL)
	}
	if *csvOut != "" {
		if err := writeCSV(*csvOut, r); err != nil {
			fmt.Println("write csv:", err)
			os.Exit(1)
		}
	}
}

func parseTime(v string, endOfDay bool) (*time.Time, error) {
	v = stringsTrim(v)
	if v == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t, nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil, err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Nanosecond)
	}
	return &t, nil
}

func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		c := s[len(s)-1]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

func writeCSV(path string, r stats.Report) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"scope", "name", "trades", "wins", "losses", "win_rate", "pnl", "avg_pnl"})
	for _, x := range r.ByStrategy {
		_ = w.Write([]string{"strategy", x.Name, fmt.Sprint(x.Trades), fmt.Sprint(x.Wins), fmt.Sprint(x.Losses), fmt.Sprintf("%.4f", x.WinRate), fmt.Sprintf("%.8f", x.PnL), fmt.Sprintf("%.8f", x.AvgPnL)})
	}
	for _, x := range r.BySymbol {
		_ = w.Write([]string{"symbol", x.Name, fmt.Sprint(x.Trades), fmt.Sprint(x.Wins), fmt.Sprint(x.Losses), fmt.Sprintf("%.4f", x.WinRate), fmt.Sprintf("%.8f", x.PnL), fmt.Sprintf("%.8f", x.AvgPnL)})
	}
	return w.Error()
}
