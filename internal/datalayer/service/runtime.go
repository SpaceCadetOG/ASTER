package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/datalayer/cache"
	dltypes "go-machine/internal/datalayer/types"
	"go-machine/internal/flow"
	itypes "go-machine/internal/types"
	"go-machine/internal/ws"
)

type Config struct {
	Symbols            []string
	PriceRefresh       time.Duration
	CandleTTL          time.Duration
	AccountRefresh     time.Duration
	UserDataMaxStale   time.Duration
	MarketLevels       int
	MarketSpeed        string
	MarketTrades       int
	EventBuffer        int
	WhaleMinUSD        float64
	LiquidationMinUSD  float64
	OrderFlowLargeUSD  float64
	WhaleWindow        time.Duration
	LiquidationWindow  time.Duration
	OrderFlowWindow    time.Duration
	OrderBookFallback  int
	DefaultCandleLimit int
}

type Runtime struct {
	cfg       Config
	startedAt time.Time
	client    *aster.Client
	rest      *aster.RESTAuth
	logger    *log.Logger

	priceCache   cache.Latest[[]dltypes.Price]
	candleCache  *cache.TTLMap[dltypes.Candles]
	accountCache cache.Latest[dltypes.Account]
	fillsCache   cache.Latest[[]dltypes.Fill]

	userDataState *aster.UserDataState

	marketMu     sync.RWMutex
	marketStates map[string]*aster.MarketState

	liqs  *eventCollector
	whale *eventCollector
	oflow *eventCollector
}

const tradeFeedChunkSize = 120

type eventCollector struct {
	name    string
	window  time.Duration
	minUSD  float64
	ring    *cache.Ring[dltypes.Event]
	all     *flow.Window
	mu      sync.RWMutex
	perSym  map[string]*flow.Window
	lastEvt map[string]dltypes.Event
}

func newEventCollector(name string, window time.Duration, minUSD float64, limit int) *eventCollector {
	if window <= 0 {
		window = 30 * time.Second
	}
	return &eventCollector{
		name:    name,
		window:  window,
		minUSD:  minUSD,
		ring:    cache.NewRing[dltypes.Event](limit),
		all:     flow.NewWindow(window, minUSD),
		perSym:  map[string]*flow.Window{},
		lastEvt: map[string]dltypes.Event{},
	}
}

func (c *eventCollector) add(evt dltypes.Event) {
	if evt.Ts.IsZero() {
		evt.Ts = time.Now().UTC()
	}
	isBuy := strings.EqualFold(evt.Side, "BUY")
	c.all.Add(flow.Event{Ts: evt.Ts, USD: evt.USD, IsBuy: isBuy})
	c.mu.Lock()
	w := c.perSym[evt.Symbol]
	if w == nil {
		w = flow.NewWindow(c.window, c.minUSD)
		c.perSym[evt.Symbol] = w
	}
	w.Add(flow.Event{Ts: evt.Ts, USD: evt.USD, IsBuy: isBuy})
	c.lastEvt[evt.Symbol] = evt
	c.mu.Unlock()
	c.ring.Add(evt)
}

func (c *eventCollector) summary(symbol string) dltypes.EventSummary {
	var stats flow.Stats
	var last dltypes.Event
	if strings.TrimSpace(symbol) == "" {
		stats = c.all.Snapshot()
	} else {
		c.mu.RLock()
		w := c.perSym[normalizeSymbol(symbol)]
		last = c.lastEvt[normalizeSymbol(symbol)]
		c.mu.RUnlock()
		if w != nil {
			stats = w.Snapshot()
		}
	}
	if strings.TrimSpace(symbol) == "" {
		events := c.ring.Items()
		if len(events) > 0 {
			last = events[len(events)-1]
		}
	}
	dominant := "NEUTRAL"
	if stats.BuyUSD > stats.SellUSD {
		dominant = "BUY"
	} else if stats.SellUSD > stats.BuyUSD {
		dominant = "SELL"
	}
	return dltypes.EventSummary{
		Symbol:       normalizeSymbol(symbol),
		Count:        stats.Count,
		TotalUSD:     stats.TotalUSD,
		BuyUSD:       stats.BuyUSD,
		SellUSD:      stats.SellUSD,
		DeltaUSD:     stats.DeltaUSD,
		BuyPct:       stats.BuyPct,
		SellPct:      stats.SellPct,
		LargeCount:   stats.LargeCount,
		DominantSide: dominant,
		LastUSD:      last.USD,
		LastSide:     strings.ToUpper(last.Side),
		LastTs:       last.Ts,
	}
}

func (c *eventCollector) assets() []dltypes.EventSummary {
	c.mu.RLock()
	keys := make([]string, 0, len(c.perSym))
	for sym := range c.perSym {
		keys = append(keys, sym)
	}
	c.mu.RUnlock()
	sort.Strings(keys)
	out := make([]dltypes.EventSummary, 0, len(keys))
	for _, sym := range keys {
		out = append(out, c.summary(sym))
	}
	return out
}

