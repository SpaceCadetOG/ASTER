package strategies

import (
	"time"

	"go-machine/internal/features"
	"go-machine/internal/levels"
)

type DailyOpenSR struct{}

func (s DailyOpenSR) Name() string { return "daily_open_sr" }

func (s DailyOpenSR) Eval(ctx Context) Signal {
	if len(ctx.Candles) < 20 {
		return Signal{Name: s.Name()}
	}
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		loc = time.UTC
	}
	last := ctx.Candles[len(ctx.Candles)-1]
	dOpen, ok := levels.OpenForAnchorDay(ctx.Candles, last.Ts, 7, loc)
	if !ok || dOpen <= 0 {
		return Signal{Name: s.Name()}
	}
	accN := 2
	if len(ctx.Candles) < accN+2 {
		return Signal{Name: s.Name()}
	}
	above := 0
	below := 0
	for i := len(ctx.Candles) - 1 - accN; i < len(ctx.Candles)-1; i++ {
		if ctx.Candles[i].C > dOpen {
			above++
		}
		if ctx.Candles[i].C < dOpen {
			below++
		}
	}
	tol := last.C * 0.0008
	if tol <= 0 {
		tol = 1e-8
	}
	if absf(last.C-dOpen) > tol {
		return Signal{Name: s.Name()}
	}
	side := features.SideLong
	if above < accN && below < accN {
		return Signal{Name: s.Name()}
	}
	if below >= accN {
		side = features.SideShort
	}
	stop := dOpen * (1 - 0.0035)
	if side == features.SideShort {
		stop = dOpen * (1 + 0.0035)
	}
	tp1, tp2 := rrTargets(last.C, stop, side)
	if tp1 <= 0 {
		return Signal{Name: s.Name()}
	}
	conf := 0.58
	if side == features.SideLong && ctx.Snapshot.Flow.WhaleDelta1m > 0 {
		conf += 0.08
	}
	if side == features.SideShort && ctx.Snapshot.Flow.WhaleDelta1m < 0 {
		conf += 0.08
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
		Tags:         []string{"ipa", "daily_open", "retest"},
		Invalidation: "daily open rejection failed",
		Reasons:      []string{"daily_open_acceptance", "daily_open_retest"},
		Confluence: map[string]float64{
			"daily_open": 0.65,
		},
		SignalSource: []string{"institutional_pa"},
		Ts:           last.Ts,
	}
}
