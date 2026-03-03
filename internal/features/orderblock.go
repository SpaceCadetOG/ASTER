package features

type OBConfig struct {
	DisplacementPct float64
	MaxAge          int
}

type OBEngine struct {
	cfg  OBConfig
	obs  []OBZone
	last StructureState
}

func NewOBEngine(cfg OBConfig) *OBEngine {
	if cfg.DisplacementPct <= 0 {
		cfg.DisplacementPct = 0.35
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 200
	}
	return &OBEngine{cfg: cfg}
}

func (e *OBEngine) Eval(c []Candle, st StructureState) []OBZone {
	if len(c) < 2 {
		return nil
	}
	last := c[len(c)-1]
	prev := c[len(c)-2]
	body := abs(last.C-prev.C) / max(1e-9, prev.C) * 100
	newBOS := !st.LastBOSTs.IsZero() && !st.LastBOSTs.Equal(e.last.LastBOSTs)
	if newBOS && body >= e.cfg.DisplacementPct {
		ob := findLastOppCandle(c[:len(c)-1], st.Trend)
		if ob != nil {
			e.obs = append(e.obs, OBZone{
				Side:      trendToSide(st.Trend),
				Low:       min(ob.O, ob.C),
				High:      max(ob.O, ob.C),
				CreatedTs: last.Ts,
				Active:    true,
			})
		}
	}
	for i := range e.obs {
		if !e.obs[i].Active {
			continue
		}
		if last.L <= e.obs[i].High && last.H >= e.obs[i].Low {
			e.obs[i].Touched = true
			if e.obs[i].Side == SideLong && last.C > last.O {
				e.obs[i].Rejected = true
			}
			if e.obs[i].Side == SideShort && last.C < last.O {
				e.obs[i].Rejected = true
			}
		}
	}
	e.last = st
	out := make([]OBZone, 0, len(e.obs))
	for _, z := range e.obs {
		if z.Active {
			out = append(out, z)
		}
	}
	return out
}

func findLastOppCandle(c []Candle, tr TrendState) *Candle {
	for i := len(c) - 1; i >= 0; i-- {
		if tr == TrendBull && c[i].C < c[i].O {
			cp := c[i]
			return &cp
		}
		if tr == TrendBear && c[i].C > c[i].O {
			cp := c[i]
			return &cp
		}
	}
	return nil
}

func trendToSide(t TrendState) Side {
	if t == TrendBear {
		return SideShort
	}
	return SideLong
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
