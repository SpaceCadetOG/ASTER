package tools

import (
	"fmt"
	"math"
)

// Side represents long/short direction.
type Side string

const (
	Long  Side = "long"
	Short Side = "short"
)

// Inputs describes a perp position snapshot.
type Inputs struct {
	Margin          float64 // your capital used
	Leverage        float64 // e.g. 5 = 5x
	EntryPrice      float64 // average entry
	MarkPrice       float64 // current price (or exit to calc realized)
	Side            Side    // "long" or "short"
	FeeOpenRate     float64 // taker fee at open (e.g., 0.0005 = 0.05%)
	FeeCloseRate    float64 // taker fee at close (same units)
	FundingPaidRate float64 // optional funding paid/received during hold (e.g., -0.0001 means you received)
}

// Result holds computed metrics.
type Result struct {
	PositionSize   float64 // Margin * Leverage
	PriceMovePct   float64 // +good for your side, -bad
	GrossPnL       float64 // before fees/funding
	FeeCost        float64 // open+close fees on notional
	FundingPnL     float64 // posSize * fundingRate (approx)
	NetPnL         float64 // after fees/funding
	ROEPercent     float64 // NetPnL / Margin * 100
	LiqPriceApprox float64 // if provided a maint. margin rate via helper
}

// PriceMoveSigned returns the signed % move helpful for the position.
// Example: long, entry 100 -> mark 105 => +5% ; short, same => -5%.
func PriceMoveSigned(entry, mark float64, side Side) float64 {
	if entry <= 0 {
		return 0
	}
	raw := (mark - entry) / entry
	if side == Short {
		raw = -raw
	}
	return raw
}

// ComputePnLROE calculates PnL/ROE with fees & funding.
func ComputePnLROE(in Inputs) Result {
	pos := in.Margin * in.Leverage
	move := PriceMoveSigned(in.EntryPrice, in.MarkPrice, in.Side) // fraction, e.g., 0.031 = 3.1%

	gross := pos * move

	// Fees are typically charged on notional at fill; we approximate open+close on notional.
	fee := pos * (in.FeeOpenRate + in.FeeCloseRate)

	// Funding: positive means you PAY; negative means you RECEIVE.
	funding := pos * in.FundingPaidRate

	net := gross - fee - funding
	roe := 0.0
	if in.Margin > 0 {
		roe = (net / in.Margin) * 100.0
	}

	return Result{
		PositionSize: pos, PriceMovePct: move * 100,
		GrossPnL: gross, FeeCost: fee, FundingPnL: -funding, // show received funding as positive
		NetPnL: net, ROEPercent: roe,
	}
}

// TP/SL for desired Risk:Reward.
// riskMovePct = absolute % move AGAINST entry (e.g., 2 means -2% for a long, +2% for a short).
// rr = Reward:Risk ratio (e.g., 2 for 2:1).
func TPSLForRR(entry float64, side Side, riskMovePct, rr float64) (tp, sl float64) {
	riskFrac := riskMovePct / 100.0
	rewardFrac := rr * riskFrac

	switch side {
	case Long:
		sl = entry * (1 - riskFrac)
		tp = entry * (1 + rewardFrac)
	case Short:
		sl = entry * (1 + riskFrac)
		tp = entry * (1 - rewardFrac)
	}
	return
}

// LiquidationPriceApprox provides a rough liq price using a maintenance margin rate.
// maintRate is often ~0.005–0.01 (0.5%–1.0%), but check your exchange.
// We assume liq when unrealized loss ≈ (Margin - maintRate*PositionSize).
func LiquidationPriceApprox(entry float64, side Side, margin, leverage, maintRate, feeBuffer float64) float64 {
	if entry <= 0 || leverage <= 0 {
		return math.NaN()
	}
	pos := margin * leverage
	maint := maintRate * pos
	// Allow an extra buffer for fees/funding if you want (in USDT).
	effectiveMargin := math.Max(0, margin-maint-feeBuffer)

	// priceMoveAgainst is the fractional move that consumes effectiveMargin.
	if pos <= 0 {
		return math.NaN()
	}
	priceMoveAgainst := effectiveMargin / pos // e.g., 0.18 means ~18% adverse move wipes you

	switch side {
	case Long:
		return entry * (1 - priceMoveAgainst)
	case Short:
		return entry * (1 + priceMoveAgainst)
	default:
		return math.NaN()
	}
}

func RR() {
	// === Example 1: your screenshot-like numbers (LONG) ===
	in := Inputs{
		Margin:          502,
		Leverage:        5,
		EntryPrice:      5.508,
		MarkPrice:       6.9289, // down 4% -> good for short
		Side:            Long,
		FeeOpenRate:     0.0004,
		FeeCloseRate:    0.0004,
		FundingPaidRate: 0.0, // ignore for intraday
	}
	res := ComputePnLROE(in)

	fmt.Printf("Position size:     $%.4f\n", res.PositionSize)
	fmt.Printf("Price move:        %+0.2f%%\n", res.PriceMovePct)
	fmt.Printf("Gross PnL:         $%.4f\n", res.GrossPnL)
	fmt.Printf("Fees (est):        $%.4f\n", res.FeeCost)
	fmt.Printf("Funding (est):     $%.4f\n", res.FundingPnL)
	fmt.Printf("Net PnL:           $%.4f\n", res.NetPnL)
	fmt.Printf("ROE:               %+0.2f%%\n", res.ROEPercent)

	// TP/SL for 2:1 with 2% risk
	tp, sl := TPSLForRR(in.EntryPrice, in.Side, 2.0, 2.0)
	fmt.Printf("\n2:1 R:R with 2%% risk -> SL: %.6f, TP: %.6f\n", sl, tp)

	// Approx liquidation with maint 0.5% and small fee buffer
	liq := LiquidationPriceApprox(in.EntryPrice, in.Side, in.Margin, in.Leverage, 0.005, 0.10)
	fmt.Printf("Approx liq price:  %.6f\n", liq)

	// === Example 2: generic example (SHORT) ===
	in2 := Inputs{
		Margin:          1000,
		Leverage:        10,
		EntryPrice:      2.9508,
		MarkPrice:       5.3289, // down 4% -> good for short
		Side:            Short,
		FeeOpenRate:     0.0004,
		FeeCloseRate:    0.0004,
		FundingPaidRate: 0.0,
	}
	res2 := ComputePnLROE(in2)
	fmt.Printf("\n[SHORT] Net PnL: $%.2f | ROE: %+0.2f%%\n", res2.NetPnL, res2.ROEPercent)
}
