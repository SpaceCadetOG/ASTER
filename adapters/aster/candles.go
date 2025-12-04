package aster

import (
	"encoding/json"
	"fmt"
	"time"

	"go-machine/internal/types"
)

// LoadCandles fetches TRADE candles (with volume) for any symbol.
// Example: symbol="BTCUSDT", tf=types.TF15m, n=200.
func (c *Client) LoadCandles(symbol string, tf types.TF, n int) ([]types.Candle, error) {
	if n <= 0 {
		n = 200
	}
	iv, err := tfToInterval(tf)
	if err != nil {
		return nil, err
	}

	url := c.buildURL("/klines", map[string]string{
		"symbol":   symbol,
		"interval": iv,
		"limit":    fmt.Sprintf("%d", n),
	})

	// The API returns [][]mixed (Binance style). We decode with UseNumber
	// so we can safely parse without float rounding surprises.
	var raw [][]json.Number
	if err := c.fetchJSON(url, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("no kline data")
	}

	out := make([]types.Candle, 0, len(raw))
	for _, row := range raw {
		// Expected at least 12 fields; we guard lightly.
		// [0] openTime, [1] O, [2] H, [3] L, [4] C,
		// [5] baseVolume, [6] closeTime, [7] quoteVolume, ...
		if len(row) < 8 {
			continue
		}

		// time(s)
		ms, _ := row[0].Int64()
		t := time.UnixMilli(ms).UTC()

		// OHLC
		o, _ := numToFloat(row[1])
		h, _ := numToFloat(row[2])
		l, _ := numToFloat(row[3])
		cl, _ := numToFloat(row[4])

		// volume preference: quote volume (index 7) in $-ish
		qv, _ := numToFloat(row[7])
		if qv == 0 {
			// fallback: baseVolume * close
			bv, _ := numToFloat(row[5])
			qv = bv * cl
		}

		out = append(out, types.Candle{
			T: t, O: o, H: h, L: l, C: cl, V: qv,
		})
	}

	return types.EnsureSorted(out), nil
}