func NewRuntime(cfg Config, client *aster.Client, rest *aster.RESTAuth, logger *log.Logger) *Runtime {
	if client == nil {
		client = aster.New("")
	}
	if logger == nil {
		logger = log.Default()
	}
	if cfg.EventBuffer <= 0 {
		cfg.EventBuffer = 50
	}
	if cfg.OrderBookFallback <= 0 {
		cfg.OrderBookFallback = 20
	}
	if cfg.DefaultCandleLimit <= 0 {
		cfg.DefaultCandleLimit = 200
	}
	rt := &Runtime{
		cfg:           cfg,
		startedAt:     time.Now().UTC(),
		client:        client,
		rest:          rest,
		logger:        logger,
		candleCache:   cache.NewTTLMap[dltypes.Candles](),
		userDataState: aster.NewUserDataState(),
		marketStates:  map[string]*aster.MarketState{},
		liqs:          newEventCollector("liquidations", cfg.LiquidationWindow, cfg.LiquidationMinUSD, cfg.EventBuffer),
		whale:         newEventCollector("whales", cfg.WhaleWindow, cfg.WhaleMinUSD, cfg.EventBuffer),
		oflow:         newEventCollector("orderflow", cfg.OrderFlowWindow, cfg.OrderFlowLargeUSD, cfg.EventBuffer),
	}
	for _, sym := range cfg.Symbols {
		rt.marketStates[normalizeSymbol(sym)] = aster.NewMarketState(aster.RawSymbol(sym), cfg.MarketTrades)
	}
	return rt
}

func (r *Runtime) Start(ctx context.Context) {
	go r.runPriceRefresh(ctx)
	go r.runMarketStreams(ctx)
	go r.runTradeFeed(ctx)
	go r.runLiquidationFeed(ctx)
	if r.rest != nil {
		go r.runUserDataStream(ctx)
		go r.runAccountRefresh(ctx)
		go r.runFillsRefresh(ctx)
	}
}

func (r *Runtime) Health() dltypes.Health {
	now := time.Now().UTC()
	components := map[string]dltypes.Component{
		"prices":      componentFromLatest(r.priceCache.Get(), now, r.cfg.PriceRefresh*3),
		"account":     componentFromLatest(r.accountCache.Get(), now, maxDuration(r.cfg.AccountRefresh*3, 45*time.Second)),
		"fills":       componentFromLatest(r.fillsCache.Get(), now, maxDuration(r.cfg.AccountRefresh*3, 45*time.Second)),
		"liquidation": eventComponent(r.liqs, now, r.cfg.LiquidationWindow*2),
		"whales":      eventComponent(r.whale, now, r.cfg.WhaleWindow*2),
		"orderflow":   eventComponent(r.oflow, now, r.cfg.OrderFlowWindow*2),
	}
	status := "ok"
	for _, c := range components {
		if c.Status == "degraded" {
			status = "degraded"
			break
		}
	}
	return dltypes.Health{
		Status:     status,
		StartedAt:  r.startedAt,
		Components: components,
		Ts:         now,
	}
}

func componentFromLatest[T any](snap cache.Snapshot[T], now time.Time, staleAfter time.Duration) dltypes.Component {
	if snap.Updated.IsZero() {
		return dltypes.Component{Status: "degraded", Detail: "not_ready", Ts: now}
	}
	stale := staleAfter > 0 && now.Sub(snap.Updated) > staleAfter
	status := "ok"
	if stale || snap.Err != nil {
		status = "degraded"
	}
	detail := ""
	if snap.Err != nil {
		detail = snap.Err.Error()
	}
	return dltypes.Component{Status: status, Detail: detail, Stale: stale, Ts: snap.Updated}
}

func eventComponent(c *eventCollector, now time.Time, staleAfter time.Duration) dltypes.Component {
	summary := c.summary("")
	if summary.LastTs.IsZero() {
		return dltypes.Component{Status: "degraded", Detail: "no_events_yet", Ts: now}
	}
	stale := staleAfter > 0 && now.Sub(summary.LastTs) > staleAfter
	status := "ok"
	if stale {
		status = "degraded"
	}
	return dltypes.Component{Status: status, Stale: stale, Ts: summary.LastTs}
}

func (r *Runtime) Prices() dltypes.PricesResponse {
	snap := r.priceCache.Get()
	if len(snap.Value) == 0 {
		r.refreshPrices()
		snap = r.priceCache.Get()
	}
	meta := metaFromSnapshot(snap, time.Now().UTC(), r.cfg.PriceRefresh*3)
	return dltypes.PricesResponse{
		Meta:   meta,
		Prices: append([]dltypes.Price(nil), snap.Value...),
	}
}

func (r *Runtime) Price(symbol string) (dltypes.PriceResponse, bool) {
	norm := normalizeSymbol(symbol)
	prices := r.Prices()
	for _, p := range prices.Prices {
		if p.Symbol == norm {
			return dltypes.PriceResponse{Meta: prices.Meta, Price: p}, true
		}
	}
	ob, err := r.orderBookForSymbol(norm, r.cfg.OrderBookFallback)
	if err != nil {
		return dltypes.PriceResponse{
			Meta: dltypes.Meta{
				Source:  "unavailable",
				Stale:   true,
				Partial: true,
				Ts:      time.Now().UTC(),
				Error:   err.Error(),
			},
		}, false
	}
	price := dltypes.Price{
		Symbol:    norm,
		Bid:       ob.TopBid,
		Ask:       ob.TopAsk,
		Mid:       midpoint(ob.TopBid, ob.TopAsk),
		SpreadBps: ob.SpreadBps,
		Ts:        ob.Ts,
	}
	return dltypes.PriceResponse{
		Meta:  dltypes.Meta{Source: "stream+rest", Partial: true, Ts: ob.Ts},
		Price: price,
	}, true
}

