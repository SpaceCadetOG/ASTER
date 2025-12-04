// go-machine/internal/backtest/strategy.go
package backtest

import (
	"fmt"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/types"
)

type bar struct {
	T time.Time
	C float64
}

func tfFromString(s string) types.TF {
	tf, ok := types.ParseTF(strings.TrimSpace(s))
	if !ok {
		return types.TF15m
	}
	return tf
}

// runStrategy produces at most one trade per symbol for now.
// It enforces 15m bias (or cfg.BiasTF), 5m confirmation, and 1m trigger where applicable.
func runStrategy(client *aster.Client, symbol string, tf types.TF, from, to time.Time, cfg Config) ([]Trade, error) {
	switch cfg.Strategy {
	case "fib":
		return runFib(client, symbol, tf, from, to, cfg)
	case "ema9":
		return runEMA9(client, symbol, tf, from, to, cfg)
	default:
		return nil, fmt.Errorf("unknown strategy: %s", cfg.Strategy)
	}
}

// helpers to load OHLCV arrays for indicators

func loadOHLCV(client *aster.Client, symbol string, tf types.TF, from, to time.Time, pad int) ([]ohlcv, error) {
	// rough #bars (pad extra to compute EMAs)
	n := max(int(to.Sub(from).Minutes()/tfMinutes(tf))+50, pad)
	raw, err := client.LoadCandles(symbol, tf, n)
	if err != nil {
		return nil, err
	}
	out := make([]ohlcv, 0, len(raw))
	for _, c := range raw {
		if c.T.Before(from) || c.T.After(to) {
			continue
		}
		out = append(out, ohlcv{T: c.T, O: c.O, H: c.H, L: c.L, C: c.C, V: c.V})
	}
	// If no bars in-range, fallback to just first/last to keep pipeline alive
	if len(out) == 0 && len(raw) >= 2 {
		out = []ohlcv{
			{T: raw[0].T, O: raw[0].O, H: raw[0].H, L: raw[0].L, C: raw[0].C, V: raw[0].V},
			{T: raw[len(raw)-1].T, O: raw[len(raw)-1].O, H: raw[len(raw)-1].H, L: raw[len(raw)-1].L, C: raw[len(raw)-1].C, V: raw[len(raw)-1].V},
		}
	}
	return out, nil
}
