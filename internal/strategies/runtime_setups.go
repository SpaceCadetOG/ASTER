package strategies

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"go-machine/internal/features"
)

func EvaluateRuntimeSignal(ctx Context) (Signal, bool) {
	if ctx.Runtime == nil {
		return Signal{}, false
	}
	switch strings.ToLower(strings.TrimSpace(ctx.Runtime.RequestedStrategy)) {
	case "exhaustion_flip_short":
		return evalExhaustionFlipShort(ctx), true
	case "exhaustion_flip_long":
		return evalExhaustionFlipLong(ctx), true
	case "mom_reversal_short":
		return evalMomentumReversalShort(ctx), true
	case "mom_reversal":
		return evalMomentumReversal(ctx), true
	default:
		return Signal{}, false
	}
}

func ApplySharedInvalidations(ctx Context, sig Signal) Signal {
	if ctx.Runtime == nil || !sig.Active {
		return sig
	}
	name := strings.ToLower(strings.TrimSpace(sig.Name))
	if sig.Side == features.SideLong &&
		strings.EqualFold(strings.TrimSpace(ctx.Runtime.CandidateState), "cooling") &&
		(name == "failed_auction_magnet" || name == "bos_pb" || name == "fa") &&
		ctx.Runtime.SessionVWAP > 0 &&
		ctx.Runtime.EMA9 > 0 &&
		ctx.Runtime.LastClose < ctx.Runtime.SessionVWAP &&
		ctx.Runtime.LastClose < ctx.Runtime.EMA9 {
		sig.Active = false
		sig.RejectReason = "VWAP_EMA_LONG_INVALIDATION"
		sig.Reasons = append(sig.Reasons, "VWAP_EMA_LONG_INVALIDATION")
	}
	return sig
}

func evalExhaustionFlipShort(ctx Context) Signal {
	rt := ctx.Runtime
	if rt == nil {
		return Signal{}
	}
	if runtimeEnvBool("LIVE_ENABLE_OFI", true) && rt.OFISamples >= runtimeEnvInt("LIVE_OFI_MIN_SAMPLES", 8) && rt.OFIZ > runtimeEnvFloat("LIVE_REV_SHORT_MAX_OFI_Z", -0.80) {
		return rejectedRuntimeSignal("exhaustion_flip_short", features.SideShort, "short_ofi_not_ready")
	}
	confirmVWAP := rt.SessionVWAP > 0 && rt.LastClose < rt.SessionVWAP && rt.FastSlope <= 0
	confirmEMA := rt.EMA9 > 0 && rt.LastClose < rt.EMA9 && rt.FastSlope <= 0
	lowerHighPrinted := rt.SlowSlope < 0 && rt.FastSlope < 0 && rt.FailedBounceCount >= 1
	bounceLowBroken := rt.FailedReclaimCount >= 1 && rt.FastSlope <= -0.10
	panicFlush := rt.BarsSincePeak <= 1 && rt.DrawdownFromPeakPct <= -6
	if panicFlush && !confirmVWAP && !confirmEMA && !bounceLowBroken {
		return rejectedRuntimeSignal("exhaustion_flip_short", features.SideShort, "panic_flush_no_bounce_confirmation")
	}
	if !(confirmVWAP || confirmEMA || bounceLowBroken || (lowerHighPrinted && rt.FailedBounceCount >= 1)) {
		return rejectedRuntimeSignal("exhaustion_flip_short", features.SideShort, "short_no_failed_reclaim_yet")
	}
	entryPx := rt.LastClose
	if entryPx <= 0 && len(ctx.Candles) > 0 {
		entryPx = ctx.Candles[len(ctx.Candles)-1].C
	}
	failedHigh := runtimeRecentHigh(ctx.Candles, runtimeEnvInt("LIVE_EXHAUSTION_STOP_LOOKBACK_BARS", 12))
	stopPx := runtimeMax(failedHigh, runtimeMax(rt.EMA9, rt.SessionVWAP))
	stopPad := runtimeEnvFloat("LIVE_EXHAUSTION_STOP_PAD_PCT", 0.0035)
	if stopPx > 0 {
		stopPx *= 1 + stopPad
	}
	if stopPx <= entryPx {
		stopPx = entryPx * (1 + runtimeEnvFloat("LIVE_EXHAUSTION_MIN_STOP_PCT", 0.02))
	}
	risk := stopPx - entryPx
	tp1 := entryPx - risk*runtimeEnvFloat("LIVE_EXHAUSTION_TP1_R", 0.8)
	tp2 := entryPx - risk*runtimeEnvFloat("LIVE_EXHAUSTION_TP2_R", 1.6)
	baseConf := runtimeEnvFloat("LIVE_EXHAUSTION_BASE_CONF", 0.60)
	confBoost := runtimeMin(0.20, rt.IntradayReversalScore*0.02+float64(rt.FailedReclaimCount)*0.03)
	conf := runtimeClamp(baseConf+confBoost, 0, 0.88)
	return Signal{
		Active:       true,
		Name:         "exhaustion_flip_short",
		Side:         features.SideShort,
		Entry:        entryPx,
		Stop:         stopPx,
		TP1:          tp1,
		TP2:          tp2,
		Confidence:   conf,
		RejectReason: "",
		Reasons: []string{
			fmt.Sprintf("drawdown_from_peak_pct=%.2f", rt.DrawdownFromPeakPct),
			fmt.Sprintf("intraday_reversal_score=%.2f", rt.IntradayReversalScore),
			fmt.Sprintf("ofi_z=%.2f", rt.OFIZ),
			fmt.Sprintf("failed_reclaim_count=%d", rt.FailedReclaimCount),
			fmt.Sprintf("failed_bounce_count=%d", rt.FailedBounceCount),
			fmt.Sprintf("entry_style=%s", ctx.EntryStyle),
			fmt.Sprintf("meta_state=%s", ctx.MetaState),
		},
		Tags: []string{"reversal_watch", "late_unwind"},
	}
}

