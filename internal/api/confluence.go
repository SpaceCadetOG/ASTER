// go-machine/internal/api/confluence.go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-machine/adapters/aster"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

// simple VWAP over the provided candles
func calcVWAP(bars []types.Candle) float64 {
	var pv, v float64
	for _, b := range bars {
		// Typical price; if you have trade price use that instead.
		tp := (b.H + b.L + b.C) / 3.0
		pv += tp * b.V
		v += b.V
	}
	if v == 0 {
		return 0
	}
	return pv / v
}

func ConfluenceHandler(c *aster.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		symbol := q.Get("symbol")
		if symbol == "" {
			http.Error(w, "missing symbol", http.StatusBadRequest)
			return
		}

		tfStr := q.Get("tf")
		if tfStr == "" {
			tfStr = "15m"
		}
		tf, ok := types.ParseTF(tfStr)
		if !ok {
			tf = types.TF15m
		}

		side := q.Get("side")
		if side == "" {
			side = "long"
		}

		n, _ := strconv.Atoi(q.Get("n"))
		if n <= 0 {
			n = 200
		}
		win, _ := strconv.Atoi(q.Get("win"))
		if win <= 0 {
			win = 20
		}
		zmin, _ := strconv.ParseFloat(q.Get("zmin"), 64)
		if zmin <= 0 {
			zmin = 2.0
		}
		vmin, _ := strconv.ParseFloat(q.Get("vmin"), 64)
		if vmin <= 0 {
			vmin = 5_000_000
		}
		levels, _ := strconv.Atoi(q.Get("levels"))
		if levels <= 0 {
			levels = 50
		}

		// 1) Candles
		bars, err := c.LoadCandles(symbol, tf, n)
		if err != nil || len(bars) == 0 {
			http.Error(w, "no candles", http.StatusBadRequest)
			return
		}

		// 2) Trend (needs VWAP)
		vwap := calcVWAP(bars)
		tr := ta.TrendMetrics(symbol, tf, bars, vwap)

		// 3) Effort (volume spikes + VWAP context)
		ef := ta.ComputeEffort(symbol, tf, bars, win, zmin, vmin)

		// 4) Orderbook → context
		obRaw, err := c.FetchOrderBook(symbol, levels)
		if err != nil {
			http.Error(w, "orderbook fetch failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		ob := ta.OrderBookContext(symbol, obRaw.Bids, obRaw.Asks, levels)

		// 5) Fuse → Confluence (side-aware)
		conf := ta.ComputeConfluence(tr, ef, ob, side)

		resp := struct {
			Symbol string   `json:"symbol"`
			TF     types.TF `json:"tf"`
			Side   string   `json:"side"`

			ta.ConfluenceResult `json:",inline"`

			Trend     ta.TrendResult  `json:"trend"`
			Effort    ta.EffortResult `json:"effort"`
			Orderbook ta.OBContext    `json:"orderbook"`
		}{
			Symbol:           symbol,
			TF:               tf,
			Side:             side,
			ConfluenceResult: conf,
			Trend:            tr,
			Effort:           ef,
			Orderbook:        ob,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
