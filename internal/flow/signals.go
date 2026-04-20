package flow

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type Trade struct {
	Time      time.Time
	Price     float64
	Size      float64
	IsBuyAgg  bool
	IsSellAgg bool
}

type BookLevel struct {
	Price float64
	Size  float64
}

type OrderBook struct {
	Bids []BookLevel
	Asks []BookLevel
}

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type FlowSnapshot struct {
	Delta                float64
	CumDelta             float64
	DeltaDivBull         bool
	DeltaDivBear         bool
	AbsorptionBull       bool
	AbsorptionBear       bool
	StackedImbalanceBull bool
	StackedImbalanceBear bool
	UnfinishedBusinessUp bool
	UnfinishedBusinessDn bool
	Confidence           float64
	Summary              string
}

func BuildFlowSnapshot(trades []Trade, book OrderBook, candles []Candle) FlowSnapshot {
	var buyAgg float64
	var sellAgg float64
	for _, t := range trades {
		if t.IsBuyAgg {
			buyAgg += t.Size
		}
		if t.IsSellAgg {
			sellAgg += t.Size
		}
	}
	delta := buyAgg - sellAgg
	cumDelta := delta
	stackedBull := detectStackedBullImbalance(book)
	stackedBear := detectStackedBearImbalance(book)
	absBull, absBear := detectAbsorption(trades, candles)
	divBull, divBear := detectDeltaDivergence(trades, candles)
	ubUp, ubDn := detectUnfinishedBusiness(candles)
	conf := computeConfidence(delta, stackedBull, stackedBear, absBull, absBear, divBull, divBear)
	return FlowSnapshot{
		Delta:                delta,
		CumDelta:             cumDelta,
		DeltaDivBull:         divBull,
		DeltaDivBear:         divBear,
		AbsorptionBull:       absBull,
		AbsorptionBear:       absBear,
		StackedImbalanceBull: stackedBull,
		StackedImbalanceBear: stackedBear,
		UnfinishedBusinessUp: ubUp,
		UnfinishedBusinessDn: ubDn,
		Confidence:           conf,
		Summary:              buildSummary(delta, stackedBull, stackedBear, absBull, absBear, divBull, divBear, ubUp, ubDn),
	}
}

func ConfirmLong(snap FlowSnapshot) (bool, []string) {
	var reasons []string
	if snap.StackedImbalanceBull {
		reasons = append(reasons, "stacked_imbalance_bull")
	}
	if snap.AbsorptionBull {
		reasons = append(reasons, "absorption_bull")
	}
	if snap.DeltaDivBull {
		reasons = append(reasons, "delta_div_bull")
	}
	ok := len(reasons) > 0 && snap.Confidence >= envFloatFlow("LIVE_FLOW_MIN_CONFIDENCE", 0.55)
	return ok, reasons
}

func ConfirmShort(snap FlowSnapshot) (bool, []string) {
	var reasons []string
	if snap.StackedImbalanceBear {
		reasons = append(reasons, "stacked_imbalance_bear")
	}
	if snap.AbsorptionBear {
		reasons = append(reasons, "absorption_bear")
	}
	if snap.DeltaDivBear {
		reasons = append(reasons, "delta_div_bear")
	}
	ok := len(reasons) > 0 && snap.Confidence >= envFloatFlow("LIVE_FLOW_MIN_CONFIDENCE", 0.55)
	return ok, reasons
}

func detectStackedBullImbalance(book OrderBook) bool {
	if len(book.Bids) < 2 {
		return false
	}
	count := 0
	for i := 0; i < len(book.Bids)-1; i++ {
		if book.Bids[i].Size > book.Bids[i+1].Size*1.5 {
			count++
		}
	}
	return count >= envIntFlow("LIVE_FLOW_STACKED_LEVELS_MIN", 2)
}

func detectStackedBearImbalance(book OrderBook) bool {
	if len(book.Asks) < 2 {
		return false
	}
	count := 0
	for i := 0; i < len(book.Asks)-1; i++ {
		if book.Asks[i].Size > book.Asks[i+1].Size*1.5 {
			count++
		}
	}
	return count >= envIntFlow("LIVE_FLOW_STACKED_LEVELS_MIN", 2)
}

func detectAbsorption(trades []Trade, candles []Candle) (bool, bool) {
	if len(trades) == 0 || len(candles) == 0 {
		return false, false
	}
	last := candles[len(candles)-1]
	body := math.Abs(last.Close - last.Open)
	rangeSize := math.Abs(last.High - last.Low)
	var buyAgg float64
	var sellAgg float64
	for _, t := range trades {
		if t.IsBuyAgg {
			buyAgg += t.Size
		}
		if t.IsSellAgg {
			sellAgg += t.Size
		}
	}
	absBull := sellAgg > buyAgg*1.2 && rangeSize > 0 && body < rangeSize*0.4 && last.Close >= (last.Low+last.High)/2.0
	absBear := buyAgg > sellAgg*1.2 && rangeSize > 0 && body < rangeSize*0.4 && last.Close <= (last.Low+last.High)/2.0
	return absBull, absBear
}

func detectDeltaDivergence(trades []Trade, candles []Candle) (bool, bool) {
	if len(trades) == 0 || len(candles) < 2 {
		return false, false
	}
	last := candles[len(candles)-1]
	prev := candles[len(candles)-2]
	var buyAgg float64
	var sellAgg float64
	for _, t := range trades {
		if t.IsBuyAgg {
			buyAgg += t.Size
		}
		if t.IsSellAgg {
			sellAgg += t.Size
		}
	}
	delta := buyAgg - sellAgg
	divBull := last.Low < prev.Low && delta > 0 && last.Close > last.Low
	divBear := last.High > prev.High && delta < 0 && last.Close < last.High
	return divBull, divBear
}

func detectUnfinishedBusiness(candles []Candle) (bool, bool) {
	if len(candles) < 2 {
		return false, false
	}
	last := candles[len(candles)-1]
	prev := candles[len(candles)-2]
	ubUp := math.Abs(last.High-prev.High) < 1e-9
	ubDn := math.Abs(last.Low-prev.Low) < 1e-9
	return ubUp, ubDn
}

func computeConfidence(delta float64, sb, sr, ab, ar, db, dr bool) float64 {
	conf := 0.0
	if math.Abs(delta) > 0 {
		conf += 0.20
	}
	if sb || sr {
		conf += 0.25
	}
	if ab || ar {
		conf += 0.25
	}
	if db || dr {
		conf += 0.20
	}
	if conf > 1.0 {
		conf = 1.0
	}
	return conf
}

func buildSummary(delta float64, sb, sr, ab, ar, db, dr, ubUp, ubDn bool) string {
	var parts []string
	parts = append(parts, "delta="+formatFloatFlow(delta))
	if sb {
		parts = append(parts, "stacked_bull")
	}
	if sr {
		parts = append(parts, "stacked_bear")
	}
	if ab {
		parts = append(parts, "abs_bull")
	}
	if ar {
		parts = append(parts, "abs_bear")
	}
	if db {
		parts = append(parts, "div_bull")
	}
	if dr {
		parts = append(parts, "div_bear")
	}
	if ubUp {
		parts = append(parts, "ub_up")
	}
	if ubDn {
		parts = append(parts, "ub_dn")
	}
	return strings.Join(parts, "|")
}

func formatFloatFlow(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func envFloatFlow(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envIntFlow(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

