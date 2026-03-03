package features

import "math"

type VolumeProfileConfig struct {
	Bins               int
	ValuePct           float64
	HVNTopN            int
	LVNTopN            int
	SignificanceMinPct float64
}

type VolumeProfileEngine struct {
	cfg VolumeProfileConfig
}

func NewVolumeProfileEngine(cfg VolumeProfileConfig) *VolumeProfileEngine {
	if cfg.Bins <= 0 {
		cfg.Bins = 64
	}
	if cfg.ValuePct <= 0 || cfg.ValuePct >= 1 {
		cfg.ValuePct = 0.70
	}
	if cfg.HVNTopN <= 0 {
		cfg.HVNTopN = 3
	}
	if cfg.LVNTopN <= 0 {
		cfg.LVNTopN = 3
	}
	if cfg.SignificanceMinPct <= 0 || cfg.SignificanceMinPct >= 1 {
		cfg.SignificanceMinPct = 0.10
	}
	return &VolumeProfileEngine{cfg: cfg}
}

func (e *VolumeProfileEngine) Eval(c []Candle) VolumeProfile {
	if len(c) == 0 {
		return VolumeProfile{}
	}
	minP, maxP := c[0].L, c[0].H
	for i := range c {
		if c[i].L < minP {
			minP = c[i].L
		}
		if c[i].H > maxP {
			maxP = c[i].H
		}
	}
	if minP <= 0 || maxP <= minP {
		return VolumeProfile{}
	}

	binsN := e.cfg.Bins
	width := (maxP - minP) / float64(binsN)
	if width <= 0 {
		return VolumeProfile{}
	}
	bins := make([]PriceVolume, binsN)
	for i := range bins {
		bins[i].Price = minP + (float64(i)+0.5)*width
	}

	total := 0.0
	for i := range c {
		vol := c[i].V
		if vol <= 0 {
			continue
		}
		lo, hi := c[i].L, c[i].H
		if hi <= lo {
			idx := clampInt(int((c[i].C-minP)/width), 0, binsN-1)
			bins[idx].Volume += vol
			total += vol
			continue
		}
		start := clampInt(int((lo-minP)/width), 0, binsN-1)
		end := clampInt(int((hi-minP)/width), 0, binsN-1)
		span := end - start + 1
		if span <= 0 {
			span = 1
		}
		alloc := vol / float64(span)
		for b := start; b <= end; b++ {
			bins[b].Volume += alloc
		}
		total += vol
	}
	if total <= 0 {
		return VolumeProfile{}
	}

	poc := 0
	for i := 1; i < binsN; i++ {
		if bins[i].Volume > bins[poc].Volume {
			poc = i
		}
	}

	target := total * e.cfg.ValuePct
	acc := bins[poc].Volume
	left, right := poc, poc
	for acc < target && (left > 0 || right < binsN-1) {
		lv, rv := -1.0, -1.0
		if left > 0 {
			lv = bins[left-1].Volume
		}
		if right < binsN-1 {
			rv = bins[right+1].Volume
		}
		if rv > lv {
			right++
			acc += bins[right].Volume
		} else if lv >= 0 {
			left--
			acc += bins[left].Volume
		} else {
			break
		}
	}

	hvns := topByVolume(bins, e.cfg.HVNTopN, true)
	lvns := topByVolume(bins, e.cfg.LVNTopN, false)
	last := c[len(c)-1].C
	pocPx := bins[poc].Price
	distBp := 0.0
	if pocPx > 0 {
		distBp = ((last - pocPx) / pocPx) * 10000.0
	}
	pocShare := 0.0
	if total > 0 {
		pocShare = bins[poc].Volume / total
	}
	vaWidthPct := 0.0
	if pocPx > 0 {
		vaWidthPct = ((bins[right].Price - bins[left].Price) / pocPx) * 100.0
	}
	shape := profileShape(bins, poc, total)
	nhAbove, nhBelow := nearestLevels(last, hvns)
	nlAbove, nlBelow := nearestLevels(last, lvns)
	firstOppDist := firstOpposingDistPct(last, bins, total, e.cfg.SignificanceMinPct)

	return VolumeProfile{
		POCPrice:                   pocPx,
		POCVolume:                  bins[poc].Volume,
		VAH:                        bins[right].Price,
		VAL:                        bins[left].Price,
		TotalVolume:                total,
		POCShare:                   pocShare,
		VAWidthPct:                 vaWidthPct,
		Shape:                      shape,
		HVNs:                       hvns,
		LVNs:                       lvns,
		Bins:                       bins,
		PriceMin:                   minP,
		PriceMax:                   maxP,
		NearestHVNAbove:            nhAbove,
		NearestHVNBelow:            nhBelow,
		NearestLVNAbove:            nlAbove,
		NearestLVNBelow:            nlBelow,
		FirstOpposingVolumeDistPct: firstOppDist,
		InValueArea:                last >= bins[left].Price && last <= bins[right].Price,
		DistToPOCBP:                distBp,
	}
}

