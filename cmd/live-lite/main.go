package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/features"
	"go-machine/internal/inplay"
	"go-machine/internal/market"
	"go-machine/internal/strategies"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

type candidate struct {
	Entry inplay.Entry
	Side  string // BUY/SELL
	Strat string
	Conf  float64
	Sig   strategies.Signal
}

type positionView struct {
	Symbol   string
	Side     string
	SizeAbs  float64
	Entry    float64
	Mark     float64
	Unreal   float64
	Leverage float64
}

type accountSnapshot struct {
	AvailableUSDT float64
	Balances      []aster.Balance
	Positions     []positionView
}

type safetyConfig struct {
	enableLiveTrading bool
	maxLeverage       int
	minAvailUSDT      float64
	maxOrdersPerDay   int
	orderCooldown     time.Duration
	pauseFile         string
	allowSymbols      map[string]struct{}
	allowShorts       bool
}

type telegramSink struct {
	enabled  bool
	token    string
	chatID   string
	timeout  time.Duration
	dedupe   time.Duration
	client   *http.Client
	lastSent map[string]time.Time
}

type symbolMeta struct {
	LastPrice float64
	Move24h   float64
}

type paperPosition struct {
	Symbol     string
	Side       string
	Entry      float64
	Qty        float64 // remaining qty
	InitialQty float64
	Margin     float64
	Leverage   int
	Stop       float64
	TP1        float64
	TP2        float64
	TP3        float64
	HitTP1     bool
	HitTP2     bool
	HitTP3     bool
	TrailOn    bool
	TrailStop  float64
	TrailRef   float64
	Realized   float64
	OpenedAt   time.Time
}

type paperTrader struct {
	enabled      bool
	startBal     float64
	balance      float64
	reserve      float64
	feeBps       float64
	stopPct      float64
	tp1R         float64
	tp2R         float64
	tp3R         float64
	tp1Frac      float64
	tp2Frac      float64
	tp3Frac      float64
	trailAfterTP int
	trailStopPct float64
	tradesCSV    string
	equityCSV    string
	maxOpen      int
	positions    map[string]*paperPosition
	reportLoc    *time.Location
	dayStats     map[string]*paperDayStats
}

type paperDayStats struct {
	Trades  int
	Wins    int
	Losses  int
	Gross   float64
	Fees    float64
	Net     float64
	Reasons map[string]int
}

