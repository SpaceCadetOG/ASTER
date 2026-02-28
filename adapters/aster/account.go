// account.go — v3 account, balance, trade history, and income endpoints.
//
// All Aster numeric fields are returned as JSON strings ("100.50") so we use
// intermediate raw structs and parse to float64.
package aster

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ---- Typed response structs ----

// AccountBalance is one asset entry from GET /fapi/v3/balance.
type AccountBalance struct {
	Asset            string  `json:"asset"`
	Balance          float64 `json:"-"`
	CrossWalletBal   float64 `json:"-"`
	AvailableBalance float64 `json:"-"`
	CrossUnPnl       float64 `json:"-"`
	MaxWithdrawAmt   float64 `json:"-"`
	MarginAvailable  bool    `json:"marginAvailable"`
	UpdateTime       int64   `json:"updateTime"`

	// raw string fields from exchange
	BalanceStr          string `json:"balance"`
	CrossWalletBalStr   string `json:"crossWalletBalance"`
	AvailableBalanceStr string `json:"availableBalance"`
	CrossUnPnlStr       string `json:"crossUnPnl"`
	MaxWithdrawAmtStr   string `json:"maxWithdrawAmount"`
}

func (b *AccountBalance) parse() {
	b.Balance, _ = strconv.ParseFloat(b.BalanceStr, 64)
	b.CrossWalletBal, _ = strconv.ParseFloat(b.CrossWalletBalStr, 64)
	b.AvailableBalance, _ = strconv.ParseFloat(b.AvailableBalanceStr, 64)
	b.CrossUnPnl, _ = strconv.ParseFloat(b.CrossUnPnlStr, 64)
	b.MaxWithdrawAmt, _ = strconv.ParseFloat(b.MaxWithdrawAmtStr, 64)
}

// AssetInfo is one entry in AccountInfo.Assets.
type AssetInfo struct {
	Asset            string  `json:"asset"`
	WalletBalance    float64 `json:"-"`
	UnrealizedProfit float64 `json:"-"`
	MarginBalance    float64 `json:"-"`
	AvailableBalance float64 `json:"-"`
	InitialMargin    float64 `json:"-"`
	MaintMargin      float64 `json:"-"`

	WalletBalanceStr    string `json:"walletBalance"`
	UnrealizedProfitStr string `json:"unrealizedProfit"`
	MarginBalanceStr    string `json:"marginBalance"`
	AvailableBalanceStr string `json:"availableBalance"`
	InitialMarginStr    string `json:"initialMargin"`
	MaintMarginStr      string `json:"maintMargin"`
}

func (a *AssetInfo) parse() {
	a.WalletBalance, _ = strconv.ParseFloat(a.WalletBalanceStr, 64)
	a.UnrealizedProfit, _ = strconv.ParseFloat(a.UnrealizedProfitStr, 64)
	a.MarginBalance, _ = strconv.ParseFloat(a.MarginBalanceStr, 64)
	a.AvailableBalance, _ = strconv.ParseFloat(a.AvailableBalanceStr, 64)
	a.InitialMargin, _ = strconv.ParseFloat(a.InitialMarginStr, 64)
	a.MaintMargin, _ = strconv.ParseFloat(a.MaintMarginStr, 64)
}

// PositionInfo is one entry in AccountInfo.Positions.
type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	PositionAmt      float64 `json:"-"`
	EntryPrice       float64 `json:"-"`
	MarkPrice        float64 `json:"-"`
	UnrealizedProfit float64 `json:"-"`
	Leverage         string  `json:"leverage"`
	PositionSide     string  `json:"positionSide"`
	MarginType       string  `json:"marginType"`
	Isolated         bool    `json:"isolated"`

	PositionAmtStr      string `json:"positionAmt"`
	EntryPriceStr       string `json:"entryPrice"`
	MarkPriceStr        string `json:"markPrice"`
	UnrealizedProfitStr string `json:"unrealizedProfit"`
}

