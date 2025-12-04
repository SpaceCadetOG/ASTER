// go-machine/internal/types/tf_candles.go
package types

import (
	"strings"
	"time"
)

// ---- Timeframes ----

type TF string

const (
	TF1m  TF = "1m"
	TF5m  TF = "5m"
	TF15m TF = "15m"
	TF30m TF = "30m"
	TF1h  TF = "1h"
	TF4h  TF = "4h"
	TF1d  TF = "1d"
)

func (tf TF) String() string { return string(tf) }

func ParseTF(s string) (TF, bool) {
	switch strings.ToLower(s) {
	case "1m", "m1":
		return TF1m, true
	case "5m", "m5":
		return TF5m, true
	case "15m", "m15":
		return TF15m, true
	case "30m", "m30":
		return TF30m, true
	case "1h", "h1":
		return TF1h, true
	case "4h", "h4":
		return TF4h, true
	case "1d", "d1", "1day", "day":
		return TF1d, true
	default:
		return TF(""), false
	}
}

func (tf TF) Duration() time.Duration {
	switch tf {
	case TF1m:
		return time.Minute
	case TF5m:
		return 5 * time.Minute
	case TF15m:
		return 15 * time.Minute
	case TF30m:
		return 30 * time.Minute
	case TF1h:
		return time.Hour
	case TF4h:
		return 4 * time.Hour
	case TF1d:
		return 24 * time.Hour
	default:
		return 0
	}
}

// Align t down to the nearest tf boundary in the given location (or UTC if nil).
func Align(t time.Time, tf TF, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	d := tf.Duration()
	if d <= 0 {
		return t.In(loc)
	}
	tt := t.In(loc)
	switch tf {
	case TF1d:
		y, m, dday := tt.Date()
		return time.Date(y, m, dday, 0, 0, 0, 0, loc)
	default:
		epoch := time.Date(1970, 1, 1, 0, 0, 0, 0, loc)
		elapsed := tt.Sub(epoch)
		slots := elapsed / d
		return epoch.Add(slots * d)
	}
}

// Next boundary after t for tf (in the given location or UTC).
func NextBoundary(t time.Time, tf TF, loc *time.Location) time.Time {
	a := Align(t, tf, loc)
	if a.Equal(t.In(loc)) {
		return a.Add(tf.Duration())
	}
	return a.Add(tf.Duration())
}

// ---- Candle types ----

type Candle struct {
	T time.Time `json:"t"`
	O float64   `json:"O"`
	H float64   `json:"H"`
	L float64   `json:"L"`
	C float64   `json:"C"`
	V float64   `json:"V"`
}

type CandleSet struct {
	Symbol string   `json:"symbol"`
	TF     TF       `json:"tf"`
	Data   []Candle `json:"data"`
}

// EnsureSorted returns a copy sorted by time (ascending) if needed.
func EnsureSorted(cs []Candle) []Candle {
	if len(cs) < 2 {
		return cs
	}
	sorted := true
	for i := 1; i < len(cs); i++ {
		if cs[i].T.Before(cs[i-1].T) {
			sorted = false
			break
		}
	}
	if sorted {
		return cs
	}
	out := make([]Candle, len(cs))
	copy(out, cs)
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j].T.Before(out[j-1].T) {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

// SliceTail returns the last n candles (or all if n >= len).
func SliceTail(cs []Candle, n int) []Candle {
	if n <= 0 || n >= len(cs) {
		return cs
	}
	return cs[len(cs)-n:]
}
