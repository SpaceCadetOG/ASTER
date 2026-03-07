package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
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
	"go-machine/internal/discovery"
	exitmgr "go-machine/internal/execution"
	"go-machine/internal/features"
	flowfeed "go-machine/internal/flow"
	"go-machine/internal/gate"
	"go-machine/internal/indicators"
	"go-machine/internal/inplay"
	"go-machine/internal/market"
	"go-machine/internal/notify"
	"go-machine/internal/risk"
	"go-machine/internal/stats"
	"go-machine/internal/strategies"
	"go-machine/internal/ta"
	"go-machine/internal/throttle"
	"go-machine/internal/types"
)

type candidate struct {
	Entry        inplay.Entry
	Side         string // BUY/SELL
	Strat        string
	Conf         float64
	VolumeUSD    float64
	Sig          strategies.Signal
	RejectReason string
	LastClose    float64
	SessionVWAP  float64
	EMA9         float64
	FastSlope    float64
	SlowSlope    float64
}

type positionView struct {
	Symbol   string
	Side     string
	Margin   float64
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
	maxOrdersPerHour  int
	orderCooldown     time.Duration
	symbolCooldown    time.Duration
	stopoutWindow     time.Duration
	stopoutLock       time.Duration
	stopoutCount      int
	pauseFile         string
	allowSymbols      map[string]struct{}
	blockSymbols      map[string]struct{}
	allowShorts       bool
	maxDailyLossPct   float64
	killClose         bool
}

type reserveLockGate struct {
	enabled       bool
	lossPct       float64
	recoveryPct   float64
	targetBase    float64
	targetReserve float64
	locked        bool
}

type telegramCommandCtx struct {
	tg      *notify.Telegram
	rest    *aster.RESTAuth
	execMgr *liveExecManager
	paper   *paperTrader
	safety  safetyConfig
	status  *liveLiteStatusStore

	metaMu sync.RWMutex
	meta   map[string]symbolMeta
}

type symbolMeta struct {
	LastPrice   float64
	Move24h     float64
	VolumeUSD   float64
	FundingRate float64
	Bid         float64
	Ask         float64
}

type paperPosition struct {
	Symbol           string
	Side             string
	Entry            float64
	Qty              float64 // remaining qty
	InitialQty       float64
	Margin           float64
	Leverage         int
	Stop             float64
	TP1              float64
	TP2              float64
	TP3              float64
	HitTP1           bool
	HitTP2           bool
	HitTP3           bool
	TrailOn          bool
	TrailStop        float64
	TrailRef         float64
	Realized         float64
	OpenedAt         time.Time
	MaxFavorableR    float64
	MaxAdverseR      float64
	LastMark         float64
	EntryReason      string
	OpposingFriction float64
	StallBars        int
}

