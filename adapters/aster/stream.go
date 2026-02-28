package aster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-machine/internal/ws"
)

// TradePrint mirrors the python TradePrint in aster_api.py.
type TradePrint struct {
	Price   float64 `json:"price"`
	SizeUSD float64 `json:"size_usd"`
	TsMS    int64   `json:"ts_ms"`
	Side    string  `json:"side"` // BUY / SELL
}

// MarketState is a threadsafe in-memory snapshot updated by the websocket streams.
type MarketState struct {
	mu sync.RWMutex

	Symbol string

	BestBid float64
	BestAsk float64

	Bids map[float64]float64 // px -> qty (base)
	Asks map[float64]float64 // px -> qty (base)

	Trades []TradePrint // newest-first
	maxT   int
}

func NewMarketState(symbol string, maxTrades int) *MarketState {
	if maxTrades <= 0 {
		maxTrades = 50
	}
	return &MarketState{
		Symbol: strings.ToUpper(symbol),
		Bids:   map[float64]float64{},
		Asks:   map[float64]float64{},
		maxT:   maxTrades,
	}
}

func (s *MarketState) Mid() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.BestBid == 0 || s.BestAsk == 0 {
		return 0
	}
	return (s.BestBid + s.BestAsk) / 2.0
}

func (s *MarketState) SnapshotTop(rows int) (bid, ask, mid float64, bids, asks [][2]float64, trades []TradePrint) {
	if rows <= 0 {
		rows = 12
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	bid, ask = s.BestBid, s.BestAsk
	if bid != 0 && ask != 0 {
		mid = (bid + ask) / 2.0
	}
	bids = make([][2]float64, 0, len(s.Bids))
	asks = make([][2]float64, 0, len(s.Asks))
	for p, q := range s.Bids {
		bids = append(bids, [2]float64{p, q})
	}
	for p, q := range s.Asks {
		asks = append(asks, [2]float64{p, q})
	}
	sort.Slice(bids, func(i, j int) bool { return bids[i][0] > bids[j][0] })
	sort.Slice(asks, func(i, j int) bool { return asks[i][0] < asks[j][0] })
	if len(bids) > rows {
		bids = bids[:rows]
	}
	if len(asks) > rows {
		asks = asks[:rows]
	}
	trades = append([]TradePrint(nil), s.Trades...)
	return
}

func (s *MarketState) setBookTicker(bid, ask float64) {
	s.mu.Lock()
	if bid != 0 {
		s.BestBid = bid
	}
	if ask != 0 {
		s.BestAsk = ask
	}
	s.mu.Unlock()
}

func (s *MarketState) applyDepth(bids, asks [][2]float64) {
	s.mu.Lock()
	for _, b := range bids {
		p, q := b[0], b[1]
		if q == 0 {
			delete(s.Bids, p)
		} else {
			s.Bids[p] = q
		}
	}
	for _, a := range asks {
		p, q := a[0], a[1]
		if q == 0 {
			delete(s.Asks, p)
		} else {
			s.Asks[p] = q
		}
	}
	s.mu.Unlock()
}

func (s *MarketState) addTrade(tp TradePrint) {
	s.mu.Lock()
	s.Trades = append([]TradePrint{tp}, s.Trades...)
	if len(s.Trades) > s.maxT {
		s.Trades = s.Trades[:s.maxT]
	}
	s.mu.Unlock()
}

// StreamClient maintains a combined market websocket connection:
//   - @bookTicker
//   - @depth{levels}[@100ms|@500ms]
//   - @aggTrade
//
// It updates the provided MarketState.
type StreamClient struct {
	WSBase string
	Symbol string
	Levels int
	Speed  string

	State *MarketState
}

func NewStreamClient(symbol string, levels int, speed string, state *MarketState) *StreamClient {
	if state == nil {
		state = NewMarketState(symbol, 50)
	}
	if levels != 5 && levels != 10 && levels != 20 {
		levels = 20
	}
	speed = strings.TrimSpace(speed)
	return &StreamClient{
		WSBase: "wss://fstream.asterdex.com",
		Symbol: strings.ToLower(symbol),
		Levels: levels,
		Speed:  speed,
		State:  state,
	}
}

func (c *StreamClient) combinedURL() (string, error) {
	depth := fmt.Sprintf("%s@depth%d", c.Symbol, c.Levels)
	if c.Speed != "" {
		depth = depth + "@" + c.Speed
	}
	streams := []string{
		fmt.Sprintf("%s@bookTicker", c.Symbol),
		depth,
		fmt.Sprintf("%s@aggTrade", c.Symbol),
	}
	u, err := url.Parse(c.WSBase)
	if err != nil {
		return "", err
	}
	// WS base already includes scheme+host
	return strings.TrimRight(u.String(), "/") + "/stream?streams=" + strings.Join(streams, "/"), nil
}

type combinedMsg struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

// Run blocks until ctx is cancelled, auto-reconnecting on errors.
func (c *StreamClient) Run(ctx context.Context) error {
	url, err := c.combinedURL()
	if err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, err := ws.Dial(ctx, url, 10*time.Second)
		if err != nil {
			select {
			case <-time.After(1500 * time.Millisecond):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		for {
			if ctx.Err() != nil {
				_ = conn.Close()
				return ctx.Err()
			}
			b, err := conn.ReadText(70 * time.Second)
			if err != nil {
				_ = conn.Close()
				break
			}

			var msg combinedMsg
			if err := json.Unmarshal(b, &msg); err != nil {
				continue
			}
			c.handle(msg.Stream, msg.Data)
		}

		// reconnect
		select {
		case <-time.After(1500 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *StreamClient) handle(stream string, raw json.RawMessage) {
	s := strings.ToLower(stream)

	// bookTicker
	if strings.HasSuffix(s, "@bookticker") {
		var bt struct {
			B string `json:"b"`
			A string `json:"a"`
		}
		if err := json.Unmarshal(raw, &bt); err != nil {
			return
		}
		bid, _ := strconvParseFloat(bt.B)
		ask, _ := strconvParseFloat(bt.A)
		c.State.setBookTicker(bid, ask)
		return
	}

	// depth update
	if strings.Contains(s, "@depth") {
		var du struct {
			B [][]string `json:"b"`
			A [][]string `json:"a"`
		}
		if err := json.Unmarshal(raw, &du); err != nil {
			return
		}
		bids := make([][2]float64, 0, len(du.B))
		asks := make([][2]float64, 0, len(du.A))
		for _, x := range du.B {
			if len(x) < 2 {
				continue
			}
			p, _ := strconvParseFloat(x[0])
			q, _ := strconvParseFloat(x[1])
			bids = append(bids, [2]float64{p, q})
		}
		for _, x := range du.A {
			if len(x) < 2 {
				continue
			}
			p, _ := strconvParseFloat(x[0])
			q, _ := strconvParseFloat(x[1])
			asks = append(asks, [2]float64{p, q})
		}
		c.State.applyDepth(bids, asks)
		return
	}

	// aggTrade
	if strings.HasSuffix(s, "@aggtrade") {
		var t struct {
			P string `json:"p"`
			Q string `json:"q"`
			T int64  `json:"T"`
			M bool   `json:"m"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return
		}
		p, _ := strconvParseFloat(t.P)
		q, _ := strconvParseFloat(t.Q)
		side := "BUY"
		if t.M {
			side = "SELL" // buyer is maker => sell aggressor
		}
		c.State.addTrade(TradePrint{Price: p, SizeUSD: p * q, TsMS: t.T, Side: side})
		return
	}
}

func strconvParseFloat(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	return strconv.ParseFloat(s, 64)
}