func (r *Runtime) OrderBook(symbol string, limit int) (dltypes.OrderBookResponse, error) {
	ob, err := r.orderBookForSymbol(symbol, limit)
	meta := dltypes.Meta{
		Source: "stream+rest",
		Ts:     ob.Ts,
	}
	if err != nil {
		meta.Stale = true
		meta.Error = err.Error()
		return dltypes.OrderBookResponse{Meta: meta}, err
	}
	return dltypes.OrderBookResponse{Meta: meta, OrderBook: ob}, nil
}

func (r *Runtime) Candles(ctx context.Context, symbol, tf string, limit int) (dltypes.CandlesResponse, error) {
	timeframe := strings.ToLower(strings.TrimSpace(tf))
	if timeframe == "" {
		timeframe = string(itypes.TF1m)
	}
	parsedTF, ok := itypes.ParseTF(timeframe)
	if !ok {
		return dltypes.CandlesResponse{}, fmt.Errorf("unsupported timeframe %q", tf)
	}
	if limit <= 0 {
		limit = r.cfg.DefaultCandleLimit
	}
	key := normalizeSymbol(symbol) + "|" + parsedTF.String() + "|" + strconv.Itoa(limit)
	if cached := r.candleCache.Get(key); cached.Found {
		return dltypes.CandlesResponse{
			Meta:    dltypes.Meta{Source: cached.Source, Ts: cached.Updated},
			Candles: cached.Value,
		}, nil
	}
	bars, err := r.client.LoadCandles(aster.RawSymbol(symbol), parsedTF, limit)
	if err != nil {
		return dltypes.CandlesResponse{
			Meta: dltypes.Meta{
				Source:  "rest",
				Stale:   true,
				Partial: true,
				Ts:      time.Now().UTC(),
				Error:   err.Error(),
			},
		}, err
	}
	out := dltypes.Candles{
		Symbol:    normalizeSymbol(symbol),
		Timeframe: parsedTF.String(),
		Candles:   make([]dltypes.Candle, 0, len(bars)),
	}
	for _, c := range bars {
		out.Candles = append(out.Candles, dltypes.Candle{
			Ts:     c.T.UTC(),
			Open:   c.O,
			High:   c.H,
			Low:    c.L,
			Close:  c.C,
			Volume: c.V,
		})
	}
	updated := time.Now().UTC()
	r.candleCache.Set(key, out, r.cfg.CandleTTL, "rest", updated)
	return dltypes.CandlesResponse{
		Meta:    dltypes.Meta{Source: "rest", Ts: updated},
		Candles: out,
	}, nil
}

func (r *Runtime) Account() dltypes.AccountResponse {
	snap := r.accountCache.Get()
	return dltypes.AccountResponse{
		Meta:    metaFromSnapshot(snap, time.Now().UTC(), maxDuration(r.cfg.AccountRefresh*3, 45*time.Second)),
		Account: snap.Value,
	}
}

func (r *Runtime) Positions() dltypes.PositionsResponse {
	resp := r.Account()
	return dltypes.PositionsResponse{
		Meta:      resp.Meta,
		Positions: append([]dltypes.Position(nil), resp.Account.Positions...),
	}
}

func (r *Runtime) Fills(symbol string, limit int) dltypes.FillsResponse {
	snap := r.fillsCache.Get()
	fills := append([]dltypes.Fill(nil), snap.Value...)
	if strings.TrimSpace(symbol) != "" {
		filtered := fills[:0]
		want := normalizeSymbol(symbol)
		for _, f := range fills {
			if f.Symbol == want {
				filtered = append(filtered, f)
			}
		}
		fills = filtered
	}
	if limit > 0 && len(fills) > limit {
		fills = fills[:limit]
	}
	return dltypes.FillsResponse{
		Meta:  metaFromSnapshot(snap, time.Now().UTC(), maxDuration(r.cfg.AccountRefresh*3, 45*time.Second)),
		Fills: fills,
	}
}

func (r *Runtime) Liquidations() dltypes.LiquidationsResponse {
	return dltypes.LiquidationsResponse{
		Meta: dltypes.Meta{
			Source: "stream",
			Stale:  isSummaryStale(r.liqs.summary(""), r.cfg.LiquidationWindow*2),
			Ts:     r.summaryTs(r.liqs.summary("")),
		},
		Summary: r.liqs.summary(""),
		Assets:  r.liqs.assets(),
		Events:  r.liqs.ring.Items(),
	}
}

func (r *Runtime) Whales() dltypes.WhalesResponse {
	return dltypes.WhalesResponse{
		Meta: dltypes.Meta{
			Source: "stream",
			Stale:  isSummaryStale(r.whale.summary(""), r.cfg.WhaleWindow*2),
			Ts:     r.summaryTs(r.whale.summary("")),
		},
		Summary: r.whale.summary(""),
		Assets:  r.whale.assets(),
		Events:  r.whale.ring.Items(),
	}
}

