package market

import "math"

type GateInput struct {
	Symbol       string
	Score        float64
	VolNow       float64
	VolMed       float64
	VolPrev      float64
	VolPrevMed   float64
	OINow        float64
	OIPrev       float64
	FundingNow   float64
	FundingPrev  float64
}

type GateResult struct {
	Pass         bool
	Reason       string
	VolRatio     float64
	VolPrevRatio float64
	OIChg        float64
	FundingFlip  bool
	Rank         float64
}

func clamp(x, lo, hi float64) float64 {
	if x < lo { return lo }
	if x > hi { return hi }
	return x
}

func HotGate(in GateInput) GateResult {
	eps := 1e-9

	volRatio := in.VolNow / math.Max(in.VolMed, eps)
	volPrevRatio := in.VolPrev / math.Max(in.VolPrevMed, eps)

	oiChg := (in.OINow - in.OIPrev) / math.Max(in.OIPrev, eps)

	fundingFlipLong := (in.FundingPrev <= 0 && in.FundingNow > 0)
	fundingFlipShort := (in.FundingPrev >= 0 && in.FundingNow < 0)
	fundingFlip := fundingFlipLong || fundingFlipShort
	fundingDelta := math.Abs(in.FundingNow - in.FundingPrev)

	// Core gates
	if in.Score < 85 {
		return GateResult{Pass:false, Reason:"score<85", VolRatio:volRatio, VolPrevRatio:volPrevRatio, OIChg:oiChg}
	}

	volPass := (volRatio >= 1.8) || (volRatio >= 1.5 && volPrevRatio >= 1.3)
	if !volPass {
		return GateResult{Pass:false, Reason:"no vol spike", VolRatio:volRatio, VolPrevRatio:volPrevRatio, OIChg:oiChg}
	}

	// OI rising + sanity
	if oiChg < 0.01 { // 1% default for 5m
		return GateResult{Pass:false, Reason:"oi not rising", VolRatio:volRatio, VolPrevRatio:volPrevRatio, OIChg:oiChg}
	}

	// Funding flip with magnitude filter
	if !fundingFlip || fundingDelta < 0.0002 {
		return GateResult{Pass:false, Reason:"no funding flip", VolRatio:volRatio, VolPrevRatio:volPrevRatio, OIChg:oiChg}
	}

	// Rank (0..1-ish)
	scoreTerm := clamp(in.Score/100.0, 0, 1)
	volTerm := clamp(volRatio/3.0, 0, 1)
	oiTerm := clamp(oiChg/0.05, 0, 1) // 5% OI change == 1
	flipBonus := 0.10

	rank := 0.45*scoreTerm + 0.30*volTerm + 0.20*oiTerm + 0.05*flipBonus

	return GateResult{
		Pass:true,
		Reason:"PASS",
		VolRatio:volRatio,
		VolPrevRatio:volPrevRatio,
		OIChg:oiChg,
		FundingFlip:true,
		Rank:rank,
	}
}