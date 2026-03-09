package market

type ExecutionResult struct {
	SpreadBps      float64
	TopBookUSD     float64
	EstSlippageBps float64
	Penalty        float64
	Flags          []string
}

func evalExecution(m Market, cfg RankConfig) ExecutionResult {
	r := ExecutionResult{
		SpreadBps:      ptrVal(m.SpreadBps),
		TopBookUSD:     ptrVal(m.TopBookUSD),
		EstSlippageBps: ptrVal(m.EstSlippageBps),
		Flags:          make([]string, 0, 6),
	}
	if !cfg.EnableExecPenalty {
		return r
	}

	spreadPen := 0.0
	if m.SpreadBps == nil {
		spreadPen = cfg.ExecMaxPenalty * 0.35
		r.Flags = append(r.Flags, "spread_missing")
	} else {
		sb := *m.SpreadBps
		if sb > cfg.SpreadBpsSoft {
			den := cfg.SpreadBpsHard - cfg.SpreadBpsSoft
			if den <= 0 {
				den = 1
			}
			spreadPen = clamp((sb-cfg.SpreadBpsSoft)/den, 0, 1) * (cfg.ExecMaxPenalty * 0.45)
			if sb > cfg.SpreadBpsHard {
				r.Flags = append(r.Flags, "spread_hard")
			}
		}
	}

	depthPen := 0.0
	if m.TopBookUSD == nil || *m.TopBookUSD <= 0 {
		depthPen = cfg.ExecMaxPenalty * 0.30
		r.Flags = append(r.Flags, "topbook_missing")
	} else {
		ratio := clamp(*m.TopBookUSD/max(cfg.TargetClipUSD, 1), 0, 10)
		minDepth := max(cfg.MinTopBookUSD, cfg.TargetClipUSD)
		if *m.TopBookUSD < minDepth || ratio < 1 {
			depthPen = (1 - clamp(*m.TopBookUSD/minDepth, 0, 1)) * (cfg.ExecMaxPenalty * 0.35)
			r.Flags = append(r.Flags, "topbook_thin")
		}
	}

	slipPen := 0.0
	if m.EstSlippageBps == nil {
		slipPen = cfg.ExecMaxPenalty * 0.20
		r.Flags = append(r.Flags, "slippage_missing")
	} else if cfg.MaxEstSlipBps > 0 {
		slipPen = clamp(*m.EstSlippageBps/cfg.MaxEstSlipBps, 0, 1) * (cfg.ExecMaxPenalty * 0.30)
		if *m.EstSlippageBps > cfg.MaxEstSlipBps {
			r.Flags = append(r.Flags, "slippage_high")
		}
	}

	r.Penalty = clamp(spreadPen+depthPen+slipPen, 0, cfg.ExecMaxPenalty)
	return r
}

func ptrVal(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
