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
		closed  = flag.String("closed", "", "Path to paper closed trades JSONL")
		csvOut  = flag.String("csv-out", "", "Optional consolidated CSV output path")
		jsonOut = flag.String("json-out", "", "Optional consolidated JSON output path")
		outDir  = flag.String("out-dir", "", "Optional reports output directory")
	)
	flag.Parse()
	if strings.TrimSpace(*closed) == "" {
		fail("set --closed")
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
	fmt.Fprintf(os.Stdout, "paper-report wrote %s (%d trades)\n", dir, outputs.Summary.TradeCount)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
