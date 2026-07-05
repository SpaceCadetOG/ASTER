package mlscore

type NoopScorer struct{}

func (NoopScorer) Score(ScoreRequest) ScoreResponse {
	return ScoreResponse{
		Enabled: false,
		Reasons: []string{"ml_disabled"},
	}
}
