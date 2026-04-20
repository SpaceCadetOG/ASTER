package indicators

import (
	"math"
	"os"
	"strconv"
	"time"
)

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type AnchorType string

const (
	AnchorSession     AnchorType = "session"
	AnchorEvent       AnchorType = "event"
	AnchorLiquidation AnchorType = "liquidation"
	AnchorImpulse     AnchorType = "impulse"
)

type VWAPAnchor struct {
	Type      AnchorType
	Label     string
	StartTime time.Time
	Price     float64
	Metadata  map[string]string
}

type AnchoredVWAPSnapshot struct {
	Anchor      VWAPAnchor
	VWAP        float64
	Dev1Upper   float64
	Dev1Lower   float64
	Slope       float64
	DistanceBps float64
	Valid       bool
}

type MarketEvent struct {
	Time     time.Time
	Type     string
	Label    string
	Price    float64
	Severity float64
}

func ComputeAnchoredVWAP(candles []Candle, anchor VWAPAnchor, markPrice float64) AnchoredVWAPSnapshot {
	if len(candles) == 0 {
		return AnchoredVWAPSnapshot{Anchor: anchor, Valid: false}
	}
	var pxvSum float64
	var volSum float64
	var prices []float64
	for _, c := range candles {
		if c.Time.Before(anchor.StartTime) || c.Volume <= 0 {
			continue
		}
		typical := (c.High + c.Low + c.Close) / 3.0
		pxvSum += typical * c.Volume
		volSum += c.Volume
		prices = append(prices, typical)
	}
	if volSum <= 0 {
		return AnchoredVWAPSnapshot{Anchor: anchor, Valid: false}
	}
	vwap := pxvSum / volSum
	std := weightedStdFromCandles(candles, anchor.StartTime, vwap)
	devMult := envFloatAnchoredVWAP("LIVE_AVWAP_DEV_MULT", 1.0)
	dev1Upper := vwap + (std * devMult)
	dev1Lower := vwap - (std * devMult)
	var slope float64
	if len(prices) >= 2 {
		slope = prices[len(prices)-1] - prices[0]
	}
	var distanceBps float64
	if vwap > 0 && markPrice > 0 {
		distanceBps = ((markPrice - vwap) / vwap) * 10000.0
	}
	return AnchoredVWAPSnapshot{
		Anchor:      anchor,
		VWAP:        vwap,
		Dev1Upper:   dev1Upper,
		Dev1Lower:   dev1Lower,
		Slope:       slope,
		DistanceBps: distanceBps,
		Valid:       true,
	}
}

func SelectPrimaryAnchor(candles []Candle, events []MarketEvent) VWAPAnchor {
	if len(candles) == 0 {
		return VWAPAnchor{
			Type:      AnchorSession,
			Label:     "session_fallback",
			StartTime: time.Now().UTC(),
			Metadata:  map[string]string{"source": "fallback"},
		}
	}
	if envBoolAnchoredVWAP("LIVE_AVWAP_PREFER_EVENT_ANCHOR", true) && len(events) > 0 {
		best := events[0]
		for _, e := range events[1:] {
			if e.Severity > best.Severity {
				best = e
			}
		}
		return VWAPAnchor{
			Type:      AnchorEvent,
			Label:     best.Label,
			StartTime: best.Time,
			Price:     best.Price,
			Metadata: map[string]string{
				"event_type": best.Type,
				"source":     "market_event",
			},
		}
	}
	if envBoolAnchoredVWAP("LIVE_AVWAP_PREFER_IMPULSE_ANCHOR", true) {
		if anchor, ok := selectImpulseAnchor(candles); ok {
			return anchor
		}
	}
	return sessionAnchor(candles)
}

func sessionAnchor(candles []Candle) VWAPAnchor {
	first := candles[0]
	resetMode := envStringAnchoredVWAP("LIVE_AVWAP_SESSION_RESET", "utc")
	year, month, day := first.Time.UTC().Date()
	var start time.Time
	switch resetMode {
	case "utc":
		start = time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	default:
		start = time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
	for _, c := range candles {
		if !c.Time.Before(start) {
			return VWAPAnchor{
				Type:      AnchorSession,
				Label:     "session_open",
				StartTime: c.Time,
				Price:     c.Open,
				Metadata:  map[string]string{"source": "session"},
			}
		}
	}
	return VWAPAnchor{
		Type:      AnchorSession,
		Label:     "session_first_candle",
		StartTime: first.Time,
		Price:     first.Open,
		Metadata:  map[string]string{"source": "session_fallback"},
	}
}

func selectImpulseAnchor(candles []Candle) (VWAPAnchor, bool) {
	if len(candles) < 3 {
		return VWAPAnchor{}, false
	}
	bestIdx := -1
	bestScore := 0.0
	for i, c := range candles {
		if c.Volume <= 0 {
			continue
		}
		rangeSize := math.Abs(c.High - c.Low)
		body := math.Abs(c.Close - c.Open)
		score := rangeSize*c.Volume + body*c.Volume
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return VWAPAnchor{}, false
	}
	c := candles[bestIdx]
	return VWAPAnchor{
		Type:      AnchorImpulse,
		Label:     "impulse_candle",
		StartTime: c.Time,
		Price:     c.Close,
		Metadata: map[string]string{
			"source": "impulse_scan",
		},
	}, true
}

func weightedStdFromCandles(candles []Candle, start time.Time, mean float64) float64 {
	var volSum float64
	var varSum float64
	for _, c := range candles {
		if c.Time.Before(start) || c.Volume <= 0 {
			continue
		}
		typical := (c.High + c.Low + c.Close) / 3.0
		diff := typical - mean
		varSum += diff * diff * c.Volume
		volSum += c.Volume
	}
	if volSum <= 0 {
		return 0
	}
	return math.Sqrt(varSum / volSum)
}

func envBoolAnchoredVWAP(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return def
	}
}

func envFloatAnchoredVWAP(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envStringAnchoredVWAP(key string, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

