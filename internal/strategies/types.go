package strategies

import (
	"time"

	"go-machine/internal/features"
)

type Signal struct {
	Active        bool
	Name          string
	Side          features.Side
	Entry         float64
	Stop          float64
	TP1           float64
	TP2           float64
	Confidence    float64
	Tags          []string
	Invalidation  string
	VPSetup       string
	VPLevel       float64
	VPTargetLevel float64
	StopMode      string
	TargetMode    string
	RejectReason  string
	Reasons       []string
	Confluence    map[string]float64
	RegimeTag     string
	SignalSource  []string
	Ts            time.Time
}

type Context struct {
	Symbol       string
	TF           string
	ScannerScore float64
	ScannerGrade string
	ScoreSlope   float64
	ScanAccel    float64
	Snapshot     features.Snapshot
	Candles      []features.Candle
}

type Strategy interface {
	Name() string
	Eval(ctx Context) Signal
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func rrTargets(entry, stop float64, side features.Side) (float64, float64) {
	r := entry - stop
	if side == features.SideShort {
		r = stop - entry
	}
	if r <= 0 {
		return 0, 0
	}
	if side == features.SideLong {
		return entry + 2*r, entry + 3*r
	}
	return entry - 2*r, entry - 3*r
}

func sideFromTrend(t features.TrendState) features.Side {
	if t == features.TrendBear {
		return features.SideShort
	}
	return features.SideLong
}
