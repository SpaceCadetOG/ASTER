package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/data"
	"go-machine/internal/inplay"
	"go-machine/internal/risk"
	"go-machine/internal/stats"
)

type triggerState string

const (
	triggerOFAbsorb    triggerState = "OF_ABSORB"
	triggerOFReclaim   triggerState = "OF_RECLAIM"
	triggerStackedBid  triggerState = "OF_STACKED_BID"
	triggerStackedAsk  triggerState = "OF_STACKED_ASK"
	triggerDeltaFlip   triggerState = "OF_DELTA_FLIP"
	triggerExhaustion  triggerState = "OF_EXHAUSTION"
	triggerImpulseCont triggerState = "OF_IMPULSE_CONT"
	triggerFailReclaim triggerState = "OF_FAIL_RECLAIM"
	triggerNone        triggerState = "OF_NONE"
)

type missedOpportunity struct {
	Symbol       string
	Side         string
	Session      string
	Rank         float64
	Discovery    float64
	Trigger      float64
	Execution    float64
	Combined     float64
	TriggerState string
	Category     string
	Reason       string
	Entry        float64
	CreatedAt    time.Time
	SeenPullback bool
	Forward1m    float64
	Forward3m    float64
	Forward5m    float64
	Forward15m   float64
	MaxForward   float64
	Emitted      bool
}

type missedTracker struct {
	items map[string]*missedOpportunity
}

func newMissedTracker() *missedTracker {
	return &missedTracker{items: map[string]*missedOpportunity{}}
}

func missedKey(symbol, side string, ts time.Time) string {
	return strings.ToUpper(strings.TrimSpace(symbol)) + "|" + strings.ToUpper(strings.TrimSpace(side)) + "|" + ts.UTC().Format(time.RFC3339)
}

