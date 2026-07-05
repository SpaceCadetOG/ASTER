package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go-machine/internal/mlscore"
	"go-machine/internal/mltrain"
)

func main() {
	input := flag.String("input", "", "Path to trade_features.csv")
	target := flag.String("target", "win", "Training target")
	outPath := flag.String("out", "", "Output model path")
	modelID := flag.String("model-id", "take_trade_v1_local", "Model id/version")
	epochs := flag.Int("epochs", 500, "Training epochs")
	lr := flag.Float64("lr", 0.05, "Learning rate")
	l2 := flag.Float64("l2", 0.001, "L2 regularization")
	flag.Parse()

	if *input == "" || *outPath == "" {
		fail("set -input and -out")
	}
	if *target != "win" {
		fail("only -target win is supported in v1")
	}

	ds, loadedRows, err := mltrain.LoadCSV(*input, *target, mltrain.DefaultNumericFeatures, mltrain.DefaultCategoricalFeatures)
	if err != nil {
		fail("load csv: %v", err)
	}
	trainRows, testRows := mltrain.TimeSplit(ds.Rows, 0.80)
	stats := mltrain.ComputeNormStats(trainRows, ds.NumericFeatures)
	trainNorm := mltrain.NormalizeRows(trainRows, stats)
	testNorm := mltrain.NormalizeRows(testRows, stats)
	model := mltrain.TrainLogistic(trainNorm, ds.NumericFeatures, ds.CategoricalFeatures, mltrain.LogisticConfig{
		Epochs: *epochs,
		LR:     *lr,
		L2:     *l2,
	})
	targets := make([]float64, 0, len(testNorm))
	probs := make([]float64, 0, len(testNorm))
	for _, row := range testNorm {
		targets = append(targets, row.Target)
		probs = append(probs, mltrain.PredictProbability(model, row, ds.NumericFeatures, ds.CategoricalFeatures))
	}
	metrics := mltrain.EvaluateClassification(targets, probs, 0.5)
	export := buildLinearExport(*modelID, *target, ds, stats, model)
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fail("mkdir out dir: %v", err)
	}
	b, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		fail("marshal model: %v", err)
	}
	if err := os.WriteFile(*outPath, b, 0o644); err != nil {
		fail("write model: %v", err)
	}

	fmt.Printf("loaded rows: %d\n", loadedRows)
	fmt.Printf("usable rows: %d\n", len(ds.Rows))
	fmt.Printf("features: %d\n", len(ds.NumericFeatures)+len(ds.CategoricalFeatures))
	fmt.Printf("train rows: %d\n", len(trainRows))
	fmt.Printf("test rows: %d\n", len(testRows))
	fmt.Printf("accuracy: %.2f\n", metrics.Accuracy)
	fmt.Printf("precision: %.2f\n", metrics.Precision)
	fmt.Printf("recall: %.2f\n", metrics.Recall)
	fmt.Printf("auc_approx: %.2f\n", metrics.AUCApprox)
	fmt.Printf("wrote model: %s\n", *outPath)
}

func buildLinearExport(modelID, target string, ds mltrain.Dataset, stats map[string]mltrain.NormStat, model mltrain.LogisticModel) mlscore.LinearModel {
	normalization := make(map[string]mlscore.NormStat, len(stats))
	for k, v := range stats {
		normalization[k] = mlscore.NormStat{Mean: v.Mean, Std: v.Std}
	}
	return mlscore.LinearModel{
		SchemaVersion:       "aster.ml.linear.v1",
		ModelID:             modelID,
		ModelType:           "logistic",
		ModelVersion:        modelID,
		Target:              target,
		CreatedAt:           time.Now().UTC(),
		FeatureOrder:        append([]string(nil), ds.NumericFeatures...),
		CategoricalFeatures: model.CategoryBiases,
		Normalization:       normalization,
		Bias:                model.Intercept,
		Weights:             model.Weights,
		CategoryBiases:      model.CategoryBiases,
		Outputs: mlscore.LinearOutputs{
			ProbabilityScale:  1.0,
			ExpectedRScale:    3.0,
			ExpectedMaxRScale: 2.0,
			ReclaimScale:      1.0,
			ReentryScale:      1.0,
			StopProfiles: []mlscore.ThresholdProfile{
				{MinScore: 0.75, Label: "wide"},
				{MinScore: 0.35, Label: "normal"},
				{MinScore: -999, Label: "tight"},
			},
			ExitProfiles: []mlscore.ThresholdProfile{
				{MinScore: 1.50, Label: "runner"},
				{MinScore: 0.75, Label: "balanced"},
				{MinScore: -999, Label: "fast"},
			},
		},
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