func (r *Runtime) OrderFlow(symbol string) (dltypes.OrderFlowResponse, error) {
	norm := normalizeSymbol(symbol)
	summary := r.oflow.summary(norm)
	recent := make([]dltypes.Event, 0)
	for _, evt := range r.oflow.ring.Items() {
		if evt.Symbol == norm {
			recent = append(recent, evt)
		}
	}
	metaSource := "stream"
	metaStale := isSummaryStale(summary, r.cfg.OrderFlowWindow*2)
	metaPartial := false
	metaErr := ""
	if summary.Count == 0 {
		fallbackSummary, fallbackRecent, err := r.orderFlowFallback(norm)
		if err == nil {
			summary = fallbackSummary
			recent = fallbackRecent
			metaSource = "rest_fallback"
			metaStale = false
			metaPartial = true
		} else {
			metaErr = err.Error()
		}
	}
	score := 50.0
	total := summary.BuyUSD + summary.SellUSD + 1
	if total > 0 {
		score = clamp(50+50*clamp(summary.DeltaUSD/total, -1, 1)+5*float64(summary.LargeCount), 0, 100)
	}
	signal := "NEUTRAL"
	if score > 70 {
		signal = "BULL"
	} else if score < 30 {
		signal = "BEAR"
	}
	return dltypes.OrderFlowResponse{
		Meta: dltypes.Meta{
			Source:  metaSource,
			Stale:   metaStale,
			Partial: metaPartial,
			Ts:      r.summaryTs(summary),
			Error:   metaErr,
		},
		OrderFlow: dltypes.OrderFlow{
			Symbol:  norm,
			Score:   score,
			Signal:  signal,
			Summary: summary,
			Recent:  recent,
		},
	}, nil
}

func (r *Runtime) summaryTs(summary dltypes.EventSummary) time.Time {
	if summary.LastTs.IsZero() {
		return time.Now().UTC()
	}
	return summary.LastTs
}

func (r *Runtime) orderBookForSymbol(symbol string, limit int) (dltypes.OrderBook, error) {
	norm := normalizeSymbol(symbol)
	raw := aster.RawSymbol(norm)
	if limit <= 0 {
		limit = r.cfg.OrderBookFallback
	}
	r.marketMu.RLock()
	state := r.marketStates[norm]
	r.marketMu.RUnlock()
	if state != nil {
		bid, ask, _, bids, asks, _ := state.SnapshotTop(limit)
		if len(bids) > 0 || len(asks) > 0 {
			return buildOrderBook(norm, bid, ask, bids, asks), nil
		}
	}
	ob, err := r.client.FetchOrderBook(raw, limit)
	if err != nil {
		return dltypes.OrderBook{}, err
	}
	bids := make([][2]float64, 0, len(ob.Bids))
	asks := make([][2]float64, 0, len(ob.Asks))
	for _, lvl := range ob.Bids {
		bids = append(bids, lvl)
	}
	for _, lvl := range ob.Asks {
		asks = append(asks, lvl)
	}
	topBid, topAsk := 0.0, 0.0
	if len(bids) > 0 {
		topBid = bids[0][0]
	}
	if len(asks) > 0 {
		topAsk = asks[0][0]
	}
	return buildOrderBook(norm, topBid, topAsk, bids, asks), nil
}

func buildOrderBook(symbol string, bid, ask float64, bids, asks [][2]float64) dltypes.OrderBook {
	out := dltypes.OrderBook{
		Symbol:    symbol,
		Bids:      make([]dltypes.BookLevel, 0, len(bids)),
		Asks:      make([]dltypes.BookLevel, 0, len(asks)),
		TopBid:    bid,
		TopAsk:    ask,
		SpreadBps: spreadBps(bid, ask),
		Ts:        time.Now().UTC(),
	}
	for _, lvl := range bids {
		out.Bids = append(out.Bids, dltypes.BookLevel{Price: lvl[0], Qty: lvl[1]})
	}
	for _, lvl := range asks {
		out.Asks = append(out.Asks, dltypes.BookLevel{Price: lvl[0], Qty: lvl[1]})
	}
	return out
}

func (r *Runtime) runPriceRefresh(ctx context.Context) {
	r.refreshPrices()
	ticker := time.NewTicker(r.cfg.PriceRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refreshPrices()
		}
	}
}

