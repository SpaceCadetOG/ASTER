package mltrain

func TimeSplit(rows []Row, trainRatio float64) (train []Row, test []Row) {
	if trainRatio <= 0 || trainRatio >= 1 {
		trainRatio = 0.80
	}
	if len(rows) == 0 {
		return nil, nil
	}
	cut := int(float64(len(rows)) * trainRatio)
	if cut < 1 {
		cut = 1
	}
	if cut >= len(rows) {
		cut = len(rows) - 1
	}
	if cut < 1 {
		return append([]Row(nil), rows...), nil
	}
	return append([]Row(nil), rows[:cut]...), append([]Row(nil), rows[cut:]...)
}
