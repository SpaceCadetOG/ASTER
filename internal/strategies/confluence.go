package strategies

import (
	"context"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-machine/internal/features"
)

const (
	confluenceMinScore       = 70.0
	confluenceWatchMin       = 70.0
	confluenceWatchMax       = 80.0
	confluenceAutoEntryMin   = 85.0
	hvnProximityPctThreshold = 0.005
)

type ConfluenceTier string

const (
	ConfluenceTierIgnore    ConfluenceTier = "ignore"
	ConfluenceTierWatchlist ConfluenceTier = "watchlist"
	ConfluenceTierAutoEntry ConfluenceTier = "auto_entry"
)

type FibImpulse struct {
	Side        features.Side
	StartIdx    int
	EndIdx      int
	StartPrice  float64
	EndPrice    float64
	Range       float64
	Level50     float64
	Level618    float64
	Level786    float64
	GoldenLow   float64
	GoldenHigh  float64
	ImpulseMove float64
}

type WeightedConfluence struct {
	Score       float64
	Trend       float64
	Volume      float64
	Fibonacci   float64
	OrderFlow   float64
	VWAP        float64
	StackedFlow float64
	Tier        ConfluenceTier
	Approved    bool
	Reason      string
	Impulse     FibImpulse
	Regime      string
	AutoMin     float64
	FundingPct  float64
}

type OrderFlowTrade struct {
	Price float64
	Qty   float64
	Side  string // BUY/SELL aggressor
	Ts    time.Time
}

type OrderBookLevel struct {
	Price   float64
	BidSize float64
	AskSize float64
}

type OrderFlowSignal struct {
	CumulativeDelta float64
	DeltaRising     bool
	HasStackedBuy   bool
	HasStackedSell  bool
	HasUnfinishedHi bool
	HasUnfinishedLo bool
	Ts              time.Time
}

type VolumeMap struct {
	levels map[float64]OrderBookLevel
	keys   []float64
}

func NewVolumeMap() *VolumeMap {
	return &VolumeMap{
		levels: make(map[float64]OrderBookLevel),
		keys:   make([]float64, 0, 64),
	}
}

func (v *VolumeMap) Update(levels []OrderBookLevel) {
	if v == nil {
		return
	}
	for _, lvl := range levels {
		if lvl.Price <= 0 {
			continue
		}
		if _, ok := v.levels[lvl.Price]; !ok {
			v.keys = append(v.keys, lvl.Price)
		}
		v.levels[lvl.Price] = lvl
	}
	sort.Float64s(v.keys)
}

func (v *VolumeMap) HasStackedImbalance(side features.Side, minConsecutive int, ratio float64) bool {
	if v == nil || minConsecutive <= 0 || ratio <= 0 || len(v.keys) == 0 {
		return false
	}
	run := 0
	for _, px := range v.keys {
		lvl := v.levels[px]
		if side == features.SideLong {
			ok := lvl.AskSize > 0 && (lvl.BidSize/lvl.AskSize) >= ratio
			if ok {
				run++
			} else {
				run = 0
			}
		} else {
			ok := lvl.BidSize > 0 && (lvl.AskSize/lvl.BidSize) >= ratio
			if ok {
				run++
			} else {
				run = 0
			}
		}
		if run >= minConsecutive {
			return true
		}
	}
	return false
}

type DeltaAccumulator struct {
	cum  float64
	prev float64
}

func (d *DeltaAccumulator) Add(tr OrderFlowTrade) {
	if d == nil {
		return
	}
	d.prev = d.cum
	switch strings.ToUpper(strings.TrimSpace(tr.Side)) {
	case "BUY", "B":
		d.cum += tr.Qty
	case "SELL", "S":
		d.cum -= tr.Qty
	}
}

func (d *DeltaAccumulator) Rising(side features.Side) bool {
	if d == nil {
		return false
	}
	if side == features.SideLong {
		return d.cum > 0 && d.cum > d.prev
	}
	return d.cum < 0 && d.cum < d.prev
}

