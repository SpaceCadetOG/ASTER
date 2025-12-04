package market

type Market struct {
	Exchange    string
	Symbol      string
	Change24h   float64
	VolumeUSD   float64
	OIUSD       *float64
	FundingRate *float64
	LongsPct    *float64

	OpenPrice float64
	LastPrice float64
}

type Scored struct {
	Market
	Eligible bool
	Reason   string
	Score    float64
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