func main() {
	scanEvery := time.Duration(envInt("LIVE_SCAN_SEC", 30)) * time.Second
	dryRun := envBool("LIVE_DRY_RUN", true)
	minGrade := envStr("LIVE_MIN_GRADE", "B")
	reserveUSDT := envFloat("LIVE_RESERVE_USDT", 5)
	reserveMode := strings.ToLower(envStr("LIVE_RESERVE_MODE", "fixed")) // fixed|percent
	reservePct := envFloat("LIVE_RESERVE_PCT", 50.0)
	tradeMargin := envFloat("LIVE_TRADE_MARGIN_USDT", 10)
	tradeMarginMode := strings.ToLower(envStr("LIVE_TRADE_MARGIN_MODE", "fixed")) // fixed|percent|slots
	tradeMarginPct := envFloat("LIVE_TRADE_MARGIN_PCT", 10.0)
	tradeSlots := envInt("LIVE_TRADE_SLOTS", 5)
	if tradeSlots <= 0 {
		tradeSlots = 5
	}
	tradeMarginMin := envFloat("LIVE_TRADE_MARGIN_MIN_USDT", 5.0)
	tradeMarginMax := envFloat("LIVE_TRADE_MARGIN_MAX_USDT", 200.0)
	leverageMode := strings.ToLower(envStr("LIVE_LEVERAGE_MODE", "grade")) // grade|fixed|auto
	leverageFixed := envInt("LIVE_LEVERAGE_FIXED", 3)
	leverageMin := envInt("LIVE_LEVERAGE_MIN", 1)
	entryBps := envFloat("LIVE_ENTRY_OFFSET_BPS", 2)
	showAccount := envBool("LIVE_SHOW_ACCOUNT", true)
	accountAssets := envCSV("LIVE_ACCOUNT_ASSETS", "USDT")
	if entryBps < 0 {
		entryBps = -entryBps
	}
	gradeTopN := envInt("LIVE_GRADE_TOP_N", 6)
	if gradeTopN <= 0 {
		gradeTopN = 6
	}
	strategyTopN := envInt("LIVE_STRATEGY_TOP_N", 3)
	if strategyTopN <= 0 {
		strategyTopN = 3
	}
	maxOpenPos := envInt("LIVE_MAX_OPEN_POS", 5)
	if maxOpenPos <= 0 {
		maxOpenPos = 1
	}

	inplayCfg := inplay.Config{
		MinGrade:       envStr("INPLAY_MIN_GRADE", "C"),
		MinVolumeUSD:   envFloat("INPLAY_MIN_VOL_USD", 1_000_000),
		HistoryN:       envInt("INPLAY_HISTORY_N", 5),
		RiseN:          envInt("INPLAY_RISE_N", 3),
		DropGradeScans: envInt("INPLAY_DROP_SCANS", 2),
		FallScans:      envInt("INPLAY_FALL_SCANS", 2),
		TTL:            time.Duration(envInt("INPLAY_TTL_MIN", 30)) * time.Minute,
	}
	longTrk := inplay.NewTracker("long", inplayCfg)
	shortTrk := inplay.NewTracker("short", inplayCfg)

	client := aster.New("")
	rest := buildRESTFromConfig()
	if rest == nil {
		fmt.Println("live-lite: no credentials found, forcing DRY_RUN mode")
		dryRun = true
	}
	safety := loadSafetyConfig(reserveUSDT, tradeMargin)
	if !dryRun && !safety.enableLiveTrading {
		fmt.Println("live-lite: LIVE_DRY_RUN=0 but LIVE_ENABLE_LIVE_TRADING is not enabled, forcing DRY_RUN")
		dryRun = true
	}
	tg := newTelegramSink()
	tgVerbose := envBool("LIVE_TG_VERBOSE", false)
	digestEvery := time.Duration(envInt("LIVE_TG_DIGEST_MIN", 15)) * time.Minute
	if digestEvery < time.Minute {
		digestEvery = 15 * time.Minute
	}
	digestLimit := envInt("LIVE_TG_LIST_LIMIT", 12)
	if digestLimit <= 0 {
		digestLimit = 12
	}
	tradeUpdateEvery := time.Duration(envInt("LIVE_TG_TRADE_UPDATE_MIN", 5)) * time.Minute
	if tradeUpdateEvery < time.Minute {
		tradeUpdateEvery = 5 * time.Minute
	}
	tradeUpdateTop := envInt("LIVE_TG_TRADE_TOP_N", 5)
	if tradeUpdateTop <= 0 {
		tradeUpdateTop = 5
	}
	reportHour := envInt("LIVE_TG_DAILY_REPORT_HOUR", 0)
	reportMinute := envInt("LIVE_TG_DAILY_REPORT_MIN", 0)
	if reportHour < 0 || reportHour > 23 {
		reportHour = 0
	}
	if reportMinute < 0 || reportMinute > 59 {
		reportMinute = 0
	}
	nextDigestAt := time.Now().UTC().Add(10 * time.Second)
	nextTradeUpdateAt := time.Now().UTC().Add(45 * time.Second)
	lastDailyReportDay := ""
	paper := newPaperTrader(dryRun, reserveUSDT, maxOpenPos)

	fmt.Printf("live-lite started (scan=%s dry_run=%v min_grade=%s reserve=%s fixed=%.2f margin=%s lev_mode=%s)\n",
		scanEvery, dryRun, strings.ToUpper(minGrade),
		reserveMode, reserveUSDT, fmt.Sprintf("%s(%.2f)", tradeMarginMode, tradeMargin), leverageMode)
	fmt.Printf("safety: live_enabled=%v max_lev=%d min_avail=%.2f max_orders_day=%d cooldown=%s allow_shorts=%v pause_file=%s\n",
		safety.enableLiveTrading, safety.maxLeverage, safety.minAvailUSDT, safety.maxOrdersPerDay, safety.orderCooldown, safety.allowShorts, safety.pauseFile)
	tg.Sendf("live-lite started scan=%s dry_run=%v min_grade=%s digest=%s", scanEvery, dryRun, strings.ToUpper(minGrade), digestEvery)

	tk := time.NewTicker(scanEvery)
	defer tk.Stop()
	lastTopKey := ""
	lastOrderAt := time.Time{}
	orderCountByDay := map[string]int{}
	for {
		now := time.Now().UTC()
		mkts := client.FetchAllMarkets()
		longRows := market.ScoreAndFilter(mkts)
		shortRows := market.ScoreAndFilterShort(mkts)
		metaBySymbol := buildSymbolMeta(longRows, shortRows)
		if paper.enabled {
			paper.CheckExit(now, metaBySymbol)
		}
		if paper.enabled && tg != nil && tg.enabled {
			if now.After(nextTradeUpdateAt) {
				if msg := paper.TradeUpdateMessage(metaBySymbol, tradeUpdateTop); msg != "" {
					tg.Sendf("%s", msg)
				}
				nextTradeUpdateAt = now.Add(tradeUpdateEvery)
			}
			localNow := now.In(paper.reportLoc)
			if localNow.Hour() > reportHour || (localNow.Hour() == reportHour && localNow.Minute() >= reportMinute) {
				dayKey := localNow.AddDate(0, 0, -1).Format("2006-01-02")
				if dayKey != lastDailyReportDay {
					if msg, ok := paper.DailyReportMessage(dayKey); ok {
						tg.Sendf("%s", msg)
					}
					lastDailyReportDay = dayKey
				}
			}
		}

		longEligible, longConf := buildEligible(client, longRows, "long", gradeTopN)
		shortEligible, shortConf := buildEligible(client, shortRows, "short", gradeTopN)

		longTrk.Update(now, longEligible, longConf)
		shortTrk.Update(now, shortEligible, shortConf)

		longInPlay := longTrk.Entries()
		shortInPlay := shortTrk.Entries()
		printInPlay("LONG", longInPlay)
		printInPlay("SHORT", shortInPlay)
		if time.Now().UTC().After(nextDigestAt) {
			sendInPlayDigest(tg, longInPlay, shortInPlay, metaBySymbol, dryRun, digestLimit)
			nextDigestAt = time.Now().UTC().Add(digestEvery)
		}

		var acct accountSnapshot
		if rest != nil && showAccount {
			snap, err := fetchAccountSnapshot(rest, accountAssets)
			if err != nil {
				fmt.Println("live-lite: account snapshot error:", err)
			} else {
				acct = snap
				printAccountSnapshot(snap, accountAssets)
			}
		}
		if paper.enabled {
			fmt.Println(paper.Summary(metaBySymbol))
			_ = paper.LogEquity(now, metaBySymbol)
		}
		effectiveReserve := computeReserveUSDT(
			reserveMode,
			reserveUSDT,
			reservePct,
			acct.AvailableUSDT,
			paper,
		)
		effectiveMargin := computeTradeMargin(
			tradeMarginMode,
			tradeMargin,
			tradeMarginPct,
			tradeSlots,
			tradeMarginMin,
			tradeMarginMax,
			effectiveReserve,
			acct.AvailableUSDT,
			paper,
		)

		cands := chooseCandidates(longInPlay, shortInPlay, minGrade)
		cands = rankWithStrategy(client, cands, strategyTopN)
		if len(cands) == 0 {
			fmt.Println("live-lite: no trade candidate")
			<-tk.C
			continue
		}
		best := cands[0]
		effectiveLev := computeLeverage(best, leverageMode, leverageFixed, leverageMin, safety.maxLeverage)
		fmt.Printf("live-lite: top candidate %s side=%s grade=%s score=%.2f slope=%.3f rank=%.2f strat=%s conf=%.2f\n",
			best.Entry.Symbol, best.Side, best.Entry.CurrentGrade, best.Entry.CurrentScore, best.Entry.ScoreSlope, best.Entry.Rank, best.Strat, best.Conf)
		topKey := fmt.Sprintf("%s|%s|%s", best.Entry.Symbol, best.Side, best.Entry.CurrentGrade)
		if tgVerbose && topKey != lastTopKey {
			tg.Sendf("top candidate %s %s grade=%s score=%.2f slope=%.3f rank=%.2f strat=%s conf=%.2f",
				strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, best.Entry.CurrentGrade,
				best.Entry.CurrentScore, best.Entry.ScoreSlope, best.Entry.Rank, best.Strat, best.Conf)
			lastTopKey = topKey
		}

		if rest == nil || dryRun {
			printTradeIntent(best, entryBps, effectiveMargin, effectiveLev)
			if paper.enabled {
				pp, err := paper.MaybeEnter(now, best, entryBps, effectiveMargin, effectiveLev, metaBySymbol)
				if err != nil {
					fmt.Println("paper enter skip:", err)
				} else if pp != nil {
					tg.Sendf("PAPER ENTER %s %s margin=$%.2f lev=%dx entry=%.6f tp1=%.6f tp2=%.6f tp3=%.6f sl=%.6f",
						pp.Symbol, pp.Side, pp.Margin, pp.Leverage, pp.Entry, pp.TP1, pp.TP2, pp.TP3, pp.Stop)
				}
			}
			if tgVerbose {
				tg.Sendf("DRY_RUN intent %s %s margin=$%.2f grade=%s score=%.2f",
					strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, effectiveMargin, best.Entry.CurrentGrade, best.Entry.CurrentScore)
			}
			<-tk.C
			continue
		}
		if reason := safetyReject(safety, best, time.Now(), lastOrderAt, orderCountByDay); reason != "" {
			fmt.Println("live-lite: safety skip:", reason)
			if tgVerbose {
				tg.Sendf("safety skip %s %s: %s", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, reason)
			}
			<-tk.C
			continue
		}

		avail := acct.AvailableUSDT
		if avail <= 0 {
			var err error
			avail, err = availableUSDT(rest)
			if err != nil {
				fmt.Println("live-lite: balance error:", err)
				<-tk.C
				continue
			}
		}
		if avail < safety.minAvailUSDT {
			fmt.Printf("live-lite: safety skip (available %.4f < min required %.4f)\n", avail, safety.minAvailUSDT)
			if tgVerbose {
				tg.Sendf("safety skip %s %s: available %.4f < min %.4f",
					strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, avail, safety.minAvailUSDT)
			}
			<-tk.C
			continue
		}
		effectiveReserve = computeReserveUSDT(reserveMode, reserveUSDT, reservePct, avail, paper)
		effectiveMargin = computeTradeMargin(tradeMarginMode, tradeMargin, tradeMarginPct, tradeSlots, tradeMarginMin, tradeMarginMax, effectiveReserve, avail, paper)
		usable := avail - effectiveReserve
		if usable < effectiveMargin {
			fmt.Printf("live-lite: skip (available %.4f, usable %.4f < margin %.4f)\n", avail, usable, effectiveMargin)
			if tgVerbose {
				tg.Sendf("skip %s %s: usable %.4f < margin %.4f",
					strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, usable, effectiveMargin)
			}
			<-tk.C
			continue
		}

		openCount := len(acct.Positions)
		if !showAccount || len(acct.Positions) == 0 {
			var err error
			openCount, err = countOpenPositions(rest)
			if err != nil {
				fmt.Println("live-lite: position check error:", err)
				<-tk.C
				continue
			}
		}
		if openCount >= maxOpenPos {
			fmt.Printf("live-lite: skip (open positions=%d, max=%d)\n", openCount, maxOpenPos)
			if tgVerbose {
				tg.Sendf("skip %s %s: open positions=%d max=%d", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, openCount, maxOpenPos)
			}
			<-tk.C
			continue
		}

		if err := placeEntry(rest, best, entryBps, effectiveMargin, effectiveLev); err != nil {
			fmt.Println("live-lite: place error:", err)
			if tgVerbose {
				tg.Sendf("place error %s %s: %v", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, err)
			}
		} else {
			lastOrderAt = time.Now()
			dayKey := time.Now().UTC().Format("2006-01-02")
			orderCountByDay[dayKey]++
			tg.Sendf("placed %s %s margin=$%.2f grade=%s",
				strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, effectiveMargin, best.Entry.CurrentGrade)
		}

		<-tk.C
	}
}

