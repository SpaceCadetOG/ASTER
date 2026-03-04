package levels

import (
	"time"

	"go-machine/internal/features"
)

type FailedAuctionMagnet struct {
	Level     float64
	Direction string // up/down
	CreatedAt time.Time
	FixedAt   time.Time
	Active    bool
}

func DetectFailedAuctionMagnet(c []features.Candle, lookback int, tolPct float64) (FailedAuctionMagnet, bool) {
	if len(c) < 5 {
		return FailedAuctionMagnet{}, false
	}
	if lookback <= 0 || lookback > len(c)-1 {
		lookback = minInt(len(c)-1, 20)
	}
	if tolPct <= 0 {
		tolPct = 0.10
	}
	start := len(c) - lookback - 1
	if start < 0 {
		start = 0
	}
	last := c[len(c)-1]
	for i := start; i < len(c)-2; i++ {
		for j := i + 1; j < len(c)-1; j++ {
			hiTol := c[i].H * tolPct / 100.0
			loTol := c[i].L * tolPct / 100.0
			if absf(c[i].H-c[j].H) <= hiTol {
				if last.C < c[j].L {
					m := FailedAuctionMagnet{Level: (c[i].H + c[j].H) / 2.0, Direction: "down", CreatedAt: c[j].Ts, Active: true}
					if fixedAt, ok := magnetFixedAt(c[j+1:], m.Level, true); ok {
						m.FixedAt = fixedAt
						m.Active = false
					}
					return m, true
				}
			}
			if absf(c[i].L-c[j].L) <= loTol {
				if last.C > c[j].H {
					m := FailedAuctionMagnet{Level: (c[i].L + c[j].L) / 2.0, Direction: "up", CreatedAt: c[j].Ts, Active: true}
					if fixedAt, ok := magnetFixedAt(c[j+1:], m.Level, false); ok {
						m.FixedAt = fixedAt
						m.Active = false
					}
					return m, true
				}
			}
		}
	}
	return FailedAuctionMagnet{}, false
}

func MagnetAgainstSide(m FailedAuctionMagnet, side features.Side, entry float64) bool {
	if !m.Active || m.Level <= 0 || entry <= 0 {
		return false
	}
	if side == features.SideLong {
		return m.Level > entry
	}
	return m.Level < entry
}

func magnetFixedAt(c []features.Candle, lvl float64, breakAbove bool) (time.Time, bool) {
	for _, x := range c {
		if breakAbove {
			if x.C > lvl {
				return x.Ts, true
			}
		} else {
			if x.C < lvl {
				return x.Ts, true
			}
		}
	}
	return time.Time{}, false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