func (p *PositionInfo) parse() {
	p.PositionAmt, _ = strconv.ParseFloat(p.PositionAmtStr, 64)
	p.EntryPrice, _ = strconv.ParseFloat(p.EntryPriceStr, 64)
	p.MarkPrice, _ = strconv.ParseFloat(p.MarkPriceStr, 64)
	p.UnrealizedProfit, _ = strconv.ParseFloat(p.UnrealizedProfitStr, 64)
}

// AccountInfo is the response from GET /fapi/v3/account.
type AccountInfo struct {
	FeeTier              int            `json:"feeTier"`
	CanTrade             bool           `json:"canTrade"`
	TotalWalletBalance   float64        `json:"-"`
	TotalUnrealizedProfit float64       `json:"-"`
	TotalMarginBalance   float64        `json:"-"`
	AvailableBalance     float64        `json:"-"`
	MaxWithdrawAmount    float64        `json:"-"`
	TotalInitialMargin   float64        `json:"-"`
	TotalMaintMargin     float64        `json:"-"`
	Assets               []AssetInfo    `json:"assets"`
	Positions            []PositionInfo `json:"positions"`

	TotalWalletBalanceStr    string `json:"totalWalletBalance"`
	TotalUnrealizedProfitStr string `json:"totalUnrealizedProfit"`
	TotalMarginBalanceStr    string `json:"totalMarginBalance"`
	AvailableBalanceStr      string `json:"availableBalance"`
	MaxWithdrawAmountStr     string `json:"maxWithdrawAmount"`
	TotalInitialMarginStr    string `json:"totalInitialMargin"`
	TotalMaintMarginStr      string `json:"totalMaintMargin"`
}

func (a *AccountInfo) parse() {
	a.TotalWalletBalance, _ = strconv.ParseFloat(a.TotalWalletBalanceStr, 64)
	a.TotalUnrealizedProfit, _ = strconv.ParseFloat(a.TotalUnrealizedProfitStr, 64)
	a.TotalMarginBalance, _ = strconv.ParseFloat(a.TotalMarginBalanceStr, 64)
	a.AvailableBalance, _ = strconv.ParseFloat(a.AvailableBalanceStr, 64)
	a.MaxWithdrawAmount, _ = strconv.ParseFloat(a.MaxWithdrawAmountStr, 64)
	a.TotalInitialMargin, _ = strconv.ParseFloat(a.TotalInitialMarginStr, 64)
	a.TotalMaintMargin, _ = strconv.ParseFloat(a.TotalMaintMarginStr, 64)
	for i := range a.Assets {
		a.Assets[i].parse()
	}
	for i := range a.Positions {
		a.Positions[i].parse()
	}
}

// UserTrade is one entry from GET /fapi/v3/userTrades.
type UserTrade struct {
	Symbol          string  `json:"symbol"`
	ID              int64   `json:"id"`
	OrderID         int64   `json:"orderId"`
	Side            string  `json:"side"`
	PositionSide    string  `json:"positionSide"`
	Price           float64 `json:"-"`
	Qty             float64 `json:"-"`
	QuoteQty        float64 `json:"-"`
	RealizedPnl     float64 `json:"-"`
	Commission      float64 `json:"-"`
	CommissionAsset string  `json:"commissionAsset"`
	Time            int64   `json:"time"`
	Buyer           bool    `json:"buyer"`
	Maker           bool    `json:"maker"`

	PriceStr       string `json:"price"`
	QtyStr         string `json:"qty"`
	QuoteQtyStr    string `json:"quoteQty"`
	RealizedPnlStr string `json:"realizedPnl"`
	CommissionStr  string `json:"commission"`
}

func (t *UserTrade) parse() {
	t.Price, _ = strconv.ParseFloat(t.PriceStr, 64)
	t.Qty, _ = strconv.ParseFloat(t.QtyStr, 64)
	t.QuoteQty, _ = strconv.ParseFloat(t.QuoteQtyStr, 64)
	t.RealizedPnl, _ = strconv.ParseFloat(t.RealizedPnlStr, 64)
	t.Commission, _ = strconv.ParseFloat(t.CommissionStr, 64)
}