func (t *missedTracker) Observe(now time.Time, c candidate, reason string) {
	if t == nil || strings.TrimSpace(reason) == "" {
		return
	}
	if c.DiscoveryScore < envFloat("LIVE_MISS_TRACK_MIN_DISCOVERY", 0.72) && c.CombinedScore < envFloat("LIVE_MISS_TRACK_MIN_COMBINED", 0.62) {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if raw == "" || c.LastClose <= 0 {
		return
	}
	key := missedKey(raw, c.Side, now)
	t.items[key] = &missedOpportunity{
		Symbol:       raw,
		Side:         strings.ToUpper(strings.TrimSpace(c.Side)),
		Session:      string(data.CurrentRegimeCT(now)),
		Rank:         c.Entry.Rank,
		Discovery:    c.DiscoveryScore,
		Trigger:      c.TriggerScore,
		Execution:    c.ExecutionScore,
		Combined:     c.CombinedScore,
		TriggerState: c.TriggerState,
		Category:     categorizeMissReason(reason),
		Reason:       reason,
		Entry:        c.LastClose,
		CreatedAt:    now,
	}
}

func (t *missedTracker) Update(now time.Time, meta map[string]symbolMeta, longCurrent, shortCurrent map[string]inplay.Entry, log *stats.EventLogger) {
	if t == nil || len(t.items) == 0 {
		return
	}
	for key, item := range t.items {
		m := meta[item.Symbol]
		px := m.LastPrice
		if px <= 0 || item.Entry <= 0 {
			continue
		}
		fwd := forwardExcursionPct(item.Side, item.Entry, px)
		if fwd > item.MaxForward {
			item.MaxForward = fwd
		}
		age := now.Sub(item.CreatedAt)
		if !item.SeenPullback {
			item.SeenPullback = alternatePullbackExists(item, longCurrent, shortCurrent)
		}
		if age >= time.Minute && item.Forward1m == 0 {
			item.Forward1m = fwd
		}
		if age >= 3*time.Minute && item.Forward3m == 0 {
			item.Forward3m = fwd
		}
		if age >= 5*time.Minute && item.Forward5m == 0 {
			item.Forward5m = fwd
		}
		if age >= 15*time.Minute && !item.Emitted {
			item.Forward15m = fwd
			item.Emitted = true
			if log != nil {
				log.Emit(stats.Event{
					Timestamp:    now,
					Type:         "MISSED_OPPORTUNITY",
					Symbol:       item.Symbol,
					Side:         item.Side,
					TF:           "1m",
					TriggerState: item.TriggerState,
					Score:        item.Rank,
					Discovery:    item.Discovery,
					Trigger:      item.Trigger,
					Execution:    item.Execution,
					Combined:     item.Combined,
					MissCategory: item.Category,
					EntryPx:      item.Entry,
					ExitPx:       px,
					PnLPct:       item.Forward15m,
					Reason:       fmt.Sprintf("%s|max=%.2f|pullback=%v|fwd1=%.2f|fwd3=%.2f|fwd5=%.2f", item.Reason, item.MaxForward, item.SeenPullback, item.Forward1m, item.Forward3m, item.Forward5m),
				})
			}
			delete(t.items, key)
		}
	}
}

func forwardExcursionPct(side string, entry, px float64) float64 {
	if entry <= 0 || px <= 0 {
		return 0
	}
	if strings.EqualFold(side, "BUY") {
		return ((px - entry) / entry) * 100.0
	}
	return ((entry - px) / entry) * 100.0
}

func alternatePullbackExists(item *missedOpportunity, longCurrent, shortCurrent map[string]inplay.Entry) bool {
	if item == nil {
		return false
	}
	var cur inplay.Entry
	var ok bool
	if strings.EqualFold(item.Side, "BUY") {
		cur, ok = longCurrent[item.Symbol]
	} else {
		cur, ok = shortCurrent[item.Symbol]
	}
	if !ok {
		return false
	}
	switch cur.State {
	case inplay.StateHeating, inplay.StateInPlay, inplay.StateBalanced:
		return cur.ScoreSlope >= envFloat("LIVE_PULLBACK_RECHECK_MIN_SLOPE", 0.02)
	default:
		return false
	}
}

func deriveTriggerState(c candidate) (string, float64, []string) {
	if c.LastClose <= 0 {
		return string(triggerNone), 0.10, []string{"no_last_close"}
	}
	extVWAP := math.Abs(relativePct(c.LastClose, c.SessionVWAP))
	extEMA := math.Abs(relativePct(c.LastClose, c.EMA9))
	stackedBid := c.BookImbalance >= envFloat("LIVE_OF_STACKED_BID_IMB", 1.08)
	stackedAsk := c.BookImbalance > 0 && c.BookImbalance <= envFloat("LIVE_OF_STACKED_ASK_IMB", 0.92)
	spreadTight := c.SpreadBps > 0 && c.SpreadBps <= envFloat("LIVE_OF_MAX_SPREAD_BPS", 18.0)
	impulseSlope := c.Entry.ScoreSlope >= envFloat("LIVE_OF_IMPULSE_MIN_SLOPE", 0.10)
	contSlope := c.Entry.ScoreSlope >= envFloat("LIVE_OF_CONT_MIN_SLOPE", 0.03)
	shortSlope := c.Entry.ScoreSlope <= -envFloat("LIVE_OF_CONT_MIN_SLOPE", 0.03)
	pullbackBias := envBool("LIVE_PULLBACK_ENTRY_BIAS", true)
	maxExtVWAP := envFloat("LIVE_MAX_EXTENSION_FROM_VWAP_PCT", 1.25)
	maxExtEMA := envFloat("LIVE_MAX_EXTENSION_FROM_EMA_PCT", 1.00)
	reasons := []string{}

	if strings.EqualFold(c.Side, "BUY") {
		if c.WallMode == "wall_defense" && c.WallSide == "bid" && c.WallConfidence >= envFloat("LIVE_WALL_DEFENSE_MIN_CONF", 0.55) {
			return string(triggerOFAbsorb), clamp(0.72+c.WallConfidence*0.18-c.WallSpoofRisk*0.20, 0, 0.92), append([]string{"wall_defense"}, c.WallReasons...)
		}
		if c.WallMode == "wall_consumption" && c.WallSide == "ask" && c.WallConfidence >= envFloat("LIVE_WALL_CONSUMPTION_MIN_CONF", 0.45) {
			return string(triggerImpulseCont), clamp(0.72+c.WallConfidence*0.16-c.WallSpoofRisk*0.15, 0, 0.92), append([]string{"wall_consumption"}, c.WallReasons...)
		}
		if c.WallMode == "wall_failure" && c.WallSide == "bid" {
			return string(triggerFailReclaim), clamp(0.18+c.WallConfidence*0.18, 0, 0.50), append([]string{"wall_failure"}, c.WallReasons...)
		}
		if spreadTight && extVWAP <= maxExtVWAP && extEMA <= maxExtEMA && c.OFIZ >= envFloat("LIVE_OF_RECLAIM_MIN_OFI_Z", 0.45) && c.LastClose >= c.SessionVWAP && c.LastClose >= c.EMA9 {
			return string(triggerOFReclaim), 0.92, []string{"vwap_reclaim", "ema_hold", fmt.Sprintf("ofi_z=%.2f", c.OFIZ)}
		}
		if spreadTight && stackedBid && c.OFIZ >= envFloat("LIVE_OF_STACK_MIN_OFI_Z", 0.35) && (c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay) {
			return string(triggerStackedBid), 0.84, []string{"stacked_bid", fmt.Sprintf("imb=%.2f", c.BookImbalance)}
		}
		if impulseSlope && c.OFIZ >= envFloat("LIVE_OF_IMPULSE_MIN_OFI_Z", 0.65) && extVWAP <= maxExtVWAP*1.4 {
			return string(triggerImpulseCont), 0.86, []string{"impulse_cont", fmt.Sprintf("slope=%.3f", c.Entry.ScoreSlope)}
		}
		if c.Entry.ReversalWatchFlag && c.OFIZ <= -envFloat("LIVE_OF_EXHAUSTION_MIN_OFI_Z", 0.55) {
			return string(triggerExhaustion), 0.32, []string{"exhaustion_watch"}
		}
		if pullbackBias && (extVWAP > maxExtVWAP || extEMA > maxExtEMA) {
			reasons = append(reasons, fmt.Sprintf("extended_vwap=%.2f", extVWAP), fmt.Sprintf("extended_ema=%.2f", extEMA))
			return string(triggerExhaustion), 0.28, reasons
		}
		if c.OFIZ <= -envFloat("LIVE_OF_FAIL_RECLAIM_Z", 0.20) && c.LastClose < c.SessionVWAP {
			return string(triggerFailReclaim), 0.18, []string{"fail_reclaim"}
		}
		if contSlope {
			return string(triggerDeltaFlip), 0.58, []string{"delta_flip"}
		}
		return string(triggerNone), 0.20, []string{"trigger_not_ready"}
	}

	if c.WallMode == "wall_defense" && c.WallSide == "ask" && c.WallConfidence >= envFloat("LIVE_WALL_DEFENSE_MIN_CONF", 0.55) {
		return string(triggerOFAbsorb), clamp(0.72+c.WallConfidence*0.18-c.WallSpoofRisk*0.20, 0, 0.92), append([]string{"wall_defense_short"}, c.WallReasons...)
	}
	if c.WallMode == "wall_consumption" && c.WallSide == "bid" && c.WallConfidence >= envFloat("LIVE_WALL_CONSUMPTION_MIN_CONF", 0.45) {
		return string(triggerImpulseCont), clamp(0.72+c.WallConfidence*0.16-c.WallSpoofRisk*0.15, 0, 0.92), append([]string{"wall_consumption_short"}, c.WallReasons...)
	}
	if c.WallMode == "wall_failure" && c.WallSide == "ask" {
		return string(triggerFailReclaim), clamp(0.18+c.WallConfidence*0.18, 0, 0.50), append([]string{"wall_failure_short"}, c.WallReasons...)
	}
	if spreadTight && extVWAP <= maxExtVWAP && extEMA <= maxExtEMA && c.OFIZ <= -envFloat("LIVE_OF_RECLAIM_MIN_OFI_Z", 0.45) && c.LastClose <= c.SessionVWAP && c.LastClose <= c.EMA9 {
		return string(triggerOFReclaim), 0.92, []string{"vwap_reclaim_short", "ema_hold_short", fmt.Sprintf("ofi_z=%.2f", c.OFIZ)}
	}
	if spreadTight && stackedAsk && c.OFIZ <= -envFloat("LIVE_OF_STACK_MIN_OFI_Z", 0.35) && (c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay) {
		return string(triggerStackedAsk), 0.84, []string{"stacked_ask", fmt.Sprintf("imb=%.2f", c.BookImbalance)}
	}
	if shortSlope && c.OFIZ <= -envFloat("LIVE_OF_IMPULSE_MIN_OFI_Z", 0.65) && extVWAP <= maxExtVWAP*1.4 {
		return string(triggerImpulseCont), 0.86, []string{"impulse_cont_short", fmt.Sprintf("slope=%.3f", c.Entry.ScoreSlope)}
	}
	if c.Entry.ReversalWatchFlag && c.OFIZ >= envFloat("LIVE_OF_EXHAUSTION_MIN_OFI_Z", 0.55) {
		return string(triggerExhaustion), 0.32, []string{"exhaustion_watch_short"}
	}
	if pullbackBias && (extVWAP > maxExtVWAP || extEMA > maxExtEMA) {
		reasons = append(reasons, fmt.Sprintf("extended_vwap=%.2f", extVWAP), fmt.Sprintf("extended_ema=%.2f", extEMA))
		return string(triggerExhaustion), 0.28, reasons
	}
	if c.OFIZ >= envFloat("LIVE_OF_FAIL_RECLAIM_Z", 0.20) && c.LastClose > c.SessionVWAP {
		return string(triggerFailReclaim), 0.18, []string{"fail_reclaim_short"}
	}
	if shortSlope {
		return string(triggerDeltaFlip), 0.58, []string{"delta_flip_short"}
	}
	return string(triggerNone), 0.20, []string{"trigger_not_ready"}
}

func relativePct(px, anchor float64) float64 {
	if px <= 0 || anchor <= 0 {
		return 0
	}
	return ((px - anchor) / anchor) * 100.0
}

func chooseExitProfile(c candidate) string {
	switch c.SetupFamily {
	case "reset_impulse_breakout":
		return "IMPULSE"
	case "micro_pullback_continuation", "breakout_retest", "deep_pullback_reclaim", "reversal_exhaustion":
		return "ROTATION"
	}
	if c.TriggerState == string(triggerImpulseCont) || c.Entry.Momentum && c.VolumeRatio >= envFloat("LIVE_EXIT_IMPULSE_MIN_VOL_RATIO", 1.40) {
		return "IMPULSE"
	}
	return "ROTATION"
}

func profileTargetRs(c candidate, base1, base2, base3 float64) (string, float64, float64, float64) {
	profile := chooseExitProfile(c)
	if profile == "IMPULSE" {
		return profile,
			envFloat("LIVE_IMPULSE_TP1_R", maxFloat(base1, 1.1)),
			envFloat("LIVE_IMPULSE_TP2_R", maxFloat(base2, 2.6)),
			envFloat("LIVE_IMPULSE_TP3_R", maxFloat(base3, 4.2))
	}
	rotationTP1 := minPositive(base1, 0.9)
	rotationTP2 := minPositive(base2, 1.8)
	rotationTP3 := minPositive(base3, 2.8)
	return profile,
		envFloat("LIVE_ROTATION_TP1_R", rotationTP1),
		envFloat("LIVE_ROTATION_TP2_R", rotationTP2),
		envFloat("LIVE_ROTATION_TP3_R", rotationTP3)
}

func computeDynamicTargetLadder(c candidate, entry, stopDist, base1, base2, base3 float64) (string, float64, float64, float64) {
	profile, tp1R, tp2R, tp3R := profileTargetRs(c, base1, base2, base3)
	if entry <= 0 || stopDist <= 0 {
		return profile, entry, entry, entry
	}
	sideBuy := strings.EqualFold(c.Side, "BUY")
	base := []float64{
		targetPriceForR(sideBuy, entry, stopDist, tp1R),
		targetPriceForR(sideBuy, entry, stopDist, tp2R),
		targetPriceForR(sideBuy, entry, stopDist, tp3R),
	}
	cands := append([]float64{}, base...)
	useStructure := envBool("LIVE_DYNAMIC_TP_USE_STRUCTURE", true)
	useATR := envBool("LIVE_DYNAMIC_TP_USE_ATR", true)
	useVWAPBands := envBool("LIVE_DYNAMIC_TP_USE_VWAP_BANDS", true)
	expectedMoveMult := envFloat("LIVE_DYNAMIC_TP_EXPECTED_MOVE_MULT", 1.0)

	if useStructure && c.Sig.VPTargetLevel > 0 && priceInTradeDirection(sideBuy, entry, c.Sig.VPTargetLevel) {
		cands = append(cands, c.Sig.VPTargetLevel)
	}
	if useVWAPBands && c.SessionVWAP > 0 {
		vwapDist := math.Abs(entry - c.SessionVWAP)
		if vwapDist > 0 {
			cands = append(cands,
				targetPriceAbsolute(sideBuy, entry, vwapDist*1.25),
				targetPriceAbsolute(sideBuy, entry, vwapDist*2.00),
			)
		}
	}
	if useATR && c.ATR > 0 {
		atrMults := []float64{1.25, 2.20, 3.40}
		if profile == "IMPULSE" {
			atrMults = []float64{1.60, 2.80, 4.20}
		}
		for _, mult := range atrMults {
			cands = append(cands, targetPriceAbsolute(sideBuy, entry, c.ATR*mult))
		}
	}
	if c.ATRPct > 0 && expectedMoveMult > 0 {
		move := entry * c.ATRPct * expectedMoveMult
		if profile == "IMPULSE" {
			move *= 1.25
		}
		cands = append(cands,
			targetPriceAbsolute(sideBuy, entry, maxFloat(stopDist*tp1R, move)),
			targetPriceAbsolute(sideBuy, entry, maxFloat(stopDist*tp2R, move*1.8)),
			targetPriceAbsolute(sideBuy, entry, maxFloat(stopDist*tp3R, move*2.4)),
		)
	}
	if c.DepthBid > 0 && c.DepthAsk > 0 {
		depthBias := math.Abs(c.DepthBid-c.DepthAsk) / maxFloat(c.DepthBid+c.DepthAsk, 1)
		if depthBias > 0.05 {
			depthMove := stopDist * (1.0 + depthBias*4)
			cands = append(cands, targetPriceAbsolute(sideBuy, entry, depthMove))
		}
	}

	levels := directionalLevels(cands, sideBuy, entry)
	if len(levels) == 0 {
		return profile, base[0], base[1], base[2]
	}
	picked := make([]float64, 0, 3)
	minSep := stopDist * envFloat("LIVE_DYNAMIC_TP_MIN_SEPARATION_R", 0.40)
	for _, lv := range levels {
		if len(picked) == 0 {
			if riskRewardToPrice(entry, stopDist, lv) >= envFloat("LIVE_STOP_MIN_RR_TO_TP1", 1.00) {
				picked = append(picked, lv)
			}
			continue
		}
		if math.Abs(lv-picked[len(picked)-1]) < minSep {
			continue
		}
		picked = append(picked, lv)
		if len(picked) == 3 {
			break
		}
	}
	for len(picked) < 3 {
		picked = append(picked, base[len(picked)])
	}
	return profile, picked[0], picked[1], picked[2]
}

func targetPriceForR(sideBuy bool, entry, stopDist, r float64) float64 {
	return targetPriceAbsolute(sideBuy, entry, stopDist*r)
}

func targetPriceAbsolute(sideBuy bool, entry, dist float64) float64 {
	if sideBuy {
		return entry + dist
	}
	return entry - dist
}

func priceInTradeDirection(sideBuy bool, entry, px float64) bool {
	if sideBuy {
		return px > entry
	}
	return px < entry
}

func directionalLevels(levels []float64, sideBuy bool, entry float64) []float64 {
	out := make([]float64, 0, len(levels))
	seen := map[int64]struct{}{}
	for _, lv := range levels {
		if lv <= 0 || !priceInTradeDirection(sideBuy, entry, lv) {
			continue
		}
		key := int64(lv * 1e8)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, lv)
	}
	sort.Slice(out, func(i, j int) bool {
		if sideBuy {
			return out[i] < out[j]
		}
		return out[i] > out[j]
	})
	return out
}