func RunOrderFlowAggregator(
	ctx context.Context,
	trades <-chan OrderFlowTrade,
	l2Book <-chan []OrderBookLevel,
	out chan<- OrderFlowSignal,
	side features.Side,
) {
	vm := NewVolumeMap()
	var d DeltaAccumulator
	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-trades:
			if !ok {
				return
			}
			d.Add(t)
			out <- OrderFlowSignal{
				CumulativeDelta: d.cum,
				DeltaRising:     d.Rising(side),
				HasStackedBuy:   vm.HasStackedImbalance(features.SideLong, 3, 3.0),
				HasStackedSell:  vm.HasStackedImbalance(features.SideShort, 3, 3.0),
				Ts:              t.Ts,
			}
		case levels, ok := <-l2Book:
			if !ok {
				return
			}
			vm.Update(levels)
		}
	}
}

func CalculateConfluenceScore(ctx Context, side features.Side, flow OrderFlowSignal) WeightedConfluence {
	res := WeightedConfluence{
		Reason:  "below_min_score",
		Tier:    ConfluenceTierIgnore,
		AutoMin: confluenceAutoEntryMin,
	}
	c := ctx.Snapshot.Candle.C
	if c <= 0 && len(ctx.Candles) > 0 {
		c = ctx.Candles[len(ctx.Candles)-1].C
	}
	if c <= 0 {
		return res
	}
	ema8 := ema(ctx.Candles, 8)
	ema20 := ema(ctx.Candles, 20)
	imp, ok := detectImpulseAndFib(ctx.Candles, side, 96)
	res.Impulse = imp

	if trendAligned(c, ema8, ema20, side) {
		res.Trend = 20
	}
	if isNearHVN(c, ctx.Snapshot.VP, hvnProximityPctThreshold) {
		res.Volume = 25
	}
	if ok && inGoldenPocket(c, imp) {
		res.Fibonacci = 20
	}
	if orderFlowAligned(side, flow, ctx.Snapshot.Flow) {
		res.OrderFlow = 25
	}
	wvwap := weeklyVWAP(ctx.Candles, time.Now().UTC())
	if weeklyVWAPAligned(c, wvwap, side) {
		res.VWAP = 10
	}

	res.Score = res.Trend + res.Volume + res.Fibonacci + res.OrderFlow + res.VWAP
	if res.Score >= confluenceWatchMin && res.Score <= confluenceWatchMax {
		// Watchlist tier consumes L2/trades to check stacked imbalance boost.
		if (side == features.SideLong && flow.HasStackedBuy) || (side == features.SideShort && flow.HasStackedSell) {
			res.StackedFlow = 10
			res.Score += 10
		}
	}
	res.Score = clamp(res.Score, 0, 100)
	res.Regime = detectConfluenceRegime(ctx)
	res.FundingPct = normalizeFundingPct(ctx.FundingRate)
	res.AutoMin = requiredAutoEntryScore(ctx, side)
	res.Tier = scoreTierWithRequired(res.Score, res.AutoMin)
	res.Approved = res.Score >= confluenceMinScore
	if res.Tier == ConfluenceTierAutoEntry {
		res.Reason = "auto_entry"
	} else if res.Tier == ConfluenceTierWatchlist {
		res.Reason = "watchlist"
	}
	return res
}

func scoreTier(score float64) ConfluenceTier {
	return scoreTierWithRequired(score, confluenceAutoEntryMin)
}

type ConfluenceResult struct {
	Score   float64
	Reasons []string
}

func ScoreConfluenceForIntent(ctx StrategyContext, intent *EntryIntent) ConfluenceResult {
	if intent == nil {
		return ConfluenceResult{}
	}
	switch intent.Strategy {
	case StrategyImpulseContinuation:
		return scoreImpulseConfluence(ctx, intent)
	case StrategyAnchoredVWAPPullback:
		return scoreAVWAPConfluence(ctx, intent)
	case StrategyVPRetest:
		return scoreVPConfluence(ctx, intent)
	default:
		return scoreGenericConfluence(ctx, intent)
	}
}