// IncomeRecord is one entry from GET /fapi/v3/income.
type IncomeRecord struct {
	Symbol     string  `json:"symbol"`
	IncomeType string  `json:"incomeType"`
	Income     float64 `json:"-"`
	Asset      string  `json:"asset"`
	Info       string  `json:"info"`
	Time       int64   `json:"time"`
	TranID     int64   `json:"tranId"`
	TradeID    string  `json:"tradeId"`

	IncomeStr string `json:"income"`
}

func (r *IncomeRecord) parse() {
	r.Income, _ = strconv.ParseFloat(r.IncomeStr, 64)
}

// LeverageBracket holds per-symbol leverage tiers.
type LeverageBracket struct {
	Symbol   string   `json:"symbol"`
	Brackets []Bracket `json:"brackets"`
}

// Bracket is one tier in the leverage bracket response.
type Bracket struct {
	BracketNum      int     `json:"bracket"`
	InitialLeverage int     `json:"initialLeverage"`
	NotionalCap     float64 `json:"-"`
	NotionalFloor   float64 `json:"-"`
	MaintMarginRatio float64 `json:"-"`

	NotionalCapStr      string `json:"notionalCap"`
	NotionalFloorStr    string `json:"notionalFloor"`
	MaintMarginRatioStr string `json:"maintMarginRatio"`
}

func (b *Bracket) parse() {
	b.NotionalCap, _ = strconv.ParseFloat(b.NotionalCapStr, 64)
	b.NotionalFloor, _ = strconv.ParseFloat(b.NotionalFloorStr, 64)
	b.MaintMarginRatio, _ = strconv.ParseFloat(b.MaintMarginRatioStr, 64)
}

// CommissionRate holds maker/taker fee rates.
type CommissionRate struct {
	Symbol              string  `json:"symbol"`
	MakerCommissionRate float64 `json:"-"`
	TakerCommissionRate float64 `json:"-"`

	MakerStr string `json:"makerCommissionRate"`
	TakerStr string `json:"takerCommissionRate"`
}

func (cr *CommissionRate) parse() {
	cr.MakerCommissionRate, _ = strconv.ParseFloat(cr.MakerStr, 64)
	cr.TakerCommissionRate, _ = strconv.ParseFloat(cr.TakerStr, 64)
}

// ---- Account endpoints ----

// GetBalance returns wallet balances for all assets.
// GET /fapi/v3/balance (HMAC SHA256, weight 5)
func (c *RESTAuth) GetBalance() ([]AccountBalance, error) {
	var raw []AccountBalance
	if err := c.doJSON(http.MethodGet, "/fapi/v3/balance", nil, true, true, &raw); err != nil {
		return nil, err
	}
	for i := range raw {
		raw[i].parse()
	}
	return raw, nil
}

// GetAccountInfo returns comprehensive account data including all assets and positions.
// GET /fapi/v3/account (HMAC SHA256, weight 5)
func (c *RESTAuth) GetAccountInfo() (*AccountInfo, error) {
	var info AccountInfo
	if err := c.doJSON(http.MethodGet, "/fapi/v3/account", nil, true, true, &info); err != nil {
		return nil, err
	}
	info.parse()
	return &info, nil
}

// GetUserTrades returns trade history for a symbol.
// GET /fapi/v3/userTrades (HMAC SHA256, weight 5)
func (c *RESTAuth) GetUserTrades(symbol string, limit int) ([]UserTrade, error) {
	vals := url.Values{}
	if symbol != "" {
		vals.Set("symbol", strings.ToUpper(symbol))
	}
	if limit > 0 {
		vals.Set("limit", strconv.Itoa(limit))
	}
	var raw []UserTrade
	if err := c.doJSON(http.MethodGet, "/fapi/v3/userTrades", vals, true, true, &raw); err != nil {
		return nil, err
	}
	for i := range raw {
		raw[i].parse()
	}
	return raw, nil
}

