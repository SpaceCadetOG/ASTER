// go-machine/internal/api/fusion.go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-machine/adapters/aster"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

func FusionHandler(c *aster.Client) http.HandlerFunc {
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

		n, _ := strconv.Atoi(q.Get("n"))
		if n <= 0 {
			n = 200
		}
		left, _ := strconv.Atoi(q.Get("left"))
		if left <= 0 {
			left = 3
		}
		right, _ := strconv.Atoi(q.Get("right"))
		if right <= 0 {
			right = 3
		}
		levels, _ := strconv.Atoi(q.Get("levels"))
		if levels <= 0 {
			levels = 50
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

		// candles
		bars, err := c.LoadCandles(symbol, tf, n)
		if err != nil || len(bars) == 0 {
			http.Error(w, "no candles", http.StatusBadRequest)
			return
		}

		// structure
		pivs := ta.DetectFractalPivots(bars, left, right)
		sw := ta.BuildSwings(pivs)

		// trend/effort
		vwap := calcVWAP(bars)
		tr := ta.TrendMetrics(symbol, tf, bars, vwap)
		ef := ta.ComputeEffort(symbol, tf, bars, win, zmin, vmin)

		// orderbook
		obRaw, err := c.FetchOrderBook(symbol, levels)
		if err != nil {
			http.Error(w, "orderbook fetch failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		ob := ta.OrderBookContext(symbol, obRaw.Bids, obRaw.Asks, levels)

		// choose side: prefer Trend bias; otherwise lean by OB imbalance
		side := "long"
		switch tr.Bias {
		case "bull":
			side = "long"
		case "bear":
			side = "short"
		default:
			if ob.Imbalance < 0 {
				side = "short"
			}
		}

		// confluence
		conf := ta.ComputeConfluence(tr, ef, ob, side)

		resp := struct {
			Symbol string   `json:"symbol"`
			TF     types.TF `json:"tf"`
			Side   string   `json:"side"`

			// fusion-ish summary
			ta.ConfluenceResult `json:",inline"`

			// details
			Trend     ta.TrendResult  `json:"trend"`
			Effort    ta.EffortResult `json:"effort"`
			Orderbook ta.OBContext    `json:"orderbook"`
			Pivots    []ta.Pivot      `json:"pivots"`
			Swings    []ta.Swing      `json:"swings"`
		}{
			Symbol:           symbol,
			TF:               tf,
			Side:             side,
			ConfluenceResult: conf,
			Trend:            tr,
			Effort:           ef,
			Orderbook:        ob,
			Pivots:           pivs,
			Swings:           sw,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
