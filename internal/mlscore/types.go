package mlscore

type ScoreRequest struct {
	CandidateID string             `json:"candidate_id"`
	Symbol      string             `json:"symbol"`
	Side        string             `json:"side"`
	Features    map[string]float64 `json:"features"`
	Categories  map[string]string  `json:"categories"`
}

type ScoreResponse struct {
	Enabled                     bool     `json:"enabled"`
	ModelVersion                string   `json:"model_version"`
	TakeTradeProbability        float64  `json:"take_trade_probability"`
	ExpectedR                   float64  `json:"expected_r"`
	ExpectedMaxR                float64  `json:"expected_max_r"`
	StopoutThenReclaimProb      float64  `json:"stopout_then_reclaim_probability"`
	ReentryAfterStopProbability float64  `json:"reentry_after_stop_probability"`
	SuggestedStopProfile        string   `json:"suggested_stop_profile"`
	SuggestedExitProfile        string   `json:"suggested_exit_profile"`
	Reasons                     []string `json:"reasons"`
}

type Scorer interface {
	Score(req ScoreRequest) ScoreResponse
}
