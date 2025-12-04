// go-machine/internal/api/effort.go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-machine/adapters/aster"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

func EffortHandler(c *aster.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		symbol := q.Get("symbol")
		tfStr := q.Get("tf")
		n, _ := strconv.Atoi(q.Get("n"))
		if n <= 0 {
			n = 200
		}
		win, _ := strconv.Atoi(q.Get("win"))
		if win <= 1 {
			win = 20
		}
		zmin, _ := strconv.ParseFloat(q.Get("zmin"), 64)
		if zmin <= 0 {
			zmin = 2.0
		}
		vmin, _ := strconv.ParseFloat(q.Get("vmin"), 64)
		if vmin < 0 {
			vmin = 0
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

		res := ta.ComputeEffort(symbol, tf, bars, win, zmin, vmin)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}
}
