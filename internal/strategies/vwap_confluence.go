package strategies

import "go-machine/internal/features"

type VWAPConfluenceStrategy struct{}

func (s VWAPConfluenceStrategy) Name() string { return "vwap_confluence" }

func (s VWAPConfluenceStrategy) Eval(ctx Context) Signal {
	c := ctx.Candles
	if len(c) < 15 {
		return Signal{Name: s.Name()}
	}
	v := rollingVWAP(c, minInt(40, len(c)))
	if v <= 0 {
		return Signal{Name: s.Name()}
	}
	last := c[len(c)-1]
	prev := c[len(c)-2]
	side := features.SideLong
	reason := "above_vwap_bias"
	if last.C < v {
		side = features.SideShort
		reason = "below_vwap_bias"
	}
	flip := false
	if prev.C < v && last.C > v {
		flip = true
		side = features.SideLong
		reason = "vwap_reclaim"
	}
	if prev.C > v && last.C < v {
		flip = true
		side = features.SideShort
		reason = "vwap_loss"
	}
	stop := v * (1 - 0.003)
	if side == features.SideShort {
		stop = v * (1 + 0.003)
	}
	tp1, tp2 := rrTargets(last.C, stop, side)
	if tp1 <= 0 {
		return Signal{Name: s.Name()}
	}
	conf := 0.55
	if flip {
		conf += 0.10
	}
	if side == features.SideLong && ctx.Snapshot.Flow.WhaleDelta1m > 0 {
		conf += 0.05
	}
	if side == features.SideShort && ctx.Snapshot.Flow.WhaleDelta1m < 0 {
		conf += 0.05
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
		Tags:         []string{"ipa", "vwap", "confluence"},
		Invalidation: "vwap role flip fails",
		Reasons:      []string{reason},
		Confluence: map[string]float64{
			"vwap": 0.66,
		},
		SignalSource: []string{"institutional_pa"},
		Ts:           last.Ts,
	}
}

func rollingVWAP(c []features.Candle, n int) float64 {
	if len(c) == 0 {
		return 0
	}
	if n <= 0 || n > len(c) {
		n = len(c)
	}
	start := len(c) - n
	num := 0.0
	den := 0.0
	for i := start; i < len(c); i++ {
		tp := (c[i].H + c[i].L + c[i].C) / 3.0
		num += tp * c[i].V
		den += c[i].V
	}
	if den <= 0 {
		return c[len(c)-1].C
	}
	return num / den
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
