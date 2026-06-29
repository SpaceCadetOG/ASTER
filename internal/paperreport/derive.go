package paperreport

import (
	"math"
	"sort"
	"strings"
)

const scannerFlatThreshold = 0.25

func BuildTradeLabels(records []ClosedTradeRecord) []TradeLabel {
	out := make([]TradeLabel, 0, len(records))
	for _, rec := range records {
		lbl := TradeLabel{
			TradeID:            rec.TradeID,
			Symbol:             rec.Symbol,
			Side:               normalizeSide(rec.Side),
			Setup:              firstNonEmpty(rec.Identity.RawStrategy, rec.Identity.Strategy, "unknown"),
			SetupFamily:        firstNonEmpty(rec.Identity.SetupFamily, "unknown"),
			StrategyFamily:     firstNonEmpty(rec.Identity.StrategyFamily, "unknown"),
			EntryStyle:         firstNonEmpty(rec.Identity.EntryStyle, "unknown"),
			ExecBucket:         firstNonEmpty(rec.Identity.ExecBucket, "unknown"),
			Session:            firstNonEmpty(rec.Identity.Session, "unknown"),
			EntryTiming:        firstNonEmpty(rec.Identity.EntryTiming, "unknown"),
			EntryOutcomeLabel:  firstNonEmpty(rec.Exit.EntryOutcomeLabel, "unknown"),
			EntryFinalScore:    rec.Identity.EntryScore.FinalScore,
			EntryScoreBucket:   entryScoreBucket(rec.Identity.EntryScore.FinalScore),
			EntryTime:          rec.Entry.EntryTs,
			ExitTime:           rec.Exit.ExitTs,
			RealizedR:          realizedR(rec),
			MaxRSeen:           rec.Exit.MaxRSeen,
			MinRSeen:           rec.Exit.MinRSeen,
			PostExitPeakR:      rec.PostExit.PostExitPeakR,
			EODR:               rec.PostExit.EODCaptureR,
			StopOutType:        firstNonEmpty(rec.Exit.StopOutType, "none"),
			StoppedThenReclaim: rec.PostExit.StoppedThenReclaim,
			ReentryWouldWork:   rec.PostExit.ReentryWouldWork,
			Record:             rec,
		}
		lbl.ScannerPatternEntry = ScannerPattern(lbl.Side, rec.Plan.Pct24hAtEntry, rec.Plan.Pct4hAtEntry, rec.Plan.Pct1hAtEntry)
		lbl.ScannerPatternExit = ScannerPattern(lbl.Side, rec.Exit.Pct24hAtExit, rec.Exit.Pct4hAtExit, rec.Exit.Pct1hAtExit)
		lbl.ScannerPatternEOD = ScannerPattern(lbl.Side, rec.PostExit.EODPct24h, rec.PostExit.EODPct4h, rec.PostExit.EODPct1h)
		lbl.TPPath = DeriveTPPath(rec)
		lbl.ExitQuality = DeriveExitQuality(lbl, rec)
		lbl.StopQuality = DeriveStopQuality(lbl)
		lbl.OppositeMoveR15m = maxFloat(0, -rec.PostExit.WorstR15m)
		lbl.OppositeMoveR60m = maxFloat(0, -rec.PostExit.WorstR60m)
		lbl.OppositeMoveREOD = maxFloat(0, -rec.PostExit.EODCaptureR)
		lbl.ShakeoutCandidate = DeriveShakeoutCandidate(lbl)
		lbl.ReentryCandidate = DeriveReentryCandidate(lbl)
		lbl.ReversalCandidate = DeriveReversalCandidate(lbl, rec)
		lbl.WickThroughStopThenReclaim = DeriveWickThroughStopThenReclaim(lbl)
		lbl.RuleCandidate = DeriveRuleCandidate(lbl)
		out = append(out, lbl)
	}
	return out
}

func entryScoreBucket(score float64) string {
	switch {
	case score >= 85:
		return "85_plus"
	case score >= 75:
		return "75_84"
	case score >= 65:
		return "65_74"
	case score >= 55:
		return "55_64"
	default:
		return "below_55"
	}
}

func ScannerPattern(side string, p24, p4, p1 float64) string {
	a24, a4, a1 := sideAdjust(side, p24), sideAdjust(side, p4), sideAdjust(side, p1)
	s24, s4, s1 := pctState(a24), pctState(a4), pctState(a1)
	switch {
	case s24 > 0 && s4 > 0 && s1 > 0:
		return "full_alignment"
	case s24 > 0 && s4 > 0 && s1 < 0:
		return "trend_with_1h_pullback"
	case s24 > 0 && s4 < 0 && s1 < 0:
		return "trend_with_4h_1h_rollover"
	case s24 < 0 && s4 > 0 && s1 > 0:
		return "countertrend_recovery"
	case s24 < 0 && s4 < 0 && s1 > 0:
		return "early_reversal_attempt"
	case s24 < 0 && s4 < 0 && s1 < 0:
		return "full_opposition"
	default:
		return "flat_or_mixed"
	}
}

