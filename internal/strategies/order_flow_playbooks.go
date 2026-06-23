package strategies

import (
	"math"

	"go-machine/internal/features"
)

type VolumeClusters struct{}

func (s VolumeClusters) Name() string { return "volume_clusters" }

func (s VolumeClusters) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	if len(ctx.Candles) < 8 || len(snap.VP.HVNs) == 0 {
		return Signal{Name: s.Name()}
	}
	last := ctx.Candles[len(ctx.Candles)-1]
	level := nearestHVN(last.C, snap.VP.HVNs)
	if level <= 0 || math.Abs(relativePctOF(last.C, level)) > 0.20 {
		return Signal{Name: s.Name()}
	}
	side := trendSideFromSnapshot(snap)
	if side == "" {
		return Signal{Name: s.Name()}
	}
	if side == features.SideLong && snap.Flow.WhaleDelta1m <= 0 {
		return Signal{Name: s.Name()}
	}
	if side == features.SideShort && snap.Flow.WhaleDelta1m >= 0 {
		return Signal{Name: s.Name()}
	}
	entry, stop, ok := simpleDirectionalEntryStop(side, last, swingBound(ctx.Candles, side, 6))
	if !ok {
		return Signal{Name: s.Name()}
	}
	tp1, tp2 := rrTargets(entry, stop, side)
	return Signal{
		Active:       tp1 > 0,
		Name:         s.Name(),
		Side:         side,
		Entry:        entry,
		Stop:         stop,
		TP1:          tp1,
		TP2:          tp2,
		Confidence:   clamp01(0.60 + volumeSpikeBoost(snap.Flow.VolumeSpike)),
		Tags:         []string{"order_flow", "volume_cluster", "hvn_retest"},
		Invalidation: "lose cluster support_resistance",
		Ts:           last.Ts,
	}
}

type MultipleNodes struct{}

func (s MultipleNodes) Name() string { return "multiple_nodes" }

func (s MultipleNodes) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	if len(snap.VP.HVNs) < 2 {
		return Signal{Name: s.Name()}
	}
	last := snap.Candle
	if last.C <= 0 {
		return Signal{Name: s.Name()}
	}
	above, below := nearestAboveBelow(last.C, snap.VP.HVNs)
	if above <= 0 || below <= 0 {
		return Signal{Name: s.Name()}
	}
	widthPct := math.Abs((above - below) / last.C * 100.0)
	if widthPct > 1.5 {
		return Signal{Name: s.Name()}
	}
	side := trendSideFromSnapshot(snap)
	if side == "" {
		return Signal{Name: s.Name()}
	}
	entry, stop, ok := simpleDirectionalEntryStop(side, last, below)
	if side == features.SideShort {
		entry, stop, ok = simpleDirectionalEntryStop(side, last, above)
	}
	if !ok {
		return Signal{Name: s.Name()}
	}
	tp1, tp2 := rrTargets(entry, stop, side)
	return Signal{
		Active:       tp1 > 0,
		Name:         s.Name(),
		Side:         side,
		Entry:        entry,
		Stop:         stop,
		TP1:          tp1,
		TP2:          tp2,
		Confidence:   clamp01(0.58 + volumeSpikeBoost(snap.Flow.VolumeSpike)),
		Tags:         []string{"order_flow", "multiple_nodes", "rotation"},
		Invalidation: "lose node rotation band",
		Ts:           last.Ts,
	}
}

type TradesFilter struct{}

func (s TradesFilter) Name() string { return "trades_filter" }

