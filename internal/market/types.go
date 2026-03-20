package market

type Market struct {
	Exchange    string
	Symbol      string
	Change24h   float64
	Change4h    *float64
	Change30m   *float64
	Change5m    *float64
	DayUTC24h   *float64
	UTC4hPct    *float64
	UTC1hPct    *float64
	VolumeUSD   float64
	OIUSD       *float64
	FundingRate *float64
	LongsPct    *float64

	OpenPrice float64
	Open4hUTC float64
	Open1hUTC float64
	LastPrice float64

	SpreadBps      *float64
	TopBookUSD     *float64
	EstSlippageBps *float64
	LastTickerTs   *int64
	LastBookTs     *int64
	LastOITs       *int64
	LastFundingTs  *int64
}

type Scored struct {
	Market
	Eligible bool
	Reason   string
	Score    float64 // legacy active score used by existing code paths

	RawScore           float64
	NormalizedScore    float64
	Grade              string
	Completeness       float64
	IntegrityPenalty   float64
	ExecutionPenalty   float64
	SpreadBps          float64
	EstSlippageBps     float64
	TopBookUSDVal      float64
	Momentum5m         float64
	Momentum30m        float64
	Momentum4h         float64
	Momentum24h        float64
	MomentumAgreement  float64
	Regime             string
	Confidence         float64
	Uncertainty        float64
	StalenessPenalty   float64
	StateBoostRaw      float64
	StateBoostDecayed  float64
	ReliabilityAdj     float64
	DataFlags          []string
	ReversalReadyLong  bool
	ReversalReadyShort bool
	ReversalSignal     ReversalSignal
}

// ReversalSignal is lightweight scaffolding for improved reversal gating.
// It is intentionally conservative and deterministic.
type ReversalSignal struct {
	Ready     bool
	Direction string // BUY / SELL
	Score     float64
	Reasons   []string
}

// scoring weights
const (
	W_CHANGE      = 1.0
	W_LOG_VOL     = 8.0
	W_LOG_OI      = 3.0
	FUND_K        = 500.0
	CROWD_LONG_P  = 0.80
	CROWD_PENALTY = 10.0
)