// GetIncome returns income history (REALIZED_PNL, FUNDING_FEE, COMMISSION, etc.).
// GET /fapi/v3/income (HMAC SHA256, weight 30)
func (c *RESTAuth) GetIncome(symbol, incomeType string, limit int) ([]IncomeRecord, error) {
	vals := url.Values{}
	if symbol != "" {
		vals.Set("symbol", strings.ToUpper(symbol))
	}
	if incomeType != "" {
		vals.Set("incomeType", incomeType)
	}
	if limit > 0 {
		vals.Set("limit", strconv.Itoa(limit))
	}
	var raw []IncomeRecord
	if err := c.doJSON(http.MethodGet, "/fapi/v3/income", vals, true, true, &raw); err != nil {
		return nil, err
	}
	for i := range raw {
		raw[i].parse()
	}
	return raw, nil
}

// GetLeverageBrackets returns leverage tiers and margin ratios for a symbol (or all).
// GET /fapi/v3/leverageBracket (HMAC SHA256, weight 1)
func (c *RESTAuth) GetLeverageBrackets(symbol string) ([]LeverageBracket, error) {
	vals := url.Values{}
	if symbol != "" {
		vals.Set("symbol", strings.ToUpper(symbol))
	}
	var raw []LeverageBracket
	if err := c.doJSON(http.MethodGet, "/fapi/v3/leverageBracket", vals, true, true, &raw); err != nil {
		return nil, err
	}
	for i := range raw {
		for j := range raw[i].Brackets {
			raw[i].Brackets[j].parse()
		}
	}
	return raw, nil
}

// GetCommissionRate returns maker/taker fee rates for a symbol.
// GET /fapi/v3/commissionRate (HMAC SHA256, weight 20)
func (c *RESTAuth) GetCommissionRate(symbol string) (*CommissionRate, error) {
	vals := url.Values{}
	vals.Set("symbol", strings.ToUpper(symbol))
	var cr CommissionRate
	if err := c.doJSON(http.MethodGet, "/fapi/v3/commissionRate", vals, true, true, &cr); err != nil {
		return nil, err
	}
	cr.parse()
	return &cr, nil
}

// ---- Multi-Assets Mode ----

type multiAssetsResp struct {
	MultiAssetsMargin bool `json:"multiAssetsMargin"`
}

// GetMultiAssetsMode returns whether multi-assets margin mode is enabled.
// GET /fapi/v3/multiAssetsMargin (HMAC SHA256, weight 30)
func (c *RESTAuth) GetMultiAssetsMode() (bool, error) {
	var resp multiAssetsResp
	if err := c.doJSON(http.MethodGet, "/fapi/v3/multiAssetsMargin", nil, true, true, &resp); err != nil {
		return false, err
	}
	return resp.MultiAssetsMargin, nil
}

// SetMultiAssetsMode enables or disables multi-assets margin mode.
// POST /fapi/v3/multiAssetsMargin (HMAC SHA256, weight 1)
func (c *RESTAuth) SetMultiAssetsMode(enabled bool) error {
	vals := url.Values{}
	if enabled {
		vals.Set("multiAssetsMargin", "true")
	} else {
		vals.Set("multiAssetsMargin", "false")
	}
	_, err := c.doRaw(http.MethodPost, "/fapi/v3/multiAssetsMargin", vals, true, true)
	return err
}

// ---- Position Mode ----

type positionModeResp struct {
	DualSidePosition bool `json:"dualSidePosition"`
}

// GetPositionMode returns whether hedge mode (dual side) is enabled.
// GET /fapi/v3/positionSide/dual (HMAC SHA256, weight 30)
func (c *RESTAuth) GetPositionMode() (dualSide bool, err error) {
	var resp positionModeResp
	if err := c.doJSON(http.MethodGet, "/fapi/v3/positionSide/dual", nil, true, true, &resp); err != nil {
		return false, err
	}
	return resp.DualSidePosition, nil
}
