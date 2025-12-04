package market

import (
	"fmt"
	"math"
	"strings"
)

func humanUSD(x float64) string {
	ax := math.Abs(x)
	switch {
	case ax >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", x/1_000_000_000)
	case ax >= 1_000_000:
		return fmt.Sprintf("%.2fM", x/1_000_000)
	case ax >= 1_000:
		return fmt.Sprintf("%.2fK", x/1_000)
	default:
		return fmt.Sprintf("%.0f", x)
	}
}

func pctFromFractionPtr(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.4f", (*p)*100.0)
}

func usdFromFloatPtr(p *float64) string {
	if p == nil {
		return "-"
	}
	return humanUSD(*p)
}

func FormatHeader(exchange string, activeLabels []string) string {
	return fmt.Sprintf("%s • [%s]", exchange, strings.Join(activeLabels, ","))
}

// Symbol | Score | Δ% | Vol($) | OI($) | Funding(%) |  Prev24h | Last
func FormatRow(s Scored) string {
	funding := pctFromFractionPtr(s.FundingRate)
	oi := usdFromFloatPtr(s.OIUSD)

	prev := "-"
	last := "-"
	if s.OpenPrice > 0 {
		prev = fmt.Sprintf("%.4f", s.OpenPrice)
	}
	if s.LastPrice > 0 {
		last = fmt.Sprintf("%.4f", s.LastPrice)
	}

	return fmt.Sprintf("%-12s | %6.2f | %5.1f | %7s | %7s | %10s | %8s | %8s",
		s.Symbol, s.Score, s.Change24h, humanUSD(s.VolumeUSD), oi, funding, prev, last)
}