func sendInPlayDigest(tg *telegramSink, longInPlay, shortInPlay []inplay.Entry, meta map[string]symbolMeta, dryRun bool, limit int) {
	if tg == nil || !tg.enabled {
		return
	}
	now := time.Now().UTC()
	var b strings.Builder
	mode := "LIVE"
	if dryRun {
		mode = "DRY_RUN"
	}
	fmt.Fprintf(&b, "Live-Lite Digest | %s | %s UTC\n", mode, now.Format("15:04"))
	appendList := func(tag string, rows []inplay.Entry) {
		fmt.Fprintf(&b, "\n%s (%d):\n", tag, len(rows))
		if len(rows) == 0 {
			b.WriteString("  • none\n")
			return
		}
		n := len(rows)
		if n > limit {
			n = limit
		}
		for i := 0; i < n; i++ {
			e := rows[i]
			fs := e.FirstSeen.UTC().Format("15:04")
			raw := strings.ToUpper(aster.RawSymbol(e.Symbol))
			m := meta[raw]
			price := "-"
			move := "-"
			status := string(e.State)
			switch e.State {
			case inplay.StateInPlay:
				status = "IN_PLAY"
			case inplay.StateWarming:
				status = "WARMING"
			case inplay.StateCooling:
				status = "COOLING"
			}
			slopeArrow := "→"
			if e.ScoreSlope > 0 {
				slopeArrow = "↑"
			} else if e.ScoreSlope < 0 {
				slopeArrow = "↓"
			}
			if m.LastPrice > 0 {
				price = fmt.Sprintf("%.6f", m.LastPrice)
			}
			if m.Move24h != 0 {
				move = fmt.Sprintf("%+.2f%%", m.Move24h)
			}
			fmt.Fprintf(&b, "  %2d) %-10s | px %-10s | %8s | %-7s | g=%-3s | s=%6.2f %s%.3f | in %s\n",
				i+1, raw, price, move, status, e.CurrentGrade, e.CurrentScore, slopeArrow, abs(e.ScoreSlope), fs)
		}
	}
	appendList("LONG", longInPlay)
	appendList("SHORT", shortInPlay)
	tg.Sendf("%s", strings.TrimSpace(b.String()))
}

func buildSymbolMeta(longRows, shortRows []market.Scored) map[string]symbolMeta {
	out := make(map[string]symbolMeta, len(longRows)+len(shortRows))
	put := func(rows []market.Scored) {
		for _, r := range rows {
			raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(r.Symbol)))
			if raw == "" {
				continue
			}
			out[raw] = symbolMeta{
				LastPrice: r.LastPrice,
				Move24h:   r.Change24h,
			}
		}
	}
	put(longRows)
	put(shortRows)
	return out
}

func newPaperTrader(dryRun bool, reserveUSDT float64, maxOpen int) *paperTrader {
	enabled := dryRun && envBool("LIVE_PAPER_ENABLE", true)
	start := envFloat("LIVE_PAPER_START_BALANCE", 1000)
	if start <= 0 {
		start = 1000
	}
	stopPct := envFloat("LIVE_PAPER_STOP_PCT", 2.0)
	if stopPct <= 0 {
		stopPct = 2.0
	}
	tp1R := envFloat("LIVE_PAPER_TP1_R", 1.0)
	tp2R := envFloat("LIVE_PAPER_TP2_R", 2.0)
	tp3R := envFloat("LIVE_PAPER_TP3_R", 3.0)
	if tp1R <= 0 {
		tp1R = 1.0
	}
	if tp2R < tp1R {
		tp2R = tp1R
	}
	if tp3R < tp2R {
		tp3R = tp2R
	}
	tp1Frac := envFloat("LIVE_PAPER_TP1_FRAC", 0.4)
	tp2Frac := envFloat("LIVE_PAPER_TP2_FRAC", 0.3)
	tp3Frac := envFloat("LIVE_PAPER_TP3_FRAC", 0.3)
	if tp1Frac < 0 {
		tp1Frac = 0
	}
	if tp2Frac < 0 {
		tp2Frac = 0
	}
	if tp3Frac < 0 {
		tp3Frac = 0
	}
	sumFrac := tp1Frac + tp2Frac + tp3Frac
	if sumFrac <= 0 {
		tp1Frac, tp2Frac, tp3Frac = 0.4, 0.3, 0.3
		sumFrac = 1.0
	}
	tp1Frac /= sumFrac
	tp2Frac /= sumFrac
	tp3Frac /= sumFrac
	trailAfterTP := envInt("LIVE_PAPER_TRAIL_AFTER_TP", 2)
	if trailAfterTP < 1 {
		trailAfterTP = 1
	}
	if trailAfterTP > 3 {
		trailAfterTP = 3
	}
	trailStopPct := envFloat("LIVE_PAPER_TRAIL_STOP_PCT", 1.0)
	if trailStopPct <= 0 {
		trailStopPct = 1.0
	}
	feeBps := envFloat("LIVE_PAPER_FEE_BPS", 6.0)
	if feeBps < 0 {
		feeBps = 0
	}
	if maxOpen <= 0 {
		maxOpen = 1
	}
	locName := envStr("LIVE_REPORT_TZ", "America/Chicago")
	reportLoc, err := time.LoadLocation(locName)
	if err != nil {
		reportLoc = time.Local
	}
	return &paperTrader{
		enabled:      enabled,
		startBal:     start,
		balance:      start,
		reserve:      reserveUSDT,
		feeBps:       feeBps,
		stopPct:      stopPct,
		tp1R:         tp1R,
		tp2R:         tp2R,
		tp3R:         tp3R,
		tp1Frac:      tp1Frac,
		tp2Frac:      tp2Frac,
		tp3Frac:      tp3Frac,
		trailAfterTP: trailAfterTP,
		trailStopPct: trailStopPct,
		tradesCSV:    envStr("LIVE_PAPER_TRADES_FILE", "out/paper_trades.csv"),
		equityCSV:    envStr("LIVE_PAPER_EQUITY_FILE", "out/paper_equity.csv"),
		maxOpen:      maxOpen,
		positions:    map[string]*paperPosition{},
		reportLoc:    reportLoc,
		dayStats:     map[string]*paperDayStats{},
	}
}

