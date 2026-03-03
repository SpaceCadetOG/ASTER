package features

import "time"

type AnchorConfig struct {
	TZ *time.Location
}

type AnchorEngine struct {
	cfg AnchorConfig
}

func NewAnchorEngine(cfg AnchorConfig) *AnchorEngine {
	if cfg.TZ == nil {
		cfg.TZ = time.UTC
	}
	return &AnchorEngine{cfg: cfg}
}

func (e *AnchorEngine) Eval(c []Candle) AnchorLevels {
	if len(c) == 0 {
		return AnchorLevels{SessionOpen: map[string]float64{}}
	}
	loc := e.cfg.TZ
	if loc == nil {
		loc = time.UTC
	}
	last := c[len(c)-1].Ts.In(loc)
	al := AnchorLevels{SessionOpen: map[string]float64{}, Session: sessionName(last)}
	day := dateKey(last)
	weekY, weekW := last.ISOWeek()
	for i := len(c) - 1; i >= 0; i-- {
		t := c[i].Ts.In(loc)
		if dateKey(t) == day {
			al.DailyOpen = c[i].O
		}
		y, w := t.ISOWeek()
		if y == weekY && w == weekW {
			if al.PWH == 0 || c[i].H > al.PWH {
				al.PWH = c[i].H
			}
			if al.PWL == 0 || c[i].L < al.PWL {
				al.PWL = c[i].L
			}
		}
		s := sessionName(t)
		if _, ok := al.SessionOpen[s]; !ok {
			al.SessionOpen[s] = c[i].O
		}
	}
	// prior day high/low
	prevDay := dayAdd(last, -1)
	for i := len(c) - 1; i >= 0; i-- {
		t := c[i].Ts.In(loc)
		if dateKey(t) != prevDay {
			continue
		}
		if al.PDH == 0 || c[i].H > al.PDH {
			al.PDH = c[i].H
		}
		if al.PDL == 0 || c[i].L < al.PDL {
			al.PDL = c[i].L
		}
	}
	return al
}

func sessionName(t time.Time) string {
	h := t.Hour()
	switch {
	case h >= 0 && h < 8:
		return "asia"
	case h >= 8 && h < 13:
		return "london"
	default:
		return "ny"
	}
}

func dateKey(t time.Time) string { return t.Format("2006-01-02") }

func dayAdd(t time.Time, n int) string { return t.AddDate(0, 0, n).Format("2006-01-02") }
