package main

import "time"

func registerReentryExit(symbol, strategyID, side, reason string, score, maxFavorR float64, wasLoss bool, now time.Time) {
	_, _, _, _, _, _, _, _ = symbol, strategyID, side, reason, score, maxFavorR, wasLoss, now
}

func clearReentryLoss(symbol string) {
	_ = symbol
}

func isSoftChurnExit(reason string) bool {
	_ = reason
	return false
}
