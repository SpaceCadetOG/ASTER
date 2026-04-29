package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"go-machine/adapters/aster"
)

type signedCacheEntry[T any] struct {
	value     T
	fetchedAt time.Time
}

var signedEndpointCacheState struct {
	mu             sync.Mutex
	positionRisk   map[string]signedCacheEntry[[]map[string]any]
	openOrders     map[string]signedCacheEntry[[]map[string]any]
	balances       map[string]signedCacheEntry[[]aster.Balance]
	accountSummary map[string]signedCacheEntry[map[string]any]
}

func init() {
	signedEndpointCacheState.positionRisk = map[string]signedCacheEntry[[]map[string]any]{}
	signedEndpointCacheState.openOrders = map[string]signedCacheEntry[[]map[string]any]{}
	signedEndpointCacheState.balances = map[string]signedCacheEntry[[]aster.Balance]{}
	signedEndpointCacheState.accountSummary = map[string]signedCacheEntry[map[string]any]{}
}

func signedCacheStaleServeWindow() time.Duration {
	sec := envInt("LIVE_SIGNED_CACHE_STALE_SEC", 120)
	if sec < 15 {
		sec = 15
	}
	return time.Duration(sec) * time.Second
}

func signedPositionRiskCacheTTL(symbol string) time.Duration {
	key := "LIVE_SIGNED_POSITION_RISK_CACHE_SEC"
	def := 3
	if strings.TrimSpace(symbol) == "" {
		key = "LIVE_SIGNED_POSITION_RISK_ALL_CACHE_SEC"
		def = 20
	}
	sec := envInt(key, def)
	if sec < 1 {
		sec = 1
	}
	return time.Duration(sec) * time.Second
}

func signedOpenOrdersCacheTTL(symbol string) time.Duration {
	key := "LIVE_SIGNED_OPEN_ORDERS_CACHE_SEC"
	def := 3
	if strings.TrimSpace(symbol) == "" {
		key = "LIVE_SIGNED_OPEN_ORDERS_ALL_CACHE_SEC"
		def = 15
	}
	sec := envInt(key, def)
	if sec < 1 {
		sec = 1
	}
	return time.Duration(sec) * time.Second
}

func signedBalanceCacheTTL() time.Duration {
	sec := envInt("LIVE_SIGNED_BALANCE_CACHE_SEC", 20)
	if sec < 1 {
		sec = 1
	}
	return time.Duration(sec) * time.Second
}

func signedAccountSummaryCacheTTL() time.Duration {
	sec := envInt("LIVE_SIGNED_ACCOUNT_SUMMARY_CACHE_SEC", 20)
	if sec < 1 {
		sec = 1
	}
	return time.Duration(sec) * time.Second
}

func signedEndpointCacheKey(rest *aster.RESTAuth, endpoint, symbol string) string {
	return fmt.Sprintf("%p|%s|%s", rest, endpoint, strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol))))
}

func cloneMapRows(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			out = append(out, nil)
			continue
		}
		cp := make(map[string]any, len(row))
		for k, v := range row {
			cp[k] = v
		}
		out = append(out, cp)
	}
	return out
}

func cloneBalanceRows(rows []aster.Balance) []aster.Balance {
	if len(rows) == 0 {
		return nil
	}
	out := make([]aster.Balance, len(rows))
	copy(out, rows)
	return out
}