func riskRewardToPrice(entry, stopDist, target float64) float64 {
	if entry <= 0 || stopDist <= 0 || target <= 0 {
		return 0
	}
	return math.Abs(target-entry) / stopDist
}

func trailProfileMultiplier(profile string) float64 {
	switch strings.ToUpper(strings.TrimSpace(profile)) {
	case "IMPULSE":
		return envFloat("LIVE_TRAIL_IMPULSE_MULT", 1.15)
	case "ROTATION":
		return envFloat("LIVE_TRAIL_ROTATION_MULT", 0.90)
	default:
		return 1.0
	}
}

func structureTrailDistance(ref, friction float64) float64 {
	if ref <= 0 || friction <= 0 {
		return 0
	}
	return math.Abs(ref-friction) * envFloat("LIVE_TRAIL_STRUCTURE_FRAC", 0.55)
}

func quickCandidateSelectionReject(c candidate, now time.Time, pureMode, allowDeadSessionTrading bool, preEODEntryBlockMin int, localMaintNow time.Time, maintEOD maintenanceWindow, postSLCooldown time.Duration, paper *paperTrader, execMgr *liveExecManager, safety safetyConfig, lastOrderAt time.Time, lastOrderBySymbol map[string]time.Time, lastOrderBySymbolSide map[string]time.Time, orderCountByDay, orderCountByHour map[string]int, symbolStopoutLockUntil map[string]time.Time) string {
	raw := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	_ = preEODEntryBlockMin
	_ = localMaintNow
	_ = maintEOD
	if postSLCooldown > 0 && hasRecentStopLoss(raw, c.Side, now, postSLCooldown, paper, execMgr) {
		return "POST_SL_COOLDOWN"
	}
	if !allowDeadSessionTrading && data.CurrentRegimeCT(now) == data.RegimeDead {
		return "DEAD_SESSION_BLOCK"
	}
	if !pureMode {
		if reason := safetyReject(safety, c, localMaintNow, lastOrderAt, lastOrderBySymbol, lastOrderBySymbolSide, orderCountByDay, orderCountByHour, symbolStopoutLockUntil); reason != "" {
			return reason
		}
	}
	if execMgr != nil && execMgr.HasActiveSymbol(c.Entry.Symbol) {
		return "already_active_in_exec_state"
	}
	return ""
}

