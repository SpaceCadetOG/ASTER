package aster

import "net/url"

type Balance struct {
	Asset             string  `json:"asset"`
	Balance           float64 `json:"balance"`
	AvailableBalance  float64 `json:"availableBalance"`
	CrossUnPnl        float64 `json:"crossUnPnl"`
	MaxWithdrawAmount float64 `json:"maxWithdrawAmount"`
}

type AccountSummary map[string]any

func (r *RESTAuth) GetAgent() (map[string]any, error) {
	b, err := r.doSignedGET("/fapi/v3/agent", url.Values{})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetBalance returns futures wallet balances from Futures V3, with legacy v2 fallback only in HMAC mode.
func (r *RESTAuth) GetBalance() ([]Balance, error) {
	paths := []string{"/fapi/v3/balance", "/fapi/v2/balance"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/balance"}
	}
	b, err := r.doSignedGETAny(paths, url.Values{})
	if err != nil {
		return nil, err
	}

	var rows []map[string]any
	if err := decodeJSONNumbers(b, &rows); err != nil {
		return nil, err
	}

	out := make([]Balance, 0, len(rows))
	for _, row := range rows {
		out = append(out, Balance{
			Asset:             stringifyAny(row["asset"]),
			Balance:           parseFloatAny(row["balance"]),
			AvailableBalance:  parseFloatAny(row["availableBalance"]),
			CrossUnPnl:        parseFloatAny(row["crossUnPnl"]),
			MaxWithdrawAmount: parseFloatAny(row["maxWithdrawAmount"]),
		})
	}
	return out, nil
}

func (r *RESTAuth) GetAccountSummary() (AccountSummary, error) {
	paths := []string{"/fapi/v3/account", "/fapi/v4/account"}
	if r.isAgentMode() {
		paths = []string{"/fapi/v3/account"}
	}
	b, err := r.doSignedGETAny(paths, url.Values{})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := decodeJSONNumbers(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func stringifyAny(v any) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	if s != "" {
		return s
	}
	return ""
}
