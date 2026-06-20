package execution

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type ReentryRecord struct {
	LastLossTime      time.Time
	LossCount         int
	LastStrategy      string
	LastSide          string
	LastStopScore     float64
	LastExitTime      time.Time
	LastExitReason    string
	LastExitWasLoss   bool
	LastExitMaxFavorR float64
}

func ShouldBlockReentry(
	symbol string,
	strategyID string,
	side string,
	rec ReentryRecord,
	now time.Time,
	currentScore float64,
) (bool, string) {
	_ = symbol
	cooldownMin := envIntRG("LIVE_REENTRY_LOSS_COOLDOWN_MIN", 15)
	maxLosses := envIntRG("LIVE_REENTRY_MAX_LOSSES_PER_WINDOW", 2)
	cooldown := time.Duration(cooldownMin) * time.Minute

	if rec.LossCount >= maxLosses && !rec.LastLossTime.IsZero() && now.Sub(rec.LastLossTime) < cooldown {
		return true, "same_symbol_loss_limit"
	}

	if strings.EqualFold(rec.LastStrategy, strategyID) &&
		strings.EqualFold(rec.LastSide, side) &&
		!rec.LastLossTime.IsZero() &&
		now.Sub(rec.LastLossTime) < cooldown {
		if envBoolRG("LIVE_REENTRY_REQUIRE_STRONGER_SCORE", true) {
			delta := envFloatRG("LIVE_REENTRY_STRONGER_SCORE_DELTA", 5.0)
			if currentScore < rec.LastStopScore+delta {
				return true, "same_setup_cooldown"
			}
		} else {
			return true, "same_setup_cooldown"
		}
	}

	softCooldownMin := envIntRG("LIVE_REENTRY_SOFT_EXIT_COOLDOWN_MIN", 20)
	softCooldown := time.Duration(softCooldownMin) * time.Minute
	if isSoftChurnExit(rec.LastExitReason) &&
		strings.EqualFold(rec.LastStrategy, strategyID) &&
		strings.EqualFold(rec.LastSide, side) &&
		!rec.LastExitTime.IsZero() &&
		now.Sub(rec.LastExitTime) < softCooldown {
		needStronger := envBoolRG("LIVE_REENTRY_REQUIRE_STRONGER_SCORE_AFTER_SOFT_EXIT", true)
		if !needStronger {
			return true, "same_setup_soft_exit_cooldown"
		}
		delta := envFloatRG("LIVE_REENTRY_SOFT_EXIT_STRONGER_SCORE_DELTA", 7.5)
		if currentScore < rec.LastStopScore+delta {
			return true, "same_setup_soft_exit_cooldown"
		}
	}
	return false, ""
}

func isSoftChurnExit(reason string) bool {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "MOMENTUM_FADE", "PROFIT_GIVEBACK":
		return true
	default:
		return false
	}
}

func envIntRG(key string, def int) int {
	v := os.Getenv(key)
	if strings.TrimSpace(v) == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func envFloatRG(key string, def float64) float64 {
	v := os.Getenv(key)
	if strings.TrimSpace(v) == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envBoolRG(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
