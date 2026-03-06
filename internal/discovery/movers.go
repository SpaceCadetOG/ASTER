package discovery

import (
	"math"
	"sort"

	"go-machine/internal/market"
	"go-machine/internal/types"
)

// Snapshot is a discovery-ready view per symbol.
type Snapshot struct {
	Symbol      string
	Change24h   float64
	VolumeUSD   float64
	Volatility  float64
	VolumeRatio float64
	Score       float64
}

// Config controls hot-token universe selection.
type Config struct {
	Enabled         bool
	TopN            int
	MinVolumeRatio  float64
	MinVolatility   float64
	LookbackMinutes int
	RefreshSeconds  int
}

func DefaultConfig() Config {
	return Config{Enabled: true, TopN: 10, MinVolumeRatio: 1.5, MinVolatility: 0, LookbackMinutes: 60, RefreshSeconds: 60}
}

func BuildSnapshots(rows []market.Scored, candles map[string][]types.Candle) []Snapshot {
	out := make([]Snapshot, 0, len(rows))
	for _, r := range rows {
		sym := normalizeSymbol(r.Symbol)
		cs := candles[sym]
		vr, _ := VolumeRatio(cs, 20)
		vol := Volatility(cs, 20)
		out = append(out, Snapshot{
			Symbol:      sym,
			Change24h:   r.Change24h,
			VolumeUSD:   r.VolumeUSD,
			Volatility:  vol,
			VolumeRatio: vr,
			Score:       compositeScore(r.Change24h, r.VolumeUSD, vol, vr),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].VolumeUSD > out[j].VolumeUSD
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func compositeScore(chg, volUSD, volty, volRatio float64) float64 {
	if volUSD < 0 {
		volUSD = 0
	}
	v := math.Log10(math.Max(volUSD, 1))
	return (math.Abs(chg) * 0.8) + (v * 1.5) + (volty * 120) + (volRatio * 2.0)
}