func evalExhaustionFlipLong(ctx Context) Signal {
	rt := ctx.Runtime
	if rt == nil {
		return Signal{}
	}
	if runtimeEnvBool("LIVE_ENABLE_OFI", true) && rt.OFISamples >= runtimeEnvInt("LIVE_OFI_MIN_SAMPLES", 8) && rt.OFIZ < runtimeEnvFloat("LIVE_REV_LONG_MIN_OFI_Z", 0.80) {
		return rejectedRuntimeSignal("exhaustion_flip_long", features.SideLong, "long_ofi_not_ready")
	}
	confirmVWAP := rt.SessionVWAP > 0 && rt.LastClose > rt.SessionVWAP && rt.FastSlope >= 0
	confirmEMA := rt.EMA9 > 0 && rt.LastClose > rt.EMA9 && rt.FastSlope >= 0
	higherLowPrinted := rt.SlowSlope > 0 && rt.FastSlope > 0 && rt.FailedBreakdownCount >= 1
	bounceHighBroken := rt.FailedBreakLowCount >= 1 && rt.FastSlope >= 0.10
	failedBreakdownTrap := rt.FailedBreakdownCount >= 1 && (confirmVWAP || confirmEMA || rt.FastSlope >= 0.05)
	firstGreen := rt.BarsSinceTrough <= 1 && rt.DrawupFromTroughPct >= 4
	if firstGreen && !(failedBreakdownTrap || confirmVWAP || confirmEMA) {
		return rejectedRuntimeSignal("exhaustion_flip_long", features.SideLong, "first_green_candle_no_structure")
	}
	if !(failedBreakdownTrap || confirmVWAP || confirmEMA || (higherLowPrinted && bounceHighBroken)) {
		if rt.FailedBreakdownCount == 0 && rt.FailedBreakLowCount == 0 {
			return rejectedRuntimeSignal("exhaustion_flip_long", features.SideLong, "dead_cat_bounce_no_failed_breakdown")
		}
		return rejectedRuntimeSignal("exhaustion_flip_long", features.SideLong, "long_no_reclaim_hold_yet")
	}
	entryPx := rt.LastClose
	if entryPx <= 0 && len(ctx.Candles) > 0 {
		entryPx = ctx.Candles[len(ctx.Candles)-1].C
	}
	failedLow := runtimeRecentLow(ctx.Candles, runtimeEnvInt("LIVE_EXHAUSTION_LONG_STOP_LOOKBACK_BARS", 12))
	stopPx := failedLow
	if rt.EMA9 > 0 {
		stopPx = runtimeMinPositive(stopPx, rt.EMA9)
	}
	if rt.SessionVWAP > 0 {
		stopPx = runtimeMinPositive(stopPx, rt.SessionVWAP)
	}
	stopPad := runtimeEnvFloat("LIVE_EXHAUSTION_LONG_STOP_PAD_PCT", 0.0035)
	if stopPx > 0 {
		stopPx *= 1 - stopPad
	}
	if stopPx <= 0 || stopPx >= entryPx {
		stopPx = entryPx * (1 - runtimeEnvFloat("LIVE_EXHAUSTION_LONG_MIN_STOP_PCT", 0.02))
	}
	risk := entryPx - stopPx
	tp1 := entryPx + risk*runtimeEnvFloat("LIVE_EXHAUSTION_LONG_TP1_R", 0.8)
	tp2 := entryPx + risk*runtimeEnvFloat("LIVE_EXHAUSTION_LONG_TP2_R", 1.6)
	baseConf := runtimeEnvFloat("LIVE_EXHAUSTION_LONG_BASE_CONF", 0.60)
	confBoost := runtimeMin(0.20, rt.BullReversalScore*0.025+float64(rt.FailedBreakdownCount)*0.03)
	conf := runtimeClamp(baseConf+confBoost, 0, 0.88)
	return Signal{
		Active:       true,
		Name:         "exhaustion_flip_long",
		Side:         features.SideLong,
		Entry:        entryPx,
		Stop:         stopPx,
		TP1:          tp1,
		TP2:          tp2,
		Confidence:   conf,
		RejectReason: "",
		Reasons: []string{
			fmt.Sprintf("drawup_from_trough_pct=%.2f", rt.DrawupFromTroughPct),
			fmt.Sprintf("bull_reversal_score=%.2f", rt.BullReversalScore),
			fmt.Sprintf("ofi_z=%.2f", rt.OFIZ),
			fmt.Sprintf("failed_breakdown_count=%d", rt.FailedBreakdownCount),
			fmt.Sprintf("failed_break_low_count=%d", rt.FailedBreakLowCount),
			fmt.Sprintf("entry_style=%s", ctx.EntryStyle),
			fmt.Sprintf("meta_state=%s", ctx.MetaState),
		},
		Tags: []string{"reversal_watch_long", "short_exhausting"},
	}
}

