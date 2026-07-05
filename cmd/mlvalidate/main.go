package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"go-machine/internal/mlscore"
)

func main() {
	modelPath := flag.String("model", "", "Path to ASTER runtime model JSON")
	featuresJSON := flag.String("features", "", "Optional inline JSON map of numeric features")
	categoriesJSON := flag.String("categories", "", "Optional inline JSON map of categorical features")
	flag.Parse()

	if strings.TrimSpace(*modelPath) == "" {
		fail("set -model")
	}
	scorer, err := mlscore.LoadLinearModel(strings.TrimSpace(*modelPath))
	if err != nil {
		fail("load model: %v", err)
	}
	summary := mlscore.SummaryFromLinearModel(scorer.Model())
	sort.Strings(summary.FeatureNames)
	sort.Strings(summary.CategoryNames)
	b, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(b))

	if strings.TrimSpace(*featuresJSON) == "" && strings.TrimSpace(*categoriesJSON) == "" {
		return
	}
	features := map[string]float64{}
	categories := map[string]string{}
	if strings.TrimSpace(*featuresJSON) != "" {
		if err := json.Unmarshal([]byte(*featuresJSON), &features); err != nil {
			fail("decode features json: %v", err)
		}
	}
	if strings.TrimSpace(*categoriesJSON) != "" {
		if err := json.Unmarshal([]byte(*categoriesJSON), &categories); err != nil {
			fail("decode categories json: %v", err)
		}
	}
	resp := scorer.Score(mlscore.ScoreRequest{
		CandidateID: "manual",
		Symbol:      "MANUAL",
		Side:        categories["side"],
		Features:    features,
		Categories:  categories,
	})
	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