func (r *Runtime) refreshPrices() {
	now := time.Now().UTC()
	rows, err := r.fetchPriceUniverse()
	if err != nil || len(rows) == 0 {
		if err == nil {
			err = fmt.Errorf("price refresh returned no markets")
		}
		r.priceCache.Set(r.priceCache.Get().Value, "rest", now, err)
		return
	}
	out := make([]dltypes.Price, 0, len(rows))
	for _, row := range rows {
		norm := normalizeSymbol(fmt.Sprint(row["symbol"]))
		last := mapFloat(row["lastPrice"])
		bid, ask, ts := r.streamBidAsk(norm)
		if bid <= 0 {
			bid = last
		}
		if ask <= 0 {
			ask = last
		}
		p := dltypes.Price{
			Symbol:       norm,
			Bid:          bid,
			Ask:          ask,
			Mid:          midpoint(bid, ask),
			SpreadBps:    spreadBps(bid, ask),
			Ts:           ts,
		}
		if p.Ts.IsZero() {
			p.Ts = now
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	r.priceCache.Set(out, "rest+stream", now, nil)
}

func (r *Runtime) runMarketStreams(ctx context.Context) {
	var wg sync.WaitGroup
	for _, sym := range r.cfg.Symbols {
		norm := normalizeSymbol(sym)
		raw := aster.RawSymbol(norm)
		r.marketMu.Lock()
		state := r.marketStates[norm]
		if state == nil {
			state = aster.NewMarketState(raw, r.cfg.MarketTrades)
			r.marketStates[norm] = state
		}
		r.marketMu.Unlock()
		client := aster.NewStreamClient(raw, r.cfg.MarketLevels, r.cfg.MarketSpeed, state)
		wg.Add(1)
		go func(c *aster.StreamClient) {
			defer wg.Done()
			for ctx.Err() == nil {
				if err := c.Run(ctx); err != nil && ctx.Err() == nil {
					r.logger.Printf("datalayer market stream error: %v", err)
				}
				select {
				case <-time.After(1500 * time.Millisecond):
				case <-ctx.Done():
					return
				}
			}
		}(client)
	}
	wg.Wait()
}

func (r *Runtime) runTradeFeed(ctx context.Context) {
	symbols := r.tradeFeedSymbols()
	if len(symbols) == 0 {
		return
	}
	var wg sync.WaitGroup
	for start := 0; start < len(symbols); start += tradeFeedChunkSize {
		end := start + tradeFeedChunkSize
		if end > len(symbols) {
			end = len(symbols)
		}
		streams := make([]string, 0, end-start)
		for _, sym := range symbols[start:end] {
			streams = append(streams, strings.ToLower(aster.RawSymbol(sym))+"@aggTrade")
		}
		wsURL := "wss://fstream.asterdex.com/stream?streams=" + strings.Join(streams, "/")
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			r.runTradeFeedConn(ctx, url)
		}(wsURL)
	}
	wg.Wait()
}

func (r *Runtime) runTradeFeedConn(ctx context.Context, wsURL string) {
	for ctx.Err() == nil {
		conn, err := ws.Dial(ctx, wsURL, 10*time.Second)
		if err != nil {
			r.logger.Printf("datalayer trade feed dial error: %v", err)
			sleepContext(ctx, 1500*time.Millisecond)
			continue
		}
		err = r.consumeTradeFeed(ctx, conn)
		_ = conn.Close()
		if err != nil && ctx.Err() == nil {
			r.logger.Printf("datalayer trade feed error: %v", err)
		}
		sleepContext(ctx, 1500*time.Millisecond)
	}
}

func (r *Runtime) consumeTradeFeed(ctx context.Context, conn *ws.Conn) error {
	for ctx.Err() == nil {
		b, err := conn.ReadText(70 * time.Second)
		if err != nil {
			return err
		}
		var msg struct {
			Stream string          `json:"stream"`
			Data   json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(b, &msg); err != nil {
			continue
		}
		var trade struct {
			P string `json:"p"`
			Q string `json:"q"`
			T int64  `json:"T"`
			M bool   `json:"m"`
			S string `json:"s"`
		}
		if err := json.Unmarshal(msg.Data, &trade); err != nil {
			continue
		}
		price := parseFloat(trade.P)
		qty := parseFloat(trade.Q)
		usd := price * qty
		side := "BUY"
		if trade.M {
			side = "SELL"
		}
		evt := dltypes.Event{
			Symbol: normalizeSymbol(trade.S),
			Side:   side,
			Price:  price,
			Qty:    qty,
			USD:    usd,
			Ts:     time.UnixMilli(trade.T).UTC(),
		}
		r.oflow.add(evt)
		if usd >= r.cfg.WhaleMinUSD {
			evt.Label = whaleTier(usd, r.cfg.WhaleMinUSD)
			r.whale.add(evt)
		}
	}
	return ctx.Err()
}

func (r *Runtime) runLiquidationFeed(ctx context.Context) {
	wsURL := "wss://fstream.asterdex.com/stream?streams=!forceOrder@arr"
	for ctx.Err() == nil {
		conn, err := ws.Dial(ctx, wsURL, 10*time.Second)
		if err != nil {
			r.logger.Printf("datalayer liquidation feed dial error: %v", err)
			sleepContext(ctx, 1500*time.Millisecond)
			continue
		}
		err = r.consumeLiquidationFeed(ctx, conn)
		_ = conn.Close()
		if err != nil && ctx.Err() == nil {
			r.logger.Printf("datalayer liquidation feed error: %v", err)
		}
		sleepContext(ctx, 1500*time.Millisecond)
	}
}

func (r *Runtime) consumeLiquidationFeed(ctx context.Context, conn *ws.Conn) error {
	for ctx.Err() == nil {
		b, err := conn.ReadText(70 * time.Second)
		if err != nil {
			return err
		}
		var msg struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(b, &msg); err != nil {
			continue
		}
		var payload struct {
			Order struct {
				Symbol string `json:"s"`
				Side   string `json:"S"`
				Price  string `json:"p"`
				Qty    string `json:"q"`
				Ts     int64  `json:"T"`
			} `json:"o"`
		}
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			continue
		}
		price := parseFloat(payload.Order.Price)
		qty := parseFloat(payload.Order.Qty)
		usd := price * qty
		if usd < r.cfg.LiquidationMinUSD {
			continue
		}
		r.liqs.add(dltypes.Event{
			Symbol: normalizeSymbol(payload.Order.Symbol),
			Side:   strings.ToUpper(strings.TrimSpace(payload.Order.Side)),
			Price:  price,
			Qty:    qty,
			USD:    usd,
			Ts:     time.UnixMilli(payload.Order.Ts).UTC(),
		})
	}
	return ctx.Err()
}

func (r *Runtime) runUserDataStream(ctx context.Context) {
	if r.rest == nil {
		return
	}
	client := aster.NewUserDataStreamClient(r.rest, r.userDataState)
	for ctx.Err() == nil {
		if err := client.Run(ctx); err != nil && ctx.Err() == nil {
			r.logger.Printf("datalayer user-data stream error: %v", err)
			sleepContext(ctx, 2*time.Second)
		}
	}
}

func (r *Runtime) runAccountRefresh(ctx context.Context) {
	r.refreshAccount()
	ticker := time.NewTicker(r.cfg.AccountRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refreshAccount()
		}
	}
}

