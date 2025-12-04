// internal/api/patterns.go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-machine/adapters/aster"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

func PatternsHandler(c *aster.Client) http.HandlerFunc {
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
			http.Error(w, "bad tf", 400)
			return
		}

		bars, err := c.LoadCandles(symbol, tf, n)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		tags := ta.DetectPatterns(bars)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Symbol string          `json:"symbol"`
			TF     types.TF        `json:"tf"`
			Count  int             `json:"count"`
			Tags   []ta.PatternTag `json:"tags"`
		}{symbol, tf, len(tags), tags})
	}
}