func paperMarkLastPrices(m symbolMeta, ob aster.OrderBook, model string, divBps float64) (float64, float64) {
	last := m.LastPrice
	bid, ask := topOfBook(ob, last)
	mark := last
	if bid > 0 && ask > 0 {
		mark = (bid + ask) / 2.0
	}
	if mark <= 0 {
		mark = last
	}
	if last <= 0 {
		last = mark
	}
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "mark_bias":
		if divBps > 0 {
			last = mark * (1 + divBps/10000.0)
		}
	case "last_bias":
		if divBps > 0 {
			mark = last * (1 - divBps/10000.0)
		}
	}
	return mark, last
}

func triggerPriceForRef(ref string, mark, last float64) float64 {
	switch strings.ToLower(strings.TrimSpace(ref)) {
	case "mark":
		if mark > 0 {
			return mark
		}
		return last
	case "last":
		if last > 0 {
			return last
		}
		return mark
	default:
		if mark > 0 {
			return mark
		}
		return last
	}
}

func depthFillRatio(side string, qty float64, ob aster.OrderBook) float64 {
	if qty <= 0 {
		return 1
	}
	levels := ob.Asks
	if !strings.EqualFold(side, "BUY") {
		levels = ob.Bids
	}
	if len(levels) == 0 {
		return 1
	}
	avail := 0.0
	for _, lv := range levels {
		if lv[1] > 0 {
			avail += lv[1]
		}
	}
	if avail <= 0 {
		return 1
	}
	return clamp(avail/qty, 0, 1)
}

