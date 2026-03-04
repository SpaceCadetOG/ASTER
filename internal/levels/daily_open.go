package levels

import (
	"time"

	"go-machine/internal/features"
)

type DailyOpenPoint struct {
	DayKey string
	Ts     time.Time
	Open   float64
}

func BuildDailyOpenSeries(candles []features.Candle, anchorHour int, loc *time.Location) []DailyOpenPoint {
	if len(candles) == 0 {
		return nil
	}
	if loc == nil {
		loc = time.UTC
	}
	if anchorHour < 0 || anchorHour > 23 {
		anchorHour = 0
	}
	out := make([]DailyOpenPoint, 0, len(candles)/24)
	seen := map[string]struct{}{}
	for _, c := range candles {
		lt := c.Ts.In(loc)
		day := anchorDayKey(lt, anchorHour)
		if _, ok := seen[day]; ok {
			continue
		}
		anchor := anchorTime(lt, anchorHour)
		if lt.Before(anchor) {
			anchor = anchor.AddDate(0, 0, -1)
		}
		out = append(out, DailyOpenPoint{DayKey: day, Ts: anchor, Open: c.O})
		seen[day] = struct{}{}
	}
	return out
}

func OpenForAnchorDay(candles []features.Candle, ts time.Time, anchorHour int, loc *time.Location) (float64, bool) {
	if len(candles) == 0 {
		return 0, false
	}
	if loc == nil {
		loc = time.UTC
	}
	want := anchorDayKey(ts.In(loc), anchorHour)
	for i := 0; i < len(candles); i++ {
		lt := candles[i].Ts.In(loc)
		if anchorDayKey(lt, anchorHour) == want {
			return candles[i].O, true
		}
	}
	return 0, false
}

func anchorDayKey(t time.Time, anchorHour int) string {
	at := anchorTime(t, anchorHour)
	if t.Before(at) {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

func anchorTime(t time.Time, anchorHour int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), anchorHour, 0, 0, 0, t.Location())
}
