package features

import "time"

type Config struct {
	Structure StructureConfig
	Liquidity LiquidityConfig
	FVG       FVGConfig
	OB        OBConfig
	Flow      FlowConfig
	Anchor    AnchorConfig
}

type Engine struct {
	st *StructureEngine
	lq *LiquidityEngine
	fg *FVGEngine
	ob *OBEngine
	fl *FlowEngine
	an *AnchorEngine
}

func NewEngine(cfg Config) *Engine {
	if cfg.Anchor.TZ == nil {
		cfg.Anchor.TZ = time.UTC
	}
	return &Engine{
		st: NewStructureEngine(cfg.Structure),
		lq: NewLiquidityEngine(cfg.Liquidity),
		fg: NewFVGEngine(cfg.FVG),
		ob: NewOBEngine(cfg.OB),
		fl: NewFlowEngine(cfg.Flow),
		an: NewAnchorEngine(cfg.Anchor),
	}
}

func (e *Engine) AddWhales(ws []WhaleEvent, now time.Time) { e.fl.AddWhales(ws, now) }
func (e *Engine) AddTape(ts []TapeEvent, now time.Time)    { e.fl.AddTape(ts, now) }

func (e *Engine) Eval(c []Candle) Snapshot {
	if len(c) == 0 {
		return Snapshot{}
	}
	st := e.st.Eval(c)
	pools, sweep := e.lq.Eval(c)
	fvgs, _ := e.fg.Eval(c)
	obs := e.ob.Eval(c, st)
	flow := e.fl.Eval(c)
	anc := e.an.Eval(c)
	return Snapshot{
		Candle:    c[len(c)-1],
		Structure: st,
		Pools:     pools,
		Sweep:     sweep,
		FVGs:      fvgs,
		OBs:       obs,
		Flow:      flow,
		Anchors:   anc,
	}
}