func applyPaperPartialFill(qty float64, side string, ob aster.OrderBook, enabled bool, minFrac float64) float64 {
	if !enabled || qty <= 0 {
		return qty
	}
	ratio := depthFillRatio(side, qty, ob)
	if ratio >= 1 {
		return qty
	}
	frac := maxFloat(minFrac, ratio)
	return qty * clamp(frac, 0.05, 1.0)
}

func categorizeMissReason(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case r == "":
		return "uncategorized"
	case strings.Contains(r, "not_selected"), strings.Contains(r, "candidate_not_ready"), strings.Contains(r, "candidate_expired"):
		return "architecture_miss"
	case strings.Contains(r, "spread"), strings.Contains(r, "execution"), strings.Contains(r, "insufficient"), strings.Contains(r, "active"):
		return "execution_miss"
	case strings.Contains(r, "risk"), strings.Contains(r, "liq"), strings.Contains(r, "funding"), strings.Contains(r, "cooldown"), strings.Contains(r, "shadow"), strings.Contains(r, "reserve"), strings.Contains(r, "max_open"), strings.Contains(r, "correlated"):
		return "risk_miss"
	default:
		return "filter_miss"
	}
}

func nextFundingBoundary(now time.Time, interval time.Duration) time.Time {
	if interval <= 0 {
		return time.Time{}
	}
	slot := now.UTC().Truncate(interval)
	if slot.Equal(now.UTC()) {
		return slot.Add(interval)
	}
	return slot.Add(interval)
}