func scoreImpulseConfluence(ctx StrategyContext, intent *EntryIntent) ConfluenceResult {
	score := 0.0
	var reasons []string
	if ctx.VolumeRatio >= envFloatCF("LIVE_CONF_IMPULSE_MIN_VOL_RATIO", 1.5) {
		score += 20
		reasons = append(reasons, "rvol_support")
	}
	if ctx.OIChangePct > 0 {
		score += 10
		reasons = append(reasons, "oi_expansion")
	}
	if ctx.Trend.Compression {
		score += 20
		reasons = append(reasons, "compression_quality")
	}
	if ctx.Trend.TF5mDir == "up" && intent.Side == SideLong {
		score += 15
		reasons = append(reasons, "tf5_align")
	}
	if ctx.Trend.TF15mDir == "up" && intent.Side == SideLong {
		score += 15
		reasons = append(reasons, "tf15_align")
	}
	if ctx.Trend.TF5mDir == "down" && intent.Side == SideShort {
		score += 15
		reasons = append(reasons, "tf5_align")
	}
	if ctx.Trend.TF15mDir == "down" && intent.Side == SideShort {
		score += 15
		reasons = append(reasons, "tf15_align")
	}
	if !ctx.VolumeProfile.HasResistance && intent.Side == SideLong {
		score += 10
		reasons = append(reasons, "no_near_overhead_vp_wall")
	}
	if !ctx.VolumeProfile.HasSupport && intent.Side == SideShort {
		score += 10
		reasons = append(reasons, "no_near_below_vp_wall")
	}
	return ConfluenceResult{Score: score, Reasons: reasons}
}

func scoreAVWAPConfluence(ctx StrategyContext, intent *EntryIntent) ConfluenceResult {
	score := 0.0
	var reasons []string
	if !ctx.AnchoredVWAP.Valid {
		return ConfluenceResult{}
	}
	if intent.Side == SideLong {
		if ctx.AnchoredVWAP.Slope >= envFloatCF("LIVE_CONF_AVWAP_MIN_SLOPE", 0.0) {
			score += 20
			reasons = append(reasons, "avwap_slope_up")
		}
		if ctx.MarkPrice >= ctx.AnchoredVWAP.Dev1Lower && ctx.MarkPrice <= ctx.AnchoredVWAP.VWAP*1.0025 {
			score += 20
			reasons = append(reasons, "avwap_pullback_zone")
		}
		if ctx.Trend.TF5mDir == "up" {
			score += 15
			reasons = append(reasons, "tf5_align")
		}
		if ctx.Trend.TF15mDir == "up" {
			score += 15
			reasons = append(reasons, "tf15_align")
		}
		if ctx.MarkPrice >= ctx.WeeklyVWAP && ctx.WeeklyVWAP > 0 {
			score += 10
			reasons = append(reasons, "weekly_vwap_align")
		}
		if ctx.Flow.AbsorptionBull || ctx.Flow.StackedImbalanceBull || ctx.Flow.DeltaDivBull {
			score += 15
			reasons = append(reasons, "flow_confirmation")
		}
	}
	if intent.Side == SideShort {
		if ctx.AnchoredVWAP.Slope <= -envFloatCF("LIVE_CONF_AVWAP_MIN_SLOPE", 0.0) {
			score += 20
			reasons = append(reasons, "avwap_slope_down")
		}
		if ctx.MarkPrice <= ctx.AnchoredVWAP.Dev1Upper && ctx.MarkPrice >= ctx.AnchoredVWAP.VWAP*0.9975 {
			score += 20
			reasons = append(reasons, "avwap_pullback_zone")
		}
		if ctx.Trend.TF5mDir == "down" {
			score += 15
			reasons = append(reasons, "tf5_align")
		}
		if ctx.Trend.TF15mDir == "down" {
			score += 15
			reasons = append(reasons, "tf15_align")
		}
		if ctx.MarkPrice <= ctx.WeeklyVWAP && ctx.WeeklyVWAP > 0 {
			score += 10
			reasons = append(reasons, "weekly_vwap_align")
		}
		if ctx.Flow.AbsorptionBear || ctx.Flow.StackedImbalanceBear || ctx.Flow.DeltaDivBear {
			score += 15
			reasons = append(reasons, "flow_confirmation")
		}
	}
	return ConfluenceResult{Score: score, Reasons: reasons}
}

