// TraderBot/go-machine/cmd/rr/main.go
package main

import (
	"fmt"
	"math"
	"strings"

	"go-machine/internal/tools"
)

func printSnapshot(title string, in tools.Inputs) {
	fmt.Println(title)
	res := tools.ComputePnLROE(in)
	fmt.Printf("Position size:     $%.4f\n", res.PositionSize)
	fmt.Printf("Price move:        %+0.2f%%\n", res.PriceMovePct)
	fmt.Printf("Gross PnL:         $%.4f\n", res.GrossPnL)
	fmt.Printf("Fees (est):        $%.4f\n", res.FeeCost)
	fmt.Printf("Funding (est):     $%.4f\n", res.FundingPnL)
	fmt.Printf("Net PnL:           $%.4f\n", res.NetPnL)
	fmt.Printf("ROE:               %+0.2f%%\n", res.ROEPercent)
}

func main() {
	// ===== LONG example =====
	long := tools.Inputs{
		Margin:          100,       // $50 margin
		Leverage:        5,         // 5x
		EntryPrice:      3833.3000, // entry
		MarkPrice:       4135.4000, // current mark for snapshot
		Side:            tools.Long,
		FeeOpenRate:     0.0004, // 0.04%
		FeeCloseRate:    0.0004, // 0.04%
		FundingPaidRate: 0.0,    // ignore for demo
	}

	printSnapshot("\n=== LONG snapshot ===", long)

	tp, sl := tools.TPSLForRR(long.EntryPrice, long.Side, 2.0, 2.0)
	fmt.Printf("\n2:1 R:R with 2%% risk -> SL: %.6f, TP: %.6f\n", sl, tp)

	liq := tools.LiquidationPriceApprox(long.EntryPrice, long.Side, long.Margin, long.Leverage, 0.005, 0.10)
	fmt.Printf("Approx liq price:  %.6f\n", liq)

	fmt.Println("\n--- LONG R Ladder (risk 2.0%%, 1R..4R) ---")
	PrintRRTable(long, 2.0, 4)

	// ===== SHORT example =====
	short := tools.Inputs{
		Margin:          1000, // $100 margin
		Leverage:        50,   // 5x
		EntryPrice:      4136, // entry (note reversed vs long)
		MarkPrice:       3355, // current mark for snapshot
		Side:            tools.Short,
		FeeOpenRate:     0.0004,
		FeeCloseRate:    0.0004,
		FundingPaidRate: 0.0,
	}

	fmt.Println("\n----------------------------------------")

	printSnapshot("\n=== SHORT snapshot ===", short)

	tp2, sl2 := tools.TPSLForRR(short.EntryPrice, short.Side, 2.0, 2.0)
	fmt.Printf("\n2:1 R:R with 2%% risk -> SL: %.6f, TP: %.6f\n", sl2, tp2)

	liq2 := tools.LiquidationPriceApprox(short.EntryPrice, short.Side, short.Margin, short.Leverage, 0.005, 0.10)
	fmt.Printf("Approx liq price:  %.6f\n", liq2)

	fmt.Println("\n--- SHORT R Ladder (risk 2%%, 1R..4R) ---")
	PrintRRTable(short, 2, 4)
}

// PriceAtR returns the mark/exit price that corresponds to +R multiples
// given a per-trade risk measured as a % move against entry (riskMovePct).
func PriceAtR(entry float64, side tools.Side, riskMovePct float64, R float64) float64 {
	riskFrac := riskMovePct / 100.0
	switch side {
	case tools.Long:
		return entry * (1 + R*riskFrac)
	case tools.Short:
		return entry * (1 - R*riskFrac)
	default:
		return math.NaN()
	}
}

// RRRow captures each checkpoint along the R ladder.
type RRRow struct {
	R         float64
	Price     float64
	NetPnL    float64
	ROE       float64
	GrossPnL  float64
	FeeCost   float64
	FundingPn float64
}

// RRTable computes +1R..+maxR targets and PnL/ROE for each.
func RRTable(in tools.Inputs, riskMovePct float64, maxR int) []RRRow {
	rows := make([]RRRow, 0, maxR)
	for r := 1; r <= maxR; r++ {
		tp := PriceAtR(in.EntryPrice, in.Side, riskMovePct, float64(r))
		tmp := in
		tmp.MarkPrice = tp
		res := tools.ComputePnLROE(tmp)
		rows = append(rows, RRRow{
			R:         float64(r),
			Price:     tp,
			NetPnL:    res.NetPnL,
			ROE:       res.ROEPercent,
			GrossPnL:  res.GrossPnL,
			FeeCost:   res.FeeCost,
			FundingPn: res.FundingPnL,
		})
	}
	return rows
}

// PrintRRTable prints a compact R ladder (+1R..+maxR)
func PrintRRTable(in tools.Inputs, riskMovePct float64, maxR int) {
	fmt.Printf("R ladder for %s at entry %.4f (risk %.2f%%)\n", strings.ToUpper(string(in.Side)), in.EntryPrice, riskMovePct)
	fmt.Printf("%-4s | %-10s | %-10s | %-8s\n", "R", "Price", "NetPnL", "ROE%")
	fmt.Println(strings.Repeat("-", 40))

	rows := RRTable(in, riskMovePct, maxR)
	for _, row := range rows {
		fmt.Printf("%4.0f | %-10.4f | %-10.2f | %-8.2f\n", row.R, row.Price, row.NetPnL, row.ROE)
	}
}