func fundingHazardWindow(now time.Time, interval, hazard, skipNew time.Duration) bool {
	if interval <= 0 {
		return false
	}
	next := nextFundingBoundary(now, interval)
	if next.IsZero() {
		return false
	}
	if skipNew < 0 {
		skipNew = 0
	}
	if hazard < 0 {
		hazard = 0
	}
	until := next.Sub(now.UTC())
	if until < 0 {
		until = -until
	}
	return until <= maxDuration(skipNew, hazard)
}

func paperFundingEntryBlocked(now time.Time, raw, side string, m symbolMeta, p *paperTrader) bool {
	if p == nil || !p.fundingEnabled || !fundingCostsPosition(side, m.FundingRate) {
		return false
	}
	interval := p.fundingEvery
	if p.fundingBySym != nil {
		if d, ok := p.fundingBySym[raw]; ok && d > 0 {
			interval = d
		}
	}
	return fundingHazardWindow(now, interval, p.fundingHazardSec, p.fundingSkipNewPos)
}

func estimatedFundingReserve(notional, fundingRate float64, interval time.Duration) float64 {
	if notional <= 0 || fundingRate == 0 || interval <= 0 {
		return 0
	}
	holdH := maxFloat(envFloat("LIVE_EXPECTED_HOLD_HOURS", 2.0), 2.0)
	intervals := holdH / maxFloat(interval.Hours(), 1.0)
	if intervals < 1 {
		intervals = 1
	}
	return notional * math.Abs(fundingRate) * intervals
}

