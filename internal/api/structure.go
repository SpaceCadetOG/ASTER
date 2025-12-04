package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-machine/adapters/aster"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

// StructureHandler returns fractal pivots + derived swings for quick structure visualization.
//
// GET /api/structure?symbol=BTCUSDT&tf=15m&n=200&left=3&right=3
// left/right are fractal window sizes (defaults 3/3).
func StructureHandler(c *aster.Client) http.HandlerFunc {
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
			http.Error(w, "bad tf", http.StatusBadRequest)
			return
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

		bars, err := c.LoadCandles(symbol, tf, n)
		if err != nil || len(bars) == 0 {
			http.Error(w, "no candles", http.StatusBadRequest)
			return
		}

		pivs := ta.DetectFractalPivots(bars, left, right)
		swings := ta.BuildSwings(pivs)

		resp := struct {
			Symbol string     `json:"symbol"`
			TF     types.TF   `json:"tf"`
			Left   int        `json:"left"`
			Right  int        `json:"right"`
			Pivots []ta.Pivot `json:"pivots"`
			Swings []ta.Swing `json:"swings"`
			NBars  int        `json:"nbars"`
		}{
			Symbol: symbol,
			TF:     tf,
			Left:   left,
			Right:  right,
			Pivots: pivs,
			Swings: swings,
			NBars:  len(bars),
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
