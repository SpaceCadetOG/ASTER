package features

import (
	"time"

	"go-machine/internal/flow"
)

type WhaleEvent struct {
	Ts    time.Time
	USD   float64
	IsBuy bool
}

type TapeEvent struct {
	Ts    time.Time
	USD   float64
	IsBuy bool
}

type FlowConfig struct {
	VolWin  int
	LargeUS float64
	DeltaN  int
}

type FlowEngine struct {
	cfg       FlowConfig
	volumes   []float64
	whaleWin  *flow.Window
	tapeWin   *flow.Window
	deltaCumQ []float64
}

func NewFlowEngine(cfg FlowConfig) *FlowEngine {
	if cfg.VolWin <= 0 {
		cfg.VolWin = 30
	}
	if cfg.LargeUS <= 0 {
		cfg.LargeUS = 5000
	}
	if cfg.DeltaN <= 0 {
		cfg.DeltaN = 20
	}
	return &FlowEngine{
		cfg:      cfg,
		whaleWin: flow.NewWindow(time.Minute, cfg.LargeUS),
		tapeWin:  flow.NewWindow(time.Minute, cfg.LargeUS),
	}
}

func (e *FlowEngine) AddWhales(ws []WhaleEvent, now time.Time) {
	for _, w := range ws {
		e.whaleWin.Add(flow.Event{Ts: w.Ts, USD: w.USD, IsBuy: w.IsBuy})
	}
	_ = e.whaleWin.SnapshotAt(now)
}

func (e *FlowEngine) AddTape(ts []TapeEvent, now time.Time) {
	for _, t := range ts {
		e.tapeWin.Add(flow.Event{Ts: t.Ts, USD: t.USD, IsBuy: t.IsBuy})
	}
	_ = e.tapeWin.SnapshotAt(now)
}

func (e *FlowEngine) Eval(c []Candle) FlowState {
	if len(c) == 0 {
		return FlowState{}
	}
	last := c[len(c)-1]
	e.volumes = append(e.volumes, last.V)
	if len(e.volumes) > e.cfg.VolWin*4 {
		e.volumes = e.volumes[len(e.volumes)-e.cfg.VolWin*4:]
	}
	vz := zscore(e.volumes, last.V, e.cfg.VolWin)
	ws := e.whaleWin.SnapshotAt(last.Ts)
	ts := e.tapeWin.SnapshotAt(last.Ts)
	e.deltaCumQ = append(e.deltaCumQ, ws.DeltaUSD)
	if len(e.deltaCumQ) > e.cfg.DeltaN {
		e.deltaCumQ = e.deltaCumQ[len(e.deltaCumQ)-e.cfg.DeltaN:]
	}
	cum := 0.0
	for _, d := range e.deltaCumQ {
		cum += d
	}
	return FlowState{
		VolumeZ:           vz,
		VolumeSpike:       vz >= 2.0,
		WhaleDelta1m:      ws.DeltaUSD,
		WhaleDeltaCum:     cum,
		WhaleBuyPct:       ws.BuyPct,
		WhaleSellPct:      ws.SellPct,
		LargeTradeCount1m: ts.LargeCount,
		LargeBuyCount1m:   int(ts.BuyPct / 100.0 * float64(ts.LargeCount)),
		LargeSellCount1m:  int(ts.SellPct / 100.0 * float64(ts.LargeCount)),
	}
}

func zscore(series []float64, x float64, n int) float64 {
	if len(series) < 2 {
		return 0
	}
	if n > len(series) {
		n = len(series)
	}
	s := series[len(series)-n:]
	mu := 0.0
	for _, v := range s {
		mu += v
	}
	mu /= float64(len(s))
	var ss float64
	for _, v := range s {
		d := v - mu
		ss += d * d
	}
	if len(s) < 2 {
		return 0
	}
	std := ss / float64(len(s)-1)
	if std <= 1e-12 {
		return 0
	}
	return (x - mu) / sqrt(std)
}

func sqrt(x float64) float64 {
	z := x
	if z <= 0 {
		return 0
	}
	for i := 0; i < 6; i++ {
		z = 0.5 * (z + x/z)
	}
	return z
}