type queueDeepPreflightCtx struct {
	Now                   time.Time
	LocalMaintNow         time.Time
	PureMode              bool
	OBFilterEnable        bool
	EntryDepth            map[string]aster.OrderBook
	OBLevels              int
	OBImbMin              float64
	OBMaxSpreadBps        float64
	RiskShell             risk.Config
	RiskFallbackStopPct   float64
	RiskHoldHours         float64
	LeverageMode          string
	LeverageFixed         int
	LeverageMin           int
	MaxLeverage           int
	EffectiveReserve      float64
	EffectiveMargin       float64
	AvailableUSDT         float64
	MetaBySymbol          map[string]symbolMeta
	InMaint               bool
	MaintWarmup           time.Duration
	MaintState            *maintenanceState
	Safety                safetyConfig
	Acct                  accountSnapshot
	Paper                 *paperTrader
	ReserveGate           *reserveLockGate
	EventLockoutMin       int
	CorrGroups            map[string]string
	MaxCorrelatedExposure float64
	RequireShadowDays     int
	ShadowEquityFile      string
	MaxOpenPos            int
	MaxOpenPerSide        int
	ExecMgr               *liveExecManager
}

type queueDeepPreflightResult struct {
	RejectReason string
	SpreadBps    float64
	BookImb      float64
}

