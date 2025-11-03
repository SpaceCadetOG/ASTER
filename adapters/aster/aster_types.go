package aster

import "encoding/json"

type KlineData [][]json.Number

type tickerStats struct {
	Symbol             string      `json:"symbol"`
	PriceChangePercent json.Number `json:"priceChangePercent"`
	QuoteVolume        json.Number `json:"quoteVolume"`
	OpenPrice          json.Number `json:"openPrice"`
	LastPrice          json.Number `json:"lastPrice"`
}

type fundingRateEntry struct {
	Symbol      string      `json:"symbol"`
	FundingRate json.Number `json:"fundingRate"`
	FundingTime int64       `json:"fundingTime"`
}

type priceTicker struct {
	Symbol string      `json:"symbol"`
	Price  json.Number `json:"price"`
	Time   int64       `json:"time"`
}
