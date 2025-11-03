package backtest

import (
	"go-machine/adapters/aster"
	"go-machine/internal/types"
)

func loadBars(c *aster.Client, symbol string, tf types.TF, n int) ([]types.Candle, error) {
	// aster client loads most-recent n bars; we’ll overshoot n to cover the window.
	return c.LoadCandles(symbol, tf, n)
}
