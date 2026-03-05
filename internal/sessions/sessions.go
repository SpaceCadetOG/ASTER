package sessions

import (
	"time"

	"go-machine/internal/data"
)

func ActiveSessionLabels(nowUTC time.Time) []string {
	regime := data.CurrentRegimeCT(nowUTC)
	labels := []string{string(regime)}
	if data.IsMajorOverlapCT(nowUTC) {
		labels = append(labels, "MAJOR_OVERLAP")
	}
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.FixedZone("CST", -6*3600)
	}
	lt := nowUTC.In(loc)
	if lt.Hour() >= 7 && lt.Hour() < 10 {
		labels = append(labels, "US_OPEN_CTX")
	}
	if lt.Hour() == 16 {
		labels = append(labels, "NY17_ROLLOVER")
	}
	return labels
}

func ScannerScoreMultiplier(nowUTC time.Time) float64 {
	_ = nowUTC
	return 1.0
}
