package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-machine/internal/mlschema"
	"go-machine/internal/paperreport"
)

func main() {
	closedTrades := flag.String("closed-trades-jsonl", "", "Path to ASTER closed trade JSONL")
	outPath := flag.String("out", "", "Output CSV path")
	flag.Parse()

	if strings.TrimSpace(*closedTrades) == "" {
		fail("set -closed-trades-jsonl")
	}
	if strings.TrimSpace(*outPath) == "" {
		fail("set -out")
	}

	records, err := paperreport.LoadClosedTradesJSONL(strings.TrimSpace(*closedTrades))
	if err != nil {
		fail("load closed trades: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(strings.TrimSpace(*outPath)), 0o755); err != nil {
		fail("mkdir out dir: %v", err)
	}
	f, err := os.Create(strings.TrimSpace(*outPath))
	if err != nil {
		fail("create output: %v", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(mlschema.FeatureCSVHeader()); err != nil {
		fail("write header: %v", err)
	}
	for _, rec := range records {
		row := mlschema.BuildFeatureRow(rec)
		if err := w.Write(row.CSVRecord()); err != nil {
			fail("write row: %v", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fail("flush csv: %v", err)
	}
	fmt.Fprintf(os.Stdout, "mlfeaturebuild wrote %s (%d rows)\n", strings.TrimSpace(*outPath), len(records))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
