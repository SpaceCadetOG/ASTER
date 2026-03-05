package risk

import "math"

type Config struct {
	Enabled              bool
	MinLiqBufferMult     float64
	MaxFundingCostR      float64
	MaxSpreadBps         float64
	MinBookImbalance     float64
	MaxRecentSlippageBps float64
}

type Input struct {
	Side              string
	Entry             float64
	Stop              float64
	Leverage          float64
	NotionalUSD       float64
	FundingRate       float64
	HoldHours         float64
	SpreadBps         float64
	BookImbalance     float64
	RecentSlippageBps float64
	VenueHealthy      bool
}

type Decision struct {
	Approved      bool
	RejectReason  string
	LiqBufferOK   bool
	LiqBufferMult float64
	FundingCostR  float64
}

func DefaultConfig() Config {
	return Config{
		Enabled:              true,
		MinLiqBufferMult:     2.5,
		MaxFundingCostR:      0.25,
		MaxSpreadBps:         15,
		MinBookImbalance:     1.02,
		MaxRecentSlippageBps: 20,
	}
}

func Approve(cfg Config, in Input) Decision {
	if !cfg.Enabled {
		return Decision{Approved: true, LiqBufferOK: true}
	}
	if cfg.MinLiqBufferMult <= 0 {
		cfg.MinLiqBufferMult = 2.5
	}
	if cfg.MaxFundingCostR <= 0 {
		cfg.MaxFundingCostR = 0.25
	}
	if cfg.MaxSpreadBps <= 0 {
		cfg.MaxSpreadBps = 15
	}
	if cfg.MinBookImbalance <= 0 {
		cfg.MinBookImbalance = 1.02
	}
	if cfg.MaxRecentSlippageBps <= 0 {
		cfg.MaxRecentSlippageBps = 20
	}
	if !in.VenueHealthy {
		return Decision{RejectReason: "venue_unhealthy"}
	}
	if in.Entry <= 0 || in.Stop <= 0 || in.Leverage <= 0 {
		return Decision{RejectReason: "invalid_risk_input"}
	}
	if in.SpreadBps > 0 && in.SpreadBps > cfg.MaxSpreadBps {
		return Decision{RejectReason: "spread_too_wide"}
	}
	if in.BookImbalance > 0 && in.BookImbalance < cfg.MinBookImbalance {
		return Decision{RejectReason: "depth_too_thin"}
	}
	if in.RecentSlippageBps > cfg.MaxRecentSlippageBps {
		return Decision{RejectReason: "slippage_anomaly"}
	}

	stopDistPct := math.Abs((in.Entry-in.Stop)/in.Entry) * 100.0
	if stopDistPct <= 0 {
		return Decision{RejectReason: "invalid_stop_distance"}
	}
	liqDistPct := approxLiqDistancePct(in.Leverage)
	liqMult := liqDistPct / stopDistPct
	if liqMult < cfg.MinLiqBufferMult {
		return Decision{
			RejectReason:  "liq_buffer_violation",
			LiqBufferOK:   false,
			LiqBufferMult: liqMult,
		}
	}

	fundingCostR := fundingCostInR(in)
	if fundingCostR > cfg.MaxFundingCostR {
		return Decision{
			RejectReason: "funding_too_expensive",
			LiqBufferOK:  true,
			FundingCostR: fundingCostR,
		}
	}

	return Decision{
		Approved:      true,
		LiqBufferOK:   true,
		LiqBufferMult: liqMult,
		FundingCostR:  fundingCostR,
	}
}

func approxLiqDistancePct(leverage float64) float64 {
	if leverage <= 0 {
		return 0
	}
	// Conservative approximation for perps with maintenance margin buffer.
	return (100.0 / leverage) * 0.9
}

func fundingCostInR(in Input) float64 {
	if in.NotionalUSD <= 0 || in.Entry <= 0 || in.Stop <= 0 {
		return 0
	}
	stopDistPct := math.Abs((in.Entry-in.Stop)/in.Entry) * 100.0
	riskUSD := in.NotionalUSD * (stopDistPct / 100.0)
	if riskUSD <= 0 {
		return 0
	}
	holdH := in.HoldHours
	if holdH <= 0 {
		holdH = 8
	}
	intervals := holdH / 8.0
	if intervals <= 0 {
		intervals = 1
	}
	fr := in.FundingRate
	if fundingAgainstSide(fr, in.Side) {
		cost := math.Abs(fr) * in.NotionalUSD * intervals
		return cost / riskUSD
	}
	return 0
}

func fundingAgainstSide(fr float64, side string) bool {
	if side == "BUY" {
		return fr > 0
	}
	if side == "SELL" {
		return fr < 0
	}
	return false
}
