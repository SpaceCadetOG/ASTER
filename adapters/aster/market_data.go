package aster

import "go-machine/internal/market"

type SimpleStats struct {
	Symbol             string
	PriceChangePercent float64
	QuoteVolume        float64
}

func ToMarket(exchange string, st SimpleStats, fund *float64) market.Market {
	m := market.Market{
		Exchange:  exchange,
		Symbol:    NormSymbol(st.Symbol),
		Change24h: st.PriceChangePercent,
		VolumeUSD: st.QuoteVolume,
	}
	if fund != nil {
		m.FundingRate = fund
	}
	return m
}
