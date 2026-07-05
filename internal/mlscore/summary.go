package mlscore

type ModelSummary struct {
	ModelType     string   `json:"model_type"`
	ModelVersion  string   `json:"model_version"`
	FeatureCount  int      `json:"feature_count"`
	CategoryCount int      `json:"category_count"`
	FeatureNames  []string `json:"feature_names,omitempty"`
	CategoryNames []string `json:"category_names,omitempty"`
}

func SummaryFromLinearModel(model LinearModel) ModelSummary {
	featureNames := append([]string(nil), model.FeatureOrder...)
	if len(featureNames) == 0 {
		featureNames = make([]string, 0, len(model.Weights))
		for name := range model.Weights {
			featureNames = append(featureNames, name)
		}
	}
	categoryMap := model.CategoryBiases
	if len(categoryMap) == 0 {
		categoryMap = model.CategoricalFeatures
	}
	categoryNames := make([]string, 0, len(categoryMap))
	for name := range categoryMap {
		categoryNames = append(categoryNames, name)
	}
	return ModelSummary{
		ModelType:     model.ModelType,
		ModelVersion:  model.ModelVersion,
		FeatureCount:  len(featureNames),
		CategoryCount: len(categoryNames),
		FeatureNames:  featureNames,
		CategoryNames: categoryNames,
	}
}
