package mltrain

import "sort"

type ClassificationMetrics struct {
	Accuracy  float64
	Precision float64
	Recall    float64
	AUCApprox float64
}

func EvaluateClassification(targets, probs []float64, threshold float64) ClassificationMetrics {
	if threshold <= 0 {
		threshold = 0.5
	}
	var tp, tn, fp, fn float64
	for i, target := range targets {
		pred := 0.0
		if probs[i] >= threshold {
			pred = 1
		}
		switch {
		case pred == 1 && target == 1:
			tp++
		case pred == 0 && target == 0:
			tn++
		case pred == 1 && target == 0:
			fp++
		case pred == 0 && target == 1:
			fn++
		}
	}
	total := tp + tn + fp + fn
	out := ClassificationMetrics{}
	if total > 0 {
		out.Accuracy = (tp + tn) / total
	}
	if tp+fp > 0 {
		out.Precision = tp / (tp + fp)
	}
	if tp+fn > 0 {
		out.Recall = tp / (tp + fn)
	}
	out.AUCApprox = approxAUC(targets, probs)
	return out
}

func approxAUC(targets, probs []float64) float64 {
	type pair struct {
		prob   float64
		target float64
	}
	pairs := make([]pair, 0, len(targets))
	pos, neg := 0.0, 0.0
	for i := range targets {
		pairs = append(pairs, pair{prob: probs[i], target: targets[i]})
		if targets[i] >= 0.5 {
			pos++
		} else {
			neg++
		}
	}
	if pos == 0 || neg == 0 {
		return 0
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].prob < pairs[j].prob })
	rankSum := 0.0
	for i, p := range pairs {
		if p.target >= 0.5 {
			rankSum += float64(i + 1)
		}
	}
	return (rankSum - (pos*(pos+1))/2) / (pos * neg)
}
