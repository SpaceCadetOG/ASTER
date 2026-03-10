package risk

import (
	"math"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Enabled              bool
	MinLiqBufferMult     float64
	MaxFundingCostR      float64
	MaxSpreadBps         float64
	MaxSpreadBySession   map[string]float64
	MinBookImbalance     float64
	MinTopBookUSD        float64
	TargetClipUSD        float64
	MaxRecentSlippageBps float64
	LiqLeverage          float64
	FundingHoldHours     float64
	MaxEntriesPerHour    int
	StopoutLookback      time.Duration
	StopoutLockThreshold int
	SymbolLockDuration   time.Duration
	Clock                func() time.Time
}

type Input struct {
	Symbol            string
	Session           string
	Side              string
	Entry             float64
	Stop              float64
	Leverage          float64
	NotionalUSD       float64
	FundingRate       float64
	HoldHours         float64
	SpreadBps         float64
	TopBookUSD        float64
	BookImbalance     float64
	EstSlippageBps    float64
	RecentSlippageBps float64
	VenueHealthy      bool
	RecordEntry       bool
	EntriesLastHour   int
	SymbolStopouts90m int
}

type Decision struct {
	Approved      bool
	RejectReason  string
	LiqBufferOK   bool
	LiqBufferMult float64
	FundingCostR  float64
}

type RiskShell struct {
	cfg Config

	mu              sync.Mutex
	entriesByHour   map[string]int
	stopoutsBySym   map[string][]time.Time
	lockUntilBySym  map[string]time.Time
	lastPruneByHour string
}

func DefaultConfig() Config {
	return Config{
		Enabled:              true,
		MinLiqBufferMult:     2.5,
		MaxFundingCostR:      0.25,
		MaxSpreadBps:         15,
		MinBookImbalance:     1.02,
		MinTopBookUSD:        25_000,
		TargetClipUSD:        1_000,
		MaxRecentSlippageBps: 12,
		LiqLeverage:          10,
		FundingHoldHours:     2,
		MaxEntriesPerHour:    2,
		StopoutLookback:      90 * time.Minute,
		StopoutLockThreshold: 2,
		SymbolLockDuration:   45 * time.Minute,
	}
}

func NewRiskShell(cfg Config) *RiskShell {
	cfg = normalizeConfig(cfg)
	return &RiskShell{
		cfg:            cfg,
		entriesByHour:  map[string]int{},
		stopoutsBySym:  map[string][]time.Time{},
		lockUntilBySym: map[string]time.Time{},
	}
}

