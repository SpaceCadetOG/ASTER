package mltrain

import "testing"

func TestTrainLogisticSeparatesSimpleSignal(t *testing.T) {
	rows := []Row{
		{Numeric: map[string]float64{"combined_score": -1}, Categorical: map[string]string{"side": "BUY"}, Target: 0},
		{Numeric: map[string]float64{"combined_score": -0.5}, Categorical: map[string]string{"side": "BUY"}, Target: 0},
		{Numeric: map[string]float64{"combined_score": 0.5}, Categorical: map[string]string{"side": "BUY"}, Target: 1},
		{Numeric: map[string]float64{"combined_score": 1}, Categorical: map[string]string{"side": "BUY"}, Target: 1},
	}
	model := TrainLogistic(rows, []string{"combined_score"}, []string{"side"}, LogisticConfig{Epochs: 400, LR: 0.1, L2: 0.001})
	low := PredictProbability(model, rows[0], []string{"combined_score"}, []string{"side"})
	high := PredictProbability(model, rows[3], []string{"combined_score"}, []string{"side"})
	if high <= low {
		t.Fatalf("expected higher probability for positive row, got low=%.4f high=%.4f", low, high)
	}
}