func (p *paperTrader) Summary(meta map[string]symbolMeta) string {
	if p == nil || !p.enabled {
		return ""
	}
	openPnL := 0.0
	realized := 0.0
	openTxt := "none"
	if len(p.positions) > 0 {
		parts := make([]string, 0, len(p.positions))
		for _, pos := range p.positions {
			raw := strings.ToUpper(aster.RawSymbol(pos.Symbol))
			m := meta[raw]
			mark := m.LastPrice
			pnl := 0.0
			if mark > 0 {
				if strings.EqualFold(pos.Side, "BUY") {
					pnl = (mark - pos.Entry) * pos.Qty
				} else {
					pnl = (pos.Entry - mark) * pos.Qty
				}
			}
			openPnL += pnl
			realized += pos.Realized
			parts = append(parts, fmt.Sprintf("%s %s e=%.6f m=%.6f q=%.6f upnl=%+.3f", raw, pos.Side, pos.Entry, mark, pos.Qty, pnl))
		}
		sort.Strings(parts)
		if len(parts) > 3 {
			openTxt = strings.Join(parts[:3], " | ") + fmt.Sprintf(" | +%d more", len(parts)-3)
		} else {
			openTxt = strings.Join(parts, " | ")
		}
	}
	eq := p.balance + openPnL
	return fmt.Sprintf("🧪 PAPER balance=%.2f equity=%.2f openPnL=%+.2f realized=%+.2f openCount=%d/%d open=%s",
		p.balance, eq, openPnL, realized, len(p.positions), p.maxOpen, openTxt)
}

func (p *paperTrader) MaybeEnter(now time.Time, c candidate, entryBps, margin float64, leverage int, meta map[string]symbolMeta) (*paperPosition, error) {
	if p == nil || !p.enabled {
		return nil, nil
	}
	if len(p.positions) >= p.maxOpen {
		return nil, fmt.Errorf("max paper positions reached (%d)", p.maxOpen)
	}
	if p.balance-p.reserve < margin {
		return nil, fmt.Errorf("insufficient usable paper balance")
	}
	raw := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	if _, exists := p.positions[raw]; exists {
		return nil, fmt.Errorf("symbol already open")
	}
	m := meta[raw]
	if m.LastPrice <= 0 {
		return nil, fmt.Errorf("no price for %s", raw)
	}
	entry := m.LastPrice
	if strings.EqualFold(c.Side, "BUY") {
		entry = m.LastPrice * (1 - entryBps/10000.0)
	} else {
		entry = m.LastPrice * (1 + entryBps/10000.0)
	}
	lev := leverage
	if lev <= 0 {
		lev = 1
	}
	notional := margin * float64(lev)
	if entry <= 0 || notional <= 0 {
		return nil, fmt.Errorf("invalid entry/notional")
	}
	qty := notional / entry
	stopPct := p.stopPct / 100.0
	tp1Pct := (p.stopPct * p.tp1R) / 100.0
	tp2Pct := (p.stopPct * p.tp2R) / 100.0
	tp3Pct := (p.stopPct * p.tp3R) / 100.0
	stop := entry
	tp1 := entry
	tp2 := entry
	tp3 := entry
	if strings.EqualFold(c.Side, "BUY") {
		stop = entry * (1 - stopPct)
		tp1 = entry * (1 + tp1Pct)
		tp2 = entry * (1 + tp2Pct)
		tp3 = entry * (1 + tp3Pct)
	} else {
		stop = entry * (1 + stopPct)
		tp1 = entry * (1 - tp1Pct)
		tp2 = entry * (1 - tp2Pct)
		tp3 = entry * (1 - tp3Pct)
	}
	fee := notional * p.feeBps / 10000.0
	p.balance -= fee
	pos := &paperPosition{
		Symbol:     raw,
		Side:       strings.ToUpper(c.Side),
		Entry:      entry,
		Qty:        qty,
		InitialQty: qty,
		Margin:     margin,
		Leverage:   lev,
		Stop:       stop,
		TP1:        tp1,
		TP2:        tp2,
		TP3:        tp3,
		TrailRef:   entry,
		OpenedAt:   now,
	}
	p.positions[raw] = pos
	fmt.Printf("paper entered %s %s entry=%.6f qty=%.6f lev=%dx tp1=%.6f tp2=%.6f tp3=%.6f sl=%.6f fee=%.4f\n",
		raw, c.Side, entry, qty, lev, tp1, tp2, tp3, stop, fee)
	return pos, nil
}

func (p *paperTrader) CheckExit(now time.Time, meta map[string]symbolMeta) {
	if p == nil || !p.enabled || len(p.positions) == 0 {
		return
	}
	// Iterate over a snapshot of keys because positions can be closed during processing.
	keys := make([]string, 0, len(p.positions))
	for k := range p.positions {
		keys = append(keys, k)
	}
	for _, raw := range keys {
		pos := p.positions[raw]
		if pos == nil {
			continue
		}
		m := meta[raw]
		if m.LastPrice <= 0 {
			continue
		}
		sideBuy := strings.EqualFold(pos.Side, "BUY")
		mark := m.LastPrice

		// 1) Hard stop has highest priority.
		if (sideBuy && mark <= pos.Stop) || (!sideBuy && mark >= pos.Stop) {
			p.exitPortion(now, pos, "SL", pos.Stop, pos.Qty)
			continue
		}

		// 2) Scale-out targets.
		if !pos.HitTP1 && p.hitPrice(sideBuy, mark, pos.TP1) {
			q := p.targetQty(pos.InitialQty, p.tp1Frac, pos.Qty)
			p.exitPortion(now, pos, "TP1", pos.TP1, q)
			pos = p.positions[raw]
			if pos == nil {
				continue
			}
			pos.HitTP1 = true
		}
		if pos == nil {
			continue
		}
		if !pos.HitTP2 && p.hitPrice(sideBuy, mark, pos.TP2) {
			q := p.targetQty(pos.InitialQty, p.tp2Frac, pos.Qty)
			p.exitPortion(now, pos, "TP2", pos.TP2, q)
			pos = p.positions[raw]
			if pos == nil {
				continue
			}
			pos.HitTP2 = true
			if p.trailAfterTP <= 2 {
				pos.TrailOn = true
				pos.TrailRef = mark
				pos.TrailStop = p.calcTrailStop(sideBuy, mark)
			}
		}
		if pos == nil {
			continue
		}
		if !pos.HitTP3 && p.hitPrice(sideBuy, mark, pos.TP3) {
			q := p.targetQty(pos.InitialQty, p.tp3Frac, pos.Qty)
			p.exitPortion(now, pos, "TP3", pos.TP3, q)
			pos = p.positions[raw]
			if pos == nil {
				continue
			}
			pos.HitTP3 = true
			if p.trailAfterTP <= 3 {
				pos.TrailOn = true
				pos.TrailRef = mark
				pos.TrailStop = p.calcTrailStop(sideBuy, mark)
			}
		}
		if pos == nil {
			continue
		}

		// 3) Trail remaining position once activated.
		if pos.TrailOn {
			if (sideBuy && mark > pos.TrailRef) || (!sideBuy && mark < pos.TrailRef) {
				pos.TrailRef = mark
				pos.TrailStop = p.calcTrailStop(sideBuy, mark)
			}
			if (sideBuy && mark <= pos.TrailStop) || (!sideBuy && mark >= pos.TrailStop) {
				p.exitPortion(now, pos, "TRAIL_STOP", pos.TrailStop, pos.Qty)
				continue
			}
		}

		if pos.Qty <= 1e-10 {
			delete(p.positions, raw)
		}
	}
}

