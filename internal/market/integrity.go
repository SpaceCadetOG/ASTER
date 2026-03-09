package market

import "time"

type IntegrityResult struct {
	Completeness     float64
	IntegrityPenalty float64
	Flags            []string
}

func evalIntegrity(m Market, cfg RankConfig, now time.Time) IntegrityResult {
	flags := make([]string, 0, 8)
	miss := 0.0
	w := 0.0

	addMiss := func(name string, cond bool, weight float64) {
		if weight <= 0 {
			return
		}
		w += weight
		if cond {
			miss += weight
			flags = append(flags, name)
		}
	}

	addMiss("missing_oi", m.OIUSD == nil || (m.OIUSD != nil && *m.OIUSD <= 0), 0.20)
	addMiss("missing_funding", m.FundingRate == nil, 0.18)
	addMiss("missing_spread", m.SpreadBps == nil, 0.14)
	addMiss("missing_topbook", m.TopBookUSD == nil, 0.14)
	addMiss("missing_est_slip", m.EstSlippageBps == nil, 0.08)

	if cfg.StaleTickerSec > 0 && m.LastTickerTs != nil {
		age := now.Sub(time.Unix(*m.LastTickerTs, 0)).Seconds()
		addMiss("stale_ticker", age > cfg.StaleTickerSec, 0.12)
	}
	if cfg.StaleBookSec > 0 && m.LastBookTs != nil {
		age := now.Sub(time.Unix(*m.LastBookTs, 0)).Seconds()
		addMiss("stale_book", age > cfg.StaleBookSec, 0.08)
	}
	if cfg.StaleOISec > 0 && m.LastOITs != nil {
		age := now.Sub(time.Unix(*m.LastOITs, 0)).Seconds()
		addMiss("stale_oi", age > cfg.StaleOISec, 0.03)
	}
	if cfg.StaleFundingSec > 0 && m.LastFundingTs != nil {
		age := now.Sub(time.Unix(*m.LastFundingTs, 0)).Seconds()
		addMiss("stale_funding", age > cfg.StaleFundingSec, 0.03)
	}

	if w <= 0 {
		return IntegrityResult{Completeness: 1, IntegrityPenalty: 0, Flags: flags}
	}
	comp := clamp01(1 - miss/w)
	penalty := 0.0
	if cfg.EnableDataIntegrity {
		penalty = (1 - comp) * cfg.IntegrityMaxPenalty
	}
	return IntegrityResult{
		Completeness:     comp,
		IntegrityPenalty: penalty,
		Flags:            flags,
	}
}
