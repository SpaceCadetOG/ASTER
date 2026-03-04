package levels

import (
	"time"

	"go-machine/internal/features"
)

type PrevLevels struct {
	PDH float64
	PDL float64
	PWH float64
	PWL float64
}

func PrevLevelsAt(candles []features.Candle, idx int, dayAnchorHour int, loc *time.Location) (PrevLevels, bool) {
	if idx < 0 || idx >= len(candles) || len(candles) < 2 {
		return PrevLevels{}, false
	}
	if loc == nil {
		loc = time.UTC
	}
	cur := candles[idx].Ts.In(loc)
	curDay := anchorDayKey(cur, dayAnchorHour)
	prevDay := anchorDay(cur, dayAnchorHour).AddDate(0, 0, -1).Format("2006-01-02")
	_, curW := anchorDay(cur, dayAnchorHour).ISOWeek()
	prevWeekAnchor := anchorDay(cur, dayAnchorHour).AddDate(0, 0, -7)
	prevWY, prevWW := prevWeekAnchor.ISOWeek()

	pl := PrevLevels{}
	dayFound := false
	weekFound := false
	for i := 0; i <= idx; i++ {
		t := candles[i].Ts.In(loc)
		dk := anchorDayKey(t, dayAnchorHour)
		if dk == curDay {
			continue
		}
		if dk == prevDay {
			if !dayFound || candles[i].H > pl.PDH {
				pl.PDH = candles[i].H
			}
			if !dayFound || candles[i].L < pl.PDL {
				pl.PDL = candles[i].L
			}
			dayFound = true
		}
		ad := anchorDay(t, dayAnchorHour)
		yw, ww := ad.ISOWeek()
		if yw == prevWY && ww == prevWW {
			if !weekFound || candles[i].H > pl.PWH {
				pl.PWH = candles[i].H
			}
			if !weekFound || candles[i].L < pl.PWL {
				pl.PWL = candles[i].L
			}
			weekFound = true
		}
		_ = curW
	}
	if !dayFound && !weekFound {
		return PrevLevels{}, false
	}
	return pl, true
}

func anchorDay(t time.Time, anchorHour int) time.Time {
	a := time.Date(t.Year(), t.Month(), t.Day(), anchorHour, 0, 0, 0, t.Location())
	if t.Before(a) {
		return a.AddDate(0, 0, -1)
	}
	return a
}