func DeriveTPPath(rec ClosedTradeRecord) string {
	switch {
	case rec.Exit.HitTP1 && rec.Exit.HitTP2 && rec.Exit.HitTP3:
		return "tp1_tp2_tp3"
	case rec.Exit.HitTP1 && rec.Exit.HitTP2:
		return "tp1_tp2"
	case rec.Exit.HitTP1:
		return "tp1_only"
	default:
		return "no_tp"
	}
}

func DeriveExitQuality(lbl TradeLabel, rec ClosedTradeRecord) string {
	switch {
	case lbl.RealizedR < 0 && lbl.PostExitPeakR < 0.5 && lbl.EODR <= 0:
		return "good_loss"
	case lbl.RealizedR < 0 && lbl.PostExitPeakR >= 1.0:
		return "bad_loss"
	case lbl.StopOutType == "breakeven" && math.Abs(lbl.RealizedR) <= 0.25:
		return "breakeven_churn"
	case lbl.StopOutType == "profit_lock" && lbl.EODR <= lbl.RealizedR+0.25:
		return "good_profit_lock"
	case lbl.StopOutType == "profit_lock" && lbl.EODR >= lbl.RealizedR+0.75:
		return "too_early_profit_lock"
	case rec.Exit.HitTP1 && lbl.EODR >= lbl.RealizedR+1.0:
		return "runner_missed"
	default:
		return "neutral"
	}
}

func DeriveStopQuality(lbl TradeLabel) string {
	switch lbl.StopOutType {
	case "loss":
		if lbl.PostExitPeakR >= 1.0 {
			return "shakeout_risk"
		}
		return "hard_loss"
	case "breakeven":
		return "breakeven_churn"
	case "profit_lock":
		if lbl.EODR >= lbl.RealizedR+0.75 {
			return "tight_profit_lock"
		}
		return "good_profit_lock"
	default:
		return "non_stop_exit"
	}
}

func DeriveShakeoutCandidate(lbl TradeLabel) bool {
	return (lbl.StopOutType == "loss" || lbl.StopOutType == "breakeven") &&
		lbl.StoppedThenReclaim &&
		lbl.PostExitPeakR >= 1.0
}

func DeriveReentryCandidate(lbl TradeLabel) bool {
	return lbl.ReentryWouldWork || (lbl.StoppedThenReclaim && lbl.PostExitPeakR >= 1.0)
}

func DeriveReversalCandidate(lbl TradeLabel, rec ClosedTradeRecord) bool {
	if lbl.RealizedR >= 0 || lbl.StoppedThenReclaim {
		return false
	}
	d4 := sideAdjust(lbl.Side, rec.Exit.Pct4hAtExit-rec.Plan.Pct4hAtEntry)
	d1 := sideAdjust(lbl.Side, rec.Exit.Pct1hAtExit-rec.Plan.Pct1hAtEntry)
	return d4 < -scannerFlatThreshold && d1 < -scannerFlatThreshold && lbl.EODR < lbl.RealizedR
}

func DeriveWickThroughStopThenReclaim(lbl TradeLabel) bool {
	return (lbl.StopOutType == "loss" || lbl.StopOutType == "breakeven") &&
		lbl.StoppedThenReclaim &&
		lbl.OppositeMoveR15m <= 0.5 &&
		lbl.PostExitPeakR >= 1.0
}

func DeriveRuleCandidate(lbl TradeLabel) string {
	switch {
	case lbl.ReversalCandidate:
		return "review_reversal"
	case lbl.ReentryCandidate:
		return "review_reentry"
	case lbl.ShakeoutCandidate:
		return "review_shakeout"
	case lbl.ExitQuality == "runner_missed" || lbl.ExitQuality == "too_early_profit_lock":
		return "review_exit_profile"
	default:
		return ""
	}
}

func realizedR(rec ClosedTradeRecord) float64 {
	risk := rec.Plan.PlannedRiskPrice
	if risk <= 0 && rec.Entry.EntryPrice > 0 && rec.Plan.OriginalStop > 0 {
		risk = math.Abs(rec.Entry.EntryPrice - rec.Plan.OriginalStop)
	}
	if risk <= 0 || rec.Entry.Qty <= 0 {
		return 0
	}
	return rec.Exit.NetPnL / (risk * rec.Entry.Qty)
}

func sideAdjust(side string, v float64) float64 {
	if normalizeSide(side) == "SELL" {
		return -v
	}
	return v
}

func pctState(v float64) int {
	switch {
	case v > scannerFlatThreshold:
		return 1
	case v < -scannerFlatThreshold:
		return -1
	default:
		return 0
	}
}

func normalizeSide(side string) string {
	if strings.EqualFold(strings.TrimSpace(side), "SELL") || strings.EqualFold(strings.TrimSpace(side), "SHORT") {
		return "SELL"
	}
	return "BUY"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float64(nil), values...)
	sort.Float64s(cp)
	mid := len(cp) / 2
	if len(cp)%2 == 1 {
		return cp[mid]
	}
	return (cp[mid-1] + cp[mid]) / 2
}