func deepQueuePreflight(c candidate, ctx queueDeepPreflightCtx) queueDeepPreflightResult {
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	meta := ctx.MetaBySymbol[raw]
	if raw == "" {
		return queueDeepPreflightResult{RejectReason: "empty_symbol"}
	}
	if ctx.InMaint {
		return queueDeepPreflightResult{RejectReason: "maintenance_window"}
	}
	if ctx.MaintWarmup > 0 {
		if _, ok := maintenanceWarmupUntil(ctx.LocalMaintNow, ctx.MaintWarmup, ctx.MaintState); ok {
			return queueDeepPreflightResult{RejectReason: "post_maint_warmup"}
		}
	}
	if c.WallSpoofRisk >= envFloat("LIVE_WALL_SPOOF_RISK_REJECT", 0.75) {
		return queueDeepPreflightResult{RejectReason: "wall_spoof_risk"}
	}
	if c.WallMode == "wall_failure" && c.WallConfidence >= envFloat("LIVE_WALL_FAILURE_REJECT_CONF", 0.55) {
		return queueDeepPreflightResult{RejectReason: "wall_failed_on_touch"}
	}
	if c.WallMode == "wall_consumption" && c.WallConfidence < envFloat("LIVE_WALL_CONSUMPTION_MIN_CONF", 0.45) {
		return queueDeepPreflightResult{RejectReason: "wall_consumption_not_confirmed"}
	}
	if c.WallConfidence > 0 && c.WallPersistence < time.Duration(envInt("LIVE_WALL_MIN_PERSIST_MS", 3000))*time.Millisecond {
		return queueDeepPreflightResult{RejectReason: "wall_not_persistent"}
	}
	if ctx.OBFilterEnable {
		ob := ctx.EntryDepth[raw]
		okOB, obReason, obSpreadBps, obImb := orderbookEntryDecision(ob, c.Side, ctx.OBLevels, ctx.OBImbMin, ctx.OBMaxSpreadBps)
		if !okOB {
			return queueDeepPreflightResult{RejectReason: obReason, SpreadBps: obSpreadBps, BookImb: obImb}
		}
	}
	spreadBps, bookImb := orderbookRiskMetrics(raw, c.Side, ctx.EntryDepth, ctx.MetaBySymbol, ctx.OBLevels)
	entryPx := c.Sig.Entry
	if entryPx <= 0 {
		entryPx = meta.LastPrice
	}
	stopPx := c.Sig.Stop
	if stopPx <= 0 && entryPx > 0 {
		d := ctx.RiskFallbackStopPct / 100.0
		if strings.EqualFold(c.Side, "BUY") {
			stopPx = entryPx * (1 - d)
		} else {
			stopPx = entryPx * (1 + d)
		}
	}
	if !ctx.PureMode {
		effectiveLev := computeLeverage(c, ctx.LeverageMode, ctx.LeverageFixed, ctx.LeverageMin, ctx.MaxLeverage)
		riskDec := risk.Approve(ctx.RiskShell, risk.Input{
			Symbol:            raw,
			Side:              strings.ToUpper(strings.TrimSpace(c.Side)),
			Entry:             entryPx,
			Stop:              stopPx,
			Leverage:          float64(maxInt(1, effectiveLev)),
			NotionalUSD:       ctx.EffectiveMargin * float64(maxInt(1, effectiveLev)),
			FundingRate:       meta.FundingRate,
			HoldHours:         ctx.RiskHoldHours,
			SpreadBps:         spreadBps,
			BookImbalance:     bookImb,
			RecentSlippageBps: 0,
			VenueHealthy:      meta.LastPrice > 0,
		})
		if !riskDec.Approved {
			return queueDeepPreflightResult{RejectReason: riskDec.RejectReason, SpreadBps: spreadBps, BookImb: bookImb}
		}
	}
	if ctx.Paper != nil && paperFundingEntryBlocked(ctx.Now, raw, c.Side, meta, ctx.Paper) {
		return queueDeepPreflightResult{RejectReason: "paper_funding_hazard", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode && ctx.ExecMgr != nil && ctx.ExecMgr.fundingEntryBlocked(ctx.Now, raw, c.Side, meta.FundingRate) {
		return queueDeepPreflightResult{RejectReason: "funding_hazard_entry_block", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode {
		if reason := safetyReject(ctx.Safety, c, ctx.Now, time.Time{}, nil, nil, nil, nil, nil); reason != "" {
			return queueDeepPreflightResult{RejectReason: reason, SpreadBps: spreadBps, BookImb: bookImb}
		}
	}
	if !ctx.PureMode && inEventLockout(ctx.Now, ctx.EventLockoutMin) {
		return queueDeepPreflightResult{RejectReason: "event_lockout", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode && isCorrelatedExposureTooHigh(c, ctx.Acct, ctx.CorrGroups, ctx.MaxCorrelatedExposure) {
		return queueDeepPreflightResult{RejectReason: "correlated_exposure_gate", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode && ctx.RequireShadowDays > 0 && !shadowReady(ctx.RequireShadowDays, ctx.ShadowEquityFile, ctx.Now) {
		return queueDeepPreflightResult{RejectReason: "shadow_gate_active", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if ctx.ExecMgr == nil && !ctx.PureMode {
		return queueDeepPreflightResult{RejectReason: "exec_manager_unavailable", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if ctx.ExecMgr != nil && ctx.ExecMgr.HasActiveSymbol(c.Entry.Symbol) {
		return queueDeepPreflightResult{RejectReason: "already_active_in_exec_state", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode && ctx.AvailableUSDT > 0 && ctx.AvailableUSDT < ctx.Safety.minAvailUSDT {
		return queueDeepPreflightResult{RejectReason: "min_available_usdt", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if !ctx.PureMode && ctx.AvailableUSDT > 0 {
		usable := ctx.AvailableUSDT - ctx.EffectiveReserve
		if usable < ctx.EffectiveMargin {
			return queueDeepPreflightResult{RejectReason: "insufficient_usable", SpreadBps: spreadBps, BookImb: bookImb}
		}
		baseBal := sizingBaseBalance(ctx.AvailableUSDT, ctx.Paper)
		if ctx.ReserveGate != nil && ctx.ReserveGate.block(baseBal) {
			return queueDeepPreflightResult{RejectReason: "reserve_lock_active", SpreadBps: spreadBps, BookImb: bookImb}
		}
	}
	openCount := len(ctx.Acct.Positions)
	if ctx.MaxOpenPos > 0 && openCount >= ctx.MaxOpenPos {
		return queueDeepPreflightResult{RejectReason: "max_open_positions", SpreadBps: spreadBps, BookImb: bookImb}
	}
	if ctx.MaxOpenPerSide > 0 {
		openSideCount := countOpenPositionsBySide(ctx.Acct, c.Side)
		if ctx.ExecMgr != nil && openSideCount == 0 {
			openSideCount = ctx.ExecMgr.ActiveCountBySide(c.Side)
		}
		if openSideCount >= ctx.MaxOpenPerSide {
			return queueDeepPreflightResult{RejectReason: "max_open_positions_side", SpreadBps: spreadBps, BookImb: bookImb}
		}
	}
	if ctx.ExecMgr != nil && ctx.MaxOpenPos > 0 && ctx.ExecMgr.ActiveCount() >= ctx.MaxOpenPos {
		return queueDeepPreflightResult{RejectReason: "max_tracked_entries", SpreadBps: spreadBps, BookImb: bookImb}
	}
	return queueDeepPreflightResult{SpreadBps: spreadBps, BookImb: bookImb}
}

func sortedMissedReasons(mem map[string]recentRejectMemory) []string {
	if len(mem) == 0 {
		return nil
	}
	keys := make([]string, 0, len(mem))
	for _, v := range mem {
		keys = append(keys, v.Reject)
	}
	sort.Strings(keys)
	return keys
}
