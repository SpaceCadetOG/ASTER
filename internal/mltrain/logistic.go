package mltrain

import (
	"math"
	"sort"
)

type LogisticConfig struct {
	Epochs int
	LR     float64
	L2     float64
}

type LogisticModel struct {
	Intercept      float64
	Weights        map[string]float64
	CategoryBiases map[string]map[string]float64
}

func TrainLogistic(rows []Row, numericFeatures, categoricalFeatures []string, cfg LogisticConfig) LogisticModel {
	if cfg.Epochs <= 0 {
		cfg.Epochs = 500
	}
	if cfg.LR <= 0 {
		cfg.LR = 0.05
	}
	weights := make(map[string]float64, len(numericFeatures))
	categoryBiases := make(map[string]map[string]float64, len(categoricalFeatures))
	for _, name := range categoricalFeatures {
		categoryBiases[name] = collectCategories(rows, name)
	}
	model := LogisticModel{
		Weights:        weights,
		CategoryBiases: categoryBiases,
	}
	for epoch := 0; epoch < cfg.Epochs; epoch++ {
		gradIntercept := 0.0
		gradWeights := make(map[string]float64, len(numericFeatures))
		gradCats := make(map[string]map[string]float64, len(categoricalFeatures))
		for _, name := range categoricalFeatures {
			gradCats[name] = make(map[string]float64, len(categoryBiases[name]))
		}
		for _, row := range rows {
			score := model.Intercept
			for _, name := range numericFeatures {
				score += row.Numeric[name] * model.Weights[name]
			}
			for _, name := range categoricalFeatures {
				score += model.CategoryBiases[name][row.Categorical[name]]
			}
			pred := sigmoid(score)
			err := pred - row.Target
			gradIntercept += err
			for _, name := range numericFeatures {
				gradWeights[name] += err * row.Numeric[name]
			}
			for _, name := range categoricalFeatures {
				gradCats[name][row.Categorical[name]] += err
			}
		}
		n := float64(len(rows))
		model.Intercept -= cfg.LR * (gradIntercept / n)
		for _, name := range numericFeatures {
			model.Weights[name] -= cfg.LR * ((gradWeights[name] / n) + cfg.L2*model.Weights[name])
		}
		for _, name := range categoricalFeatures {
			for cat := range model.CategoryBiases[name] {
				model.CategoryBiases[name][cat] -= cfg.LR * ((gradCats[name][cat] / n) + cfg.L2*model.CategoryBiases[name][cat])
			}
		}
	}
	return model
}

func PredictProbability(model LogisticModel, row Row, numericFeatures, categoricalFeatures []string) float64 {
	score := model.Intercept
	for _, name := range numericFeatures {
		score += row.Numeric[name] * model.Weights[name]
	}
	for _, name := range categoricalFeatures {
		score += model.CategoryBiases[name][row.Categorical[name]]
	}
	return sigmoid(score)
}

func sigmoid(x float64) float64 {
	if x > 30 {
		return 1
	}
	if x < -30 {
		return 0
	}
	return 1 / (1 + math.Exp(-x))
}

func collectCategories(rows []Row, feature string) map[string]float64 {
	seen := map[string]struct{}{}
	for _, row := range rows {
		seen[row.Categorical[feature]] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]float64, len(keys))
	for _, key := range keys {
		out[key] = 0
	}
	return out
}
