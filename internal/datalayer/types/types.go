package types

import "time"

type Meta struct {
	Source  string    `json:"source"`
	Stale   bool      `json:"stale"`
	Partial bool      `json:"partial,omitempty"`
	Ts      time.Time `json:"ts"`
	Error   string    `json:"error,omitempty"`
}

type Health struct {
	Status     string               `json:"status"`
	StartedAt  time.Time            `json:"started_at"`
	Components map[string]Component `json:"components"`
	Ts         time.Time            `json:"ts"`
}

type Component struct {
	Status string    `json:"status"`
	Detail string    `json:"detail,omitempty"`
	Stale  bool      `json:"stale,omitempty"`
	Ts     time.Time `json:"ts"`
}

type Price struct {
	Symbol       string    `json:"symbol"`
	Bid          float64   `json:"bid"`
	Ask          float64   `json:"ask"`
	Mid          float64   `json:"mid"`
	SpreadBps    float64   `json:"spread_bps"`
	FundingRate  *float64  `json:"funding_rate,omitempty"`
	OpenInterest *float64  `json:"open_interest,omitempty"`
	Ts           time.Time `json:"ts"`
}

type PricesResponse struct {
	Meta   Meta    `json:"meta"`
	Prices []Price `json:"prices"`
}

type PriceResponse struct {
	Meta  Meta  `json:"meta"`
	Price Price `json:"price"`
}

type BookLevel struct {
	Price float64 `json:"price"`
	Qty   float64 `json:"qty"`
}

type OrderBook struct {
	Symbol    string      `json:"symbol"`
	Bids      []BookLevel `json:"bids"`
	Asks      []BookLevel `json:"asks"`
	TopBid    float64     `json:"top_bid"`
	TopAsk    float64     `json:"top_ask"`
	SpreadBps float64     `json:"spread_bps"`
	Ts        time.Time   `json:"ts"`
}

type OrderBookResponse struct {
	Meta      Meta      `json:"meta"`
	OrderBook OrderBook `json:"orderbook"`
}

type Candle struct {
	Ts     time.Time `json:"ts"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume float64   `json:"volume"`
}

type Candles struct {
	Symbol    string   `json:"symbol"`
	Timeframe string   `json:"timeframe"`
	Candles   []Candle `json:"candles"`
}

type CandlesResponse struct {
	Meta    Meta    `json:"meta"`
	Candles Candles `json:"candles"`
}

type Position struct {
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Qty         float64 `json:"qty"`
	EntryPrice  float64 `json:"entry_price"`
	MarkPrice   float64 `json:"mark_price"`
	Margin      float64 `json:"margin"`
	Leverage    float64 `json:"leverage"`
	Unrealized  float64 `json:"unrealized_pnl"`
	NotionalUSD float64 `json:"notional_usd"`
}

type OpenOrder struct {
	Symbol  string    `json:"symbol"`
	Side    string    `json:"side"`
	Type    string    `json:"type"`
	Price   float64   `json:"price"`
	Qty     float64   `json:"qty"`
	Status  string    `json:"status"`
	OrderID int64     `json:"order_id"`
	Reduce  bool      `json:"reduce_only"`
	Ts      time.Time `json:"ts"`
}

type Account struct {
	Equity        float64     `json:"equity"`
	AvailableUSDT float64     `json:"available_usdt"`
	OpenPnL       float64     `json:"open_pnl"`
	Positions     []Position  `json:"positions"`
	OpenOrders    []OpenOrder `json:"open_orders,omitempty"`
	Ts            time.Time   `json:"ts"`
}

type AccountResponse struct {
	Meta    Meta    `json:"meta"`
	Account Account `json:"account"`
}

type PositionsResponse struct {
	Meta      Meta       `json:"meta"`
	Positions []Position `json:"positions"`
}

type Fill struct {
	Symbol      string    `json:"symbol"`
	Side        string    `json:"side"`
	Price       float64   `json:"price"`
	Qty         float64   `json:"qty"`
	Fee         float64   `json:"fee"`
	RealizedPnL *float64  `json:"realized_pnl,omitempty"`
	Ts          time.Time `json:"ts"`
}

type FillsResponse struct {
	Meta  Meta   `json:"meta"`
	Fills []Fill `json:"fills"`
}

type Event struct {
	Symbol string    `json:"symbol"`
	Side   string    `json:"side"`
	Price  float64   `json:"price,omitempty"`
	Qty    float64   `json:"qty,omitempty"`
	USD    float64   `json:"usd"`
	Ts     time.Time `json:"ts"`
	Label  string    `json:"label,omitempty"`
	Score  float64   `json:"score,omitempty"`
}

type EventSummary struct {
	Symbol       string    `json:"symbol"`
	Count        int       `json:"count"`
	TotalUSD     float64   `json:"total_usd"`
	BuyUSD       float64   `json:"buy_usd"`
	SellUSD      float64   `json:"sell_usd"`
	DeltaUSD     float64   `json:"delta_usd"`
	BuyPct       float64   `json:"buy_pct"`
	SellPct      float64   `json:"sell_pct"`
	LargeCount   int       `json:"large_count,omitempty"`
	DominantSide string    `json:"dominant_side,omitempty"`
	LastUSD      float64   `json:"last_usd,omitempty"`
	LastSide     string    `json:"last_side,omitempty"`
	LastTs       time.Time `json:"last_ts,omitempty"`
}

type LiquidationsResponse struct {
	Meta    Meta           `json:"meta"`
	Summary EventSummary   `json:"summary"`
	Assets  []EventSummary `json:"assets"`
	Events  []Event        `json:"events"`
}

type WhalesResponse struct {
	Meta    Meta           `json:"meta"`
	Summary EventSummary   `json:"summary"`
	Assets  []EventSummary `json:"assets"`
	Events  []Event        `json:"events"`
}

type OrderFlow struct {
	Symbol  string       `json:"symbol"`
	Score   float64      `json:"score"`
	Signal  string       `json:"signal"`
	Summary EventSummary `json:"summary"`
	Recent  []Event      `json:"recent"`
}

type OrderFlowResponse struct {
	Meta      Meta      `json:"meta"`
	OrderFlow OrderFlow `json:"orderflow"`
}
