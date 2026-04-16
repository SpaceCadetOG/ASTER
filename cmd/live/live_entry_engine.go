package main

import (
	"fmt"
	"strings"
	"time"

	"go-machine/internal/inplay"
	"go-machine/internal/strategies"
)

func choosePrimaryLiveSignal(c candidate, now time.Time) candidate {
	if reason, blocked := hardBlockEntry(c); blocked {
		c.Strat = "none"
		c.Conf = 0
		c.RejectReason = reason
		return c
	}
	if out, ok := tryMomentumIgnite(c, now); ok {
		return out
	}
	if out, ok := tryContinuationFast(c, now); ok {
		return out
	}
	if out, ok := tryImpulsiveStarter(c, now); ok {
		return out
	}
	c.Strat = "none"
	c.Conf = 0
	c.RejectReason = "no_live_entry_path"
	return c
}

func hardBlockEntry(c candidate) (string, bool) {
	switch {
	case c.ExtensionATR >= envFloat("LIVE_TRUE_EXTENSION_ATR", 2.25):
		return "extended", true
	case candidateSpikeCandle(c) || (c.Entry.ExhaustionRisk >= envFloat("LIVE_TRUE_EXHAUSTION_RISK", 5.5)):
		return "exhausted", true
	case directionalConflictRejectReason(c) != "":
		return directionalConflictRejectReason(c), true
	case c.SpreadBps > envFloat("LIVE_MAX_SPREAD_BPS", envFloat("LIVE_OB_MAX_SPREAD_BPS", 10)):
		return "spread_too_wide", true
	default:
		return "", false
	}
}

func tryMomentumIgnite(c candidate, now time.Time) (candidate, bool) {
	if !envBool("LIVE_ENABLE_MOMENTUM_IGNITE", true) {
		return c, false
	}
	if resetName, resetConf, resetReasons := qualifiesResetImpulse(c, now); resetName != "" {
		c.Strat = resetName
		c.Conf = resetConf
		c.Sig = strategies.Signal{
			Active:       true,
			Name:         resetName,
			Side:         toFeatureSide(c.Side),
			Confidence:   resetConf,
			RejectReason: "",
			Reasons:      resetReasons,
			Tags:         []string{"reset_impulse"},
		}
		c.Sig = applySignalRiskGeometry(c, resetName)
		c.RejectReason = ""
		return c, true
	}

	igniteMinScore := envFloat("LIVE_IGNITE_MIN_SCORE", 60.0)
	igniteMinSlope := envFloat("LIVE_IGNITE_MIN_SLOPE", 0.08)
	igniteMinVolRatio := envFloat("LIVE_IGNITE_MIN_VOL_RATIO", 1.00)
	igniteMinOFIZ := envFloat("LIVE_IGNITE_MIN_OFI_Z", 0.60)
	igniteBaseConf := envFloat("LIVE_IGNITE_BASE_CONF", 0.56)
	igniteHeatMaxStateMin := envFloat("LIVE_IGNITE_HEATING_MAX_STATE_MIN", 14.0)
	igniteInPlayMaxStateMin := envFloat("LIVE_IGNITE_INPLAY_MAX_STATE_MIN", 8.0)

	structureOK := starterDirectionalContextOK(c)
	freshStateOK := false
	switch c.Entry.State {
	case inplay.StateHeating:
		freshStateOK = c.Entry.TimeInStateMin <= igniteHeatMaxStateMin
	case inplay.StateInPlay:
		freshStateOK = c.Entry.TimeInStateMin <= igniteInPlayMaxStateMin
	}

	styleMatch := false
	igniteName := ""
	if strings.EqualFold(c.Side, "BUY") {
		styleMatch = c.Entry.EntryStyle == "momentum_ignite_long"
		igniteName = "momentum_ignite_long"
	} else {
		styleMatch = c.Entry.EntryStyle == "momentum_ignite_short"
		igniteName = "momentum_ignite_short"
	}
	ofiReady := c.OFISamples < maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8))
	if !ofiReady {
		if strings.EqualFold(c.Side, "BUY") {
			ofiReady = c.OFIZ >= igniteMinOFIZ
		} else {
			ofiReady = c.OFIZ <= -igniteMinOFIZ
		}
	}
	if !(styleMatch && freshStateOK && c.Entry.Momentum && c.Entry.CurrentScore >= igniteMinScore &&
		c.Entry.ScoreSlope >= igniteMinSlope && c.VolumeRatio >= igniteMinVolRatio && ofiReady && structureOK) {
		return c, false
	}

	c.Strat = igniteName
	c.Conf = clamp(igniteBaseConf+min(0.20, max(0.0, c.Entry.ScoreSlope-igniteMinSlope)*0.35+max(0.0, c.VolumeRatio-igniteMinVolRatio)*0.10), 0, 0.86)
	c.Sig = strategies.Signal{Active: true, Name: igniteName, Side: toFeatureSide(c.Side)}
	c.Sig = applySignalRiskGeometry(c, igniteName)
	c.RejectReason = ""
	return c, true
}