// Approve is the stateful shell decision.
func (s *RiskShell) Approve(in Input) Decision {
	if s == nil {
		sh := NewRiskShell(DefaultConfig())
		return sh.Approve(in)
	}
	cfg := s.cfg
	if !cfg.Enabled {
		return Decision{Approved: true, LiqBufferOK: true}
	}
	now := cfg.nowUTC()
	s.prune(now)

	symbol := strings.ToUpper(strings.TrimSpace(in.Symbol))
	if in.EntriesLastHour > 0 && in.EntriesLastHour >= cfg.MaxEntriesPerHour {
		return Decision{RejectReason: "hourly_entry_cap"}
	}
	if in.SymbolStopouts90m > 0 && in.SymbolStopouts90m >= cfg.StopoutLockThreshold {
		return Decision{RejectReason: "symbol_lockout"}
	}
	if symbol != "" {
		if until, ok := s.lockUntilBySym[symbol]; ok && now.Before(until) {
			return Decision{RejectReason: "symbol_lockout"}
		}
	}

	if !in.VenueHealthy {
		return Decision{RejectReason: "venue_unhealthy"}
	}
	if in.Entry <= 0 || in.Stop <= 0 {
		return Decision{RejectReason: "invalid_risk_input"}
	}

	maxSpread := cfg.MaxSpreadBps
	if in.Session != "" {
		if v, ok := cfg.MaxSpreadBySession[strings.ToUpper(strings.TrimSpace(in.Session))]; ok && v > 0 {
			maxSpread = v
		}
	}
	if in.SpreadBps > 0 && in.SpreadBps > maxSpread {
		return Decision{RejectReason: "spread_too_wide"}
	}

	reqDepth := maxFloat(cfg.MinTopBookUSD, cfg.TargetClipUSD)
	if in.NotionalUSD > 0 {
		reqDepth = maxFloat(reqDepth, in.NotionalUSD)
	}
	if in.TopBookUSD > 0 && in.TopBookUSD < reqDepth {
		return Decision{RejectReason: "depth_too_thin"}
	}
	if in.BookImbalance > 0 && in.BookImbalance < cfg.MinBookImbalance {
		return Decision{RejectReason: "depth_imbalance_thin"}
	}

	slip := in.EstSlippageBps
	if slip <= 0 {
		slip = in.RecentSlippageBps
	}
	if slip > cfg.MaxRecentSlippageBps {
		return Decision{RejectReason: "slippage_anomaly"}
	}

	stopDistPct := math.Abs((in.Entry-in.Stop)/in.Entry) * 100.0
	if stopDistPct <= 0 {
		return Decision{RejectReason: "invalid_stop_distance"}
	}
	liqLev := cfg.LiqLeverage
	if liqLev <= 0 {
		liqLev = 10
	}
	liqDistPct := approxLiqDistancePct(liqLev)
	liqMult := liqDistPct / stopDistPct
	if liqMult < cfg.MinLiqBufferMult {
		return Decision{
			RejectReason:  "liq_buffer_violation",
			LiqBufferOK:   false,
			LiqBufferMult: liqMult,
		}
	}

	fundingCostR := fundingCostInR(in, cfg.FundingHoldHours)
	if fundingCostR > cfg.MaxFundingCostR {
		return Decision{
			RejectReason: "funding_too_expensive",
			LiqBufferOK:  true,
			FundingCostR: fundingCostR,
		}
	}

	if in.RecordEntry {
		s.recordEntry(now)
	}
	return Decision{
		Approved:      true,
		LiqBufferOK:   true,
		LiqBufferMult: liqMult,
		FundingCostR:  fundingCostR,
	}
}

