TraderBot Project — Phase 1 & 2 Troubleshooting & Maintenance Guide

⚙️ Phase 1 — Scanners (Live Feeds)

Purpose

The scanner phase handles live market feeds, session overlaps, and ranking of top tokens per exchange. It ensures constant data flow and ranking logic for downstream TA engines.

Key Modules
	•	session.go — defines global market sessions (Asia, London, NY) with DST handling.
	•	scanner.go — fetches 24h stats per symbol, computes scores (momentum, funding bias, etc.).
	•	ranker.go — filters and ranks top 5 symbols every 30 seconds per exchange.

Common Issues

Symptom	Likely Cause	Fix
Scanner stops printing	Lost connection or bad API key	Check LoadMarkets() and ensure valid credentials
0 results from one exchange	Endpoint rate-limited or no volume	Retry with lower frequency (30s+) or skip exchange
Scores all zero	Missing normalization or bad math in score formula	Check normalizeChange() and volume weighting
Timestamps wrong	Timezone mismatch	Verify time.LoadLocation() and DST logic

Quick Verification

curl -fsS http://localhost:8080/api/scan?exchange=hyperliquid

Expect to see top-ranked tokens with score, volume, and 24h change.

Maintenance Tips
	•	Refresh API keys monthly if using rate-limited exchanges.
	•	Keep symbols.json updated — inactive pairs can break scoring.
	•	Monitor session overlap timing logs to ensure scanner sync.

⸻

🧠 Phase 2 — TA & Confluence Engine

Purpose

Generates analytical confluence from multiple market layers:
	1.	Trend Metrics (EMA/VWAP)
	2.	Effort Metrics (volume spikes)
	3.	Orderbook Context (imbalances)

Result → Confluence Score (0–100) with Label A/B/C and diagnostic notes.

Core Modules

File	Description
ta/trend.go	EMA(9,21) slope, VWAP distance, bias, trend strength
ta/effort.go	Detects volume spikes, computes effort intensity
ta/orderbook.go	Parses bids/asks, finds dominant walls and imbalance
ta/confluence.go	Fuses Trend + Effort + OB → single graded signal
internal/api/confluence.go	HTTP handler returning full JSON per symbol

Quick API Tests

curl -fsS "http://localhost:8080/api/confluence?symbol=BTCUSDT&tf=15m&n=200&win=20&zmin=2.0&vmin=5000000&levels=50&side=long"

Expect keys: score, label, notes, trend, effort, orderbook.

Typical Symptoms & Fixes

Symptom	Cause	Fix
score=0 or label=C always	EMA flat + low effort + balanced OB	Increase timeframe or lower zmin threshold
orderbook fetch failed	Rate limit or invalid symbol	Reduce levels or retry after delay
Missing spikes	Volume too low	Reduce vmin or window
Trend bias misaligned	VWAP off	Confirm VWAP matches latest candles

Structure

internal/
 └── ta/
     ├── patterns.go
     ├── pivots.go
     ├── structure.go
     ├── trend.go
     ├── effort.go
     ├── orderbook.go
     ├── confluence.go
     ├── fusion.go (Phase 3 placeholder)
     └── *_test.go

Tuning Knobs

Category	Variable	Default	Purpose
Trend	EMA windows	9,21	Sensitivity of bias detection
Trend	VWAP distance	0.002	Reward for deviation strength
Effort	zmin	2.0	Spike detection sigma threshold
Effort	win	20	Rolling window length
OrderBook	levels	50	Depth sampling granularity
Confluence	weights	0.45/0.35/0.20	Trend/Effort/OB influence

Output Interpretation

Field	Meaning
score	0–100 total confluence score
label	A=high confluence, B=moderate, C=weak
notes	human-readable breakdown of strengths/weaknesses
trend.bias	bull/bear/neutral direction
effort.spikeDensity	frequency of abnormal volume activity
orderbook.imbalance	bid/ask dominance (-1→ask heavy, +1→bid heavy)

Tests

Run all unit tests:

go fmt ./internal/ta/...
go test ./internal/ta -v

Expect:
	•	✅ All PASS
	•	🔸 Minor float rounding differences are acceptable (<1%).

Logs & Debug

journalctl -u traderbot -f

Use /api/confluence JSON logs for verifying real-time state transitions.

Edge Cases
	•	Tight-range candles → low TrendScore (normal)
	•	Flash volume spikes with no OB support → C-grade (normal)
	•	One-sided OB without trend confirmation → B-grade (watchlist)

⸻

✅ Final Checklist Before Phase 3
	•	Scanners online and returning top tokens
	•	Trend/Effort/OB modules passing tests
	•	Confluence API stable
	•	Output verified across BTC, ETH, and SOL
	•	Pushed to GitHub with clean module structure

Next: Phase 3 — Backtester & Replay Engine