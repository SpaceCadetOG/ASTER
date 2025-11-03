// go-machine/internal/api/orderbook.go
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"go-machine/adapters/aster"
	"go-machine/internal/ta"
)

func OBContextHandler(c *aster.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		symbol := q.Get("symbol")
		levels, _ := strconv.Atoi(q.Get("levels"))
		if levels <= 0 {
			levels = 20
		}
		ob, err := c.FetchOrderBook(symbol, levels)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		ctx := ta.OrderBookContext(symbol, ob.Bids, ob.Asks, levels)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ctx)
	}
}