func tryContinuationFast(c candidate, _ time.Time) (candidate, bool) {
	if !envBool("LIVE_ENABLE_CONTINUATION_FAST", true) {
		return c, false
	}
	if strings.EqualFold(c.Side, "BUY") && c.Entry.LongDemotionFlag {
		return c, false
	}
	if strings.EqualFold(c.Side, "SELL") && c.Entry.ShortDemotionFlag {
		return c, false
	}

	fastMinScore := envFloat("LIVE_CONT_FAST_MIN_SCORE", 65.0)
	fastMinSlope := envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02)
	fastMinVolRatio := envFloat("LIVE_CONT_FAST_MIN_VOL_RATIO", 1.15)
	fastMinOFIZ := envFloat("LIVE_CONT_FAST_MIN_OFI_Z", 0.35)
	fastBaseConf := envFloat("LIVE_CONT_FAST_BASE_CONF", 0.58)
	structureConfirmOK := continuationStructureConfirmed(c)
	vwapEMAOK := starterDirectionalContextOK(c)
	ofiOK := c.OFISamples < maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8))
	if !ofiOK {
		if strings.EqualFold(c.Side, "BUY") {
			ofiOK = c.OFIZ >= fastMinOFIZ
		} else {
			ofiOK = c.OFIZ <= -fastMinOFIZ
		}
	}
	stateOK := c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay || c.Entry.State == inplay.StatePumping

	if stateOK &&
		c.Entry.CurrentScore >= fastMinScore &&
		c.Entry.ScoreSlope >= fastMinSlope &&
		c.VolumeRatio >= fastMinVolRatio &&
		ofiOK &&
		structureConfirmOK &&
		vwapEMAOK {
		c.Strat = "continuation_fast"
		c.Conf = clamp(fastBaseConf+min(0.22, (c.VolumeRatio-fastMinVolRatio)*0.08+max(0.0, c.Entry.ScoreSlope-fastMinSlope)*0.30), 0, 0.90)
		c.Sig = strategies.Signal{Active: true, Name: "continuation_fast", Side: toFeatureSide(c.Side)}
		c.Sig = applySignalRiskGeometry(c, "continuation_fast")
		c.RejectReason = ""
		return c, true
	}
	return c, false
}

func tryImpulsiveStarter(c candidate, _ time.Time) (candidate, bool) {
	fails := make([]string, 0, 6)
	if c.Entry.CurrentScore < envFloat("LIVE_CONT_FAST_MIN_SCORE", 65.0) {
		fails = append(fails, fmt.Sprintf("score:%.2f<%.2f", c.Entry.CurrentScore, envFloat("LIVE_CONT_FAST_MIN_SCORE", 65.0)))
	}
	if c.Entry.ScoreSlope < envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02) {
		fails = append(fails, fmt.Sprintf("slope:%.3f<%.3f", c.Entry.ScoreSlope, envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02)))
	}
	if c.VolumeRatio < envFloat("LIVE_CONT_FAST_MIN_VOL_RATIO", 1.15) {
		fails = append(fails, fmt.Sprintf("vol_ratio:%.2f<%.2f", c.VolumeRatio, envFloat("LIVE_CONT_FAST_MIN_VOL_RATIO", 1.15)))
	}
	if !continuationStructureConfirmed(c) {
		fails = append(fails, firstNonEmpty(c.StructureReason, "continuation_no_structure_confirm"))
	}
	if !starterDirectionalContextOK(c) {
		if strings.EqualFold(c.Side, "BUY") {
			fails = append(fails, "below_vwap_ema")
		} else {
			fails = append(fails, "above_vwap_ema")
		}
	}

	if conf, reasons, ok := qualifiesImpulsiveShortStarter(c, fails); ok && starterSoftRejectsOnly(c) {
		c.Strat = "impulsive_short_starter"
		c.Conf = conf
		c.Sig = strategies.Signal{Active: true, Name: "impulsive_short_starter", Side: toFeatureSide(c.Side), Confidence: conf, Reasons: reasons, Tags: []string{"starter_only"}}
		c.Sig = applySignalRiskGeometry(c, "impulsive_short_starter")
		c.RejectReason = ""
		return c, true
	}
	if conf, reasons, ok := qualifiesImpulsiveLongStarter(c, fails); ok && starterSoftRejectsOnly(c) {
		c.Strat = "impulsive_long_starter"
		c.Conf = conf
		c.Sig = strategies.Signal{Active: true, Name: "impulsive_long_starter", Side: toFeatureSide(c.Side), Confidence: conf, Reasons: reasons, Tags: []string{"starter_only"}}
		c.Sig = applySignalRiskGeometry(c, "impulsive_long_starter")
		c.RejectReason = ""
		return c, true
	}
	return c, false
}

func starterSoftRejectsOnly(c candidate) bool {
	if c.Entry.ScoreSlope <= -0.01 {
		return false
	}
	if hardReason, blocked := hardBlockEntry(c); blocked {
		_ = hardReason
		return false
	}
	return c.Entry.Rank <= envFloat("LIVE_ELITE_STARTER_MAX_RANK", 2.0) ||
		qualifiesEliteStarterCandidate(c) ||
		starterStructureContextOK(c) ||
		candidateDirectionalMovePct(c) >= envFloat("LIVE_STARTER_MIN_DIRECTIONAL_PCT", 3.0)
}
