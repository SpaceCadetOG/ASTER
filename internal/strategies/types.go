package strategies

import (
	"time"

	"go-machine/internal/features"
	"go-machine/internal/indicators"
)

type StrategyID string

const (
	StrategyUnknown              StrategyID = "unknown"
	StrategyImpulseContinuation  StrategyID = "impulse_continuation"
	StrategyAnchoredVWAPPullback StrategyID = "anchored_vwap_pullback"
	StrategyVPRetest             StrategyID = "vp_retest"
)

type Side string

const (
	SideLong  Side = "long"
	SideShort Side = "short"
)

type VolumeProfileSnapshot struct {
	POC           float64
	ValueAreaHigh float64
	ValueAreaLow  float64
	NearestAbove  float64
	NearestBelow  float64
	WidthBps      float64
	Shape         string
	HasResistance bool
	HasSupport    bool
}

type FlowSnapshot struct {
	Delta                float64
	CumDelta             float64
	DeltaDivBull         bool
	DeltaDivBear         bool
	AbsorptionBull       bool
	AbsorptionBear       bool
	StackedImbalanceBull bool
	StackedImbalanceBear bool
	UnfinishedBusinessUp bool
	UnfinishedBusinessDn bool
	Confidence           float64
	Summary              string
}

type TrendSnapshot struct {
	TF1mDir         string
	TF3mDir         string
	TF5mDir         string
	TF15mDir        string
	Slope1m         float64
	Slope5m         float64
	Slope15m        float64
	AboveVWAP       bool
	AboveEMA20      bool
	AboveEMA50      bool
	BelowVWAP       bool
	BelowEMA20      bool
	BelowEMA50      bool
	Compression     bool
	ImpulseUp       bool
	ImpulseDown     bool
	BreakoutLevel   float64
	BreakdownLevel  float64
	CompressionHigh float64
	CompressionLow  float64
}

type Target struct {
	Label string
	Price float64
	Size  float64
}

type EntryIntent struct {
	Strategy        StrategyID
	Symbol          string
	Side            Side
	Timeframe       string
	Confidence      float64
	Score           float64
	TriggerPrice    float64
	Invalidation    float64
	StopPrice       float64
	Targets         []Target
	TimeStopBars    int
	ReasonCodes     []string
	RequiresConfirm []string
	Features        map[string]float64
	CreatedAt       time.Time
}

type StrategyContext struct {
	Symbol         string
	Now            time.Time
	MarkPrice      float64
	IndexPrice     float64
	LastPrice      float64
	SpreadBps      float64
	VolumeRatio    float64
	OIChangePct    float64
	CandidateScore float64
	SessionVWAP    float64
	WeeklyVWAP     float64
	VWAPDistBps    float64
	AnchoredVWAP   indicators.AnchoredVWAPSnapshot
	AVWAPLabel     string
	VolumeProfile  VolumeProfileSnapshot
	Flow           FlowSnapshot
	Trend          TrendSnapshot
	MarketRegime   string
	WatchlistTier  string
	AutoEntryTier  string
	Raw            any
}

type EntryDecision struct {
	Allowed      bool
	Intent       *EntryIntent
	RejectReason string
	RejectCodes  []string
	FinalScore   float64
	HardBlocks   []string
}

type Signal struct {
	Active          bool
	Name            string
	Side            features.Side
	Entry           float64
	Stop            float64
	TP1             float64
	TP2             float64
	TP3             float64
	Confidence      float64
	Tags            []string
	Invalidation    string
	VPSetup         string
	VPLevel         float64
	VPTargetLevel   float64
	StopMode        string
	TargetMode      string
	RejectReason    string
	Reasons         []string
	Confluence      map[string]float64
	ConfluenceScore ConfluenceScore
	RegimeTag       string
	SignalSource    []string
	WallMode        string
	WallStatus      string
	WallConfidence  float64
	WallBiasScore   float64
	WallSpoofRisk   float64
	WallReasons     []string
	Ts              time.Time
}

type ConfluenceScore struct {
	TotalScore     float64
	StrategyScore  float64
	FlowScore      float64
	StructureScore float64
	Reasons        []string
	Approved       bool
}

type Context struct {
	Symbol            string
	TF                string
	ScannerScore      float64
	ScannerGrade      string
	ScoreSlope        float64
	DayUTCPct         float64
	UTC4hPct          float64
	UTC1hPct          float64
	EntryStyle        string
	MetaState         string
	ScanAccel         float64
	NotionalUSD       float64
	FundingRate       float64
	SpreadBps         float64
	TopBookUSD        float64
	EstSlippageBps    float64
	RecentSlippageBps float64
	VenueHealthy      bool
	VenueHealthKnown  bool
	EntriesLastHour   int
	SymbolStopouts90m int
	Snapshot          features.Snapshot
	Candles           []features.Candle
	Runtime           *RuntimeSignalContext
}

type Strategy interface {
	Name() string
	Eval(ctx Context) Signal
}

type RuntimeSignalContext struct {
	RequestedStrategy     string
	Side                  features.Side
	CandidateState        string
	LastClose             float64
	EMA9                  float64
	SessionVWAP           float64
	FastSlope             float64
	SlowSlope             float64
	OFIZ                  float64
	OFISamples            int
	ATRPct                float64
	FailedReclaimCount    int
	FailedBounceCount     int
	FailedBreakdownCount  int
	FailedBreakLowCount   int
	BarsSincePeak         int
	BarsSinceTrough       int
	DrawdownFromPeakPct   float64
	DrawupFromTroughPct   float64
	IntradayReversalScore float64
	BullReversalScore     float64
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
