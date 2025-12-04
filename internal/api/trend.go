// go-machine/internal/api/trend.go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-machine/adapters/aster"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

func TrendHandler(c *aster.Client) http.HandlerFunc {
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
			http.Error(w, "invalid tf", 400)
			return
		}

		bars, err := c.LoadCandles(symbol, tf, n)
		if err != nil || len(bars) == 0 {
			http.Error(w, "no candles", 400)
			return
		}
		// need VWAP; reusing the same VWAP as effort module
		VW := ta.ComputeEffort(symbol, tf, bars, 20, 2.0, 0).VWAP

		res := ta.TrendMetrics(symbol, tf, bars, VW)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}
}