func cloneAccountSummaryRow(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func canServeSignedCacheEntry(fetchedAt, now time.Time, freshTTL, staleTTL time.Duration) bool {
	if fetchedAt.IsZero() {
		return false
	}
	age := now.Sub(fetchedAt)
	if age <= freshTTL {
		return true
	}
	return signedUserDataBackoffActive(now) && age <= staleTTL
}

func cachedPositionRisk(rest *aster.RESTAuth, symbol string) ([]map[string]any, error) {
	if rest == nil {
		return nil, fmt.Errorf("position risk unavailable: rest not configured")
	}
	now := time.Now().UTC()
	cacheKey := signedEndpointCacheKey(rest, "position_risk", symbol)
	freshTTL := signedPositionRiskCacheTTL(symbol)
	staleTTL := signedCacheStaleServeWindow()

	signedEndpointCacheState.mu.Lock()
	if entry, ok := signedEndpointCacheState.positionRisk[cacheKey]; ok && canServeSignedCacheEntry(entry.fetchedAt, now, freshTTL, staleTTL) {
		rows := cloneMapRows(entry.value)
		signedEndpointCacheState.mu.Unlock()
		return rows, nil
	}
	signedEndpointCacheState.mu.Unlock()

	if err := signedUserDataBackoffCheck(now); err != nil {
		signedEndpointCacheState.mu.Lock()
		if entry, ok := signedEndpointCacheState.positionRisk[cacheKey]; ok && now.Sub(entry.fetchedAt) <= staleTTL {
			rows := cloneMapRows(entry.value)
			signedEndpointCacheState.mu.Unlock()
			return rows, nil
		}
		signedEndpointCacheState.mu.Unlock()
		return nil, err
	}

	rows, err := rest.PositionRisk(symbol)
	signedUserDataBackoffObserve(now, err)
	if err == nil {
		signedEndpointCacheState.mu.Lock()
		signedEndpointCacheState.positionRisk[cacheKey] = signedCacheEntry[[]map[string]any]{
			value:     cloneMapRows(rows),
			fetchedAt: now,
		}
		signedEndpointCacheState.mu.Unlock()
		return rows, nil
	}

	signedEndpointCacheState.mu.Lock()
	if entry, ok := signedEndpointCacheState.positionRisk[cacheKey]; ok && now.Sub(entry.fetchedAt) <= staleTTL {
		rows := cloneMapRows(entry.value)
		signedEndpointCacheState.mu.Unlock()
		return rows, nil
	}
	signedEndpointCacheState.mu.Unlock()
	return nil, err
}

func cachedOpenOrders(rest *aster.RESTAuth, symbol string) ([]map[string]any, error) {
	if rest == nil {
		return nil, fmt.Errorf("open orders unavailable: rest not configured")
	}
	now := time.Now().UTC()
	cacheKey := signedEndpointCacheKey(rest, "open_orders", symbol)
	freshTTL := signedOpenOrdersCacheTTL(symbol)
	staleTTL := signedCacheStaleServeWindow()

	signedEndpointCacheState.mu.Lock()
	if entry, ok := signedEndpointCacheState.openOrders[cacheKey]; ok && canServeSignedCacheEntry(entry.fetchedAt, now, freshTTL, staleTTL) {
		rows := cloneMapRows(entry.value)
		signedEndpointCacheState.mu.Unlock()
		return rows, nil
	}
	signedEndpointCacheState.mu.Unlock()

	if err := signedUserDataBackoffCheck(now); err != nil {
		signedEndpointCacheState.mu.Lock()
		if entry, ok := signedEndpointCacheState.openOrders[cacheKey]; ok && now.Sub(entry.fetchedAt) <= staleTTL {
			rows := cloneMapRows(entry.value)
			signedEndpointCacheState.mu.Unlock()
			return rows, nil
		}
		signedEndpointCacheState.mu.Unlock()
		return nil, err
	}

	rows, err := rest.OpenOrders(symbol)
	signedUserDataBackoffObserve(now, err)
	if err == nil {
		signedEndpointCacheState.mu.Lock()
		signedEndpointCacheState.openOrders[cacheKey] = signedCacheEntry[[]map[string]any]{
			value:     cloneMapRows(rows),
			fetchedAt: now,
		}
		signedEndpointCacheState.mu.Unlock()
		return rows, nil
	}

	signedEndpointCacheState.mu.Lock()
	if entry, ok := signedEndpointCacheState.openOrders[cacheKey]; ok && now.Sub(entry.fetchedAt) <= staleTTL {
		rows := cloneMapRows(entry.value)
		signedEndpointCacheState.mu.Unlock()
		return rows, nil
	}
	signedEndpointCacheState.mu.Unlock()
	return nil, err
}

func cachedBalances(rest *aster.RESTAuth) ([]aster.Balance, error) {
	if rest == nil {
		return nil, fmt.Errorf("balance unavailable: rest not configured")
	}
	now := time.Now().UTC()
	cacheKey := signedEndpointCacheKey(rest, "balances", "")
	freshTTL := signedBalanceCacheTTL()
	staleTTL := signedCacheStaleServeWindow()

	signedEndpointCacheState.mu.Lock()
	if entry, ok := signedEndpointCacheState.balances[cacheKey]; ok && canServeSignedCacheEntry(entry.fetchedAt, now, freshTTL, staleTTL) {
		rows := cloneBalanceRows(entry.value)
		signedEndpointCacheState.mu.Unlock()
		return rows, nil
	}
	signedEndpointCacheState.mu.Unlock()

	if err := signedUserDataBackoffCheck(now); err != nil {
		signedEndpointCacheState.mu.Lock()
		if entry, ok := signedEndpointCacheState.balances[cacheKey]; ok && now.Sub(entry.fetchedAt) <= staleTTL {
			rows := cloneBalanceRows(entry.value)
			signedEndpointCacheState.mu.Unlock()
			return rows, nil
		}
		signedEndpointCacheState.mu.Unlock()
		return nil, err
	}

	rows, err := rest.GetBalance()
	signedUserDataBackoffObserve(now, err)
	if err == nil {
		signedEndpointCacheState.mu.Lock()
		signedEndpointCacheState.balances[cacheKey] = signedCacheEntry[[]aster.Balance]{
			value:     cloneBalanceRows(rows),
			fetchedAt: now,
		}
		signedEndpointCacheState.mu.Unlock()
		return rows, nil
	}

	signedEndpointCacheState.mu.Lock()
	if entry, ok := signedEndpointCacheState.balances[cacheKey]; ok && now.Sub(entry.fetchedAt) <= staleTTL {
		rows := cloneBalanceRows(entry.value)
		signedEndpointCacheState.mu.Unlock()
		return rows, nil
	}
	signedEndpointCacheState.mu.Unlock()
	return nil, err
}

func cachedAccountSummary(rest *aster.RESTAuth) (map[string]any, error) {
	if rest == nil {
		return nil, fmt.Errorf("account summary unavailable: rest not configured")
	}
	now := time.Now().UTC()
	cacheKey := signedEndpointCacheKey(rest, "account_summary", "")
	freshTTL := signedAccountSummaryCacheTTL()
	staleTTL := signedCacheStaleServeWindow()

	signedEndpointCacheState.mu.Lock()
	if entry, ok := signedEndpointCacheState.accountSummary[cacheKey]; ok && canServeSignedCacheEntry(entry.fetchedAt, now, freshTTL, staleTTL) {
		row := cloneAccountSummaryRow(entry.value)
		signedEndpointCacheState.mu.Unlock()
		return row, nil
	}
	signedEndpointCacheState.mu.Unlock()

	if err := signedUserDataBackoffCheck(now); err != nil {
		signedEndpointCacheState.mu.Lock()
		if entry, ok := signedEndpointCacheState.accountSummary[cacheKey]; ok && now.Sub(entry.fetchedAt) <= staleTTL {
			row := cloneAccountSummaryRow(entry.value)
			signedEndpointCacheState.mu.Unlock()
			return row, nil
		}
		signedEndpointCacheState.mu.Unlock()
		return nil, err
	}

	row, err := rest.GetAccountSummary()
	signedUserDataBackoffObserve(now, err)
	if err == nil {
		signedEndpointCacheState.mu.Lock()
		signedEndpointCacheState.accountSummary[cacheKey] = signedCacheEntry[map[string]any]{
			value:     cloneAccountSummaryRow(row),
			fetchedAt: now,
		}
		signedEndpointCacheState.mu.Unlock()
		return row, nil
	}

	signedEndpointCacheState.mu.Lock()
	if entry, ok := signedEndpointCacheState.accountSummary[cacheKey]; ok && now.Sub(entry.fetchedAt) <= staleTTL {
		row := cloneAccountSummaryRow(entry.value)
		signedEndpointCacheState.mu.Unlock()
		return row, nil
	}
	signedEndpointCacheState.mu.Unlock()
	return nil, err
}
