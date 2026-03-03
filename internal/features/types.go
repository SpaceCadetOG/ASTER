package features

import "time"

type Candle struct {
	Ts time.Time
	O  float64
	H  float64
	L  float64
	C  float64
	V  float64
}

type Side string

const (
	SideLong  Side = "long"
	SideShort Side = "short"
)

type Pivot struct {
	Index int
	Ts    time.Time
	Price float64
	High  bool
}

type TrendState string

const (
	TrendBull  TrendState = "bull"
	TrendBear  TrendState = "bear"
	TrendRange TrendState = "range"
)

type StructureState struct {
	Trend         TrendState
	LastSwingHigh *Pivot
	LastSwingLow  *Pivot
	LastBOSTs     time.Time
	LastCHOCHTs   time.Time
	Label         string // HH/HL/LH/LL
}

type LiquidityPool struct {
	Side   Side
	Level  float64
	Count  int
	First  time.Time
	Last   time.Time
	Active bool
}

type SweepEvent struct {
	Side            Side
	Level           float64
	WickPct         float64
	CloseBackInside bool
	Strength        float64
	Ts              time.Time
}

type FVGZone struct {
	Side      Side
	Low       float64
	High      float64
	CreatedTs time.Time
	AgeBars   int
	Mitigated bool
	Active    bool
}

type OBZone struct {
	Side      Side
	Low       float64
	High      float64
	CreatedTs time.Time
	Touched   bool
	Rejected  bool
	Active    bool
}

type FlowState struct {
	VolumeZ           float64
	VolumeSpike       bool
	WhaleDelta1m      float64
	WhaleDeltaCum     float64
	WhaleBuyPct       float64
	WhaleSellPct      float64
	LargeTradeCount1m int
	LargeBuyCount1m   int
	LargeSellCount1m  int
}

type AnchorLevels struct {
	DailyOpen   float64
	SessionOpen map[string]float64
	PDH         float64
	PDL         float64
	PWH         float64
	PWL         float64
	Session     string
}

type Snapshot struct {
	Candle    Candle
	Structure StructureState
	Pools     []LiquidityPool
	Sweep     *SweepEvent
	FVGs      []FVGZone
	OBs       []OBZone
	Flow      FlowState
	Anchors   AnchorLevels
}
