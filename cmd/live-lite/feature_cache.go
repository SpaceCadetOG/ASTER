package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"go-machine/internal/gate"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

type candleLoadFunc func(symbol string, tf types.TF, limit int) ([]types.Candle, error)

type cachedCandleSeries struct {
	bars      []types.Candle
	expiresAt time.Time
}

type cachedMicroSnapshot struct {
	snapshot  ta.MicroSnapshot
	expiresAt time.Time
}

type cachedEMAPair struct {
	fast      float64
	slow      float64
	expiresAt time.Time
}

type featureCacheStats struct {
	CandleHits   int64
	CandleMisses int64
	MicroHits    int64
	MicroMisses  int64
	EMAHits      int64
	EMAMisses    int64
	Evictions    int64
	CandleKeys   int
	MicroKeys    int
	EMAKeys      int
}

type featureRuntimeCache struct {
	load     candleLoadFunc
	ttl      time.Duration
	maxKeys  int
	now      func() time.Time
	mu       sync.Mutex
	candles  map[string]cachedCandleSeries
	micro    map[string]cachedMicroSnapshot
	emaPairs map[string]cachedEMAPair
	stats    featureCacheStats
}

func newFeatureRuntimeCache(load candleLoadFunc) *featureRuntimeCache {
	ttl := time.Duration(envInt("LIVE_FEATURE_CACHE_TTL_SEC", 3)) * time.Second
	if ttl <= 0 {
		ttl = 3 * time.Second
	}
	maxKeys := envInt("LIVE_FEATURE_CACHE_MAX_KEYS", 512)
	if maxKeys <= 0 {
		maxKeys = 512
	}
	return &featureRuntimeCache{
		load:     load,
		ttl:      ttl,
		maxKeys:  maxKeys,
		now:      time.Now,
		candles:  map[string]cachedCandleSeries{},
		micro:    map[string]cachedMicroSnapshot{},
		emaPairs: map[string]cachedEMAPair{},
	}
}

