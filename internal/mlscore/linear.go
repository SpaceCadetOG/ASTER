package mlscore

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

type LinearModel struct {
	SchemaVersion       string                        `json:"schema_version,omitempty"`
	ModelID             string                        `json:"model_id,omitempty"`
	ModelType           string                        `json:"model_type"`
	ModelVersion        string                        `json:"model_version"`
	Target              string                        `json:"target,omitempty"`
	CreatedAt           time.Time                     `json:"created_at,omitempty"`
	FeatureOrder        []string                      `json:"feature_order,omitempty"`
	CategoricalFeatures map[string]map[string]float64 `json:"categorical_features,omitempty"`
	Normalization       map[string]NormStat           `json:"normalization,omitempty"`
	Bias                float64                       `json:"bias"`
	Intercept           float64                       `json:"intercept,omitempty"`
	Weights             map[string]float64            `json:"weights"`
	CategoryBiases      map[string]map[string]float64 `json:"category_biases,omitempty"`
	Outputs             LinearOutputs                 `json:"outputs"`
}

type NormStat struct {
	Mean float64 `json:"mean"`
	Std  float64 `json:"std"`
}

type LinearOutputs struct {
	ProbabilityScale  float64            `json:"probability_scale"`
	ExpectedRScale    float64            `json:"expected_r_scale"`
	ExpectedMaxRScale float64            `json:"expected_max_r_scale"`
	ReclaimScale      float64            `json:"reclaim_scale"`
	ReentryScale      float64            `json:"reentry_scale"`
	StopProfiles      []ThresholdProfile `json:"stop_profiles,omitempty"`
	ExitProfiles      []ThresholdProfile `json:"exit_profiles,omitempty"`
}

type ThresholdProfile struct {
	MinScore float64 `json:"min_score"`
	Label    string  `json:"label"`
}

type LinearScorer struct {
	model LinearModel
}

func LoadLinearModel(path string) (*LinearScorer, error) {
	b, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	var model LinearModel
	if err := json.Unmarshal(b, &model); err != nil {
		return nil, err
	}
	if strings.TrimSpace(model.ModelVersion) == "" {
		model.ModelVersion = "linear.v1"
	}
	if strings.TrimSpace(model.ModelID) == "" {
		model.ModelID = model.ModelVersion
	}
	if strings.TrimSpace(model.ModelType) == "" {
		model.ModelType = "linear"
	}
	if model.Intercept != 0 && model.Bias == 0 {
		model.Bias = model.Intercept
	}
	if len(model.CategoryBiases) == 0 && len(model.CategoricalFeatures) > 0 {
		model.CategoryBiases = model.CategoricalFeatures
	}
	if err := ValidateLinearModel(model); err != nil {
		return nil, fmt.Errorf("validate linear model: %w", err)
	}
	return &LinearScorer{model: model}, nil
}

func ValidateLinearModel(model LinearModel) error {
	if strings.TrimSpace(model.ModelType) != "" &&
		!strings.EqualFold(strings.TrimSpace(model.ModelType), "linear") &&
		!strings.EqualFold(strings.TrimSpace(model.ModelType), "logistic") {
		return fmt.Errorf("unsupported model_type %q", model.ModelType)
	}
	if model.Weights == nil {
		return errors.New("weights must be set")
	}
	for name := range model.Weights {
		if strings.TrimSpace(name) == "" {
			return errors.New("weights contains blank feature name")
		}
	}
	for name, mapping := range model.CategoryBiases {
		if strings.TrimSpace(name) == "" {
			return errors.New("category_biases contains blank category name")
		}
		for value := range mapping {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("category_biases[%s] contains blank label", name)
			}
		}
	}
	if err := validateProfiles("stop_profiles", model.Outputs.StopProfiles); err != nil {
		return err
	}
	if err := validateProfiles("exit_profiles", model.Outputs.ExitProfiles); err != nil {
		return err
	}
	return nil
}

func (s *LinearScorer) Model() LinearModel {
	return s.model
}

func (s *LinearScorer) Score(req ScoreRequest) ScoreResponse {
	raw := s.model.Bias
	for name, value := range req.Features {
		if stat, ok := s.model.Normalization[name]; ok && stat.Std != 0 {
			value = (value - stat.Mean) / stat.Std
		}
		raw += value * s.model.Weights[name]
	}
	for name, cat := range req.Categories {
		raw += s.model.CategoryBiases[name][cat]
	}
	probScale := fallbackScale(s.model.Outputs.ProbabilityScale, 1.0)
	expScale := fallbackScale(s.model.Outputs.ExpectedRScale, 1.0)
	maxScale := fallbackScale(s.model.Outputs.ExpectedMaxRScale, 1.0)
	reclaimScale := fallbackScale(s.model.Outputs.ReclaimScale, 1.0)
	reentryScale := fallbackScale(s.model.Outputs.ReentryScale, 1.0)
	takeProb := sigmoid(raw / probScale)
	expectedR := raw / expScale
	expectedMaxR := math.Max(0, raw/maxScale)
	reclaimProb := sigmoid(-raw / reclaimScale)
	reentryProb := sigmoid((raw - 0.15) / reentryScale)
	return ScoreResponse{
		Enabled:                     true,
		ModelVersion:                s.model.ModelVersion,
		TakeTradeProbability:        takeProb,
		ExpectedR:                   expectedR,
		ExpectedMaxR:                expectedMaxR,
		StopoutThenReclaimProb:      reclaimProb,
		ReentryAfterStopProbability: reentryProb,
		SuggestedStopProfile:        selectProfile(expectedR, s.model.Outputs.StopProfiles, "default"),
		SuggestedExitProfile:        selectProfile(expectedMaxR, s.model.Outputs.ExitProfiles, "default"),
		Reasons:                     []string{firstNonEmptyModelKind(s.model.ModelType, "linear_model")},
	}
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

func selectProfile(score float64, profiles []ThresholdProfile, fallback string) string {
	if len(profiles) == 0 {
		return fallback
	}
	cp := append([]ThresholdProfile(nil), profiles...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].MinScore > cp[j].MinScore })
	for _, profile := range cp {
		if score >= profile.MinScore && strings.TrimSpace(profile.Label) != "" {
			return profile.Label
		}
	}
	return fallback
}

func fallbackScale(v, fallback float64) float64 {
	if v == 0 {
		return fallback
	}
	return v
}

func validateProfiles(name string, profiles []ThresholdProfile) error {
	for i, profile := range profiles {
		if strings.TrimSpace(profile.Label) == "" {
			return fmt.Errorf("%s[%d] has blank label", name, i)
		}
	}
	return nil
}

func firstNonEmptyModelKind(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