func (s TradesFilter) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	if snap.Flow.LargeTradeCount1m < 3 || !snap.Flow.VolumeSpike {
		return Signal{Name: s.Name()}
	}
	last := snap.Candle
	side := features.SideLong
	if snap.Flow.WhaleDelta1m < 0 || snap.Flow.LargeSellCount1m > snap.Flow.LargeBuyCount1m {
		side = features.SideShort
	}
	entry, stop, ok := simpleDirectionalEntryStop(side, last, swingBound(ctx.Candles, side, 5))
	if !ok {
		return Signal{Name: s.Name()}
	}
	tp1, tp2 := rrTargets(entry, stop, side)
	return Signal{
		Active:       tp1 > 0,
		Name:         s.Name(),
		Side:         side,
		Entry:        entry,
		Stop:         stop,
		TP1:          tp1,
		TP2:          tp2,
		Confidence:   clamp01(0.57 + minFloat(0.18, math.Abs(snap.Flow.WhaleDelta1m)/maxFloat(math.Abs(snap.Flow.WhaleDeltaCum), 1)*0.25)),
		Tags:         []string{"order_flow", "trades_filter", "aggression"},
		Invalidation: "aggression fails to follow through",
		Ts:           last.Ts,
	}
}

type StackedImbalances struct{}

func (s StackedImbalances) Name() string { return "stacked_imbalances" }

func (s StackedImbalances) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	last := snap.Candle
	switch {
	case snap.Flow.StackedImbalanceBull:
		entry, stop, ok := simpleDirectionalEntryStop(features.SideLong, last, swingBound(ctx.Candles, features.SideLong, 4))
		if !ok {
			return Signal{Name: s.Name()}
		}
		tp1, tp2 := rrTargets(entry, stop, features.SideLong)
		return Signal{
			Active:       tp1 > 0,
			Name:         s.Name(),
			Side:         features.SideLong,
			Entry:        entry,
			Stop:         stop,
			TP1:          tp1,
			TP2:          tp2,
			Confidence:   clamp01(0.64 + volumeSpikeBoost(snap.Flow.VolumeSpike)),
			Tags:         []string{"order_flow", "stacked_imbalance", "continuation"},
			Invalidation: "stacked bids fail",
			Ts:           last.Ts,
		}
	case snap.Flow.StackedImbalanceBear:
		entry, stop, ok := simpleDirectionalEntryStop(features.SideShort, last, swingBound(ctx.Candles, features.SideShort, 4))
		if !ok {
			return Signal{Name: s.Name()}
		}
		tp1, tp2 := rrTargets(entry, stop, features.SideShort)
		return Signal{
			Active:       tp1 > 0,
			Name:         s.Name(),
			Side:         features.SideShort,
			Entry:        entry,
			Stop:         stop,
			TP1:          tp1,
			TP2:          tp2,
			Confidence:   clamp01(0.64 + volumeSpikeBoost(snap.Flow.VolumeSpike)),
			Tags:         []string{"order_flow", "stacked_imbalance", "continuation"},
			Invalidation: "stacked asks fail",
			Ts:           last.Ts,
		}
	default:
		return Signal{Name: s.Name()}
	}
}

type UnfinishedBusiness struct{}

func (s UnfinishedBusiness) Name() string { return "unfinished_business" }

func (s UnfinishedBusiness) Eval(ctx Context) Signal {
	snap := ctx.Snapshot
	last := snap.Candle
	switch {
	case snap.Flow.UnfinishedBusinessUp && snap.Flow.WhaleDelta1m > 0:
		entry, stop, ok := simpleDirectionalEntryStop(features.SideLong, last, swingBound(ctx.Candles, features.SideLong, 5))
		if !ok {
			return Signal{Name: s.Name()}
		}
		tp1, tp2 := rrTargets(entry, stop, features.SideLong)
		return Signal{
			Active:       tp1 > 0,
			Name:         s.Name(),
			Side:         features.SideLong,
			Entry:        entry,
			Stop:         stop,
			TP1:          tp1,
			TP2:          tp2,
			Confidence:   clamp01(0.56 + volumeSpikeBoost(snap.Flow.VolumeSpike)),
			Tags:         []string{"order_flow", "unfinished_business", "target_cleanup"},
			Invalidation: "unfinished auction resolves opposite",
			Ts:           last.Ts,
		}
	case snap.Flow.UnfinishedBusinessDn && snap.Flow.WhaleDelta1m < 0:
		entry, stop, ok := simpleDirectionalEntryStop(features.SideShort, last, swingBound(ctx.Candles, features.SideShort, 5))
		if !ok {
			return Signal{Name: s.Name()}
		}
		tp1, tp2 := rrTargets(entry, stop, features.SideShort)
		return Signal{
			Active:       tp1 > 0,
			Name:         s.Name(),
			Side:         features.SideShort,
			Entry:        entry,
			Stop:         stop,
			TP1:          tp1,
			TP2:          tp2,
			Confidence:   clamp01(0.56 + volumeSpikeBoost(snap.Flow.VolumeSpike)),
			Tags:         []string{"order_flow", "unfinished_business", "target_cleanup"},
			Invalidation: "unfinished auction resolves opposite",
			Ts:           last.Ts,
		}
	default:
		return Signal{Name: s.Name()}
	}
}

