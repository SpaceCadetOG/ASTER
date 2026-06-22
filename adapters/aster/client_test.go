package aster

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestUTCBucketStartBoundaries(t *testing.T) {
	tm := time.Date(2026, 6, 20, 16, 57, 17, 0, time.UTC)
	if got := utcBucketStart(tm, time.Hour); !got.Equal(time.Date(2026, 6, 20, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("1h start mismatch: %s", got)
	}
	if got := utcBucketStart(tm, 4*time.Hour); !got.Equal(time.Date(2026, 6, 20, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("4h start mismatch at boundary hour: %s", got)
	}
	if got := utcBucketStart(tm, 24*time.Hour); !got.Equal(time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("24h start mismatch: %s", got)
	}

	tm = time.Date(2026, 6, 20, 17, 1, 0, 0, time.UTC)
	if got := utcBucketStart(tm, time.Hour); !got.Equal(time.Date(2026, 6, 20, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("1h start mismatch after top of hour: %s", got)
	}
	if got := utcBucketStart(tm, 4*time.Hour); !got.Equal(time.Date(2026, 6, 20, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("4h start mismatch after top of hour: %s", got)
	}
}

func TestFetchAllMarketsComputesDistinctUTCWindows(t *testing.T) {
	now := time.Date(2026, 6, 20, 17, 1, 0, 0, time.UTC)
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
			"fundingTime": now.UnixMilli(),
		}})
	})
	mux.HandleFunc("/ticker/price", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"symbol": "BTCUSDT",
			"price":  json.Number("112"),
		})
	})
	mux.HandleFunc("/openInterest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"symbol":       "BTCUSDT",
			"openInterest": json.Number("10"),
		})
	})
	mux.HandleFunc("/klines", func(w http.ResponseWriter, r *http.Request) {
		startMs, _ := strconv.ParseInt(r.URL.Query().Get("startTime"), 10, 64)
		start := time.UnixMilli(startMs).UTC()
		open := "100"
		switch start {
		case time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC):
			open = "100"
		case time.Date(2026, 6, 20, 16, 0, 0, 0, time.UTC):
			open = "108"
		case time.Date(2026, 6, 20, 17, 0, 0, 0, time.UTC):
			open = "111"
		default:
			t.Fatalf("unexpected startTime requested: %s", start)
		}
		_ = json.NewEncoder(w).Encode([][]any{{
			json.Number(strconv.FormatInt(startMs, 10)),
			json.Number(open),
		}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)
	c.openCache = map[string]map[string]float64{}
	origTimeNow := timeNow
	timeNow = func() time.Time { return now }
	defer func() { timeNow = origTimeNow }()

	mkts := c.FetchAllMarkets()
	if len(mkts) != 1 {
		t.Fatalf("expected one market, got %d", len(mkts))
	}
	m := mkts[0]
	if m.DayUTC24h == nil || m.UTC4hPct == nil || m.UTC1hPct == nil {
		t.Fatalf("expected day/4h/1h percentages to be populated: %+v", m)
	}
	if *m.DayUTC24h == *m.UTC4hPct || *m.UTC4hPct == *m.UTC1hPct {
		t.Fatalf("expected distinct day/4h/1h percentages outside reset overlap, got day=%.4f 4h=%.4f 1h=%.4f", *m.DayUTC24h, *m.UTC4hPct, *m.UTC1hPct)
	}
}
