// Package aster — trader_pool.go provides a lazy cache of per-symbol Traders.
//
// Aster's Trader is bound to a single symbol because it fetches symbol metadata
// (stepSize, tickSize, etc.) on creation. In a multi-symbol live bot we need
// a pool that creates/caches Traders on demand.
package aster

import (
	"fmt"
	"sync"
)

// TraderPool lazily creates and caches per-symbol Traders.
type TraderPool struct {
	mu        sync.Mutex
	apiKey    string
	apiSecret string
	traders   map[string]*Trader
}

// NewTraderPool creates a pool with the given API credentials.
func NewTraderPool(apiKey, apiSecret string) *TraderPool {
	return &TraderPool{
		apiKey:    apiKey,
		apiSecret: apiSecret,
		traders:   make(map[string]*Trader),
	}
}

// Get returns the Trader for the given symbol, creating one if needed.
// The MarketState is optional (pass nil if WS state is not available).
func (p *TraderPool) Get(symbol string) (*Trader, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if t, ok := p.traders[symbol]; ok {
		return t, nil
	}

	t, err := NewTrader(symbol, p.apiKey, p.apiSecret, nil)
	if err != nil {
		return nil, fmt.Errorf("create trader for %s: %w", symbol, err)
	}
	p.traders[symbol] = t
	return t, nil
}

// SetState attaches a MarketState to an existing trader (after WS stream starts).
func (p *TraderPool) SetState(symbol string, state *MarketState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.traders[symbol]; ok {
		t.State = state
	}
}