func (r *Runtime) refreshAccount() {
	account, err := r.fetchAccount()
	r.accountCache.Set(account, "rest+stream", time.Now().UTC(), err)
}

func (r *Runtime) fetchAccount() (dltypes.Account, error) {
	if r.rest == nil {
		return dltypes.Account{}, fmt.Errorf("account disabled: no auth configured")
	}
	now := time.Now().UTC()
	account := dltypes.Account{Ts: now}
	var errs []string

	available := 0.0
	if snap, ok := r.userDataSnapshotFresh(); ok {
		if usdt, found := snap.Balances["USDT"]; found {
			available = firstPositive(usdt.CrossWallet, usdt.WalletBalance)
		}
		for _, p := range snap.Positions {
			qty := math.Abs(p.PositionAmt)
			if qty <= 1e-10 {
				continue
			}
			side := "LONG"
			if p.PositionAmt < 0 || strings.EqualFold(p.PositionSide, "SHORT") {
				side = "SHORT"
			}
			mark := r.lastPriceForSymbol(p.Symbol)
			notional := qty * firstPositive(mark, p.EntryPrice)
			account.Positions = append(account.Positions, dltypes.Position{
				Symbol:      normalizeSymbol(p.Symbol),
				Side:        side,
				Qty:         qty,
				EntryPrice:  p.EntryPrice,
				MarkPrice:   mark,
				Margin:      maxFloat(p.IsolatedWallet, 0),
				Leverage:    0,
				Unrealized:  p.UnrealizedPnL,
				NotionalUSD: notional,
			})
			account.OpenPnL += p.UnrealizedPnL
		}
	}

	summary, err := r.rest.GetAccountSummary()
	if err != nil {
		errs = append(errs, "account_summary:"+err.Error())
	} else {
		account.Equity = firstPositive(
			mapFloat(summary["totalWalletBalance"])+mapFloat(summary["totalUnrealizedProfit"]),
			mapFloat(summary["totalMarginBalance"]),
			mapFloat(summary["availableBalance"])+account.OpenPnL,
		)
		if available <= 0 {
			available = firstPositive(mapFloat(summary["availableBalance"]), mapFloat(summary["maxWithdrawAmount"]))
		}
	}

	if available <= 0 {
		bals, balErr := r.rest.GetBalance()
		if balErr != nil {
			errs = append(errs, "balances:"+balErr.Error())
		} else {
			for _, b := range bals {
				if strings.EqualFold(b.Asset, "USDT") {
					available = firstPositive(b.AvailableBalance, b.Balance)
					break
				}
			}
		}
	}
	account.AvailableUSDT = available

	orders, err := r.rest.OpenOrders("")
	if err != nil {
		errs = append(errs, "open_orders:"+err.Error())
	} else {
		account.OpenOrders = normalizeOpenOrders(orders)
	}
	if account.Equity <= 0 {
		account.Equity = account.AvailableUSDT + account.OpenPnL
	}
	sort.Slice(account.Positions, func(i, j int) bool {
		return math.Abs(account.Positions[i].Unrealized) > math.Abs(account.Positions[j].Unrealized)
	})
	if len(errs) > 0 {
		return account, errors.New(strings.Join(errs, "; "))
	}
	return account, nil
}

func (r *Runtime) runFillsRefresh(ctx context.Context) {
	r.refreshFills()
	ticker := time.NewTicker(r.cfg.AccountRefresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refreshFills()
		}
	}
}

func (r *Runtime) refreshFills() {
	if r.rest == nil {
		return
	}
	var all []dltypes.Fill
	var errs []string
	for _, sym := range r.cfg.Symbols {
		rows, err := r.rest.UserTrades(aster.RawSymbol(sym), 50)
		if err != nil {
			errs = append(errs, normalizeSymbol(sym)+":"+err.Error())
			continue
		}
		all = append(all, normalizeFills(rows)...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Ts.After(all[j].Ts) })
	var err error
	if len(errs) > 0 {
		err = errors.New(strings.Join(errs, "; "))
	}
	r.fillsCache.Set(all, "rest", time.Now().UTC(), err)
}

