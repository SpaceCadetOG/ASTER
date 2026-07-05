package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"go-machine/internal/paperreport"
)

func main() {
	var (
		closed  = flag.String("closed-trades-jsonl", "", "Path to paper closed trades JSONL")
		trades  = flag.String("trades-csv", "", "Unused legacy option; prefer closed JSONL")
		outDir  = flag.String("out-dir", "", "Optional reports output directory")
		csvOut  = flag.String("csv-out", "", "Optional consolidated CSV output path")
		jsonOut = flag.String("json-out", "", "Optional consolidated JSON output path")
	)
	flag.Parse()
	_ = trades
	if strings.TrimSpace(*closed) == "" {
		fail("set -closed-trades-jsonl")
	}
	records, err := paperreport.LoadClosedTradesJSONL(strings.TrimSpace(*closed))
	if err != nil {
		fail("load closed trades: %v", err)
	}
	outputs := paperreport.BuildOutputs(records)
	dir := strings.TrimSpace(*outDir)
	if dir == "" {
		dir = paperreport.DefaultReportDir(strings.TrimSpace(*closed))
	}
	if err := paperreport.WriteCompatFiles(dir, outputs, strings.TrimSpace(*csvOut), strings.TrimSpace(*jsonOut)); err != nil {
		fail("write reports: %v", err)
	}
	fmt.Fprintf(os.Stdout, "paperreport wrote %s (%d trades)\n", dir, outputs.Summary.TradeCount)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
