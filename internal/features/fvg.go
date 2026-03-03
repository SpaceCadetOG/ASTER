package features

type FVGConfig struct {
	MaxAge int
}

type FVGEngine struct {
	cfg   FVGConfig
	zones []FVGZone
}

func NewFVGEngine(cfg FVGConfig) *FVGEngine {
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 200
	}
	return &FVGEngine{cfg: cfg}
}

func (e *FVGEngine) Eval(c []Candle) ([]FVGZone, []FVGZone) {
	if len(c) < 3 {
		return nil, nil
	}
	newZones := make([]FVGZone, 0, 2)
	i := len(c) - 1
	c1, c2, c3 := c[i-2], c[i-1], c[i]
	_ = c2
	if c1.H < c3.L {
		z := FVGZone{Side: SideLong, Low: c1.H, High: c3.L, CreatedTs: c3.Ts, Active: true}
		e.zones = append(e.zones, z)
		newZones = append(newZones, z)
	}
	if c1.L > c3.H {
		z := FVGZone{Side: SideShort, Low: c3.H, High: c1.L, CreatedTs: c3.Ts, Active: true}
		e.zones = append(e.zones, z)
		newZones = append(newZones, z)
	}
	last := c[i]
	for j := range e.zones {
		if !e.zones[j].Active {
			continue
		}
		e.zones[j].AgeBars++
		if e.zones[j].AgeBars > e.cfg.MaxAge {
			e.zones[j].Active = false
			continue
		}
		if last.L <= e.zones[j].High && last.H >= e.zones[j].Low {
			e.zones[j].Mitigated = true
		}
	}
	active := make([]FVGZone, 0, len(e.zones))
	for _, z := range e.zones {
		if z.Active {
			active = append(active, z)
		}
	}
	return active, newZones
}