func evalMomentumReversalShort(ctx Context) Signal {
	rt := ctx.Runtime
	if rt == nil {
		return Signal{}
	}
	lastVol := 0.0
	if len(ctx.Candles) > 0 {
		lastVol = ctx.Candles[len(ctx.Candles)-1].V
	}
	avgVol := runtimeSMAVolume(ctx.Candles, 20)
	volSpike := 0.0
	if avgVol > 0 {
		volSpike = lastVol / avgVol
	}
	reversalVolSpike := runtimeEnvFloat("LIVE_REVERSAL_VOL_SPIKE", 1.80)
	if rt.LastClose < rt.EMA9 && volSpike >= reversalVolSpike {
		conf := 0.62 + runtimeMin(0.18, (volSpike-reversalVolSpike)*0.05)
		return Signal{
			Active:     true,
			Name:       "mom_reversal_short",
			Side:       features.SideShort,
			Confidence: conf,
		}
	}
	return rejectedRuntimeSignal("mom_reversal_short", features.SideShort, "mom_reversal_short_not_ready")
}

func evalMomentumReversal(ctx Context) Signal {
	rt := ctx.Runtime
	if rt == nil {
		return Signal{}
	}
	side := rt.Side
	if side != features.SideShort && side != features.SideLong {
		side = features.SideLong
	}
	conf := 0.35 + runtimeMin(0.25, runtimeAbs(ctx.ScoreSlope)*0.15)
	return Signal{
		Active:     true,
		Name:       "mom_reversal",
		Side:       side,
		Confidence: conf,
	}
}

func rejectedRuntimeSignal(name string, side features.Side, reason string) Signal {
	return Signal{
		Name:         name,
		Side:         side,
		RejectReason: reason,
	}
}

func runtimeRecentHigh(c []features.Candle, lookback int) float64 {
	if len(c) == 0 {
		return 0
	}
	if lookback <= 0 || lookback > len(c) {
		lookback = len(c)
	}
	best := 0.0
	for i := len(c) - lookback; i < len(c); i++ {
		if i < 0 {
			continue
		}
		if c[i].H > best {
			best = c[i].H
		}
	}
	return best
}

func runtimeRecentLow(c []features.Candle, lookback int) float64 {
	if len(c) == 0 {
		return 0
	}
	if lookback <= 0 || lookback > len(c) {
		lookback = len(c)
	}
	best := 0.0
	for i := len(c) - lookback; i < len(c); i++ {
		if i < 0 {
			continue
		}
		if best == 0 || c[i].L < best {
			best = c[i].L
		}
	}
	return best
}

func runtimeSMAVolume(c []features.Candle, n int) float64 {
	if len(c) == 0 {
		return 0
	}
	if n <= 0 || n > len(c) {
		n = len(c)
	}
	sum := 0.0
	for i := len(c) - n; i < len(c); i++ {
		sum += c[i].V
	}
	return sum / float64(n)
}

func runtimeEnvBool(key string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func runtimeEnvFloat(key string, def float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	return v
}

func runtimeEnvInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

func runtimeClamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func runtimeMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func runtimeMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func runtimeAbs(v float64) float64 {
	return math.Abs(v)
}

func runtimeMinPositive(a, b float64) float64 {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}