func (p *paperTrader) exitPortion(now time.Time, pos *paperPosition, reason string, exitPrice, qty float64) {
	if p == nil || !p.enabled || pos == nil {
		return
	}
	symbol := strings.ToUpper(aster.RawSymbol(pos.Symbol))
	if qty <= 0 || pos.Qty <= 0 {
		return
	}
	if qty > pos.Qty {
		qty = pos.Qty
	}
	gross := 0.0
	if strings.EqualFold(pos.Side, "BUY") {
		gross = (exitPrice - pos.Entry) * qty
	} else {
		gross = (pos.Entry - exitPrice) * qty
	}
	notional := exitPrice * qty
	fee := notional * p.feeBps / 10000.0
	net := gross - fee
	pos.Realized += net
	p.balance += net
	p.recordDayStat(now, reason, gross, fee, net)
	pos.Qty -= qty
	if pos.Qty < 0 {
		pos.Qty = 0
	}
	holdMin := now.Sub(pos.OpenedAt).Minutes()
	fmt.Printf("paper exit %s %s reason=%s qty=%.6f entry=%.6f exit=%.6f pnl=%+.4f realized=%+.4f rem=%.6f balance=%.2f hold=%.1fm\n",
		symbol, pos.Side, reason, qty, pos.Entry, exitPrice, net, pos.Realized, pos.Qty, p.balance, holdMin)
	_ = p.logTrade(now, symbol, pos.Side, pos.Entry, exitPrice, qty, pos.Leverage, pos.Margin, pos.Stop, exitPrice, reason, gross, fee, net, holdMin)
	if pos.Qty <= 1e-10 {
		delete(p.positions, symbol)
	}
}

func (p *paperTrader) recordDayStat(now time.Time, reason string, gross, fee, net float64) {
	if p == nil {
		return
	}
	loc := p.reportLoc
	if loc == nil {
		loc = time.Local
	}
	dayKey := now.In(loc).Format("2006-01-02")
	ds := p.dayStats[dayKey]
	if ds == nil {
		ds = &paperDayStats{Reasons: map[string]int{}}
		p.dayStats[dayKey] = ds
	}
	ds.Trades++
	if net >= 0 {
		ds.Wins++
	} else {
		ds.Losses++
	}
	ds.Gross += gross
	ds.Fees += fee
	ds.Net += net
	ds.Reasons[strings.ToUpper(strings.TrimSpace(reason))]++
}

func (p *paperTrader) TradeUpdateMessage(meta map[string]symbolMeta, topN int) string {
	if p == nil || !p.enabled {
		return ""
	}
	if topN <= 0 {
		topN = 5
	}
	type row struct {
		sym   string
		side  string
		entry float64
		mark  float64
		qty   float64
		upnl  float64
	}
	rows := make([]row, 0, len(p.positions))
	totalUPnL := 0.0
	for sym, pos := range p.positions {
		mark := meta[sym].LastPrice
		upnl := 0.0
		if strings.EqualFold(pos.Side, "BUY") {
			upnl = (mark - pos.Entry) * pos.Qty
		} else {
			upnl = (pos.Entry - mark) * pos.Qty
		}
		totalUPnL += upnl
		rows = append(rows, row{sym: sym, side: pos.Side, entry: pos.Entry, mark: mark, qty: pos.Qty, upnl: upnl})
	}
	sort.Slice(rows, func(i, j int) bool { return abs(rows[i].upnl) > abs(rows[j].upnl) })
	if len(rows) > topN {
		rows = rows[:topN]
	}
	eq := p.balance + totalUPnL
	var b strings.Builder
	fmt.Fprintf(&b, "Paper Update (%s)\n", time.Now().In(p.reportLoc).Format("15:04 MST"))
	fmt.Fprintf(&b, "bal=%.2f eq=%.2f openPnL=%+.2f open=%d/%d\n", p.balance, eq, totalUPnL, len(p.positions), p.maxOpen)
	if len(rows) == 0 {
		b.WriteString("open: none")
		return b.String()
	}
	for i, r := range rows {
		fmt.Fprintf(&b, "%d) %s %s e=%.6f m=%.6f q=%.4f upnl=%+.3f\n",
			i+1, r.sym, r.side, r.entry, r.mark, r.qty, r.upnl)
	}
	return strings.TrimSpace(b.String())
}

func (p *paperTrader) DailyReportMessage(dayKey string) (string, bool) {
	if p == nil || !p.enabled {
		return "", false
	}
	ds := p.dayStats[dayKey]
	if ds == nil || ds.Trades == 0 {
		return fmt.Sprintf("Paper Daily Report %s (%s)\nno trades", dayKey, p.reportLoc.String()), true
	}
	winRate := 0.0
	if ds.Trades > 0 {
		winRate = (float64(ds.Wins) / float64(ds.Trades)) * 100.0
	}
	var reasons []string
	for k, v := range ds.Reasons {
		reasons = append(reasons, fmt.Sprintf("%s=%d", k, v))
	}
	sort.Strings(reasons)
	return fmt.Sprintf(
		"Paper Daily Report %s (%s)\ntrades=%d wins=%d losses=%d winRate=%.1f%%\ngross=%+.2f fees=%.2f net=%+.2f\nreasons: %s",
		dayKey, p.reportLoc.String(), ds.Trades, ds.Wins, ds.Losses, winRate, ds.Gross, ds.Fees, ds.Net, strings.Join(reasons, ", "),
	), true
}

func (p *paperTrader) hitPrice(sideBuy bool, mark, level float64) bool {
	if level <= 0 || mark <= 0 {
		return false
	}
	if sideBuy {
		return mark >= level
	}
	return mark <= level
}

func (p *paperTrader) targetQty(initialQty, frac, rem float64) float64 {
	if initialQty <= 0 || rem <= 0 {
		return 0
	}
	if frac <= 0 {
		return rem
	}
	q := initialQty * frac
	if q > rem {
		q = rem
	}
	if q <= 0 {
		return rem
	}
	return q
}

func (p *paperTrader) calcTrailStop(sideBuy bool, ref float64) float64 {
	if ref <= 0 {
		return 0
	}
	pct := p.trailStopPct / 100.0
	if pct <= 0 {
		pct = 0.01
	}
	if sideBuy {
		return ref * (1 - pct)
	}
	return ref * (1 + pct)
}