func trendSideFromSnapshot(snap features.Snapshot) features.Side {
	switch snap.Structure.Trend {
	case features.TrendBull:
		return features.SideLong
	case features.TrendBear:
		return features.SideShort
	default:
		if snap.Flow.WhaleDelta1m > 0 {
			return features.SideLong
		}
		if snap.Flow.WhaleDelta1m < 0 {
			return features.SideShort
		}
		return ""
	}
}

func simpleDirectionalEntryStop(side features.Side, last features.Candle, anchor float64) (float64, float64, bool) {
	entry := last.C
	if entry <= 0 || anchor <= 0 {
		return 0, 0, false
	}
	stop := anchor
	switch side {
	case features.SideLong:
		if stop >= entry {
			stop = last.L
		}
	case features.SideShort:
		if stop <= entry {
			stop = last.H
		}
	default:
		return 0, 0, false
	}
	if stop <= 0 || stop == entry {
		return 0, 0, false
	}
	return entry, stop, true
}

func swingBound(c []features.Candle, side features.Side, lookback int) float64 {
	if len(c) == 0 {
		return 0
	}
	if lookback <= 0 || lookback > len(c) {
		lookback = len(c)
	}
	start := len(c) - lookback
	if start < 0 {
		start = 0
	}
	bound := c[start].L
	if side == features.SideShort {
		bound = c[start].H
	}
	for i := start; i < len(c); i++ {
		if side == features.SideLong {
			if c[i].L < bound {
				bound = c[i].L
			}
			continue
		}
		if c[i].H > bound {
			bound = c[i].H
		}
	}
	return bound
}

func nearestHVN(price float64, hvns []features.PriceVolume) float64 {
	best := 0.0
	bestDist := math.MaxFloat64
	for _, hvn := range hvns {
		if hvn.Price <= 0 {
			continue
		}
		dist := math.Abs(hvn.Price - price)
		if dist < bestDist {
			best = hvn.Price
			bestDist = dist
		}
	}
	return best
}

func nearestAboveBelow(price float64, hvns []features.PriceVolume) (float64, float64) {
	above := 0.0
	below := 0.0
	aboveDist := math.MaxFloat64
	belowDist := math.MaxFloat64
	for _, hvn := range hvns {
		if hvn.Price <= 0 {
			continue
		}
		if hvn.Price >= price {
			dist := hvn.Price - price
			if dist < aboveDist {
				above = hvn.Price
				aboveDist = dist
			}
		}
		if hvn.Price <= price {
			dist := price - hvn.Price
			if dist < belowDist {
				below = hvn.Price
				belowDist = dist
			}
		}
	}
	return above, below
}

func volumeSpikeBoost(spike bool) float64 {
	if spike {
		return 0.08
	}
	return 0
}

func relativePctOF(px, anchor float64) float64 {
	if px <= 0 || anchor <= 0 {
		return 0
	}
	return ((px - anchor) / anchor) * 100.0
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
