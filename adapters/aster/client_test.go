package aster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchOneStyleMapping(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ticker/24hr", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"symbol":             "BTCUSDT",
			"priceChangePercent": json.Number("12.34"),
			"quoteVolume":        json.Number("25000000"),
			"openPrice":          json.Number("62800"),
			"lastPrice":          json.Number("63100"),
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/fundingRate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"symbol":      "BTCUSDT",
			"fundingRate": json.Number("0.0008"),
			"fundingTime": time.Now().UnixMilli(),
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	mkts := c.FetchAllMarkets()
	if len(mkts) == 0 {
		t.Fatal("no markets")
	}
	var found bool
	for _, m := range mkts {
		if m.Symbol == "BTC-USD" {
			found = true
			if m.Change24h != 12.34 {
				t.Fatalf("pct mismatch: %v", m.Change24h)
			}
			if m.VolumeUSD != 25_000_000 {
				t.Fatalf("vol mismatch: %v", m.VolumeUSD)
			}
			if m.FundingRate == nil || *m.FundingRate != 0.0008 {
				t.Fatalf("funding mismatch")
			}
			if m.LastPrice != 63100 {
				t.Fatalf("last mismatch: %v", m.LastPrice)
			}
			if m.OpenPrice != 62800 {
				t.Fatalf("open mismatch: %v", m.OpenPrice)
			}
		}
	}
	if !found {
		t.Fatal("BTC-USD not found")
	}
}