func scoreVPConfluence(ctx StrategyContext, intent *EntryIntent) ConfluenceResult {
	score := 0.0
	var reasons []string
	if ctx.VolumeProfile.POC > 0 {
		score += 15
		reasons = append(reasons, "vp_zone_present")
	}
	if ctx.VolumeProfile.WidthBps > 0 && ctx.VolumeProfile.WidthBps <= envFloatCF("LIVE_STRAT_VP_ZONE_WIDTH_BPS_MAX", 120.0) {
		score += 10
		reasons = append(reasons, "vp_zone_width_ok")
	}
	if intent.Side == SideLong && ctx.Trend.TF15mDir == "up" {
		score += 15
		reasons = append(reasons, "tf15_align")
	}
	if intent.Side == SideShort && ctx.Trend.TF15mDir == "down" {
		score += 15
		reasons = append(reasons, "tf15_align")
	}
	if ctx.Flow.Confidence >= envFloatCF("LIVE_CONF_VP_FLOW_MIN_CONF", 0.55) {
		score += 15
		reasons = append(reasons, "flow_support")
	}
	if intent.Side == SideLong && (ctx.Flow.AbsorptionBull || ctx.Flow.StackedImbalanceBull || ctx.Flow.DeltaDivBull) {
		score += 15
		reasons = append(reasons, "bull_confirmation")
	}
	if intent.Side == SideShort && (ctx.Flow.AbsorptionBear || ctx.Flow.StackedImbalanceBear || ctx.Flow.DeltaDivBear) {
		score += 15
		reasons = append(reasons, "bear_confirmation")
	}
	if ctx.VolumeRatio >= 1.0 {
		score += 10
		reasons = append(reasons, "volume_ratio_support")
	}
	return ConfluenceResult{Score: score, Reasons: reasons}
}

func scoreGenericConfluence(ctx StrategyContext, intent *EntryIntent) ConfluenceResult {
	score := 0.0
	var reasons []string
	if ctx.CandidateScore > 0 {
		score += ctx.CandidateScore * 0.25
		reasons = append(reasons, "candidate_score")
	}
	if ctx.VolumeRatio > 1.0 {
		score += 10
		reasons = append(reasons, "volume_ratio")
	}
	return ConfluenceResult{Score: score, Reasons: reasons}
}

