package strategies

import (
	"go-machine/internal/features"
	"go-machine/internal/levels"
)

type FailedAuctionMagnetStrategy struct{}

func (s FailedAuctionMagnetStrategy) Name() string { return "failed_auction_magnet" }

func (s FailedAuctionMagnetStrategy) Eval(ctx Context) Signal {
	if len(ctx.Candles) < 12 {
		return Signal{Name: s.Name()}
	}
	m, ok := levels.DetectFailedAuctionMagnet(ctx.Candles, 30, 0.10)
	if !ok || m.Level <= 0 {
		return Signal{Name: s.Name()}
	}
	last := ctx.Candles[len(ctx.Candles)-1]
	side := features.SideLong
	if m.Level < last.C {
		side = features.SideShort
	}
	stop := last.C * (1 - 0.004)
	if side == features.SideShort {
		stop = last.C * (1 + 0.004)
	}
	tp1, tp2 := rrTargets(last.C, stop, side)
	if tp1 <= 0 {
		return Signal{Name: s.Name()}
	}
	// Pull TP1 toward the magnet if it's closer than default RR target.
	if side == features.SideLong && m.Level > last.C && m.Level < tp1 {
		tp1 = m.Level * 0.999
	}
	if side == features.SideShort && m.Level < last.C && m.Level > tp1 {
		tp1 = m.Level * 1.001
	}
	conf := 0.56
	if m.Active {
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
		Tags:         []string{"ipa", "failed_auction", "magnet"},
		Invalidation: "magnet fixed or invalidated",
		Reasons:      []string{"failed_auction_magnet_pull"},
		Confluence: map[string]float64{
			"failed_auction": 0.62,
		},
		SignalSource: []string{"institutional_pa"},
		Ts:           last.Ts,
	}
}
