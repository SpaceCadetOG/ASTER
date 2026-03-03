package features

import "time"

type StructureConfig struct {
	Left  int
	Right int
}

type StructureEngine struct {
	cfg    StructureConfig
	pivots []Pivot
}

func NewStructureEngine(cfg StructureConfig) *StructureEngine {
	if cfg.Left <= 0 {
		cfg.Left = 2
	}
	if cfg.Right <= 0 {
		cfg.Right = 2
	}
	return &StructureEngine{cfg: cfg}
}

func (e *StructureEngine) Eval(c []Candle) StructureState {
	if len(c) == 0 {
		return StructureState{Trend: TrendRange}
	}
	e.pivots = detectPivotsCausal(c, e.cfg.Left, e.cfg.Right)
	st := StructureState{Trend: TrendRange}
	if len(e.pivots) == 0 {
		return st
	}
	for i := len(e.pivots) - 1; i >= 0; i-- {
		p := e.pivots[i]
		if p.High && st.LastSwingHigh == nil {
			cp := p
			st.LastSwingHigh = &cp
		}
		if !p.High && st.LastSwingLow == nil {
			cp := p
			st.LastSwingLow = &cp
		}
		if st.LastSwingHigh != nil && st.LastSwingLow != nil {
			break
		}
	}
	if len(e.pivots) >= 4 {
		hhhl, lhll := 0, 0
		for i := 1; i < len(e.pivots); i++ {
			a, b := e.pivots[i-1], e.pivots[i]
			if a.High && b.High {
				if b.Price > a.Price {
					hhhl++
				} else {
					lhll++
				}
			}
			if !a.High && !b.High {
				if b.Price > a.Price {
					hhhl++
				} else {
					lhll++
				}
			}
		}
		if hhhl > lhll {
			st.Trend = TrendBull
		} else if lhll > hhhl {
			st.Trend = TrendBear
		}
	}
	last := c[len(c)-1]
	if st.LastSwingHigh != nil && last.C > st.LastSwingHigh.Price {
		if st.Trend == TrendBull || st.Trend == TrendRange {
			st.LastBOSTs = last.Ts
		} else {
			st.LastCHOCHTs = last.Ts
			st.Trend = TrendBull
		}
	}
	if st.LastSwingLow != nil && last.C < st.LastSwingLow.Price {
		if st.Trend == TrendBear || st.Trend == TrendRange {
			st.LastBOSTs = last.Ts
		} else {
			st.LastCHOCHTs = last.Ts
			st.Trend = TrendBear
		}
	}
	st.Label = classifyLastLeg(e.pivots)
	return st
}

func classifyLastLeg(p []Pivot) string {
	if len(p) < 3 {
		return ""
	}
	last := p[len(p)-1]
	for i := len(p) - 2; i >= 0; i-- {
		if p[i].High == last.High {
			if last.High {
				if last.Price > p[i].Price {
					return "HH"
				}
				return "LH"
			}
			if last.Price > p[i].Price {
				return "HL"
			}
			return "LL"
		}
	}
	return ""
}

func detectPivotsCausal(c []Candle, left, right int) []Pivot {
	if len(c) < left+right+1 {
		return nil
	}
	out := make([]Pivot, 0, len(c)/4)
	for i := left; i+right < len(c); i++ {
		// pivot at i is only known after right bars have closed -> causal at i+right
		h, l := c[i].H, c[i].L
		isHigh, isLow := true, true
		for j := i - left; j <= i+right; j++ {
			if j == i {
				continue
			}
			if c[j].H >= h {
				isHigh = false
			}
			if c[j].L <= l {
				isLow = false
			}
			if !isHigh && !isLow {
				break
			}
		}
		if isHigh {
			out = append(out, Pivot{Index: i, Ts: c[i+right].Ts, Price: h, High: true})
		}
		if isLow {
			out = append(out, Pivot{Index: i, Ts: c[i+right].Ts, Price: l, High: false})
		}
	}
	return out
}

func recent(ts time.Time, n int, tf time.Duration) time.Time {
	if n <= 0 || tf <= 0 {
		return ts
	}
	return ts.Add(-time.Duration(n) * tf)
}