func envFloatCF(key string, def float64) float64 {
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

func scoreTierWithRequired(score, autoEntryMin float64) ConfluenceTier {
	switch {
	case score >= autoEntryMin:
		return ConfluenceTierAutoEntry
	case score >= confluenceWatchMin:
		return ConfluenceTierWatchlist
	default:
		return ConfluenceTierIgnore
	}
}

func detectConfluenceRegime(ctx Context) string {
	switch ctx.Snapshot.Structure.Trend {
	case features.TrendRange:
		return "rotating"
	case features.TrendBull, features.TrendBear:
		return "trending"
	default:
		return "unknown"
	}
}

func requiredAutoEntryScore(ctx Context, side features.Side) float64 {
	autoMin := confluenceAutoEntryMin
	crowdedMin := 90.0
	fundingThresholdPct := 0.10
	fundingPct := normalizeFundingPct(ctx.FundingRate)
	if side == features.SideLong && fundingPct > fundingThresholdPct {
		return crowdedMin
	}
	if side == features.SideShort && fundingPct < -fundingThresholdPct {
		return crowdedMin
	}
	return autoMin
}

func normalizeFundingPct(rate float64) float64 {
	if math.Abs(rate) <= 0.01 {
		return rate * 100.0
	}
	return rate
}

func trendAligned(price, ema8, ema20 float64, side features.Side) bool {
	if price <= 0 || ema8 <= 0 || ema20 <= 0 {
		return false
	}
	if side == features.SideLong {
		return ema8 > ema20 && price > ema8
	}
	return ema8 < ema20 && price < ema8
}

func weeklyVWAPAligned(price, weeklyVWAP float64, side features.Side) bool {
	if price <= 0 || weeklyVWAP <= 0 {
		return false
	}
	if side == features.SideLong {
		return price > weeklyVWAP
	}
	return price < weeklyVWAP
}

func orderFlowAligned(side features.Side, flow OrderFlowSignal, snap features.FlowState) bool {
	if side == features.SideLong {
		return (flow.CumulativeDelta > 0 && flow.DeltaRising) ||
			(snap.WhaleDeltaCum > 0 && snap.WhaleDelta1m > 0)
	}
	return (flow.CumulativeDelta < 0 && flow.DeltaRising) ||
		(snap.WhaleDeltaCum < 0 && snap.WhaleDelta1m < 0)
}

func isNearHVN(price float64, vp features.VolumeProfile, tol float64) bool {
	if price <= 0 || tol <= 0 {
		return false
	}
	check := func(level float64) bool {
		return level > 0 && math.Abs((price-level)/price) <= tol
	}
	if check(vp.NearestHVNAbove) || check(vp.NearestHVNBelow) {
		return true
	}
	for _, n := range vp.HVNs {
		if check(n.Price) {
			return true
		}
	}
	return false
}

func inGoldenPocket(price float64, imp FibImpulse) bool {
	if price <= 0 || imp.GoldenLow <= 0 || imp.GoldenHigh <= 0 {
		return false
	}
	lo := math.Min(imp.GoldenLow, imp.GoldenHigh)
	hi := math.Max(imp.GoldenLow, imp.GoldenHigh)
	return price >= lo && price <= hi
}

func ema(c []features.Candle, n int) float64 {
	if n <= 1 || len(c) == 0 {
		return 0
	}
	k := 2.0 / float64(n+1)
	start := c[0].C
	if start <= 0 {
		return 0
	}
	out := start
	for i := 1; i < len(c); i++ {
		if c[i].C <= 0 {
			continue
		}
		out = c[i].C*k + out*(1-k)
	}
	return out
}

func weeklyVWAP(c []features.Candle, now time.Time) float64 {
	if len(c) == 0 {
		return 0
	}
	cutoff := now.Add(-7 * 24 * time.Hour)
	var pv, vv float64
	for _, x := range c {
		if x.Ts.IsZero() || x.Ts.Before(cutoff) {
			continue
		}
		if x.C <= 0 || x.V <= 0 {
			continue
		}
		pv += x.C * x.V
		vv += x.V
	}
	if vv <= 0 {
		return 0
	}
	return pv / vv
}

func detectImpulseAndFib(c []features.Candle, side features.Side, lookback int) (FibImpulse, bool) {
	if len(c) < 4 {
		return FibImpulse{}, false
	}
	start := len(c) - lookback
	if start < 0 {
		start = 0
	}
	bestMove := 0.0
	best := FibImpulse{Side: side}
	if side == features.SideLong {
		for i := start; i < len(c)-1; i++ {
			lo := c[i].L
			if lo <= 0 {
				continue
			}
			hi := lo
			hiIdx := i
			for j := i + 1; j < len(c); j++ {
				if c[j].H > hi {
					hi = c[j].H
					hiIdx = j
				}
			}
			if hiIdx <= i || hi <= lo {
				continue
			}
			move := (hi - lo) / lo
			if move > bestMove {
				bestMove = move
				best = buildFibImpulse(side, i, hiIdx, lo, hi)
			}
		}
	} else {
		for i := start; i < len(c)-1; i++ {
			hi := c[i].H
			if hi <= 0 {
				continue
			}
			lo := hi
			loIdx := i
			for j := i + 1; j < len(c); j++ {
				if c[j].L > 0 && c[j].L < lo {
					lo = c[j].L
					loIdx = j
				}
			}
			if loIdx <= i || hi <= lo {
				continue
			}
			move := (hi - lo) / hi
			if move > bestMove {
				bestMove = move
				best = buildFibImpulse(side, i, loIdx, hi, lo)
			}
		}
	}
	if bestMove <= 0 {
		return FibImpulse{}, false
	}
	best.ImpulseMove = bestMove
	return best, true
}

// DetectImpulseAndFib exposes impulse + Fibonacci levels for execution modules.
func DetectImpulseAndFib(c []features.Candle, side features.Side, lookback int) (FibImpulse, bool) {
	return detectImpulseAndFib(c, side, lookback)
}

func buildFibImpulse(side features.Side, startIdx, endIdx int, startPrice, endPrice float64) FibImpulse {
	imp := FibImpulse{
		Side:       side,
		StartIdx:   startIdx,
		EndIdx:     endIdx,
		StartPrice: startPrice,
		EndPrice:   endPrice,
		Range:      math.Abs(endPrice - startPrice),
	}
	if imp.Range <= 0 {
		return imp
	}
	if side == features.SideLong {
		imp.Level50 = endPrice - imp.Range*0.5
		imp.Level618 = endPrice - imp.Range*0.618
		imp.Level786 = endPrice - imp.Range*0.786
	} else {
		imp.Level50 = endPrice + imp.Range*0.5
		imp.Level618 = endPrice + imp.Range*0.618
		imp.Level786 = endPrice + imp.Range*0.786
	}
	imp.GoldenLow = math.Min(imp.Level50, imp.Level618)
	imp.GoldenHigh = math.Max(imp.Level50, imp.Level618)
	return imp
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