// RecordExit keeps anti-overtrading state synchronized with fills.
func (s *RiskShell) RecordExit(symbol, reason string, at time.Time) {
	if s == nil {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(reason), "SL") &&
		!strings.EqualFold(strings.TrimSpace(reason), "STOP") {
		return
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return
	}
	if at.IsZero() {
		at = s.cfg.nowUTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopoutsBySym[symbol] = append(s.stopoutsBySym[symbol], at.UTC())
	lookback := s.cfg.StopoutLookback
	if lookback <= 0 {
		lookback = 90 * time.Minute
	}
	cut := at.Add(-lookback)
	pruned := s.stopoutsBySym[symbol][:0]
	for _, t := range s.stopoutsBySym[symbol] {
		if !t.Before(cut) {
			pruned = append(pruned, t)
		}
	}
	s.stopoutsBySym[symbol] = pruned
	if len(pruned) >= s.cfg.StopoutLockThreshold {
		s.lockUntilBySym[symbol] = at.Add(s.cfg.SymbolLockDuration)
	}
}

// Approve provides backward compatibility with prior stateless callers.
func Approve(cfg Config, in Input) Decision {
	sh := NewRiskShell(cfg)
	in.RecordEntry = false
	return sh.Approve(in)
}

func normalizeConfig(cfg Config) Config {
	def := DefaultConfig()
	if cfg.MinLiqBufferMult <= 0 {
		cfg.MinLiqBufferMult = def.MinLiqBufferMult
	}
	if cfg.MaxFundingCostR <= 0 {
		cfg.MaxFundingCostR = def.MaxFundingCostR
	}
	if cfg.MaxSpreadBps <= 0 {
		cfg.MaxSpreadBps = def.MaxSpreadBps
	}
	if cfg.MinBookImbalance <= 0 {
		cfg.MinBookImbalance = def.MinBookImbalance
	}
	if cfg.MinTopBookUSD <= 0 {
		cfg.MinTopBookUSD = def.MinTopBookUSD
	}
	if cfg.TargetClipUSD <= 0 {
		cfg.TargetClipUSD = def.TargetClipUSD
	}
	if cfg.MaxRecentSlippageBps <= 0 {
		cfg.MaxRecentSlippageBps = def.MaxRecentSlippageBps
	}
	if cfg.LiqLeverage <= 0 {
		cfg.LiqLeverage = def.LiqLeverage
	}
	if cfg.FundingHoldHours <= 0 {
		cfg.FundingHoldHours = def.FundingHoldHours
	}
	if cfg.MaxEntriesPerHour <= 0 {
		cfg.MaxEntriesPerHour = def.MaxEntriesPerHour
	}
	if cfg.StopoutLookback <= 0 {
		cfg.StopoutLookback = def.StopoutLookback
	}
	if cfg.StopoutLockThreshold <= 0 {
		cfg.StopoutLockThreshold = def.StopoutLockThreshold
	}
	if cfg.SymbolLockDuration <= 0 {
		cfg.SymbolLockDuration = def.SymbolLockDuration
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.MaxSpreadBySession == nil {
		cfg.MaxSpreadBySession = map[string]float64{}
	}
	return cfg
}

func (c Config) nowUTC() time.Time {
	if c.Clock == nil {
		return time.Now().UTC()
	}
	return c.Clock().UTC()
}

func (s *RiskShell) recordEntry(now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hourKey := now.UTC().Format("2006-01-02T15")
	s.entriesByHour[hourKey] = s.entriesByHour[hourKey] + 1
}

func (s *RiskShell) prune(now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Keep only current hour and prior hour counters.
	curHour := now.UTC().Format("2006-01-02T15")
	if s.lastPruneByHour != curHour {
		prev := now.Add(-time.Hour).UTC().Format("2006-01-02T15")
		for k := range s.entriesByHour {
			if k != curHour && k != prev {
				delete(s.entriesByHour, k)
			}
		}
		s.lastPruneByHour = curHour
	}
	for sym, until := range s.lockUntilBySym {
		if !now.Before(until) {
			delete(s.lockUntilBySym, sym)
		}
	}
	cut := now.Add(-s.cfg.StopoutLookback)
	for sym, ts := range s.stopoutsBySym {
		pruned := ts[:0]
		for _, t := range ts {
			if !t.Before(cut) {
				pruned = append(pruned, t)
			}
		}
		if len(pruned) == 0 {
			delete(s.stopoutsBySym, sym)
			continue
		}
		s.stopoutsBySym[sym] = pruned
	}
}

func approxLiqDistancePct(leverage float64) float64 {
	if leverage <= 0 {
		return 0
	}
	// Conservative approximation for perps with maintenance margin buffer.
	return (100.0 / leverage) * 0.9
}

func fundingCostInR(in Input, defaultHoldHours float64) float64 {
	if in.NotionalUSD <= 0 || in.Entry <= 0 || in.Stop <= 0 {
		return 0
	}
	stopDistPct := math.Abs((in.Entry-in.Stop)/in.Entry) * 100.0
	riskUSD := in.NotionalUSD * (stopDistPct / 100.0)
	if riskUSD <= 0 {
		return 0
	}
	holdH := in.HoldHours
	if holdH <= 0 {
		holdH = defaultHoldHours
	}
	if holdH <= 0 {
		holdH = 2
	}
	intervals := holdH / 8.0
	if intervals <= 0 {
		intervals = 1
	}
	fr := in.FundingRate
	if fundingAgainstSide(fr, in.Side) {
		cost := math.Abs(fr) * in.NotionalUSD * intervals
		return cost / riskUSD
	}
	return 0
}

func fundingAgainstSide(fr float64, side string) bool {
	if strings.EqualFold(side, "BUY") {
		return fr > 0
	}
	if strings.EqualFold(side, "SELL") {
		return fr < 0
	}
	return false
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