func (c *featureRuntimeCache) candleSeries(symbol string, tf types.TF, limit int) ([]types.Candle, error) {
	if c == nil || c.load == nil {
		return nil, fmt.Errorf("feature cache loader missing")
	}
	if limit <= 0 {
		limit = 64
	}
	raw := strings.ToUpper(strings.TrimSpace(symbol))
	if raw == "" {
		return nil, fmt.Errorf("symbol required")
	}
	now := c.now()
	key := fmt.Sprintf("%s|%s|%d", raw, tf.String(), limit)
	fallbackTTL := time.Duration(envInt("LIVE_FEATURE_CACHE_FALLBACK_TTL_SEC", 30)) * time.Second
	if fallbackTTL <= 0 {
		fallbackTTL = 30 * time.Second
	}
	var fallback []types.Candle
	c.mu.Lock()
	if cached, ok := c.candles[key]; ok && now.Before(cached.expiresAt) {
		c.stats.CandleHits++
		out := append([]types.Candle(nil), cached.bars...)
		c.mu.Unlock()
		return out, nil
	}
	if cached, ok := c.candles[key]; ok && len(cached.bars) > 0 {
		fallback = append([]types.Candle(nil), cached.bars...)
	}
	c.stats.CandleMisses++
	c.mu.Unlock()

	bars, err := c.load(raw, tf, limit)
	if err != nil {
		if len(fallback) > 0 {
			c.mu.Lock()
			c.pruneLocked(now)
			c.candles[key] = cachedCandleSeries{bars: append([]types.Candle(nil), fallback...), expiresAt: now.Add(fallbackTTL)}
			c.mu.Unlock()
			return fallback, nil
		}
		return nil, err
	}
	out := append([]types.Candle(nil), bars...)
	c.mu.Lock()
	c.pruneLocked(now)
	c.candles[key] = cachedCandleSeries{bars: append([]types.Candle(nil), out...), expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()
	return out, nil
}

func (c *featureRuntimeCache) microSnapshot(symbol string, limit, atrLen, fastSlopeN, slowSlopeN, volumeN int) (ta.MicroSnapshot, []types.Candle, error) {
	if c == nil {
		return ta.MicroSnapshot{}, nil, fmt.Errorf("feature cache missing")
	}
	if limit <= 0 {
		limit = 64
	}
	raw := strings.ToUpper(strings.TrimSpace(symbol))
	now := c.now()
	key := fmt.Sprintf("%s|%d|%d|%d|%d|%d", raw, limit, atrLen, fastSlopeN, slowSlopeN, volumeN)
	fallbackTTL := time.Duration(envInt("LIVE_FEATURE_CACHE_FALLBACK_TTL_SEC", 30)) * time.Second
	if fallbackTTL <= 0 {
		fallbackTTL = 30 * time.Second
	}
	var fallback *ta.MicroSnapshot
	c.mu.Lock()
	if cached, ok := c.micro[key]; ok && now.Before(cached.expiresAt) {
		c.stats.MicroHits++
		c.mu.Unlock()
		bars, err := c.candleSeries(raw, types.TF1m, limit)
		return cached.snapshot, bars, err
	}
	if cached, ok := c.micro[key]; ok {
		tmp := cached.snapshot
		fallback = &tmp
	}
	c.stats.MicroMisses++
	c.mu.Unlock()

	bars, err := c.candleSeries(raw, types.TF1m, limit)
	if err != nil {
		if fallback != nil {
			c.mu.Lock()
			c.pruneLocked(now)
			c.micro[key] = cachedMicroSnapshot{snapshot: *fallback, expiresAt: now.Add(fallbackTTL)}
			c.mu.Unlock()
			return *fallback, nil, nil
		}
		return ta.MicroSnapshot{}, nil, err
	}
	snap := ta.SnapshotFromTypesCandles(bars, atrLen, fastSlopeN, slowSlopeN, volumeN)
	c.mu.Lock()
	c.pruneLocked(now)
	c.micro[key] = cachedMicroSnapshot{snapshot: snap, expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()
	return snap, bars, nil
}

func (c *featureRuntimeCache) mtfEMAPair(symbol string, tf types.TF, limit, fastLen, slowLen int) (float64, float64, error) {
	if c == nil {
		return 0, 0, fmt.Errorf("feature cache missing")
	}
	if limit <= 0 {
		limit = 64
	}
	raw := strings.ToUpper(strings.TrimSpace(symbol))
	now := c.now()
	key := fmt.Sprintf("%s|%s|%d|%d|%d", raw, tf.String(), limit, fastLen, slowLen)
	c.mu.Lock()
	if cached, ok := c.emaPairs[key]; ok && now.Before(cached.expiresAt) {
		c.stats.EMAHits++
		c.mu.Unlock()
		return cached.fast, cached.slow, nil
	}
	c.stats.EMAMisses++
	c.mu.Unlock()
	bars, err := c.candleSeries(raw, tf, limit)
	if err != nil {
		return 0, 0, err
	}
	fast, slow := ta.EMAPairFromTypesCandles(bars, fastLen, slowLen)
	c.mu.Lock()
	c.pruneLocked(now)
	c.emaPairs[key] = cachedEMAPair{fast: fast, slow: slow, expiresAt: now.Add(c.ttl)}
	c.mu.Unlock()
	return fast, slow, nil
}

func (c *featureRuntimeCache) pruneLocked(now time.Time) {
	if c == nil {
		return
	}
	for key, cached := range c.candles {
		if !now.Before(cached.expiresAt) {
			delete(c.candles, key)
			c.stats.Evictions++
		}
	}
	for key, cached := range c.micro {
		if !now.Before(cached.expiresAt) {
			delete(c.micro, key)
			c.stats.Evictions++
		}
	}
	for key, cached := range c.emaPairs {
		if !now.Before(cached.expiresAt) {
			delete(c.emaPairs, key)
			c.stats.Evictions++
		}
	}
	for len(c.candles) > c.maxKeys {
		for key := range c.candles {
			delete(c.candles, key)
			c.stats.Evictions++
			break
		}
	}
	for len(c.micro) > c.maxKeys {
		for key := range c.micro {
			delete(c.micro, key)
			c.stats.Evictions++
			break
		}
	}
	for len(c.emaPairs) > c.maxKeys {
		for key := range c.emaPairs {
			delete(c.emaPairs, key)
			c.stats.Evictions++
			break
		}
	}
}

func (c *featureRuntimeCache) statsSnapshot() featureCacheStats {
	if c == nil {
		return featureCacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snap := c.stats
	snap.CandleKeys = len(c.candles)
	snap.MicroKeys = len(c.micro)
	snap.EMAKeys = len(c.emaPairs)
	return snap
}

func (s featureCacheStats) delta(prev featureCacheStats) featureCacheStats {
	out := s
	out.CandleHits -= prev.CandleHits
	out.CandleMisses -= prev.CandleMisses
	out.MicroHits -= prev.MicroHits
	out.MicroMisses -= prev.MicroMisses
	out.EMAHits -= prev.EMAHits
	out.EMAMisses -= prev.EMAMisses
	out.Evictions -= prev.Evictions
	return out
}

func (s featureCacheStats) totalHits() int64 {
	return s.CandleHits + s.MicroHits + s.EMAHits
}

func (s featureCacheStats) totalMisses() int64 {
	return s.CandleMisses + s.MicroMisses + s.EMAMisses
}

func buildGateInputWithCache(cache *featureRuntimeCache, cand candidate, cfg gate.Config) gate.Input {
	raw := strings.ToUpper(strings.TrimSpace(cand.Entry.Symbol))
	in := gate.Input{
		Symbol: raw,
		Side:   cand.Side,
		Grade:  cand.Entry.CurrentGrade,
		Score:  cand.Entry.CurrentScore,
		Slope:  cand.Entry.ScoreSlope,
	}
	if cache == nil {
		return in
	}
	if snap, _, err := cache.microSnapshot(raw, 120, envInt("LIVE_ATR_LEN", 14), 3, 15, 20); err == nil {
		in.VolumeRatio = snap.VolumeRatio
	}
	mtfTFs := cfg.MTF.TFs
	if len(mtfTFs) == 0 {
		mtfTFs = []string{"1m", "5m"}
	}
	for _, tfS := range mtfTFs {
		tf, ok := types.ParseTF(tfS)
		if !ok {
			continue
		}
		fast, slow, err := cache.mtfEMAPair(raw, tf, 64, cfg.MTF.EMAFast, cfg.MTF.EMASlow)
		if err != nil || fast == 0 || slow == 0 {
			continue
		}
		in.MTF = append(in.MTF, gate.MTFSnapshot{TF: tf.String(), EMAFast: fast, EMASlow: slow})
	}
	return in
}

func estimateATRPctWithCache(cache *featureRuntimeCache, symbol string, candlesN, atrN int) float64 {
	if cache == nil {
		return estimateATRPct(symbol, candlesN, atrN)
	}
	snap, _, err := cache.microSnapshot(symbol, candlesN, atrN, 3, 15, 20)
	if err != nil {
		return estimateATRPct(symbol, candlesN, atrN)
	}
	return snap.ATRPct
}
