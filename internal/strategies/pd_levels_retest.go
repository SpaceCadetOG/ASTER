package strategies

import (
	"time"

	"go-machine/internal/features"
	"go-machine/internal/levels"
)

type PDLevelsRetest struct{}

func (s PDLevelsRetest) Name() string { return "pd_levels_retest" }

func (s PDLevelsRetest) Eval(ctx Context) Signal {
	c := ctx.Candles
	if len(c) < 40 {
		return Signal{Name: s.Name()}
	}
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.UTC
	}
	idx := len(c) - 1
	pl, ok := levels.PrevLevelsAt(c, idx, 16, loc)
	if !ok {
		return Signal{Name: s.Name()}
	}
	last := c[idx]
	accN := 2

	check := func(level float64, long bool) bool {
		if level <= 0 || len(c) < accN+3 {
			return false
		}
		start := idx - (accN + 1)
		if start < 0 {
			return false
		}
		breach := false
		for i := start; i < idx-accN; i++ {
			if long && c[i].H > level {
				breach = true
			}
			if !long && c[i].L < level {
				breach = true
			}
		}
		if !breach {
			return false
		}
		accept := true
		for i := idx - accN; i < idx; i++ {
			if long && c[i].C <= level {
				accept = false
			}
			if !long && c[i].C >= level {
				accept = false
			}
		}
		if !accept {
			return false
		}
		tol := last.C * 0.0010
		if tol <= 0 {
			tol = 1e-8
		}
		return absf(last.C-level) <= tol
	}

	side := features.SideLong
	triggerLevel := 0.0
	reason := ""
	switch {
	case check(pl.PDH, true):
		side = features.SideLong
		triggerLevel = pl.PDH
		reason = "pdh_breach_accept_retest"
	case check(pl.PDL, false):
		side = features.SideShort
		triggerLevel = pl.PDL
		reason = "pdl_breach_accept_retest"
	case check(pl.PWH, true):
		side = features.SideLong
		triggerLevel = pl.PWH
		reason = "pwh_breach_accept_retest"
	case check(pl.PWL, false):
		side = features.SideShort
		triggerLevel = pl.PWL
		reason = "pwl_breach_accept_retest"
	default:
		return Signal{Name: s.Name()}
	}
	stop := triggerLevel * (1 - 0.004)
	if side == features.SideShort {
		stop = triggerLevel * (1 + 0.004)
	}
	tp1, tp2 := rrTargets(last.C, stop, side)
	if tp1 <= 0 {
		return Signal{Name: s.Name()}
	}
	conf := 0.62
	sw := levels.DetectStrongWeakSwings(c, 2, 0.10)
	if len(sw) > 0 {
		latest := sw[len(sw)-1]
		if latest.Strong {
			conf += 0.05
		} else {
			conf -= 0.05
		}
	}
	if side == features.SideLong && last.C > ctx.Snapshot.Anchors.DailyOpen {
		conf += 0.06
	}
	if side == features.SideShort && last.C < ctx.Snapshot.Anchors.DailyOpen {
		conf += 0.06
	}
	return Signal{
		Active:       true,
		Name:         s.Name(),
		Side:         side,
		Entry:        last.C,
		Stop:         stop,
		TP1:          tp1,
		TP2:          tp2,
		Confidence:   clamp01(conf),
		Tags:         []string{"ipa", "pd_levels", "acceptance", "retest"},
		Invalidation: "level acceptance lost",
		Reasons:      []string{reason},
		Confluence: map[string]float64{
			"pd_levels": 0.70,
		},
		SignalSource: []string{"institutional_pa"},
		Ts:           last.Ts,
	}
}