type paperTrader struct {
	enabled            bool
	startBal           float64
	balance            float64
	reserve            float64
	feeBps             float64
	makerFeeBps        float64
	takerFeeBps        float64
	stopPct            float64
	tp1R               float64
	tp2R               float64
	tp3R               float64
	tp1Frac            float64
	tp2Frac            float64
	tp3Frac            float64
	trailAfterTP       int
	trailStopPct       float64
	stateFile          string
	tradesCSV          string
	equityCSV          string
	maxOpen            int
	positions          map[string]*paperPosition
	reportLoc          *time.Location
	dayStats           map[string]*paperDayStats
	minStopPct         float64
	maxStopPct         float64
	minTP1RR           float64
	beLockBps          float64
	fundingEvery       time.Duration
	fundingBySym       map[string]time.Duration
	lastFundKey        map[string]string
	openCostMode       string
	onExit             func(string)
	lossCooldown       time.Duration
	lastExitAt         map[string]time.Time
	lastExitLoss       map[string]bool
	lossStreak         map[string]int
	lockUntil          map[string]time.Time
	maxLossStreak      int
	lossLock           time.Duration
	eventLog           *stats.EventLogger
	stressRoundtripBps float64
	exitManager        *exitmgr.Manager
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
	StartBal     float64                   `json:"startBal"`
	Balance      float64                   `json:"balance"`
	Reserve      float64                   `json:"reserve"`
	Positions    map[string]*paperPosition `json:"positions"`
	DayStats     map[string]*paperDayStats `json:"dayStats"`
	LastFund     map[string]string         `json:"lastFund,omitempty"`
	LastExitAt   map[string]time.Time      `json:"lastExitAt,omitempty"`
	LastExitLoss map[string]bool           `json:"lastExitLoss,omitempty"`
	LossStreak   map[string]int            `json:"lossStreak,omitempty"`
	LockUntil    map[string]time.Time      `json:"lockUntil,omitempty"`
	UpdatedAt    time.Time                 `json:"updatedAt"`
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
	Symbol         string    `json:"symbol"`
	Side           string    `json:"side"`
	State          execState `json:"state"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	ClosedAt       time.Time `json:"closedAt,omitempty"`
	CloseReason    string    `json:"closeReason,omitempty"`
	EntryOrderID   int64     `json:"entryOrderId"`
	EntryPrice     float64   `json:"entryPrice"`
	Qty            float64   `json:"qty"`
	FilledQty      float64   `json:"filledQty"`
	RemainingQty   float64   `json:"remainingQty"`
	Margin         float64   `json:"margin"`
	Leverage       int       `json:"leverage"`
	StopPrice      float64   `json:"stopPrice"`
	TP1Price       float64   `json:"tp1Price"`
	TP2Price       float64   `json:"tp2Price"`
	TP3Price       float64   `json:"tp3Price"`
	TP1Qty         float64   `json:"tp1Qty"`
	TP2Qty         float64   `json:"tp2Qty"`
	TP3Qty         float64   `json:"tp3Qty"`
	StopOrderID    int64     `json:"stopOrderId"`
	TP1OrderID     int64     `json:"tp1OrderId"`
	TP2OrderID     int64     `json:"tp2OrderId"`
	TP3OrderID     int64     `json:"tp3OrderId"`
	TrailOn        bool      `json:"trailOn"`
	TrailRef       float64   `json:"trailRef"`
	TrailStop      float64   `json:"trailStop"`
	VPSetup        string    `json:"vpSetup,omitempty"`
	VPLevel        float64   `json:"vpLevel,omitempty"`
	VPTargetLevel  float64   `json:"vpTargetLevel,omitempty"`
	VPStopMode     string    `json:"vpStopMode,omitempty"`
	VPTargetMode   string    `json:"vpTargetMode,omitempty"`
	RejectReason   string    `json:"rejectReason,omitempty"`
	CustomRiskPct  float64   `json:"customRiskPct,omitempty"`
	CustomTP1R     float64   `json:"customTp1R,omitempty"`
	CustomTP2R     float64   `json:"customTp2R,omitempty"`
	EntryReason    string    `json:"entryReason,omitempty"`
	EntryConf      float64   `json:"entryConf,omitempty"`
	EntryTags      []string  `json:"entryTags,omitempty"`
	EntryReasons   []string  `json:"entryReasons,omitempty"`
	EntryVolumeUSD float64   `json:"entryVolumeUsd,omitempty"`
	RegimeTag      string    `json:"regimeTag,omitempty"`
	MaxFavorableR  float64   `json:"maxFavorableR,omitempty"`
	MaxAdverseR    float64   `json:"maxAdverseR,omitempty"`
	StallBars      int       `json:"stallBars,omitempty"`
	LastMark       float64   `json:"lastMark,omitempty"`
	RealizedPnL    float64   `json:"realizedPnl,omitempty"`
}

type liveExecStore struct {
	Positions map[string]*livePosition `json:"positions"`
}

type liveExecManager struct {
	rest                 *aster.RESTAuth
	tg                   *notify.Telegram
	path                 string
	tradesCSV            string
	fillReceipt          bool
	entryTimeout         time.Duration
	stopPct              float64
	tp1R                 float64
	tp2R                 float64
	tp3R                 float64
	tp1Frac              float64
	tp2Frac              float64
	tp3Frac              float64
	trailAfterTP         int
	trailStopPct         float64
	trailStepBps         float64
	minStopPct           float64
	maxStopPct           float64
	minTP1RR             float64
	beLockBps            float64
	marginType           string
	enforceIsolated      bool
	multiAssetMode       bool
	positions            map[string]*livePosition
	dayRealized          map[string]float64
	reportLoc            *time.Location
	eventLog             *stats.EventLogger
	recoverStopRetries   int
	recoverStopBackoff   time.Duration
	recoverATRMult       float64
	recoverForceFlatFail bool
	exitManager          *exitmgr.Manager
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
	PayoutCycleID   string           `json:"payout_cycle_id,omitempty"`
	PayoutNextAt    string           `json:"payout_next_at,omitempty"`
	PayoutLastAmt   float64          `json:"payout_last_amount,omitempty"`
	PayoutLastPnL   float64          `json:"payout_last_profit,omitempty"`
	PayoutLastType  string           `json:"payout_last_action,omitempty"`
	Exec            liveExecSnapshot `json:"exec"`
}

type liveLiteStatusStore struct {
	mu  sync.RWMutex
	cur liveLiteStatus
}

type maintenanceWindow struct {
	Name      string
	StartHour int
	StartMin  int
	EndHour   int
	EndMin    int
	ForceFlat bool
	HookPath  string
	HookTO    time.Duration
}

type maintenanceState struct {
	LastStartDay map[string]string
	LastEndDay   map[string]string
	LastEndAt    map[string]time.Time
	FlatDoneDay  map[string]string
	HookDoneDay  map[string]string
}

type momentumView struct {
	Long  *inplay.Entry
	Short *inplay.Entry
}

type payoutRunState string

const (
	payoutIdle         payoutRunState = "IDLE"
	payoutPendingClose payoutRunState = "PENDING_CLOSE"
	payoutDone         payoutRunState = "DONE"
	payoutDoneFallback payoutRunState = "DONE_FALLBACK"
)

type payoutState struct {
	CycleID         string         `json:"cycleId"`
	CycleStart      time.Time      `json:"cycleStart"`
	CycleEnd        time.Time      `json:"cycleEnd"`
	StartEquity     float64        `json:"startEquity"`
	CycleCloseDate  string         `json:"cycleCloseDate"`
	PendingSince    time.Time      `json:"pendingSince"`
	DeadlineAt      time.Time      `json:"deadlineAt"`
	ActionAt        time.Time      `json:"actionAt"`
	ActionType      string         `json:"actionType"`
	ActionReason    string         `json:"actionReason"`
	LastPayoutAt    time.Time      `json:"lastPayoutAt"`
	LastPayoutAmt   float64        `json:"lastPayoutAmount"`
	LastCycleProfit float64        `json:"lastCycleProfit"`
	TradingBase     float64        `json:"tradingBase"`
	LastAction      string         `json:"lastAction"`
	RunState        payoutRunState `json:"runState"`
}

type payoutManager struct {
	enabled        bool
	mode           string
	onlyIfFlat     bool
	notifyTelegram bool
	cycleDays      int
	anchorHour     int
	anchorMin      int
	deadlineMin    int
	minPayoutUSDT  float64
	keepTradingUSD float64
	stateFile      string
	ledgerFile     string
	loc            *time.Location
	state          payoutState
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
	bNearAOnly := envBool("LIVE_B_NEAR_A_ONLY", true)
	bNearAScoreMin := envFloat("LIVE_B_NEAR_A_SCORE_MIN", 92.0)
	obFilterEnable := envBool("LIVE_OB_FILTER_ENABLE", false)
	obLevels := envInt("LIVE_OB_LEVELS", 5)
	obImbMin := envFloat("LIVE_OB_IMBALANCE_MIN", 1.10)
	obMaxSpreadBps := envFloat("LIVE_OB_MAX_SPREAD_BPS", 10)
	riskShell := risk.DefaultConfig()
	riskShell.Enabled = envBool("LIVE_RISK_SHELL_ENABLE", true)
	riskShell.MinLiqBufferMult = envFloat("LIVE_MIN_LIQ_BUFFER_MULT", 2.5)
	riskShell.MaxFundingCostR = envFloat("LIVE_MAX_FUNDING_COST_R", 0.20)
	riskShell.MaxSpreadBps = envFloat("LIVE_MAX_SPREAD_BPS", obMaxSpreadBps)
	riskShell.MinBookImbalance = envFloat("LIVE_MIN_BOOK_IMBALANCE", obImbMin)
	riskShell.MaxRecentSlippageBps = envFloat("LIVE_MAX_RECENT_SLIPPAGE_BPS", 15)
	riskHoldHours := envFloat("LIVE_EXPECTED_HOLD_HOURS", 8.0)
	riskFallbackStopPct := envFloat("LIVE_STOP_PCT", 2.0)
	entryBps := envFloat("LIVE_ENTRY_OFFSET_BPS", 2)
	showAccount := envBool("LIVE_SHOW_ACCOUNT", true)
	accountAssets := envCSV("LIVE_ACCOUNT_ASSETS", "")
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
			tg.Sendf("%s", notify.BuildEventHTML("🧩", "BOOT RECONCILE COMPLETE",
				fmt.Sprintf("<b>Closed local:</b> %d", nClosed),
				fmt.Sprintf("<b>Imported remote:</b> %d", nImported),
			))
		}
	}
	statusStore := newLiveLiteStatusStore()
	statusAddr := envStr("LIVE_STATUS_ADDR", ":8787")
	startLiveLiteStatusServer(statusAddr, statusStore)
	var cmdCtx *telegramCommandCtx
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
	eodReportHour := envInt("LIVE_TG_EOD_REPORT_HOUR", 16)
	eodReportMinute := envInt("LIVE_TG_EOD_REPORT_MIN", 0)
	sodReportHour := envInt("LIVE_TG_SOD_REPORT_HOUR", 18)
	sodReportMinute := envInt("LIVE_TG_SOD_REPORT_MIN", 0)
	preUSReportHour := envInt("LIVE_TG_PRE_US_REPORT_HOUR", 8)
	preUSReportMinute := envInt("LIVE_TG_PRE_US_REPORT_MIN", 0)
	reportDayOffset := envInt("LIVE_TG_DAILY_REPORT_DAY_OFFSET", 0)
	receiptEnable := envBool("LIVE_TG_DAILY_RECEIPT_ENABLE", true)
	receiptLimit := envInt("LIVE_TG_DAILY_RECEIPT_LIMIT", 25)
	if receiptLimit <= 0 {
		receiptLimit = 25
	}
	liveReceiptEnable := envBool("LIVE_TG_DAILY_LIVE_RECEIPT_ENABLE", true)
	liveReceiptLimit := envInt("LIVE_TG_DAILY_LIVE_RECEIPT_LIMIT", 25)
	if liveReceiptLimit <= 0 {
		liveReceiptLimit = 25
	}
	if eodReportHour < 0 || eodReportHour > 23 {
		eodReportHour = 16
	}
	if eodReportMinute < 0 || eodReportMinute > 59 {
		eodReportMinute = 0
	}
	if sodReportHour < 0 || sodReportHour > 23 {
		sodReportHour = 18
	}
	if sodReportMinute < 0 || sodReportMinute > 59 {
		sodReportMinute = 0
	}
	if preUSReportHour < 0 || preUSReportHour > 23 {
		preUSReportHour = 8
	}
	if preUSReportMinute < 0 || preUSReportMinute > 59 {
		preUSReportMinute = 0
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
	maintWarmup := time.Duration(envInt("LIVE_MAINT_WARMUP_MIN", 0)) * time.Minute
	preEODExitEnable := envBool("LIVE_PRE_EOD_EXIT_ENABLE", true)
	preEODEndHour := envInt("LIVE_PRE_EOD_EXIT_END_HOUR", 16)
	preEODEndMin := envInt("LIVE_PRE_EOD_EXIT_END_MIN", 0)
	preEODMinHold := time.Duration(envInt("LIVE_PRE_EOD_EXIT_MIN_HOLD_MIN", 0)) * time.Minute
	preEODUpnlPctMax := envFloat("LIVE_PRE_EOD_EXIT_UPNL_PCT_MAX", 0.30)
	preEODEntryBlockMin := envInt("LIVE_PRE_EOD_ENTRY_BLOCK_MIN", 60)
	postSLCooldown := time.Duration(envInt("POST_SL_COOLDOWN_MIN", 30)) * time.Minute
	allowDeadSessionTrading := envBool("ALLOW_DEAD_SESSION_TRADING", false)
	inertiaEnable := envBool("LIVE_INERTIA_BREAKER_ENABLE", true)
	inertiaScoreMin := envFloat("LIVE_INERTIA_SCORE_MIN", 80)
	inertiaSlowMin := envFloat("LIVE_INERTIA_SLOW_SLOPE_MIN", 0.5)
	inertiaFastMax := envFloat("LIVE_INERTIA_FAST_SLOPE_MAX", -1.0)
	inertiaSlowN := envInt("LIVE_INERTIA_SLOW_N", 15)
	inertiaFastN := envInt("LIVE_INERTIA_FAST_N", 3)
	reversalTopLongN := envInt("LIVE_REVERSAL_TOP_LONG_N", 5)
	reversalVolSpike := envFloat("LIVE_REVERSAL_VOL_SPIKE_MIN", 3.0)
	flowFeedPath := envStr("LIVE_FLOW_FEED_FILE", "")
	flowFeedTTL := time.Duration(envInt("LIVE_FLOW_FEED_TTL_SEC", 300)) * time.Second
	if postSLCooldown < 0 {
		postSLCooldown = 0
	}
	if preEODEntryBlockMin < 0 {
		preEODEntryBlockMin = 0
	}
	if inertiaSlowN < 2 {
		inertiaSlowN = 15
	}
	if inertiaFastN < 2 {
		inertiaFastN = 3
	}
	if reversalTopLongN <= 0 {
		reversalTopLongN = 5
	}
	if reversalVolSpike <= 0 {
		reversalVolSpike = 3.0
	}
	maintMidnight := maintenanceWindow{
		Name:      "M1",
		StartHour: envInt("LIVE_MAINT1_START_HOUR", 0),
		StartMin:  envInt("LIVE_MAINT1_START_MIN", 0),
		EndHour:   envInt("LIVE_MAINT1_END_HOUR", 1),
		EndMin:    envInt("LIVE_MAINT1_END_MIN", 30),
		ForceFlat: envBool("LIVE_MAINT1_FORCE_FLAT", false),
		HookPath:  envStr("LIVE_MAINT1_HOOK", ""),
		HookTO:    time.Duration(envInt("LIVE_MAINT1_HOOK_TIMEOUT_SEC", 900)) * time.Second,
	}
	maintEOD := maintenanceWindow{
		Name:      "M2",
		StartHour: envInt("LIVE_MAINT2_START_HOUR", 16),
		StartMin:  envInt("LIVE_MAINT2_START_MIN", 0),
		EndHour:   envInt("LIVE_MAINT2_END_HOUR", 18),
		EndMin:    envInt("LIVE_MAINT2_END_MIN", 0),
		ForceFlat: envBool("LIVE_MAINT2_FORCE_FLAT", true),
		HookPath:  envStr("LIVE_MAINT2_HOOK", ""),
		HookTO:    time.Duration(envInt("LIVE_MAINT2_HOOK_TIMEOUT_SEC", 900)) * time.Second,
	}
	maintMidnight = normalizeMaintenanceWindow(maintMidnight)
	maintEOD = normalizeMaintenanceWindow(maintEOD)
	nextDigestAt := time.Now().UTC().Add(10 * time.Second)
	nextTradeUpdateAt := time.Now().UTC().Add(45 * time.Second)
	lastDailyReportDay := ""
	lastDailyLiveReceiptDay := ""
	lastSODReportDay := ""
	lastPreUSReportDay := ""
	lastM1ReportDay := ""
	lastM2ReportDay := ""
	lastPreEODDecisionDay := ""
	lastHourlyKey := ""
	var lastPulseSentAt time.Time
	maintState := maintenanceState{
		LastStartDay: map[string]string{},
		LastEndDay:   map[string]string{},
		LastEndAt:    map[string]time.Time{},
		FlatDoneDay:  map[string]string{},
		HookDoneDay:  map[string]string{},
	}
	asiaMinGrade := envStr("LIVE_ASIA_MIN_GRADE", "A")
	asiaStrongConfMin := envFloat("LIVE_ASIA_STRONG_CONF_MIN", 0.72)
	paper := newPaperTrader(dryRun, reserveUSDT, maxOpenPos)
	if paper != nil && tg != nil && tg.Enabled() {
		paper.onExit = func(msg string) {
			tg.Sendf("%s", msg)
		}
	}
	cmdCtx = &telegramCommandCtx{
		tg:      tg,
		rest:    rest,
		execMgr: execMgr,
		paper:   paper,
		safety:  safety,
		status:  statusStore,
		meta:    map[string]symbolMeta{},
	}
	if envBool("LIVE_TG_COMMANDS_ENABLE", true) {
		go cmdCtx.run()
	}
	payoutMgr := newPayoutManager()
	if paper != nil || execMgr != nil || payoutMgr != nil {
		paperPath := ""
		livePath := ""
		payoutPath := ""
		if paper != nil {
			paperPath = paper.stateFile
		}
		if execMgr != nil {
			livePath = execMgr.path
		}
		if payoutMgr != nil {
			payoutPath = payoutMgr.stateFile
		}
		fmt.Printf("state paths: paper=%s (exists=%v) live=%s (exists=%v) payout=%s (exists=%v)\n",
			paperPath, fileExists(paperPath), livePath, fileExists(livePath), payoutPath, fileExists(payoutPath))
	}

	pureMode := envBool("LIVE_PURE_MODE", true)
	fmt.Printf("live-lite started (scan=%s dry_run=%v min_grade=%s reserve=%s fixed=%.2f margin=%s lev_mode=%s)\n",
		scanEvery, dryRun, strings.ToUpper(minGrade),
		reserveMode, reserveUSDT, fmt.Sprintf("%s(%.2f)", tradeMarginMode, tradeMargin), leverageMode)
	if cfgPath == "" {
		cfgPath = "(none)"
	}
	fmt.Printf("live-lite config: ASTER_CONFIG=%s REST_BASE_URL=%s\n", cfgPath, effectiveRESTBaseURL())
	fmt.Printf("safety: live_enabled=%v max_lev=%d min_avail=%.2f max_orders_day=%d max_orders_hour=%d cooldown=%s allow_shorts=%v pause_file=%s stopout=%d/%s lock=%s\n",
		safety.enableLiveTrading, safety.maxLeverage, safety.minAvailUSDT, safety.maxOrdersPerDay, safety.maxOrdersPerHour, safety.orderCooldown, safety.allowShorts, safety.pauseFile, safety.stopoutCount, safety.stopoutWindow, safety.stopoutLock)
	fmt.Printf("mode: pure_mode=%v\n", pureMode)
	if execMgr != nil {
		fmt.Printf("execution: margin_type=%s enforce_isolated=%v multi_asset_mode=%v\n",
			execMgr.marginType, execMgr.enforceIsolated, execMgr.multiAssetMode)
	}
	if payoutMgr != nil && payoutMgr.enabled {
		fmt.Printf("payout: mode=%s cycle=%dd anchor=%02d:%02d deadline=%dm tz=%s\n",
			payoutMgr.mode, payoutMgr.cycleDays, payoutMgr.anchorHour, payoutMgr.anchorMin, payoutMgr.deadlineMin, payoutMgr.loc.String())
	}
	tg.Sendf("%s", notify.BuildEventHTML("🚀", "LIVE-LITE STARTED",
		fmt.Sprintf("<b>Scan:</b> %s", scanEvery),
		fmt.Sprintf("<b>Dry Run:</b> %v", dryRun),
		fmt.Sprintf("<b>Min Grade:</b> %s", strings.ToUpper(minGrade)),
		fmt.Sprintf("<b>Digest:</b> %s", digestEvery),
		fmt.Sprintf("<b>Maint M1:</b> %02d:%02d-%02d:%02d", maintMidnight.StartHour, maintMidnight.StartMin, maintMidnight.EndHour, maintMidnight.EndMin),
		fmt.Sprintf("<b>Maint M2:</b> %02d:%02d-%02d:%02d", maintEOD.StartHour, maintEOD.StartMin, maintEOD.EndHour, maintEOD.EndMin),
	))

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
	orderCountByHour := map[string]int{}
	symbolStopoutLockUntil := map[string]time.Time{}
	dayStartEq := map[string]float64{}
	killDay := map[string]bool{}
	reserveGate := newReserveLockGate()
	discoveryCfg := loadDiscoveryConfig()
	gateCfg := loadEntryGateConfig()
	symbolCooldown := throttle.NewCooldown(time.Duration(envInt("LIVE_THROTTLE_SYMBOL_COOLDOWN_SECONDS", 300)) * time.Second)
	intentDedupe := throttle.NewDedupe(time.Duration(envInt("LIVE_THROTTLE_DEDUPE_WINDOW_SECONDS", 120)) * time.Second)
	eventLog := stats.NewEventLogger(
		envStr("LIVE_EVENTS_LOG", "logs/events.jsonl"),
		envBool("LIVE_EVENTS_ENABLE", true),
		envBool("LIVE_EVENTS_STDOUT", false),
		dryRun,
	)
	if pureMode {
		discoveryCfg.Enabled = false
	}
	if paper != nil {
		paper.eventLog = eventLog
	}
	if execMgr != nil {
		execMgr.eventLog = eventLog
	}
	externalFlowFeed := flowfeed.NewFileFeed(flowFeedPath, flowFeedTTL)
	for {
		cycleStart := time.Now()
		now := cycleStart.UTC()
		localMaintNow := now.In(maintLoc)
		maintWindow, inMaint := activeMaintenanceWindow(localMaintNow, maintEnabled, maintMidnight, maintEOD)
		mkts := client.FetchAllMarkets()
		longRows := market.ScoreAndFilter(mkts)
		shortRows := market.ScoreAndFilterShort(mkts)
		longRows = filterBlockedScored(longRows, safety.blockSymbols)
		shortRows = filterBlockedScored(shortRows, safety.blockSymbols)
		if discoveryCfg.Enabled {
			baseLongRows := append([]market.Scored(nil), longRows...)
			baseShortRows := append([]market.Scored(nil), shortRows...)
			candleBySymbol := buildDiscoveryCandles(client, longRows, shortRows, discoveryCfg)
			snaps := discovery.BuildSnapshots(longRows, candleBySymbol)
			universe := discovery.SelectUniverse(snaps, discoveryCfg)
			if len(universe) > 0 {
				longRows = filterUniverseRows(longRows, universe)
				shortRows = filterUniverseRows(shortRows, universe)
				if len(longRows) == 0 && len(shortRows) == 0 {
					// Discovery produced an empty active universe after filtering; fallback to full scanner rows.
					fmt.Println("live-lite: discovery_fallback_active reason=filtered_universe_empty")
					eventLog.Emit(stats.Event{
						Timestamp: now,
						Type:      "METRICS_SNAPSHOT",
						Reason:    "discovery_fallback_active:filtered_universe_empty",
					})
					longRows = baseLongRows
					shortRows = baseShortRows
				}
			} else {
				// Empty discovery result should never hard-stop trading.
				fmt.Println("live-lite: discovery_fallback_active reason=empty_universe")
				eventLog.Emit(stats.Event{
					Timestamp: now,
					Type:      "METRICS_SNAPSHOT",
					Reason:    "discovery_fallback_active:empty_universe",
				})
				longRows = baseLongRows
				shortRows = baseShortRows
			}
		}
		metaBySymbol := buildSymbolMeta(longRows, shortRows)
		cmdCtx.setMeta(metaBySymbol)
		paperDepth := map[string]aster.OrderBook{}
		if paper.enabled {
			paperDepth = fetchOrderBooks(client, paper.OpenSymbols(), envInt("LIVE_PAPER_OB_LEVELS", 20))
			mergeTopOfBookIntoMeta(metaBySymbol, paperDepth)
			paper.ApplyFunding(now, metaBySymbol)
		}
		if paper.enabled {
			paper.CheckExit(now, metaBySymbol, paperDepth)
		}
		if paper.enabled && tg != nil && tg.Enabled() {
			if !hourlyEnable && now.After(nextTradeUpdateAt) {
				if msg := paper.TradeUpdateMessage(metaBySymbol, tradeUpdateTop); msg != "" {
					tg.Sendf("%s", tgPre(msg))
				}
				nextTradeUpdateAt = now.Add(tradeUpdateEvery)
			}
			localNow := now.In(paper.reportLoc)
			if localNow.Hour() > eodReportHour || (localNow.Hour() == eodReportHour && localNow.Minute() >= eodReportMinute) {
				dayKey := localNow.AddDate(0, 0, reportDayOffset).Format("2006-01-02")
				if dayKey != lastDailyReportDay {
					tg.Sendf("%s", notify.BuildEventHTML("🧾", "EOD DAILY DISPATCH",
						fmt.Sprintf("<b>Day:</b> %s", dayKey),
						fmt.Sprintf("<b>Time:</b> %s", localNow.Format("15:04 MST")),
					))
					if msg, ok := paper.DailyReportMessage(dayKey); ok {
						tg.Sendf("%s", msg)
					}
					if receiptEnable {
						if rmsg, ok := paper.DailyReceiptMessage(dayKey, receiptLimit); ok {
							tg.Sendf("%s", tgPre(rmsg))
						}
					}
					lastDailyReportDay = dayKey
				}
			}
			if localNow.Hour() > sodReportHour || (localNow.Hour() == sodReportHour && localNow.Minute() >= sodReportMinute) {
				dayKey := localNow.Format("2006-01-02")
				if dayKey != lastSODReportDay {
					if shouldSendPulse(now, lastPulseSentAt, 10*time.Minute) {
						tg.Sendf("%s", buildSODReport(localNow, paper, metaBySymbol))
						lastPulseSentAt = now
						lastSODReportDay = dayKey
					}
				}
			}
		}
		if tg != nil && tg.Enabled() && execMgr != nil && liveReceiptEnable {
			localNow := now.In(execMgr.reportLoc)
			if localNow.Hour() > eodReportHour || (localNow.Hour() == eodReportHour && localNow.Minute() >= eodReportMinute) {
				dayKey := localNow.AddDate(0, 0, reportDayOffset).Format("2006-01-02")
				if dayKey != lastDailyLiveReceiptDay {
					if msg, ok := execMgr.DailyReceiptMessage(dayKey, liveReceiptLimit); ok {
						tg.Sendf("%s", tgPre(msg))
					}
					lastDailyLiveReceiptDay = dayKey
				}
			}
		}

		longEligible, longConf := buildEligible(client, longRows, "long", gradeTopN)
		shortEligible, shortConf := buildEligible(client, shortRows, "short", gradeTopN)

		longTrk.Update(now, longEligible, longConf)
		shortTrk.Update(now, shortEligible, shortConf)

		longInPlay := longTrk.Entries()
		shortInPlay := shortTrk.Entries()
		momBySymbol := buildMomentumIndex(longInPlay, shortInPlay)
		externalFlow := externalFlowFeed.Snapshot(now)
		if paper.enabled {
			paper.ApplyMomentumExit(now, momBySymbol, metaBySymbol, paperDepth, externalFlow)
		}
		if execMgr != nil {
			execMgr.ApplyMomentumExit(now, momBySymbol, externalFlow)
		}
		if preEODExitEnable {
			dayKey := localMaintNow.Format("2006-01-02")
			decisionNow := localMaintNow.Hour() == preEODEndHour && localMaintNow.Minute() == preEODEndMin
			if decisionNow && lastPreEODDecisionDay != dayKey {
				if paper.enabled {
					paper.ApplyPreEODExit(now, momBySymbol, metaBySymbol, paperDepth, preEODMinHold, preEODUpnlPctMax)
				}
				if execMgr != nil {
					execMgr.ApplyPreEODExit(now, momBySymbol, preEODMinHold, preEODUpnlPctMax)
				}
				lastPreEODDecisionDay = dayKey
			}
		}
		printInPlay("LONG", longInPlay)
		printInPlay("SHORT", shortInPlay)
		eventLog.Emit(stats.Event{
			Timestamp: now,
			Type:      "METRICS_SNAPSHOT",
			TF:        "1m",
			Reason:    fmt.Sprintf("long_inplay=%d short_inplay=%d", len(longInPlay), len(shortInPlay)),
		})
		if paper.enabled && tg != nil && tg.Enabled() && hourlyEnable {
			hk := localMaintNow.Format("2006-01-02 15")
			if localMaintNow.Minute() == 0 && hk != lastHourlyKey {
				if shouldSendPulse(now, lastPulseSentAt, 10*time.Minute) {
					tg.Sendf("%s", buildHourlyDigest(localMaintNow, paper, metaBySymbol, longInPlay, shortInPlay, digestLimit))
					lastPulseSentAt = now
					lastHourlyKey = hk
				}
			}
			if localMaintNow.Hour() > preUSReportHour || (localMaintNow.Hour() == preUSReportHour && localMaintNow.Minute() >= preUSReportMinute) {
				dayKey := localMaintNow.Format("2006-01-02")
				if dayKey != lastPreUSReportDay {
					if shouldSendPulse(now, lastPulseSentAt, 10*time.Minute) {
						tg.Sendf("%s", buildWindowReport("Pre-US Report", localMaintNow, paper, metaBySymbol, longInPlay, shortInPlay, digestLimit))
						lastPulseSentAt = now
						lastPreUSReportDay = dayKey
					}
				}
			}
		}
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
				realizedToday := 0.0
				if execMgr != nil {
					realizedToday = execMgr.dayRealizedAt(now)
				}
				printAccountSnapshot(snap, accountAssets, realizedToday)
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
						tg.Sendf("%s", notify.BuildEventHTML("🛑", "KILL SWITCH",
							fmt.Sprintf("<b>Equity:</b> %.4f", eq),
							fmt.Sprintf("<b>Start:</b> %.4f | <b>Limit:</b> %.4f", dayStartEq[dayKey], minEq),
						))
					}
				}
			}
		}
		if execMgr != nil {
			execMgr.Reconcile(now)
		}
		if inMaint {
			dayKey := localMaintNow.Format("2006-01-02")
			if maintState.LastStartDay[maintWindow.Name] != dayKey {
				maintState.LastStartDay[maintWindow.Name] = dayKey
				tg.Sendf("%s", notify.BuildEventHTML("🛠️", "MAINTENANCE START",
					fmt.Sprintf("<b>Window:</b> %s", maintWindow.Name),
					fmt.Sprintf("<b>Range:</b> %02d:%02d-%02d:%02d %s",
						maintWindow.StartHour, maintWindow.StartMin, maintWindow.EndHour, maintWindow.EndMin, maintLoc.String()),
					fmt.Sprintf("<b>Session:</b> %s", sessionTag(localMaintNow)),
				))
				if paper.enabled && tg != nil && tg.Enabled() {
					switch maintWindow.Name {
					case "M1":
						if lastM1ReportDay != dayKey {
							tg.Sendf("%s", buildWindowReport("M1 Report", localMaintNow, paper, metaBySymbol, longInPlay, shortInPlay, digestLimit))
							lastM1ReportDay = dayKey
						}
					case "M2":
						if lastM2ReportDay != dayKey {
							tg.Sendf("%s", buildWindowReport("EOD/M2 Report", localMaintNow, paper, metaBySymbol, longInPlay, shortInPlay, digestLimit))
							lastM2ReportDay = dayKey
						}
					}
				}
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
					paper.ForceCloseAll(now, metaBySymbol, paperDepth, "EOD_FORCE_FLAT")
					_ = paper.save()
				}
				tg.Sendf("%s", notify.BuildEventHTML("✅", "MAINTENANCE FLAT COMPLETE",
					fmt.Sprintf("<b>Window:</b> %s", maintWindow.Name),
				))
			}
			if maintWindow.HookPath != "" && maintState.HookDoneDay[maintWindow.Name] != dayKey {
				maintState.HookDoneDay[maintWindow.Name] = dayKey
				if err := runMaintenanceHook(maintWindow.HookPath, maintWindow.HookTO); err != nil {
					tg.Sendf("%s", notify.BuildEventHTML("❌", "MAINTENANCE HOOK ERROR",
						fmt.Sprintf("<b>Window:</b> %s", maintWindow.Name),
						fmt.Sprintf("<b>Error:</b> %v", err),
					))
				} else {
					tg.Sendf("%s", notify.BuildEventHTML("✅", "MAINTENANCE HOOK COMPLETE",
						fmt.Sprintf("<b>Window:</b> %s", maintWindow.Name),
					))
				}
			}
		} else {
			for _, w := range []maintenanceWindow{maintMidnight, maintEOD} {
				dayKey := localMaintNow.Format("2006-01-02")
				if maintState.LastStartDay[w.Name] == dayKey && maintState.LastEndDay[w.Name] != dayKey {
					maintState.LastEndDay[w.Name] = dayKey
					maintState.LastEndAt[w.Name] = localMaintNow
					tg.Sendf("%s", notify.BuildEventHTML("🟢", "MAINTENANCE END",
						fmt.Sprintf("<b>Window:</b> %s", w.Name),
						"<b>Trading:</b> resumed",
						fmt.Sprintf("<b>Session:</b> %s", sessionTag(localMaintNow)),
					))
				}
			}
		}
		if payoutMgr != nil && payoutMgr.enabled {
			payoutMgr.maybeRun(now, localMaintNow, maintEOD, &maintState, paper, metaBySymbol, acct, execMgr, tg)
		}
		if paper.enabled {
			fmt.Println(paper.Summary(metaBySymbol))
			fmt.Println(paper.PositionsTable(metaBySymbol))
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

		cands := chooseCandidates(longInPlay, shortInPlay, minGrade, enableMomentumReversal, reversalMinGrade, reversalSlopeMin, bNearAOnly, bNearAScoreMin, reversalTopLongN)
		cands = rankWithStrategy(client, cands, strategyTopN, stopMode, targetMode, vpMinTargetPct, inertiaEnable, inertiaScoreMin, inertiaSlowMin, inertiaFastMax, inertiaSlowN, inertiaFastN, reversalVolSpike)
		filtered := make([]candidate, 0, len(cands))
		for _, c := range cands {
			eventLog.Emit(stats.Event{
				Timestamp: now,
				Type:      "SIGNAL",
				Symbol:    strings.ToUpper(aster.RawSymbol(c.Entry.Symbol)),
				Side:      c.Side,
				TF:        "1m",
				Strategy:  c.Strat,
				Score:     c.Entry.CurrentScore,
				Slope:     c.Entry.ScoreSlope,
			})
			if c.RejectReason == "STATE_INERTIA_KILL" || c.RejectReason == "VWAP_EMA_LONG_INVALIDATION" {
				f := false
				eventLog.Emit(stats.Event{
					Timestamp:   now,
					Type:        "GATE_DECISION",
					Symbol:      strings.ToUpper(aster.RawSymbol(c.Entry.Symbol)),
					Side:        c.Side,
					Strategy:    c.Strat,
					Score:       c.Entry.CurrentScore,
					Slope:       c.Entry.ScoreSlope,
					GateAllow:   &f,
					GateReasons: []string{c.RejectReason},
				})
				continue
			}
			if !pureMode && !passesAsiaEntryQuality(now, c, asiaMinGrade, asiaStrongConfMin) {
				deny := []string{"asia_quality_gate"}
				f := false
				eventLog.Emit(stats.Event{
					Timestamp:   now,
					Type:        "GATE_DECISION",
					Symbol:      strings.ToUpper(aster.RawSymbol(c.Entry.Symbol)),
					Side:        c.Side,
					Strategy:    c.Strat,
					Score:       c.Entry.CurrentScore,
					Slope:       c.Entry.ScoreSlope,
					GateAllow:   &f,
					GateReasons: deny,
				})
				continue
			}
			if !pureMode {
				gateInput := buildGateInput(client, c, gateCfg)
				dec := gate.Evaluate(gateInput, gateCfg)
				eventLog.Emit(stats.Event{
					Timestamp:   now,
					Type:        "GATE_DECISION",
					Symbol:      strings.ToUpper(aster.RawSymbol(c.Entry.Symbol)),
					Side:        c.Side,
					Strategy:    c.Strat,
					Score:       c.Entry.CurrentScore,
					Slope:       c.Entry.ScoreSlope,
					VolumeRatio: gateInput.VolumeRatio,
					GateAllow:   &dec.Allow,
					GateReasons: dec.Reasons,
				})
				if !dec.Allow {
					continue
				}
			}
			filtered = append(filtered, c)
		}
		cands = filtered
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
		if payoutMgr != nil && payoutMgr.enabled {
			ps := payoutMgr.state
			st.PayoutCycleID = ps.CycleID
			if !ps.CycleEnd.IsZero() {
				st.PayoutNextAt = ps.CycleEnd.In(payoutMgr.loc).Format(time.RFC3339)
			}
			st.PayoutLastAmt = ps.LastPayoutAmt
			st.PayoutLastPnL = ps.LastCycleProfit
			st.PayoutLastType = ps.LastAction
		}
		if len(cands) == 0 {
			statusStore.Set(st)
			if data.CurrentRegimeCT(now) == data.RegimeAsia {
				fmt.Println("live-lite: no trade candidate (asia quality gate)")
			} else {
				fmt.Println("live-lite: no trade candidate")
			}
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
			tg.Sendf("%s", notify.BuildEventHTML("🎯", "TOP CANDIDATE",
				fmt.Sprintf("<b>%s %s</b>", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side),
				fmt.Sprintf("<b>Grade:</b> %s | <b>Score:</b> %.2f", best.Entry.CurrentGrade, best.Entry.CurrentScore),
				fmt.Sprintf("<b>Slope:</b> %+.3f | <b>Rank:</b> %.2f", best.Entry.ScoreSlope, best.Entry.Rank),
				fmt.Sprintf("<b>Setup:</b> <code>%s</code> | <b>Conf:</b> %.2f", best.Strat, best.Conf),
			))
			lastTopKey = topKey
		}
		rawBest := strings.ToUpper(aster.RawSymbol(best.Entry.Symbol))
		best.VolumeUSD = metaBySymbol[rawBest].VolumeUSD
		if sig, ok := externalFlow[rawBest]; ok {
			if sig.LiqSpike {
				if (strings.EqualFold(best.Side, "BUY") && sig.FlowDelta < 0) || (strings.EqualFold(best.Side, "SELL") && sig.FlowDelta > 0) {
					st.TopRejectReason = "external_liq_flow_against"
					statusStore.Set(st)
					eventLog.Emit(stats.Event{
						Timestamp: now,
						Type:      "GATE_DECISION",
						Symbol:    rawBest,
						Side:      best.Side,
						Strategy:  best.Strat,
						Score:     best.Entry.CurrentScore,
						Slope:     best.Entry.ScoreSlope,
						GateAllow: boolPtr(false),
						GateReasons: []string{
							"external_liq_flow_against",
						},
					})
					waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
					continue
				}
			}
		}
		if postSLCooldown > 0 && hasRecentStopLoss(rawBest, best.Side, now, postSLCooldown, paper, execMgr) {
			st.TopRejectReason = "POST_SL_COOLDOWN"
			statusStore.Set(st)
			eventLog.Emit(stats.Event{
				Timestamp: now,
				Type:      "GATE_DECISION",
				Symbol:    rawBest,
				Side:      best.Side,
				Strategy:  best.Strat,
				Score:     best.Entry.CurrentScore,
				Slope:     best.Entry.ScoreSlope,
				GateAllow: boolPtr(false),
				GateReasons: []string{
					"POST_SL_COOLDOWN",
				},
			})
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if !allowDeadSessionTrading && data.CurrentRegimeCT(now) == data.RegimeDead {
			st.TopRejectReason = "DEAD_SESSION_BLOCK"
			statusStore.Set(st)
			eventLog.Emit(stats.Event{
				Timestamp: now,
				Type:      "GATE_DECISION",
				Symbol:    rawBest,
				Side:      best.Side,
				Strategy:  best.Strat,
				Score:     best.Entry.CurrentScore,
				Slope:     best.Entry.ScoreSlope,
				GateAllow: boolPtr(false),
				GateReasons: []string{
					"DEAD_SESSION_BLOCK",
				},
			})
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if preEODEntryBlockMin > 0 && inPreEODEntryBlock(localMaintNow, maintEOD, preEODEntryBlockMin) {
			st.TopRejectReason = "PRE_EOD_ENTRY_BLOCK"
			statusStore.Set(st)
			eventLog.Emit(stats.Event{
				Timestamp: now,
				Type:      "GATE_DECISION",
				Symbol:    rawBest,
				Side:      best.Side,
				Strategy:  best.Strat,
				Score:     best.Entry.CurrentScore,
				Slope:     best.Entry.ScoreSlope,
				GateAllow: boolPtr(false),
				GateReasons: []string{
					"PRE_EOD_ENTRY_BLOCK",
				},
			})
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if !pureMode && !symbolCooldown.Allow(rawBest, now) {
			st.TopRejectReason = "symbol_cooldown"
			statusStore.Set(st)
			eventLog.Emit(stats.Event{
				Timestamp: now,
				Type:      "GATE_DECISION",
				Symbol:    rawBest,
				Side:      best.Side,
				Strategy:  best.Strat,
				Score:     best.Entry.CurrentScore,
				Slope:     best.Entry.ScoreSlope,
				GateAllow: boolPtr(false),
				GateReasons: []string{
					"throttle_symbol_cooldown",
				},
			})
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if !pureMode && !intentDedupe.Allow(rawBest, best.Side, now) {
			st.TopRejectReason = "intent_dedupe"
			statusStore.Set(st)
			eventLog.Emit(stats.Event{
				Timestamp: now,
				Type:      "GATE_DECISION",
				Symbol:    rawBest,
				Side:      best.Side,
				Strategy:  best.Strat,
				Score:     best.Entry.CurrentScore,
				Slope:     best.Entry.ScoreSlope,
				GateAllow: boolPtr(false),
				GateReasons: []string{
					"throttle_dedupe",
				},
			})
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		entryDepth := map[string]aster.OrderBook{}
		if obFilterEnable {
			entryDepth = fetchOrderBooks(client, []string{rawBest}, obLevels)
			ob := entryDepth[rawBest]
			okOB, obReason, obSpreadBps, obImb := orderbookEntryDecision(ob, best.Side, obLevels, obImbMin, obMaxSpreadBps)
			if !okOB {
				st.TopRejectReason = obReason
				statusStore.Set(st)
				eventLog.Emit(stats.Event{
					Timestamp: now,
					Type:      "GATE_DECISION",
					Symbol:    rawBest,
					Side:      best.Side,
					Strategy:  best.Strat,
					Score:     best.Entry.CurrentScore,
					Slope:     best.Entry.ScoreSlope,
					GateAllow: boolPtr(false),
					GateReasons: []string{
						obReason,
						fmt.Sprintf("spread_bps=%.2f", obSpreadBps),
						fmt.Sprintf("imb=%.3f", obImb),
					},
				})
				fmt.Printf("live-lite: skip (%s reason=%s spread_bps=%.2f imb=%.3f)\n", rawBest, obReason, obSpreadBps, obImb)
				waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
				continue
			}
		}
		spreadBps, bookImb := orderbookRiskMetrics(rawBest, best.Side, entryDepth, metaBySymbol, obLevels)
		entryPx := best.Sig.Entry
		if entryPx <= 0 {
			entryPx = metaBySymbol[rawBest].LastPrice
		}
		stopPx := best.Sig.Stop
		if stopPx <= 0 && entryPx > 0 {
			d := riskFallbackStopPct / 100.0
			if strings.EqualFold(best.Side, "BUY") {
				stopPx = entryPx * (1 - d)
			} else {
				stopPx = entryPx * (1 + d)
			}
		}
		if !pureMode {
			riskDec := risk.Approve(riskShell, risk.Input{
				Side:              strings.ToUpper(strings.TrimSpace(best.Side)),
				Entry:             entryPx,
				Stop:              stopPx,
				Leverage:          float64(maxInt(1, effectiveLev)),
				NotionalUSD:       effectiveMargin * float64(maxInt(1, effectiveLev)),
				FundingRate:       metaBySymbol[rawBest].FundingRate,
				HoldHours:         riskHoldHours,
				SpreadBps:         spreadBps,
				BookImbalance:     bookImb,
				RecentSlippageBps: 0,
				VenueHealthy:      metaBySymbol[rawBest].LastPrice > 0,
			})
			if !riskDec.Approved {
				st.TopRejectReason = riskDec.RejectReason
				statusStore.Set(st)
				fmt.Printf("live-lite: skip (%s reason=%s)\n", rawBest, riskDec.RejectReason)
				waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
				continue
			}
		}

		if inMaint {
			st.TopRejectReason = "maintenance_window"
			statusStore.Set(st)
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if maintWarmup > 0 {
			if until, ok := maintenanceWarmupUntil(localMaintNow, maintWarmup, &maintState); ok {
				st.TopRejectReason = "post_maint_warmup"
				statusStore.Set(st)
				fmt.Printf("live-lite: skip (reason=post_maint_warmup until %s)\n", until.Format("15:04 MST"))
				waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
				continue
			}
		}
		if rest == nil || dryRun {
			eventLog.Emit(stats.Event{
				Timestamp:   now,
				Type:        "INTENT",
				Simulated:   true,
				Symbol:      rawBest,
				Side:        best.Side,
				TF:          "1m",
				Strategy:    best.Strat,
				Score:       best.Entry.CurrentScore,
				Slope:       best.Entry.ScoreSlope,
				VolumeRatio: 0,
				EntryPx:     best.Sig.Entry,
				Reason:      "dry_run_intent",
			})
			printTradeIntent(best, entryBps, effectiveMargin, effectiveLev)
			if paper.enabled {
				if len(entryDepth) == 0 {
					entryDepth = fetchOrderBooks(client, []string{rawBest}, envInt("LIVE_PAPER_OB_LEVELS", 20))
				}
				mergeTopOfBookIntoMeta(metaBySymbol, entryDepth)
				pp, err := paper.MaybeEnter(now, best, entryBps, effectiveMargin, effectiveLev, metaBySymbol, entryDepth)
				if err != nil {
					fmt.Println("paper enter skip:", err)
				} else if pp != nil {
					eventLog.Emit(stats.Event{
						Timestamp: now,
						Type:      "POSITION_OPEN",
						Simulated: true,
						Symbol:    pp.Symbol,
						Side:      pp.Side,
						TF:        "1m",
						Strategy:  best.Strat,
						Score:     best.Entry.CurrentScore,
						Slope:     best.Entry.ScoreSlope,
						EntryPx:   pp.Entry,
						Reason:    "paper_enter",
					})
					tg.Sendf("🟦 <b>PAPER ENTER | %s %s</b>\n• <b>Margin:</b> $%.2f | <b>Lev:</b> %dx | <b>Grade:</b> %s | <b>Conf:</b> %.2f\n• <b>Setup:</b> <code>%s</code>\n• <b>Entry:</b> %s | <b>SL:</b> %s\n• <b>TP1:</b> %s | <b>TP2:</b> %s | <b>TP3:</b> %s",
						pp.Symbol, pp.Side, pp.Margin, pp.Leverage, best.Entry.CurrentGrade, best.Conf, best.Strat,
						fmtPrice(pp.Entry), fmtPrice(pp.Stop), fmtPrice(pp.TP1), fmtPrice(pp.TP2), fmtPrice(pp.TP3))
				}
			}
			if tgVerbose {
				tg.Sendf("%s", notify.BuildEventHTML("🧪", "DRY RUN INTENT",
					fmt.Sprintf("<b>%s %s</b>", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side),
					fmt.Sprintf("<b>Margin:</b> $%.2f", effectiveMargin),
					fmt.Sprintf("<b>Grade:</b> %s | <b>Score:</b> %.2f", best.Entry.CurrentGrade, best.Entry.CurrentScore),
				))
			}
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		nowLocal := time.Now()
		if !pureMode && safety.stopoutCount > 0 && safety.stopoutWindow > 0 && safety.stopoutLock > 0 && execMgr != nil {
			raw := strings.ToUpper(aster.RawSymbol(best.Entry.Symbol))
			cnt := execMgr.StopoutCountSince(raw, nowLocal.Add(-safety.stopoutWindow))
			if cnt >= safety.stopoutCount {
				if until, ok := symbolStopoutLockUntil[raw]; !ok || nowLocal.After(until) {
					symbolStopoutLockUntil[raw] = nowLocal.Add(safety.stopoutLock)
				}
			}
		}
		if !pureMode {
			if reason := safetyReject(safety, best, nowLocal, lastOrderAt, lastOrderBySymbol, orderCountByDay, orderCountByHour, symbolStopoutLockUntil); reason != "" {
				st.TopRejectReason = reason
				statusStore.Set(st)
				fmt.Println("live-lite: safety skip:", reason)
				if tgVerbose {
					tg.Sendf("%s", notify.BuildEventHTML("🛡️", "SAFETY SKIP",
						fmt.Sprintf("<b>%s %s</b>", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side),
						fmt.Sprintf("<b>Reason:</b> %s", reason),
					))
				}
				waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
				continue
			}
		}
		if !pureMode && inEventLockout(time.Now(), eventLockoutMin) {
			st.TopRejectReason = "event_lockout"
			statusStore.Set(st)
			fmt.Println("live-lite: skip reason=event_lockout")
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if !pureMode && isCorrelatedExposureTooHigh(best, acct, corrGroups, maxCorrelatedExposure) {
			st.TopRejectReason = "correlated_exposure_gate"
			statusStore.Set(st)
			fmt.Println("live-lite: skip reason=correlated_exposure_gate")
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if !pureMode && requireShadowDays > 0 && !shadowReady(requireShadowDays, shadowEquityFile, now) {
			if shadowWarnAt.IsZero() || now.Sub(shadowWarnAt) > 30*time.Minute {
				msg := fmt.Sprintf("shadow gate active: need %d day(s) paper history before live", requireShadowDays)
				fmt.Println("live-lite:", msg)
				tg.Sendf("%s", notify.BuildEventHTML("⏳", "SHADOW GATE ACTIVE", fmt.Sprintf("<b>Requirement:</b> %s", msg)))
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
				tg.Sendf("%s", notify.BuildEventHTML("🛡️", "SAFETY SKIP",
					fmt.Sprintf("<b>%s %s</b>", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side),
					fmt.Sprintf("<b>Available:</b> %.4f < <b>Min:</b> %.4f", avail, safety.minAvailUSDT),
				))
			}
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		effectiveReserve = computeReserveUSDT(reserveMode, reserveUSDT, reservePct, avail, paper)
		effectiveMargin = computeTradeMargin(tradeMarginMode, tradeMargin, tradeMarginPct, tradeSlots, tradeMarginMin, tradeMarginMax, effectiveReserve, avail, paper)
		usable := avail - effectiveReserve
		baseBal := sizingBaseBalance(avail, paper)
		if reserveGate != nil {
			reserveGate.ensureTarget(baseBal, effectiveReserve)
		}
		if usable < effectiveMargin {
			fmt.Printf("live-lite: skip (available %.4f, usable %.4f < margin %.4f)\n", avail, usable, effectiveMargin)
			if tgVerbose {
				tg.Sendf("%s", notify.BuildEventHTML("🛡️", "SKIP: INSUFFICIENT USABLE",
					fmt.Sprintf("<b>%s %s</b>", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side),
					fmt.Sprintf("<b>Usable:</b> %.4f | <b>Required Margin:</b> %.4f", usable, effectiveMargin),
				))
			}
			waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
			continue
		}
		if !pureMode && reserveGate != nil && reserveGate.block(baseBal) {
			fmt.Printf("live-lite: reserve lock active (base=%.4f reserve_target=%.4f)\n", baseBal, reserveGate.targetReserve)
			if tgVerbose {
				tg.Sendf("%s", notify.BuildEventHTML("🔒", "RESERVE LOCK ACTIVE",
					fmt.Sprintf("<b>Base:</b> %.2f below threshold", baseBal),
					"<b>Entries:</b> paused until reserve recovers",
				))
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
				tg.Sendf("%s", notify.BuildEventHTML("🛡️", "SKIP: MAX OPEN POSITIONS",
					fmt.Sprintf("<b>%s %s</b>", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side),
					fmt.Sprintf("<b>Open:</b> %d | <b>Max:</b> %d", openCount, maxOpenPos),
				))
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
		eventLog.Emit(stats.Event{
			Timestamp: now,
			Type:      "INTENT",
			Symbol:    strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)),
			Side:      best.Side,
			TF:        "1m",
			Strategy:  best.Strat,
			Score:     best.Entry.CurrentScore,
			Slope:     best.Entry.ScoreSlope,
			EntryPx:   best.Sig.Entry,
			Reason:    "live_intent",
		})
		eventLog.Emit(stats.Event{
			Timestamp: now,
			Type:      "ORDER_SUBMIT",
			Symbol:    strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)),
			Side:      best.Side,
			TF:        "1m",
			Strategy:  best.Strat,
			Score:     best.Entry.CurrentScore,
			Slope:     best.Entry.ScoreSlope,
			EntryPx:   best.Sig.Entry,
		})
		if err := execMgr.PlaceEntry(best, entryBps, effectiveMargin, effectiveLev); err != nil {
			fmt.Println("live-lite: place error:", err)
			eventLog.Emit(stats.Event{
				Timestamp: now,
				Type:      "ORDER_REJECT",
				Symbol:    strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)),
				Side:      best.Side,
				TF:        "1m",
				Strategy:  best.Strat,
				Score:     best.Entry.CurrentScore,
				Slope:     best.Entry.ScoreSlope,
				EntryPx:   best.Sig.Entry,
				Reason:    err.Error(),
			})
			if tgVerbose {
				tg.Sendf("%s", notify.BuildEventHTML("❌", "ORDER ERROR",
					fmt.Sprintf("<b>%s %s</b>", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side),
					fmt.Sprintf("<b>Error:</b> %v", err),
				))
			}
		} else {
			eventLog.Emit(stats.Event{
				Timestamp: now,
				Type:      "ORDER_FILL",
				Symbol:    strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)),
				Side:      best.Side,
				TF:        "1m",
				Strategy:  best.Strat,
				Score:     best.Entry.CurrentScore,
				Slope:     best.Entry.ScoreSlope,
				EntryPx:   best.Sig.Entry,
				Reason:    "entry_accepted",
			})
			lastOrderAt = time.Now()
			lastOrderBySymbol[strings.ToUpper(aster.RawSymbol(best.Entry.Symbol))] = lastOrderAt
			dayKey := time.Now().UTC().Format("2006-01-02")
			hourKey := time.Now().UTC().Format("2006-01-02T15")
			orderCountByDay[dayKey]++
			orderCountByHour[hourKey]++
			tg.Sendf("%s", notify.BuildEventHTML("✅", "ORDER PLACED",
				fmt.Sprintf("<b>%s %s</b>", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side),
				fmt.Sprintf("<b>Margin:</b> $%.2f", effectiveMargin),
				fmt.Sprintf("<b>Grade:</b> %s", best.Entry.CurrentGrade),
			))
		}

		waitForNextCycle(cycleStart, scanEvery, reconEvery, execMgr)
	}
}

func sendInPlayDigest(tg *notify.Telegram, longInPlay, shortInPlay []inplay.Entry, meta map[string]symbolMeta, dryRun bool, limit int) {
	if tg == nil || !tg.Enabled() {
		return
	}
	now := time.Now().UTC()
	mode := "LIVE"
	if dryRun {
		mode = "DRY_RUN"
	}
	longTop, shortTop, bias := topScanSnapshot(longInPlay, shortInPlay, maxInt(1, limit))
	tg.Sendf("%s", notify.BuildEventHTML("📡", "LIVE-LITE DIGEST",
		fmt.Sprintf("<b>Mode:</b> %s | <b>UTC:</b> %s", mode, now.Format("15:04")),
		fmt.Sprintf("<b>Regime:</b> %s", data.CurrentRegimeCT(now)),
		notify.BuildScannerSnapshotHTML(longTop, shortTop, bias),
	))
}

func buildHourlyDigest(now time.Time, p *paperTrader, meta map[string]symbolMeta, longInPlay, shortInPlay []inplay.Entry, topN int) string {
	if p == nil || !p.enabled {
		return notify.BuildSessionPulseHTML(notify.PulseSnapshot{
			Title:     "HOURLY DIGEST",
			TimeLabel: now.Format("15:04 MST"),
			Session:   sessionTag(now),
		})
	}
	pulse, cards := buildPaperPulseAndCards("HOURLY DIGEST", now, p, meta)
	longTop, shortTop, bias := topScanSnapshot(longInPlay, shortInPlay, 2)
	var b strings.Builder
	b.WriteString(notify.BuildSessionPulseHTML(pulse))
	for _, c := range cards {
		b.WriteString("\n\n")
		b.WriteString(notify.BuildPositionCard(c))
	}
	b.WriteString("\n\n")
	b.WriteString(notify.BuildScannerSnapshotHTML(longTop, shortTop, bias))
	return strings.TrimSpace(b.String())
}

func buildSODReport(now time.Time, p *paperTrader, meta map[string]symbolMeta) string {
	if p == nil || !p.enabled {
		return notify.BuildSessionPulseHTML(notify.PulseSnapshot{
			Title:     "SOD REPORT",
			TimeLabel: now.Format("15:04 MST"),
			Session:   sessionTag(now),
		})
	}
	pulse, cards := buildPaperPulseAndCards("SOD REPORT", now, p, meta)
	var b strings.Builder
	b.WriteString(notify.BuildSessionPulseHTML(pulse))
	for _, c := range cards {
		b.WriteString("\n\n")
		b.WriteString(notify.BuildPositionCard(c))
	}
	return strings.TrimSpace(b.String())
}

func buildPaperPulseAndCards(title string, now time.Time, p *paperTrader, meta map[string]symbolMeta) (notify.PulseSnapshot, []notify.PositionCard) {
	dayKey := now.In(p.reportLoc).Format("2006-01-02")
	realized := 0.0
	if ds := p.dayStats[dayKey]; ds != nil {
		realized = ds.Net
	}
	openPnL := 0.0
	cards := make([]notify.PositionCard, 0, len(p.positions))
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		m := meta[raw]
		mark := m.LastPrice
		if mark <= 0 {
			mark = pos.LastMark
		}
		if mark <= 0 {
			mark = pos.Entry
		}
		upnl, pct := realizedFromFill(pos.Side, pos.Entry, mark, pos.Qty)
		openPnL += upnl
		conf := estimateConfluenceFromReason(pos.EntryReason)
		cards = append(cards, notify.PositionCard{
			Symbol:           raw,
			Side:             pos.Side,
			EntryPrice:       pos.Entry,
			MarkPrice:        mark,
			UnrealizedPnL:    upnl,
			UnrealizedPnLPct: pct,
			Leverage:         maxInt(pos.Leverage, 1),
			Setup:            pos.EntryReason,
			Confluence:       conf,
			AgeMin:           int(now.Sub(pos.OpenedAt).Minutes()),
			StopLoss:         pos.Stop,
			TakeProfit:       firstPositive(pos.TP1, pos.TP2, pos.TP3),
		})
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].UnrealizedPnL > cards[j].UnrealizedPnL })
	eq := p.balance + openPnL
	net := realized + openPnL
	netPct := 0.0
	if p.startBal > 0 {
		netPct = (net / p.startBal) * 100.0
	}
	return notify.PulseSnapshot{
		Title:     title,
		TimeLabel: now.Format("15:04 MST"),
		Session:   sessionTag(now),
		Balance:   p.balance,
		Equity:    eq,
		Realized:  realized,
		OpenPnL:   openPnL,
		NetDay:    net,
		OpenCount: len(cards),
		NetDayPct: netPct,
	}, cards
}

func topScanSnapshot(longInPlay, shortInPlay []inplay.Entry, topN int) ([]notify.ScanItem, []notify.ScanItem, string) {
	if topN <= 0 {
		topN = 2
	}
	toItems := func(rows []inplay.Entry) []notify.ScanItem {
		n := len(rows)
		if n > topN {
			n = topN
		}
		out := make([]notify.ScanItem, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, notify.ScanItem{
				Symbol: strings.ToUpper(aster.RawSymbol(rows[i].Symbol)),
				Grade:  rows[i].CurrentGrade,
				Score:  rows[i].CurrentScore,
			})
		}
		return out
	}
	longs := toItems(longInPlay)
	shorts := toItems(shortInPlay)
	bias := "NEUTRAL"
	if len(longInPlay) > 0 && len(shortInPlay) == 0 {
		bias = "LONG"
	} else if len(shortInPlay) > 0 && len(longInPlay) == 0 {
		bias = "SHORT"
	} else if len(longInPlay) > 0 && len(shortInPlay) > 0 {
		if longInPlay[0].CurrentScore-shortInPlay[0].CurrentScore > 8 {
			bias = "LONG"
		} else if shortInPlay[0].CurrentScore-longInPlay[0].CurrentScore > 8 {
			bias = "SHORT"
		}
	}
	return longs, shorts, bias
}

func estimateConfluenceFromReason(reason string) float64 {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch r {
	case "vwap_confluence", "vp_trend", "pd_levels_retest":
		return 0.66
	case "failed_auction_magnet", "fa", "lsr", "bos_pb":
		return 0.60
	case "":
		return 0.55
	default:
		return 0.58
	}
}

func firstPositive(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func exitAlertEmoji(reason string) string {
	r := strings.ToUpper(strings.TrimSpace(reason))
	switch {
	case strings.Contains(r, "WHALE_DIVERGENCE"), strings.Contains(r, "HVN_FRONT"), strings.Contains(r, "FRONT_RUN"):
		return "⚠️"
	case strings.Contains(r, "STOP"), strings.Contains(r, "SL"):
		return "🛑"
	case strings.Contains(r, "TP"):
		return "✅"
	case strings.Contains(r, "MOMENTUM"):
		return "📉"
	default:
		return "ℹ️"
	}
}

func appendInPlayRows(b *strings.Builder, tag string, rows []inplay.Entry, meta map[string]symbolMeta, limit int) {
	fmt.Fprintf(b, "%s (%d)\n", tag, len(rows))
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
		raw := strings.ToUpper(aster.RawSymbol(e.Symbol))
		m := meta[raw]
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
		price := "n/a"
		if m.LastPrice > 0 {
			price = fmtPrice(m.LastPrice)
		}
		fmt.Fprintf(b, "%d) %s %s g=%s s=%.2f %s%.3f px=%s 24h=%+.2f%%\n",
			i+1, raw, status, e.CurrentGrade, e.CurrentScore, slopeArrow, abs(e.ScoreSlope), price, m.Move24h)
	}
}

func activeMaintenanceWindow(now time.Time, enabled bool, w1, w2 maintenanceWindow) (maintenanceWindow, bool) {
	if !enabled {
		return maintenanceWindow{}, false
	}
	if inMinuteWindow(now.Hour(), now.Minute(), w1.StartHour, w1.StartMin, w1.EndHour, w1.EndMin) {
		return w1, true
	}
	if inMinuteWindow(now.Hour(), now.Minute(), w2.StartHour, w2.StartMin, w2.EndHour, w2.EndMin) {
		return w2, true
	}
	return maintenanceWindow{}, false
}

func normalizeMaintenanceWindow(w maintenanceWindow) maintenanceWindow {
	if w.StartHour < 0 || w.StartHour > 23 {
		w.StartHour = 0
	}
	if w.EndHour < 0 || w.EndHour > 23 {
		w.EndHour = 0
	}
	if w.StartMin < 0 || w.StartMin > 59 {
		w.StartMin = 0
	}
	if w.EndMin < 0 || w.EndMin > 59 {
		w.EndMin = 0
	}
	return w
}

func maintenanceWarmupUntil(now time.Time, warmup time.Duration, st *maintenanceState) (time.Time, bool) {
	if warmup <= 0 || st == nil || len(st.LastEndAt) == 0 {
		return time.Time{}, false
	}
	latest := time.Time{}
	for _, t := range st.LastEndAt {
		if t.IsZero() {
			continue
		}
		if t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		return time.Time{}, false
	}
	until := latest.Add(warmup)
	if now.Before(until) {
		return until, true
	}
	return time.Time{}, false
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

func inMinuteWindow(hour, min, startHour, startMin, endHour, endMin int) bool {
	nowM := hour*60 + min
	startM := startHour*60 + startMin
	endM := endHour*60 + endMin
	if startM == endM {
		return false
	}
	if startM < endM {
		return nowM >= startM && nowM < endM
	}
	return nowM >= startM || nowM < endM
}

func shouldSendPulse(now, last time.Time, minGap time.Duration) bool {
	if minGap <= 0 || last.IsZero() {
		return true
	}
	return now.Sub(last) >= minGap
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
	adv := 0.0
	if strings.EqualFold(p.Side, "BUY") {
		adv = (p.EntryPrice - mark) / risk
	} else {
		adv = (mark - p.EntryPrice) / risk
	}
	if adv > p.MaxAdverseR {
		p.MaxAdverseR = adv
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
	adv := 0.0
	if strings.EqualFold(p.Side, "BUY") {
		adv = (p.Entry - mark) / risk
	} else {
		adv = (mark - p.Entry) / risk
	}
	if adv > p.MaxAdverseR {
		p.MaxAdverseR = adv
	}
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

func enforceTPProgression(side string, tp1, tp2, tp3 float64) (float64, float64, float64) {
	step := 0.0015 // 15 bps minimum separation
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		if tp2 <= tp1 {
			tp2 = tp1 * (1 + step)
		}
		if tp3 <= tp2 {
			tp3 = tp2 * (1 + step)
		}
		return tp1, tp2, tp3
	}
	if tp2 >= tp1 {
		tp2 = tp1 * (1 - step)
	}
	if tp3 >= tp2 {
		tp3 = tp2 * (1 - step)
	}
	return tp1, tp2, tp3
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

func adjustBracketParams(reason string, conf, volumeUSD, stopPct, tp1R, tp2R, tp3R, minStopPct, maxStopPct float64) (float64, float64, float64, float64) {
	tp1Max := envFloat("LIVE_TP1_MAX_R", 2.5)
	tp2Max := envFloat("LIVE_TP2_MAX_R", 4.0)
	tp3Max := envFloat("LIVE_TP3_MAX_R", 6.0)
	stopWiden := envFloat("LIVE_STOP_WIDEN_MULT", 1.32)
	softenConf := envFloat("LIVE_SOFTEN_CONF_MAX", 0.65)

	r := strings.ToLower(strings.TrimSpace(reason))
	soften := strings.Contains(r, "failed_auction") || strings.Contains(r, "rejection") || conf <= softenConf
	if soften && stopWiden > 0 {
		stopPct *= stopWiden
	}
	stopPct *= volumeStopWiden(volumeUSD)
	stopPct = clamp(stopPct, minStopPct, maxStopPct)

	if tp1Max > 0 && tp1R > tp1Max {
		tp1R = tp1Max
	}
	if tp2Max > 0 && tp2R > tp2Max {
		tp2R = tp2Max
	}
	if tp3Max > 0 && tp3R > tp3Max {
		tp3R = tp3Max
	}
	if tp1R < 0.8 {
		tp1R = 0.8
	}
	if tp2R < tp1R {
		tp2R = tp1R
	}
	if tp3R < tp2R {
		tp3R = tp2R
	}
	return stopPct, tp1R, tp2R, tp3R
}

func volumeStopWiden(volumeUSD float64) float64 {
	if volumeUSD <= 0 {
		return 1.0
	}
	if volumeUSD >= envFloat("LIVE_STOP_VOL_USD_TIER3", 250_000_000) {
		return envFloat("LIVE_STOP_VOL_WIDEN_TIER3", 1.32)
	}
	if volumeUSD >= envFloat("LIVE_STOP_VOL_USD_TIER2", 100_000_000) {
		return envFloat("LIVE_STOP_VOL_WIDEN_TIER2", 1.20)
	}
	if volumeUSD >= envFloat("LIVE_STOP_VOL_USD_TIER1", 25_000_000) {
		return envFloat("LIVE_STOP_VOL_WIDEN_TIER1", 1.11)
	}
	return 1.0
}

func beArmThreshold(configured, tp1R float64) float64 {
	if configured <= 0 {
		configured = 0.5
	}
	nearTP1 := tp1R - 0.5
	if nearTP1 < 0.5 {
		nearTP1 = 0.5
	}
	if tp1R > 0 && nearTP1 > tp1R {
		nearTP1 = tp1R
	}
	if nearTP1 > configured {
		return nearTP1
	}
	return configured
}

func tp1RFromBracket(entry, stop, tp1 float64) float64 {
	risk := abs(entry - stop)
	if risk <= 0 || entry <= 0 || tp1 <= 0 {
		return 1.0
	}
	return abs(tp1-entry) / risk
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

func (m *liveExecManager) dayRealizedAt(now time.Time) float64 {
	if m == nil {
		return 0
	}
	loc := m.reportLoc
	if loc == nil {
		loc = time.Local
	}
	dayKey := now.In(loc).Format("2006-01-02")
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
			fr := 0.0
			if r.FundingRate != nil {
				fr = *r.FundingRate
			}
			out[raw] = symbolMeta{
				LastPrice:   r.LastPrice,
				Move24h:     r.Change24h,
				VolumeUSD:   r.VolumeUSD,
				FundingRate: fr,
			}
		}
	}
	put(longRows)
	put(shortRows)
	return out
}

func loadDiscoveryConfig() discovery.Config {
	cfg := discovery.DefaultConfig()
	cfg.Enabled = envBool("LIVE_DISCOVERY_ENABLE", true)
	cfg.TopN = envInt("LIVE_DISCOVERY_TOP_N", 10)
	cfg.MinVolumeRatio = envFloat("LIVE_DISCOVERY_MIN_VOLUME_RATIO", 1.5)
	cfg.MinVolatility = envFloat("LIVE_DISCOVERY_MIN_VOLATILITY", 0)
	cfg.LookbackMinutes = envInt("LIVE_DISCOVERY_LOOKBACK_MIN", 60)
	cfg.RefreshSeconds = envInt("LIVE_DISCOVERY_REFRESH_SEC", 60)
	if cfg.TopN <= 0 {
		cfg.TopN = 10
	}
	if cfg.LookbackMinutes <= 0 {
		cfg.LookbackMinutes = 60
	}
	return cfg
}

func loadEntryGateConfig() gate.Config {
	cfg := gate.DefaultConfig()
	cfg.MinGrade = envStr("LIVE_GATE_MIN_GRADE", "B")
	cfg.MinScore = envFloat("LIVE_GATE_MIN_SCORE", 75)
	cfg.MinSlope = envFloat("LIVE_GATE_MIN_SLOPE", 0.15)
	cfg.RequireVolumeSpike = envBool("LIVE_GATE_REQUIRE_VOLUME_SPIKE", true)
	cfg.MinVolumeRatio = envFloat("LIVE_GATE_MIN_VOLUME_RATIO", 1.5)
	cfg.RequireMTF = envBool("LIVE_GATE_REQUIRE_MTF", true)
	cfg.MTF.EMAFast = envInt("LIVE_GATE_EMA_FAST", 8)
	cfg.MTF.EMASlow = envInt("LIVE_GATE_EMA_SLOW", 20)
	cfg.MTF.Use15m = envBool("LIVE_GATE_MTF_USE_15M", false)
	cfg.RequireRegime = envBool("LIVE_GATE_REQUIRE_REGIME", false)
	cfg.Regime.ProxySymbol = envStr("LIVE_GATE_REGIME_PROXY", "BTCUSDT")
	cfg.Regime.MinATRPct = envFloat("LIVE_GATE_REGIME_MIN_ATR_PCT", 0.8)
	return cfg
}

func filterUniverseRows(rows []market.Scored, universe []string) []market.Scored {
	if len(universe) == 0 {
		return rows
	}
	allow := make(map[string]struct{}, len(universe))
	for _, s := range universe {
		allow[strings.ToUpper(strings.TrimSpace(aster.RawSymbol(s)))] = struct{}{}
	}
	out := make([]market.Scored, 0, len(rows))
	for _, r := range rows {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(r.Symbol)))
		if _, ok := allow[raw]; ok {
			out = append(out, r)
		}
	}
	return out
}

func buildDiscoveryCandles(c *aster.Client, longRows, shortRows []market.Scored, cfg discovery.Config) map[string][]types.Candle {
	if c == nil {
		return map[string][]types.Candle{}
	}
	maxLoad := cfg.TopN * 3
	if maxLoad < 20 {
		maxLoad = 20
	}
	seen := map[string]struct{}{}
	load := make([]string, 0, maxLoad)
	add := func(rows []market.Scored) {
		for _, r := range rows {
			raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(r.Symbol)))
			if raw == "" {
				continue
			}
			if _, ok := seen[raw]; ok {
				continue
			}
			seen[raw] = struct{}{}
			load = append(load, raw)
			if len(load) >= maxLoad {
				return
			}
		}
	}
	add(longRows)
	if len(load) < maxLoad {
		add(shortRows)
	}
	out := make(map[string][]types.Candle, len(load))
	limit := cfg.LookbackMinutes
	if limit < 30 {
		limit = 30
	}
	for _, sym := range load {
		bars, err := c.LoadCandles(sym, types.TF1m, limit)
		if err != nil || len(bars) == 0 {
			continue
		}
		cs := make([]types.Candle, 0, len(bars))
		for _, b := range bars {
			cs = append(cs, b)
		}
		out[sym] = cs
	}
	return out
}

func buildGateInput(c *aster.Client, cand candidate, cfg gate.Config) gate.Input {
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(cand.Entry.Symbol)))
	in := gate.Input{
		Symbol: raw,
		Side:   cand.Side,
		Grade:  cand.Entry.CurrentGrade,
		Score:  cand.Entry.CurrentScore,
		Slope:  cand.Entry.ScoreSlope,
	}
	bars, err := c.LoadCandles(raw, types.TF1m, 120)
	if err == nil && len(bars) > 25 {
		in.VolumeRatio, _ = discovery.VolumeRatio(bars, 20)
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
		b, err := c.LoadCandles(raw, tf, 64)
		if err != nil || len(b) < cfg.MTF.EMASlow+1 {
			continue
		}
		closes := make([]float64, 0, len(b))
		for _, x := range b {
			closes = append(closes, x.C)
		}
		fast := indicators.EMA(closes, cfg.MTF.EMAFast)
		slow := indicators.EMA(closes, cfg.MTF.EMASlow)
		if len(fast) == 0 || len(slow) == 0 {
			continue
		}
		in.MTF = append(in.MTF, gate.MTFSnapshot{
			TF:      tf.String(),
			EMAFast: fast[len(fast)-1],
			EMASlow: slow[len(slow)-1],
		})
	}
	return in
}

func boolPtr(v bool) *bool { return &v }

func fetchOrderBooks(c *aster.Client, symbols []string, limit int) map[string]aster.OrderBook {
	out := map[string]aster.OrderBook{}
	if c == nil || len(symbols) == 0 {
		return out
	}
	if limit <= 0 {
		limit = 20
	}
	seen := map[string]struct{}{}
	for _, s := range symbols {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(s)))
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		ob, err := c.FetchOrderBook(raw, limit)
		if err != nil {
			continue
		}
		out[raw] = ob
	}
	return out
}

func mergeTopOfBookIntoMeta(meta map[string]symbolMeta, books map[string]aster.OrderBook) {
	if len(meta) == 0 || len(books) == 0 {
		return
	}
	for raw, ob := range books {
		m, ok := meta[raw]
		if !ok {
			continue
		}
		if len(ob.Bids) > 0 && ob.Bids[0][0] > 0 {
			m.Bid = ob.Bids[0][0]
		}
		if len(ob.Asks) > 0 && ob.Asks[0][0] > 0 {
			m.Ask = ob.Asks[0][0]
		}
		meta[raw] = m
	}
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
	feeProfile := envStr("LIVE_FEE_PROFILE", "pro")
	discPct := envFloat("LIVE_FEE_DISCOUNT_PCT", 0)
	profileMaker, profileTaker := resolvePaperFeeProfile(feeProfile)
	feeBps := envFloat("LIVE_PAPER_FEE_BPS", profileTaker)
	if feeBps < 0 {
		feeBps = 0
	}
	makerFeeBps := envFloat("LIVE_PAPER_FEE_MAKER_BPS", profileMaker)
	takerFeeBps := envFloat("LIVE_PAPER_FEE_TAKER_BPS", feeBps)
	if discPct > 0 {
		f := 1.0 - clamp(discPct, 0, 100)/100.0
		makerFeeBps *= f
		takerFeeBps *= f
		feeBps *= f
	}
	if makerFeeBps < 0 {
		makerFeeBps = 0
	}
	if takerFeeBps < 0 {
		takerFeeBps = 0
	}
	fundingEvery := time.Duration(envInt("LIVE_PAPER_FUNDING_INTERVAL_MIN", 480)) * time.Minute
	if fundingEvery <= 0 {
		fundingEvery = 480 * time.Minute
	}
	fundingBySym := parseSymbolMinutesMap(envStr("LIVE_PAPER_FUNDING_INTERVALS", ""))
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
	beLockBps := envFloat("LIVE_BE_LOCK_BPS", 5)
	lossCooldown := time.Duration(envInt("LIVE_PAPER_LOSS_COOLDOWN_MIN", 0)) * time.Minute
	maxLossStreak := envInt("LIVE_PAPER_SYMBOL_MAX_LOSS_STREAK", 3)
	if maxLossStreak < 0 {
		maxLossStreak = 0
	}
	lossLock := time.Duration(envInt("LIVE_PAPER_SYMBOL_LOSS_LOCK_MIN", 5)) * time.Minute
	if lossLock < 0 {
		lossLock = 0
	}
	if lossCooldown < 0 {
		lossCooldown = 0
	}
	openCostMode := strings.ToLower(envStr("LIVE_PAPER_OPEN_COST_MODE", "aster"))
	stressRoundtripBps := envFloat("PAPER_STRESS_BPS_ROUNDTRIP", 0)
	if stressRoundtripBps < 0 {
		stressRoundtripBps = 0
	}
	p := &paperTrader{
		enabled:            enabled,
		startBal:           start,
		balance:            start,
		reserve:            reserveUSDT,
		feeBps:             feeBps,
		makerFeeBps:        makerFeeBps,
		takerFeeBps:        takerFeeBps,
		stopPct:            stopPct,
		tp1R:               tp1R,
		tp2R:               tp2R,
		tp3R:               tp3R,
		tp1Frac:            tp1Frac,
		tp2Frac:            tp2Frac,
		tp3Frac:            tp3Frac,
		trailAfterTP:       trailAfterTP,
		trailStopPct:       trailStopPct,
		stateFile:          envStr("LIVE_PAPER_STATE_FILE", "out/paper_state.json"),
		tradesCSV:          resolveStatePath(envStr("LIVE_PAPER_TRADES_FILE", "out/paper_trades.csv")),
		equityCSV:          resolveStatePath(envStr("LIVE_PAPER_EQUITY_FILE", "out/paper_equity.csv")),
		maxOpen:            maxOpen,
		positions:          map[string]*paperPosition{},
		reportLoc:          reportLoc,
		dayStats:           map[string]*paperDayStats{},
		minStopPct:         minStopPct,
		maxStopPct:         maxStopPct,
		minTP1RR:           minTP1RR,
		beLockBps:          beLockBps,
		fundingEvery:       fundingEvery,
		fundingBySym:       fundingBySym,
		lastFundKey:        map[string]string{},
		openCostMode:       openCostMode,
		lossCooldown:       lossCooldown,
		lastExitAt:         map[string]time.Time{},
		lastExitLoss:       map[string]bool{},
		lossStreak:         map[string]int{},
		lockUntil:          map[string]time.Time{},
		maxLossStreak:      maxLossStreak,
		lossLock:           lossLock,
		stressRoundtripBps: stressRoundtripBps,
		exitManager: exitmgr.NewManager(exitmgr.Config{
			FrontRunPct:            envFloat("LIVE_TP_FRONT_RUN_PCT", 0.001),
			NoFollowThroughBars:    envInt("LIVE_EXIT_NO_FT_BARS", 8),
			NoFollowThroughMinMFER: envFloat("LIVE_EXIT_NO_FT_MIN_MFE_R", 0.25),
			NoFollowThroughMinMAER: envFloat("LIVE_EXIT_NO_FT_MIN_MAE_R", 0.70),
			WeakFlowArmBER:         envFloat("LIVE_EXIT_WEAK_FLOW_BE_R", 0.45),
			LiqSpikePartialPct:     envFloat("LIVE_EXIT_LIQ_SPIKE_PARTIAL_PCT", 0.35),
			StallBarsForTighten:    envInt("LIVE_EXIT_STALL_BARS", 3),
			StallTightenToR:        envFloat("LIVE_EXIT_STALL_TIGHTEN_TO_R", 0.20),
		}),
	}
	p.stateFile = resolveStatePath(p.stateFile)
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
	if st.LastFund != nil {
		p.lastFundKey = st.LastFund
	}
	if st.LastExitAt != nil {
		p.lastExitAt = st.LastExitAt
	}
	if st.LastExitLoss != nil {
		p.lastExitLoss = st.LastExitLoss
	}
	if st.LossStreak != nil {
		p.lossStreak = st.LossStreak
	}
	if st.LockUntil != nil {
		p.lockUntil = st.LockUntil
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
		StartBal:     p.startBal,
		Balance:      p.balance,
		Reserve:      p.reserve,
		Positions:    p.positions,
		DayStats:     p.dayStats,
		LastFund:     p.lastFundKey,
		LastExitAt:   p.lastExitAt,
		LastExitLoss: p.lastExitLoss,
		LossStreak:   p.lossStreak,
		LockUntil:    p.lockUntil,
		UpdatedAt:    time.Now().UTC(),
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.stateFile, b, 0o644)
}

func newLiveExecManager(rest *aster.RESTAuth, tg *notify.Telegram) *liveExecManager {
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
	marginType := strings.ToUpper(envStr("LIVE_MARGIN_TYPE", "ISOLATED"))
	enforceIsolated := envBool("LIVE_ENFORCE_MARGIN_TYPE", true)
	multiAssetMode := envBool("LIVE_MULTI_ASSET_MODE", false)
	if multiAssetMode {
		marginType = "CROSSED"
		enforceIsolated = false
	}
	reportTZ := envStr("LIVE_REPORT_TZ", "America/Chicago")
	reportLoc, err := time.LoadLocation(reportTZ)
	if err != nil {
		reportLoc = time.Local
	}

	m := &liveExecManager{
		rest:                 rest,
		tg:                   tg,
		path:                 resolveStatePath(envStr("LIVE_STATE_FILE", "out/live_exec_state.json")),
		tradesCSV:            resolveStatePath(envStr("LIVE_TRADES_FILE", "out/live_trades.csv")),
		fillReceipt:          envBool("LIVE_TG_FILL_RECEIPT_ENABLE", true),
		entryTimeout:         time.Duration(envInt("LIVE_ENTRY_TIMEOUT_SEC", 90)) * time.Second,
		stopPct:              stopPct,
		tp1R:                 tp1R,
		tp2R:                 tp2R,
		tp3R:                 tp3R,
		tp1Frac:              tp1Frac,
		tp2Frac:              tp2Frac,
		tp3Frac:              tp3Frac,
		trailAfterTP:         trailAfterTP,
		trailStopPct:         trailStopPct,
		trailStepBps:         trailStepBps,
		minStopPct:           minStopPct,
		maxStopPct:           maxStopPct,
		minTP1RR:             minTP1RR,
		beLockBps:            beLockBps,
		marginType:           marginType,
		enforceIsolated:      enforceIsolated,
		multiAssetMode:       multiAssetMode,
		positions:            map[string]*livePosition{},
		dayRealized:          map[string]float64{},
		reportLoc:            reportLoc,
		recoverStopRetries:   envInt("LIVE_RECOVERY_STOP_RETRIES", 3),
		recoverStopBackoff:   time.Duration(envInt("LIVE_RECOVERY_STOP_RETRY_SEC", 1)) * time.Second,
		recoverATRMult:       envFloat("LIVE_RECOVERY_ATR_MULT", 1.5),
		recoverForceFlatFail: envBool("LIVE_RECOVERY_FORCE_FLAT_ON_STOP_FAIL", true),
		exitManager: exitmgr.NewManager(exitmgr.Config{
			FrontRunPct:            envFloat("LIVE_TP_FRONT_RUN_PCT", 0.001),
			NoFollowThroughBars:    envInt("LIVE_EXIT_NO_FT_BARS", 8),
			NoFollowThroughMinMFER: envFloat("LIVE_EXIT_NO_FT_MIN_MFE_R", 0.25),
			NoFollowThroughMinMAER: envFloat("LIVE_EXIT_NO_FT_MIN_MAE_R", 0.70),
			WeakFlowArmBER:         envFloat("LIVE_EXIT_WEAK_FLOW_BE_R", 0.45),
			LiqSpikePartialPct:     envFloat("LIVE_EXIT_LIQ_SPIKE_PARTIAL_PCT", 0.35),
			StallBarsForTighten:    envInt("LIVE_EXIT_STALL_BARS", 3),
			StallTightenToR:        envFloat("LIVE_EXIT_STALL_TIGHTEN_TO_R", 0.20),
		}),
	}
	if m.entryTimeout <= 0 {
		m.entryTimeout = 90 * time.Second
	}
	if m.recoverStopRetries <= 0 {
		m.recoverStopRetries = 3
	}
	if m.recoverStopBackoff <= 0 {
		m.recoverStopBackoff = time.Second
	}
	if m.recoverATRMult <= 0 {
		m.recoverATRMult = 1.5
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

func (m *liveExecManager) logFill(now time.Time, p *livePosition, action, reason string, qty, fillPx, pnl, pct float64) error {
	if m == nil || strings.TrimSpace(m.tradesCSV) == "" || p == nil {
		return nil
	}
	if err := ensureCSVWithHeader(m.tradesCSV, []string{
		"ts", "symbol", "side", "action", "reason", "qty", "fill_px", "entry_px", "pnl", "pnl_pct", "state", "hold_min",
	}); err != nil {
		return err
	}
	f, err := os.OpenFile(m.tradesCSV, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	holdMin := now.Sub(p.CreatedAt).Minutes()
	w := csv.NewWriter(f)
	_ = w.Write([]string{
		now.Format(time.RFC3339),
		p.Symbol,
		p.Side,
		strings.ToUpper(strings.TrimSpace(action)),
		strings.ToUpper(strings.TrimSpace(reason)),
		fmt.Sprintf("%.8f", qty),
		fmt.Sprintf("%.8f", fillPx),
		fmt.Sprintf("%.8f", p.EntryPrice),
		fmt.Sprintf("%.8f", pnl),
		fmt.Sprintf("%.8f", pct),
		string(p.State),
		fmt.Sprintf("%.2f", holdMin),
	})
	w.Flush()
	if m.eventLog != nil {
		evtType := "ORDER_FILL"
		if strings.EqualFold(action, "CLOSE") || strings.EqualFold(action, "FORCE_CLOSE") || strings.EqualFold(action, "STOP") || strings.EqualFold(action, "TP") {
			evtType = "POSITION_CLOSE"
		}
		m.eventLog.Emit(stats.Event{
			Timestamp: now,
			Type:      evtType,
			Symbol:    p.Symbol,
			Side:      p.Side,
			TF:        "1m",
			Strategy:  p.EntryReason,
			EntryPx:   p.EntryPrice,
			ExitPx:    fillPx,
			PnLUSD:    pnl,
			PnLPct:    pct,
			Reason:    strings.ToUpper(strings.TrimSpace(reason)),
		})
	}
	return w.Error()
}

func (m *liveExecManager) sendFillReceipt(now time.Time, p *livePosition, action, reason string, qty, fillPx, pnl, pct float64) {
	if m == nil || p == nil || m.tg == nil || !m.fillReceipt {
		return
	}
	loc := m.reportLoc
	if loc == nil {
		loc = time.Local
	}
	dayKey := now.In(loc).Format("2006-01-02")
	dayRealized := 0.0
	if m.dayRealized != nil {
		dayRealized = m.dayRealized[dayKey]
	}
	holdMin := now.Sub(p.CreatedAt).Minutes()
	reasonU := strings.ToUpper(strings.TrimSpace(reason))
	m.tg.Sendf("%s <b>FILL %s %s</b>\n• <b>Action:</b> %s | <b>Reason:</b> %s\n• <b>Qty:</b> %.6f | <b>Fill:</b> %s\n• <b>PnL:</b> %+.2f (%+.2f%%)\n• <b>Hold:</b> %.1fm | <b>Day Realized:</b> %+.2f\n• <b>Session:</b> %s",
		exitAlertEmoji(reasonU),
		p.Symbol,
		p.Side,
		strings.ToUpper(strings.TrimSpace(action)),
		reasonU,
		qty,
		fmtPrice(fillPx),
		pnl,
		pct,
		holdMin,
		dayRealized,
		sessionTag(now))
}

func (m *liveExecManager) DailyReceiptMessage(dayKey string, limit int) (string, bool) {
	if m == nil || strings.TrimSpace(m.tradesCSV) == "" || m.reportLoc == nil {
		return "", false
	}
	if limit <= 0 {
		limit = 25
	}
	f, err := os.Open(m.tradesCSV)
	if err != nil {
		return "", false
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil || len(header) == 0 {
		return "", false
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	req := []string{"ts", "symbol", "side", "action", "reason", "qty", "fill_px", "pnl", "pnl_pct", "hold_min"}
	for _, k := range req {
		if _, ok := idx[k]; !ok {
			return "", false
		}
	}
	type row struct {
		ts     time.Time
		symbol string
		side   string
		action string
		reason string
		qty    string
		fill   string
		pnl    string
		pct    string
		hold   string
	}
	rows := make([]row, 0, limit)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, rec[idx["ts"]])
		if err != nil {
			continue
		}
		if ts.In(m.reportLoc).Format("2006-01-02") != dayKey {
			continue
		}
		rows = append(rows, row{
			ts:     ts,
			symbol: rec[idx["symbol"]],
			side:   rec[idx["side"]],
			action: rec[idx["action"]],
			reason: rec[idx["reason"]],
			qty:    rec[idx["qty"]],
			fill:   rec[idx["fill_px"]],
			pnl:    rec[idx["pnl"]],
			pct:    rec[idx["pnl_pct"]],
			hold:   rec[idx["hold_min"]],
		})
	}
	if len(rows) == 0 {
		return fmt.Sprintf("Live Trade Receipt %s (%s)\nno fills", dayKey, m.reportLoc.String()), true
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ts.Before(rows[j].ts) })
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Live Trade Receipt %s (%s)\n", dayKey, m.reportLoc.String())
	b.WriteString("| Time | Sym | Side | Action | Qty | Fill | PnL | PnL% | Hold(m) | Reason |\n")
	b.WriteString("|---|---|---|---|---:|---:|---:|---:|---:|---|\n")
	for _, x := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			x.ts.In(m.reportLoc).Format("15:04"),
			strings.ToUpper(strings.TrimSpace(x.symbol)),
			strings.ToUpper(strings.TrimSpace(x.side)),
			strings.ToUpper(strings.TrimSpace(x.action)),
			x.qty, x.fill, x.pnl, x.pct, x.hold, strings.ToUpper(strings.TrimSpace(x.reason)))
	}
	return strings.TrimSpace(b.String()), true
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
		if atrPct := estimateATRPct(sym, 64, 14); atrPct > 0 {
			stopPct = clamp(atrPct*m.recoverATRMult, m.minStopPct/100.0, m.maxStopPct/100.0)
		}
		if strings.EqualFold(side, "BUY") {
			p.StopPrice = entry * (1 - stopPct)
		} else {
			p.StopPrice = entry * (1 + stopPct)
		}
		m.positions[sym] = p
		if m.tg != nil {
			m.tg.Sendf("%s", notify.BuildEventHTML("🧩", "ORPHAN RECOVERED",
				fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
				fmt.Sprintf("<b>Qty:</b> %.6f | <b>Entry:</b> %s", p.RemainingQty, fmtPrice(p.EntryPrice)),
			))
		}
		attachErr := m.placeOrReplaceStopWithRetry(p)
		if attachErr != nil && m.recoverForceFlatFail {
			if errClose := m.forceFlatRecovered(p); errClose == nil {
				p.State = execClosed
				p.CloseReason = "RECOVERY_FORCE_FLAT"
				p.ClosedAt = now
				p.UpdatedAt = now
				if m.tg != nil {
					m.tg.Sendf("%s", notify.BuildEventHTML("⚠️", "RECOVERY FORCE FLAT",
						fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
						"<b>Reason:</b> stop_attach_failed",
					))
				}
			}
		} else if attachErr == nil && m.tg != nil {
			m.tg.Sendf("%s", notify.BuildEventHTML("🛟", "EMERGENCY STOP ATTACHED",
				fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
				fmt.Sprintf("<b>Stop:</b> %s", fmtPrice(p.StopPrice)),
			))
		}
		importedRemote++
	}
	_ = m.save()
	return closedLocal, importedRemote, nil
}

func (m *liveExecManager) placeOrReplaceStopWithRetry(p *livePosition) error {
	var err error
	for i := 0; i < m.recoverStopRetries; i++ {
		err = m.placeOrReplaceStop(p)
		if err == nil {
			return nil
		}
		time.Sleep(m.recoverStopBackoff)
	}
	return err
}

func (m *liveExecManager) forceFlatRecovered(p *livePosition) error {
	if m == nil || m.rest == nil || p == nil || p.RemainingQty <= 0 {
		return fmt.Errorf("invalid recovered position")
	}
	meta, err := m.rest.SymbolMeta(p.Symbol, true)
	if err != nil {
		return err
	}
	closeSide := "SELL"
	if strings.EqualFold(p.Side, "SELL") {
		closeSide = "BUY"
	}
	qty, _, err := m.rest.RoundQty(p.Symbol, p.RemainingQty)
	if err != nil {
		return err
	}
	vals := url.Values{}
	vals.Set("symbol", p.Symbol)
	vals.Set("side", closeSide)
	vals.Set("type", "MARKET")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "true")
	vals.Set("quantity", formatFloat(qty, meta.QtyPrecision))
	_, err = m.rest.PlaceOrder(vals)
	return err
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

func (m *liveExecManager) StopoutCountSince(symbol string, since time.Time) int {
	if m == nil {
		return 0
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if raw == "" {
		return 0
	}
	n := 0
	for _, p := range m.positions {
		if p == nil {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(p.Symbol)) != raw {
			continue
		}
		if p.State != execClosed || p.ClosedAt.IsZero() || p.ClosedAt.Before(since) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(p.CloseReason), "SL") {
			n++
		}
	}
	return n
}

func (m *liveExecManager) HadRecentStopLoss(symbol, side string, now time.Time, cooldown time.Duration) bool {
	if m == nil || cooldown <= 0 {
		return false
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	cutoff := now.Add(-cooldown)
	for _, p := range m.positions {
		if p == nil || p.State != execClosed || p.ClosedAt.IsZero() || p.ClosedAt.Before(cutoff) {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(p.Symbol)) != raw {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(p.CloseReason), "STOP_HIT") {
			continue
		}
		if side == "" || strings.EqualFold(strings.TrimSpace(side), strings.TrimSpace(p.Side)) {
			return true
		}
	}
	return false
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
		Symbol:         rawSym,
		Side:           strings.ToUpper(c.Side),
		State:          execPendingEntry,
		CreatedAt:      now,
		UpdatedAt:      now,
		EntryOrderID:   orderID,
		EntryPrice:     price,
		Qty:            qty,
		Margin:         margin,
		Leverage:       lev,
		VPSetup:        c.Sig.VPSetup,
		VPLevel:        c.Sig.VPLevel,
		VPTargetLevel:  c.Sig.VPTargetLevel,
		VPStopMode:     c.Sig.StopMode,
		VPTargetMode:   c.Sig.TargetMode,
		RejectReason:   c.RejectReason,
		EntryReason:    c.Strat,
		EntryConf:      c.Conf,
		EntryTags:      append([]string{}, c.Sig.Tags...),
		EntryReasons:   append([]string{}, c.Sig.Reasons...),
		EntryVolumeUSD: c.VolumeUSD,
		RegimeTag:      c.Sig.RegimeTag,
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
		m.tg.Sendf("%s", notify.BuildEventHTML("📨", "ENTRY SUBMITTED",
			fmt.Sprintf("<b>%s %s</b>", rawSym, p.Side),
			fmt.Sprintf("<b>Qty:</b> %s | <b>Limit:</b> %s", vals.Get("quantity"), vals.Get("price")),
			fmt.Sprintf("<b>Order ID:</b> %d", orderID),
		))
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
			m.tg.Sendf("%s", notify.BuildEventHTML("✅", "ENTRY FILLED",
				fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
				fmt.Sprintf("<b>Qty:</b> %.6f | <b>Avg:</b> %s", p.FilledQty, fmtPrice(p.EntryPrice)),
				fmt.Sprintf("<b>Setup:</b> <code>%s</code> | <b>Conf:</b> %.2f", p.EntryReason, p.EntryConf),
			))
		}
		_ = m.logFill(now, p, "ENTRY", "ENTRY_FILLED", p.FilledQty, p.EntryPrice, 0, 0)
		m.sendFillReceipt(now, p, "ENTRY", "ENTRY_FILLED", p.FilledQty, p.EntryPrice, 0, 0)
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
			m.tg.Sendf("%s", notify.BuildEventHTML("⌛", "ENTRY TIMEOUT",
				fmt.Sprintf("<b>%s</b>", p.Symbol),
				fmt.Sprintf("<b>Order ID:</b> %d", p.EntryOrderID),
			))
		}
		_ = m.logFill(now, p, "ENTRY", "ENTRY_TIMEOUT", 0, 0, 0, 0)
		m.sendFillReceipt(now, p, "ENTRY", "ENTRY_TIMEOUT", 0, 0, 0, 0)
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
			if p.LastMark > 0 && abs(mark-p.LastMark)/maxFloat(p.EntryPrice, 1e-9) < 0.0006 {
				p.StallBars++
			} else {
				p.StallBars = 0
			}
			p.LastMark = mark
			updateFavorableRLive(p, mark)
			tp1R := tp1RFromBracket(p.EntryPrice, p.StopPrice, p.TP1Price)
			beArmR := beArmThreshold(envFloat("LIVE_BE_ARM_R", 0.5), tp1R)
			if m.beLockBps > 0 && beArmR > 0 && p.MaxFavorableR >= beArmR {
				be := beLockPrice(p.Side, p.EntryPrice, m.beLockBps)
				if (strings.EqualFold(p.Side, "BUY") && be > p.StopPrice) || (strings.EqualFold(p.Side, "SELL") && be < p.StopPrice) {
					p.StopPrice = be
					if err := m.placeOrReplaceStop(p); err == nil {
						changed = true
					}
				}
			}
			updated, err := m.updateTrailingStop(p, mark)
			if err != nil {
				return changed, err
			}
			if updated {
				changed = true
			}
			if m.exitManager != nil {
				mv := m.exitManager.EvaluateProtect(exitmgr.ProtectInput{
					Side:          p.Side,
					Entry:         p.EntryPrice,
					Stop:          p.StopPrice,
					Mark:          mark,
					MFER:          p.MaxFavorableR,
					MAER:          p.MaxAdverseR,
					BarsHeld:      int(now.Sub(p.CreatedAt) / time.Minute),
					StallBars:     p.StallBars,
					NearFriction:  p.VPTargetLevel > 0 && abs(mark-p.VPTargetLevel)/maxFloat(mark, 1e-9) < 0.002,
					UnrealizedPct: abs((mark-p.EntryPrice)/maxFloat(p.EntryPrice, 1e-9)) * 100,
				})
				if mv.MoveStopToBE {
					be := beLockPrice(p.Side, p.EntryPrice, m.beLockBps)
					if (strings.EqualFold(p.Side, "BUY") && be > p.StopPrice) || (strings.EqualFold(p.Side, "SELL") && be < p.StopPrice) {
						p.StopPrice = be
						_ = m.placeOrReplaceStop(p)
						changed = true
					}
				}
				if mv.TightenStop {
					if (strings.EqualFold(p.Side, "BUY") && mv.TightenToPrice > p.StopPrice) || (strings.EqualFold(p.Side, "SELL") && mv.TightenToPrice < p.StopPrice) {
						p.StopPrice = mv.TightenToPrice
						_ = m.placeOrReplaceStop(p)
						changed = true
					}
				}
				if mv.FullExit {
					_ = m.cancelRemainingExits(p)
					if err := m.closeSymbolMarket(p.Symbol); err == nil {
						p.State = execClosed
						p.CloseReason = mv.Reason
						p.ClosedAt = now
						p.UpdatedAt = now
						changed = true
					}
				}
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
				m.tg.Sendf("%s", notify.BuildEventHTML("📪", "POSITION CLOSED",
					fmt.Sprintf("<b>%s</b>", p.Symbol),
					fmt.Sprintf("<b>Reason:</b> %s", p.CloseReason),
				))
			}
			px := p.LastMark
			if px <= 0 {
				px = p.EntryPrice
			}
			pnl, pct := realizedFromFill(p.Side, p.EntryPrice, px, p.RemainingQty)
			_ = m.logFill(now, p, "CLOSE", p.CloseReason, p.RemainingQty, px, pnl, pct)
			m.sendFillReceipt(now, p, "CLOSE", p.CloseReason, p.RemainingQty, px, pnl, pct)
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
				m.tg.Sendf("%s", notify.BuildEventHTML("✅", "TP1 HIT",
					fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
					fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", execQty, fmtPrice(fillPx)),
					fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%) | <b>Day:</b> %+.2f", pnl, pct, dayRealized),
				))
			}
			_ = m.logFill(now, p, "TP", "TP1_HIT", execQty, fillPx, pnl, pct)
			m.sendFillReceipt(now, p, "TP", "TP1_HIT", execQty, fillPx, pnl, pct)
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
				m.tg.Sendf("%s", notify.BuildEventHTML("✅", "TP2 HIT",
					fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
					fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", execQty, fmtPrice(fillPx)),
					fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%) | <b>Day:</b> %+.2f", pnl, pct, dayRealized),
				))
			}
			_ = m.logFill(now, p, "TP", "TP2_HIT", execQty, fillPx, pnl, pct)
			m.sendFillReceipt(now, p, "TP", "TP2_HIT", execQty, fillPx, pnl, pct)
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
				m.tg.Sendf("%s", notify.BuildEventHTML("✅", "TP3 HIT",
					fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
					fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", execQty, fmtPrice(fillPx)),
					fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%) | <b>Day:</b> %+.2f", pnl, pct, dayRealized),
				))
			}
			_ = m.logFill(now, p, "TP", "TP3_HIT", execQty, fillPx, pnl, pct)
			m.sendFillReceipt(now, p, "TP", "TP3_HIT", execQty, fillPx, pnl, pct)
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
				m.tg.Sendf("%s", notify.BuildEventHTML("🛑", "STOP HIT",
					fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
					fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", execQty, fmtPrice(fillPx)),
					fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%) | <b>Day:</b> %+.2f", pnl, pct, dayRealized),
				))
			}
			_ = m.logFill(now, p, "STOP", "STOP_HIT", execQty, fillPx, pnl, pct)
			m.sendFillReceipt(now, p, "STOP", "STOP_HIT", execQty, fillPx, pnl, pct)
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
	tp3R := m.tp3R
	if p.CustomTP1R > 0 {
		tp1R = p.CustomTP1R
	}
	if p.CustomTP2R > 0 {
		tp2R = p.CustomTP2R
	}
	stopPct, tp1R, tp2R, tp3R = adjustBracketParams(
		p.EntryReason,
		p.EntryConf,
		p.EntryVolumeUSD,
		stopPct,
		tp1R,
		tp2R,
		tp3R,
		m.minStopPct/100.0,
		m.maxStopPct/100.0,
	)
	if sideBuy {
		p.StopPrice = p.EntryPrice * (1 - stopPct)
		p.TP1Price = p.EntryPrice * (1 + stopPct*tp1R)
		p.TP2Price = p.EntryPrice * (1 + stopPct*tp2R)
		p.TP3Price = p.EntryPrice * (1 + stopPct*tp3R)
	} else {
		p.StopPrice = p.EntryPrice * (1 + stopPct)
		p.TP1Price = p.EntryPrice * (1 - stopPct*tp1R)
		p.TP2Price = p.EntryPrice * (1 - stopPct*tp2R)
		p.TP3Price = p.EntryPrice * (1 - stopPct*tp3R)
	}
	if m.exitManager != nil {
		p.TP1Price = m.exitManager.FrontRunTarget(p.Side, p.TP1Price, p.VPTargetLevel)
		p.TP2Price = m.exitManager.FrontRunTarget(p.Side, p.TP2Price, p.VPTargetLevel)
		p.TP3Price = m.exitManager.FrontRunTarget(p.Side, p.TP3Price, p.VPTargetLevel)
	}
	p.TP1Price, p.TP2Price, p.TP3Price = enforceTPProgression(p.Side, p.TP1Price, p.TP2Price, p.TP3Price)
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
	// Prevent duplicated TP levels after exchange tick rounding.
	if p.TP3Qty > 0 {
		tp2Rounded, _, err2 := m.rest.RoundPrice(p.Symbol, p.TP2Price)
		if err2 == nil {
			tp3Rounded, _, err3 := m.rest.RoundPrice(p.Symbol, p.TP3Price)
			if err3 == nil && tp2Rounded > 0 && tp3Rounded > 0 && tp2Rounded == tp3Rounded {
				p.TP2Qty += p.TP3Qty
				p.TP3Qty = 0
				p.TP3OrderID = 0
			}
		}
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
	out, err := m.rest.ReplaceStopOrder(
		p.Symbol,
		closeSide,
		p.StopOrderID,
		qty,
		stopPx,
		meta.QtyPrecision,
		meta.PricePrecision,
	)
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
		m.tg.Sendf("%s", notify.BuildEventHTML("📈", "TRAIL MOVE",
			fmt.Sprintf("<b>%s</b>", p.Symbol),
			fmt.Sprintf("<b>Stop:</b> %s | <b>Mark:</b> %s", fmtPrice(p.StopPrice), fmtPrice(mark)),
		))
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
			m.tg.Sendf("%s", notify.BuildEventHTML("⚠️", "FORCED CLOSE",
				fmt.Sprintf("<b>%s %s</b>", sym, p.Side),
				fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", p.RemainingQty, fmtPrice(mark)),
				fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%)", pnl, pct),
				fmt.Sprintf("<b>Reason:</b> %s | <b>Day:</b> %+.2f", reason, dayRealized),
			))
		}
		_ = m.logFill(now, p, "FORCE_CLOSE", reason, p.RemainingQty, mark, pnl, pct)
		m.sendFillReceipt(now, p, "FORCE_CLOSE", reason, p.RemainingQty, mark, pnl, pct)
		p.State = execClosed
		p.CloseReason = reason
		p.ClosedAt = now
		p.UpdatedAt = now
	}
	_ = m.save()
	return nil
}

func (m *liveExecManager) ForceCloseSymbol(symbol, reason string) (bool, error) {
	if m == nil || m.rest == nil {
		return false, nil
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if raw == "" {
		return false, fmt.Errorf("symbol required")
	}
	p, ok := m.positions[raw]
	if !ok || p == nil || p.State == execClosed {
		return false, nil
	}
	now := time.Now().UTC()
	mark, _ := m.currentMark(raw)
	if mark <= 0 {
		mark = p.EntryPrice
	}
	pnl, pct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
	dayRealized := m.addDayRealized(now, pnl)
	_ = m.cancelRemainingExits(p)
	_, _ = m.rest.CancelAllOrders(raw)
	_ = m.closeSymbolMarket(raw)
	if m.tg != nil {
		m.tg.Sendf("%s", notify.BuildEventHTML("⚠️", "FORCED CLOSE",
			fmt.Sprintf("<b>%s %s</b>", raw, p.Side),
			fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", p.RemainingQty, fmtPrice(mark)),
			fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%)", pnl, pct),
			fmt.Sprintf("<b>Reason:</b> %s | <b>Day:</b> %+.2f", reason, dayRealized),
		))
	}
	_ = m.logFill(now, p, "FORCE_CLOSE", reason, p.RemainingQty, mark, pnl, pct)
	m.sendFillReceipt(now, p, "FORCE_CLOSE", reason, p.RemainingQty, mark, pnl, pct)
	p.State = execClosed
	p.CloseReason = reason
	p.ClosedAt = now
	p.UpdatedAt = now
	_ = m.save()
	return true, nil
}

func (m *liveExecManager) ApplyMomentumExit(now time.Time, mom map[string]momentumView, ext map[string]flowfeed.ExternalSignal) {
	if m == nil || m.rest == nil || !envBool("LIVE_MOMENTUM_EXIT_ENABLE", true) || len(m.positions) == 0 {
		return
	}
	slopeMax := envFloat("LIVE_MOMENTUM_EXIT_SLOPE_MAX", 0.0)
	minHold := time.Duration(envInt("LIVE_MOMENTUM_EXIT_MIN_HOLD_MIN", 10)) * time.Minute
	minUpnlPct := envFloat("LIVE_MOMENTUM_EXIT_MIN_UPNL_PCT", 0.25)
	minMFER := envFloat("LIVE_MOMENTUM_EXIT_MIN_MFE_R", 0.80)
	changed := false
	for sym, p := range m.positions {
		if p == nil || p.State == execClosed || p.RemainingQty <= 0 {
			continue
		}
		mv := mom[sym]
		if !shouldExitOnMomentumFade(p.Side, mv, slopeMax) {
			continue
		}
		if minHold > 0 && now.Sub(p.CreatedAt) < minHold {
			continue
		}
		if minMFER > 0 && p.MaxFavorableR < minMFER {
			continue
		}
		mark := p.LastMark
		if mark <= 0 {
			px, err := m.currentMark(sym)
			if err != nil || px <= 0 {
				continue
			}
			mark = px
		}
		_, upct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
		if upct < minUpnlPct {
			continue
		}
		if m.exitManager != nil {
			dec := m.exitManager.EvaluateProtect(exitmgr.ProtectInput{
				Side:          p.Side,
				Entry:         p.EntryPrice,
				Stop:          p.StopPrice,
				Mark:          mark,
				MFER:          p.MaxFavorableR,
				MAER:          p.MaxAdverseR,
				BarsHeld:      int(now.Sub(p.CreatedAt) / time.Minute),
				StallBars:     p.StallBars,
				WeakFlow:      shouldExitOnMomentumFade(p.Side, mv, slopeMax),
				LiqSpike:      ext[sym].LiqSpike,
				UnrealizedPct: upct,
			})
			if dec.PartialExitPct > 0 && p.RemainingQty > 0 {
				q := p.RemainingQty * dec.PartialExitPct
				if q > 0 && q < p.RemainingQty {
					if err := m.closeSymbolMarketQty(sym, q); err == nil {
						p.RemainingQty -= q
					}
				}
			}
			if dec.MoveStopToBE {
				be := beLockPrice(p.Side, p.EntryPrice, m.beLockBps)
				if (strings.EqualFold(p.Side, "BUY") && be > p.StopPrice) || (strings.EqualFold(p.Side, "SELL") && be < p.StopPrice) {
					p.StopPrice = be
					_ = m.placeOrReplaceStop(p)
				}
			}
		}
		pnl, pct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
		dayRealized := m.addDayRealized(now, pnl)
		_ = m.cancelRemainingExits(p)
		if err := m.closeSymbolMarket(sym); err != nil {
			continue
		}
		p.State = execClosed
		p.CloseReason = "MOMENTUM_FADE"
		p.ClosedAt = now
		p.UpdatedAt = now
		if m.tg != nil {
			m.tg.Sendf("%s", notify.BuildEventHTML(exitAlertEmoji("MOMENTUM_FADE"), "MOMENTUM EXIT",
				fmt.Sprintf("<b>%s %s</b>", sym, p.Side),
				fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", p.RemainingQty, fmtPrice(mark)),
				fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%) | <b>Day:</b> %+.2f", pnl, pct, dayRealized),
			))
		}
		_ = m.logFill(now, p, "CLOSE", "MOMENTUM_FADE", p.RemainingQty, mark, pnl, pct)
		m.sendFillReceipt(now, p, "CLOSE", "MOMENTUM_FADE", p.RemainingQty, mark, pnl, pct)
		changed = true
	}
	if changed {
		_ = m.save()
	}
}

func (m *liveExecManager) ApplyPreEODExit(now time.Time, mom map[string]momentumView, minHold time.Duration, upnlPctMax float64) {
	if m == nil || m.rest == nil || len(m.positions) == 0 {
		return
	}
	changed := false
	for sym, p := range m.positions {
		if p == nil || p.State == execClosed || p.RemainingQty <= 0 {
			continue
		}
		if minHold > 0 && now.Sub(p.CreatedAt) < minHold {
			continue
		}
		mark := p.LastMark
		if mark <= 0 {
			px, err := m.currentMark(sym)
			if err != nil || px <= 0 {
				continue
			}
			mark = px
		}
		_, upct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
		mv := mom[sym]
		reason := preEODExitReason(p.Side, mv, upct, upnlPctMax)
		if reason == "" {
			continue
		}
		pnl, pct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
		dayRealized := m.addDayRealized(now, pnl)
		_ = m.cancelRemainingExits(p)
		if err := m.closeSymbolMarket(sym); err != nil {
			continue
		}
		p.State = execClosed
		p.CloseReason = reason
		p.ClosedAt = now
		p.UpdatedAt = now
		if m.tg != nil {
			m.tg.Sendf("%s", notify.BuildEventHTML(exitAlertEmoji(reason), "PRE-EOD EXIT",
				fmt.Sprintf("<b>%s %s</b>", sym, p.Side),
				fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", p.RemainingQty, fmtPrice(mark)),
				fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%)", pnl, pct),
				fmt.Sprintf("<b>Reason:</b> %s | <b>Day:</b> %+.2f", reason, dayRealized),
			))
		}
		_ = m.logFill(now, p, "CLOSE", reason, p.RemainingQty, mark, pnl, pct)
		m.sendFillReceipt(now, p, "CLOSE", reason, p.RemainingQty, mark, pnl, pct)
		changed = true
	}
	if changed {
		_ = m.save()
	}
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

func (m *liveExecManager) closeSymbolMarketQty(symbol string, qty float64) error {
	if qty <= 0 {
		return nil
	}
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
		meta, err := m.rest.SymbolMeta(symbol, true)
		if err != nil {
			return err
		}
		q, _, err := m.rest.RoundQty(symbol, min(abs(amt), qty))
		if err != nil {
			return err
		}
		if q <= 0 {
			continue
		}
		vals := url.Values{}
		vals.Set("symbol", symbol)
		vals.Set("side", side)
		vals.Set("type", "MARKET")
		vals.Set("positionSide", "BOTH")
		vals.Set("reduceOnly", "true")
		vals.Set("quantity", formatFloat(q, meta.QtyPrecision))
		if _, err := m.rest.PlaceOrder(vals); err != nil {
			return err
		}
		return nil
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

func (p *paperTrader) PositionsTable(meta map[string]symbolMeta) string {
	if p == nil || !p.enabled {
		return ""
	}
	type row struct {
		sym    string
		side   string
		margin float64
		size   float64
		entry  float64
		mark   float64
		upnl   float64
		lev    int
	}
	rows := make([]row, 0, len(p.positions))
	totalUPnL := 0.0
	totalMargin := 0.0
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		m := meta[raw]
		mark := m.LastPrice
		upnl := 0.0
		if mark > 0 {
			if strings.EqualFold(pos.Side, "BUY") {
				upnl = (mark - pos.Entry) * pos.Qty
			} else {
				upnl = (pos.Entry - mark) * pos.Qty
			}
		}
		totalUPnL += upnl
		totalMargin += pos.Margin
		rows = append(rows, row{
			sym:    raw,
			side:   pos.Side,
			margin: pos.Margin,
			size:   pos.Qty,
			entry:  pos.Entry,
			mark:   mark,
			upnl:   upnl,
			lev:    maxInt(pos.Leverage, 1),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].upnl > rows[j].upnl })
	var b strings.Builder
	b.WriteString("paper active positions:\n")
	b.WriteString("symbol      side   margin    size      entry      mark       uPnL      lev\n")
	b.WriteString("----------------------------------------------------------------------------\n")
	dayKey := time.Now().In(p.reportLoc).Format("2006-01-02")
	realizedToday := 0.0
	if ds := p.dayStats[dayKey]; ds != nil {
		realizedToday = ds.Net
	}
	if len(rows) == 0 {
		b.WriteString("(none)")
		fmt.Fprintf(&b, "\ntotals: margin=$%.2f openPnL=%+.2f realizedToday=%+.2f netDay=%+.2f",
			0.0, 0.0, realizedToday, realizedToday)
		return b.String()
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "%-10s %-6s $%-8.2f %-9.4f %-10s %-10s %+.2f    %dx\n",
			r.sym, r.side, r.margin, r.size, fmtPrice(r.entry), fmtPrice(r.mark), r.upnl, r.lev)
	}
	fmt.Fprintf(&b, "totals: margin=$%.2f openPnL=%+.2f realizedToday=%+.2f netDay=%+.2f",
		totalMargin, totalUPnL, realizedToday, realizedToday+totalUPnL)
	return strings.TrimSpace(b.String())
}

func buildWindowReport(label string, now time.Time, p *paperTrader, meta map[string]symbolMeta, longInPlay, shortInPlay []inplay.Entry, topN int) string {
	if p == nil || !p.enabled {
		return notify.BuildSessionPulseHTML(notify.PulseSnapshot{
			Title:     strings.TrimSpace(label),
			TimeLabel: now.Format("15:04 MST"),
			Session:   sessionTag(now),
		})
	}
	pulse, cards := buildPaperPulseAndCards(strings.TrimSpace(label), now, p, meta)
	longTop, shortTop, bias := topScanSnapshot(longInPlay, shortInPlay, 2)
	var b strings.Builder
	b.WriteString(notify.BuildSessionPulseHTML(pulse))
	for _, c := range cards {
		b.WriteString("\n\n")
		b.WriteString(notify.BuildPositionCard(c))
	}
	b.WriteString("\n\n")
	b.WriteString(notify.BuildScannerSnapshotHTML(longTop, shortTop, bias))
	return strings.TrimSpace(b.String())
}

func (p *paperTrader) Equity(meta map[string]symbolMeta) float64 {
	if p == nil || !p.enabled {
		return 0
	}
	openPnL := 0.0
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		mark := meta[raw].LastPrice
		if mark <= 0 {
			continue
		}
		if strings.EqualFold(pos.Side, "BUY") {
			openPnL += (mark - pos.Entry) * pos.Qty
		} else {
			openPnL += (pos.Entry - mark) * pos.Qty
		}
	}
	return p.balance + openPnL
}

func (p *paperTrader) ApplyPayout(amount float64) float64 {
	if p == nil || !p.enabled || amount <= 0 {
		return 0
	}
	if amount > p.balance {
		amount = p.balance
	}
	if amount <= 0 {
		return 0
	}
	p.balance -= amount
	_ = p.save()
	return amount
}

func (p *paperTrader) OpenSymbols() []string {
	if p == nil || len(p.positions) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.positions))
	for raw := range p.positions {
		out = append(out, raw)
	}
	return out
}

func (p *paperTrader) freeForEntries() float64 {
	if p == nil {
		return 0
	}
	used := 0.0
	for _, pos := range p.positions {
		if pos == nil {
			continue
		}
		used += pos.Margin
	}
	free := p.balance - p.reserve - used
	if free < 0 {
		return 0
	}
	return free
}

func (p *paperTrader) ApplyFunding(now time.Time, meta map[string]symbolMeta) {
	if p == nil || !p.enabled || len(p.positions) == 0 || p.fundingEvery <= 0 {
		return
	}
	for raw, pos := range p.positions {
		if pos == nil || pos.Qty <= 0 {
			continue
		}
		interval := p.fundingEvery
		if p.fundingBySym != nil {
			if d, ok := p.fundingBySym[raw]; ok && d > 0 {
				interval = d
			}
		}
		slot := now.UTC().Truncate(interval).Format(time.RFC3339)
		key := raw + "|" + slot
		if p.lastFundKey[key] != "" {
			continue
		}
		m := meta[raw]
		mark := m.LastPrice
		if m.Bid > 0 && m.Ask > 0 {
			mark = (m.Bid + m.Ask) / 2.0
		}
		if m.FundingRate == 0 || mark <= 0 {
			continue
		}
		notional := mark * pos.Qty
		if notional <= 0 {
			continue
		}
		// Positive funding: longs pay, shorts receive. Negative funding: inverse.
		cost := notional * m.FundingRate
		net := 0.0
		if strings.EqualFold(pos.Side, "BUY") {
			net = -cost
		} else {
			net = cost
		}
		if net != 0 {
			p.balance += net
			p.recordDayStat(now, "FUNDING", net, 0, net)
		}
		p.lastFundKey[key] = "1"
	}
	_ = p.save()
}

func (p *paperTrader) hadRecentStopLoss(symbol string, now time.Time, cooldown time.Duration) bool {
	if p == nil || cooldown <= 0 {
		return false
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	t := p.lastExitAt[raw]
	if t.IsZero() {
		return false
	}
	if !p.lastExitLoss[raw] {
		return false
	}
	return now.Sub(t) < cooldown
}

func (p *paperTrader) MaybeEnter(now time.Time, c candidate, entryBps, margin float64, leverage int, meta map[string]symbolMeta, depth map[string]aster.OrderBook) (*paperPosition, error) {
	if p == nil || !p.enabled {
		return nil, nil
	}
	raw := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	if len(p.positions) >= p.maxOpen {
		return nil, fmt.Errorf("max paper positions reached (%d)", p.maxOpen)
	}
	free := p.freeForEntries()
	if free < margin {
		return nil, fmt.Errorf("insufficient usable paper balance")
	}
	if _, exists := p.positions[raw]; exists {
		return nil, fmt.Errorf("symbol already open")
	}
	if t := p.lockUntil[raw]; !t.IsZero() && now.Before(t) {
		return nil, fmt.Errorf("symbol loss lock active")
	}
	if p.lossCooldown > 0 {
		if t := p.lastExitAt[raw]; !t.IsZero() && p.lastExitLoss[raw] && now.Sub(t) < p.lossCooldown {
			return nil, fmt.Errorf("symbol loss cooldown active")
		}
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
	ob := depth[raw]
	fillPx := paperSimFillPrice(strings.ToUpper(c.Side), qty, m, ob, data.CurrentRegimeCT(now), true)
	if fillPx > 0 {
		entry = fillPx
	}
	if p.stressRoundtripBps > 0 {
		entry = applyBpsAdverse(entry, strings.ToUpper(c.Side), p.stressRoundtripBps)
	}
	feeRate := p.feeRateBpsForReason("ENTRY")
	entryFee := (entry * qty) * feeRate / 10000.0
	if strings.EqualFold(p.openCostMode, "aster") {
		refMark := m.LastPrice
		if m.Bid > 0 && m.Ask > 0 {
			refMark = (m.Bid + m.Ask) / 2.0
		}
		if refMark <= 0 {
			refMark = entry
		}
		openLoss := 0.0
		if strings.EqualFold(c.Side, "BUY") {
			openLoss = math.Max(0, (entry-refMark)*qty)
		} else {
			openLoss = math.Max(0, (refMark-entry)*qty)
		}
		required := margin + entryFee + openLoss
		if free < required {
			return nil, fmt.Errorf("paper open cost %.4f exceeds free %.4f", required, free)
		}
	} else if free < margin+entryFee {
		return nil, fmt.Errorf("paper margin+fee exceeds free balance")
	}
	stopPct := p.stopPct / 100.0
	stopPct = clamp(stopPct, p.minStopPct/100.0, p.maxStopPct/100.0)
	tp1R := p.tp1R
	tp2R := p.tp2R
	tp3R := p.tp3R
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
	stopPct, tp1R, tp2R, tp3R = adjustBracketParams(
		c.Strat,
		c.Conf,
		m.VolumeUSD,
		stopPct,
		tp1R,
		tp2R,
		tp3R,
		p.minStopPct/100.0,
		p.maxStopPct/100.0,
	)
	tp1Pct := stopPct * tp1R
	tp2Pct := stopPct * tp2R
	tp3Pct := stopPct * tp3R
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
	if p.exitManager != nil {
		tp1 = p.exitManager.FrontRunTarget(c.Side, tp1, c.Sig.VPTargetLevel)
		tp2 = p.exitManager.FrontRunTarget(c.Side, tp2, c.Sig.VPTargetLevel)
		tp3 = p.exitManager.FrontRunTarget(c.Side, tp3, c.Sig.VPTargetLevel)
	}
	tp1, tp2, tp3 = enforceTPProgression(c.Side, tp1, tp2, tp3)
	if stop <= 0 || tp1 <= 0 || tp2 <= 0 || tp3 <= 0 {
		return nil, fmt.Errorf("invalid paper bracket levels")
	}
	risk := abs(entry - stop)
	reward := abs(tp1 - entry)
	if risk <= 0 || reward/risk < p.minTP1RR {
		return nil, fmt.Errorf("paper tp1 rr below minimum")
	}
	p.balance -= entryFee
	pos := &paperPosition{
		Symbol:           raw,
		Side:             strings.ToUpper(c.Side),
		Entry:            entry,
		Qty:              qty,
		InitialQty:       qty,
		Margin:           margin,
		Leverage:         lev,
		Stop:             stop,
		TP1:              tp1,
		TP2:              tp2,
		TP3:              tp3,
		TrailRef:         entry,
		OpenedAt:         now,
		EntryReason:      c.Strat,
		OpposingFriction: c.Sig.VPTargetLevel,
	}
	p.positions[raw] = pos
	_ = p.save()
	fmt.Printf("paper entered %s %s entry=%.6f qty=%.6f lev=%dx tp1=%.6f tp2=%.6f tp3=%.6f sl=%.6f fee=%.4f\n",
		raw, c.Side, entry, qty, lev, tp1, tp2, tp3, stop, entryFee)
	return pos, nil
}

func (p *paperTrader) CheckExit(now time.Time, meta map[string]symbolMeta, depth map[string]aster.OrderBook) {
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
		if pos.LastMark > 0 && abs(mark-pos.LastMark)/maxFloat(pos.Entry, 1e-9) < 0.0006 {
			pos.StallBars++
		} else {
			pos.StallBars = 0
		}
		pos.LastMark = mark
		updateFavorableRPaper(pos, mark)
		tp1R := tp1RFromBracket(pos.Entry, pos.Stop, pos.TP1)
		beArmR := beArmThreshold(envFloat("LIVE_PAPER_BE_ARM_R", 0.5), tp1R)
		if p.beLockBps > 0 && beArmR > 0 && pos.MaxFavorableR >= beArmR {
			be := beLockPrice(pos.Side, pos.Entry, p.beLockBps)
			if (sideBuy && be > pos.Stop) || (!sideBuy && be < pos.Stop) {
				pos.Stop = be
			}
		}
		frTP1 := pos.TP1
		frTP2 := pos.TP2
		frTP3 := pos.TP3
		if p.exitManager != nil {
			frTP1 = p.exitManager.FrontRunTarget(pos.Side, pos.TP1, pos.OpposingFriction)
			frTP2 = p.exitManager.FrontRunTarget(pos.Side, pos.TP2, pos.OpposingFriction)
			frTP3 = p.exitManager.FrontRunTarget(pos.Side, pos.TP3, pos.OpposingFriction)
			dec := p.exitManager.EvaluateProtect(exitmgr.ProtectInput{
				Side:          pos.Side,
				Entry:         pos.Entry,
				Stop:          pos.Stop,
				Mark:          mark,
				MFER:          pos.MaxFavorableR,
				MAER:          pos.MaxAdverseR,
				BarsHeld:      int(now.Sub(pos.OpenedAt) / time.Minute),
				StallBars:     pos.StallBars,
				NearFriction:  p.hitPrice(sideBuy, mark, pos.OpposingFriction),
				UnrealizedPct: abs((mark-pos.Entry)/maxFloat(pos.Entry, 1e-9)) * 100,
			})
			if dec.MoveStopToBE {
				be := beLockPrice(pos.Side, pos.Entry, p.beLockBps)
				if (sideBuy && be > pos.Stop) || (!sideBuy && be < pos.Stop) {
					pos.Stop = be
				}
			}
			if dec.TightenStop {
				if (sideBuy && dec.TightenToPrice > pos.Stop) || (!sideBuy && dec.TightenToPrice < pos.Stop) {
					pos.Stop = dec.TightenToPrice
				}
			}
			if dec.FullExit {
				p.exitPortion(now, pos, dec.Reason, mark, pos.Qty, meta[raw], depth[raw])
				continue
			}
		}

		// 1) Hard stop has highest priority.
		if (sideBuy && mark <= pos.Stop) || (!sideBuy && mark >= pos.Stop) {
			p.exitPortion(now, pos, "SL", pos.Stop, pos.Qty, meta[raw], depth[raw])
			continue
		}

		// 2) Scale-out targets.
		if !pos.HitTP1 && p.hitPrice(sideBuy, mark, frTP1) {
			q := p.targetQty(pos.InitialQty, p.tp1Frac, pos.Qty)
			p.exitPortion(now, pos, "TP1", frTP1, q, meta[raw], depth[raw])
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
		if !pos.HitTP2 && p.hitPrice(sideBuy, mark, frTP2) {
			q := p.targetQty(pos.InitialQty, p.tp2Frac, pos.Qty)
			p.exitPortion(now, pos, "TP2", frTP2, q, meta[raw], depth[raw])
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
		if !pos.HitTP3 && p.hitPrice(sideBuy, mark, frTP3) {
			q := p.targetQty(pos.InitialQty, p.tp3Frac, pos.Qty)
			p.exitPortion(now, pos, "TP3", frTP3, q, meta[raw], depth[raw])
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
				p.exitPortion(now, pos, "TRAIL_STOP", pos.TrailStop, pos.Qty, meta[raw], depth[raw])
				continue
			}
		}

		if pos.Qty <= 1e-10 {
			delete(p.positions, raw)
		}
	}
}

func (p *paperTrader) ApplyMomentumExit(now time.Time, mom map[string]momentumView, meta map[string]symbolMeta, depth map[string]aster.OrderBook, ext map[string]flowfeed.ExternalSignal) {
	if p == nil || !p.enabled || !envBool("LIVE_MOMENTUM_EXIT_ENABLE", true) || len(p.positions) == 0 {
		return
	}
	slopeMax := envFloat("LIVE_MOMENTUM_EXIT_SLOPE_MAX", 0.0)
	minHold := time.Duration(envInt("LIVE_MOMENTUM_EXIT_MIN_HOLD_MIN", 10)) * time.Minute
	minUpnlPct := envFloat("LIVE_MOMENTUM_EXIT_MIN_UPNL_PCT", 0.25)
	minMFER := envFloat("LIVE_MOMENTUM_EXIT_MIN_MFE_R", 0.80)
	changed := false
	for raw, pos := range p.positions {
		if pos == nil || pos.Qty <= 0 {
			continue
		}
		mv := mom[raw]
		if !shouldExitOnMomentumFade(pos.Side, mv, slopeMax) {
			continue
		}
		if minHold > 0 && now.Sub(pos.OpenedAt) < minHold {
			continue
		}
		if minMFER > 0 && pos.MaxFavorableR < minMFER {
			continue
		}
		m := meta[raw]
		mark := m.LastPrice
		if mark <= 0 {
			mark = pos.LastMark
		}
		if mark <= 0 {
			continue
		}
		_, upnlPct := realizedFromFill(pos.Side, pos.Entry, mark, pos.Qty)
		if upnlPct < minUpnlPct {
			continue
		}
		if p.exitManager != nil {
			dec := p.exitManager.EvaluateProtect(exitmgr.ProtectInput{
				Side:          pos.Side,
				Entry:         pos.Entry,
				Stop:          pos.Stop,
				Mark:          mark,
				MFER:          pos.MaxFavorableR,
				MAER:          pos.MaxAdverseR,
				BarsHeld:      int(now.Sub(pos.OpenedAt) / time.Minute),
				StallBars:     pos.StallBars,
				WeakFlow:      shouldExitOnMomentumFade(pos.Side, mv, slopeMax),
				LiqSpike:      ext[raw].LiqSpike,
				UnrealizedPct: upnlPct,
			})
			if dec.MoveStopToBE {
				be := beLockPrice(pos.Side, pos.Entry, p.beLockBps)
				if (strings.EqualFold(pos.Side, "BUY") && be > pos.Stop) || (!strings.EqualFold(pos.Side, "BUY") && be < pos.Stop) {
					pos.Stop = be
				}
			}
			if dec.PartialExitPct > 0 && pos.Qty > 0 {
				q := pos.Qty * dec.PartialExitPct
				if q > 0 && q < pos.Qty {
					p.exitPortion(now, pos, "SOFT_LIQ_SPIKE_PARTIAL", mark, q, m, depth[raw])
					changed = true
					continue
				}
			}
			if dec.FullExit {
				p.exitPortion(now, pos, dec.Reason, mark, pos.Qty, m, depth[raw])
				changed = true
				continue
			}
		}
		p.exitPortion(now, pos, "MOMENTUM_FADE", mark, pos.Qty, m, depth[raw])
		changed = true
	}
	if changed {
		_ = p.save()
	}
}

func (p *paperTrader) ApplyPreEODExit(now time.Time, mom map[string]momentumView, meta map[string]symbolMeta, depth map[string]aster.OrderBook, minHold time.Duration, upnlPctMax float64) {
	if p == nil || !p.enabled || len(p.positions) == 0 {
		return
	}
	changed := false
	for raw, pos := range p.positions {
		if pos == nil || pos.Qty <= 0 {
			continue
		}
		if minHold > 0 && now.Sub(pos.OpenedAt) < minHold {
			continue
		}
		m := meta[raw]
		mark := m.LastPrice
		if mark <= 0 {
			mark = pos.LastMark
		}
		if mark <= 0 {
			continue
		}
		_, upnlPct := realizedFromFill(pos.Side, pos.Entry, mark, pos.Qty)
		mv := mom[raw]
		reason := preEODExitReason(pos.Side, mv, upnlPct, upnlPctMax)
		if reason == "" {
			continue
		}
		p.exitPortion(now, pos, reason, mark, pos.Qty, m, depth[raw])
		changed = true
	}
	if changed {
		_ = p.save()
	}
}

func (p *paperTrader) ForceCloseAll(now time.Time, meta map[string]symbolMeta, depth map[string]aster.OrderBook, reason string) {
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
		p.exitPortion(now, pos, reason, mark, pos.Qty, meta[raw], depth[raw])
	}
	_ = p.save()
}

func (p *paperTrader) ForceCloseSymbol(now time.Time, symbol string, meta map[string]symbolMeta, depth map[string]aster.OrderBook, reason string) bool {
	if p == nil || !p.enabled {
		return false
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	pos := p.positions[raw]
	if pos == nil {
		return false
	}
	mark := meta[raw].LastPrice
	if mark <= 0 {
		mark = pos.Entry
	}
	p.exitPortion(now, pos, reason, mark, pos.Qty, meta[raw], depth[raw])
	_ = p.save()
	return true
}

func (p *paperTrader) exitPortion(now time.Time, pos *paperPosition, reason string, exitPrice, qty float64, m symbolMeta, ob aster.OrderBook) {
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
	fillPx := paperSimFillPrice(exitSideForPosition(pos.Side), qty, m, ob, data.CurrentRegimeCT(now), false)
	if fillPx > 0 {
		exitPrice = fillPx
	}
	gross := 0.0
	if strings.EqualFold(pos.Side, "BUY") {
		gross = (exitPrice - pos.Entry) * qty
	} else {
		gross = (pos.Entry - exitPrice) * qty
	}
	notional := exitPrice * qty
	feeRate := p.feeRateBpsForReason(reason)
	fee := notional * feeRate / 10000.0
	net := gross - fee
	pct := 0.0
	if pos.Entry > 0 {
		if strings.EqualFold(pos.Side, "BUY") {
			pct = ((exitPrice - pos.Entry) / pos.Entry) * 100.0
		} else {
			pct = ((pos.Entry - exitPrice) / pos.Entry) * 100.0
		}
	}
	pos.Realized += net
	p.balance += net
	p.recordDayStat(now, reason, gross, fee, net)
	if p.lastExitAt != nil {
		p.lastExitAt[symbol] = now
	}
	if p.lastExitLoss != nil {
		p.lastExitLoss[symbol] = net < 0
	}
	if net < 0 {
		if p.lossStreak != nil {
			p.lossStreak[symbol] = p.lossStreak[symbol] + 1
			if p.maxLossStreak > 0 && p.lossLock > 0 && p.lossStreak[symbol] >= p.maxLossStreak && p.lockUntil != nil {
				p.lockUntil[symbol] = now.Add(p.lossLock)
			}
		}
	} else {
		if p.lossStreak != nil {
			p.lossStreak[symbol] = 0
		}
		if p.lockUntil != nil {
			delete(p.lockUntil, symbol)
		}
	}
	pos.Qty -= qty
	if pos.Qty < 0 {
		pos.Qty = 0
	}
	holdMin := now.Sub(pos.OpenedAt).Minutes()
	fmt.Printf("paper exit %s %s reason=%s qty=%.6f entry=%.6f exit=%.6f pnl=%+.4f realized=%+.4f rem=%.6f balance=%.2f hold=%.1fm\n",
		symbol, pos.Side, reason, qty, pos.Entry, exitPrice, net, pos.Realized, pos.Qty, p.balance, holdMin)
	if p.onExit != nil {
		loc := p.reportLoc
		if loc == nil {
			loc = time.Local
		}
		dayKey := now.In(loc).Format("2006-01-02")
		realizedToday := 0.0
		if ds := p.dayStats[dayKey]; ds != nil {
			realizedToday = ds.Net
		}
		reasonU := strings.ToUpper(strings.TrimSpace(reason))
		p.onExit(fmt.Sprintf(
			"%s <b>PAPER EXIT | %s %s</b>\n• <b>Qty:</b> %.6f | <b>Exit:</b> %s\n• <b>PnL:</b> %+.2f (%+.2f%%)\n• <b>Reason:</b> %s | <b>Hold:</b> %.1fm\n• <b>Remaining:</b> %.6f | <b>Session:</b> %s\n• <b>Realized Today:</b> %+.2f | <b>Balance:</b> $%.2f",
			exitAlertEmoji(reasonU), symbol, pos.Side, qty, fmtPrice(exitPrice), net, pct, reasonU, holdMin, pos.Qty, sessionTag(now.In(loc)),
			realizedToday, p.balance,
		))
	}
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
		sym    string
		side   string
		entry  float64
		mark   float64
		qty    float64
		upnl   float64
		margin float64
		lev    int
		upct   float64
		ageMin int
		reason string
	}
	rows := make([]row, 0, len(p.positions))
	totalUPnL := 0.0
	for sym, pos := range p.positions {
		mark := meta[sym].LastPrice
		upnl := 0.0
		upct := 0.0
		if strings.EqualFold(pos.Side, "BUY") {
			upnl = (mark - pos.Entry) * pos.Qty
			if pos.Entry > 0 {
				upct = ((mark - pos.Entry) / pos.Entry) * 100
			}
		} else {
			upnl = (pos.Entry - mark) * pos.Qty
			if pos.Entry > 0 {
				upct = ((pos.Entry - mark) / pos.Entry) * 100
			}
		}
		totalUPnL += upnl
		reason := strings.TrimSpace(pos.EntryReason)
		if reason == "" {
			reason = "-"
		}
		rows = append(rows, row{
			sym:    sym,
			side:   pos.Side,
			entry:  pos.Entry,
			mark:   mark,
			qty:    pos.Qty,
			upnl:   upnl,
			margin: pos.Margin,
			lev:    maxInt(pos.Leverage, 1),
			upct:   upct,
			ageMin: int(time.Since(pos.OpenedAt).Minutes()),
			reason: reason,
		})
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
	nowLocal := time.Now().In(p.reportLoc)
	fmt.Fprintf(&b, "Paper Update (%s) session=%s\n", nowLocal.Format("15:04 MST"), sessionTag(nowLocal))
	fmt.Fprintf(&b, "bal=$%.2f eq=$%.2f realized=%+.2f openPnL=%+.2f netDay=%+.2f open=%d/%d\n",
		p.balance, eq, realizedToday, totalUPnL, realizedToday+totalUPnL, len(p.positions), p.maxOpen)
	if len(rows) == 0 {
		b.WriteString("open: none")
		return b.String()
	}
	b.WriteString("| Sym | Side | Margin | Qty | Entry | Mark | Lev | uPnL | uPnL% | Age(m) | Reason |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | $%.2f | %.4f | %s | %s | %dx | %+.2f | %+.2f%% | %d | %s |\n",
			r.sym, r.side, r.margin, r.qty, fmtPrice(r.entry), fmtPrice(r.mark), r.lev, r.upnl, r.upct, r.ageMin, r.reason)
	}
	fmt.Fprintf(&b, "\nTotals: openPnL=%+.2f realizedToday=%+.2f netDay=%+.2f", totalUPnL, realizedToday, realizedToday+totalUPnL)
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

func (p *paperTrader) DailyReceiptMessage(dayKey string, limit int) (string, bool) {
	if p == nil || !p.enabled || strings.TrimSpace(p.tradesCSV) == "" {
		return "", false
	}
	if limit <= 0 {
		limit = 25
	}
	f, err := os.Open(p.tradesCSV)
	if err != nil {
		return "", false
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil || len(header) == 0 {
		return "", false
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	req := []string{"exit_ts", "symbol", "side", "entry", "exit", "qty", "reason", "net_pnl", "hold_min"}
	for _, k := range req {
		if _, ok := idx[k]; !ok {
			return "", false
		}
	}
	type row struct {
		ts     time.Time
		symbol string
		side   string
		entry  string
		exit   string
		qty    string
		reason string
		net    string
		hold   string
	}
	rows := make([]row, 0, limit)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, rec[idx["exit_ts"]])
		if err != nil {
			continue
		}
		if ts.In(p.reportLoc).Format("2006-01-02") != dayKey {
			continue
		}
		rows = append(rows, row{
			ts:     ts,
			symbol: rec[idx["symbol"]],
			side:   rec[idx["side"]],
			entry:  rec[idx["entry"]],
			exit:   rec[idx["exit"]],
			qty:    rec[idx["qty"]],
			reason: rec[idx["reason"]],
			net:    rec[idx["net_pnl"]],
			hold:   rec[idx["hold_min"]],
		})
	}
	if len(rows) == 0 {
		return fmt.Sprintf("Paper Trade Receipt %s (%s)\nno trades", dayKey, p.reportLoc.String()), true
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ts.Before(rows[j].ts) })
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Paper Trade Receipt %s (%s)\n", dayKey, p.reportLoc.String())
	b.WriteString("| Time | Sym | Side | Qty | Entry | Exit | Net | Hold(m) | Reason |\n")
	b.WriteString("|---|---|---|---:|---:|---:|---:|---:|---|\n")
	for _, x := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			x.ts.In(p.reportLoc).Format("15:04"),
			strings.ToUpper(strings.TrimSpace(x.symbol)),
			strings.ToUpper(strings.TrimSpace(x.side)),
			x.qty, x.entry, x.exit, x.net, x.hold, strings.ToUpper(strings.TrimSpace(x.reason)))
	}
	return strings.TrimSpace(b.String()), true
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

func (p *paperTrader) feeRateBpsForReason(reason string) float64 {
	r := strings.ToUpper(strings.TrimSpace(reason))
	switch r {
	case "TP1", "TP2", "TP3":
		if p.makerFeeBps > 0 {
			return p.makerFeeBps
		}
	case "SL", "TRAIL_STOP", "EOD_FORCE_FLAT", "ENTRY_TIMEOUT":
		if p.takerFeeBps > 0 {
			return p.takerFeeBps
		}
	}
	if p.feeBps > 0 {
		return p.feeBps
	}
	if p.takerFeeBps > 0 {
		return p.takerFeeBps
	}
	return 0
}

func exitSideForPosition(posSide string) string {
	if strings.EqualFold(strings.TrimSpace(posSide), "BUY") {
		return "SELL"
	}
	return "BUY"
}

func paperSimFillPrice(side string, qty float64, m symbolMeta, ob aster.OrderBook, regime data.Regime, isEntry bool) float64 {
	if qty <= 0 {
		return m.LastPrice
	}
	topBid, topAsk := topOfBook(ob, m.LastPrice)
	mid := m.LastPrice
	if topBid > 0 && topAsk > 0 {
		mid = (topBid + topAsk) / 2.0
	}
	if mid <= 0 {
		return 0
	}
	vwap, ok := vwapFromDepth(side, qty, ob)
	if ok && vwap > 0 {
		return applyRegimeSlip(vwap, side, regime, mid, m.VolumeUSD, isEntry)
	}
	baseSpread := 0.0
	if topBid > 0 && topAsk > 0 {
		baseSpread = (topAsk - topBid) / mid
	}
	if baseSpread <= 0 {
		baseSpread = 0.0004
	}
	fill := mid
	if strings.EqualFold(side, "BUY") {
		fill = mid * (1 + baseSpread/2.0)
	} else {
		fill = mid * (1 - baseSpread/2.0)
	}
	return applyRegimeSlip(fill, side, regime, mid, m.VolumeUSD, isEntry)
}

func applyRegimeSlip(px float64, side string, regime data.Regime, mid, volUSD float64, isEntry bool) float64 {
	if px <= 0 || mid <= 0 {
		return px
	}
	mult := 1.0
	switch regime {
	case data.OverlapAE, data.OverlapEUUS:
		mult = 0.75
	case data.RegimeEU, data.RegimeUS, data.RegimeAsia:
		mult = 1.0
	default:
		mult = 1.35
	}
	impactBps := 2.0 * mult
	if volUSD > 0 {
		if volUSD < 5_000_000 {
			impactBps += 4.0 * mult
		} else if volUSD < 20_000_000 {
			impactBps += 2.0 * mult
		}
	}
	if !isEntry {
		impactBps += 0.5 * mult
	}
	d := impactBps / 10000.0
	if strings.EqualFold(side, "BUY") {
		return px * (1 + d)
	}
	return px * (1 - d)
}

func applyBpsAdverse(px float64, side string, bps float64) float64 {
	if px <= 0 || bps <= 0 {
		return px
	}
	d := bps / 10000.0
	if strings.EqualFold(side, "BUY") {
		return px * (1 + d)
	}
	return px * (1 - d)
}

func topOfBook(ob aster.OrderBook, fallback float64) (bid, ask float64) {
	if len(ob.Bids) > 0 {
		bid = ob.Bids[0][0]
	}
	if len(ob.Asks) > 0 {
		ask = ob.Asks[0][0]
	}
	if bid <= 0 || ask <= 0 {
		if fallback > 0 {
			return fallback * 0.9995, fallback * 1.0005
		}
	}
	return bid, ask
}

func vwapFromDepth(side string, qty float64, ob aster.OrderBook) (float64, bool) {
	levels := ob.Asks
	if !strings.EqualFold(side, "BUY") {
		levels = ob.Bids
	}
	if len(levels) == 0 {
		return 0, false
	}
	remaining := qty
	totalQty := 0.0
	totalNotional := 0.0
	for _, lv := range levels {
		price := lv[0]
		levelQty := lv[1]
		if price <= 0 || levelQty <= 0 {
			continue
		}
		take := levelQty
		if take > remaining {
			take = remaining
		}
		totalQty += take
		totalNotional += price * take
		remaining -= take
		if remaining <= 1e-12 {
			break
		}
	}
	if totalQty <= 0 {
		return 0, false
	}
	avg := totalNotional / totalQty
	if remaining > 1e-12 {
		last := levels[len(levels)-1][0]
		if last <= 0 {
			last = avg
		}
		// Penalize remainder beyond visible depth.
		worse := last
		if strings.EqualFold(side, "BUY") {
			worse = last * 1.0015
		} else {
			worse = last * 0.9985
		}
		avg = ((avg * totalQty) + (worse * remaining)) / (totalQty + remaining)
	}
	return avg, true
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
	if p != nil && p.eventLog != nil {
		pnlPct := 0.0
		if entry > 0 {
			if strings.EqualFold(side, "BUY") {
				pnlPct = ((exit - entry) / entry) * 100.0
			} else {
				pnlPct = ((entry - exit) / entry) * 100.0
			}
		}
		riskR := 0.0
		if entry > 0 && stop > 0 {
			risk := math.Abs(entry-stop) * qty
			if risk > 0 {
				riskR = net / risk
			}
		}
		p.eventLog.Emit(stats.Event{
			Timestamp: now,
			Type:      "POSITION_CLOSE",
			Simulated: true,
			Symbol:    symbol,
			Side:      side,
			TF:        "1m",
			Strategy:  reason,
			EntryPx:   entry,
			ExitPx:    exit,
			RiskR:     riskR,
			PnLUSD:    net,
			PnLPct:    pnlPct,
			Fees:      fee,
			Reason:    reason,
		})
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

func chooseCandidates(longInPlay, shortInPlay []inplay.Entry, minGrade string, enableMomentumReversal bool, reversalMinGrade string, reversalSlopeMin float64, bNearAOnly bool, bNearAScoreMin float64, reversalTopLongN int) []candidate {
	minVal := gradeValue(minGrade)
	revMinVal := gradeValue(reversalMinGrade)
	if reversalSlopeMin < 0 {
		reversalSlopeMin = -reversalSlopeMin
	}
	allow := func(e inplay.Entry) bool {
		if !bNearAOnly {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(e.CurrentGrade), "B") && e.CurrentScore < bNearAScoreMin {
			return false
		}
		return true
	}
	out := make([]candidate, 0, len(longInPlay)+len(shortInPlay))
	for _, e := range longInPlay {
		if !allow(e) {
			continue
		}
		if e.State == inplay.StateExhausted {
			continue
		}
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
		if !allow(e) {
			continue
		}
		if e.State == inplay.StateExhausted {
			continue
		}
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
	if enableMomentumReversal {
		n := minInt(reversalTopLongN, len(longInPlay))
		for i := 0; i < n; i++ {
			e := longInPlay[i]
			if !allow(e) || gradeValue(e.CurrentGrade) < revMinVal {
				continue
			}
			flip := e
			flip.Rank = e.Rank + 8 + 4*abs(e.ScoreSlope)
			out = append(out, candidate{
				Entry: flip,
				Side:  "SELL",
				Strat: "mom_reversal_short",
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entry.Rank > out[j].Entry.Rank })
	return out
}

func passesAsiaEntryQuality(now time.Time, c candidate, asiaMinGrade string, asiaStrongConfMin float64) bool {
	if data.CurrentRegimeCT(now) != data.RegimeAsia {
		return true
	}
	if gradeValue(c.Entry.CurrentGrade) >= gradeValue(asiaMinGrade) {
		return true
	}
	return c.Conf >= asiaStrongConfMin
}

func rankWithStrategy(c *aster.Client, in []candidate, topN int, stopMode, targetMode string, vpMinTargetPct float64, inertiaEnable bool, inertiaScoreMin, inertiaSlowMin, inertiaFastMax float64, inertiaSlowN, inertiaFastN int, reversalVolSpike float64) []candidate {
	if len(in) == 0 {
		return in
	}
	out := make([]candidate, len(in))
	copy(out, in)
	if topN > len(out) {
		topN = len(out)
	}
	for i := 0; i < topN; i++ {
		out[i] = enrichCandidate(c, out[i], stopMode, targetMode, vpMinTargetPct, inertiaEnable, inertiaScoreMin, inertiaSlowMin, inertiaFastMax, inertiaSlowN, inertiaFastN, reversalVolSpike)
	}
	for i := topN; i < len(out); i++ {
		if strings.EqualFold(out[i].Strat, "mom_reversal_short") {
			out[i] = enrichCandidate(c, out[i], stopMode, targetMode, vpMinTargetPct, inertiaEnable, inertiaScoreMin, inertiaSlowMin, inertiaFastMax, inertiaSlowN, inertiaFastN, reversalVolSpike)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		ri := out[i].Entry.Rank * (1 + out[i].Conf)
		rj := out[j].Entry.Rank * (1 + out[j].Conf)
		return ri > rj
	})
	return out
}

func enrichCandidate(c *aster.Client, cand candidate, stopMode, targetMode string, vpMinTargetPct float64, inertiaEnable bool, inertiaScoreMin, inertiaSlowMin, inertiaFastMax float64, inertiaSlowN, inertiaFastN int, reversalVolSpike float64) candidate {
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
	cand.LastClose = fc[len(fc)-1].C
	cand.EMA9 = emaLast(fc, 9)
	cand.SessionVWAP = sessionVWAP(fc)
	cand.SlowSlope = closeSlopePct(fc, inertiaSlowN)
	cand.FastSlope = closeSlopePct(fc, inertiaFastN)
	if inertiaEnable &&
		strings.EqualFold(cand.Side, "BUY") &&
		strings.EqualFold(cand.Strat, "") &&
		cand.Entry.CurrentScore >= inertiaScoreMin &&
		cand.SlowSlope > inertiaSlowMin &&
		cand.FastSlope < inertiaFastMax {
		cand.Strat = "none"
		cand.Conf = 0
		cand.RejectReason = "STATE_INERTIA_KILL"
		return cand
	}
	if strings.EqualFold(cand.Strat, "mom_reversal_short") {
		lastVol := fc[len(fc)-1].V
		avgVol := smaVolume(fc, 20)
		volSpike := 0.0
		if avgVol > 0 {
			volSpike = lastVol / avgVol
		}
		if cand.LastClose < cand.EMA9 && volSpike >= reversalVolSpike {
			cand.Conf = 0.62 + min(0.18, (volSpike-reversalVolSpike)*0.05)
			cand.Sig = strategies.Signal{
				Active: true,
				Name:   "mom_reversal_short",
				Side:   features.SideShort,
			}
			cand.RejectReason = ""
			return cand
		}
		cand.Strat = "none"
		cand.Conf = 0
		cand.RejectReason = "mom_reversal_short_not_ready"
		return cand
	}
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
		StrategyWeight:            envFloat("LIVE_CONFLUENCE_STRATEGY_WEIGHT", 0.50),
		FlowWeight:                envFloat("LIVE_CONFLUENCE_FLOW_WEIGHT", 0.30),
		StructureWeight:           envFloat("LIVE_CONFLUENCE_STRUCTURE_WEIGHT", 0.20),
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
	if strings.EqualFold(cand.Side, "BUY") &&
		cand.Entry.State == inplay.StateCooling &&
		(cand.Strat == "failed_auction_magnet" || cand.Strat == "bos_pb" || cand.Strat == "fa") &&
		cand.SessionVWAP > 0 && cand.EMA9 > 0 &&
		cand.LastClose < cand.SessionVWAP && cand.LastClose < cand.EMA9 {
		cand.Strat = "none"
		cand.Conf = 0
		cand.RejectReason = "VWAP_EMA_LONG_INVALIDATION"
	}
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

func emaLast(c []features.Candle, n int) float64 {
	if len(c) == 0 {
		return 0
	}
	if n <= 1 {
		return c[len(c)-1].C
	}
	alpha := 2.0 / (float64(n) + 1.0)
	ema := c[0].C
	for i := 1; i < len(c); i++ {
		ema = alpha*c[i].C + (1-alpha)*ema
	}
	return ema
}

func sessionVWAP(c []features.Candle) float64 {
	dayKey := ""
	pv := 0.0
	vol := 0.0
	for i := 0; i < len(c); i++ {
		k := data.DayKeyNY17CT(c[i].Ts)
		if dayKey == "" {
			dayKey = k
		}
		if k != dayKey {
			pv = 0
			vol = 0
			dayKey = k
		}
		typ := (c[i].H + c[i].L + c[i].C) / 3.0
		pv += typ * c[i].V
		vol += c[i].V
	}
	if vol <= 0 {
		return 0
	}
	return pv / vol
}

func closeSlopePct(c []features.Candle, n int) float64 {
	if len(c) < 2 {
		return 0
	}
	if n < 2 {
		n = 2
	}
	if n > len(c)-1 {
		n = len(c) - 1
	}
	start := c[len(c)-1-n].C
	end := c[len(c)-1].C
	if start <= 0 || end <= 0 {
		return 0
	}
	return ((end / start) - 1.0) * 100.0
}

func smaVolume(c []features.Candle, n int) float64 {
	if len(c) == 0 {
		return 0
	}
	if n <= 0 || n > len(c) {
		n = len(c)
	}
	sum := 0.0
	for i := len(c) - n; i < len(c); i++ {
		sum += c[i].V
	}
	return sum / float64(n)
}

func estimateATRPct(symbol string, candlesN, atrN int) float64 {
	if candlesN < atrN+2 {
		candlesN = atrN + 2
	}
	bars, err := aster.LoadCandles(symbol, types.TF1m, candlesN)
	if err != nil || len(bars) < atrN+2 {
		return 0
	}
	trs := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		hi := bars[i].H
		lo := bars[i].L
		pc := bars[i-1].C
		tr := maxFloat(hi-lo, maxFloat(abs(hi-pc), abs(lo-pc)))
		trs = append(trs, tr)
	}
	if len(trs) < atrN {
		return 0
	}
	sum := 0.0
	for i := len(trs) - atrN; i < len(trs); i++ {
		sum += trs[i]
	}
	atr := sum / float64(atrN)
	lastClose := bars[len(bars)-1].C
	if atr <= 0 || lastClose <= 0 {
		return 0
	}
	return atr / lastClose
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

func buildMomentumIndex(longInPlay, shortInPlay []inplay.Entry) map[string]momentumView {
	out := make(map[string]momentumView, len(longInPlay)+len(shortInPlay))
	for i := range longInPlay {
		e := longInPlay[i]
		raw := strings.ToUpper(aster.RawSymbol(e.Symbol))
		if raw == "" {
			continue
		}
		mv := out[raw]
		mv.Long = &e
		out[raw] = mv
	}
	for i := range shortInPlay {
		e := shortInPlay[i]
		raw := strings.ToUpper(aster.RawSymbol(e.Symbol))
		if raw == "" {
			continue
		}
		mv := out[raw]
		mv.Short = &e
		out[raw] = mv
	}
	return out
}

func shouldExitOnMomentumFade(side string, mv momentumView, slopeMax float64) bool {
	if strings.EqualFold(side, "BUY") {
		if mv.Long == nil {
			return false
		}
		st := mv.Long.State
		return (st == inplay.StateCooling || st == inplay.StateDumping) && mv.Long.ScoreSlope <= slopeMax
	}
	if mv.Short == nil {
		return false
	}
	st := mv.Short.State
	return (st == inplay.StateCooling || st == inplay.StateDumping) && mv.Short.ScoreSlope <= slopeMax
}

func preEODExitReason(side string, mv momentumView, upnlPct, upnlPctMax float64) string {
	if shouldExitOnMomentumFade(side, mv, 0.0) {
		return "PRE_EOD_MOMENTUM_FADE"
	}
	if upnlPct <= upnlPctMax {
		return "PRE_EOD_WEAK_PNL"
	}
	return ""
}

func inPreEODEntryBlock(now time.Time, eod maintenanceWindow, blockMin int) bool {
	if blockMin <= 0 {
		return false
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), eod.StartHour, eod.StartMin, 0, 0, now.Location())
	if now.After(start) || now.Equal(start) {
		return false
	}
	return now.Add(time.Duration(blockMin) * time.Minute).After(start)
}

func hasRecentStopLoss(symbol, side string, now time.Time, cooldown time.Duration, paper *paperTrader, execMgr *liveExecManager) bool {
	if cooldown <= 0 {
		return false
	}
	if paper != nil && paper.hadRecentStopLoss(symbol, now, cooldown) {
		return true
	}
	if execMgr != nil && execMgr.HadRecentStopLoss(symbol, side, now, cooldown) {
		return true
	}
	return false
}

func orderbookSupportsEntry(ob aster.OrderBook, side string, levels int, minImb, maxSpreadBps float64) bool {
	ok, _, _, _ := orderbookEntryDecision(ob, side, levels, minImb, maxSpreadBps)
	return ok
}

func orderbookEntryDecision(ob aster.OrderBook, side string, levels int, minImb, maxSpreadBps float64) (bool, string, float64, float64) {
	if levels <= 0 {
		levels = 5
	}
	if minImb <= 0 {
		minImb = 1.05
	}
	if maxSpreadBps <= 0 {
		maxSpreadBps = 15
	}
	if len(ob.Bids) == 0 || len(ob.Asks) == 0 || ob.Bids[0][0] <= 0 || ob.Asks[0][0] <= 0 {
		return false, "orderbook_empty", 0, 0
	}
	mid := (ob.Bids[0][0] + ob.Asks[0][0]) / 2.0
	if mid <= 0 {
		return false, "orderbook_bad_mid", 0, 0
	}
	spreadBps := ((ob.Asks[0][0] - ob.Bids[0][0]) / mid) * 10000.0
	if spreadBps > maxSpreadBps {
		return false, "orderbook_spread", spreadBps, 0
	}
	nBid := minInt(levels, len(ob.Bids))
	nAsk := minInt(levels, len(ob.Asks))
	bidUSD := 0.0
	askUSD := 0.0
	for i := 0; i < nBid; i++ {
		bidUSD += ob.Bids[i][0] * ob.Bids[i][1]
	}
	for i := 0; i < nAsk; i++ {
		askUSD += ob.Asks[i][0] * ob.Asks[i][1]
	}
	if bidUSD <= 0 || askUSD <= 0 {
		return false, "orderbook_depth_zero", spreadBps, 0
	}
	imb := bidUSD / askUSD
	if strings.EqualFold(side, "SELL") {
		imb = askUSD / bidUSD
	}
	if imb < minImb {
		return false, "orderbook_imbalance", spreadBps, imb
	}
	return true, "", spreadBps, imb
}

func orderbookRiskMetrics(rawSymbol, side string, depth map[string]aster.OrderBook, meta map[string]symbolMeta, levels int) (float64, float64) {
	ob, ok := depth[rawSymbol]
	if ok && len(ob.Bids) > 0 && len(ob.Asks) > 0 && ob.Bids[0][0] > 0 && ob.Asks[0][0] > 0 {
		mid := (ob.Bids[0][0] + ob.Asks[0][0]) / 2.0
		if mid <= 0 {
			return 0, 0
		}
		spreadBps := ((ob.Asks[0][0] - ob.Bids[0][0]) / mid) * 10000.0
		nBid := minInt(levels, len(ob.Bids))
		nAsk := minInt(levels, len(ob.Asks))
		bidUSD := 0.0
		askUSD := 0.0
		for i := 0; i < nBid; i++ {
			bidUSD += ob.Bids[i][0] * ob.Bids[i][1]
		}
		for i := 0; i < nAsk; i++ {
			askUSD += ob.Asks[i][0] * ob.Asks[i][1]
		}
		if bidUSD <= 0 || askUSD <= 0 {
			return spreadBps, 0
		}
		if strings.EqualFold(side, "SELL") {
			return spreadBps, askUSD / bidUSD
		}
		return spreadBps, bidUSD / askUSD
	}
	m := meta[rawSymbol]
	if m.Bid > 0 && m.Ask > 0 {
		mid := (m.Bid + m.Ask) / 2.0
		if mid > 0 {
			return ((m.Ask - m.Bid) / mid) * 10000.0, 0
		}
	}
	return 0, 0
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
		entry := mapFloat(r["entryPrice"])
		lev := mapFloat(r["leverage"])
		margin := mapFloat(r["isolatedWallet"])
		if margin <= 0 {
			margin = mapFloat(r["positionInitialMargin"])
		}
		if margin <= 0 && entry > 0 && lev > 0 {
			margin = (entry * abs(amt)) / lev
		}
		snap.Positions = append(snap.Positions, positionView{
			Symbol:   strings.ToUpper(strings.TrimSpace(fmt.Sprint(r["symbol"]))),
			Side:     side,
			Margin:   margin,
			SizeAbs:  abs(amt),
			Entry:    entry,
			Mark:     mapFloat(r["markPrice"]),
			Unreal:   mapFloat(r["unRealizedProfit"]),
			Leverage: lev,
		})
	}
	sort.Slice(snap.Positions, func(i, j int) bool {
		return snap.Positions[i].Unreal > snap.Positions[j].Unreal
	})
	return snap, nil
}

func printAccountSnapshot(snap accountSnapshot, assets []string, realizedToday float64) {
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
	fmt.Println("symbol      side   margin    size      entry      mark       uPnL      lev")
	fmt.Println("----------------------------------------------------------------------------")
	if len(snap.Positions) == 0 {
		fmt.Println("(none)")
		fmt.Printf("totals: openPnL=%+.2f realizedToday=%+.2f netDay=%+.2f\n", 0.0, realizedToday, realizedToday)
		return
	}
	openPnL := 0.0
	totalMargin := 0.0
	for _, p := range snap.Positions {
		openPnL += p.Unreal
		totalMargin += p.Margin
		fmt.Printf("%-10s %-6s $%-8.2f %-9.4f %-10s %-10s %+.2f    %.0fx\n",
			p.Symbol, p.Side, p.Margin, p.SizeAbs, fmtPrice(p.Entry), fmtPrice(p.Mark), p.Unreal, p.Leverage)
	}
	fmt.Printf("totals: margin=$%.2f openPnL=%+.2f realizedToday=%+.2f netDay=%+.2f\n",
		totalMargin, openPnL, realizedToday, realizedToday+openPnL)
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
	maxOrders := envInt("LIVE_MAX_ORDERS_PER_DAY", 6)
	if maxOrders < 0 {
		maxOrders = 0
	}
	maxOrdersHour := envInt("LIVE_MAX_ORDERS_PER_HOUR", 2)
	if maxOrdersHour < 0 {
		maxOrdersHour = 0
	}
	coolSec := envInt("LIVE_ORDER_COOLDOWN_SEC", 180)
	if coolSec < 0 {
		coolSec = 0
	}
	symCoolSec := envInt("LIVE_SYMBOL_COOLDOWN_SEC", 900)
	if symCoolSec < 0 {
		symCoolSec = 0
	}
	maxDailyLossPct := envFloat("LIVE_MAX_DAILY_LOSS_PCT", 0)
	if maxDailyLossPct < 0 {
		maxDailyLossPct = 0
	}
	stopoutWindowMin := envInt("LIVE_SYMBOL_STOPOUT_WINDOW_MIN", 60)
	if stopoutWindowMin < 0 {
		stopoutWindowMin = 0
	}
	stopoutLockMin := envInt("LIVE_SYMBOL_STOPOUT_LOCK_MIN", 5)
	if stopoutLockMin < 0 {
		stopoutLockMin = 0
	}
	stopoutCount := envInt("LIVE_SYMBOL_STOPOUT_COUNT", 3)
	if stopoutCount < 0 {
		stopoutCount = 0
	}
	allow := envCSV("LIVE_ALLOW_SYMBOLS", "")
	allowMap := make(map[string]struct{}, len(allow))
	for _, s := range allow {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(s)))
		if raw != "" {
			allowMap[raw] = struct{}{}
		}
	}
	block := envCSV("LIVE_BLOCK_SYMBOLS", "")
	blockMap := make(map[string]struct{}, len(block))
	for _, s := range block {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(s)))
		if raw != "" {
			blockMap[raw] = struct{}{}
		}
	}
	return safetyConfig{
		enableLiveTrading: envBool("LIVE_ENABLE_LIVE_TRADING", false),
		maxLeverage:       maxLev,
		minAvailUSDT:      minAvail,
		maxOrdersPerDay:   maxOrders,
		maxOrdersPerHour:  maxOrdersHour,
		orderCooldown:     time.Duration(coolSec) * time.Second,
		symbolCooldown:    time.Duration(symCoolSec) * time.Second,
		stopoutWindow:     time.Duration(stopoutWindowMin) * time.Minute,
		stopoutLock:       time.Duration(stopoutLockMin) * time.Minute,
		stopoutCount:      stopoutCount,
		pauseFile:         envStr("LIVE_PAUSE_FILE", "/tmp/live-lite.pause"),
		allowSymbols:      allowMap,
		blockSymbols:      blockMap,
		allowShorts:       envBool("LIVE_ALLOW_SHORTS", true),
		maxDailyLossPct:   maxDailyLossPct,
		killClose:         envBool("LIVE_KILL_CLOSE_POSITIONS", false),
	}
}

func safetyReject(cfg safetyConfig, c candidate, now, lastOrderAt time.Time, lastBySymbol map[string]time.Time, byDay, byHour map[string]int, stopoutLockUntil map[string]time.Time) string {
	if cfg.pauseFile != "" {
		if _, err := os.Stat(cfg.pauseFile); err == nil {
			return "pause file present"
		}
	}
	if !cfg.allowShorts && strings.EqualFold(c.Side, "SELL") {
		return "shorts disabled"
	}
	sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if _, blocked := cfg.blockSymbols[sym]; blocked {
		return "symbol blocked"
	}
	if len(cfg.allowSymbols) > 0 {
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
	if cfg.stopoutCount > 0 && cfg.stopoutLock > 0 {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
		if t := stopoutLockUntil[raw]; !t.IsZero() && now.Before(t) {
			return fmt.Sprintf("symbol stopout lock active until %s", t.UTC().Format(time.RFC3339))
		}
	}
	if cfg.maxOrdersPerHour > 0 {
		hourKey := now.UTC().Format("2006-01-02T15")
		if byHour[hourKey] >= cfg.maxOrdersPerHour {
			return "max orders/hour reached"
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

func filterBlockedScored(rows []market.Scored, blocked map[string]struct{}) []market.Scored {
	if len(rows) == 0 || len(blocked) == 0 {
		return rows
	}
	out := make([]market.Scored, 0, len(rows))
	for _, r := range rows {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(r.Symbol)))
		if _, skip := blocked[raw]; skip {
			continue
		}
		out = append(out, r)
	}
	return out
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

func sessionTag(ts time.Time) string {
	return string(data.CurrentRegimeCT(ts))
}

func newPayoutManager() *payoutManager {
	enabled := envBool("LIVE_PAYOUT_ENABLE", true)
	locName := envStr("LIVE_PAYOUT_TZ", "America/Chicago")
	loc, err := time.LoadLocation(locName)
	if err != nil {
		loc = time.Local
	}
	pm := &payoutManager{
		enabled:        enabled,
		mode:           strings.ToLower(envStr("LIVE_PAYOUT_MODE", "telegram_alert")),
		onlyIfFlat:     envBool("LIVE_PAYOUT_ONLY_IF_FORCE_FLAT", true),
		notifyTelegram: envBool("LIVE_PAYOUT_NOTIFY_TELEGRAM", true),
		cycleDays:      envInt("LIVE_PAYOUT_CYCLE_DAYS", 1),
		anchorHour:     envInt("LIVE_PAYOUT_ANCHOR_HOUR", 16),
		anchorMin:      envInt("LIVE_PAYOUT_ANCHOR_MIN", 0),
		deadlineMin:    envInt("LIVE_PAYOUT_DEADLINE_MIN", 15),
		minPayoutUSDT:  envFloat("LIVE_PAYOUT_MIN_USDT", 1.0),
		keepTradingUSD: envFloat("LIVE_PAYOUT_KEEP_USDT", 0),
		stateFile:      resolveStatePath(envStr("LIVE_PAYOUT_STATE_FILE", "out/payout_state.json")),
		ledgerFile:     resolveStatePath(envStr("LIVE_PAYOUT_LEDGER_FILE", "out/payouts.csv")),
		loc:            loc,
	}
	if pm.cycleDays <= 0 {
		pm.cycleDays = 1
	}
	if pm.deadlineMin < 0 {
		pm.deadlineMin = 15
	}
	if pm.anchorHour < 0 || pm.anchorHour > 23 {
		pm.anchorHour = 16
	}
	if pm.anchorMin < 0 || pm.anchorMin > 59 {
		pm.anchorMin = 0
	}
	if pm.minPayoutUSDT < 0 {
		pm.minPayoutUSDT = 0
	}
	if pm.enabled {
		_ = pm.load()
	}
	return pm
}

func (pm *payoutManager) load() error {
	if pm == nil || !pm.enabled || strings.TrimSpace(pm.stateFile) == "" {
		return nil
	}
	b, err := os.ReadFile(pm.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var st payoutState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	pm.state = st
	return nil
}

func (pm *payoutManager) save() error {
	if pm == nil || !pm.enabled || strings.TrimSpace(pm.stateFile) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(pm.stateFile), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(pm.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pm.stateFile, b, 0o644)
}

func (pm *payoutManager) maybeRun(nowUTC, localNow time.Time, eod maintenanceWindow, ms *maintenanceState, paper *paperTrader, meta map[string]symbolMeta, acct accountSnapshot, execMgr *liveExecManager, tg *notify.Telegram) {
	if pm == nil || !pm.enabled {
		return
	}
	pm.ensureCycle(nowUTC, localNow, paper, meta, acct)
	if pm.state.CycleEnd.IsZero() {
		return
	}
	cycleEndLocal := pm.state.CycleEnd.In(pm.loc)
	if localNow.Before(cycleEndLocal) {
		return
	}
	closeDay := cycleEndLocal.Format("2006-01-02")
	if pm.state.RunState == payoutIdle {
		pm.state.RunState = payoutPendingClose
		pm.state.CycleCloseDate = closeDay
		pm.state.PendingSince = nowUTC
		pm.state.DeadlineAt = time.Date(cycleEndLocal.Year(), cycleEndLocal.Month(), cycleEndLocal.Day(), pm.anchorHour, pm.anchorMin, 0, 0, pm.loc).Add(time.Duration(pm.deadlineMin) * time.Minute).UTC()
		pm.state.ActionType = ""
		pm.state.ActionReason = ""
		_ = pm.save()
	}
	if pm.state.CycleCloseDate != closeDay {
		return
	}
	if pm.state.RunState == payoutDone || pm.state.RunState == payoutDoneFallback {
		return
	}

	flatConfirmed := pm.flatConfirmed(localNow, eod, ms, paper, acct, execMgr)
	deadlinePassed := !pm.state.DeadlineAt.IsZero() && !nowUTC.Before(pm.state.DeadlineAt)
	if pm.onlyIfFlat && !flatConfirmed && !deadlinePassed {
		return
	}

	closeEq := pm.currentEquity(paper, meta, acct)
	if pm.state.TradingBase <= 0 {
		pm.state.TradingBase = pm.initialTradingBase(pm.state.StartEquity)
	}
	newProfit := maxFloat(0, closeEq-pm.state.StartEquity)
	withdrawable := maxFloat(0, closeEq-pm.state.TradingBase)
	retained := closeEq
	payoutAmt := withdrawable
	if payoutAmt < pm.minPayoutUSDT {
		payoutAmt = 0
	}

	actionType := "NO_PAYOUT"
	actionReason := "NO_WITHDRAWABLE_PROFIT"
	runState := payoutDone
	if deadlinePassed && pm.onlyIfFlat && !flatConfirmed {
		runState = payoutDoneFallback
		actionReason = "FLAT_TIMEOUT_FALLBACK"
	} else if !flatConfirmed && pm.onlyIfFlat {
		actionReason = "WAITING_FORCE_FLAT"
	}

	if payoutAmt > 0 {
		if paper != nil && paper.enabled {
			debited := paper.ApplyPayout(payoutAmt)
			payoutAmt = debited
			actionType = "PAPER_DEBIT"
			actionReason = "WITHDRAW_NEW_PROFIT"
			if payoutAmt <= 0 {
				actionType = "NO_PAYOUT"
				actionReason = "PAPER_DEBIT_ZERO"
			}
		} else {
			actionType = "LIVE_ALERT"
			actionReason = "MANUAL_WITHDRAW_NEW_PROFIT"
		}
	}
	if runState == payoutDoneFallback && !strings.HasPrefix(actionType, "FALLBACK_") {
		actionType = "FALLBACK_" + actionType
	}

	pm.state.ActionAt = nowUTC
	pm.state.ActionType = actionType
	pm.state.ActionReason = actionReason
	pm.state.LastPayoutAt = nowUTC
	pm.state.LastPayoutAmt = payoutAmt
	pm.state.LastCycleProfit = newProfit
	pm.state.LastAction = actionType
	pm.state.RunState = runState
	execCycleID := pm.state.CycleID
	execStartEq := pm.state.StartEquity
	execDeadline := time.Date(cycleEndLocal.Year(), cycleEndLocal.Month(), cycleEndLocal.Day(), pm.anchorHour, pm.anchorMin, 0, 0, pm.loc).Add(time.Duration(pm.deadlineMin) * time.Minute)
	retained = closeEq - payoutAmt
	_ = pm.appendLedger(nowUTC, closeEq, newProfit, payoutAmt, retained, pm.state.TradingBase, actionType, actionReason)
	_ = pm.save()

	nextCloseLocal := cycleEndLocal.AddDate(0, 0, pm.cycleDays)
	pm.state.CycleStart = cycleEndLocal.UTC()
	pm.state.CycleEnd = nextCloseLocal.UTC()
	pm.state.CycleID = pm.cycleID(nextCloseLocal)
	pm.state.StartEquity = closeEq - payoutAmt
	if pm.state.StartEquity < 0 {
		pm.state.StartEquity = 0
	}
	pm.state.CycleCloseDate = ""
	pm.state.PendingSince = time.Time{}
	pm.state.DeadlineAt = time.Time{}
	pm.state.RunState = payoutIdle
	_ = pm.save()

	if tg != nil && tg.Enabled() && pm.notifyTelegram {
		prefix := "PAYOUT CYCLE CLOSE"
		if strings.HasPrefix(actionType, "FALLBACK_") {
			prefix = "PAYOUT DEADLINE FALLBACK"
		}
		msg := fmt.Sprintf("%s\ncycle_id=%s\nstart_equity=%.2f close_equity=%.2f locked_profit=%.2f payout_amount=%.2f\naction_type=%s reason=%s\nexecuted_at=%s deadline=%s\nnext_cycle_close=%s",
			prefix,
			execCycleID,
			execStartEq,
			closeEq,
			newProfit,
			payoutAmt,
			actionType,
			actionReason,
			nowUTC.In(pm.loc).Format("2006-01-02 15:04:05 MST"),
			execDeadline.Format("2006-01-02 15:04 MST"),
			nextCloseLocal.Format("2006-01-02 15:04 MST"),
		)
		msg += fmt.Sprintf("\ntrading_base=%.2f retained_for_next=%.2f withdrawable=%.2f", pm.state.TradingBase, retained, withdrawable)
		msg += fmt.Sprintf("\nsession=%s", sessionTag(nowUTC.In(pm.loc)))
		tg.Sendf("%s", notify.BuildEventHTML("💸", prefix,
			fmt.Sprintf("<b>Cycle ID:</b> %s", execCycleID),
			fmt.Sprintf("<b>Start Eq:</b> %.2f | <b>Close Eq:</b> %.2f", execStartEq, closeEq),
			fmt.Sprintf("<b>Locked Profit:</b> %.2f | <b>Payout:</b> %.2f", newProfit, payoutAmt),
			fmt.Sprintf("<b>Action:</b> %s | <b>Reason:</b> %s", actionType, actionReason),
			fmt.Sprintf("<b>Executed:</b> %s | <b>Deadline:</b> %s", nowUTC.In(pm.loc).Format("2006-01-02 15:04:05 MST"), execDeadline.Format("2006-01-02 15:04 MST")),
			fmt.Sprintf("<b>Next Close:</b> %s", nextCloseLocal.Format("2006-01-02 15:04 MST")),
			fmt.Sprintf("<b>Trading Base:</b> %.2f | <b>Retained:</b> %.2f | <b>Withdrawable:</b> %.2f", pm.state.TradingBase, retained, withdrawable),
			fmt.Sprintf("<b>Session:</b> %s", sessionTag(nowUTC.In(pm.loc))),
		))
	}
}

func (pm *payoutManager) ensureCycle(nowUTC, localNow time.Time, paper *paperTrader, meta map[string]symbolMeta, acct accountSnapshot) {
	if pm == nil {
		return
	}
	if pm.state.CycleID != "" && !pm.state.CycleEnd.IsZero() {
		return
	}
	nextClose := pm.nextCloseAfter(localNow)
	pm.state.CycleStart = nowUTC
	pm.state.CycleEnd = nextClose.UTC()
	pm.state.CycleID = pm.cycleID(nextClose)
	pm.state.StartEquity = pm.currentEquity(paper, meta, acct)
	if pm.state.TradingBase <= 0 {
		pm.state.TradingBase = pm.initialTradingBase(pm.state.StartEquity)
	}
	pm.state.RunState = payoutIdle
	_ = pm.save()
}

func (pm *payoutManager) initialTradingBase(currentEq float64) float64 {
	if pm == nil {
		return currentEq
	}
	if pm.keepTradingUSD > 0 {
		return pm.keepTradingUSD
	}
	return currentEq
}

func (pm *payoutManager) nextCloseAfter(localNow time.Time) time.Time {
	target := localNow.Add(time.Duration(pm.cycleDays) * 24 * time.Hour)
	closeAt := time.Date(target.Year(), target.Month(), target.Day(), pm.anchorHour, pm.anchorMin, 0, 0, pm.loc)
	if closeAt.Before(target) {
		closeAt = closeAt.Add(24 * time.Hour)
	}
	return closeAt
}

func (pm *payoutManager) cycleID(closeLocal time.Time) string {
	return fmt.Sprintf("%s@%02d%02d", closeLocal.Format("2006-01-02"), pm.anchorHour, pm.anchorMin)
}

func (pm *payoutManager) currentEquity(paper *paperTrader, meta map[string]symbolMeta, acct accountSnapshot) float64 {
	if paper != nil && paper.enabled {
		return paper.Equity(meta)
	}
	return accountEquity(acct)
}

func (pm *payoutManager) flatConfirmed(localNow time.Time, eod maintenanceWindow, ms *maintenanceState, paper *paperTrader, acct accountSnapshot, execMgr *liveExecManager) bool {
	dayKey := localNow.Format("2006-01-02")
	if ms != nil && ms.FlatDoneDay[eod.Name] != dayKey {
		return false
	}
	if paper != nil && paper.enabled && len(paper.positions) > 0 {
		return false
	}
	if execMgr != nil && execMgr.ActiveCount() > 0 {
		return false
	}
	if len(acct.Positions) > 0 {
		return false
	}
	return true
}

func (pm *payoutManager) appendLedger(nowUTC time.Time, closeEq, profit, payout, retained, base float64, actionType, reason string) error {
	if pm == nil || !pm.enabled || strings.TrimSpace(pm.ledgerFile) == "" {
		return nil
	}
	if err := ensureCSVWithHeader(pm.ledgerFile, []string{
		"ts", "cycle_id", "cycle_start", "cycle_end", "start_equity", "close_equity", "new_profit", "trading_base", "retained_for_next", "payout_amount", "action_type", "action_reason",
	}); err != nil {
		return err
	}
	f, err := os.OpenFile(pm.ledgerFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{
		nowUTC.Format(time.RFC3339),
		pm.state.CycleID,
		pm.state.CycleStart.Format(time.RFC3339),
		pm.state.CycleEnd.Format(time.RFC3339),
		fmt.Sprintf("%.8f", pm.state.StartEquity),
		fmt.Sprintf("%.8f", closeEq),
		fmt.Sprintf("%.8f", profit),
		fmt.Sprintf("%.8f", base),
		fmt.Sprintf("%.8f", retained),
		fmt.Sprintf("%.8f", payout),
		actionType,
		reason,
	})
	w.Flush()
	return w.Error()
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

func minInt(a, b int) int {
	if a < b {
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

func newReserveLockGate() *reserveLockGate {
	enabled := envBool("LIVE_RESERVE_LOCK_ENABLE", false)
	lossPct := envFloat("LIVE_RESERVE_LOCK_LOSS_PCT", 40.0)
	recoveryPct := envFloat("LIVE_RESERVE_LOCK_RECOVERY_PCT", 100.0)
	if lossPct < 0 {
		lossPct = 0
	}
	if lossPct > 100 {
		lossPct = 100
	}
	if recoveryPct < 0 {
		recoveryPct = 0
	}
	if recoveryPct > 200 {
		recoveryPct = 200
	}
	return &reserveLockGate{
		enabled:     enabled,
		lossPct:     lossPct,
		recoveryPct: recoveryPct,
	}
}

func (g *reserveLockGate) ensureTarget(base, reserve float64) {
	if g == nil || !g.enabled {
		return
	}
	if base > g.targetBase {
		g.targetBase = base
	}
	next := reserve
	// Fallback: 50% reserve target if reserve resolver returns 0.
	if next <= 0 && base > 0 {
		next = base * 0.5
	}
	if next <= 0 {
		return
	}
	// Ratchet-up behavior: scale with growth, don't loosen lock target on drawdown.
	if next > g.targetReserve {
		g.targetReserve = next
	}
}

func (g *reserveLockGate) block(base float64) bool {
	if g == nil || !g.enabled || g.targetReserve <= 0 {
		return false
	}
	targetBase := g.targetBase
	if targetBase <= 0 {
		targetBase = g.targetReserve * 2
	}
	usableTarget := targetBase - g.targetReserve
	lockAt := usableTarget + g.targetReserve*(1.0-g.lossPct/100.0)
	unlockAt := usableTarget + g.targetReserve*(g.recoveryPct/100.0)
	if unlockAt < lockAt {
		unlockAt = lockAt
	}
	if !g.locked && base <= lockAt {
		g.locked = true
	}
	if g.locked && base >= unlockAt {
		g.locked = false
	}
	return g.locked
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

func resolveStatePath(p string) string {
	s := strings.TrimSpace(p)
	if s == "" {
		return ""
	}
	if filepath.IsAbs(s) {
		return filepath.Clean(s)
	}
	base := strings.TrimSpace(os.Getenv("LIVE_STATE_DIR"))
	if base == "" {
		if exe, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exe)
			if filepath.Base(exeDir) == "bin" {
				exeDir = filepath.Dir(exeDir)
			}
			if !strings.HasPrefix(exeDir, os.TempDir()) {
				base = exeDir
			}
		}
	}
	if base == "" {
		if wd, err := os.Getwd(); err == nil {
			base = wd
		}
	}
	if base == "" {
		return filepath.Clean(s)
	}
	return filepath.Clean(filepath.Join(base, s))
}

func fileExists(path string) bool {
	p := strings.TrimSpace(path)
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
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

func resolvePaperFeeProfile(profile string) (makerBps, takerBps float64) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "vip":
		return 0.3, 3.0
	case "standard":
		return 1.0, 6.0
	case "pro":
		fallthrough
	default:
		return 0.5, 4.0
	}
}

func parseSymbolMinutesMap(raw string) map[string]time.Duration {
	out := map[string]time.Duration{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out
	}
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(kv[0])))
		if sym == "" {
			continue
		}
		mins, err := strconv.Atoi(strings.TrimSpace(kv[1]))
		if err != nil || mins <= 0 {
			continue
		}
		out[sym] = time.Duration(mins) * time.Minute
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

func newTelegramSink() *notify.Telegram {
	return notify.NewTelegramFromConfig(getConfigKV())
}

func tgPre(msg string) string {
	return notify.Pre(msg)
}

func (c *telegramCommandCtx) setMeta(meta map[string]symbolMeta) {
	if c == nil {
		return
	}
	cp := make(map[string]symbolMeta, len(meta))
	for k, v := range meta {
		cp[k] = v
	}
	c.metaMu.Lock()
	c.meta = cp
	c.metaMu.Unlock()
}

func (c *telegramCommandCtx) getMeta() map[string]symbolMeta {
	if c == nil {
		return nil
	}
	c.metaMu.RLock()
	defer c.metaMu.RUnlock()
	cp := make(map[string]symbolMeta, len(c.meta))
	for k, v := range c.meta {
		cp[k] = v
	}
	return cp
}

func (c *telegramCommandCtx) run() {
	if c == nil || c.tg == nil || !c.tg.Enabled() {
		return
	}
	c.tg.Listen(context.Background(), c.handleCommand)
}

func (c *telegramCommandCtx) handleCommand(_ string, msg string) string {
	rawMsg := strings.TrimSpace(msg)
	cmd := strings.ToLower(rawMsg)
	fields := strings.Fields(rawMsg)
	switch {
	case strings.HasPrefix(cmd, "/help"), strings.HasPrefix(cmd, "/start"):
		return strings.Join([]string{
			"Commands:",
			"/help - show this command guide",
			"/status - runtime snapshot (mode, top candidate, in-play counts, exec state)",
			"/balance - live account balances (all assets), available USDT, equityUSDTView, open positions",
			"/positions - open positions summary (paper table in dry-run, live tracked positions otherwise)",
			"/pause - pause new entries (risk management still runs)",
			"/resume - resume new entries",
			"/close SYMBOL - close one symbol now (live + paper paths)",
			"/closeall - close all open positions now",
		}, "\n")
	case strings.HasPrefix(cmd, "/status"):
		s := c.status.Snapshot()
		return fmt.Sprintf("status\ndry_run=%v live_enabled=%v\nlong_inplay=%d short_inplay=%d\ntop=%s %s g=%s s=%.2f\navailable=%.2f\npaper=%s\nexec_open=%d pending=%d partial1=%d partial2=%d",
			s.DryRun, s.LiveEnabled, s.LongInPlay, s.ShortInPlay,
			s.TopSymbol, s.TopSide, s.TopGrade, s.TopScore,
			s.AvailableUSDT,
			s.PaperSummary,
			s.Exec.Open, s.Exec.Pending, s.Exec.Partial1, s.Exec.Partial2)
	case strings.HasPrefix(cmd, "/balance"):
		if c.rest != nil {
			assets := envCSV("LIVE_ACCOUNT_ASSETS", "")
			snap, err := fetchAccountSnapshot(c.rest, assets)
			if err == nil {
				eq := accountEquity(snap)
				var b strings.Builder
				fmt.Fprintf(&b, "balance (live)\navailableUSDT=%.4f\nequityUSDTView=%.4f\nopen_positions=%d\nbalances:\n",
					snap.AvailableUSDT, eq, len(snap.Positions))
				if len(snap.Balances) == 0 {
					b.WriteString("(none)\n")
				} else {
					for _, x := range snap.Balances {
						fmt.Fprintf(&b, "- %-6s bal=%-12.6f avail=%-12.6f upnl=%-10.6f\n",
							strings.ToUpper(x.Asset), x.Balance, x.AvailableBalance, x.CrossUnPnl)
					}
				}
				return strings.TrimSpace(b.String())
			}
		}
		s := c.status.Snapshot()
		return fmt.Sprintf("balance\navailableUSDT=%.4f", s.AvailableUSDT)
	case strings.HasPrefix(cmd, "/positions"):
		if c.paper != nil && c.paper.enabled {
			meta := c.getMeta()
			return c.paper.PositionsTable(meta)
		}
		if c.execMgr != nil {
			ex := c.execMgr.Snapshot(10)
			var b strings.Builder
			fmt.Fprintf(&b, "live positions: open=%d pending=%d active=%d\n", ex.Open, ex.Pending, len(ex.Active))
			for i, p := range ex.Active {
				if i >= 10 {
					break
				}
				fmt.Fprintf(&b, "%d) %s %s qty=%.6f entry=%s stop=%s reason=%s\n",
					i+1, p.Symbol, p.Side, p.RemainingQty, fmtPrice(p.EntryPrice), fmtPrice(p.StopPrice), p.EntryReason)
			}
			return strings.TrimSpace(b.String())
		}
		return "positions unavailable"
	case strings.HasPrefix(cmd, "/pause"):
		if c.safety.pauseFile == "" {
			return "pause file is not configured"
		}
		_ = os.WriteFile(c.safety.pauseFile, []byte(time.Now().Format(time.RFC3339)+" manual pause\n"), 0o644)
		return "entries paused"
	case strings.HasPrefix(cmd, "/resume"):
		if c.safety.pauseFile == "" {
			return "pause file is not configured"
		}
		_ = os.Remove(c.safety.pauseFile)
		return "entries resumed"
	case strings.HasPrefix(cmd, "/close "):
		if len(fields) < 2 {
			return "usage: /close SYMBOL"
		}
		sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(fields[1])))
		if sym == "" {
			return "usage: /close SYMBOL"
		}
		now := time.Now().UTC()
		meta := c.getMeta()
		closed := false
		if c.execMgr != nil {
			ok, err := c.execMgr.ForceCloseSymbol(sym, "TG_CLOSE_SYMBOL")
			if err != nil {
				return fmt.Sprintf("close %s failed: %v", sym, err)
			}
			closed = closed || ok
		}
		if c.paper != nil && c.paper.enabled {
			closed = closed || c.paper.ForceCloseSymbol(now, sym, meta, map[string]aster.OrderBook{}, "TG_CLOSE_SYMBOL")
		}
		if !closed {
			return fmt.Sprintf("no active position for %s", sym)
		}
		return fmt.Sprintf("close requested for %s", sym)
	case strings.HasPrefix(cmd, "/closeall"):
		now := time.Now().UTC()
		meta := c.getMeta()
		if c.execMgr != nil {
			_ = c.execMgr.ForceCloseAll("TG_CLOSE_ALL")
		}
		if c.paper != nil && c.paper.enabled {
			c.paper.ForceCloseAll(now, meta, map[string]aster.OrderBook{}, "TG_CLOSE_ALL")
		}
		return "close all requested"
	default:
		return ""
	}
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
