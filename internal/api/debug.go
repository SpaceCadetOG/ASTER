package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-machine/adapters/aster"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

// /api/candles?symbol=BTCUSDT&tf=15m&n=200
func CandlesHandler(c *aster.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		symbol := q.Get("symbol")
		tfStr := q.Get("tf")
		n, _ := strconv.Atoi(q.Get("n"))
		if n <= 0 {
			n = 200
		}

		tf, ok := types.ParseTF(tfStr)
		if !ok {
			http.Error(w, "bad tf", http.StatusBadRequest)
			return
		}

		data, err := c.LoadCandles(symbol, tf, n)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := types.CandleSet{Symbol: symbol, TF: tf, Data: data}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// /api/pivots?symbol=BTCUSDT&tf=15m&n=200&left=3&right=3
func PivotsHandler(c *aster.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		symbol := q.Get("symbol")
		tfStr := q.Get("tf")
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

		tf, ok := types.ParseTF(tfStr)
		if !ok {
			http.Error(w, "bad tf", http.StatusBadRequest)
			return
		}
		bars, err := c.LoadCandles(symbol, tf, n)
		if err != nil || len(bars) == 0 {
			http.Error(w, "no candles", http.StatusBadRequest)
			return
		}

		pivs := ta.DetectFractalPivots(bars, left, right)
		resp := struct {
			Symbol string     `json:"symbol"`
			TF     types.TF   `json:"tf"`
			Left   int        `json:"left"`
			Right  int        `json:"right"`
			Data   []ta.Pivot `json:"data"`
		}{symbol, tf, left, right, pivs}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