func (p *paperTrader) logTrade(now time.Time, symbol, side string, entry, exit, qty float64, lev int, margin, stop, tp float64, reason string, gross, fee, net, holdMin float64) error {
	if p == nil || !p.enabled {
		return nil
	}
	if err := ensureCSVWithHeader(p.tradesCSV, []string{
		"exit_ts", "symbol", "side", "entry", "exit", "qty", "lev", "margin", "stop", "tp", "reason", "gross_pnl", "fees", "net_pnl", "balance", "hold_min",
	}); err != nil {
		return err
	}
	f, err := os.OpenFile(p.tradesCSV, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	row := []string{
		now.UTC().Format(time.RFC3339),
		symbol,
		side,
		fmt.Sprintf("%.8f", entry),
		fmt.Sprintf("%.8f", exit),
		fmt.Sprintf("%.8f", qty),
		strconv.Itoa(lev),
		fmt.Sprintf("%.2f", margin),
		fmt.Sprintf("%.8f", stop),
		fmt.Sprintf("%.8f", tp),
		reason,
		fmt.Sprintf("%.8f", gross),
		fmt.Sprintf("%.8f", fee),
		fmt.Sprintf("%.8f", net),
		fmt.Sprintf("%.8f", p.balance),
		fmt.Sprintf("%.2f", holdMin),
	}
	if err := w.Write(row); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func (p *paperTrader) LogEquity(now time.Time, meta map[string]symbolMeta) error {
	if p == nil || !p.enabled {
		return nil
	}
	if err := ensureCSVWithHeader(p.equityCSV, []string{
		"ts", "balance", "equity", "open_symbol", "open_side", "open_entry", "open_qty", "open_mark", "open_pnl",
	}); err != nil {
		return err
	}
	openSym, openSide := "", ""
	openEntry, openQty, openMark := 0.0, 0.0, 0.0
	totalOpenPnL := 0.0
	if len(p.positions) > 0 {
		for sym, pos := range p.positions {
			mark := meta[sym].LastPrice
			pnl := 0.0
			if strings.EqualFold(pos.Side, "BUY") {
				pnl = (mark - pos.Entry) * pos.Qty
			} else {
				pnl = (pos.Entry - mark) * pos.Qty
			}
			totalOpenPnL += pnl
			if openSym == "" {
				openSym = sym
				openSide = pos.Side
				openEntry = pos.Entry
				openQty = pos.Qty
				openMark = mark
			}
		}
	}
	eq := p.balance + totalOpenPnL
	f, err := os.OpenFile(p.equityCSV, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	row := []string{
		now.UTC().Format(time.RFC3339),
		fmt.Sprintf("%.8f", p.balance),
		fmt.Sprintf("%.8f", eq),
		openSym,
		openSide,
		fmt.Sprintf("%.8f", openEntry),
		fmt.Sprintf("%.8f", openQty),
		fmt.Sprintf("%.8f", openMark),
		fmt.Sprintf("%.8f", totalOpenPnL),
	}
	if err := w.Write(row); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func ensureCSVWithHeader(path string, header []string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func buildEligible(c *aster.Client, rows []market.Scored, side string, gradeTopN int) ([]market.Scored, map[string]string) {
	out := make([]market.Scored, 0, len(rows))
	conf := make(map[string]string, len(rows))
	eligibleByScore := make([]market.Scored, 0, len(rows))
	for _, r := range rows {
		if !r.Eligible {
			continue
		}
		out = append(out, r)
		eligibleByScore = append(eligibleByScore, r)
		conf[r.Symbol] = market.FallbackGradeDirectional(r.Score, r.Change24h, side)
	}
	sort.Slice(eligibleByScore, func(i, j int) bool { return eligibleByScore[i].Score > eligibleByScore[j].Score })
	for i := 0; i < len(eligibleByScore) && i < gradeTopN; i++ {
		sym := eligibleByScore[i].Symbol
		lbl := confluenceLabel(c, sym, side)
		if lbl == "" || lbl == "_" || lbl == "C" {
			lbl = market.FallbackGradeDirectional(eligibleByScore[i].Score, eligibleByScore[i].Change24h, side)
		}
		conf[sym] = lbl
	}
	return out, conf
}

func chooseCandidates(longInPlay, shortInPlay []inplay.Entry, minGrade string) []candidate {
	minVal := gradeValue(minGrade)
	out := make([]candidate, 0, len(longInPlay)+len(shortInPlay))
	for _, e := range longInPlay {
		if e.State == inplay.StateInPlay && gradeValue(e.CurrentGrade) >= minVal && e.ScoreSlope > 0 {
			out = append(out, candidate{Entry: e, Side: "BUY"})
		}
	}
	for _, e := range shortInPlay {
		if e.State == inplay.StateInPlay && gradeValue(e.CurrentGrade) >= minVal && e.ScoreSlope > 0 {
			out = append(out, candidate{Entry: e, Side: "SELL"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entry.Rank > out[j].Entry.Rank })
	return out
}

func rankWithStrategy(c *aster.Client, in []candidate, topN int) []candidate {
	if len(in) == 0 {
		return in
	}
	out := make([]candidate, len(in))
	copy(out, in)
	if topN > len(out) {
		topN = len(out)
	}
	for i := 0; i < topN; i++ {
		out[i] = enrichCandidate(c, out[i])
	}
	sort.Slice(out, func(i, j int) bool {
		ri := out[i].Entry.Rank * (1 + out[i].Conf)
		rj := out[j].Entry.Rank * (1 + out[j].Conf)
		return ri > rj
	})
	return out
}

func enrichCandidate(c *aster.Client, cand candidate) candidate {
	raw := strings.ToUpper(aster.RawSymbol(cand.Entry.Symbol))
	bars, err := c.LoadCandles(raw, types.TF1m, 240)
	if err != nil || len(bars) < 30 {
		cand.Strat = "none"
		return cand
	}
	fc := make([]features.Candle, 0, len(bars))
	for _, b := range bars {
		fc = append(fc, features.Candle{Ts: b.T, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V})
	}
	fe := features.NewEngine(features.Config{})
	snap := fe.Eval(fc)
	rt := strategies.NewRouter(strategies.RouterConfig{
		MinGrade:       "B",
		MinScore:       0,
		MinWhaleDelta:  -1e18,
		AllowWarmup:    true,
		WarmupSlopeMin: 0,
		MaxOne:         true,
	})
	ctx := strategies.Context{
		Symbol:       raw,
		TF:           "1m",
		ScannerScore: cand.Entry.CurrentScore,
		ScannerGrade: cand.Entry.CurrentGrade,
		ScoreSlope:   cand.Entry.ScoreSlope,
		Snapshot:     snap,
		Candles:      fc,
	}
	cs := rt.Eval(ctx)
	if len(cs) == 0 {
		cand.Strat = "none"
		cand.Conf = 0
		return cand
	}
	cand.Sig = cs[0].Signal
	cand.Strat = cs[0].Signal.Name
	cand.Conf = cs[0].Signal.Confidence
	return cand
}

func printInPlay(tag string, entries []inplay.Entry) {
	fmt.Printf("🔥 IN-PLAY (%s)\n", tag)
	for i := 0; i < len(entries) && i < 5; i++ {
		e := entries[i]
		fmt.Printf("%d) %-12s grade=%-2s score=%6.2f slope=%6.3f state=%-8s\n",
			i+1, e.Symbol, e.CurrentGrade, e.CurrentScore, e.ScoreSlope, e.State)
	}
	if len(entries) == 0 {
		fmt.Println("(none)")
	}
}

func printTradeIntent(c candidate, entryBps, margin float64, lev int) {
	sym := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	if lev <= 0 {
		lev = 1
	}
	fmt.Printf("DRY_RUN intent: symbol=%s side=%s margin=$%.2f leverage=%dx entry=LIMIT(mid %+0.2fbps in direction)\n",
		sym, c.Side, margin, lev, entryBps)
}

func fetchAccountSnapshot(rest *aster.RESTAuth, assets []string) (accountSnapshot, error) {
	snap := accountSnapshot{}
	bals, err := rest.GetBalance()
	if err != nil {
		return snap, err
	}
	snap.Balances = filterBalances(bals, assets)
	for _, b := range bals {
		if strings.EqualFold(strings.TrimSpace(b.Asset), "USDT") {
			snap.AvailableUSDT = b.AvailableBalance
			break
		}
	}
	rows, err := rest.PositionRisk("")
	if err != nil {
		return snap, err
	}
	for _, r := range rows {
		amt := mapFloat(r["positionAmt"])
		if amt == 0 {
			continue
		}
		side := "LONG"
		if amt < 0 {
			side = "SHORT"
		}
		snap.Positions = append(snap.Positions, positionView{
			Symbol:   strings.ToUpper(strings.TrimSpace(fmt.Sprint(r["symbol"]))),
			Side:     side,
			SizeAbs:  abs(amt),
			Entry:    mapFloat(r["entryPrice"]),
			Mark:     mapFloat(r["markPrice"]),
			Unreal:   mapFloat(r["unRealizedProfit"]),
			Leverage: mapFloat(r["leverage"]),
		})
	}
	sort.Slice(snap.Positions, func(i, j int) bool {
		return snap.Positions[i].Unreal > snap.Positions[j].Unreal
	})
	return snap, nil
}

func printAccountSnapshot(snap accountSnapshot, assets []string) {
	fmt.Printf("💼 ACCOUNT availableUSDT=%.4f\n", snap.AvailableUSDT)
	if len(snap.Balances) == 0 {
		fmt.Println("balances: (none matching filter)")
	} else {
		fmt.Println("balances:")
		for _, b := range snap.Balances {
			fmt.Printf("- %-6s bal=%-12.6f avail=%-12.6f upnl=%-10.6f\n",
				strings.ToUpper(b.Asset), b.Balance, b.AvailableBalance, b.CrossUnPnl)
		}
	}
	fmt.Println("active positions:")
	if len(snap.Positions) == 0 {
		fmt.Println("(none)")
		return
	}
	for _, p := range snap.Positions {
		fmt.Printf("- %-10s %-5s qty=%-10.4f entry=%-10.4f mark=%-10.4f upnl=%-10.4f lev=%g\n",
			p.Symbol, p.Side, p.SizeAbs, p.Entry, p.Mark, p.Unreal, p.Leverage)
	}
}

func filterBalances(rows []aster.Balance, assets []string) []aster.Balance {
	if len(assets) == 0 {
		out := make([]aster.Balance, 0, len(rows))
		for _, b := range rows {
			if b.Balance != 0 || b.AvailableBalance != 0 || b.CrossUnPnl != 0 {
				out = append(out, b)
			}
		}
		return out
	}
	want := make(map[string]struct{}, len(assets))
	for _, a := range assets {
		if s := strings.ToUpper(strings.TrimSpace(a)); s != "" {
			want[s] = struct{}{}
		}
	}
	out := make([]aster.Balance, 0, len(rows))
	for _, b := range rows {
		if _, ok := want[strings.ToUpper(strings.TrimSpace(b.Asset))]; ok {
			out = append(out, b)
		}
	}
	return out
}

func availableUSDT(rest *aster.RESTAuth) (float64, error) {
	bals, err := rest.GetBalance()
	if err != nil {
		return 0, err
	}
	for _, b := range bals {
		if strings.EqualFold(strings.TrimSpace(b.Asset), "USDT") {
			return b.AvailableBalance, nil
		}
	}
	return 0, nil
}

func countOpenPositions(rest *aster.RESTAuth) (int, error) {
	rows, err := rest.PositionRisk("")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		amt := mapFloat(r["positionAmt"])
		if amt != 0 {
			n++
		}
	}
	return n, nil
}

func placeEntry(rest *aster.RESTAuth, c candidate, entryBps, margin float64, lev int) error {
	rawSym := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	bid, ask, err := rest.BookTicker(rawSym)
	if err != nil {
		return err
	}
	mid := (bid + ask) / 2
	if mid <= 0 {
		return fmt.Errorf("invalid mid price")
	}
	price := mid
	if c.Side == "BUY" {
		price = mid * (1 - entryBps/10000.0)
	} else {
		price = mid * (1 + entryBps/10000.0)
	}
	meta, err := rest.SymbolMeta(rawSym, false)
	if err != nil {
		return err
	}
	price, _, err = rest.RoundPrice(rawSym, price)
	if err != nil {
		return err
	}
	qty := margin / price
	qty, _, err = rest.RoundQty(rawSym, qty)
	if err != nil {
		return err
	}
	if qty <= 0 {
		return fmt.Errorf("qty <= 0 after rounding")
	}

	if lev <= 0 {
		lev = 1
	}
	_, _ = rest.ChangeLeverage(rawSym, lev)

	vals := url.Values{}
	vals.Set("symbol", rawSym)
	vals.Set("side", c.Side)
	vals.Set("type", "LIMIT")
	vals.Set("timeInForce", "GTC")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "false")
	vals.Set("quantity", formatFloat(qty, meta.QtyPrecision))
	vals.Set("price", formatFloat(price, meta.PricePrecision))

	out, err := rest.PlaceOrder(vals)
	if err != nil {
		return err
	}
	fmt.Printf("live-lite: placed entry %s %s qty=%s price=%s -> %v\n",
		rawSym, c.Side, vals.Get("quantity"), vals.Get("price"), out)
	return nil
}

func loadSafetyConfig(reserveUSDT, tradeMargin float64) safetyConfig {
	minAvail := envFloat("LIVE_MIN_AVAILABLE_USDT", reserveUSDT+tradeMargin)
	maxLev := envInt("LIVE_MAX_LEVERAGE", 3)
	if maxLev <= 0 {
		maxLev = 3
	}
	maxOrders := envInt("LIVE_MAX_ORDERS_PER_DAY", 3)
	if maxOrders < 0 {
		maxOrders = 0
	}
	coolSec := envInt("LIVE_ORDER_COOLDOWN_SEC", 90)
	if coolSec < 0 {
		coolSec = 0
	}
	allow := envCSV("LIVE_ALLOW_SYMBOLS", "")
	allowMap := make(map[string]struct{}, len(allow))
	for _, s := range allow {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(s)))
		if raw != "" {
			allowMap[raw] = struct{}{}
		}
	}
	return safetyConfig{
		enableLiveTrading: envBool("LIVE_ENABLE_LIVE_TRADING", false),
		maxLeverage:       maxLev,
		minAvailUSDT:      minAvail,
		maxOrdersPerDay:   maxOrders,
		orderCooldown:     time.Duration(coolSec) * time.Second,
		pauseFile:         envStr("LIVE_PAUSE_FILE", "/tmp/live-lite.pause"),
		allowSymbols:      allowMap,
		allowShorts:       envBool("LIVE_ALLOW_SHORTS", true),
	}
}

func safetyReject(cfg safetyConfig, c candidate, now, lastOrderAt time.Time, byDay map[string]int) string {
	if cfg.pauseFile != "" {
		if _, err := os.Stat(cfg.pauseFile); err == nil {
			return "pause file present"
		}
	}
	if !cfg.allowShorts && strings.EqualFold(c.Side, "SELL") {
		return "shorts disabled"
	}
	if len(cfg.allowSymbols) > 0 {
		sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
		if _, ok := cfg.allowSymbols[sym]; !ok {
			return "symbol not in allowlist"
		}
	}
	if cfg.orderCooldown > 0 && !lastOrderAt.IsZero() && now.Sub(lastOrderAt) < cfg.orderCooldown {
		return "order cooldown active"
	}
	if cfg.maxOrdersPerDay > 0 {
		dayKey := now.UTC().Format("2006-01-02")
		if byDay[dayKey] >= cfg.maxOrdersPerDay {
			return "max orders/day reached"
		}
	}
	return ""
}

func buildRESTFromConfig() *aster.RESTAuth {
	fileKV := getConfigKV()

	key := cfgGet(fileKV, "ASTER_API_KEY", "aster_api_key", "api_key", "key")
	sec := cfgGet(fileKV, "ASTER_API_SECRET", "aster_api_secret", "api_secret", "secret")
	if key == "" || sec == "" {
		return nil
	}
	baseURL := cfgGet(fileKV, "EXEC_BASE_URL", "aster_base_url", "base_url")
	rest := aster.NewRESTAuthWithConfig(aster.RESTAuthConfig{
		APIKey:    key,
		APISecret: sec,
		AuthMode:  "hmac",
		BaseURL:   baseURL,
	})
	_ = rest.SyncTime()
	return rest
}

func loadSimpleYAMLKV(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(parts[0]))
		v := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		if k != "" && v != "" {
			out[k] = v
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func getConfigKV() map[string]string {
	cfgPath := strings.TrimSpace(os.Getenv("ASTER_CONFIG"))
	if cfgPath == "" {
		if _, err := os.Stat(".aster.yaml"); err == nil {
			cfgPath = ".aster.yaml"
		} else if home, err := os.UserHomeDir(); err == nil && home != "" {
			cfgPath = filepath.Join(home, ".aster.yaml")
		}
	}
	if cfgPath == "" {
		return map[string]string{}
	}
	kv, err := loadSimpleYAMLKV(cfgPath)
	if err != nil {
		return map[string]string{}
	}
	return kv
}

func cfgGet(fileKV map[string]string, envName string, keys ...string) string {
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		return v
	}
	for _, k := range keys {
		if v := strings.TrimSpace(fileKV[strings.ToLower(strings.TrimSpace(k))]); v != "" {
			return v
		}
	}
	return ""
}

func mapFloat(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	default:
		f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f
	}
}

func formatFloat(v float64, prec int) string {
	if prec < 0 {
		prec = 0
	}
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

func computeTradeMargin(mode string, fixed, pct float64, slots int, minV, maxV, reserve, avail float64, p *paperTrader) float64 {
	if fixed <= 0 {
		fixed = 10
	}
	if minV <= 0 {
		minV = 1
	}
	if maxV <= 0 {
		maxV = fixed
	}
	base := sizingBaseBalance(avail, p)
	if base <= 0 {
		return fixed
	}
	m := fixed
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "percent":
		if pct <= 0 {
			pct = 10
		}
		m = base * (pct / 100.0)
	case "slots":
		if slots <= 0 {
			slots = 5
		}
		tradeable := base - reserve
		if tradeable <= 0 {
			return minV
		}
		m = tradeable / float64(slots)
	default:
		m = fixed
	}

	if m < minV {
		m = minV
	}
	if m > maxV {
		m = maxV
	}
	usable := base - reserve
	if usable <= 0 {
		return minV
	}
	if m > usable {
		m = usable
	}
	// Stable 2dp for sizing/log readability.
	return float64(int(m*100)) / 100.0
}

func computeReserveUSDT(mode string, fixed, pct, avail float64, p *paperTrader) float64 {
	if fixed < 0 {
		fixed = 0
	}
	base := sizingBaseBalance(avail, p)
	if strings.ToLower(strings.TrimSpace(mode)) != "percent" {
		return fixed
	}
	if base <= 0 {
		return fixed
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 95 {
		pct = 95
	}
	r := base * (pct / 100.0)
	if r < 0 {
		r = 0
	}
	if r > base {
		r = base
	}
	return r
}

func sizingBaseBalance(avail float64, p *paperTrader) float64 {
	base := avail
	if p != nil && p.enabled && p.balance > 0 {
		base = p.balance
	}
	return base
}

func computeLeverage(c candidate, mode string, fixed, minLev, maxLev int) int {
	if minLev <= 0 {
		minLev = 1
	}
	if maxLev <= 0 {
		maxLev = 3
	}
	if minLev > maxLev {
		minLev = maxLev
	}
	if fixed <= 0 {
		fixed = minLev
	}
	clamp := func(v int) int {
		if v < minLev {
			return minLev
		}
		if v > maxLev {
			return maxLev
		}
		return v
	}

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "fixed":
		return clamp(fixed)
	case "auto":
		lev := minLev
		if strings.EqualFold(c.Entry.CurrentGrade, "A+") {
			lev += 2
		} else if strings.EqualFold(c.Entry.CurrentGrade, "A") {
			lev += 1
		}
		if c.Entry.CurrentScore >= 100 {
			lev++
		}
		if c.Entry.ScoreSlope >= 0.8 {
			lev++
		} else if c.Entry.ScoreSlope <= 0.05 {
			lev--
		}
		if c.Conf >= 0.75 {
			lev++
		}
		return clamp(lev)
	case "grade":
		if strings.EqualFold(c.Entry.CurrentGrade, "A+") {
			return clamp(5)
		}
		if strings.EqualFold(c.Entry.CurrentGrade, "A") {
			return clamp(4)
		}
		return clamp(3)
	default:
		return clamp(fixed)
	}
}

func envStr(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func envFloat(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	v = strings.ToLower(v)
	return !(v == "0" || v == "false" || v == "no" || v == "off")
}

func envCSV(k, def string) []string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		v = def
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func newTelegramSink() *telegramSink {
	fileKV := getConfigKV()
	token := cfgGet(fileKV, "LIVE_TG_BOT_TOKEN", "live_tg_bot_token")
	if token == "" {
		token = cfgGet(fileKV, "TELEGRAM_BOT_TOKEN", "telegram_bot_token", "tg_bot_token")
	}
	chatID := cfgGet(fileKV, "LIVE_TG_CHAT_ID", "live_tg_chat_id")
	if chatID == "" {
		chatID = cfgGet(fileKV, "TELEGRAM_CHAT_ID", "telegram_chat_id", "tg_chat_id")
	}
	timeoutSec := envInt("LIVE_TG_TIMEOUT_SEC", 5)
	if timeoutSec <= 0 {
		timeoutSec = 5
	}
	dedupeSec := envInt("LIVE_TG_DEDUPE_SEC", 30)
	if dedupeSec < 0 {
		dedupeSec = 0
	}
	return &telegramSink{
		enabled:  token != "" && chatID != "",
		token:    token,
		chatID:   chatID,
		timeout:  time.Duration(timeoutSec) * time.Second,
		dedupe:   time.Duration(dedupeSec) * time.Second,
		client:   &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
		lastSent: map[string]time.Time{},
	}
}

func (t *telegramSink) Sendf(format string, args ...any) {
	if t == nil || !t.enabled {
		return
	}
	msg := strings.TrimSpace(fmt.Sprintf(format, args...))
	if msg == "" {
		return
	}
	now := time.Now()
	if t.dedupe > 0 {
		if at, ok := t.lastSent[msg]; ok && now.Sub(at) < t.dedupe {
			return
		}
		t.lastSent[msg] = now
	}
	_ = t.send(msg)
}

func (t *telegramSink) send(msg string) error {
	form := url.Values{}
	form.Set("chat_id", t.chatID)
	form.Set("text", msg)
	form.Set("disable_web_page_preview", "true")
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram http %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func gradeValue(g string) int {
	switch strings.ToUpper(strings.TrimSpace(g)) {
	case "A+":
		return 5
	case "A":
		return 4
	case "B":
		return 3
	case "C":
		return 2
	case "D":
		return 1
	default:
		return 0
	}
}

func calcVWAP(bars []types.Candle) float64 {
	var pv, v float64
	for _, b := range bars {
		tp := (b.H + b.L + b.C) / 3.0
		pv += tp * b.V
		v += b.V
	}
	if v == 0 {
		return 0
	}
	return pv / v
}

func symbolCandidates(ui string) []string {
	base := strings.ReplaceAll(ui, "-", "")
	cands := []string{}
	if strings.HasSuffix(base, "USD") {
		cands = append(cands, base+"T")
		cands = append(cands, base)
	}
	cands = append(cands, ui)
	return cands
}

func confluenceLabel(c *aster.Client, symbol, side string) string {
	const (
		tf     = types.TF5m
		n      = 200
		win    = 20
		zmin   = 2.0
		vmin   = 1_000_000.0
		levels = 50
	)

	var useSym string
	var bars []types.Candle
	for _, cand := range symbolCandidates(symbol) {
		b, err := c.LoadCandles(cand, tf, n)
		if err == nil && len(b) > 0 {
			useSym, bars = cand, b
			break
		}
	}
	if len(bars) == 0 {
		return "C"
	}

	vwap := calcVWAP(bars)
	tr := ta.TrendMetrics(useSym, tf, bars, vwap)
	ef := ta.ComputeEffort(useSym, tf, bars, win, zmin, vmin)
	obRaw, err := c.FetchOrderBook(useSym, levels)
	if err != nil {
		return "C"
	}
	ob := ta.OrderBookContext(useSym, obRaw.Bids, obRaw.Asks, levels)
	conf := ta.ComputeConfluence(tr, ef, ob, side)
	return conf.Label
}