func nearestLevels(px float64, levels []PriceVolume) (above, below float64) {
	if px <= 0 || len(levels) == 0 {
		return 0, 0
	}
	bestAbove := 0.0
	bestBelow := 0.0
	for _, lv := range levels {
		if lv.Price <= 0 {
			continue
		}
		if lv.Price >= px {
			if bestAbove == 0 || lv.Price < bestAbove {
				bestAbove = lv.Price
			}
		}
		if lv.Price <= px {
			if bestBelow == 0 || lv.Price > bestBelow {
				bestBelow = lv.Price
			}
		}
	}
	return bestAbove, bestBelow
}

func firstOpposingDistPct(last float64, bins []PriceVolume, total, minShare float64) float64 {
	if last <= 0 || len(bins) == 0 || total <= 0 {
		return 0
	}
	best := 0.0
	for _, b := range bins {
		if b.Price <= 0 || b.Volume <= 0 {
			continue
		}
		if (b.Volume / total) < minShare {
			continue
		}
		d := math.Abs((b.Price-last)/last) * 100.0
		if d <= 0 {
			continue
		}
		if best == 0 || d < best {
			best = d
		}
	}
	return best
}

func (v VolumeProfile) LevelAtHeaviestInRange(low, high float64) (float64, bool) {
	if len(v.Bins) == 0 {
		return 0, false
	}
	if high < low {
		low, high = high, low
	}
	bestVol := -1.0
	bestPx := 0.0
	for _, b := range v.Bins {
		if b.Price < low || b.Price > high {
			continue
		}
		if b.Volume > bestVol {
			bestVol = b.Volume
			bestPx = b.Price
		}
	}
	if bestVol <= 0 || bestPx <= 0 {
		return 0, false
	}
	return bestPx, true
}

func (v VolumeProfile) FirstSignificantOpposingLevel(entry float64, side Side, minShare float64) (float64, bool) {
	if entry <= 0 || len(v.Bins) == 0 || v.TotalVolume <= 0 {
		return 0, false
	}
	if minShare <= 0 || minShare >= 1 {
		minShare = 0.10
	}
	best := 0.0
	for _, b := range v.Bins {
		if b.Price <= 0 || b.Volume <= 0 {
			continue
		}
		if (b.Volume / v.TotalVolume) < minShare {
			continue
		}
		if side == SideLong {
			if b.Price <= entry {
				continue
			}
			if best == 0 || b.Price < best {
				best = b.Price
			}
		} else {
			if b.Price >= entry {
				continue
			}
			if best == 0 || b.Price > best {
				best = b.Price
			}
		}
	}
	if best > 0 {
		return best, true
	}
	if side == SideLong && v.VAH > entry {
		return v.VAH, true
	}
	if side == SideShort && v.VAL < entry && v.VAL > 0 {
		return v.VAL, true
	}
	return 0, false
}

func topByVolume(bins []PriceVolume, n int, desc bool) []PriceVolume {
	if n <= 0 || len(bins) == 0 {
		return nil
	}
	out := make([]PriceVolume, 0, n)
	used := make([]bool, len(bins))
	for len(out) < n {
		best := -1
		for i := range bins {
			if used[i] {
				continue
			}
			if best < 0 {
				best = i
				continue
			}
			if desc {
				if bins[i].Volume > bins[best].Volume {
					best = i
				}
			} else {
				if bins[i].Volume < bins[best].Volume {
					best = i
				}
			}
		}
		if best < 0 {
			break
		}
		used[best] = true
		if math.IsNaN(bins[best].Volume) || math.IsInf(bins[best].Volume, 0) {
			continue
		}
		out = append(out, bins[best])
	}
	return out
}

func profileShape(bins []PriceVolume, poc int, total float64) string {
	if len(bins) == 0 || poc < 0 || poc >= len(bins) || total <= 0 {
		return "UNKNOWN"
	}
	below := 0.0
	for i := 0; i < poc; i++ {
		below += bins[i].Volume
	}
	above := 0.0
	for i := poc + 1; i < len(bins); i++ {
		above += bins[i].Volume
	}
	skew := (above - below) / total
	switch {
	case math.Abs(skew) <= 0.10:
		return "D"
	case skew > 0:
		return "P"
	default:
		return "b"
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