func (r *Runtime) userDataSnapshotFresh() (aster.UserDataSnapshot, bool) {
	if r.userDataState == nil {
		return aster.UserDataSnapshot{}, false
	}
	snap := r.userDataState.Snapshot()
	if snap.UpdatedAt.IsZero() {
		return aster.UserDataSnapshot{}, false
	}
	if time.Since(snap.UpdatedAt) > r.cfg.UserDataMaxStale {
		return aster.UserDataSnapshot{}, false
	}
	if len(snap.Balances) == 0 && len(snap.Positions) == 0 {
		return aster.UserDataSnapshot{}, false
	}
	return snap, true
}

func (r *Runtime) streamBidAsk(symbol string) (float64, float64, time.Time) {
	r.marketMu.RLock()
	state := r.marketStates[normalizeSymbol(symbol)]
	r.marketMu.RUnlock()
	if state == nil {
		return 0, 0, time.Time{}
	}
	bid, ask, _, _, _, trades := state.SnapshotTop(5)
	ts := time.Now().UTC()
	if len(trades) > 0 {
		ts = time.UnixMilli(trades[0].TsMS).UTC()
	}
	return bid, ask, ts
}

func (r *Runtime) lastPriceForSymbol(symbol string) float64 {
	norm := normalizeSymbol(symbol)
	if resp, ok := r.Price(norm); ok {
		if resp.Price.Mid > 0 {
			return resp.Price.Mid
		}
	}
	return 0
}

func (r *Runtime) bookTickerRaw(symbol string) (float64, float64, error) {
	if r.rest != nil {
		return r.rest.BookTicker(aster.RawSymbol(symbol))
	}
	u := r.client.BaseURL + "/ticker/bookTicker"
	parsed, err := url.Parse(u)
	if err != nil {
		return 0, 0, err
	}
	q := parsed.Query()
	q.Set("symbol", aster.RawSymbol(symbol))
	parsed.RawQuery = q.Encode()
	var out struct {
		Bid string `json:"bidPrice"`
		Ask string `json:"askPrice"`
	}
	resp, err := r.client.HTTP.Get(parsed.String())
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, 0, fmt.Errorf("bookTicker status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, 0, err
	}
	return parseFloat(out.Bid), parseFloat(out.Ask), nil
}

func metaFromSnapshot[T any](snap cache.Snapshot[T], now time.Time, staleAfter time.Duration) dltypes.Meta {
	meta := dltypes.Meta{
		Source: snap.Source,
		Ts:     snap.Updated,
	}
	if meta.Source == "" {
		meta.Source = "unavailable"
	}
	if meta.Ts.IsZero() {
		meta.Ts = now
		meta.Stale = true
		meta.Partial = true
		meta.Error = "not_ready"
		return meta
	}
	if staleAfter > 0 && now.Sub(snap.Updated) > staleAfter {
		meta.Stale = true
	}
	if snap.Err != nil {
		meta.Partial = true
		meta.Error = snap.Err.Error()
	}
	return meta
}

func isSummaryStale(summary dltypes.EventSummary, staleAfter time.Duration) bool {
	if summary.LastTs.IsZero() {
		return true
	}
	return staleAfter > 0 && time.Since(summary.LastTs) > staleAfter
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(aster.NormSymbol(symbol)))
}

func parseFloat(v string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return f
}

func mapFloat(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	default:
		f, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(v)), 64)
		return f
	}
}

func normalizeOpenOrders(rows []map[string]any) []dltypes.OpenOrder {
	out := make([]dltypes.OpenOrder, 0, len(rows))
	for _, row := range rows {
		ts := time.UnixMilli(int64(mapFloat(row["time"]))).UTC()
		out = append(out, dltypes.OpenOrder{
			Symbol:  normalizeSymbol(fmt.Sprint(row["symbol"])),
			Side:    strings.ToUpper(strings.TrimSpace(fmt.Sprint(row["side"]))),
			Type:    strings.ToUpper(strings.TrimSpace(fmt.Sprint(row["type"]))),
			Price:   mapFloat(row["price"]),
			Qty:     firstPositive(mapFloat(row["origQty"]), mapFloat(row["qty"])),
			Status:  strings.ToUpper(strings.TrimSpace(fmt.Sprint(row["status"]))),
			OrderID: int64(mapFloat(row["orderId"])),
			Reduce:  strings.EqualFold(fmt.Sprint(row["reduceOnly"]), "true"),
			Ts:      ts,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts.After(out[j].Ts) })
	return out
}

func normalizeFills(rows []map[string]any) []dltypes.Fill {
	out := make([]dltypes.Fill, 0, len(rows))
	for _, row := range rows {
		realized := mapFloat(row["realizedPnl"])
		var realizedPtr *float64
		if realized != 0 {
			v := realized
			realizedPtr = &v
		}
		fee := firstPositive(math.Abs(mapFloat(row["commission"])), math.Abs(mapFloat(row["fee"])))
		side := "BUY"
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(row["side"])), "SELL") || strings.EqualFold(strings.TrimSpace(fmt.Sprint(row["buyer"])), "false") {
			side = "SELL"
		}
		out = append(out, dltypes.Fill{
			Symbol:      normalizeSymbol(fmt.Sprint(row["symbol"])),
			Side:        side,
			Price:       mapFloat(row["price"]),
			Qty:         firstPositive(mapFloat(row["qty"]), mapFloat(row["executedQty"])),
			Fee:         fee,
			RealizedPnL: realizedPtr,
			Ts:          time.UnixMilli(int64(firstPositive(mapFloat(row["time"]), mapFloat(row["tradeTime"])))).UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts.After(out[j].Ts) })
	return out
}

