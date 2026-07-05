package mlscore

import "strings"

func Load(path string, enabled bool) (Scorer, error) {
	if !enabled || strings.TrimSpace(path) == "" {
		return NoopScorer{}, nil
	}
	return LoadLinearModel(path)
}
