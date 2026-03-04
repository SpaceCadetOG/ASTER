package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/data"
	"go-machine/internal/features"
	"go-machine/internal/inplay"
	"go-machine/internal/market"
	"go-machine/internal/strategies"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

type candidate struct {
	Entry        inplay.Entry
	Side         string // BUY/SELL
	Strat        string
	Conf         float64
	Sig          strategies.Signal
	RejectReason string
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
	symbolCooldown    time.Duration
	pauseFile         string
	allowSymbols      map[string]struct{}
	allowShorts       bool
	maxDailyLossPct   float64
	killClose         bool
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
	Symbol        string
	Side          string
	Entry         float64
	Qty           float64 // remaining qty
	InitialQty    float64
	Margin        float64
	Leverage      int
	Stop          float64
	TP1           float64
	TP2           float64
	TP3           float64
	HitTP1        bool
	HitTP2        bool
	HitTP3        bool
	TrailOn       bool
	TrailStop     float64
	TrailRef      float64
	Realized      float64
	OpenedAt      time.Time
	MaxFavorableR float64
	LastMark      float64
	EntryReason   string
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
	stateFile    string
	tradesCSV    string
	equityCSV    string
	maxOpen      int
	positions    map[string]*paperPosition
	reportLoc    *time.Location
	dayStats     map[string]*paperDayStats
	minStopPct   float64
	maxStopPct   float64
	minTP1RR     float64
	staleEnable  bool
	staleMaxAge  time.Duration
	staleMinProg float64
	staleGraceR  float64
	beLockBps    float64
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

type paperState struct {
	StartBal  float64                   `json:"startBal"`
	Balance   float64                   `json:"balance"`
	Reserve   float64                   `json:"reserve"`
	Positions map[string]*paperPosition `json:"positions"`
	DayStats  map[string]*paperDayStats `json:"dayStats"`
	UpdatedAt time.Time                 `json:"updatedAt"`
}

type execState string

const (
	execPendingEntry execState = "PENDING_ENTRY"
	execOpen         execState = "OPEN"
	execPartialTP1   execState = "PARTIAL_TP1"
	execPartialTP2   execState = "PARTIAL_TP2"
	execClosed       execState = "CLOSED"
)

type livePosition struct {
	Symbol        string    `json:"symbol"`
	Side          string    `json:"side"`
	State         execState `json:"state"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	ClosedAt      time.Time `json:"closedAt,omitempty"`
	CloseReason   string    `json:"closeReason,omitempty"`
	EntryOrderID  int64     `json:"entryOrderId"`
	EntryPrice    float64   `json:"entryPrice"`
	Qty           float64   `json:"qty"`
	FilledQty     float64   `json:"filledQty"`
	RemainingQty  float64   `json:"remainingQty"`
	Margin        float64   `json:"margin"`
	Leverage      int       `json:"leverage"`
	StopPrice     float64   `json:"stopPrice"`
	TP1Price      float64   `json:"tp1Price"`
	TP2Price      float64   `json:"tp2Price"`
	TP3Price      float64   `json:"tp3Price"`
	TP1Qty        float64   `json:"tp1Qty"`
	TP2Qty        float64   `json:"tp2Qty"`
	TP3Qty        float64   `json:"tp3Qty"`
	StopOrderID   int64     `json:"stopOrderId"`
	TP1OrderID    int64     `json:"tp1OrderId"`
	TP2OrderID    int64     `json:"tp2OrderId"`
	TP3OrderID    int64     `json:"tp3OrderId"`
	TrailOn       bool      `json:"trailOn"`
	TrailRef      float64   `json:"trailRef"`
	TrailStop     float64   `json:"trailStop"`
	VPSetup       string    `json:"vpSetup,omitempty"`
	VPLevel       float64   `json:"vpLevel,omitempty"`
	VPTargetLevel float64   `json:"vpTargetLevel,omitempty"`
	VPStopMode    string    `json:"vpStopMode,omitempty"`
	VPTargetMode  string    `json:"vpTargetMode,omitempty"`
	RejectReason  string    `json:"rejectReason,omitempty"`
	CustomRiskPct float64   `json:"customRiskPct,omitempty"`
	CustomTP1R    float64   `json:"customTp1R,omitempty"`
	CustomTP2R    float64   `json:"customTp2R,omitempty"`
	EntryReason   string    `json:"entryReason,omitempty"`
	EntryConf     float64   `json:"entryConf,omitempty"`
	EntryTags     []string  `json:"entryTags,omitempty"`
	EntryReasons  []string  `json:"entryReasons,omitempty"`
	RegimeTag     string    `json:"regimeTag,omitempty"`
	MaxFavorableR float64   `json:"maxFavorableR,omitempty"`
	LastMark      float64   `json:"lastMark,omitempty"`
	RealizedPnL   float64   `json:"realizedPnl,omitempty"`
}

type liveExecStore struct {
	Positions map[string]*livePosition `json:"positions"`
}

type liveExecManager struct {
	rest            *aster.RESTAuth
	tg              *telegramSink
	path            string
	entryTimeout    time.Duration
	stopPct         float64
	tp1R            float64
	tp2R            float64
	tp3R            float64
	tp1Frac         float64
	tp2Frac         float64
	tp3Frac         float64
	trailAfterTP    int
	trailStopPct    float64
	trailStepBps    float64
	minStopPct      float64
	maxStopPct      float64
	minTP1RR        float64
	beLockBps       float64
	staleEnable     bool
	staleMaxAge     time.Duration
	staleMinProg    float64
	staleGraceR     float64
	marginType      string
	enforceIsolated bool
	positions       map[string]*livePosition
	dayRealized     map[string]float64
	reportLoc       *time.Location
}

type liveExecSnapshot struct {
	Generated time.Time      `json:"generated"`
	Total     int            `json:"total"`
	Pending   int            `json:"pending"`
	Open      int            `json:"open"`
	Partial1  int            `json:"partial_tp1"`
	Partial2  int            `json:"partial_tp2"`
	Closed    int            `json:"closed"`
	Active    []livePosition `json:"active"`
}

type liveLiteStatus struct {
	Generated       time.Time        `json:"generated"`
	DryRun          bool             `json:"dry_run"`
	LiveEnabled     bool             `json:"live_enabled"`
	TopSymbol       string           `json:"top_symbol,omitempty"`
	TopSide         string           `json:"top_side,omitempty"`
	TopGrade        string           `json:"top_grade,omitempty"`
	TopScore        float64          `json:"top_score,omitempty"`
	TopSlope        float64          `json:"top_slope,omitempty"`
	TopVPSetup      string           `json:"top_vp_setup,omitempty"`
	TopVPLevel      float64          `json:"top_vp_level,omitempty"`
	TopVPTarget     float64          `json:"top_vp_target_level,omitempty"`
	TopVPStopMode   string           `json:"top_vp_stop_mode,omitempty"`
	TopVPTargetMode string           `json:"top_vp_target_mode,omitempty"`
	TopRejectReason string           `json:"top_reject_reason,omitempty"`
	TopRegimeTag    string           `json:"top_regime_tag,omitempty"`
	LongInPlay      int              `json:"long_inplay"`
	ShortInPlay     int              `json:"short_inplay"`
	AvailableUSDT   float64          `json:"available_usdt"`
	PaperSummary    string           `json:"paper_summary,omitempty"`
	Exec            liveExecSnapshot `json:"exec"`
}

type liveLiteStatusStore struct {
	mu  sync.RWMutex
	cur liveLiteStatus
}

type maintenanceWindow struct {
	Name      string
	StartHour int
	EndHour   int
	ForceFlat bool
	HookPath  string
	HookTO    time.Duration
}

type maintenanceState struct {
	LastStartDay map[string]string
	LastEndDay   map[string]string
	FlatDoneDay  map[string]string
	HookDoneDay  map[string]string
}

func main() {
	scanEvery := time.Duration(envInt("LIVE_SCAN_SEC", 30)) * time.Second
	dryRun := envBool("LIVE_DRY_RUN", true)
	minGrade := envStr("LIVE_MIN_GRADE", "B")
	reserveUSDT := envFloat("LIVE_RESERVE_USDT", 5)
	reserveMode := strings.ToLower(envStr("LIVE_RESERVE_MODE", "fixed")) // fixed|percent
	reservePct := envFloat("LIVE_RESERVE_PCT", 50.0)
	tradeMargin := envFloat("LIVE_TRADE_MARGIN_USDT", 100)
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
	stopMode := strings.ToLower(envStr("LIVE_STOP_MODE", "hybrid"))
	targetMode := strings.ToLower(envStr("LIVE_TARGET_MODE", "hybrid"))
	vpMinTargetPct := envFloat("LIVE_VP_MIN_TARGET_PCT", 0.10)
	eventLockoutMin := envInt("LIVE_EVENT_LOCKOUT_MIN", 0)
	maxCorrelatedExposure := envFloat("LIVE_MAX_CORRELATED_USD_EXPOSURE", 0)
	corrGroups := parseCorrGroups(envStr("LIVE_CORR_GROUPS", ""))
	enableMomentumReversal := envBool("LIVE_ENABLE_MOMENTUM_REVERSAL", true)
	reversalMinGrade := envStr("LIVE_REVERSAL_MIN_GRADE", "A+")
	reversalSlopeMin := envFloat("LIVE_REVERSAL_SLOPE_MIN", 0.15)
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
	cfgPath := resolveConfigPath()
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
	execMgr := newLiveExecManager(rest, tg)
	if execMgr != nil {
		if nClosed, nImported, err := execMgr.ReconcileBootState(); err != nil {
			fmt.Println("live-lite: boot reconcile warning:", err)
		} else if nClosed > 0 || nImported > 0 {
			tg.Sendf("boot reconcile complete\nclosed_local=%d imported_remote=%d", nClosed, nImported)
		}
	}
	statusStore := newLiveLiteStatusStore()
	statusAddr := envStr("LIVE_STATUS_ADDR", ":8787")
	startLiveLiteStatusServer(statusAddr, statusStore)
	tgVerbose := envBool("LIVE_TG_VERBOSE", false)
	digestEvery := time.Duration(envInt("LIVE_TG_DIGEST_MIN", 60)) * time.Minute
	if digestEvery < time.Minute {
		digestEvery = 60 * time.Minute
	}
	digestLimit := envInt("LIVE_TG_LIST_LIMIT", 12)
	if digestLimit <= 0 {
		digestLimit = 12
	}
	tradeUpdateEvery := time.Duration(envInt("LIVE_TG_TRADE_UPDATE_MIN", 60)) * time.Minute
	if tradeUpdateEvery < time.Minute {
		tradeUpdateEvery = 60 * time.Minute
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
	hourlyEnable := envBool("LIVE_TG_HOURLY_ENABLE", true)
	hourlyTZName := envStr("LIVE_TG_HOURLY_TZ", "America/Chicago")
	hourlyLoc, err := time.LoadLocation(hourlyTZName)
	if err != nil {
		hourlyLoc = time.Local
	}
	maintTZName := envStr("LIVE_MAINT_TZ", "America/Chicago")
	maintLoc, err := time.LoadLocation(maintTZName)
	if err != nil {
		maintLoc = hourlyLoc
	}
	maintEnabled := envBool("LIVE_MAINT_ENABLE", true)
	maintMidnight := maintenanceWindow{
		Name:      "M1",
		StartHour: envInt("LIVE_MAINT1_START_HOUR", 0),
		EndHour:   envInt("LIVE_MAINT1_END_HOUR", 1),
		ForceFlat: envBool("LIVE_MAINT1_FORCE_FLAT", false),
		HookPath:  envStr("LIVE_MAINT1_HOOK", ""),
		HookTO:    time.Duration(envInt("LIVE_MAINT1_HOOK_TIMEOUT_SEC", 900)) * time.Second,
	}
	maintEOD := maintenanceWindow{
		Name:      "M2",
		StartHour: envInt("LIVE_MAINT2_START_HOUR", 16),
		EndHour:   envInt("LIVE_MAINT2_END_HOUR", 17),
		ForceFlat: envBool("LIVE_MAINT2_FORCE_FLAT", true),
		HookPath:  envStr("LIVE_MAINT2_HOOK", ""),
		HookTO:    time.Duration(envInt("LIVE_MAINT2_HOOK_TIMEOUT_SEC", 900)) * time.Second,
	}
	nextDigestAt := time.Now().UTC().Add(10 * time.Second)
	nextTradeUpdateAt := time.Now().UTC().Add(45 * time.Second)
	lastDailyReportDay := ""
	lastHourlyKey := ""
	maintState := maintenanceState{
		LastStartDay: map[string]string{},
		LastEndDay:   map[string]string{},
		FlatDoneDay:  map[string]string{},
		HookDoneDay:  map[string]string{},
	}
	paper := newPaperTrader(dryRun, reserveUSDT, maxOpenPos)

	fmt.Printf("live-lite started (scan=%s dry_run=%v min_grade=%s reserve=%s fixed=%.2f margin=%s lev_mode=%s)\n",
		scanEvery, dryRun, strings.ToUpper(minGrade),
		reserveMode, reserveUSDT, fmt.Sprintf("%s(%.2f)", tradeMarginMode, tradeMargin), leverageMode)
	if cfgPath == "" {
		cfgPath = "(none)"
	}
	fmt.Printf("live-lite config: ASTER_CONFIG=%s REST_BASE_URL=%s\n", cfgPath, effectiveRESTBaseURL())
	fmt.Printf("safety: live_enabled=%v max_lev=%d min_avail=%.2f max_orders_day=%d cooldown=%s allow_shorts=%v pause_file=%s\n",
		safety.enableLiveTrading, safety.maxLeverage, safety.minAvailUSDT, safety.maxOrdersPerDay, safety.orderCooldown, safety.allowShorts, safety.pauseFile)
	tg.Sendf("live-lite started\nscan=%s dry_run=%v min_grade=%s digest=%s maint=%s/%s",
		scanEvery, dryRun, strings.ToUpper(minGrade), digestEvery,
		fmt.Sprintf("%02d-%02d", maintMidnight.StartHour, maintMidnight.EndHour),
		fmt.Sprintf("%02d-%02d", maintEOD.StartHour, maintEOD.EndHour))

	reconEvery := time.Duration(envInt("LIVE_RECON_SEC", 10)) * time.Second
	if reconEvery <= 0 {
		reconEvery = 10 * time.Second
	}
	requireShadowDays := envInt("LIVE_REQUIRE_PAPER_DAYS", 0)
	shadowEquityFile := envStr("LIVE_PAPER_EQUITY_FILE", "out/paper_equity.csv")
	shadowWarnAt := time.Time{}
	lastTopKey := ""
	lastOrderAt := time.Time{}
	lastOrderBySymbol := map[string]time.Time{}
	orderCountByDay := map[string]int{}
	dayStartEq := map[string]float64{}
	killDay := map[string]bool{}
	for {
		cycleStart := time.Now()
		now := cycleStart.UTC()
		localMaintNow := now.In(maintLoc)
		maintWindow, inMaint := activeMaintenanceWindow(localMaintNow, maintEnabled, maintMidnight, maintEOD)
		mkts := client.FetchAllMarkets()
		longRows := market.ScoreAndFilter(mkts)
		shortRows := market.ScoreAndFilterShort(mkts)
		metaBySymbol := buildSymbolMeta(longRows, shortRows)
		if paper.enabled {
			paper.CheckExit(now, metaBySymbol)
		}
		if paper.enabled && tg != nil && tg.enabled {
			if !hourlyEnable && now.After(nextTradeUpdateAt) {
				if msg := paper.TradeUpdateMessage(metaBySymbol, tradeUpdateTop); msg != "" {
					tg.Sendf("%s", msg)
				}
				nextTradeUpdateAt = now.Add(tradeUpdateEvery)
			}
			if hourlyEnable {
				hk := localMaintNow.Format("2006-01-02 15")
				if localMaintNow.Minute() == 0 && hk != lastHourlyKey {
					tg.Sendf("%s", buildHourlyDigest(localMaintNow, paper, metaBySymbol, tradeUpdateTop))
					lastHourlyKey = hk
				}
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
		if !hourlyEnable && time.Now().UTC().After(nextDigestAt) {
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
				dayKey := now.Format("2006-01-02")
				eq := accountEquity(snap)
				if eq > 0 && dayStartEq[dayKey] == 0 {
					dayStartEq[dayKey] = eq
				}
				if safety.maxDailyLossPct > 0 && dayStartEq[dayKey] > 0 {
					minEq := dayStartEq[dayKey] * (1.0 - safety.maxDailyLossPct/100.0)
					if eq <= minEq && !killDay[dayKey] {
						killDay[dayKey] = true
						if safety.pauseFile != "" {
							_ = os.WriteFile(safety.pauseFile, []byte(now.Format(time.RFC3339)+" daily loss kill-switch\n"), 0o644)
						}
						if safety.killClose && execMgr != nil {
							_ = execMgr.ForceCloseAll("DAILY_LOSS_KILL")
						}
						msg := fmt.Sprintf("KILL_SWITCH daily loss hit: eq=%.4f start=%.4f limit=%.4f", eq, dayStartEq[dayKey], minEq)
						fmt.Println("live-lite:", msg)
						tg.Sendf("%s", msg)
					}
				}
			}
		}
		if execMgr != nil {
			execMgr.Reconcile(now)
		}
		if paper.enabled {
			paper.ApplyStale(now, metaBySymbol)
		}
		if inMaint {
			dayKey := localMaintNow.Format("2006-01-02")
			if maintState.LastStartDay[maintWindow.Name] != dayKey {
				maintState.LastStartDay[maintWindow.Name] = dayKey
				tg.Sendf("maintenance start %s\nwindow=%02d:00-%02d:00 %s", maintWindow.Name, maintWindow.StartHour, maintWindow.EndHour, maintLoc.String())
				if paper.enabled {
					_ = paper.save()
				}
			}
			if maintWindow.ForceFlat && maintState.FlatDoneDay[maintWindow.Name] != dayKey {
				maintState.FlatDoneDay[maintWindow.Name] = dayKey
				if execMgr != nil {
					_ = execMgr.ForceCloseAll("EOD_FORCE_FLAT")
				}
				if paper.enabled {
					paper.ForceCloseAll(now, metaBySymbol, "EOD_FORCE_FLAT")
					_ = paper.save()
				}
				tg.Sendf("maintenance flat complete %s", maintWindow.Name)
			}
			if maintWindow.HookPath != "" && maintState.HookDoneDay[maintWindow.Name] != dayKey {
				maintState.HookDoneDay[maintWindow.Name] = dayKey
				if err := runMaintenanceHook(maintWindow.HookPath, maintWindow.HookTO); err != nil {
					tg.Sendf("maintenance hook error %s\n%v", maintWindow.Name, err)
				} else {
					tg.Sendf("maintenance hook complete %s", maintWindow.Name)
				}
			}
		} else {
			for _, w := range []maintenanceWindow{maintMidnight, maintEOD} {
				dayKey := localMaintNow.Format("2006-01-02")
				if maintState.LastStartDay[w.Name] == dayKey && maintState.LastEndDay[w.Name] != dayKey {
					maintState.LastEndDay[w.Name] = dayKey
					tg.Sendf("maintenance end %s\ntrading resumed", w.Name)
				}
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

		cands := chooseCandidates(longInPlay, shortInPlay, minGrade, enableMomentumReversal, reversalMinGrade, reversalSlopeMin)
		cands = rankWithStrategy(client, cands, strategyTopN, stopMode, targetMode, vpMinTargetPct)
		st := liveLiteStatus{
			Generated:     now,
			DryRun:        dryRun,
			LiveEnabled:   safety.enableLiveTrading,
			LongInPlay:    len(longInPlay),
			ShortInPlay:   len(shortInPlay),
			AvailableUSDT: acct.AvailableUSDT,
			Exec:          liveExecSnapshot{},
		}
		if execMgr != nil {
			st.Exec = execMgr.Snapshot(10)
		}
		if paper.enabled {
			st.PaperSummary = paper.Summary(metaBySymbol)
		}
		if len(cands) == 0 {
			statusStore.Set(st)
			fmt.Println("live-lite: no trade candidate")
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		best := cands[0]
		st.TopSymbol = strings.ToUpper(aster.RawSymbol(best.Entry.Symbol))
		st.TopSide = best.Side
		st.TopGrade = best.Entry.CurrentGrade
		st.TopScore = best.Entry.CurrentScore
		st.TopSlope = best.Entry.ScoreSlope
		st.TopVPSetup = best.Sig.VPSetup
		st.TopVPLevel = best.Sig.VPLevel
		st.TopVPTarget = best.Sig.VPTargetLevel
		st.TopVPStopMode = best.Sig.StopMode
		st.TopVPTargetMode = best.Sig.TargetMode
		st.TopRejectReason = best.RejectReason
		st.TopRegimeTag = best.Sig.RegimeTag
		statusStore.Set(st)
		effectiveLev := computeLeverage(best, leverageMode, leverageFixed, leverageMin, safety.maxLeverage)
		fmt.Printf("live-lite: top candidate %s side=%s grade=%s score=%.2f slope=%.3f rank=%.2f strat=%s conf=%.2f\n",
			best.Entry.Symbol, best.Side, best.Entry.CurrentGrade, best.Entry.CurrentScore, best.Entry.ScoreSlope, best.Entry.Rank, best.Strat, best.Conf)
		topKey := fmt.Sprintf("%s|%s|%s", best.Entry.Symbol, best.Side, best.Entry.CurrentGrade)
		if tgVerbose && topKey != lastTopKey {
			tg.Sendf("top %s %s | g=%s s=%.2f slope=%+.3f rank=%.2f | %s conf=%.2f",
				strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, best.Entry.CurrentGrade,
				best.Entry.CurrentScore, best.Entry.ScoreSlope, best.Entry.Rank, best.Strat, best.Conf)
			lastTopKey = topKey
		}

		if inMaint {
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if rest == nil || dryRun {
			printTradeIntent(best, entryBps, effectiveMargin, effectiveLev)
			if paper.enabled {
				pp, err := paper.MaybeEnter(now, best, entryBps, effectiveMargin, effectiveLev, metaBySymbol)
				if err != nil {
					fmt.Println("paper enter skip:", err)
				} else if pp != nil {
					tg.Sendf("PAPER ENTER %s %s\nmargin=$%.2f lev=%dx grade=%s reason=%s conf=%.2f\nentry=%s\ntp1=%s tp2=%s tp3=%s\nsl=%s",
						pp.Symbol, pp.Side, pp.Margin, pp.Leverage, best.Entry.CurrentGrade, best.Strat, best.Conf,
						fmtPrice(pp.Entry), fmtPrice(pp.TP1), fmtPrice(pp.TP2), fmtPrice(pp.TP3), fmtPrice(pp.Stop))
				}
			}
			if tgVerbose {
				tg.Sendf("DRY_RUN intent %s %s margin=$%.2f grade=%s score=%.2f",
					strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, effectiveMargin, best.Entry.CurrentGrade, best.Entry.CurrentScore)
			}
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if reason := safetyReject(safety, best, time.Now(), lastOrderAt, lastOrderBySymbol, orderCountByDay); reason != "" {
			fmt.Println("live-lite: safety skip:", reason)
			if tgVerbose {
				tg.Sendf("safety skip %s %s: %s", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, reason)
			}
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if inEventLockout(time.Now(), eventLockoutMin) {
			fmt.Println("live-lite: lockout skip: event window")
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if isCorrelatedExposureTooHigh(best, acct, corrGroups, maxCorrelatedExposure) {
			fmt.Println("live-lite: skip (correlated exposure gate)")
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if requireShadowDays > 0 && !shadowReady(requireShadowDays, shadowEquityFile, now) {
			if shadowWarnAt.IsZero() || now.Sub(shadowWarnAt) > 30*time.Minute {
				msg := fmt.Sprintf("shadow gate active: need %d day(s) paper history before live", requireShadowDays)
				fmt.Println("live-lite:", msg)
				tg.Sendf("%s", msg)
				shadowWarnAt = now
			}
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if execMgr != nil && execMgr.HasActiveSymbol(best.Entry.Symbol) {
			fmt.Printf("live-lite: skip (%s already active in exec state)\n", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)))
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}

		avail := acct.AvailableUSDT
		if avail <= 0 {
			var err error
			avail, err = availableUSDT(rest)
			if err != nil {
				fmt.Println("live-lite: balance error:", err)
				waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
				continue
			}
		}
		if avail < safety.minAvailUSDT {
			fmt.Printf("live-lite: safety skip (available %.4f < min required %.4f)\n", avail, safety.minAvailUSDT)
			if tgVerbose {
				tg.Sendf("safety skip %s %s: available %.4f < min %.4f",
					strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, avail, safety.minAvailUSDT)
			}
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
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
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}

		openCount := len(acct.Positions)
		if !showAccount || len(acct.Positions) == 0 {
			var err error
			openCount, err = countOpenPositions(rest)
			if err != nil {
				fmt.Println("live-lite: position check error:", err)
				waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
				continue
			}
		}
		if openCount >= maxOpenPos {
			fmt.Printf("live-lite: skip (open positions=%d, max=%d)\n", openCount, maxOpenPos)
			if tgVerbose {
				tg.Sendf("skip %s %s: open positions=%d max=%d", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, openCount, maxOpenPos)
			}
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if execMgr != nil && execMgr.ActiveCount() >= maxOpenPos {
			fmt.Printf("live-lite: skip (active tracked entries=%d, max=%d)\n", execMgr.ActiveCount(), maxOpenPos)
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}

		if execMgr == nil {
			fmt.Println("live-lite: execution manager unavailable")
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if err := execMgr.PlaceEntry(best, entryBps, effectiveMargin, effectiveLev); err != nil {
			fmt.Println("live-lite: place error:", err)
			if tgVerbose {
				tg.Sendf("order error %s %s\n%v", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, err)
			}
		} else {
			lastOrderAt = time.Now()
			lastOrderBySymbol[strings.ToUpper(aster.RawSymbol(best.Entry.Symbol))] = lastOrderAt
			dayKey := time.Now().UTC().Format("2006-01-02")
			orderCountByDay[dayKey]++
			tg.Sendf("order placed %s %s\nmargin=$%.2f grade=%s",
				strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, effectiveMargin, best.Entry.CurrentGrade)
		}

		waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
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
	fmt.Fprintf(&b, "Live-Lite Digest (%s) %s UTC regime=%s\n", mode, now.Format("15:04"), data.CurrentRegimeCT(now))
	appendList := func(tag string, rows []inplay.Entry) {
		fmt.Fprintf(&b, "\n%s (%d)\n", tag, len(rows))
		if len(rows) == 0 {
			b.WriteString("- none\n")
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
			price := "n/a"
			move := "0.00%"
			status := string(e.State)
			switch e.State {
			case inplay.StateInPlay:
				status = "IN_PLAY"
			case inplay.StateHeating:
				status = "HEATING"
			case inplay.StateCooling:
				status = "COOLING"
			case inplay.StatePumping:
				status = "PUMPING"
			case inplay.StateDumping:
				status = "DUMPING"
			}
			slopeArrow := "→"
			if e.ScoreSlope > 0 {
				slopeArrow = "↑"
			} else if e.ScoreSlope < 0 {
				slopeArrow = "↓"
			}
			if m.LastPrice > 0 {
				price = fmtPrice(m.LastPrice)
			}
			move = fmt.Sprintf("%+.2f%%", m.Move24h)
			fmt.Fprintf(&b, "%d) %s %s g=%s s=%.2f %s%.3f\n", i+1, raw, status, e.CurrentGrade, e.CurrentScore, slopeArrow, abs(e.ScoreSlope))
			fmt.Fprintf(&b, "   px=%s 24h=%s seen=%s\n", price, move, fs)
		}
	}
	appendList("LONG", longInPlay)
	appendList("SHORT", shortInPlay)
	tg.Sendf("%s", strings.TrimSpace(b.String()))
}

func buildHourlyDigest(now time.Time, p *paperTrader, meta map[string]symbolMeta, topN int) string {
	if p == nil || !p.enabled {
		return fmt.Sprintf("Hourly Digest (%s)\npaper=disabled", now.Format("15:04 MST"))
	}
	dayKey := now.In(p.reportLoc).Format("2006-01-02")
	realized := 0.0
	if ds := p.dayStats[dayKey]; ds != nil {
		realized = ds.Net
	}
	openPnL := 0.0
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		m := meta[raw]
		if strings.EqualFold(pos.Side, "BUY") {
			openPnL += (m.LastPrice - pos.Entry) * pos.Qty
		} else {
			openPnL += (pos.Entry - m.LastPrice) * pos.Qty
		}
	}
	eq := p.balance + openPnL
	msg := p.TradeUpdateMessage(meta, topN)
	return fmt.Sprintf("Hourly Digest (%s)\nrealized=%+.2f openPnL=%+.2f netDay=%+.2f bal=%.2f eq=%.2f\n%s",
		now.Format("15:04 MST"), realized, openPnL, realized+openPnL, p.balance, eq, msg)
}

func activeMaintenanceWindow(now time.Time, enabled bool, w1, w2 maintenanceWindow) (maintenanceWindow, bool) {
	if !enabled {
		return maintenanceWindow{}, false
	}
	if inHourWindow(now.Hour(), w1.StartHour, w1.EndHour) {
		return w1, true
	}
	if inHourWindow(now.Hour(), w2.StartHour, w2.EndHour) {
		return w2, true
	}
	return maintenanceWindow{}, false
}

func inHourWindow(hour, start, end int) bool {
	if start == end {
		return false
	}
	if start < end {
		return hour >= start && hour < end
	}
	return hour >= start || hour < end
}

func runMaintenanceHook(path string, timeout time.Duration) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-lc", p)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("hook failed: %w output=%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func updateFavorableRLive(p *livePosition, mark float64) {
	if p == nil || mark <= 0 || p.EntryPrice <= 0 {
		return
	}
	risk := abs(p.EntryPrice - p.StopPrice)
	if risk <= 0 {
		return
	}
	fav := 0.0
	if strings.EqualFold(p.Side, "BUY") {
		fav = (mark - p.EntryPrice) / risk
	} else {
		fav = (p.EntryPrice - mark) / risk
	}
	if fav > p.MaxFavorableR {
		p.MaxFavorableR = fav
	}
}

func updateFavorableRPaper(p *paperPosition, mark float64) {
	if p == nil || mark <= 0 || p.Entry <= 0 {
		return
	}
	risk := abs(p.Entry - p.Stop)
	if risk <= 0 {
		return
	}
	fav := 0.0
	if strings.EqualFold(p.Side, "BUY") {
		fav = (mark - p.Entry) / risk
	} else {
		fav = (p.Entry - mark) / risk
	}
	if fav > p.MaxFavorableR {
		p.MaxFavorableR = fav
	}
}

func shouldCloseStaleLive(p *livePosition, now time.Time, maxAge time.Duration, minProgR, graceR float64) bool {
	if p == nil || maxAge <= 0 {
		return false
	}
	if p.MaxFavorableR >= graceR {
		return false
	}
	return now.Sub(p.CreatedAt) >= maxAge && p.MaxFavorableR < minProgR
}

func shouldCloseStalePaper(p *paperPosition, now time.Time, maxAge time.Duration, minProgR, graceR float64) bool {
	if p == nil || maxAge <= 0 {
		return false
	}
	if p.MaxFavorableR >= graceR {
		return false
	}
	return now.Sub(p.OpenedAt) >= maxAge && p.MaxFavorableR < minProgR
}

func beLockPrice(side string, entry, beLockBps float64) float64 {
	if entry <= 0 {
		return entry
	}
	d := beLockBps / 10000.0
	if strings.EqualFold(side, "SELL") {
		return entry * (1 - d)
	}
	return entry * (1 + d)
}

func realizedFromFill(side string, entry, fillPx, qty float64) (float64, float64) {
	if entry <= 0 || fillPx <= 0 || qty <= 0 {
		return 0, 0
	}
	pnl := 0.0
	pct := 0.0
	if strings.EqualFold(side, "BUY") {
		pnl = (fillPx - entry) * qty
		pct = ((fillPx - entry) / entry) * 100
	} else {
		pnl = (entry - fillPx) * qty
		pct = ((entry - fillPx) / entry) * 100
	}
	return pnl, pct
}

func (m *liveExecManager) addDayRealized(now time.Time, pnl float64) float64 {
	if m == nil {
		return 0
	}
	loc := m.reportLoc
	if loc == nil {
		loc = time.Local
	}
	dayKey := now.In(loc).Format("2006-01-02")
	m.dayRealized[dayKey] += pnl
	return m.dayRealized[dayKey]
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
	minStopPct := envFloat("LIVE_MIN_STOP_PCT", 0.25)
	maxStopPct := envFloat("LIVE_MAX_STOP_PCT", 8.0)
	if maxStopPct < minStopPct {
		maxStopPct = minStopPct
	}
	minTP1RR := envFloat("LIVE_MIN_RR_TP1", 0.8)
	if minTP1RR <= 0 {
		minTP1RR = 0.8
	}
	staleEnable := envBool("LIVE_STALE_ENABLE", true)
	staleMaxAge := time.Duration(envInt("LIVE_STALE_MAX_AGE_MIN", 180)) * time.Minute
	if staleMaxAge <= 0 {
		staleMaxAge = 180 * time.Minute
	}
	staleMinProg := envFloat("LIVE_STALE_MIN_PROGRESS_R", 0.25)
	staleGraceR := envFloat("LIVE_STALE_PROFIT_GRACE_R", 0.75)
	beLockBps := envFloat("LIVE_BE_LOCK_BPS", 5)
	p := &paperTrader{
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
		stateFile:    envStr("LIVE_PAPER_STATE_FILE", "out/paper_state.json"),
		tradesCSV:    envStr("LIVE_PAPER_TRADES_FILE", "out/paper_trades.csv"),
		equityCSV:    envStr("LIVE_PAPER_EQUITY_FILE", "out/paper_equity.csv"),
		maxOpen:      maxOpen,
		positions:    map[string]*paperPosition{},
		reportLoc:    reportLoc,
		dayStats:     map[string]*paperDayStats{},
		minStopPct:   minStopPct,
		maxStopPct:   maxStopPct,
		minTP1RR:     minTP1RR,
		staleEnable:  staleEnable,
		staleMaxAge:  staleMaxAge,
		staleMinProg: staleMinProg,
		staleGraceR:  staleGraceR,
		beLockBps:    beLockBps,
	}
	if p.enabled {
		if err := p.load(); err != nil {
			fmt.Printf("live-lite: paper state load warning: %v\n", err)
		}
	}
	return p
}

func (p *paperTrader) load() error {
	if p == nil || !p.enabled || strings.TrimSpace(p.stateFile) == "" {
		return nil
	}
	b, err := os.ReadFile(p.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var st paperState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	if st.Balance > 0 {
		p.balance = st.Balance
	}
	if st.StartBal > 0 {
		p.startBal = st.StartBal
	}
	if st.Reserve > 0 {
		p.reserve = st.Reserve
	}
	if st.Positions != nil {
		p.positions = st.Positions
	}
	if st.DayStats != nil {
		p.dayStats = st.DayStats
	}
	return nil
}

func (p *paperTrader) save() error {
	if p == nil || !p.enabled || strings.TrimSpace(p.stateFile) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.stateFile), 0o755); err != nil {
		return err
	}
	st := paperState{
		StartBal:  p.startBal,
		Balance:   p.balance,
		Reserve:   p.reserve,
		Positions: p.positions,
		DayStats:  p.dayStats,
		UpdatedAt: time.Now().UTC(),
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.stateFile, b, 0o644)
}

func newLiveExecManager(rest *aster.RESTAuth, tg *telegramSink) *liveExecManager {
	if rest == nil {
		return nil
	}
	stopPct := envFloat("LIVE_STOP_PCT", 2.0)
	if stopPct <= 0 {
		stopPct = 2.0
	}
	tp1R := envFloat("LIVE_TP1_R", 1.0)
	tp2R := envFloat("LIVE_TP2_R", 2.0)
	tp3R := envFloat("LIVE_TP3_R", 3.0)
	if tp1R <= 0 {
		tp1R = 1.0
	}
	if tp2R < tp1R {
		tp2R = tp1R
	}
	if tp3R < tp2R {
		tp3R = tp2R
	}
	tp1Frac := envFloat("LIVE_TP1_FRAC", 0.4)
	tp2Frac := envFloat("LIVE_TP2_FRAC", 0.3)
	tp3Frac := envFloat("LIVE_TP3_FRAC", 0.3)
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
		sumFrac = 1
	}
	tp1Frac /= sumFrac
	tp2Frac /= sumFrac
	tp3Frac /= sumFrac
	trailAfterTP := envInt("LIVE_TRAIL_AFTER_TP", 2)
	if trailAfterTP < 1 {
		trailAfterTP = 1
	}
	if trailAfterTP > 3 {
		trailAfterTP = 3
	}
	trailStopPct := envFloat("LIVE_TRAIL_STOP_PCT", 1.0)
	if trailStopPct <= 0 {
		trailStopPct = 1.0
	}
	trailStepBps := envFloat("LIVE_TRAIL_STEP_BPS", 10.0)
	if trailStepBps < 0 {
		trailStepBps = 0
	}
	minStopPct := envFloat("LIVE_MIN_STOP_PCT", 0.25)
	maxStopPct := envFloat("LIVE_MAX_STOP_PCT", 8.0)
	if maxStopPct < minStopPct {
		maxStopPct = minStopPct
	}
	minTP1RR := envFloat("LIVE_MIN_RR_TP1", 0.8)
	if minTP1RR <= 0 {
		minTP1RR = 0.8
	}
	beLockBps := envFloat("LIVE_BE_LOCK_BPS", 5)
	staleEnable := envBool("LIVE_STALE_ENABLE", true)
	staleMaxAge := time.Duration(envInt("LIVE_STALE_MAX_AGE_MIN", 180)) * time.Minute
	if staleMaxAge <= 0 {
		staleMaxAge = 180 * time.Minute
	}
	staleMinProg := envFloat("LIVE_STALE_MIN_PROGRESS_R", 0.25)
	staleGraceR := envFloat("LIVE_STALE_PROFIT_GRACE_R", 0.75)
	marginType := strings.ToUpper(envStr("LIVE_MARGIN_TYPE", "ISOLATED"))
	enforceIsolated := envBool("LIVE_ENFORCE_MARGIN_TYPE", true)
	reportTZ := envStr("LIVE_REPORT_TZ", "America/Chicago")
	reportLoc, err := time.LoadLocation(reportTZ)
	if err != nil {
		reportLoc = time.Local
	}

	m := &liveExecManager{
		rest:            rest,
		tg:              tg,
		path:            envStr("LIVE_STATE_FILE", "out/live_exec_state.json"),
		entryTimeout:    time.Duration(envInt("LIVE_ENTRY_TIMEOUT_SEC", 90)) * time.Second,
		stopPct:         stopPct,
		tp1R:            tp1R,
		tp2R:            tp2R,
		tp3R:            tp3R,
		tp1Frac:         tp1Frac,
		tp2Frac:         tp2Frac,
		tp3Frac:         tp3Frac,
		trailAfterTP:    trailAfterTP,
		trailStopPct:    trailStopPct,
		trailStepBps:    trailStepBps,
		minStopPct:      minStopPct,
		maxStopPct:      maxStopPct,
		minTP1RR:        minTP1RR,
		beLockBps:       beLockBps,
		staleEnable:     staleEnable,
		staleMaxAge:     staleMaxAge,
		staleMinProg:    staleMinProg,
		staleGraceR:     staleGraceR,
		marginType:      marginType,
		enforceIsolated: enforceIsolated,
		positions:       map[string]*livePosition{},
		dayRealized:     map[string]float64{},
		reportLoc:       reportLoc,
	}
	if m.entryTimeout <= 0 {
		m.entryTimeout = 90 * time.Second
	}
	_ = m.load()
	return m
}

func (m *liveExecManager) load() error {
	if m == nil || m.path == "" {
		return nil
	}
	b, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var st liveExecStore
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	if st.Positions == nil {
		st.Positions = map[string]*livePosition{}
	}
	m.positions = st.Positions
	return nil
}

func (m *liveExecManager) save() error {
	if m == nil || m.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	st := liveExecStore{Positions: m.positions}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0o644)
}

func (m *liveExecManager) ReconcileBootState() (closedLocal int, importedRemote int, err error) {
	if m == nil || m.rest == nil {
		return 0, 0, nil
	}
	rows, err := m.rest.PositionRisk("")
	if err != nil {
		return 0, 0, err
	}
	type remotePos struct {
		amt   float64
		entry float64
		mark  float64
		lev   int
	}
	remote := map[string]remotePos{}
	for _, row := range rows {
		sym := strings.ToUpper(strings.TrimSpace(fmt.Sprint(row["symbol"])))
		amt := mapFloat(row["positionAmt"])
		if sym == "" || abs(amt) <= 1e-10 {
			continue
		}
		remote[sym] = remotePos{
			amt:   amt,
			entry: mapFloat(row["entryPrice"]),
			mark:  mapFloat(row["markPrice"]),
			lev:   int(mapFloat(row["leverage"])),
		}
	}
	now := time.Now().UTC()
	for sym, p := range m.positions {
		if !m.isActive(p) {
			continue
		}
		rp, ok := remote[sym]
		if ok && abs(rp.amt) > 1e-10 {
			continue
		}
		_ = m.cancelRemainingExits(p)
		p.State = execClosed
		p.CloseReason = "POSITION_FLAT_RECOVERED"
		p.ClosedAt = now
		p.UpdatedAt = now
		closedLocal++
	}
	for sym, rp := range remote {
		if m.isActive(m.positions[sym]) {
			continue
		}
		side := "BUY"
		if rp.amt < 0 {
			side = "SELL"
		}
		qty := abs(rp.amt)
		entry := rp.entry
		if entry <= 0 {
			entry = rp.mark
		}
		if entry <= 0 || qty <= 0 {
			continue
		}
		p := &livePosition{
			Symbol:       sym,
			Side:         side,
			State:        execOpen,
			CreatedAt:    now,
			UpdatedAt:    now,
			EntryPrice:   entry,
			Qty:          qty,
			FilledQty:    qty,
			RemainingQty: qty,
			Leverage:     maxInt(1, rp.lev),
			CloseReason:  "RECOVERED_POSITION",
		}
		stopPct := clamp(m.stopPct/100.0, m.minStopPct/100.0, m.maxStopPct/100.0)
		if strings.EqualFold(side, "BUY") {
			p.StopPrice = entry * (1 - stopPct)
		} else {
			p.StopPrice = entry * (1 + stopPct)
		}
		m.positions[sym] = p
		_ = m.placeOrReplaceStop(p)
		importedRemote++
	}
	_ = m.save()
	return closedLocal, importedRemote, nil
}

func (m *liveExecManager) isActive(p *livePosition) bool {
	return p != nil && p.State != execClosed
}

func (m *liveExecManager) HasActiveSymbol(symbol string) bool {
	if m == nil {
		return false
	}
	raw := strings.ToUpper(aster.RawSymbol(symbol))
	p := m.positions[raw]
	return m.isActive(p)
}

func (m *liveExecManager) ActiveCount() int {
	if m == nil {
		return 0
	}
	n := 0
	for _, p := range m.positions {
		if m.isActive(p) {
			n++
		}
	}
	return n
}

func (m *liveExecManager) Snapshot(limit int) liveExecSnapshot {
	out := liveExecSnapshot{Generated: time.Now().UTC()}
	if m == nil {
		return out
	}
	if limit <= 0 {
		limit = 10
	}
	for _, p := range m.positions {
		if p == nil {
			continue
		}
		out.Total++
		switch p.State {
		case execPendingEntry:
			out.Pending++
		case execOpen:
			out.Open++
		case execPartialTP1:
			out.Partial1++
		case execPartialTP2:
			out.Partial2++
		case execClosed:
			out.Closed++
		}
		if p.State != execClosed && len(out.Active) < limit {
			cp := *p
			out.Active = append(out.Active, cp)
		}
	}
	sort.Slice(out.Active, func(i, j int) bool {
		return out.Active[i].UpdatedAt.After(out.Active[j].UpdatedAt)
	})
	return out
}

func (m *liveExecManager) PlaceEntry(c candidate, entryBps, margin float64, lev int) error {
	if m == nil || m.rest == nil {
		return fmt.Errorf("execution manager not ready")
	}
	rawSym := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	if m.HasActiveSymbol(rawSym) {
		return fmt.Errorf("active state already exists for %s", rawSym)
	}
	bid, ask, err := m.rest.BookTicker(rawSym)
	if err != nil {
		return err
	}
	mid := (bid + ask) / 2
	if mid <= 0 {
		return fmt.Errorf("invalid mid price")
	}
	price := mid
	if strings.EqualFold(c.Side, "BUY") {
		price = mid * (1 - entryBps/10000.0)
	} else {
		price = mid * (1 + entryBps/10000.0)
	}
	meta, err := m.rest.SymbolMeta(rawSym, true)
	if err != nil {
		return err
	}
	price, _, err = m.rest.RoundPrice(rawSym, price)
	if err != nil {
		return err
	}
	qty := margin * float64(maxInt(lev, 1)) / price
	qty, _, err = m.rest.RoundQty(rawSym, qty)
	if err != nil {
		return err
	}
	if qty <= 0 {
		return fmt.Errorf("qty <= 0 after rounding")
	}
	if lev <= 0 {
		lev = 1
	}
	_, _ = m.rest.ChangeLeverage(rawSym, lev)
	if m.marginType != "" {
		if _, err := m.rest.ChangeMarginType(rawSym, m.marginType); err != nil && m.enforceIsolated {
			return fmt.Errorf("set margin type %s failed: %w", m.marginType, err)
		}
	}

	vals := url.Values{}
	vals.Set("symbol", rawSym)
	vals.Set("side", strings.ToUpper(c.Side))
	vals.Set("type", "LIMIT")
	vals.Set("timeInForce", "GTC")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "false")
	vals.Set("quantity", formatFloat(qty, meta.QtyPrecision))
	vals.Set("price", formatFloat(price, meta.PricePrecision))

	out, err := m.rest.PlaceOrder(vals)
	if err != nil {
		return err
	}
	orderID := mapInt64(out["orderId"])
	if orderID == 0 {
		return fmt.Errorf("missing orderId from place response")
	}
	now := time.Now().UTC()
	p := &livePosition{
		Symbol:        rawSym,
		Side:          strings.ToUpper(c.Side),
		State:         execPendingEntry,
		CreatedAt:     now,
		UpdatedAt:     now,
		EntryOrderID:  orderID,
		EntryPrice:    price,
		Qty:           qty,
		Margin:        margin,
		Leverage:      lev,
		VPSetup:       c.Sig.VPSetup,
		VPLevel:       c.Sig.VPLevel,
		VPTargetLevel: c.Sig.VPTargetLevel,
		VPStopMode:    c.Sig.StopMode,
		VPTargetMode:  c.Sig.TargetMode,
		RejectReason:  c.RejectReason,
		EntryReason:   c.Strat,
		EntryConf:     c.Conf,
		EntryTags:     append([]string{}, c.Sig.Tags...),
		EntryReasons:  append([]string{}, c.Sig.Reasons...),
		RegimeTag:     c.Sig.RegimeTag,
	}
	if c.Sig.Entry > 0 && c.Sig.Stop > 0 {
		risk := abs(c.Sig.Entry-c.Sig.Stop) / c.Sig.Entry
		if risk > 0 {
			p.CustomRiskPct = risk
			baseRisk := abs(c.Sig.Entry - c.Sig.Stop)
			if baseRisk > 0 && c.Sig.TP1 > 0 {
				p.CustomTP1R = abs(c.Sig.TP1-c.Sig.Entry) / baseRisk
			}
			if baseRisk > 0 && c.Sig.TP2 > 0 {
				p.CustomTP2R = abs(c.Sig.TP2-c.Sig.Entry) / baseRisk
			}
		}
	}
	m.positions[rawSym] = p
	_ = m.save()
	fmt.Printf("live-lite: entry submitted %s %s qty=%s px=%s orderId=%d\n",
		rawSym, p.Side, vals.Get("quantity"), vals.Get("price"), orderID)
	if m.tg != nil {
		m.tg.Sendf("entry submitted %s %s\nqty=%s limit=%s id=%d",
			rawSym, p.Side, vals.Get("quantity"), vals.Get("price"), orderID)
	}
	return nil
}

func (m *liveExecManager) Reconcile(now time.Time) {
	if m == nil || m.rest == nil || len(m.positions) == 0 {
		return
	}
	changed := false
	for sym, p := range m.positions {
		if p == nil || p.State == execClosed {
			continue
		}
		switch p.State {
		case execPendingEntry:
			ch, err := m.reconcilePendingEntry(now, p)
			if err != nil {
				fmt.Printf("live-lite: reconcile pending %s error: %v\n", sym, err)
			}
			changed = changed || ch
		case execOpen, execPartialTP1, execPartialTP2:
			ch, err := m.reconcileOpen(now, p)
			if err != nil {
				fmt.Printf("live-lite: reconcile open %s error: %v\n", sym, err)
			}
			changed = changed || ch
		}
	}
	if changed {
		_ = m.save()
	}
}

func (m *liveExecManager) reconcilePendingEntry(now time.Time, p *livePosition) (bool, error) {
	order, err := m.rest.GetOrder(p.Symbol, p.EntryOrderID)
	if err != nil {
		return false, err
	}
	status := strings.ToUpper(strings.TrimSpace(fmt.Sprint(order["status"])))
	execQty := mapFloat(order["executedQty"])
	avgPx := mapFloat(order["avgPrice"])
	if avgPx <= 0 {
		avgPx = mapFloat(order["price"])
	}
	if status == "FILLED" {
		if execQty <= 0 {
			execQty = p.Qty
		}
		if avgPx > 0 {
			p.EntryPrice = avgPx
		}
		p.FilledQty = execQty
		p.RemainingQty = execQty
		p.State = execOpen
		p.UpdatedAt = now
		if err := m.placeInitialBrackets(p); err != nil {
			return true, err
		}
		fmt.Printf("live-lite: entry filled %s qty=%.6f avg=%.6f\n", p.Symbol, p.FilledQty, p.EntryPrice)
		if m.tg != nil {
			m.tg.Sendf("entry filled %s %s\nqty=%.6f avg=%s reason=%s conf=%.2f",
				p.Symbol, p.Side, p.FilledQty, fmtPrice(p.EntryPrice), p.EntryReason, p.EntryConf)
		}
		return true, nil
	}
	if now.Sub(p.CreatedAt) >= m.entryTimeout {
		_, _ = m.rest.CancelOrder(p.Symbol, p.EntryOrderID)
		p.State = execClosed
		p.CloseReason = "ENTRY_TIMEOUT"
		p.ClosedAt = now
		p.UpdatedAt = now
		fmt.Printf("live-lite: entry timeout/canceled %s orderId=%d\n", p.Symbol, p.EntryOrderID)
		if m.tg != nil {
			m.tg.Sendf("entry timeout %s\nid=%d", p.Symbol, p.EntryOrderID)
		}
		return true, nil
	}
	return false, nil
}

func (m *liveExecManager) reconcileOpen(now time.Time, p *livePosition) (bool, error) {
	changed := false
	closedByStop, err := m.reconcileExitOrders(now, p)
	if err != nil {
		return changed, err
	}
	if closedByStop {
		return true, nil
	}
	if p.State == execClosed {
		return true, nil
	}
	if p.RemainingQty > 0 {
		mark, err := m.currentMark(p.Symbol)
		if err == nil && mark > 0 {
			p.LastMark = mark
			updateFavorableRLive(p, mark)
			updated, err := m.updateTrailingStop(p, mark)
			if err != nil {
				return changed, err
			}
			if updated {
				changed = true
			}
			if m.staleEnable && shouldCloseStaleLive(p, now, m.staleMaxAge, m.staleMinProg, m.staleGraceR) {
				pnl, pct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
				_ = m.cancelRemainingExits(p)
				if err := m.closeSymbolMarket(p.Symbol); err != nil {
					return changed, err
				}
				p.State = execClosed
				p.CloseReason = "STALE_NO_PROGRESS"
				p.ClosedAt = now
				p.UpdatedAt = now
				if m.tg != nil {
					m.tg.Sendf("position closed %s %s\nqty=%.6f px=%s pnl=%+.2f (%+.2f%%)\nreason=%s",
						p.Symbol, p.Side, p.RemainingQty, fmtPrice(mark), pnl, pct, p.CloseReason)
				}
				return true, nil
			}
		}
	}
	// Ensure there is always a protective stop while position is live.
	if p.RemainingQty > 0 && p.StopOrderID == 0 {
		if err := m.placeOrReplaceStop(p); err != nil {
			return changed, err
		}
		changed = true
	}
	// If exchange position is flat, close local state and cancel leftovers.
	rows, err := m.rest.PositionRisk(p.Symbol)
	if err == nil {
		amtAbs := 0.0
		for _, row := range rows {
			amtAbs = maxFloat(amtAbs, abs(mapFloat(row["positionAmt"])))
		}
		if amtAbs <= 1e-10 {
			_ = m.cancelRemainingExits(p)
			p.State = execClosed
			p.CloseReason = "POSITION_FLAT"
			p.ClosedAt = now
			p.UpdatedAt = now
			if m.tg != nil {
				m.tg.Sendf("position closed %s\nreason=%s", p.Symbol, p.CloseReason)
			}
			return true, nil
		}
	}
	return changed, nil
}

func (m *liveExecManager) reconcileExitOrders(now time.Time, p *livePosition) (bool, error) {
	changed := false
	if p.TP1OrderID > 0 {
		filled, execQty, fillPx, err := m.checkOrderFilled(p.Symbol, p.TP1OrderID)
		if err != nil {
			return changed, err
		}
		if filled {
			p.TP1OrderID = 0
			p.RemainingQty = maxFloat(0, p.RemainingQty-execQty)
			p.State = execPartialTP1
			p.UpdatedAt = now
			pnl, pct := realizedFromFill(p.Side, p.EntryPrice, fillPx, execQty)
			p.RealizedPnL += pnl
			dayRealized := m.addDayRealized(now, pnl)
			if m.beLockBps > 0 {
				p.StopPrice = beLockPrice(p.Side, p.EntryPrice, m.beLockBps)
			}
			m.maybeEnableTrail(p, 1)
			if err := m.placeOrReplaceStop(p); err != nil {
				return true, err
			}
			if m.tg != nil {
				m.tg.Sendf("tp1 hit %s %s\nqty=%.6f px=%s pnl=%+.2f (%+.2f%%)\nreason=TP1_HIT dayRealized=%+.2f",
					p.Symbol, p.Side, execQty, fmtPrice(fillPx), pnl, pct, dayRealized)
			}
			changed = true
		}
	}
	if p.TP2OrderID > 0 {
		filled, execQty, fillPx, err := m.checkOrderFilled(p.Symbol, p.TP2OrderID)
		if err != nil {
			return changed, err
		}
		if filled {
			p.TP2OrderID = 0
			p.RemainingQty = maxFloat(0, p.RemainingQty-execQty)
			p.State = execPartialTP2
			p.UpdatedAt = now
			pnl, pct := realizedFromFill(p.Side, p.EntryPrice, fillPx, execQty)
			p.RealizedPnL += pnl
			dayRealized := m.addDayRealized(now, pnl)
			m.maybeEnableTrail(p, 2)
			if err := m.placeOrReplaceStop(p); err != nil {
				return true, err
			}
			if m.tg != nil {
				m.tg.Sendf("tp2 hit %s %s\nqty=%.6f px=%s pnl=%+.2f (%+.2f%%)\nreason=TP2_HIT dayRealized=%+.2f",
					p.Symbol, p.Side, execQty, fmtPrice(fillPx), pnl, pct, dayRealized)
			}
			changed = true
		}
	}
	if p.TP3OrderID > 0 {
		filled, execQty, fillPx, err := m.checkOrderFilled(p.Symbol, p.TP3OrderID)
		if err != nil {
			return changed, err
		}
		if filled {
			p.TP3OrderID = 0
			p.RemainingQty = maxFloat(0, p.RemainingQty-execQty)
			p.UpdatedAt = now
			pnl, pct := realizedFromFill(p.Side, p.EntryPrice, fillPx, execQty)
			p.RealizedPnL += pnl
			dayRealized := m.addDayRealized(now, pnl)
			m.maybeEnableTrail(p, 3)
			if m.tg != nil {
				m.tg.Sendf("tp3 hit %s %s\nqty=%.6f px=%s pnl=%+.2f (%+.2f%%)\nreason=TP3_HIT dayRealized=%+.2f",
					p.Symbol, p.Side, execQty, fmtPrice(fillPx), pnl, pct, dayRealized)
			}
			changed = true
		}
	}
	if p.StopOrderID > 0 {
		filled, execQty, fillPx, err := m.checkOrderFilled(p.Symbol, p.StopOrderID)
		if err != nil {
			return changed, err
		}
		if filled {
			p.StopOrderID = 0
			p.RemainingQty = maxFloat(0, p.RemainingQty-execQty)
			_ = m.cancelRemainingExits(p)
			p.State = execClosed
			p.CloseReason = "STOP_HIT"
			p.ClosedAt = now
			p.UpdatedAt = now
			pnl, pct := realizedFromFill(p.Side, p.EntryPrice, fillPx, execQty)
			p.RealizedPnL += pnl
			dayRealized := m.addDayRealized(now, pnl)
			if m.tg != nil {
				m.tg.Sendf("stop hit %s %s\nqty=%.6f px=%s pnl=%+.2f (%+.2f%%)\nreason=STOP_HIT dayRealized=%+.2f",
					p.Symbol, p.Side, execQty, fmtPrice(fillPx), pnl, pct, dayRealized)
			}
			return true, nil
		}
	}
	if p.RemainingQty <= 1e-10 {
		_ = m.cancelRemainingExits(p)
		p.State = execClosed
		p.CloseReason = "TP_DONE"
		p.ClosedAt = now
		p.UpdatedAt = now
		return true, nil
	}
	return changed, nil
}

func (m *liveExecManager) placeInitialBrackets(p *livePosition) error {
	sideBuy := strings.EqualFold(p.Side, "BUY")
	stopPct := m.stopPct / 100.0
	if stopPct <= 0 {
		stopPct = 0.02
	}
	stopPct = clamp(stopPct, m.minStopPct/100.0, m.maxStopPct/100.0)
	if p.CustomRiskPct > 0 {
		stopPct = clamp(p.CustomRiskPct, m.minStopPct/100.0, m.maxStopPct/100.0)
	}
	tp1R := m.tp1R
	tp2R := m.tp2R
	if p.CustomTP1R > 0 {
		tp1R = p.CustomTP1R
	}
	if p.CustomTP2R > 0 {
		tp2R = p.CustomTP2R
	}
	if sideBuy {
		p.StopPrice = p.EntryPrice * (1 - stopPct)
		p.TP1Price = p.EntryPrice * (1 + stopPct*tp1R)
		p.TP2Price = p.EntryPrice * (1 + stopPct*tp2R)
		p.TP3Price = p.EntryPrice * (1 + stopPct*m.tp3R)
	} else {
		p.StopPrice = p.EntryPrice * (1 + stopPct)
		p.TP1Price = p.EntryPrice * (1 - stopPct*tp1R)
		p.TP2Price = p.EntryPrice * (1 - stopPct*tp2R)
		p.TP3Price = p.EntryPrice * (1 - stopPct*m.tp3R)
	}
	if p.StopPrice <= 0 || p.TP1Price <= 0 || p.TP2Price <= 0 || p.TP3Price <= 0 {
		return fmt.Errorf("invalid bracket levels stop=%.6f tp1=%.6f tp2=%.6f tp3=%.6f",
			p.StopPrice, p.TP1Price, p.TP2Price, p.TP3Price)
	}
	risk := abs(p.EntryPrice - p.StopPrice)
	reward := abs(p.TP1Price - p.EntryPrice)
	if risk <= 0 || reward/risk < m.minTP1RR {
		return fmt.Errorf("tp1 rr below minimum: rr=%.3f min=%.3f", reward/maxFloat(risk, 1e-9), m.minTP1RR)
	}
	p.TrailRef = p.EntryPrice
	p.TrailStop = p.StopPrice
	q1 := p.FilledQty * m.tp1Frac
	q2 := p.FilledQty * m.tp2Frac

	var err error
	p.TP1Qty, err = m.roundQty(p.Symbol, q1)
	if err != nil {
		return err
	}
	p.TP2Qty, err = m.roundQty(p.Symbol, q2)
	if err != nil {
		return err
	}
	remForTP3 := maxFloat(0, p.FilledQty-p.TP1Qty-p.TP2Qty)
	p.TP3Qty, err = m.roundQty(p.Symbol, remForTP3)
	if err != nil {
		return err
	}

	if p.TP1Qty > 0 {
		if p.TP1OrderID, err = m.placeReduceLimit(p, p.TP1Qty, p.TP1Price); err != nil {
			return err
		}
	}
	if p.TP2Qty > 0 {
		if p.TP2OrderID, err = m.placeReduceLimit(p, p.TP2Qty, p.TP2Price); err != nil {
			return err
		}
	}
	if p.TP3Qty > 0 {
		if p.TP3OrderID, err = m.placeReduceLimit(p, p.TP3Qty, p.TP3Price); err != nil {
			return err
		}
	}
	if err := m.placeOrReplaceStop(p); err != nil {
		return err
	}
	return nil
}

func (m *liveExecManager) placeReduceLimit(p *livePosition, qty, price float64) (int64, error) {
	meta, err := m.rest.SymbolMeta(p.Symbol, true)
	if err != nil {
		return 0, err
	}
	qty, _, err = m.rest.RoundQty(p.Symbol, qty)
	if err != nil {
		return 0, err
	}
	price, _, err = m.rest.RoundPrice(p.Symbol, price)
	if err != nil {
		return 0, err
	}
	if qty <= 0 || price <= 0 {
		return 0, fmt.Errorf("invalid reduce limit qty/price")
	}
	closeSide := "SELL"
	if strings.EqualFold(p.Side, "SELL") {
		closeSide = "BUY"
	}
	vals := url.Values{}
	vals.Set("symbol", p.Symbol)
	vals.Set("side", closeSide)
	vals.Set("type", "LIMIT")
	vals.Set("timeInForce", "GTC")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "true")
	vals.Set("quantity", formatFloat(qty, meta.QtyPrecision))
	vals.Set("price", formatFloat(price, meta.PricePrecision))
	out, err := m.rest.PlaceOrder(vals)
	if err != nil {
		return 0, err
	}
	return mapInt64(out["orderId"]), nil
}

func (m *liveExecManager) placeOrReplaceStop(p *livePosition) error {
	if p.RemainingQty <= 0 {
		return nil
	}
	if p.StopOrderID > 0 {
		_, _ = m.rest.CancelOrder(p.Symbol, p.StopOrderID)
		p.StopOrderID = 0
	}
	meta, err := m.rest.SymbolMeta(p.Symbol, true)
	if err != nil {
		return err
	}
	qty, _, err := m.rest.RoundQty(p.Symbol, p.RemainingQty)
	if err != nil {
		return err
	}
	stopPx, _, err := m.rest.RoundPrice(p.Symbol, p.StopPrice)
	if err != nil {
		return err
	}
	if qty <= 0 || stopPx <= 0 {
		return fmt.Errorf("invalid stop qty/price")
	}
	closeSide := "SELL"
	if strings.EqualFold(p.Side, "SELL") {
		closeSide = "BUY"
	}
	vals := url.Values{}
	vals.Set("symbol", p.Symbol)
	vals.Set("side", closeSide)
	vals.Set("type", "STOP_MARKET")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "true")
	vals.Set("quantity", formatFloat(qty, meta.QtyPrecision))
	vals.Set("stopPrice", formatFloat(stopPx, meta.PricePrecision))
	out, err := m.rest.PlaceOrder(vals)
	if err != nil {
		return err
	}
	p.StopOrderID = mapInt64(out["orderId"])
	return nil
}

func (m *liveExecManager) currentMark(symbol string) (float64, error) {
	bid, ask, err := m.rest.BookTicker(symbol)
	if err != nil {
		return 0, err
	}
	mid := (bid + ask) / 2
	if mid <= 0 {
		return 0, fmt.Errorf("invalid mark")
	}
	return mid, nil
}

func (m *liveExecManager) maybeEnableTrail(p *livePosition, stage int) {
	if p == nil {
		return
	}
	if stage < m.trailAfterTP {
		return
	}
	p.TrailOn = true
}

func (m *liveExecManager) updateTrailingStop(p *livePosition, mark float64) (bool, error) {
	if p == nil || !p.TrailOn || p.RemainingQty <= 0 || mark <= 0 {
		return false, nil
	}
	sideBuy := strings.EqualFold(p.Side, "BUY")
	if p.TrailRef <= 0 {
		p.TrailRef = p.EntryPrice
	}
	if sideBuy {
		if mark <= p.TrailRef {
			return false, nil
		}
	} else {
		if mark >= p.TrailRef {
			return false, nil
		}
	}
	newRef := mark
	newStop := m.calcTrailStop(sideBuy, newRef)
	threshold := p.StopPrice * (m.trailStepBps / 10000.0)
	if threshold < 0 {
		threshold = 0
	}
	improved := false
	if sideBuy {
		improved = newStop > p.StopPrice+threshold
	} else {
		improved = newStop < p.StopPrice-threshold
	}
	if !improved {
		return false, nil
	}
	p.TrailRef = newRef
	p.TrailStop = newStop
	p.StopPrice = newStop
	if err := m.placeOrReplaceStop(p); err != nil {
		return false, err
	}
	if m.tg != nil {
		m.tg.Sendf("trail move %s\nstop=%s mark=%s", p.Symbol, fmtPrice(p.StopPrice), fmtPrice(mark))
	}
	return true, nil
}

func (m *liveExecManager) calcTrailStop(sideBuy bool, ref float64) float64 {
	pct := m.trailStopPct / 100.0
	if pct <= 0 {
		pct = 0.01
	}
	if sideBuy {
		return ref * (1 - pct)
	}
	return ref * (1 + pct)
}

func (m *liveExecManager) checkOrderFilled(symbol string, orderID int64) (bool, float64, float64, error) {
	if orderID <= 0 {
		return false, 0, 0, nil
	}
	o, err := m.rest.GetOrder(symbol, orderID)
	if err != nil {
		return false, 0, 0, err
	}
	status := strings.ToUpper(strings.TrimSpace(fmt.Sprint(o["status"])))
	if status == "FILLED" {
		execQty := mapFloat(o["executedQty"])
		if execQty <= 0 {
			execQty = mapFloat(o["origQty"])
		}
		fillPx := mapFloat(o["avgPrice"])
		if fillPx <= 0 {
			fillPx = mapFloat(o["price"])
		}
		return true, execQty, fillPx, nil
	}
	if status == "CANCELED" || status == "EXPIRED" || status == "REJECTED" {
		return false, 0, 0, nil
	}
	return false, 0, 0, nil
}

func (m *liveExecManager) cancelRemainingExits(p *livePosition) error {
	for _, id := range []int64{p.TP1OrderID, p.TP2OrderID, p.TP3OrderID, p.StopOrderID} {
		if id > 0 {
			_, _ = m.rest.CancelOrder(p.Symbol, id)
		}
	}
	p.TP1OrderID, p.TP2OrderID, p.TP3OrderID, p.StopOrderID = 0, 0, 0, 0
	return nil
}

func (m *liveExecManager) roundQty(symbol string, qty float64) (float64, error) {
	q, _, err := m.rest.RoundQty(symbol, qty)
	return q, err
}

func (m *liveExecManager) ForceCloseAll(reason string) error {
	if m == nil || m.rest == nil {
		return nil
	}
	now := time.Now().UTC()
	for sym, p := range m.positions {
		if p == nil || p.State == execClosed {
			continue
		}
		mark, _ := m.currentMark(sym)
		if mark <= 0 {
			mark = p.EntryPrice
		}
		pnl, pct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
		dayRealized := m.addDayRealized(now, pnl)
		_ = m.cancelRemainingExits(p)
		_, _ = m.rest.CancelAllOrders(sym)
		_ = m.closeSymbolMarket(sym)
		if m.tg != nil {
			m.tg.Sendf("forced close %s %s\nqty=%.6f px=%s pnl=%+.2f (%+.2f%%)\nreason=%s dayRealized=%+.2f",
				sym, p.Side, p.RemainingQty, fmtPrice(mark), pnl, pct, reason, dayRealized)
		}
		p.State = execClosed
		p.CloseReason = reason
		p.ClosedAt = now
		p.UpdatedAt = now
	}
	_ = m.save()
	return nil
}

func (m *liveExecManager) closeSymbolMarket(symbol string) error {
	rows, err := m.rest.PositionRisk(symbol)
	if err != nil {
		return err
	}
	for _, row := range rows {
		amt := mapFloat(row["positionAmt"])
		if amt == 0 {
			continue
		}
		side := "SELL"
		if amt < 0 {
			side = "BUY"
		}
		qty, _, err := m.rest.RoundQty(symbol, abs(amt))
		if err != nil {
			return err
		}
		if qty <= 0 {
			continue
		}
		meta, err := m.rest.SymbolMeta(symbol, true)
		if err != nil {
			return err
		}
		vals := url.Values{}
		vals.Set("symbol", symbol)
		vals.Set("side", side)
		vals.Set("type", "MARKET")
		vals.Set("positionSide", "BOTH")
		vals.Set("reduceOnly", "true")
		vals.Set("quantity", formatFloat(qty, meta.QtyPrecision))
		if _, err := m.rest.PlaceOrder(vals); err != nil {
			return err
		}
	}
	return nil
}

func (p *paperTrader) Summary(meta map[string]symbolMeta) string {
	if p == nil || !p.enabled {
		return ""
	}
	openPnL := 0.0
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
			parts = append(parts, fmt.Sprintf("%s %s e=%.6f m=%.6f q=%.6f upnl=%+.3f", raw, pos.Side, pos.Entry, mark, pos.Qty, pnl))
		}
		sort.Strings(parts)
		if len(parts) > 3 {
			openTxt = strings.Join(parts[:3], " | ") + fmt.Sprintf(" | +%d more", len(parts)-3)
		} else {
			openTxt = strings.Join(parts, " | ")
		}
	}
	dayKey := time.Now().In(p.reportLoc).Format("2006-01-02")
	realized := 0.0
	if ds := p.dayStats[dayKey]; ds != nil {
		realized = ds.Net
	}
	eq := p.balance + openPnL
	return fmt.Sprintf("🧪 PAPER bal=%.2f eq=%.2f realizedToday=%+.2f openPnL=%+.2f netDay=%+.2f open=%d/%d %s",
		p.balance, eq, realized, openPnL, realized+openPnL, len(p.positions), p.maxOpen, openTxt)
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
	stopPct = clamp(stopPct, p.minStopPct/100.0, p.maxStopPct/100.0)
	tp1R := p.tp1R
	tp2R := p.tp2R
	if c.Sig.Entry > 0 && c.Sig.Stop > 0 {
		riskPct := abs(c.Sig.Entry-c.Sig.Stop) / c.Sig.Entry
		if riskPct > 0 {
			stopPct = clamp(riskPct, p.minStopPct/100.0, p.maxStopPct/100.0)
			baseRisk := abs(c.Sig.Entry - c.Sig.Stop)
			if baseRisk > 0 && c.Sig.TP1 > 0 {
				tp1R = abs(c.Sig.TP1-c.Sig.Entry) / baseRisk
			}
			if baseRisk > 0 && c.Sig.TP2 > 0 {
				tp2R = abs(c.Sig.TP2-c.Sig.Entry) / baseRisk
			}
		}
	}
	tp1Pct := stopPct * tp1R
	tp2Pct := stopPct * tp2R
	tp3Pct := stopPct * p.tp3R
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
	if stop <= 0 || tp1 <= 0 || tp2 <= 0 || tp3 <= 0 {
		return nil, fmt.Errorf("invalid paper bracket levels")
	}
	risk := abs(entry - stop)
	reward := abs(tp1 - entry)
	if risk <= 0 || reward/risk < p.minTP1RR {
		return nil, fmt.Errorf("paper tp1 rr below minimum")
	}
	fee := notional * p.feeBps / 10000.0
	p.balance -= fee
	pos := &paperPosition{
		Symbol:      raw,
		Side:        strings.ToUpper(c.Side),
		Entry:       entry,
		Qty:         qty,
		InitialQty:  qty,
		Margin:      margin,
		Leverage:    lev,
		Stop:        stop,
		TP1:         tp1,
		TP2:         tp2,
		TP3:         tp3,
		TrailRef:    entry,
		OpenedAt:    now,
		EntryReason: c.Strat,
	}
	p.positions[raw] = pos
	_ = p.save()
	fmt.Printf("paper entered %s %s entry=%.6f qty=%.6f lev=%dx tp1=%.6f tp2=%.6f tp3=%.6f sl=%.6f fee=%.4f\n",
		raw, c.Side, entry, qty, lev, tp1, tp2, tp3, stop, fee)
	return pos, nil
}

func (p *paperTrader) CheckExit(now time.Time, meta map[string]symbolMeta) {
	if p == nil || !p.enabled || len(p.positions) == 0 {
		return
	}
	defer func() { _ = p.save() }()
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
		pos.LastMark = mark
		updateFavorableRPaper(pos, mark)

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
			if p.beLockBps > 0 {
				pos.Stop = beLockPrice(pos.Side, pos.Entry, p.beLockBps)
			}
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

func (p *paperTrader) ApplyStale(now time.Time, meta map[string]symbolMeta) {
	if p == nil || !p.enabled || !p.staleEnable {
		return
	}
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		mark := meta[raw].LastPrice
		if mark <= 0 {
			continue
		}
		if shouldCloseStalePaper(pos, now, p.staleMaxAge, p.staleMinProg, p.staleGraceR) {
			p.exitPortion(now, pos, "STALE_NO_PROGRESS", mark, pos.Qty)
		}
	}
}

func (p *paperTrader) ForceCloseAll(now time.Time, meta map[string]symbolMeta, reason string) {
	if p == nil || !p.enabled {
		return
	}
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		mark := meta[raw].LastPrice
		if mark <= 0 {
			mark = pos.Entry
		}
		p.exitPortion(now, pos, reason, mark, pos.Qty)
	}
	_ = p.save()
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
	_ = p.save()
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
	dayKey := time.Now().In(p.reportLoc).Format("2006-01-02")
	realizedToday := 0.0
	if ds := p.dayStats[dayKey]; ds != nil {
		realizedToday = ds.Net
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Paper Update (%s)\n", time.Now().In(p.reportLoc).Format("15:04 MST"))
	fmt.Fprintf(&b, "bal=$%.2f eq=$%.2f realized=%+.2f openPnL=%+.2f netDay=%+.2f open=%d/%d\n",
		p.balance, eq, realizedToday, totalUPnL, realizedToday+totalUPnL, len(p.positions), p.maxOpen)
	if len(rows) == 0 {
		b.WriteString("open: none")
		return b.String()
	}
	b.WriteString("sym       side  entry      mark       qty      margin  lev   uPnL    uPnL%\n")
	b.WriteString("--------------------------------------------------------------------------\n")
	for _, r := range rows {
		upct := 0.0
		if r.entry > 0 {
			if strings.EqualFold(r.side, "BUY") {
				upct = ((r.mark - r.entry) / r.entry) * 100
			} else {
				upct = ((r.entry - r.mark) / r.entry) * 100
			}
		}
		margin := 0.0
		lev := 1
		if pos, ok := p.positions[r.sym]; ok && pos != nil {
			margin = pos.Margin
			lev = pos.Leverage
		}
		fmt.Fprintf(&b, "%-8s %-5s %-10s %-10s %-8.4f %-7.2f %-4dx %+.2f  %+.2f%%\n",
			r.sym, r.side, fmtPrice(r.entry), fmtPrice(r.mark), r.qty, margin, lev, r.upnl, upct)
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

func chooseCandidates(longInPlay, shortInPlay []inplay.Entry, minGrade string, enableMomentumReversal bool, reversalMinGrade string, reversalSlopeMin float64) []candidate {
	minVal := gradeValue(minGrade)
	revMinVal := gradeValue(reversalMinGrade)
	if reversalSlopeMin < 0 {
		reversalSlopeMin = -reversalSlopeMin
	}
	out := make([]candidate, 0, len(longInPlay)+len(shortInPlay))
	for _, e := range longInPlay {
		if (e.State == inplay.StatePumping || e.State == inplay.StateInPlay || e.State == inplay.StateHeating) &&
			gradeValue(e.CurrentGrade) >= minVal && e.ScoreSlope > 0 {
			out = append(out, candidate{Entry: e, Side: "BUY"})
		}
		if enableMomentumReversal &&
			gradeValue(e.CurrentGrade) >= revMinVal &&
			e.ScoreSlope <= -reversalSlopeMin &&
			(e.State == inplay.StateDumping || e.State == inplay.StateCooling || e.State == inplay.StateInPlay) {
			flip := e
			flip.Rank = e.Rank + 6*abs(e.ScoreSlope)
			out = append(out, candidate{
				Entry: flip,
				Side:  "SELL",
				Strat: "mom_reversal",
			})
		}
	}
	for _, e := range shortInPlay {
		if (e.State == inplay.StatePumping || e.State == inplay.StateInPlay || e.State == inplay.StateHeating) &&
			gradeValue(e.CurrentGrade) >= minVal && e.ScoreSlope > 0 {
			out = append(out, candidate{Entry: e, Side: "SELL"})
		}
		if enableMomentumReversal &&
			gradeValue(e.CurrentGrade) >= revMinVal &&
			e.ScoreSlope <= -reversalSlopeMin &&
			(e.State == inplay.StateDumping || e.State == inplay.StateCooling || e.State == inplay.StateInPlay) {
			flip := e
			flip.Rank = e.Rank + 6*abs(e.ScoreSlope)
			out = append(out, candidate{
				Entry: flip,
				Side:  "BUY",
				Strat: "mom_reversal",
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entry.Rank > out[j].Entry.Rank })
	return out
}

func rankWithStrategy(c *aster.Client, in []candidate, topN int, stopMode, targetMode string, vpMinTargetPct float64) []candidate {
	if len(in) == 0 {
		return in
	}
	out := make([]candidate, len(in))
	copy(out, in)
	if topN > len(out) {
		topN = len(out)
	}
	for i := 0; i < topN; i++ {
		out[i] = enrichCandidate(c, out[i], stopMode, targetMode, vpMinTargetPct)
	}
	sort.Slice(out, func(i, j int) bool {
		ri := out[i].Entry.Rank * (1 + out[i].Conf)
		rj := out[j].Entry.Rank * (1 + out[j].Conf)
		return ri > rj
	})
	return out
}

func enrichCandidate(c *aster.Client, cand candidate, stopMode, targetMode string, vpMinTargetPct float64) candidate {
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
		MinGrade:                  "B",
		MinScore:                  0,
		MinWhaleDelta:             -1e18,
		AllowWarmup:               true,
		WarmupSlopeMin:            0,
		MaxOne:                    true,
		EnableVPSetups:            true,
		MinVPConfidence:           0.55,
		UseVPReversal:             true,
		EnableInstitutionalPA:     true,
		UseSessionRegimeRisk:      true,
		AllowDeadZoneOnlyAPlus:    true,
		MinConfluenceScore:        0.58,
		RejectIfTargetTooClosePct: vpMinTargetPct,
		RiskPolicy: strategies.RiskPolicyConfig{
			StopMode:             strategies.StopMode(stopMode),
			TargetMode:           strategies.TargetMode(targetMode),
			MinTargetDistancePct: vpMinTargetPct,
		},
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
		if cand.Strat == "mom_reversal" {
			cand.Conf = 0.35 + min(0.25, abs(cand.Entry.ScoreSlope)*0.15)
			cand.Sig = strategies.Signal{
				Active: true,
				Name:   "mom_reversal",
				Side:   toFeatureSide(cand.Side),
			}
			cand.RejectReason = ""
			return cand
		}
		cand.Strat = "none"
		cand.Conf = 0
		cand.RejectReason = "no_strategy_match"
		return cand
	}
	chosen := cs[0].Signal
	targetSide := toFeatureSide(cand.Side)
	for _, x := range cs {
		if x.Signal.Side == targetSide {
			chosen = x.Signal
			break
		}
	}
	cand.Sig = chosen
	cand.Strat = chosen.Name
	cand.Conf = chosen.Confidence
	cand.RejectReason = chosen.RejectReason
	return cand
}

func toFeatureSide(side string) features.Side {
	if strings.EqualFold(strings.TrimSpace(side), "SELL") {
		return features.SideShort
	}
	return features.SideLong
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func printInPlay(tag string, entries []inplay.Entry) {
	fmt.Printf("🔥 IN-PLAY (%s)\n", tag)
	fmt.Println("sym          grade score   slope   state")
	fmt.Println("---------------------------------------------")
	for i := 0; i < len(entries) && i < 5; i++ {
		e := entries[i]
		fmt.Printf("%-12s %-5s %6.2f %7.3f %-8s\n",
			e.Symbol, e.CurrentGrade, e.CurrentScore, e.ScoreSlope, e.State)
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
	fmt.Println("symbol      side   size      entry      mark       uPnL      lev")
	fmt.Println("------------------------------------------------------------------")
	if len(snap.Positions) == 0 {
		fmt.Println("(none)")
		return
	}
	for _, p := range snap.Positions {
		fmt.Printf("%-10s %-6s %-9.4f %-10s %-10s %+.2f    %.0fx\n",
			p.Symbol, p.Side, p.SizeAbs, fmtPrice(p.Entry), fmtPrice(p.Mark), p.Unreal, p.Leverage)
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
	symCoolSec := envInt("LIVE_SYMBOL_COOLDOWN_SEC", coolSec)
	if symCoolSec < 0 {
		symCoolSec = 0
	}
	maxDailyLossPct := envFloat("LIVE_MAX_DAILY_LOSS_PCT", 0)
	if maxDailyLossPct < 0 {
		maxDailyLossPct = 0
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
		symbolCooldown:    time.Duration(symCoolSec) * time.Second,
		pauseFile:         envStr("LIVE_PAUSE_FILE", "/tmp/live-lite.pause"),
		allowSymbols:      allowMap,
		allowShorts:       envBool("LIVE_ALLOW_SHORTS", true),
		maxDailyLossPct:   maxDailyLossPct,
		killClose:         envBool("LIVE_KILL_CLOSE_POSITIONS", false),
	}
}

func safetyReject(cfg safetyConfig, c candidate, now, lastOrderAt time.Time, lastBySymbol map[string]time.Time, byDay map[string]int) string {
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
	if cfg.symbolCooldown > 0 {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
		if t := lastBySymbol[raw]; !t.IsZero() && now.Sub(t) < cfg.symbolCooldown {
			return "symbol cooldown active"
		}
	}
	if cfg.maxOrdersPerDay > 0 {
		dayKey := now.UTC().Format("2006-01-02")
		if byDay[dayKey] >= cfg.maxOrdersPerDay {
			return "max orders/day reached"
		}
	}
	return ""
}

func inEventLockout(now time.Time, lockMin int) bool {
	if lockMin <= 0 {
		return false
	}
	m := now.UTC().Minute()
	return m < lockMin || m >= (60-lockMin)
}

func parseCorrGroups(v string) map[string]string {
	out := map[string]string{}
	v = strings.TrimSpace(v)
	if v == "" {
		return out
	}
	groups := strings.Split(v, ";")
	for gi, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		label := fmt.Sprintf("g%d", gi+1)
		for _, sym := range strings.Split(g, ",") {
			raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(sym)))
			if raw != "" {
				out[raw] = label
			}
		}
	}
	return out
}

func isCorrelatedExposureTooHigh(c candidate, acct accountSnapshot, groups map[string]string, maxExposure float64) bool {
	if maxExposure <= 0 || len(groups) == 0 {
		return false
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	g, ok := groups[raw]
	if !ok || g == "" {
		return false
	}
	exposure := 0.0
	for _, p := range acct.Positions {
		ps := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(p.Symbol)))
		if groups[ps] != g {
			continue
		}
		mark := p.Mark
		if mark <= 0 {
			mark = p.Entry
		}
		exposure += mark * p.SizeAbs
	}
	return exposure >= maxExposure
}

func buildRESTFromConfig() *aster.RESTAuth {
	fileKV := getConfigKV()

	key := cfgGet(fileKV, "ASTER_API_KEY", "aster_api_key", "api_key", "key")
	sec := cfgGet(fileKV, "ASTER_API_SECRET", "aster_api_secret", "api_secret", "secret")
	if key == "" || sec == "" {
		return nil
	}
	// Mainnet by default. Only environment variables can override this at runtime.
	baseURL := effectiveRESTBaseURL()
	rest := aster.NewRESTAuthWithConfig(aster.RESTAuthConfig{
		APIKey:    key,
		APISecret: sec,
		AuthMode:  "hmac",
		BaseURL:   baseURL,
	})
	_ = rest.SyncTime()
	return rest
}

func resolveConfigPath() string {
	if cfgPath := strings.TrimSpace(os.Getenv("ASTER_CONFIG")); cfgPath != "" {
		return cfgPath
	}
	for _, p := range []string{"/etc/aster/.aster.yaml", ".aster.yaml"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		p := filepath.Join(home, ".aster.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func effectiveRESTBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("EXEC_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("ASTER_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "https://fapi.asterdex.com"
	}
	if looksLikeTestnet(baseURL) && !envBool("LIVE_ALLOW_TESTNET", false) {
		fmt.Printf("live-lite: overriding testnet URL %q with mainnet https://fapi.asterdex.com (set LIVE_ALLOW_TESTNET=1 to bypass)\n", baseURL)
		baseURL = "https://fapi.asterdex.com"
	}
	return strings.TrimRight(baseURL, "/")
}

func looksLikeTestnet(u string) bool {
	v := strings.ToLower(strings.TrimSpace(u))
	if v == "" {
		return false
	}
	return strings.Contains(v, "testnet") || strings.Contains(v, "fapi-test") || strings.Contains(v, "demo")
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
	cfgPath := resolveConfigPath()
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

func waitForNextCycle(cycleStart time.Time, scanEvery, reconEvery time.Duration, execMgr *liveExecManager) {
	next := cycleStart.Add(scanEvery)
	for {
		rem := time.Until(next)
		if rem <= 0 {
			return
		}
		sleepFor := rem
		if execMgr != nil && reconEvery > 0 && sleepFor > reconEvery {
			sleepFor = reconEvery
		}
		time.Sleep(sleepFor)
		if execMgr != nil {
			execMgr.Reconcile(time.Now().UTC())
		}
	}
}

func accountEquity(s accountSnapshot) float64 {
	eq := s.AvailableUSDT
	for _, p := range s.Positions {
		eq += p.Unreal
	}
	return eq
}

func shadowReady(days int, equityFile string, now time.Time) bool {
	if days <= 0 {
		return true
	}
	f, err := os.Open(equityFile)
	if err != nil {
		return false
	}
	defer f.Close()
	r := csv.NewReader(f)
	// header
	if _, err := r.Read(); err != nil {
		return false
	}
	var first time.Time
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(row) == 0 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, strings.TrimSpace(row[0]))
		if err != nil {
			continue
		}
		first = ts
		break
	}
	if first.IsZero() {
		return false
	}
	return now.Sub(first) >= time.Duration(days)*24*time.Hour
}

func newLiveLiteStatusStore() *liveLiteStatusStore { return &liveLiteStatusStore{} }

func (s *liveLiteStatusStore) Set(v liveLiteStatus) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cur = v
	s.mu.Unlock()
}

func (s *liveLiteStatusStore) Snapshot() liveLiteStatus {
	if s == nil {
		return liveLiteStatus{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

func startLiveLiteStatusServer(addr string, s *liveLiteStatusStore) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Snapshot())
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		b, _ := json.MarshalIndent(s.Snapshot(), "", "  ")
		_, _ = w.Write(b)
	})
	go func() {
		fmt.Println("live-lite status server:", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Println("live-lite status server error:", err)
		}
	}()
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

func mapInt64(v any) int64 {
	switch x := v.(type) {
	case nil:
		return 0
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case float32:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	default:
		n, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(v)), 10, 64)
		return n
	}
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
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

func clamp(v, lo, hi float64) float64 {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func fmtPrice(v float64) string {
	a := abs(v)
	switch {
	case a >= 1000:
		return fmt.Sprintf("%.2f", v)
	case a >= 1:
		return fmt.Sprintf("%.4f", v)
	default:
		return fmt.Sprintf("%.6f", v)
	}
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
