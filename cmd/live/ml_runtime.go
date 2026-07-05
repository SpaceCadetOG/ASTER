package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/mlscore"
	"go-machine/internal/stats"
)

type mlRuntimeConfig struct {
	Enable       bool
	Mode         string
	ModelPath    string
	MinTakeProb  float64
	MinExpectedR float64
	ShadowLog    bool
}

func loadMLRuntimeConfig() mlRuntimeConfig {
	mode := strings.ToLower(envStr("LIVE_ML_MODE", "shadow"))
	switch mode {
	case "shadow", "rank", "filter", "manage":
	default:
		mode = "shadow"
	}
	return mlRuntimeConfig{
		Enable:       envBool("LIVE_ML_ENABLE", false),
		Mode:         mode,
		ModelPath:    envStr("LIVE_ML_MODEL_PATH", "models/ml/take_trade_v1.json"),
		MinTakeProb:  envFloat("LIVE_ML_MIN_TAKE_PROB", 0.55),
		MinExpectedR: envFloat("LIVE_ML_MIN_EXPECTED_R", 0.20),
		ShadowLog:    envBool("LIVE_ML_SHADOW_LOG", true),
	}
}

func loadMLRuntimeScorer(cfg mlRuntimeConfig) (mlscore.Scorer, error) {
	return mlscore.Load(cfg.ModelPath, cfg.Enable)
}

func applyMLRuntimeScoring(cands []candidate, cfg mlRuntimeConfig, scorer mlscore.Scorer, eventLog *stats.EventLogger, now time.Time) []candidate {
	if len(cands) == 0 || !cfg.Enable || scorer == nil {
		return cands
	}
	out := append([]candidate(nil), cands...)
	for i := range out {
		req := buildMLScoreRequest(out[i])
		resp := scorer.Score(req)
		out[i].MLEnabled = resp.Enabled
		out[i].MLModelVersion = resp.ModelVersion
		out[i].MLTakeTradeProbability = resp.TakeTradeProbability
		out[i].MLExpectedR = resp.ExpectedR
		out[i].MLExpectedMaxR = resp.ExpectedMaxR
		out[i].MLStopReclaimProb = resp.StopoutThenReclaimProb
		out[i].MLReentryProb = resp.ReentryAfterStopProbability
		out[i].MLSuggestedStopProfile = resp.SuggestedStopProfile
		out[i].MLSuggestedExitProfile = resp.SuggestedExitProfile
		if cfg.ShadowLog && eventLog != nil {
			eventLog.Emit(stats.Event{
				Timestamp:         now,
				Type:              "ML_SCORE",
				Symbol:            strings.ToUpper(aster.RawSymbol(out[i].Entry.Symbol)),
				Side:              out[i].Side,
				Strategy:          out[i].Strat,
				SetupFamily:       out[i].SetupFamily,
				SetupSource:       out[i].SetupSource,
				TradeHorizon:      out[i].TradeHorizon,
				TriggerState:      out[i].TriggerState,
				ExitProfile:       out[i].ExitProfile,
				Discovery:         out[i].DiscoveryScore,
				Trigger:           out[i].TriggerScore,
				Execution:         out[i].ExecutionScore,
				Combined:          out[i].CombinedScore,
				ModelVersion:      resp.ModelVersion,
				MLTakeProb:        resp.TakeTradeProbability,
				MLExpectedR:       resp.ExpectedR,
				MLExpectedMaxR:    resp.ExpectedMaxR,
				MLStopReclaimProb: resp.StopoutThenReclaimProb,
				MLReentryProb:     resp.ReentryAfterStopProbability,
				MLStopProfile:     resp.SuggestedStopProfile,
				MLExitProfile:     resp.SuggestedExitProfile,
				Reason:            strings.Join(resp.Reasons, ","),
			})
		}
		if !resp.Enabled {
			continue
		}
		if cfg.Mode == "rank" {
			out[i].FinalRank = adjustRankWithML(out[i].FinalRank, resp)
		}
		if cfg.Mode == "filter" {
			if resp.TakeTradeProbability < cfg.MinTakeProb {
				out[i].RejectReason = firstNonEmpty(strings.TrimSpace(out[i].RejectReason), "ml_low_take_probability")
			}
			if out[i].RejectReason == "" && resp.ExpectedR < cfg.MinExpectedR {
				out[i].RejectReason = "ml_low_expected_r"
			}
		}
		if cfg.Mode == "manage" {
			if strings.TrimSpace(resp.SuggestedExitProfile) != "" {
				out[i].ExitProfile = resp.SuggestedExitProfile
			}
		}
	}
	if cfg.Mode == "rank" {
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].FinalRank > out[j].FinalRank
		})
	}
	return out
}

func buildMLScoreRequest(c candidate) mlscore.ScoreRequest {
	return mlscore.ScoreRequest{
		CandidateID: candidateMLID(c),
		Symbol:      strings.ToUpper(aster.RawSymbol(c.Entry.Symbol)),
		Side:        c.Side,
		Features: map[string]float64{
			"day_utc_24h_pct":       c.DayUTC24h,
			"utc_4h_pct":            c.UTC4hPct,
			"utc_1h_pct":            c.UTC1hPct,
			"volume_ratio":          c.VolumeRatio,
			"distance_to_vwap_pct":  c.DistanceToVWAPPct,
			"atr_pct":               c.ATRPct,
			"extension_atr":         c.ExtensionATR,
			"spread_bps":            c.SpreadBps,
			"book_imbalance":        c.BookImbalance,
			"ofi_z":                 c.OFIZ,
			"combined_score":        c.CombinedScore,
			"trade_quality":         c.TradeQuality,
			"candidate_age_seconds": c.CandidateAgeSeconds,
		},
		Categories: map[string]string{
			"side":          c.Side,
			"strategy_id":   firstNonEmpty(c.StrategyID, c.Strat, "unknown"),
			"setup_family":  firstNonEmpty(c.SetupFamily, "unknown"),
			"session_label": firstNonEmpty(c.SessionLabel, "unknown"),
			"entry_timing":  firstNonEmpty(c.EntryTiming, "unknown"),
		},
	}
}

func adjustRankWithML(ruleRank float64, resp mlscore.ScoreResponse) float64 {
	if !resp.Enabled {
		return ruleRank
	}
	mlEdge := 0.0
	mlEdge += (resp.TakeTradeProbability - 0.50) * 20.0
	mlEdge += resp.ExpectedR * 5.0
	if resp.StopoutThenReclaimProb > 0.65 {
		mlEdge -= 5.0
	}
	return ruleRank + mlEdge
}

func candidateMLID(c candidate) string {
	return fmt.Sprintf("%s|%s|%s|%.4f", strings.ToUpper(aster.RawSymbol(c.Entry.Symbol)), strings.ToUpper(strings.TrimSpace(c.Side)), firstNonEmpty(c.Strat, c.StrategyID, "unknown"), c.Entry.Rank)
}
