package mlscore

import "testing"

func TestLinearScorerScore(t *testing.T) {
	scorer := &LinearScorer{
		model: LinearModel{
			ModelVersion: "test-v1",
			Bias:         0.2,
			Weights: map[string]float64{
				"combined_score": 2.0,
				"volume_ratio":   0.5,
			},
			CategoryBiases: map[string]map[string]float64{
				"side": {"BUY": 0.1},
			},
			Outputs: LinearOutputs{
				ProbabilityScale:  1,
				ExpectedRScale:    2,
				ExpectedMaxRScale: 1,
				ReclaimScale:      1,
				ReentryScale:      1,
				StopProfiles: []ThresholdProfile{
					{MinScore: 0.6, Label: "wide"},
				},
				ExitProfiles: []ThresholdProfile{
					{MinScore: 1.0, Label: "runner"},
				},
			},
		},
	}
	resp := scorer.Score(ScoreRequest{
		Features: map[string]float64{
			"combined_score": 0.8,
			"volume_ratio":   1.2,
		},
		Categories: map[string]string{
			"side": "BUY",
		},
	})
	if !resp.Enabled {
		t.Fatalf("expected enabled response")
	}
	if resp.ModelVersion != "test-v1" {
		t.Fatalf("unexpected model version %q", resp.ModelVersion)
	}
	if resp.TakeTradeProbability <= 0.5 {
		t.Fatalf("expected take prob > 0.5, got %.4f", resp.TakeTradeProbability)
	}
	if resp.SuggestedExitProfile != "runner" {
		t.Fatalf("expected runner exit profile, got %q", resp.SuggestedExitProfile)
	}
}
