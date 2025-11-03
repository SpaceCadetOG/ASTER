package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/types"
)

// ---- Public HTTP handler ----

func VolStatsHandler(c *aster.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		symbol := q.Get("symbol")
		tfStr := q.Get("tf")
		n, _ := strconv.Atoi(q.Get("n"))
		if n <= 0 {
			n = 200
		}

		win, _ := strconv.Atoi(q.Get("win")) // window for moving stats/EMA
		if win <= 1 {
			win = 20
		}

		zmin, _ := strconv.ParseFloat(q.Get("zmin"), 64) // minimum z-score to flag spikes
		if zmin <= 0 {
			zmin = 2.0
		}

		vmin, _ := strconv.ParseFloat(q.Get("vmin"), 64) // minimum raw volume to show spike
		if vmin < 0 {
			vmin = 0
		}

		tf, ok := types.ParseTF(tfStr)
		if !ok {
			httpErrorJSON(w, http.StatusBadRequest, "invalid tf")
			return
		}

		// Load candles (with volume)
		bars, err := c.LoadCandles(symbol, tf, n)
		if err != nil {
			httpErrorJSON(w, http.StatusInternalServerError, "load candles: "+err.Error())
			return
		}
		if len(bars) == 0 {
			writeJSON(w, map[string]any{
				"symbol": symbol, "tf": tf, "count": 0,
				"vwap": 0, "emaV": 0, "spikes": []any{},
			})
			return
		}

		bars = types.EnsureSorted(bars)
		// compute metrics
		vwap := computeVWAP(bars)
		emaV := emaVolume(bars, win)
		mean, std := meanStdVolume(bars, win)

		spikes := detectVolumeSpikes(bars, mean, std, zmin, vmin)

		resp := map[string]any{
			"symbol": symbol,
			"tf":     tf.String(),
			"count":  len(bars),
			"window": win,
			"zmin":   zmin,
			"vmin":   vmin,
			"vwap":   vwap,
			"emaV":   emaV,
			"meanV":  mean,
			"stdV":   std,
			"spikes": spikes,
		}
		writeJSON(w, resp)
	}
}

// ---- Helpers (pure) ----

func httpErrorJSON(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

type volSpike struct {
	Idx  int       `json:"i"`
	Time time.Time `json:"t"`
	V    float64   `json:"v"`
	Z    float64   `json:"z"`
}

func computeVWAP(bars []types.Candle) float64 {
	var num, den float64
	for _, b := range bars {
		typ := (b.H + b.L + b.C) / 3.0
		num += typ * b.V
		den += b.V
	}
	if den == 0 {
		return 0
	}
	return num / den
}

func emaVolume(bars []types.Candle, win int) float64 {
	if len(bars) == 0 {
		return 0
	}
	alpha := 2.0 / (float64(win) + 1.0)
	ema := bars[0].V
	for i := 1; i < len(bars); i++ {
		ema = alpha*bars[i].V + (1.0-alpha)*ema
	}
	return ema
}

func meanStdVolume(bars []types.Candle, win int) (mean, std float64) {
	if len(bars) == 0 {
		return 0, 0
	}
	// use last 'win' samples (or all if smaller)
	start := 0
	if len(bars) > win {
		start = len(bars) - win
	}
	var sum float64
	n := float64(len(bars) - start)
	for i := start; i < len(bars); i++ {
		sum += bars[i].V
	}
	mean = sum / n
	var varsum float64
	for i := start; i < len(bars); i++ {
		d := bars[i].V - mean
		varsum += d * d
	}
	if n > 1 {
		std = math.Sqrt(varsum / (n - 1))
	}
	return
}

func detectVolumeSpikes(bars []types.Candle, mean, std, zmin, vmin float64) []volSpike {
	out := make([]volSpike, 0, 8)
	if std == 0 {
		return out
	}
	for i := range bars {
		z := (bars[i].V - mean) / std
		if z >= zmin && bars[i].V >= vmin {
			out = append(out, volSpike{
				Idx:  i,
				Time: bars[i].T,
				V:    bars[i].V,
				Z:    z,
			})
		}
	}
	return out
}
