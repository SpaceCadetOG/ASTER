// go-machine/adapters/aster/orderbook.go
package aster

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

type depthResp struct {
	LastUpdateId int        `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"` // [price, qty]
	Asks         [][]string `json:"asks"`
}

type OrderBook struct {
	Bids [][2]float64
	Asks [][2]float64
}

func (c *Client) FetchOrderBook(symbol string, limit int) (OrderBook, error) {
	if limit <= 0 {
		limit = 50
	}
	u := c.buildURL("/depth", map[string]string{
		"symbol": symbol,
		"limit":  strconv.Itoa(limit),
	})
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return OrderBook{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return OrderBook{}, fmt.Errorf("depth status %d: %s", resp.StatusCode, string(b))
	}
	var dr depthResp
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return OrderBook{}, err
	}
	ob := OrderBook{Bids: make([][2]float64, 0, len(dr.Bids)), Asks: make([][2]float64, 0, len(dr.Asks))}
	for _, x := range dr.Bids {
		if len(x) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(x[0], 64)
		q, _ := strconv.ParseFloat(x[1], 64)
		ob.Bids = append(ob.Bids, [2]float64{p, q})
	}
	for _, x := range dr.Asks {
		if len(x) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(x[0], 64)
		q, _ := strconv.ParseFloat(x[1], 64)
		ob.Asks = append(ob.Asks, [2]float64{p, q})
	}
	return ob, nil
}