func (r *Runtime) fetchPriceUniverse() ([]map[string]any, error) {
	if r.client == nil || r.client.HTTP == nil {
		return nil, fmt.Errorf("price client unavailable")
	}
	resp, err := r.client.HTTP.Get(strings.TrimRight(r.client.BaseURL, "/") + "/ticker/24hr")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ticker/24hr status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rows []map[string]any
	if err := decodeJSONNumbersFromReader(resp.Body, &rows); err == nil && len(rows) > 0 {
		return rows, nil
	}
	resp2, err := r.client.HTTP.Get(strings.TrimRight(r.client.BaseURL, "/") + "/ticker/24hr")
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		return nil, fmt.Errorf("ticker/24hr status %d: %s", resp2.StatusCode, strings.TrimSpace(string(body)))
	}
	var single map[string]any
	if err := decodeJSONNumbersFromReader(resp2.Body, &single); err != nil {
		return nil, err
	}
	if len(single) == 0 {
		return nil, fmt.Errorf("ticker/24hr empty payload")
	}
	return []map[string]any{single}, nil
}

func decodeJSONNumbersFromReader(r io.Reader, target any) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	return dec.Decode(target)
}

func (r *Runtime) tradeFeedSymbols() []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(r.cfg.Symbols))
	appendSymbol := func(sym string) {
		norm := normalizeSymbol(sym)
		if norm == "" {
			return
		}
		if _, ok := seen[norm]; ok {
			return
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	if snap := r.priceCache.Get(); len(snap.Value) > 0 {
		for _, p := range snap.Value {
			appendSymbol(p.Symbol)
		}
	}
	if len(out) == 0 && r.client != nil {
		for _, m := range r.client.FetchAllMarkets("USDT", "USD") {
			appendSymbol(m.Symbol)
		}
	}
	for _, sym := range r.cfg.Symbols {
		appendSymbol(sym)
	}
	sort.Strings(out)
	return out
}

func (r *Runtime) orderFlowFallback(symbol string) (dltypes.EventSummary, []dltypes.Event, error) {
	if r.client == nil {
		return dltypes.EventSummary{}, nil, fmt.Errorf("orderflow unavailable")
	}
	rows, err := r.client.RecentAggTrades(aster.RawSymbol(symbol), 100)
	if err != nil {
		return dltypes.EventSummary{}, nil, err
	}
	window := flow.NewWindow(r.cfg.OrderFlowWindow, r.cfg.OrderFlowLargeUSD)
	recent := make([]dltypes.Event, 0, len(rows))
	for _, row := range rows {
		price, _ := strconv.ParseFloat(row.Price.String(), 64)
		qty, _ := strconv.ParseFloat(row.Qty.String(), 64)
		usd := price * qty
		side := "BUY"
		if row.IsBuyerMaker {
			side = "SELL"
		}
		evtSymbol := row.Symbol
		if strings.TrimSpace(evtSymbol) == "" {
			evtSymbol = symbol
		}
		evt := dltypes.Event{
			Symbol: normalizeSymbol(evtSymbol),
			Side:   side,
			Price:  price,
			Qty:    qty,
			USD:    usd,
			Ts:     time.UnixMilli(row.TradeTime).UTC(),
		}
		window.Add(flow.Event{Ts: evt.Ts, USD: evt.USD, IsBuy: side == "BUY"})
		recent = append(recent, evt)
	}
	stats := window.Snapshot()
	last := dltypes.Event{}
	if len(recent) > 0 {
		last = recent[len(recent)-1]
	}
	dominant := "NEUTRAL"
	if stats.BuyUSD > stats.SellUSD {
		dominant = "BUY"
	} else if stats.SellUSD > stats.BuyUSD {
		dominant = "SELL"
	}
	return dltypes.EventSummary{
		Symbol:       normalizeSymbol(symbol),
		Count:        stats.Count,
		TotalUSD:     stats.TotalUSD,
		BuyUSD:       stats.BuyUSD,
		SellUSD:      stats.SellUSD,
		DeltaUSD:     stats.DeltaUSD,
		BuyPct:       stats.BuyPct,
		SellPct:      stats.SellPct,
		LargeCount:   stats.LargeCount,
		DominantSide: dominant,
		LastUSD:      last.USD,
		LastSide:     strings.ToUpper(last.Side),
		LastTs:       last.Ts,
	}, recent, nil
}

func spreadBps(bid, ask float64) float64 {
	mid := midpoint(bid, ask)
	if bid <= 0 || ask <= 0 || mid <= 0 {
		return 0
	}
	return ((ask - bid) / mid) * 10000.0
}

func midpoint(bid, ask float64) float64 {
	if bid > 0 && ask > 0 {
		return (bid + ask) / 2
	}
	return maxFloat(bid, ask)
}

func firstPositive(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func whaleTier(usd, base float64) string {
	switch {
	case usd >= base*20:
		return "XL"
	case usd >= base*10:
		return "L"
	case usd >= base*5:
		return "M"
	default:
		return "S"
	}
}

func sleepContext(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}
