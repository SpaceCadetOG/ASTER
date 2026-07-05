package mltrain

import "math"

type NormStat struct {
	Mean float64 `json:"mean"`
	Std  float64 `json:"std"`
}

func ComputeNormStats(rows []Row, features []string) map[string]NormStat {
	out := make(map[string]NormStat, len(features))
	if len(rows) == 0 {
		return out
	}
	for _, name := range features {
		sum := 0.0
		for _, row := range rows {
			sum += row.Numeric[name]
		}
		mean := sum / float64(len(rows))
		variance := 0.0
		for _, row := range rows {
			d := row.Numeric[name] - mean
			variance += d * d
		}
		std := math.Sqrt(variance / float64(len(rows)))
		if std == 0 {
			std = 1
		}
		out[name] = NormStat{Mean: mean, Std: std}
	}
	return out
}

func NormalizeRows(rows []Row, stats map[string]NormStat) []Row {
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		cp := Row{
			TradeID:     row.TradeID,
			EntryTs:     row.EntryTs,
			Numeric:     make(map[string]float64, len(row.Numeric)),
			Categorical: make(map[string]string, len(row.Categorical)),
			Target:      row.Target,
		}
		for k, v := range row.Numeric {
			stat := stats[k]
			cp.Numeric[k] = (v - stat.Mean) / stat.Std
		}
		for k, v := range row.Categorical {
			cp.Categorical[k] = v
		}
		out = append(out, cp)
	}
	return out
}
