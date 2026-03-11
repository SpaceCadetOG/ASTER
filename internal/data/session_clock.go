package data

import (
	"time"

	"go-machine/internal/features"
)

const chicagoTZ = "America/Chicago"

type Regime string

const (
	RegimeAsia  Regime = "ASIA"
	RegimeEU    Regime = "EU"
	RegimeUS    Regime = "US"
	RegimeDead  Regime = "DEAD"
	OverlapAE   Regime = "ASIA_EU_OVERLAP"
	OverlapEUUS Regime = "EU_US_OVERLAP"
)

func mustChicago() *time.Location {
	loc, err := time.LoadLocation(chicagoTZ)
	if err != nil {
		return time.FixedZone("CST", -6*3600)
	}
	return loc
}

// DayKeyNY17CT returns a day key where a trading day rolls at 16:00 Chicago time (NY17 anchor).
func DayKeyNY17CT(ts time.Time) string {
	loc := mustChicago()
	t := ts.In(loc)
	anchor := time.Date(t.Year(), t.Month(), t.Day(), 16, 0, 0, 0, loc)
	if t.Before(anchor) {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

// USMorningKeyCT returns the local day key anchored by US morning context (07:00 CT).
func USMorningKeyCT(ts time.Time) string {
	loc := mustChicago()
	t := ts.In(loc)
	anchor := time.Date(t.Year(), t.Month(), t.Day(), 7, 0, 0, 0, loc)
	if t.Before(anchor) {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

func CurrentRegimeCT(ts time.Time) Regime {
	loc := mustChicago()
	t := ts.In(loc)
	cur := t.Hour()*60 + t.Minute()

	// Overlaps take priority.
	if inMinuteRange(cur, 2*60, 3*60) {
		return OverlapAE
	}
	if inMinuteRange(cur, 7*60, 10*60) {
		return OverlapEUUS
	}
	if inMinuteWrap(cur, 19*60, 2*60) {
		return RegimeAsia
	}
	if inMinuteRange(cur, 2*60, 10*60) {
		return RegimeEU
	}
	if inMinuteRange(cur, 7*60, 16*60+30) {
		return RegimeUS
	}
	return RegimeDead
}

func IsMajorOverlapCT(ts time.Time) bool {
	r := CurrentRegimeCT(ts)
	return r == OverlapAE || r == OverlapEUUS
}

func SessionRiskMultiplier(ts time.Time, confidence float64) float64 {
	r := CurrentRegimeCT(ts)
	switch r {
	case OverlapAE, OverlapEUUS:
		return 1.0
	case RegimeEU, RegimeUS, RegimeAsia:
		return 0.8
	default:
		if confidence >= 0.75 {
			return 0.65
		}
		return 0.45
	}
}

type DailyBar struct {
	DayKey string
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
	Start  time.Time
	End    time.Time
}

func BuildDailyBars(candles []features.Candle) []DailyBar {
	if len(candles) == 0 {
		return nil
	}
	loc := mustChicago()
	byKey := make(map[string]*DailyBar, len(candles)/200+1)
	order := make([]string, 0, len(candles)/200+1)
	for _, c := range candles {
		lt := c.Ts.In(loc)
		key := DayKeyNY17CT(lt)
		db, ok := byKey[key]
		if !ok {
			db = &DailyBar{
				DayKey: key, Open: c.O, High: c.H, Low: c.L, Close: c.C, Volume: c.V, Start: c.Ts, End: c.Ts,
			}
			byKey[key] = db
			order = append(order, key)
			continue
		}
		if c.H > db.High {
			db.High = c.H
		}
		if c.L < db.Low {
			db.Low = c.L
		}
		db.Close = c.C
		db.Volume += c.V
		if c.Ts.Before(db.Start) {
			db.Start = c.Ts
		}
		if c.Ts.After(db.End) {
			db.End = c.Ts
		}
	}
	out := make([]DailyBar, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out
}

func PrevDayHighLow(ts time.Time, candles []features.Candle) (float64, float64, bool) {
	daily := BuildDailyBars(candles)
	if len(daily) < 2 {
		return 0, 0, false
	}
	key := DayKeyNY17CT(ts)
	for i := 1; i < len(daily); i++ {
		if daily[i].DayKey == key {
			return daily[i-1].High, daily[i-1].Low, true
		}
	}
	return 0, 0, false
}

func PrevWeekHighLow(ts time.Time, candles []features.Candle) (float64, float64, bool) {
	if len(candles) == 0 {
		return 0, 0, false
	}
	loc := mustChicago()
	cur := ts.In(loc)
	_, curWeek := cur.ISOWeek()
	curYear, _ := cur.ISOWeek()
	prevRef := cur.AddDate(0, 0, -7)
	prevYear, prevWeek := prevRef.ISOWeek()
	_ = curWeek
	h := 0.0
	l := 0.0
	found := false
	for _, c := range candles {
		lt := c.Ts.In(loc)
		y, w := lt.ISOWeek()
		if y == curYear && w == curWeek {
			continue
		}
		if y == prevYear && w == prevWeek {
			if !found || c.H > h {
				h = c.H
			}
			if !found || c.L < l {
				l = c.L
			}
			found = true
		}
	}
	if !found {
		return 0, 0, false
	}
	return h, l, true
}

func inMinuteRange(cur, start, end int) bool {
	return cur >= start && cur < end
}

func inMinuteWrap(cur, start, end int) bool {
	if start < end {
		return inMinuteRange(cur, start, end)
	}
	return cur >= start || cur < end
}
