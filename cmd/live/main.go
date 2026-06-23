package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
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
	trailx "go-machine/internal/executor"
	"go-machine/internal/features"
	flowfeed "go-machine/internal/flow"
	"go-machine/internal/gate"
	"go-machine/internal/inplay"
	"go-machine/internal/market"
	"go-machine/internal/notify"
	"go-machine/internal/reliability"
	"go-machine/internal/risk"
	"go-machine/internal/stats"
	"go-machine/internal/strategies"
	"go-machine/internal/ta"
	"go-machine/internal/throttle"
	"go-machine/internal/types"
)

type candidate struct {
	Entry                  inplay.Entry
	Side                   string // BUY/SELL
	Strat                  string
	StrategyID             string
	SetupFamily            string
	SetupSource            string
	TradeHorizon           string
	Conf                   float64
	FinalRank              float64
	TriggerState           string
	TriggerStateN          float64
	TriggerStage           string
	TriggerScans           int
	ExitProfile            string
	VolumeUSD              float64
	FundingRate            float64
	DayUTC24h              float64
	UTC4hPct               float64
	UTC1hPct               float64
	VolumeRatio            float64
	OFIRaw                 float64
	OFIZ                   float64
	OFISamples             int
	SpreadBps              float64
	DepthBid               float64
	DepthAsk               float64
	BookImbalance          float64
	ATR                    float64
	ATRPct                 float64
	Sig                    strategies.Signal
	RejectReason           string
	LastClose              float64
	SessionVWAP            float64
	EMA9                   float64
	FastSlope              float64
	SlowSlope              float64
	ReliabilityAdj         float64
	DiscoveryScore         float64
	TriggerScore           float64
	ExecutionScore         float64
	CombinedScore          float64
	TradeQuality           float64
	SessionLabel           string
	EntryTiming            string
	CandidateAgeSeconds    float64
	DistanceToVWAPPct      float64
	QualityReasons         []string
	LifecycleStage         string
	LifecycleScans         int
	StopPlan               exitmgr.HybridStopResult
	StructureFresh         bool
	ClosedBreakHold        bool
	ReclaimHold            bool
	RetestHold             bool
	ResetRebreak           bool
	ExtensionATR           float64
	StructureReason        string
	LiquidityRisk          bool
	LiquidityRiskReason    string
	LiquidityPoolLevel     float64
	LiquidityPoolCount     int
	LiquidityPoolSide      string
	LiquiditySweepSeen     bool
	LiquiditySweepStrength float64
	LiquiditySweepBarsAgo  int
	PatternBias            float64
	PatternReasons         []string
	WallMode               string
	WallStatus             string
	WallConfidence         float64
	WallBiasScore          float64
	WallSpoofRisk          float64
	WallDistanceBps        float64
	WallSizeRatio          float64
	WallPersistence        time.Duration
	WallPullRate           float64
	WallAddRate            float64
	WallRefillCount        int
	WallPrice              float64
	WallSide               string
	WallReasons            []string
	PersistenceSeenCount   int
	PersistenceTopNCount   int
	PersistenceBestRank    float64
	PersistenceVolumeTrend bool
	PersistenceMomentum    bool
	PersistenceReason      string
	PriorDayBestRank       float64
	PriorDayBestScore      float64
	PriorDayGrade          string
	PriorDayState          string
	PriorDayClose          float64
	PriorDayRelStrength    float64
	PriorDayVolumeUSD      float64
	PriorDayLeaderBoost    float64
	PriorDayLeaderMode     string
	PriorDayLeaderReasons  []string
	EntryPosture           string
	EntryPostureReason     string
}

const unresolvedSourceTagPrefix = "unresolved_source:"

type entryQualityConfig struct {
	EnableMetaGate          bool
	MinQuality              float64
	MinQualityCont          float64
	MinQualityIgnite        float64
	MinQualityRev           float64
	RequireStrategyMatch    bool
	MinEntryConf            float64
	MinEntryConfCont        float64
	MinEntryConfIgnite      float64
	MinEntryConfRev         float64
	PersistenceOverride     bool
	PersistMinQuality       float64
	PersistMinScans         int
	PersistMinScore         float64
	PersistMinGrade         string
	EnableScoreGate         bool
	MinDiscovery            float64
	MinTrigger              float64
	MinExecution            float64
	ScoreWeightDiscovery    float64
	ScoreWeightTrigger      float64
	ScoreWeightExecution    float64
	DayUTCWeight            float64
	DayUTCMinAbsPct         float64
	DayUTCScalePct          float64
	BlockContExhaustion     bool
	DayUTCMaturityBrake     bool
	DayUTCMaturityPct       float64
	RequireFreshPullback    bool
	PersistenceSoftOverride bool
	PersistSoftMetaMin      float64
	PersistSoftMinSeen      int
	PersistSoftMinTopN      int
}

type sessionChurn struct {
	DayKey         string
	EntryCount     int
	StopCount      int
	QuickLossCount int
	DayLossCount   int
	LastEntryAt    time.Time
	LastStopAt     time.Time
	LastStyle      string
	LastDayUTCPct  float64
	LossTimes      []time.Time
	SetupLossTimes map[string][]time.Time
}

type protectionStage int

const (
	protectionStageNone protectionStage = iota
	protectionStageArmed
	protectionStageLocked
)

type tradeManagePhase string

const (
	managePhaseStarter      tradeManagePhase = "starter"
	managePhaseContinuation tradeManagePhase = "continuation"
	managePhaseExhaustion   tradeManagePhase = "exhaustion"
)

type utcSessionPhase string

const (
	sessionUTCOffHours  utcSessionPhase = "UTC_OFF_HOURS"
	sessionAsiaDev      utcSessionPhase = "ASIA_DEV"
	sessionAsiaBreakout utcSessionPhase = "ASIA_BREAKOUT"
	sessionAsiaContinue utcSessionPhase = "ASIA_CONTINUE"
	sessionLondonOpen   utcSessionPhase = "LONDON_OPEN"
	sessionLondonExpand utcSessionPhase = "LONDON_EXPAND"
	sessionNYOpen       utcSessionPhase = "NY_OPEN"
	sessionNYExpand     utcSessionPhase = "NY_EXPAND"
)

type majorMarketVenue struct {
	Name       string
	Timezone   string
	OpenHour   int
	OpenMin    int
	CloseHour  int
	CloseMin   int
	BreakStart int
	BreakEnd   int
}

type regionSessionStats struct {
	OpenCount      int
	BreakCount     int
	RecentlyClosed bool
}

type candidateLifecycleConfig struct {
	Enable        bool
	ArmScans      int
	ReadyScans    int
	ExpireAfter   time.Duration
	ReadyMinScore float64
	ReadyMinSlope float64
}

type candidateMemory struct {
	SeenScans int
	Stage     string
	LastSeen  time.Time
}

type acceptanceQueueConfig struct {
	TopN                  int
	MaxAttemptsPerCycle   int
	MaxNewPositionsWindow int
	EntryWindow           time.Duration
	RecentRejectTTL       time.Duration
}

type recentRejectMemory struct {
	Symbol       string
	Side         string
	Reject       string
	ExpiresAt    time.Time
	Discovery    float64
	Combined     float64
	Score        float64
	Slope        float64
	State        string
	SpreadBps    float64
	ExtensionATR float64
	FinalRank    float64
	LastAttempt  time.Time
}

type runtimeProfile string

const (
	runtimeProfilePaperContinuationClean runtimeProfile = "paper_continuation_clean"
	runtimeProfilePaperContinuationLoose runtimeProfile = "paper_continuation_loose"
	runtimeProfilePaperReversalTest      runtimeProfile = "paper_reversal_test"
	runtimeProfilePaperImpulseTest       runtimeProfile = "paper_impulse_test"
	runtimeProfileLiveSafe               runtimeProfile = "live_safe"
	runtimeProfileLiveConservative       runtimeProfile = "live_conservative"
)

type runtimeProfileConfig struct {
	Name                      runtimeProfile
	EffectiveVPEnabled        bool
	EffectiveInstitutional    bool
	EffectiveReversal         bool
	EffectiveImpulse          bool
	EffectiveReentry          bool
	EffectiveRequireStructure bool
	EffectiveCandidateMemory  bool
	EffectiveTriggerMemory    bool
	EffectiveSharedManagement bool
}

type setupBlueprint struct {
	SetupFamily    string
	SetupSource    string
	TradeHorizon   string
	StrategyFamily string
}

var setupBlueprints = map[string]setupBlueprint{
	"lsr":                         {SetupFamily: "breakout_retest", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"bos_pb":                      {SetupFamily: "breakout_retest", SetupSource: "market_structure", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"ob_r":                        {SetupFamily: "deep_pullback_reclaim", SetupSource: "market_structure", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"fvg_c":                       {SetupFamily: "micro_pullback_continuation", SetupSource: "market_structure", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"fa":                          {SetupFamily: "reversal_exhaustion", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "rev"},
	"failed_auction_magnet":       {SetupFamily: "reversal_exhaustion", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "rev"},
	"od":                          {SetupFamily: "reset_impulse_breakout", SetupSource: "order_flow", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"volume_clusters":             {SetupFamily: "deep_pullback_reclaim", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"multiple_nodes":              {SetupFamily: "breakout_retest", SetupSource: "order_flow", TradeHorizon: "swing", StrategyFamily: "cont"},
	"trades_filter":               {SetupFamily: "micro_pullback_continuation", SetupSource: "order_flow", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"stacked_imbalances":          {SetupFamily: "reset_impulse_breakout", SetupSource: "order_flow", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"unfinished_business":         {SetupFamily: "breakout_retest", SetupSource: "order_flow", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"vwap_confluence":             {SetupFamily: "micro_pullback_continuation", SetupSource: "vwap", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"daily_open_sr":               {SetupFamily: "breakout_retest", SetupSource: "vwap", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"pd_levels_retest":            {SetupFamily: "deep_pullback_reclaim", SetupSource: "vwap", TradeHorizon: "swing", StrategyFamily: "cont"},
	"vp_accumulation":             {SetupFamily: "deep_pullback_reclaim", SetupSource: "volume_profile", TradeHorizon: "swing", StrategyFamily: "cont"},
	"vp_trend":                    {SetupFamily: "micro_pullback_continuation", SetupSource: "volume_profile", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"vp_rejection":                {SetupFamily: "breakout_retest", SetupSource: "volume_profile", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"vp_reversal":                 {SetupFamily: "reversal_exhaustion", SetupSource: "volume_profile", TradeHorizon: "intraday_swing", StrategyFamily: "rev"},
	"momentum_ignite_long":        {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"momentum_ignite_short":       {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"reset_impulse_long":          {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"reset_impulse_short":         {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"impulse_long":                {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"impulse_short":               {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"continuation_fast":           {SetupFamily: "micro_pullback_continuation", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"micro_pullback_continuation": {SetupFamily: "micro_pullback_continuation", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "cont"},
	"breakout_retest":             {SetupFamily: "breakout_retest", SetupSource: "technical_analysis", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"deep_pullback_reclaim":       {SetupFamily: "deep_pullback_reclaim", SetupSource: "technical_analysis", TradeHorizon: "intraday_swing", StrategyFamily: "cont"},
	"reset_impulse_breakout":      {SetupFamily: "reset_impulse_breakout", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "ignite"},
	"reversal_exhaustion":         {SetupFamily: "reversal_exhaustion", SetupSource: "technical_analysis", TradeHorizon: "intraday", StrategyFamily: "rev"},
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

type livePriceQuote struct {
	Symbol    string
	MarkPrice float64
	LastPrice float64
	BidPrice  float64
	AskPrice  float64
	SpreadBps float64
	UpdatedAt time.Time
}

type liveAccountPosition struct {
	Symbol           string
	Side             string
	Source           string
	ManageState      string
	ProtectionState  string
	Managed          bool
	Protected        bool
	Qty              float64
	EntryPrice       float64
	MarkPrice        float64
	LastPrice        float64
	SpreadBps        float64
	UnrealizedPnL    float64
	UnrealizedPnLPct float64
	RealizedPnL      float64
	ExchangeUnreal   float64
	Leverage         int
	Margin           float64
	StopPrice        float64
	HoldMin          float64
	EntryReason      string
}

type liveAccountSnapshot struct {
	Generated     time.Time
	Health        string
	HealthDetail  string
	AvailableUSDT float64
	Equity        float64
	RealizedDay   float64
	OpenPnL       float64
	OpenCount     int
	BotCount      int
	ManualCount   int
	Positions     []liveAccountPosition
}

type manualManageRequest struct {
	Key         string
	Fingerprint string
	Symbol      string
	Side        string
	Qty         float64
	Entry       float64
	Margin      float64
	Leverage    int
	Action      string
	Failure     string
	DetectedAt  time.Time
	PromptedAt  time.Time
	DecidedAt   time.Time
	Status      string
}

const (
	manualRequestPending  = "PENDING"
	manualRequestApproved = "APPROVED"
	manualRequestPassive  = "PASSIVE"
	manualRequestDeclined = "DECLINED"

	manualEntryReasonPassive = "MANUAL_IMPORT"
	manualEntryReasonManaged = "manual_managed_live"
	operatorEntryReason      = "OPERATOR_EXECUTION"

	manualEntrySourcePassive = "MANUAL_PASSIVE"
	manualEntrySourceManaged = "MANUAL_MANAGED"

	manualManageStatePassive           = "manual_manage_state_passive_import"
	manualManageStatePendingProtection = "manual_manage_state_protecting"
	manualManageStateLive              = "manual_managed_live"
	manualManageStateForceClose        = "manual_manage_state_force_close"
	manualManageStateCritical          = "manual_manage_state_critical"
	manualManageStateDegraded          = manualManageStateCritical
	manualManageStateConflict          = "manual_state_conflict"
)

type safetyConfig struct {
	enableLiveTrading      bool
	maxLeverage            int
	minAvailUSDT           float64
	maxOrdersPerDay        int
	maxOrdersPerHour       int
	orderCooldown          time.Duration
	symbolCooldownSameSide time.Duration
	symbolCooldownFlipSide time.Duration
	stopoutWindow          time.Duration
	stopoutLock            time.Duration
	stopoutCount           int
	pauseFile              string
	allowSymbols           map[string]struct{}
	blockSymbols           map[string]struct{}
	contextOnlySymbols     map[string]struct{}
	allowShorts            bool
	maxDailyLossPct        float64
	killClose              bool
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
	missed  *missedTracker
	safety  safetyConfig
	status  *liveStatusStore
	mode    *runtimeModeController

	metaMu sync.RWMutex
	meta   map[string]symbolMeta

	decisionMu sync.RWMutex
	decisions  map[string]operatorDecision

	suggestMu   sync.RWMutex
	suggestions map[string]operatorSuggestion
	suggestTTL  time.Duration
}

type runtimeModeController struct {
	mu                sync.RWMutex
	dryRun            bool
	enableLiveTrading bool
	paperEnabled      bool
	requestedMode     runtimeOperatingMode
}

func newRuntimeModeController(dryRun, enableLiveTrading, paperEnabled bool) *runtimeModeController {
	requestedMode := parseRuntimeOperatingMode(envStr("LIVE_RUNTIME_MODE", ""))
	if requestedMode == "" {
		if dryRun {
			requestedMode = runtimeModePaper
		} else {
			requestedMode = runtimeModeLive
		}
	}
	return &runtimeModeController{
		dryRun:            dryRun,
		enableLiveTrading: enableLiveTrading,
		paperEnabled:      paperEnabled,
		requestedMode:     requestedMode,
	}
}

func (m *runtimeModeController) snapshot() (bool, bool, bool) {
	if m == nil {
		return true, false, true
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dryRun, m.enableLiveTrading, m.paperEnabled
}

func (m *runtimeModeController) setLive() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dryRun = false
	m.enableLiveTrading = true
	m.paperEnabled = false
}

func (m *runtimeModeController) setPaper() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dryRun = true
	m.enableLiveTrading = false
	m.paperEnabled = true
}

func (m *runtimeModeController) operatingMode() runtimeOperatingMode {
	if m == nil {
		return runtimeModeLive
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	switch m.requestedMode {
	case runtimeModePaper:
		if m.dryRun && !m.enableLiveTrading && m.paperEnabled {
			return runtimeModePaper
		}
	case runtimeModeLive:
		return runtimeModeLive
	}
	if m.dryRun && m.paperEnabled {
		return runtimeModePaper
	}
	return runtimeModeLive
}

func (m *runtimeModeController) requestedOperatingMode() runtimeOperatingMode {
	if m == nil {
		return runtimeModeLive
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.requestedMode
}

type operatorDecision struct {
	Symbol              string
	Side                string
	Grade               string
	Score               float64
	Slope               float64
	Strategy            string
	Confidence          float64
	RawConfidence       float64
	AdjustedConfidence  float64
	RejectReason        string
	BlockerClass        string
	TopBlockers         []string
	StarterAllowed      bool
	PersistenceStatus   string
	State               string
	QualityFlags        []string
	PenaltyTotal        float64
	ScoreBefore         float64
	ScoreAfterPenalties float64
	MinScore            float64
	HardBlockReasons    []string
	BlockReason         string
	UpdatedAt           time.Time
}

type RejectClass string

const (
	rejectClassHardSafety    RejectClass = "hard_safety"
	rejectClassSoftConfirm   RejectClass = "soft_confirmation"
	rejectClassCapacity      RejectClass = "capacity_runtime"
	rejectClassStateCooldown RejectClass = "state_cooldown"
)

type EntryEligibilitySummary struct {
	Symbol           string
	Side             string
	Strat            string
	Rank             float64
	Grade            string
	PersistenceScore float64

	HardBlocks     []string
	SoftBlocks     []string
	CapacityBlocks []string
	StateBlocks    []string

	StarterAllowed   bool
	FullEntryAllowed bool
	ReentryAllowed   bool

	FinalDecision            string
	FinalReason              string
	AdjustedConfidence       float64
	ConfidencePenaltyReasons []string
	Quality                  strategies.EntryQualityAccumulator
}

type operatorSuggestion struct {
	Symbol          string
	Side            string
	Source          string
	PreferredLev    int
	PreferredMargin float64
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type symbolMeta struct {
	LastPrice   float64
	OpenPrice   float64
	Move24h     float64
	DayUTC24h   float64
	UTC4hPct    float64
	UTC1hPct    float64
	VolumeUSD   float64
	FundingRate float64
	Bid         float64
	Ask         float64
}

type topBookSnapshot struct {
	Ts    time.Time
	BidPx float64
	BidSz float64
	AskPx float64
	AskSz float64
}

type flowMetrics struct {
	UpdatedAt     time.Time
	OFIRaw        float64
	OFIZ          float64
	OFISamples    int
	SpreadBps     float64
	DepthBid      float64
	DepthAsk      float64
	BookImbalance float64
	Mid           float64
}

type wallObservation struct {
	Price       float64
	Size        float64
	SizeRatio   float64
	FirstSeenAt time.Time
	LastSeenAt  time.Time
	Samples     int
	Adds        int
	Pulls       int
	Refills     int
}

type wallSignal struct {
	Mode        string
	Status      string
	Confidence  float64
	BiasScore   float64
	SpoofRisk   float64
	DistanceBps float64
	SizeRatio   float64
	Persistence time.Duration
	PullRate    float64
	AddRate     float64
	RefillCount int
	Price       float64
	Side        string
	Reasons     []string
	BidWall     *ta.OBWall
	AskWall     *ta.OBWall
	Interaction float64
}

type wallTracker struct {
	mu     sync.RWMutex
	bidObs map[string]wallObservation
	askObs map[string]wallObservation
	ctx    map[string]ta.OBContext
	sig    map[string]wallSignal
}

type ofiTracker struct {
	Last    topBookSnapshot
	Mu      float64
	Var     float64
	Samples int
	Init    bool
}

type watchConfig struct {
	Enable          bool
	Every           time.Duration
	PriorityEvery   time.Duration
	MaxCandidates   int
	TopNOnly        bool
	WatchOpen       bool
	BookLevels      int
	EnableOFI       bool
	OFIAlpha        float64
	OFIMinSamples   int
	IgniteMinOFIZ   float64
	ContMinOFIZ     float64
	RevLongMinOFIZ  float64
	RevShortMaxOFIZ float64
}

type watchRuntime struct {
	cfg            watchConfig
	client         *aster.Client
	longInPlay     []inplay.Entry
	shortInPlay    []inplay.Entry
	meta           map[string]symbolMeta
	symbols        []string
	priority       map[string]operatorSuggestion
	ofi            map[string]*ofiTracker
	flow           map[string]flowMetrics
	walls          *wallTracker
	lastUrgentAt   time.Time
	lastPriorityAt time.Time
}

var (
	liveWatchEvery     time.Duration
	liveWatchTick      func(time.Time) bool
	liveWakeReason     string
	livePriorityEvery  time.Duration
	livePriorityActive func() bool
)

type paperPosition struct {
	Symbol                 string
	Side                   string
	TradeID                string
	Entry                  float64
	Qty                    float64 // remaining qty
	InitialQty             float64
	Margin                 float64
	Leverage               int
	Stop                   float64
	TP1                    float64
	TP2                    float64
	TP3                    float64
	HitTP1                 bool
	HitTP2                 bool
	HitTP3                 bool
	TrailOn                bool
	TrailStop              float64
	TrailRef               float64
	Realized               float64
	OpenedAt               time.Time
	MaxFavorableR          float64
	MaxAdverseR            float64
	LastMark               float64
	EntryReason            string
	RawEntryReason         string
	EntryStrategyID        string
	EntryStyle             string
	EntryStrategyFamily    string
	EntryMode              string
	EntryGrade             string
	EntryState             inplay.State
	EntryTrigger           string
	ExitProfile            string
	EntryConf              float64
	EntryConfluenceScore   float64
	EntrySignalReasons     []string
	EntrySignalSources     []string
	EntryDecisionReasons   []string
	EntryDecisionRejects   []string
	EntryDecisionReject    string
	EntryDecisionProof     []string
	DiscoveryScore         float64
	TriggerScore           float64
	ExecutionScore         float64
	CombinedScore          float64
	Sponsored              bool
	SponsorshipScore       float64
	WeakSponsorStreak      int
	StrongSponsorStreak    int
	LastConfluenceRefresh  time.Time
	ConfluenceRefreshCount int
	EntryVolumeUSD         float64
	EntryATRPct            float64
	EntryATRExtension      float64
	EntrySession           string
	EntryTiming            string
	CandidateAgeSeconds    float64
	EntryDistanceToVWAPPct float64
	EntryPct24h            float64
	EntryPct4h             float64
	EntryPct1h             float64
	ShortBucket            string
	ShortFilterReason      string
	ShortRequireConfirm    string
	DirectShortAllowed     bool
	BounceFromLocalLowPct  float64
	FailedBounceConfirmed  bool
	PostPumpBreakdown      bool
	LateChaseBlocked       bool
	OpposingFriction       float64
	StopReason             string
	StopDistancePct        float64
	EntrySetupFamily       string
	EntrySetupSource       string
	EntryTradeHorizon      string
	ExecBucket             string
	StallBars              int
	ProtectionStage        protectionStage
	FirstProtectAt         time.Time
	ProtectedStop          float64
	WinnerLifecycle        string
	MaxGivebackR           float64
	CaptureRatio           float64
	OriginalStop           float64
	OriginalTP1            float64
	OriginalTP2            float64
	OriginalTP3            float64
	GrossRealized          float64
	FeesRealized           float64
	lastProtectDecisionKey string
}

type paperClosedTradeIdentity struct {
	Strategy         string  `json:"strategy"`
	RawStrategy      string  `json:"raw_strategy,omitempty"`
	StrategyMissing  bool    `json:"strategy_missing,omitempty"`
	SetupFamily      string  `json:"setup_family,omitempty"`
	SetupSource      string  `json:"setup_source,omitempty"`
	TradeHorizon     string  `json:"trade_horizon,omitempty"`
	ExecBucket       string  `json:"exec_bucket,omitempty"`
	EntryStyle       string  `json:"entry_style,omitempty"`
	StrategyFamily   string  `json:"strategy_family,omitempty"`
	Session          string  `json:"session,omitempty"`
	Grade            string  `json:"grade,omitempty"`
	ConfluenceScore  float64 `json:"confluence_score,omitempty"`
	EntryTiming      string  `json:"entry_timing,omitempty"`
	CandidateAgeSecs float64 `json:"candidate_age_seconds,omitempty"`
	DistanceToVWAP   float64 `json:"distance_to_vwap_pct,omitempty"`
	ATRExension      float64 `json:"atr_extension,omitempty"`
}

type paperClosedTradeEntry struct {
	EntryTs    time.Time `json:"entry_ts"`
	EntryPrice float64   `json:"entry_price"`
	Qty        float64   `json:"qty"`
	Leverage   int       `json:"leverage"`
	MarginUsed float64   `json:"margin_used"`
}

type paperClosedTradePlan struct {
	OriginalStop          float64 `json:"original_stop"`
	OriginalTP1           float64 `json:"original_tp1"`
	OriginalTP2           float64 `json:"original_tp2"`
	OriginalTP3           float64 `json:"original_tp3"`
	PlannedRiskPrice      float64 `json:"planned_risk_price"`
	PlannedRiskPct        float64 `json:"planned_risk_pct"`
	ShortBucket           string  `json:"short_bucket,omitempty"`
	ShortFilterReason     string  `json:"short_filter_reason,omitempty"`
	DirectShortAllowed    bool    `json:"direct_short_allowed,omitempty"`
	RequireConfirmation   string  `json:"require_confirmation,omitempty"`
	Pct24hAtEntry         float64 `json:"pct24h_at_entry,omitempty"`
	Pct4hAtEntry          float64 `json:"pct4h_at_entry,omitempty"`
	Pct1hAtEntry          float64 `json:"pct1h_at_entry,omitempty"`
	BounceFromLocalLowPct float64 `json:"bounce_from_local_low_pct,omitempty"`
	FailedBounceConfirmed bool    `json:"failed_bounce_confirmed,omitempty"`
	PostPumpBreakdown     bool    `json:"post_pump_breakdown,omitempty"`
	LateChaseBlocked      bool    `json:"late_chase_blocked,omitempty"`
}

type paperClosedTradeExit struct {
	ExitTs            time.Time `json:"exit_ts"`
	RealizedExitPrice float64   `json:"realized_exit_price"`
	ExitReason        string    `json:"exit_reason"`
	RawExitReason     string    `json:"raw_exit_reason"`
	GrossPnL          float64   `json:"gross_pnl"`
	Fees              float64   `json:"fees"`
	NetPnL            float64   `json:"net_pnl"`
	HoldMinutes       float64   `json:"hold_minutes"`
	MaxRSeen          float64   `json:"max_r_seen"`
	MinRSeen          float64   `json:"min_r_seen"`
	ProtectionState   string    `json:"protection_state,omitempty"`
	NoProofTriggered  bool      `json:"no_proof_triggered"`
	CloseType         string    `json:"close_type"`
}

type paperClosedTradePostExit struct {
	PeakPrice15m   float64 `json:"peak_price_15m,omitempty"`
	PeakPrice30m   float64 `json:"peak_price_30m,omitempty"`
	PeakPrice60m   float64 `json:"peak_price_60m,omitempty"`
	TroughPrice15m float64 `json:"trough_price_15m,omitempty"`
	TroughPrice30m float64 `json:"trough_price_30m,omitempty"`
	TroughPrice60m float64 `json:"trough_price_60m,omitempty"`
	BestR15m       float64 `json:"best_r_15m,omitempty"`
	BestR30m       float64 `json:"best_r_30m,omitempty"`
	BestR60m       float64 `json:"best_r_60m,omitempty"`
	WorstR15m      float64 `json:"worst_r_15m,omitempty"`
	WorstR30m      float64 `json:"worst_r_30m,omitempty"`
	WorstR60m      float64 `json:"worst_r_60m,omitempty"`
	MissedTP1      bool    `json:"missed_tp1_after_exit,omitempty"`
	MissedTP2      bool    `json:"missed_tp2_after_exit,omitempty"`
	MissedTP3      bool    `json:"missed_tp3_after_exit,omitempty"`
	ExitVsTP1      float64 `json:"exit_vs_tp1_price_diff,omitempty"`
	ExitVsTP2      float64 `json:"exit_vs_tp2_price_diff,omitempty"`
	ExitVsTP3      float64 `json:"exit_vs_tp3_price_diff,omitempty"`
}

type paperClosedTradeRecord struct {
	TradeID  string                   `json:"trade_id"`
	Mode     string                   `json:"mode"`
	Symbol   string                   `json:"symbol"`
	Side     string                   `json:"side"`
	Identity paperClosedTradeIdentity `json:"identity"`
	Entry    paperClosedTradeEntry    `json:"entry"`
	Plan     paperClosedTradePlan     `json:"plan"`
	Exit     paperClosedTradeExit     `json:"exit"`
	PostExit paperClosedTradePostExit `json:"post_exit"`
}

type paperPostExitWindowState struct {
	PeakPrice   float64   `json:"peak_price"`
	PeakAt      time.Time `json:"peak_at"`
	TroughPrice float64   `json:"trough_price"`
	TroughAt    time.Time `json:"trough_at"`
}

type paperPostExitTracker struct {
	TradeID           string                              `json:"trade_id"`
	Symbol            string                              `json:"symbol"`
	Side              string                              `json:"side"`
	EntryPrice        float64                             `json:"entry_price"`
	OriginalStop      float64                             `json:"original_stop"`
	OriginalTP1       float64                             `json:"original_tp1"`
	OriginalTP2       float64                             `json:"original_tp2"`
	OriginalTP3       float64                             `json:"original_tp3"`
	RealizedExitPrice float64                             `json:"realized_exit_price"`
	ExitTs            time.Time                           `json:"exit_ts"`
	Windows           map[string]paperPostExitWindowState `json:"windows"`
}

type paperTrader struct {
	enabled                bool
	startBal               float64
	balance                float64
	reserve                float64
	feeBps                 float64
	makerFeeBps            float64
	takerFeeBps            float64
	stopMode               string
	atrLen                 int
	stopPct                float64
	tp1R                   float64
	tp2R                   float64
	tp3R                   float64
	tp1Frac                float64
	tp2Frac                float64
	tp3Frac                float64
	tpRatchetOnly          bool
	trailAfterTP           int
	trailStopPct           float64
	trailStopPctTP3        float64
	trailPctMin            float64
	stateFile              string
	tradesCSV              string
	closedTradesJSONL      string
	equityCSV              string
	maxOpen                int
	positions              map[string]*paperPosition
	closedTradeLedger      map[string]paperClosedTradeRecord
	postExitTrackers       map[string]*paperPostExitTracker
	reportLoc              *time.Location
	dayStats               map[string]*paperDayStats
	minStopPct             float64
	maxStopPct             float64
	minTP1RR               float64
	beLockBps              float64
	fundingEvery           time.Duration
	fundingBySym           map[string]time.Duration
	fundingEnabled         bool
	fundingHazardSec       time.Duration
	fundingSkipNewPos      time.Duration
	allowFundingInMargin   bool
	fundingExitEnable      bool
	fundingExitMinAge      time.Duration
	fundingExitMaxUpnl     float64
	fundingExitMinMFER     float64
	lastFundKey            map[string]string
	openCostMode           string
	hybridStopCfg          exitmgr.HybridStopConfig
	onExit                 func(string)
	lossCooldown           time.Duration
	lastExitAt             map[string]time.Time
	lastExitLoss           map[string]bool
	lastHarvestAt          map[string]time.Time
	symbolTradeDay         map[string]string
	symbolTradeCount       map[string]int
	lossStreak             map[string]int
	lockUntil              map[string]time.Time
	lossDay                map[string]string
	lossDayCount           map[string]int
	recentLosses           map[string][]time.Time
	setupLockUntil         map[string]time.Time
	setupLosses            map[string][]time.Time
	maxLossStreak          int
	lossLock               time.Duration
	harvestLock            time.Duration
	harvestMinSlope        float64
	harvestMaxStateMin     float64
	maxTradesPerDay        int
	slotReplaceEnable      bool
	slotReplaceMinAge      time.Duration
	slotReplaceMinConf     float64
	slotReplaceMinSlope    float64
	slotReplaceMinScoreGap float64
	slotReplaceMaxUpnl     float64
	slotReplaceMinGrade    string
	eventLog               *stats.EventLogger
	stressRoundtripBps     float64
	exitManager            *exitmgr.Manager
	riskOnMargin           bool
	riskMarginPct          float64
	governorRecords        []executionGovernorRecord
	stopTriggerRef         string
	tpTriggerRef           string
	markLastModel          string
	markLastDivBps         float64
	partialFillEnable      bool
	partialFillMinFrac     float64
	stopMarketSlipBps      float64
	featureCache           *featureRuntimeCache
	htf1HBySymbol          map[string]HTFStructureSnapshot
}

type paperDayStats struct {
	Trades                         int
	Wins                           int
	Losses                         int
	Gross                          float64
	Fees                           float64
	Net                            float64
	Reasons                        map[string]int
	WinnerRevertedToLossCount      int
	WinnerRevertedToSmallLossCount int
	SameSymbolReentryLossCount     int
	StoppedAfterPositiveOpenCount  int
	TradesHit1RButNotProtected     int
	SumMaxROnLosers                float64
	LosingTradesWithMaxR           int
}

type paperState struct {
	StartBal         float64                           `json:"startBal"`
	Balance          float64                           `json:"balance"`
	Reserve          float64                           `json:"reserve"`
	Positions        map[string]*paperPosition         `json:"positions"`
	ClosedTrades     map[string]paperClosedTradeRecord `json:"closedTrades,omitempty"`
	PostExitTrackers map[string]*paperPostExitTracker  `json:"postExitTrackers,omitempty"`
	GovernorRecords  []executionGovernorRecord         `json:"governorRecords,omitempty"`
	DayStats         map[string]*paperDayStats         `json:"dayStats"`
	LastFund         map[string]string                 `json:"lastFund,omitempty"`
	LastExitAt       map[string]time.Time              `json:"lastExitAt,omitempty"`
	LastExitLoss     map[string]bool                   `json:"lastExitLoss,omitempty"`
	LastHarvest      map[string]time.Time              `json:"lastHarvest,omitempty"`
	SymbolTradeDay   map[string]string                 `json:"symbolTradeDay,omitempty"`
	SymbolTradeCount map[string]int                    `json:"symbolTradeCount,omitempty"`
	LossStreak       map[string]int                    `json:"lossStreak,omitempty"`
	LockUntil        map[string]time.Time              `json:"lockUntil,omitempty"`
	LossDay          map[string]string                 `json:"lossDay,omitempty"`
	LossDayCount     map[string]int                    `json:"lossDayCount,omitempty"`
	RecentLosses     map[string][]time.Time            `json:"recentLosses,omitempty"`
	SetupLockUntil   map[string]time.Time              `json:"setupLockUntil,omitempty"`
	SetupLosses      map[string][]time.Time            `json:"setupLosses,omitempty"`
	UpdatedAt        time.Time                         `json:"updatedAt"`
}

type execState string

const (
	execPendingEntry execState = "PENDING_ENTRY"
	execOpen         execState = "OPEN"
	execPartialTP1   execState = "PARTIAL_TP1"
	execPartialTP2   execState = "PARTIAL_TP2"
	execClosed       execState = "CLOSED"
)

const (
	blockedMaintenanceWindowReason        = "BLOCKED_MAINTENANCE_WINDOW"
	degradedAccountHealthPartialReason    = "DEGRADED_ACCOUNT_HEALTH_PARTIAL"
	degradedUserDataStaleReason           = "DEGRADED_USERDATA_STALE"
	degradedReconcileStaleReason          = "DEGRADED_RECONCILE_STALE"
	degradedOrderLegalityQuarantineReason = "DEGRADED_ORDER_LEGALITY_SYMBOL_QUARANTINE"
	orderIllegalMaxQtyReason              = "ORDER_ILLEGAL_MAX_QTY"
	orderIllegalMinQtyReason              = "ORDER_ILLEGAL_MIN_QTY"
	orderIllegalStepSizeReason            = "ORDER_ILLEGAL_STEP_SIZE"
	orderIllegalMinNotionalReason         = "ORDER_ILLEGAL_MIN_NOTIONAL"
	orderIllegalTickSizeReason            = "ORDER_ILLEGAL_TICK_SIZE"
)

type livePosition struct {
	Symbol                   string           `json:"symbol"`
	Side                     string           `json:"side"`
	State                    execState        `json:"state"`
	CreatedAt                time.Time        `json:"createdAt"`
	UpdatedAt                time.Time        `json:"updatedAt"`
	ClosedAt                 time.Time        `json:"closedAt,omitempty"`
	CloseReason              string           `json:"closeReason,omitempty"`
	EntryOrderID             int64            `json:"entryOrderId"`
	EntryPrice               float64          `json:"entryPrice"`
	ManageAnchorPrice        float64          `json:"manageAnchorPrice,omitempty"`
	Qty                      float64          `json:"qty"`
	FilledQty                float64          `json:"filledQty"`
	RemainingQty             float64          `json:"remainingQty"`
	Margin                   float64          `json:"margin"`
	DeployedMargin           float64          `json:"deployedMargin,omitempty"`
	Leverage                 int              `json:"leverage"`
	AddCount                 int              `json:"addCount,omitempty"`
	StarterOnly              bool             `json:"starterOnly,omitempty"`
	AddLockedUntilConfirm    bool             `json:"addLockedUntilConfirm,omitempty"`
	StopPrice                float64          `json:"stopPrice"`
	TP1Price                 float64          `json:"tp1Price"`
	TP2Price                 float64          `json:"tp2Price"`
	TP3Price                 float64          `json:"tp3Price"`
	TP1Qty                   float64          `json:"tp1Qty"`
	TP2Qty                   float64          `json:"tp2Qty"`
	TP3Qty                   float64          `json:"tp3Qty"`
	TP1FilledQty             float64          `json:"tp1FilledQty,omitempty"`
	TP2FilledQty             float64          `json:"tp2FilledQty,omitempty"`
	TP3FilledQty             float64          `json:"tp3FilledQty,omitempty"`
	StopFilledQty            float64          `json:"stopFilledQty,omitempty"`
	HitTP1                   bool             `json:"hitTp1,omitempty"`
	HitTP2                   bool             `json:"hitTp2,omitempty"`
	HitTP3                   bool             `json:"hitTp3,omitempty"`
	StopOrderID              int64            `json:"stopOrderId"`
	TP1OrderID               int64            `json:"tp1OrderId"`
	TP2OrderID               int64            `json:"tp2OrderId"`
	TP3OrderID               int64            `json:"tp3OrderId"`
	TrailOn                  bool             `json:"trailOn"`
	TrailRef                 float64          `json:"trailRef"`
	TrailStop                float64          `json:"trailStop"`
	TrailCandidateRef        float64          `json:"trailCandidateRef,omitempty"`
	TrailCandidateStop       float64          `json:"trailCandidateStop,omitempty"`
	TrailCandidateLevel      float64          `json:"trailCandidateLevel,omitempty"`
	TrailCandidateAt         time.Time        `json:"trailCandidateAt,omitempty"`
	TrailProtectedLastClose  time.Time        `json:"trailProtectedLastClose,omitempty"`
	VPSetup                  string           `json:"vpSetup,omitempty"`
	VPLevel                  float64          `json:"vpLevel,omitempty"`
	VPTargetLevel            float64          `json:"vpTargetLevel,omitempty"`
	VPStopMode               string           `json:"vpStopMode,omitempty"`
	VPTargetMode             string           `json:"vpTargetMode,omitempty"`
	RejectReason             string           `json:"rejectReason,omitempty"`
	CustomRiskPct            float64          `json:"customRiskPct,omitempty"`
	CustomTP1R               float64          `json:"customTp1R,omitempty"`
	CustomTP2R               float64          `json:"customTp2R,omitempty"`
	EntryReason              string           `json:"entryReason,omitempty"`
	EntryStrategyID          string           `json:"entryStrategyId,omitempty"`
	EntrySource              string           `json:"entrySource,omitempty"`
	EntryGrade               string           `json:"entryGrade,omitempty"`
	EntryState               string           `json:"entryState,omitempty"`
	EntryTrigger             string           `json:"entryTrigger,omitempty"`
	ExitProfile              string           `json:"exitProfile,omitempty"`
	EntryConf                float64          `json:"entryConf,omitempty"`
	DiscoveryScore           float64          `json:"discoveryScore,omitempty"`
	TriggerScore             float64          `json:"triggerScore,omitempty"`
	ExecutionScore           float64          `json:"executionScore,omitempty"`
	CombinedScore            float64          `json:"combinedScore,omitempty"`
	Sponsored                bool             `json:"sponsored,omitempty"`
	SponsorshipScore         float64          `json:"sponsorshipScore,omitempty"`
	WeakSponsorStreak        int              `json:"weakSponsorStreak,omitempty"`
	StrongSponsorStreak      int              `json:"strongSponsorStreak,omitempty"`
	LastConfluenceRefresh    time.Time        `json:"lastConfluenceRefresh,omitempty"`
	ConfluenceRefreshCount   int              `json:"confluenceRefreshCount,omitempty"`
	EntryTags                []string         `json:"entryTags,omitempty"`
	EntryReasons             []string         `json:"entryReasons,omitempty"`
	EntryVolumeUSD           float64          `json:"entryVolumeUsd,omitempty"`
	EntryATRPct              float64          `json:"entryAtrPct,omitempty"`
	EntryATRExtension        float64          `json:"entryAtrExtension,omitempty"`
	EntrySession             string           `json:"entrySession,omitempty"`
	EntryTiming              string           `json:"entryTiming,omitempty"`
	CandidateAgeSeconds      float64          `json:"candidateAgeSeconds,omitempty"`
	EntryDistanceToVWAPPct   float64          `json:"entryDistanceToVwapPct,omitempty"`
	StopReason               string           `json:"stopReason,omitempty"`
	StopDistancePct          float64          `json:"stopDistancePct,omitempty"`
	RegimeTag                string           `json:"regimeTag,omitempty"`
	EntrySetupFamily         string           `json:"entrySetupFamily,omitempty"`
	EntrySetupSource         string           `json:"entrySetupSource,omitempty"`
	EntryTradeHorizon        string           `json:"entryTradeHorizon,omitempty"`
	ExecBucket               string           `json:"execBucket,omitempty"`
	MaxFavorableR            float64          `json:"maxFavorableR,omitempty"`
	MaxAdverseR              float64          `json:"maxAdverseR,omitempty"`
	StallBars                int              `json:"stallBars,omitempty"`
	LastMark                 float64          `json:"lastMark,omitempty"`
	RealizedPnL              float64          `json:"realizedPnl,omitempty"`
	ProtectionPending        bool             `json:"protectionPending,omitempty"`
	ProtectionRetryAfter     time.Time        `json:"protectionRetryAfter,omitempty"`
	ProtectionRetryCount     int              `json:"protectionRetryCount,omitempty"`
	ProtectionFailCount      int              `json:"protectionFailCount,omitempty"`
	ForceProtectionNow       bool             `json:"forceProtectionNow,omitempty"`
	ManageFailSuppressCount  int              `json:"manageFailSuppressCount,omitempty"`
	LastManageFailAt         time.Time        `json:"lastManageFailAt,omitempty"`
	LastManageFailCause      string           `json:"lastManageFailCause,omitempty"`
	LastManageProtectRef     float64          `json:"lastManageProtectRef,omitempty"`
	LastManageComputedStop   float64          `json:"lastManageComputedStop,omitempty"`
	LastManageNormalizedStop float64          `json:"lastManageNormalizedStop,omitempty"`
	LastManageNotifyState    string           `json:"lastManageNotifyState,omitempty"`
	LastManageNotifyCause    string           `json:"lastManageNotifyCause,omitempty"`
	LastManageNotifyAt       time.Time        `json:"lastManageNotifyAt,omitempty"`
	ManualManageState        string           `json:"manualManageState,omitempty"`
	Managed                  bool             `json:"managed,omitempty"`
	Protected                bool             `json:"protected,omitempty"`
	UnknownEntryChecks       int              `json:"unknownEntryChecks,omitempty"`
	UnknownExitChecks        int              `json:"unknownExitChecks,omitempty"`
	PendingAddOrderID        int64            `json:"pendingAddOrderId,omitempty"`
	PendingAddPrice          float64          `json:"pendingAddPrice,omitempty"`
	PendingAddQty            float64          `json:"pendingAddQty,omitempty"`
	PendingAddFilledQty      float64          `json:"pendingAddFilledQty,omitempty"`
	PendingAddMargin         float64          `json:"pendingAddMargin,omitempty"`
	PendingAddCreatedAt      time.Time        `json:"pendingAddCreatedAt,omitempty"`
	PendingAddEntryReason    string           `json:"pendingAddEntryReason,omitempty"`
	PendingExitOrderID       int64            `json:"pendingExitOrderId,omitempty"`
	PendingExitPrice         float64          `json:"pendingExitPrice,omitempty"`
	PendingExitQty           float64          `json:"pendingExitQty,omitempty"`
	PendingExitFilledQty     float64          `json:"pendingExitFilledQty,omitempty"`
	PendingExitCreatedAt     time.Time        `json:"pendingExitCreatedAt,omitempty"`
	PendingExitReason        string           `json:"pendingExitReason,omitempty"`
	PendingExitAction        string           `json:"pendingExitAction,omitempty"`
	ProfitSweptUSDT          float64          `json:"profitSweptUsdt,omitempty"`
	ReentryCount             int              `json:"reentryCount,omitempty"`
	ExhaustionExit           bool             `json:"exhaustionExit,omitempty"`
	ProtectionStage          protectionStage  `json:"protectionStage,omitempty"`
	FirstProtectAt           time.Time        `json:"firstProtectAt,omitempty"`
	ProtectedStop            float64          `json:"protectedStop,omitempty"`
	WinnerLifecycle          string           `json:"winnerLifecycle,omitempty"`
	MaxGivebackR             float64          `json:"maxGivebackR,omitempty"`
	CaptureRatio             float64          `json:"captureRatio,omitempty"`
	ManagePhase              tradeManagePhase `json:"managePhase,omitempty"`
	RunnerMinQty             float64          `json:"runnerMinQty,omitempty"`
	RunnerCaptureFailed      bool             `json:"runnerCaptureFailed,omitempty"`
	lastProtectDecisionKey   string
	pendingProtectAction     string
	pendingProtectReason     string
	pendingProtectCurrent    string
	pendingProtectNext       string
	pendingProtectTrigger    string
	pendingProtectHTFState   string
	pendingProtectMFE        float64
	pendingProtectPersistent bool
	pendingProtectFailed     bool
	pendingProtectCaution    bool
	pendingProtectComputed   float64
}

type liveExecStore struct {
	Positions       map[string]*livePosition  `json:"positions"`
	GovernorRecords []executionGovernorRecord `json:"governorRecords,omitempty"`
}

type liveExecManager struct {
	mu                   sync.RWMutex
	rest                 *aster.RESTAuth
	tg                   *notify.Telegram
	dispatcher           *notify.Dispatcher
	notifyAccum          *notify.Accumulator
	path                 string
	tradesCSV            string
	fillReceipt          bool
	entryTimeout         time.Duration
	stopMode             string
	atrLen               int
	stopPct              float64
	tp1R                 float64
	tp2R                 float64
	tp3R                 float64
	tp1Frac              float64
	tp2Frac              float64
	tp3Frac              float64
	tpRatchetOnly        bool
	trailAfterTP         int
	trailStopPct         float64
	trailStopPctTP3      float64
	trailPctMin          float64
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
	riskOnMargin         bool
	riskMarginPct        float64
	hybridStopCfg        exitmgr.HybridStopConfig
	fundingEvery         time.Duration
	fundingBySym         map[string]time.Duration
	fundingHazardSec     time.Duration
	fundingSkipNewPos    time.Duration
	fundingExitEnable    bool
	fundingExitMinAge    time.Duration
	fundingExitMaxUpnl   float64
	fundingExitMinMFER   float64
	fundingExitWindow    time.Duration
	expensiveFundingRate float64
	stopTriggerRef       string
	tpTriggerRef         string
	accountAssets        []string
	snapshotPoll         time.Duration
	remoteImportEvery    time.Duration
	wsLevels             int
	wsSpeed              string
	liveAccount          liveAccountSnapshot
	userDataState        *aster.UserDataState
	userDataCancel       context.CancelFunc
	marketStates         map[string]*aster.MarketState
	marketCancels        map[string]context.CancelFunc
	featureCache         *featureRuntimeCache
	manualConfirm        bool
	manualRequests       map[string]manualManageRequest
	ladderCfg            ladderConfig
	fundsCfg             fundsManagerConfig
	reentryCfg           reentryConfig
	postWinCooldownCfg   postWinCooldownConfig
	transferManager      TransferManager
	accountReportCfg     accountReportConfig
	accountReport        accountReport
	entryBlockActive     bool
	entryBlockReason     string
	healthyAccountReads  int
	governorRecords      []executionGovernorRecord
	lastTransferStatus   string
	lastRemoteImportAt   time.Time
	lastFundsCheckAt     time.Time
	lastReconcileOKAt    time.Time
	lastReconcileErrAt   time.Time
	reconcileFailCount   int
	legalityFailCount    map[string]int
	symbolQuarantineTill map[string]time.Time
	unknownExecGuards    map[string]UnknownExecutionGuard
	htf1HBySymbol        map[string]HTFStructureSnapshot
}

type ladderConfig struct {
	StarterUSDT   float64
	StepUSDT      float64
	MaxTotalUSDT  float64
	OnlyIfGreen   bool
	MinAddPnLPct  float64
	MaxAdds       int
	OneSymbolOnly bool
}

type fundsManagerConfig struct {
	Enable                    bool
	PerpTargetUSDT            float64
	PerpFloorUSDT             float64
	TopupMinUSDT              float64
	SweepMinUSDT              float64
	SweepProfitEnable         bool
	SweepExcessOverTargetOnly bool
}

type reentryConfig struct {
	Enable       bool
	SizeUSDT     float64
	MaxPerSymbol int
	Cooldown     time.Duration
}

type postWinCooldownConfig struct {
	Enable       bool
	ThresholdPct float64
	Cooldown     time.Duration
}

type TransferManager interface {
	Supported() bool
	SpotAvailableUSDT() (float64, error)
	TransferSpotToPerp(amount float64) error
	TransferPerpToSpot(amount float64) error
}

type noopTransferManager struct{}

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

type liveStatus struct {
	Generated       time.Time           `json:"generated"`
	Mode            string              `json:"mode,omitempty"`
	ModeState       string              `json:"mode_state,omitempty"`
	DryRun          bool                `json:"dry_run"`
	LiveEnabled     bool                `json:"live_enabled"`
	ScannerBias     string              `json:"scanner_bias,omitempty"`
	ScannerLongs    []notify.ScanItem   `json:"scanner_longs,omitempty"`
	ScannerShorts   []notify.ScanItem   `json:"scanner_shorts,omitempty"`
	TopSymbol       string              `json:"top_symbol,omitempty"`
	TopSide         string              `json:"top_side,omitempty"`
	TopGrade        string              `json:"top_grade,omitempty"`
	TopScore        float64             `json:"top_score,omitempty"`
	TopSlope        float64             `json:"top_slope,omitempty"`
	TopTriggerState string              `json:"top_trigger_state,omitempty"`
	TopExitProfile  string              `json:"top_exit_profile,omitempty"`
	TopDiscovery    float64             `json:"top_discovery,omitempty"`
	TopTrigger      float64             `json:"top_trigger,omitempty"`
	TopExecution    float64             `json:"top_execution,omitempty"`
	TopCombined     float64             `json:"top_combined,omitempty"`
	TopVPSetup      string              `json:"top_vp_setup,omitempty"`
	TopVPLevel      float64             `json:"top_vp_level,omitempty"`
	TopVPTarget     float64             `json:"top_vp_target_level,omitempty"`
	TopVPStopMode   string              `json:"top_vp_stop_mode,omitempty"`
	TopVPTargetMode string              `json:"top_vp_target_mode,omitempty"`
	TopRejectReason string              `json:"top_reject_reason,omitempty"`
	TopDecision     string              `json:"top_decision,omitempty"`
	TopDecisionWhy  string              `json:"top_decision_why,omitempty"`
	TopRegimeTag    string              `json:"top_regime_tag,omitempty"`
	LongInPlay      int                 `json:"long_inplay"`
	ShortInPlay     int                 `json:"short_inplay"`
	AvailableUSDT   float64             `json:"available_usdt"`
	PaperSummary    string              `json:"paper_summary,omitempty"`
	Paper           *livePaperSnapshot  `json:"paper,omitempty"`
	PayoutCycleID   string              `json:"payout_cycle_id,omitempty"`
	PayoutNextAt    string              `json:"payout_next_at,omitempty"`
	PayoutLastAmt   float64             `json:"payout_last_amount,omitempty"`
	PayoutLastPnL   float64             `json:"payout_last_profit,omitempty"`
	PayoutLastType  string              `json:"payout_last_action,omitempty"`
	Exec            liveExecSnapshot    `json:"exec"`
	Live            liveAccountSnapshot `json:"live"`
	BlockedStrong   []string            `json:"blocked_strong,omitempty"`
}

type liveStatusStore struct {
	mu  sync.RWMutex
	cur liveStatus
}

type maintenanceWindow struct {
	Name      string
	Enabled   bool
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
	scanEvery := time.Duration(envInt("LIVE_SCAN_SEC", 20)) * time.Second
	dryRun := envBool("LIVE_DRY_RUN", true)
	minGrade := envStr("LIVE_MIN_GRADE", "B")
	reserveUSDT := envFloat("LIVE_RESERVE_USDT", 0)
	reserveMode := strings.ToLower(envStr("LIVE_RESERVE_MODE", "fixed")) // fixed|percent|dynamic
	reservePct := envFloat("LIVE_RESERVE_PCT", 50.0)
	tradeMargin := envFloat("LIVE_TRADE_MARGIN_USDT", 10)
	tradeMarginMode := strings.ToLower(envStr("LIVE_TRADE_MARGIN_MODE", "fixed")) // fixed|percent|slots|dynamic
	tradeMarginPct := envFloat("LIVE_TRADE_MARGIN_PCT", 10.0)
	tradeSlots := envInt("LIVE_TRADE_SLOTS", 5)
	if tradeSlots <= 0 {
		tradeSlots = 5
	}
	tradeMarginMin := envFloat("LIVE_TRADE_MARGIN_MIN_USDT", 5.0)
	tradeMarginMax := envFloat("LIVE_TRADE_MARGIN_MAX_USDT", 200.0)
	ladderCfg := loadLadderConfig(tradeMargin)
	reentryCfg := loadReentryConfig(ladderCfg.StarterUSDT)
	leverageMode := strings.ToLower(envStr("LIVE_LEVERAGE_MODE", "grade")) // grade|fixed|auto
	leverageFixed := envInt("LIVE_LEVERAGE_FIXED", 2)
	leverageMin := envInt("LIVE_LEVERAGE_MIN", 2)
	leverageMax := envInt("LIVE_MAX_LEVERAGE", 20)
	stopMode := strings.ToLower(envStr("LIVE_STOP_MODE", "hybrid"))
	targetMode := strings.ToLower(envStr("LIVE_TARGET_MODE", "hybrid"))
	vpMinTargetPct := envFloat("LIVE_VP_MIN_TARGET_PCT", 0.10)
	eventLockoutMin := envInt("LIVE_EVENT_LOCKOUT_MIN", 0)
	maxCorrelatedExposure := envFloat("LIVE_MAX_CORRELATED_USD_EXPOSURE", 0)
	corrGroups := parseCorrGroups(envStr("LIVE_CORR_GROUPS", ""))
	enableMomentumReversal := effectiveMomentumReversalEnabled()
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
	riskFallbackStopPct := envFloat("LIVE_STOP_PCT", 3.0)
	_ = reentryCfg
	_ = leverageMode
	_ = leverageFixed
	_ = leverageMin
	_ = eventLockoutMin
	_ = maxCorrelatedExposure
	_ = corrGroups
	_ = obFilterEnable
	_ = obLevels
	_ = riskShell
	_ = riskHoldHours
	_ = riskFallbackStopPct
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
	strategyTopN := envInt("LIVE_STRATEGY_TOP_N", 0)
	if strategyTopN < 0 {
		strategyTopN = 0
	}
	maxOpenPos := envInt("LIVE_MAX_OPEN_POS", 1)
	if maxOpenPos <= 0 {
		maxOpenPos = 1
	}
	maxOpenPerSide := envInt("LIVE_MAX_OPEN_PER_SIDE", 1)
	if maxOpenPerSide < 0 {
		maxOpenPerSide = 0
	}

	inplayCfg := inplay.Config{
		MinGrade:               envStr("INPLAY_MIN_GRADE", "C"),
		MinVolumeUSD:           envFloat("INPLAY_MIN_VOL_USD", 1_000_000),
		HistoryN:               envInt("INPLAY_HISTORY_N", 5),
		RiseN:                  envInt("INPLAY_RISE_N", 3),
		DropGradeScans:         envInt("INPLAY_DROP_SCANS", 2),
		FallScans:              envInt("INPLAY_FALL_SCANS", 2),
		TTL:                    time.Duration(envInt("INPLAY_TTL_MIN", 30)) * time.Minute,
		EnableStateDecay:       envBool("RANK_ENABLE_STATE_DECAY", true),
		StateDecayMin:          envFloat("RANK_STATE_DECAY_MIN", 25),
		EnableStalenessPenalty: envBool("RANK_ENABLE_STALENESS_PENALTY", true),
		StaleImpulseMin:        envFloat("RANK_STALE_IMPULSE_MIN", 20),
	}
	longTrk := inplay.NewTracker("long", inplayCfg)
	shortTrk := inplay.NewTracker("short", inplayCfg)

	candCfg := candidateSelectConfig{
		UseContinuous:           envBool("LIVE_RANK_USE_CONTINUOUS", false),
		MinNormalizedScore:      envFloat("LIVE_RANK_MIN_SCORE", 70.0),
		MinCompleteness:         envFloat("LIVE_RANK_MIN_COMPLETENESS", 0.0),
		MinConfidence:           envFloat("LIVE_RANK_MIN_CONFIDENCE", 0.0),
		ReversalMinScore:        envFloat("LIVE_REVERSAL_MIN_SCORE", 68.0),
		ReversalMinConfidence:   envFloat("LIVE_REVERSAL_MIN_CONFIDENCE", 0.0),
		ReversalMinComplete:     envFloat("LIVE_REVERSAL_MIN_COMPLETENESS", 0.0),
		ReversalMinStateMin:     envFloat("LIVE_REVERSAL_MIN_STATE_MIN", 1.0),
		ReversalShortCoolingMin: envFloat("LIVE_REVERSAL_SHORT_COOLING_MIN", 3.0),
		ReversalShortMinSlope:   envFloat("LIVE_REVERSAL_SHORT_MIN_SLOPE", 0.25),
	}
	entryQualityCfg := entryQualityConfig{
		EnableMetaGate:          envBool("LIVE_META_GATE_ENABLE", false),
		MinQuality:              envFloat("LIVE_META_MIN_QUALITY", 0.52),
		MinQualityCont:          envFloat("LIVE_META_MIN_QUALITY_CONT", envFloat("LIVE_META_MIN_QUALITY", 0.52)),
		MinQualityIgnite:        envFloat("LIVE_META_MIN_QUALITY_IGNITE", min(envFloat("LIVE_META_MIN_QUALITY", 0.52), 0.50)),
		MinQualityRev:           envFloat("LIVE_META_MIN_QUALITY_REV", min(envFloat("LIVE_META_MIN_QUALITY", 0.52), 0.48)),
		RequireStrategyMatch:    envBool("LIVE_REQUIRE_STRATEGY_MATCH", false),
		MinEntryConf:            envFloat("LIVE_MIN_ENTRY_CONF", 0.48),
		MinEntryConfCont:        envFloat("LIVE_MIN_ENTRY_CONF_CONT", envFloat("LIVE_MIN_ENTRY_CONF", 0.48)),
		MinEntryConfIgnite:      envFloat("LIVE_MIN_ENTRY_CONF_IGNITE", min(envFloat("LIVE_MIN_ENTRY_CONF", 0.48), 0.45)),
		MinEntryConfRev:         envFloat("LIVE_MIN_ENTRY_CONF_REV", min(envFloat("LIVE_MIN_ENTRY_CONF", 0.48), 0.40)),
		PersistenceOverride:     envBool("LIVE_PERSISTENCE_OVERRIDE_ENABLE", false),
		PersistMinQuality:       envFloat("LIVE_PERSISTENCE_OVERRIDE_MIN_QUALITY", 0.50),
		PersistMinScans:         envInt("LIVE_PERSISTENCE_OVERRIDE_MIN_SCANS", 3),
		PersistMinScore:         envFloat("LIVE_PERSISTENCE_OVERRIDE_MIN_SCORE", 80.0),
		PersistMinGrade:         envStr("LIVE_PERSISTENCE_OVERRIDE_MIN_GRADE", "B"),
		EnableScoreGate:         envBool("LIVE_ENTRY_SCORE_ENABLE", false),
		MinDiscovery:            envFloat("LIVE_DISCOVERY_MIN_SCORE", 0.0),
		MinTrigger:              envFloat("LIVE_TRIGGER_MIN_SCORE", 0.0),
		MinExecution:            envFloat("LIVE_EXECUTION_MIN_SCORE", 0.0),
		ScoreWeightDiscovery:    envFloat("LIVE_DISCOVERY_WEIGHT", 0.35),
		ScoreWeightTrigger:      envFloat("LIVE_TRIGGER_WEIGHT", 0.40),
		ScoreWeightExecution:    envFloat("LIVE_EXECUTION_WEIGHT", 0.25),
		DayUTCWeight:            envFloat("LIVE_DAYUTC_WEIGHT", 0.30),
		DayUTCMinAbsPct:         envFloat("LIVE_DAYUTC_MIN_ABS_PCT", 5.0),
		DayUTCScalePct:          envFloat("LIVE_DAYUTC_SCALE_PCT", 20.0),
		BlockContExhaustion:     envBool("LIVE_BLOCK_CONTINUATION_ON_EXHAUSTION", false),
		DayUTCMaturityBrake:     envBool("LIVE_DAYUTC_MATURITY_BRAKE_ENABLE", false),
		DayUTCMaturityPct:       envFloat("LIVE_DAYUTC_MATURITY_BRAKE_PCT", 25.0),
		RequireFreshPullback:    envBool("LIVE_REQUIRE_PULLBACK_AFTER_EXTREME_DAYUTC", false),
		PersistenceSoftOverride: envBool("LIVE_PERSISTENCE_SOFT_OVERRIDE_ENABLE", false),
		PersistSoftMetaMin:      envFloat("LIVE_PERSISTENCE_META_QUALITY_MIN", 0.45),
		PersistSoftMinSeen:      envInt("LIVE_PERSISTENCE_OVERRIDE_MIN_SEEN", 3),
		PersistSoftMinTopN:      envInt("LIVE_PERSISTENCE_OVERRIDE_MIN_TOPN", 2),
	}
	acceptanceCfg := acceptanceQueueConfig{
		TopN:                  envInt("LIVE_ACCEPTANCE_TOPN", 4),
		MaxAttemptsPerCycle:   envInt("LIVE_MAX_ENTRY_ATTEMPTS_PER_CYCLE", 3),
		MaxNewPositionsWindow: envInt("LIVE_MAX_NEW_POSITIONS_PER_WINDOW", 1),
		EntryWindow:           time.Duration(envInt("LIVE_ENTRY_WINDOW_SEC", 60)) * time.Second,
		RecentRejectTTL:       time.Duration(envInt("LIVE_RECENT_REJECT_TTL_SEC", 180)) * time.Second,
	}
	lifecycleCfg := candidateLifecycleConfig{
		Enable:        effectiveCandidateMemoryEnabled(),
		ArmScans:      envInt("LIVE_CANDIDATE_ARM_SCANS", 2),
		ReadyScans:    envInt("LIVE_CANDIDATE_READY_SCANS", 3),
		ExpireAfter:   time.Duration(envInt("LIVE_CANDIDATE_EXPIRE_MIN", 20)) * time.Minute,
		ReadyMinScore: envFloat("LIVE_CANDIDATE_READY_MIN_SCORE", 65),
		ReadyMinSlope: envFloat("LIVE_CANDIDATE_READY_MIN_SLOPE", 0.01),
	}
	triggerCfg := normalizeTriggerLifecycleConfig(triggerLifecycleConfig{
		Enable:             effectiveTriggerMemoryEnabled(),
		ArmScans:           envInt("LIVE_TRIGGER_ARM_SCANS", 1),
		ReadyScans:         envInt("LIVE_TRIGGER_READY_SCANS", 2),
		ReversalReadyScans: envInt("LIVE_TRIGGER_REV_READY_SCANS", 3),
		InvalidateScans:    envInt("LIVE_TRIGGER_INVALIDATE_SCANS", 2),
		ExpireAfter:        time.Duration(envInt("LIVE_TRIGGER_EXPIRE_MIN", 12)) * time.Minute,
		ArmedBoost:         envFloat("LIVE_TRIGGER_ARMED_BOOST", 0.06),
		ReadyBoost:         envFloat("LIVE_TRIGGER_READY_BOOST", 0.12),
		InvalidPenalty:     envFloat("LIVE_TRIGGER_INVALID_PENALTY", 0.18),
	})
	_ = triggerCfg
	if lifecycleCfg.ArmScans < 1 {
		lifecycleCfg.ArmScans = 1
	}
	if lifecycleCfg.ReadyScans < lifecycleCfg.ArmScans {
		lifecycleCfg.ReadyScans = lifecycleCfg.ArmScans
	}
	if lifecycleCfg.ExpireAfter <= 0 {
		lifecycleCfg.ExpireAfter = 20 * time.Minute
	}
	if entryQualityCfg.MinEntryConf < 0 {
		entryQualityCfg.MinEntryConf = 0
	}
	if entryQualityCfg.MinEntryConf > 1 {
		entryQualityCfg.MinEntryConf = 1
	}
	if entryQualityCfg.MinQuality < 0 {
		entryQualityCfg.MinQuality = 0
	}
	if entryQualityCfg.MinQuality > 1 {
		entryQualityCfg.MinQuality = 1
	}
	entryQualityCfg.MinQualityCont = clamp(entryQualityCfg.MinQualityCont, 0, 1)
	entryQualityCfg.MinQualityIgnite = clamp(entryQualityCfg.MinQualityIgnite, 0, 1)
	entryQualityCfg.MinQualityRev = clamp(entryQualityCfg.MinQualityRev, 0, 1)
	entryQualityCfg.MinEntryConfCont = clamp(entryQualityCfg.MinEntryConfCont, 0, 1)
	entryQualityCfg.MinEntryConfIgnite = clamp(entryQualityCfg.MinEntryConfIgnite, 0, 1)
	entryQualityCfg.MinEntryConfRev = clamp(entryQualityCfg.MinEntryConfRev, 0, 1)
	if entryQualityCfg.PersistMinQuality < 0 {
		entryQualityCfg.PersistMinQuality = 0
	}
	if entryQualityCfg.PersistMinQuality > 1 {
		entryQualityCfg.PersistMinQuality = 1
	}
	if entryQualityCfg.PersistMinScans < 1 {
		entryQualityCfg.PersistMinScans = 1
	}
	entryQualityCfg.MinDiscovery = clamp(entryQualityCfg.MinDiscovery, 0, 1)
	entryQualityCfg.MinTrigger = clamp(entryQualityCfg.MinTrigger, 0, 1)
	entryQualityCfg.MinExecution = clamp(entryQualityCfg.MinExecution, 0, 1)
	if entryQualityCfg.DayUTCWeight < 0 {
		entryQualityCfg.DayUTCWeight = 0
	}
	if entryQualityCfg.DayUTCWeight > 0.85 {
		entryQualityCfg.DayUTCWeight = 0.85
	}
	if entryQualityCfg.DayUTCMinAbsPct < 0 {
		entryQualityCfg.DayUTCMinAbsPct = -entryQualityCfg.DayUTCMinAbsPct
	}
	if entryQualityCfg.DayUTCScalePct <= 0 {
		entryQualityCfg.DayUTCScalePct = 20.0
	}
	if entryQualityCfg.DayUTCScalePct < entryQualityCfg.DayUTCMinAbsPct {
		entryQualityCfg.DayUTCScalePct = maxFloat(entryQualityCfg.DayUTCMinAbsPct, 5.0)
	}
	if acceptanceCfg.TopN < 1 {
		acceptanceCfg.TopN = 1
	}
	if acceptanceCfg.MaxAttemptsPerCycle < 1 {
		acceptanceCfg.MaxAttemptsPerCycle = acceptanceCfg.TopN
	}
	if acceptanceCfg.MaxNewPositionsWindow < 1 {
		acceptanceCfg.MaxNewPositionsWindow = 1
	}
	if acceptanceCfg.EntryWindow <= 0 {
		acceptanceCfg.EntryWindow = time.Minute
	}
	if acceptanceCfg.RecentRejectTTL < 0 {
		acceptanceCfg.RecentRejectTTL = 0
	}
	rankSortCfg := rankSortConfig{
		UseConfidence:      envBool("LIVE_RANK_SORT_USE_CONFIDENCE", true),
		ConfidenceWeight:   envFloat("LIVE_RANK_SORT_CONF_WEIGHT", 1.0),
		UseCompleteness:    envBool("LIVE_RANK_SORT_USE_COMPLETENESS", true),
		CompletenessWeight: envFloat("LIVE_RANK_SORT_COMPLETENESS_WEIGHT", 0.6),
		UseReliability:     envBool("LIVE_RANK_SORT_USE_RELIABILITY", false),
		ReliabilityWeight:  envFloat("LIVE_RANK_SORT_RELIABILITY_WEIGHT", 1.0),
		UseVolume:          envBool("LIVE_RANK_SORT_USE_VOLUME", true),
		VolumeWeight:       envFloat("LIVE_RANK_SORT_VOLUME_WEIGHT", 12.0),
	}
	reliabilityStore := reliability.NewInMemoryStore(reliability.Config{
		Enabled:    envBool("RANK_ENABLE_RELIABILITY", false),
		MaxPenalty: envFloat("RANK_RELIABILITY_MAX_PENALTY", 8),
		MaxBonus:   envFloat("RANK_RELIABILITY_MAX_BONUS", 4),
	})

	client := aster.New("")
	featureCache := newFeatureRuntimeCache(client.LoadCandles)
	rest := buildRESTFromConfig()
	if rest == nil {
		fmt.Println("live: no credentials found, forcing DRY_RUN mode")
		dryRun = true
	}
	safety := loadSafetyConfig(reserveUSDT, tradeMargin)
	if !dryRun && !safety.enableLiveTrading {
		fmt.Println("live: LIVE_DRY_RUN=0 but LIVE_ENABLE_LIVE_TRADING is not enabled, forcing DRY_RUN")
		dryRun = true
	}
	modeCtrl := newRuntimeModeController(dryRun, safety.enableLiveTrading, dryRun)
	tg := newTelegramSink()
	notifyAccum := notify.NewAccumulator()
	notifyDispatcher := notify.NewDispatcher(notify.TelegramRouteSender{Telegram: tg}, notifyAccum)
	execMgr := newLiveExecManager(rest, tg)
	if execMgr != nil {
		execMgr.dispatcher = notifyDispatcher
		execMgr.notifyAccum = notifyAccum
	}
	if execMgr != nil {
		if nClosed, nImported, err := execMgr.ReconcileBootState(); err != nil {
			fmt.Println("live: boot reconcile warning:", err)
		} else if nClosed > 0 || nImported > 0 {
			emitNotifyEvent(notifyDispatcher, notify.Event{
				Key:      "BOOT_RECONCILE_COMPLETE",
				Title:    "BOOT RECONCILE COMPLETE",
				Class:    notify.ClassLifecycle,
				Severity: notify.SeverityNotice,
				Route:    notify.RouteNormal,
				Message:  "boot reconcile completed",
				Metadata: map[string]string{
					"closed_local":    fmt.Sprintf("%d", nClosed),
					"imported_remote": fmt.Sprintf("%d", nImported),
				},
			})
		}
		report := execMgr.ensureAccountReportFresh(time.Now().UTC(), 15*time.Second)
		if !dryRun && !accountHealthAllowsLiveBoot(report) && !envBool("LIVE_ALLOW_UNHEALTHY_ACCOUNT_AUTH", false) {
			fmt.Printf("live: refusing live boot because account auth is unhealthy (health=%s detail=%s). Set LIVE_ALLOW_UNHEALTHY_ACCOUNT_AUTH=1 to override.\n",
				firstNonEmpty(report.Health, "failed"), firstNonEmpty(report.HealthDetail, "none"))
			os.Exit(1)
		}
	}
	statusStore := newLiveStatusStore()
	statusAddr := envStr("LIVE_STATUS_ADDR", ":8787")
	if err := startLiveStatusServer(statusAddr, statusStore); err != nil {
		fmt.Println("live status server error:", err)
		os.Exit(1)
	}
	watchCfg := loadWatchConfig()
	watcher := newWatchRuntime(watchCfg, client)
	wallSignals := map[string]wallSignal{}
	_ = wallSignals
	liveWatchEvery = 0
	liveWatchTick = nil
	livePriorityEvery = 0
	livePriorityActive = nil
	if watcher != nil {
		liveWatchEvery = watchCfg.Every
		livePriorityEvery = watchCfg.PriorityEvery
		liveWatchTick = func(ts time.Time) bool {
			return watcher.Tick(ts)
		}
	}
	var cmdCtx *telegramCommandCtx
	tgVerbose := envBool("LIVE_TG_VERBOSE", false)
	_ = tgVerbose
	digestEvery := time.Duration(envInt("LIVE_TG_DIGEST_MIN", 60)) * time.Minute
	if digestEvery < time.Minute {
		digestEvery = 60 * time.Minute
	}
	digestLimit := envInt("LIVE_TG_LIST_LIMIT", 12)
	if digestLimit <= 0 {
		digestLimit = 12
	}
	tradeUpdateEvery := time.Duration(envInt("LIVE_TG_TRADE_UPDATE_MIN", 10)) * time.Minute
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
	overnightReportHour, overnightReportMinute := parseHHMM(envStr("LIVE_TG_OVERNIGHT_REPORT_TIME", "07:00"), 7, 0)
	dailyReportHour, dailyReportMinute := parseHHMM(envStr("LIVE_TG_DAILY_REPORT_TIME", "19:00"), 19, 0)
	reportDayOffset := envInt("LIVE_TG_DAILY_REPORT_DAY_OFFSET", 0)
	receiptEnable := envBool("LIVE_TG_DAILY_RECEIPT_ENABLE", false)
	receiptLimit := envInt("LIVE_TG_DAILY_RECEIPT_LIMIT", 25)
	if receiptLimit <= 0 {
		receiptLimit = 25
	}
	liveReceiptEnable := envBool("LIVE_TG_DAILY_LIVE_RECEIPT_ENABLE", false)
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
	digestEnable := envBool("LIVE_TG_DIGEST_ENABLE", true)
	overnightReportEnable := envBool("LIVE_TG_OVERNIGHT_REPORT_ENABLE", true)
	dailyReportEnable := envBool("LIVE_TG_DAILY_REPORT_ENABLE", true)
	sodEnable := envBool("LIVE_TG_SOD_ENABLE", false)
	preUSEnable := envBool("LIVE_TG_PRE_US_ENABLE", false)
	eodLegacyEnable := envBool("LIVE_TG_EOD_ENABLE", false)
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
	maintWarmup := time.Duration(envInt("LIVE_MAINT_WARMUP_MIN", 0)) * time.Minute
	preEODEntryBlockMin := 0
	postSLCooldown := time.Duration(envInt("POST_SL_COOLDOWN_MIN", 30)) * time.Minute
	allowDeadSessionTrading := true
	_ = maintWarmup
	_ = preEODEntryBlockMin
	_ = allowDeadSessionTrading
	inertiaEnable := envBool("LIVE_INERTIA_BREAKER_ENABLE", false)
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
	maintenanceWindows := runtimeMaintenanceWindows()
	maintEOD := maintenanceWindow{}
	for _, w := range maintenanceWindows {
		if w.ForceFlat {
			maintEOD = w
			break
		}
	}
	nextDigestAt := time.Now().UTC().Add(10 * time.Second)
	nextTradeUpdateAt := time.Now().UTC().Add(45 * time.Second)
	lastDailyReportDay := ""
	lastDailyLiveReceiptDay := ""
	lastSODReportDay := ""
	lastPreUSReportDay := ""
	lastM2ReportDay := ""
	lastOvernightReportDay := ""
	last1900ReportDay := ""
	lastHourlyKey := ""
	lastPaperTradeUpdateSig := ""
	lastLiveTradeUpdateSig := ""
	var lastPulseSentAt time.Time
	var lastCriticalProtectSummaryAt time.Time
	maintState := maintenanceState{
		LastStartDay: map[string]string{},
		LastEndDay:   map[string]string{},
		LastEndAt:    map[string]time.Time{},
		FlatDoneDay:  map[string]string{},
		HookDoneDay:  map[string]string{},
	}
	asiaQualityEnable := envBool("LIVE_ASIA_QUALITY_ENABLE", false)
	asiaMinGrade := envStr("LIVE_ASIA_MIN_GRADE", "B")
	asiaStrongConfMin := envFloat("LIVE_ASIA_STRONG_CONF_MIN", 0.60)
	asiaMinSlope := envFloat("LIVE_ASIA_MIN_SLOPE", 0.01)
	_, _, _, _ = asiaQualityEnable, asiaMinGrade, asiaStrongConfMin, asiaMinSlope
	paper := newPaperTrader(dryRun, reserveUSDT, maxOpenPos)
	if paper != nil {
		paper.featureCache = featureCache
	}
	if execMgr != nil {
		execMgr.featureCache = featureCache
	}
	if paper != nil && tg != nil && tg.Enabled() {
		paper.onExit = func(msg string) {
			tg.Sendf("%s", msg)
		}
	}
	cmdCtx = &telegramCommandCtx{
		tg:          tg,
		rest:        rest,
		execMgr:     execMgr,
		paper:       paper,
		safety:      safety,
		status:      statusStore,
		mode:        modeCtrl,
		meta:        map[string]symbolMeta{},
		decisions:   map[string]operatorDecision{},
		suggestions: map[string]operatorSuggestion{},
		suggestTTL:  time.Duration(envInt("LIVE_TG_SUGGEST_TTL_MIN", 15)) * time.Minute,
	}
	livePriorityActive = cmdCtx.hasActiveSuggestions
	if envBool("LIVE_TG_COMMANDS_ENABLE", true) {
		go cmdCtx.run()
	}
	payoutMgr := newPayoutManager()
	if (paper != nil || execMgr != nil || payoutMgr != nil) && envBool("LIVE_STARTUP_DEBUG", false) {
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
	fmt.Printf("LIVE STARTED | mode=%s\n", map[bool]string{true: "PAPER", false: "LIVE"}[dryRun])
	modeLabel := "LIVE"
	if dryRun {
		modeLabel = "PAPER"
	}
	if modeCtrl.requestedOperatingMode() == runtimeModeLive {
		fmt.Println("live: LIVE_RUNTIME_MODE=live selected; autonomous paper entry remains disabled outside paper mode")
	}
	for _, line := range startupSummaryLines(modeLabel, scanEvery, watchCfg, ladderCfg, reentryCfg, safety, execMgr) {
		fmt.Printf("  %s\n", line)
	}
	for _, line := range startupWarningLines(ladderCfg, safety, execMgr) {
		fmt.Printf("  %s\n", line)
	}
	fmt.Printf("  min_grade=%s | mode=manual_scanner\n", strings.ToUpper(minGrade))
	if execMgr != nil {
		fmt.Printf("  %s\n", compactAccountSummaryLine(execMgr.ensureAccountReportFresh(time.Now().UTC(), 30*time.Second)))
	}
	fmt.Println()
	emitNotifyEvent(notifyDispatcher, notify.Event{
		Key:      "LIVE_STARTED",
		Title:    "LIVE STARTED",
		Class:    notify.ClassLifecycle,
		Severity: notify.SeverityNotice,
		Route:    notify.RouteNormal,
		Message:  "live process started",
		Metadata: map[string]string{
			"mode":              modeLabel,
			"scan_watch":        fmt.Sprintf("%s/%s", scanEvery, watchCfg.Every),
			"starter_add_max":   fmt.Sprintf("%.2f/%.2f/%.2f", ladderCfg.StarterUSDT, ladderCfg.StepUSDT, ladderCfg.MaxTotalUSDT),
			"min_avail_reentry": fmt.Sprintf("%.2f/%.2f", safety.minAvailUSDT, reentryCfg.SizeUSDT),
		},
	})

	requireShadowDays := envInt("LIVE_REQUIRE_PAPER_DAYS", 0)
	shadowEquityFile := envStr("LIVE_PAPER_EQUITY_FILE", "out/paper_equity.csv")
	shadowWarnAt := time.Time{}
	lastTopKey := ""
	_ = requireShadowDays
	_ = shadowEquityFile
	_ = shadowWarnAt
	_ = lastTopKey
	lastOrderAt := time.Time{}
	lastOrderBySymbol := map[string]time.Time{}
	lastOrderBySymbolSide := map[string]time.Time{}
	orderCountByDay := map[string]int{}
	orderCountByHour := map[string]int{}
	symbolStopoutLockUntil := map[string]time.Time{}
	candidateMem := map[string]candidateMemory{}
	triggerMem := map[string]triggerMemory{}
	recentRejects := map[string]recentRejectMemory{}
	sessionChurns := map[string]*sessionChurn{}
	handledClosedLive := map[string]time.Time{}
	recentEntryAttempts := []time.Time{}
	_ = lastOrderAt
	_ = lastOrderBySymbol
	_ = lastOrderBySymbolSide
	_ = orderCountByDay
	_ = orderCountByHour
	_ = symbolStopoutLockUntil
	_ = triggerMem
	_ = recentRejects
	_ = recentEntryAttempts
	missed := newMissedTracker()
	if cmdCtx != nil {
		cmdCtx.missed = missed
	}
	livePriorityActive = func() bool {
		active := cmdCtx != nil && cmdCtx.hasActiveSuggestions()
		if missed != nil && missed.HasPriority(time.Now().UTC()) {
			active = true
		}
		return active
	}
	dayStartEq := map[string]float64{}
	killDay := map[string]bool{}
	lastDayUTCResetKey := ""
	reserveGate := newReserveLockGate()
	discoveryCfg := loadDiscoveryConfig()
	gateCfg := loadEntryGateConfig()
	symbolCooldownSameSide := throttle.NewCooldown(time.Duration(envInt("LIVE_THROTTLE_SYMBOL_SAME_SIDE_COOLDOWN_SECONDS", envInt("LIVE_THROTTLE_SYMBOL_COOLDOWN_SECONDS", 300))) * time.Second)
	symbolCooldownFlipSide := throttle.NewCooldown(time.Duration(envInt("LIVE_THROTTLE_SYMBOL_FLIP_SIDE_COOLDOWN_SECONDS", 120)) * time.Second)
	_ = reserveGate
	_ = discoveryCfg
	_ = gateCfg
	_ = symbolCooldownSameSide
	_ = symbolCooldownFlipSide
	intentDedupe := throttle.NewDedupe(time.Duration(envInt("LIVE_THROTTLE_DEDUPE_WINDOW_SECONDS", 120)) * time.Second)
	_ = intentDedupe
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
	terminalPrintEvery := time.Duration(envInt("LIVE_TERMINAL_PRINT_SEC", 20)) * time.Second
	if terminalPrintEvery <= 0 {
		terminalPrintEvery = 20 * time.Second
	}
	nextTerminalPrintAt := time.Now().UTC()
	runtimeLoop := newLiveRuntimeLoop()
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	defer runtimeCancel()
	runtimeLoop.startScannerWorker(runtimeCtx, scanEvery, func(now time.Time) (liveScannerSnapshot, bool) {
		mkts := client.FetchAllMarkets()
		longRows := market.ScoreAndFilter(mkts)
		shortRows := market.ScoreAndFilterShort(mkts)
		longRows = filterBlockedScored(longRows, safety.blockSymbols)
		shortRows = filterBlockedScored(shortRows, safety.blockSymbols)
		if discoveryCfg.Enabled {
			baseLongRows := append([]market.Scored(nil), longRows...)
			baseShortRows := append([]market.Scored(nil), shortRows...)
			candleBySymbol := buildDiscoveryCandles(featureCache, longRows, shortRows, discoveryCfg)
			snaps := discovery.BuildSnapshots(longRows, candleBySymbol)
			universe := discovery.SelectUniverse(snaps, discoveryCfg)
			if len(universe) > 0 {
				longRows = filterUniverseRows(longRows, universe)
				shortRows = filterUniverseRows(shortRows, universe)
				if len(longRows) == 0 && len(shortRows) == 0 {
					longRows = baseLongRows
					shortRows = baseShortRows
				}
			} else {
				longRows = baseLongRows
				shortRows = baseShortRows
			}
		}
		metaBySymbol := buildSymbolMeta(longRows, shortRows)
		longEligible, longConf := buildEligible(client, longRows, "long", gradeTopN)
		shortEligible, shortConf := buildEligible(client, shortRows, "short", gradeTopN)
		longTrk.Update(now, longEligible, longConf)
		shortTrk.Update(now, shortEligible, shortConf)
		return liveScannerSnapshot{
			At:          now,
			LongInPlay:  longTrk.Entries(),
			ShortInPlay: shortTrk.Entries(),
			MetaBySym:   metaBySymbol,
		}, true
	})
	runtimeLoop.startWatcherWorker(runtimeCtx, time.Second, func(now time.Time, scan liveScannerSnapshot) (liveWatcherSnapshot, bool) {
		longInPlay := append([]inplay.Entry(nil), scan.LongInPlay...)
		shortInPlay := append([]inplay.Entry(nil), scan.ShortInPlay...)
		metaBySymbol := copySymbolMetaMap(scan.MetaBySym)
		watchSet := map[string]struct{}{}
		for _, sym := range topNSymbolsFromEntries(longInPlay, 6) {
			watchSet[sym] = struct{}{}
		}
		for _, sym := range topNSymbolsFromEntries(shortInPlay, 6) {
			watchSet[sym] = struct{}{}
		}
		if paper != nil {
			for _, sym := range paper.OpenSymbols() {
				watchSet[sym] = struct{}{}
			}
		}
		if execMgr != nil {
			for _, sym := range execMgr.ActiveSymbols() {
				watchSet[sym] = struct{}{}
			}
			for _, sym := range execMgr.PendingProtectionSymbols() {
				watchSet[sym] = struct{}{}
			}
		}
		watchExtra := make([]string, 0, len(watchSet))
		for sym := range watchSet {
			watchExtra = append(watchExtra, sym)
		}
		sort.Strings(watchExtra)
		if watcher != nil {
			watcher.SetSnapshot(longInPlay, shortInPlay, metaBySymbol, watchExtra, nil)
			_ = watcher.Tick(now)
			for raw, meta := range watcher.MetaSnapshot() {
				cur := metaBySymbol[raw]
				if meta.LastPrice > 0 {
					cur.LastPrice = meta.LastPrice
				}
				if meta.Bid > 0 {
					cur.Bid = meta.Bid
				}
				if meta.Ask > 0 {
					cur.Ask = meta.Ask
				}
				metaBySymbol[raw] = cur
			}
			return liveWatcherSnapshot{
				At:              now,
				LongInPlay:      longInPlay,
				ShortInPlay:     shortInPlay,
				MetaBySym:       metaBySymbol,
				FlowBySym:       watcher.FlowMetrics(),
				WatchSymbols:    watchExtra,
				WallSignalsBySy: watcher.WallSignals(),
			}, true
		}
		return liveWatcherSnapshot{
			At:           now,
			LongInPlay:   longInPlay,
			ShortInPlay:  shortInPlay,
			MetaBySym:    metaBySymbol,
			FlowBySym:    map[string]flowMetrics{},
			WatchSymbols: watchExtra,
		}, true
	})
	runtimeLoop.startAccountHealthWorker(runtimeCtx, 5*time.Second, func(now time.Time) accountHealthSummary {
		if execMgr == nil {
			return accountHealthSummary{State: "healthy"}
		}
		report := execMgr.ensureAccountReportFresh(now, 30*time.Second)
		return accountHealthSummary{
			State:                 strings.ToLower(strings.TrimSpace(report.Health)),
			SignedUserDataBackoff: signedUserDataBackoffActive(now),
		}
	})
	setLiveEntryAccountHealthProvider(func() accountHealthSummary {
		if snap, ok := runtimeLoop.latestHealth(); ok {
			return snap.Summary
		}
		return accountHealthSummary{State: "healthy"}
	})
	decisionEvery := time.Duration(envInt("LIVE_DECISION_MS", 350)) * time.Millisecond
	if decisionEvery < 250*time.Millisecond {
		decisionEvery = 250 * time.Millisecond
	}
	if decisionEvery > 500*time.Millisecond {
		decisionEvery = 500 * time.Millisecond
	}
	decisionTicker := time.NewTicker(decisionEvery)
	defer decisionTicker.Stop()
	for range decisionTicker.C {
		cycleStart := time.Now().UTC()
		now := cycleStart
		emitTerminal := !now.Before(nextTerminalPrintAt)
		if emitTerminal {
			nextTerminalPrintAt = now.Add(terminalPrintEvery)
		}
		resetKey := dayUTCResetKey(now)
		if resetKey != lastDayUTCResetKey {
			if lastDayUTCResetKey != "" {
				longTrk.Reset()
				shortTrk.Reset()
				candidateMem = map[string]candidateMemory{}
				triggerMem = map[string]triggerMemory{}
				recentRejects = map[string]recentRejectMemory{}
				sessionChurns = map[string]*sessionChurn{}
				fmt.Printf("live: dayutc reset state cleared anchor=%s rolling24_context=preserved\n", resetKey)
			}
			lastDayUTCResetKey = resetKey
		}
		operatingMode := runtimeModeLive
		if modeCtrl != nil {
			modeDryRun, modeLiveEnabled, modePaperEnabled := modeCtrl.snapshot()
			dryRun = modeDryRun
			safety.enableLiveTrading = modeLiveEnabled
			if paper != nil {
				paper.enabled = modePaperEnabled
			}
			operatingMode = modeCtrl.operatingMode()
		}
		cacheStatsStart := featureCache.statsSnapshot()
		localMaintNow := now.In(maintLoc)
		maintWindow, inMaint := activeMaintenanceWindow(localMaintNow, true, maintenanceWindows...)
		waitAndReport := func() {
			cycleEnd := time.Now().UTC()
			cacheStatsEnd := featureCache.statsSnapshot()
			cacheDelta := cacheStatsEnd.delta(cacheStatsStart)
			eventLog.Emit(stats.Event{
				Timestamp:         cycleEnd,
				Type:              "METRICS_SNAPSHOT",
				TF:                "1m",
				Reason:            "cycle_complete mode=decision_worker",
				LoopMs:            float64(cycleEnd.Sub(cycleStart).Milliseconds()),
				CacheHits:         cacheDelta.totalHits(),
				CacheMisses:       cacheDelta.totalMisses(),
				CacheCandleHits:   cacheDelta.CandleHits,
				CacheCandleMisses: cacheDelta.CandleMisses,
				CacheMicroHits:    cacheDelta.MicroHits,
				CacheMicroMisses:  cacheDelta.MicroMisses,
				CacheEMAHits:      cacheDelta.EMAHits,
				CacheEMAMisses:    cacheDelta.EMAMisses,
				CacheEvictions:    cacheDelta.Evictions,
				CacheEntries:      cacheStatsEnd.CandleKeys + cacheStatsEnd.MicroKeys + cacheStatsEnd.EMAKeys,
			})
			if envBool("LIVE_PERF_LOG_ENABLE", false) {
				fmt.Printf("live: perf mode=decision_worker loop_ms=%.1f cache_hits=%d cache_misses=%d candle=%d/%d micro=%d/%d ema=%d/%d entries=%d evictions=%d\n",
					float64(cycleEnd.Sub(cycleStart).Milliseconds()),
					cacheDelta.totalHits(),
					cacheDelta.totalMisses(),
					cacheDelta.CandleHits,
					cacheDelta.CandleMisses,
					cacheDelta.MicroHits,
					cacheDelta.MicroMisses,
					cacheDelta.EMAHits,
					cacheDelta.EMAMisses,
					cacheStatsEnd.CandleKeys+cacheStatsEnd.MicroKeys+cacheStatsEnd.EMAKeys,
					cacheDelta.Evictions,
				)
			}
		}
		scanSnap, ok := runtimeLoop.latestScanner()
		if !ok {
			continue
		}
		watchSnap, wOK := runtimeLoop.latestWatcher()
		if !wOK {
			watchSnap = liveWatcherSnapshot{
				At:          scanSnap.At,
				LongInPlay:  append([]inplay.Entry(nil), scanSnap.LongInPlay...),
				ShortInPlay: append([]inplay.Entry(nil), scanSnap.ShortInPlay...),
				MetaBySym:   copySymbolMetaMap(scanSnap.MetaBySym),
				FlowBySym:   map[string]flowMetrics{},
			}
		}
		longInPlay := append([]inplay.Entry(nil), watchSnap.LongInPlay...)
		shortInPlay := append([]inplay.Entry(nil), watchSnap.ShortInPlay...)
		metaBySymbol := copySymbolMetaMap(watchSnap.MetaBySym)
		cmdCtx.setMeta(metaBySymbol)
		longCurrent := sideEntryMap(longInPlay)
		shortCurrent := sideEntryMap(shortInPlay)
		flowMetricsBySymbol := copyFlowMetricsMap(watchSnap.FlowBySym)
		wallSignals = copyWallSignalsMap(watchSnap.WallSignalsBySy)
		momBySymbol := buildMomentumIndex(longInPlay, shortInPlay)
		paperDepth := map[string]aster.OrderBook{}
		// Manual-only mode: no autonomous trade management/exits.
		inCriticalProtection := execMgr != nil && execMgr.hasCriticalProtectionState()
		if !inMaint {
			if paper.enabled && tg != nil && tg.Enabled() {
				if now.After(nextTradeUpdateAt) {
					if len(paper.positions) > 0 {
						if sig := paper.TradeUpdateSignature(metaBySymbol, tradeUpdateTop); sig != "" && sig != lastPaperTradeUpdateSig {
							if msg := paper.TradeUpdateMessage(metaBySymbol, tradeUpdateTop); msg != "" {
								tg.Sendf("%s", tgPre(msg))
								lastPaperTradeUpdateSig = sig
							}
						}
						nextTradeUpdateAt = now.Add(tradeUpdateEvery)
					} else {
						lastPaperTradeUpdateSig = ""
						nextTradeUpdateAt = now.Add(time.Minute)
					}
				}
				localNow := now.In(paper.reportLoc)
				if eodLegacyEnable && (localNow.Hour() > eodReportHour || (localNow.Hour() == eodReportHour && localNow.Minute() >= eodReportMinute)) {
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
				if sodEnable && (localNow.Hour() > sodReportHour || (localNow.Hour() == sodReportHour && localNow.Minute() >= sodReportMinute)) {
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
			if tg != nil && tg.Enabled() && execMgr != nil {
				if inCriticalProtection {
					if shouldSendPulse(now, lastCriticalProtectSummaryAt, 3*time.Minute) {
						lines := execMgr.criticalProtectionSummaryLines(3)
						if len(lines) > 0 {
							tg.Sendf("%s", notify.BuildManagementStatusCard(notify.ManageStateDegraded, "", "",
								append([]string{
									"<b>Critical protection mode:</b> suppressing non-essential digests/live updates",
								}, lines...)...,
							))
						}
						lastCriticalProtectSummaryAt = now
					}
				} else if now.After(nextTradeUpdateAt) {
					if execMgr.ActiveCount() > 0 {
						if sig := execMgr.liveTradeUpdateSignature(metaBySymbol); sig != "" && sig != lastLiveTradeUpdateSig && !(hourlyEnable && localMaintNow.Minute() == 0) {
							if msg := execMgr.liveTradeUpdateMessage(metaBySymbol); msg != "" {
								tg.Sendf("%s", msg)
								lastLiveTradeUpdateSig = sig
							}
						}
						nextTradeUpdateAt = now.Add(tradeUpdateEvery)
					} else {
						lastLiveTradeUpdateSig = ""
						nextTradeUpdateAt = now.Add(time.Minute)
					}
				}
			}
			if tg != nil && tg.Enabled() && execMgr != nil && liveReceiptEnable {
				localNow := now.In(execMgr.reportLoc)
				if eodLegacyEnable && (localNow.Hour() > eodReportHour || (localNow.Hour() == eodReportHour && localNow.Minute() >= eodReportMinute)) {
					dayKey := localNow.AddDate(0, 0, reportDayOffset).Format("2006-01-02")
					if dayKey != lastDailyLiveReceiptDay {
						tg.Sendf("%s", notify.BuildEventHTML("🧾", "EOD DAILY DISPATCH",
							fmt.Sprintf("<b>Day:</b> %s", dayKey),
							fmt.Sprintf("<b>Time:</b> %s", localNow.Format("15:04 MST")),
						))
						if msg, ok := execMgr.DailyReportMessage(dayKey); ok {
							tg.Sendf("%s", msg)
						}
						if msg, ok := execMgr.DailyReceiptMessage(dayKey, liveReceiptLimit); ok {
							tg.Sendf("%s", tgPre(msg))
						}
						lastDailyLiveReceiptDay = dayKey
					}
				}
			}
			if missed != nil {
				missed.Update(now, metaBySymbol, longCurrent, shortCurrent, eventLog)
			}
		}
		externalFlow := externalFlowFeed.Snapshot(now)
		if paper.enabled {
			runPaperLifecycle(now, paper, metaBySymbol, paperDepth, longCurrent, shortCurrent, momBySymbol, flowMetricsBySymbol, externalFlow)
		}
		if execMgr != nil {
			execMgr.ApplyMomentumExit(now, momBySymbol, flowMetricsBySymbol, externalFlow)
		}
		if !inMaint {
			if emitTerminal {
				printScanHeader(localMaintNow)
				printUnifiedInPlay(longInPlay, shortInPlay, metaBySymbol)
			}
			eventLog.Emit(stats.Event{
				Timestamp: now,
				Type:      "METRICS_SNAPSHOT",
				TF:        "1m",
				Reason:    fmt.Sprintf("long_inplay=%d short_inplay=%d", len(longInPlay), len(shortInPlay)),
			})
			if tg != nil && tg.Enabled() && hourlyEnable && digestEnable && ((paper != nil && paper.enabled) || execMgr != nil) && !inCriticalProtection {
				hk := localMaintNow.Format("2006-01-02 15")
				if localMaintNow.Minute() == 0 && hk != lastHourlyKey {
					if shouldSendPulse(now, lastPulseSentAt, 10*time.Minute) {
						snap := buildNotifySnapshot(modeLabel, localMaintNow, paper, execMgr, metaBySymbol, longInPlay, shortInPlay)
						tg.Sendf("%s", notifyAccum.RenderHourlyReport(localMaintNow, snap))
						lastPulseSentAt = now
						lastHourlyKey = hk
					}
				}
				if preUSEnable && (localMaintNow.Hour() > preUSReportHour || (localMaintNow.Hour() == preUSReportHour && localMaintNow.Minute() >= preUSReportMinute)) {
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
			if tg != nil && tg.Enabled() && digestEnable && overnightReportEnable && !inCriticalProtection {
				if localMaintNow.Hour() > overnightReportHour || (localMaintNow.Hour() == overnightReportHour && localMaintNow.Minute() >= overnightReportMinute) {
					dayKey := localMaintNow.Format("2006-01-02")
					if dayKey != lastOvernightReportDay {
						snap := buildNotifySnapshot(modeLabel, localMaintNow, paper, execMgr, metaBySymbol, longInPlay, shortInPlay)
						tg.Sendf("%s", notifyAccum.RenderOvernightReport(localMaintNow, snap))
						lastOvernightReportDay = dayKey
					}
				}
			}
			if tg != nil && tg.Enabled() && digestEnable && dailyReportEnable && !inCriticalProtection {
				if localMaintNow.Hour() > dailyReportHour || (localMaintNow.Hour() == dailyReportHour && localMaintNow.Minute() >= dailyReportMinute) {
					dayKey := localMaintNow.Format("2006-01-02")
					if dayKey != last1900ReportDay {
						snap := buildNotifySnapshot(modeLabel, localMaintNow, paper, execMgr, metaBySymbol, longInPlay, shortInPlay)
						tg.Sendf("%s", notifyAccum.RenderDailyReport(localMaintNow, snap))
						last1900ReportDay = dayKey
					}
				}
			}
			if !hourlyEnable && !inCriticalProtection && time.Now().UTC().After(nextDigestAt) {
				sendInPlayDigest(tg, longInPlay, shortInPlay, metaBySymbol, dryRun, digestLimit)
				nextDigestAt = time.Now().UTC().Add(digestEvery)
			}
		}

		var acct accountSnapshot
		if rest != nil && showAccount {
			snap, err := fetchAccountSnapshot(rest, func() *aster.UserDataState {
				if execMgr != nil {
					return execMgr.userDataState
				}
				return nil
			}(), accountAssets)
			if err != nil {
				fmt.Println("live: account snapshot error:", err)
			} else {
				acct = snap
				realizedToday := 0.0
				if execMgr != nil {
					realizedToday = execMgr.dayRealizedAt(now)
				}
				printAccountSnapshot(snap, realizedToday)
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
						fmt.Println("live:", msg)
						tg.Sendf("%s", notify.BuildEventHTML("🛑", "KILL SWITCH",
							fmt.Sprintf("<b>Equity:</b> %.4f", eq),
							fmt.Sprintf("<b>Start:</b> %.4f | <b>Limit:</b> %.4f", dayStartEq[dayKey], minEq),
						))
					}
				}
			}
		}
		if execMgr != nil {
			execMgr.Reconcile(now, momBySymbol, flowMetricsBySymbol, metaBySymbol)
			if !inMaint {
				execMgr.MaintainPerpBalance(now)
			}
			for sym, pos := range execMgr.positions {
				if pos == nil || pos.State != execClosed || pos.ClosedAt.IsZero() {
					continue
				}
				if seenAt, ok := handledClosedLive[sym]; ok && !pos.ClosedAt.After(seenAt) {
					continue
				}
				handledClosedLive[sym] = pos.ClosedAt
				reason := strings.ToUpper(strings.TrimSpace(pos.CloseReason))
				if reason == "STOP_HIT" || reason == "SL" || reason == "TRAIL_STOP" || strings.Contains(reason, "STOP") {
					dayUTC := metaBySymbol[sym].DayUTC24h
					_, pnlPct := realizedFromFill(pos.Side, pos.EntryPrice, pos.LastMark, maxFloat(pos.FilledQty, pos.Qty))
					markSessionStop(sessionChurns, pos.ClosedAt, sym, pos.Side, firstNonEmpty(pos.EntryReason, pos.EntrySetupFamily), pos.ClosedAt.Sub(pos.CreatedAt).Minutes(), pnlPct, dayUTC)
				}
			}
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
				if eodLegacyEnable && paper.enabled && tg != nil && tg.Enabled() {
					switch maintWindow.Name {
					case "EOD":
						if lastM2ReportDay != dayKey {
							tg.Sendf("%s", buildWindowReport("EOD Report", localMaintNow, paper, metaBySymbol, longInPlay, shortInPlay, digestLimit))
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
				forceFlatWinners := envBool("LIVE_EOD_FORCE_FLAT_WINNERS", false)
				liveClosed := 0
				liveKeptWinners := 0
				paperClosed := 0
				paperKeptWinners := 0
				if execMgr != nil {
					if forceFlatWinners {
						_ = execMgr.ForceCloseAll("EOD_FORCE_FLAT")
					} else {
						liveClosed, liveKeptWinners, _ = execMgr.ForceCloseNonWinners("EOD_FORCE_FLAT_NON_WINNERS")
					}
				}
				if paper.enabled {
					if forceFlatWinners {
						paper.ForceCloseAll(now, metaBySymbol, paperDepth, "EOD_FORCE_FLAT")
						_ = paper.save()
					} else {
						paperClosed, paperKeptWinners = paper.ForceCloseNonWinners(now, metaBySymbol, paperDepth, "EOD_FORCE_FLAT_NON_WINNERS")
					}
				}
				if forceFlatWinners {
					tg.Sendf("%s", notify.BuildEventHTML("✅", "MAINTENANCE FLAT COMPLETE",
						fmt.Sprintf("<b>Window:</b> %s", maintWindow.Name),
					))
				} else {
					tg.Sendf("%s", notify.BuildEventHTML("✅", "MAINTENANCE RISK REDUCTION COMPLETE",
						fmt.Sprintf("<b>Window:</b> %s", maintWindow.Name),
						fmt.Sprintf("<b>Closed non-winners:</b> live=%d paper=%d", liveClosed, paperClosed),
						fmt.Sprintf("<b>Kept winners open:</b> live=%d paper=%d", liveKeptWinners, paperKeptWinners),
					))
				}
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
			for _, w := range maintenanceWindows {
				if !w.Enabled {
					continue
				}
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
		if payoutMgr != nil && payoutMgr.enabled && !inMaint {
			payoutMgr.maybeRun(now, localMaintNow, maintEOD, &maintState, paper, metaBySymbol, acct, execMgr, tg)
		}
		if paper.enabled && !inMaint {
			if emitTerminal {
				fmt.Println(paper.ConsoleSummary(metaBySymbol))
				for _, line := range paper.ConsolePositions(metaBySymbol) {
					fmt.Println(line)
				}
			}
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
		_ = effectiveMargin
		paperIgnoresMaintenance := paper != nil && paper.enabled && envBool("LIVE_PAPER_IGNORE_MAINTENANCE", true)
		if inMaint && !paperIgnoresMaintenance {
			reason := blockedWindowReason(maintWindow)
			fmt.Printf("live: %s window=%s session=%s\n", reason, maintWindow.Name, sessionTag(localMaintNow))
			st := liveStatus{
				Generated:       now,
				Mode:            surfacedRuntimeMode(operatingMode),
				ModeState:       "blocked",
				DryRun:          dryRun,
				LiveEnabled:     safety.enableLiveTrading,
				AvailableUSDT:   acct.AvailableUSDT,
				TopDecision:     "blocked",
				TopDecisionWhy:  reason,
				TopRejectReason: reason,
			}
			if paper != nil && paper.enabled {
				st.PaperSummary = paper.Summary(metaBySymbol)
				st.Paper = buildLivePaperSnapshot(operatingMode, paper, metaBySymbol, eventLog, 12)
			}
			statusStore.Set(st)
			waitAndReport()
			continue
		}
		if healthSnap, ok := runtimeLoop.latestHealth(); ok {
			healthBlockReason := ""
			healthBlocked := false
			if paper != nil && paper.enabled {
				healthBlockReason, healthBlocked = entriesBlockedByPaperAccountHealth(healthSnap.Summary)
			} else {
				healthBlockReason, healthBlocked = entriesBlockedByAccountHealth(healthSnap.Summary)
			}
			if healthBlocked {
				st := liveStatus{
					Generated:       now,
					Mode:            surfacedRuntimeMode(operatingMode),
					ModeState:       "blocked",
					DryRun:          dryRun,
					LiveEnabled:     safety.enableLiveTrading,
					AvailableUSDT:   acct.AvailableUSDT,
					TopDecision:     "blocked",
					TopDecisionWhy:  healthBlockReason,
					TopRejectReason: healthBlockReason,
				}
				if paper != nil && paper.enabled {
					st.PaperSummary = paper.Summary(metaBySymbol)
					st.Paper = buildLivePaperSnapshot(operatingMode, paper, metaBySymbol, eventLog, 12)
				}
				statusStore.Set(st)
				waitAndReport()
				continue
			}
		}

		cands := chooseCandidates(longInPlay, shortInPlay, minGrade, enableMomentumReversal, reversalMinGrade, reversalSlopeMin, bNearAOnly, bNearAScoreMin, reversalTopLongN, candCfg)
		cands = rankWithStrategy(featureCache, cands, strategyTopN, stopMode, targetMode, vpMinTargetPct, inertiaEnable, inertiaScoreMin, inertiaSlowMin, inertiaFastMax, inertiaSlowN, inertiaFastN, reversalVolSpike, rankSortCfg, reliabilityStore, flowMetricsBySymbol)
		cands = applyCandidateLifecycle(cands, now, candidateMem, lifecycleCfg)
		if watcher != nil {
			wallSignals = watcher.WallSignals()
		}
		liveObserveOnlyMode := operatingMode == runtimeModeLive
		if liveObserveOnlyMode {
			st := liveStatus{
				Generated:     now,
				Mode:          string(runtimeModeLive),
				ModeState:     "scanner_only",
				DryRun:        dryRun,
				LiveEnabled:   safety.enableLiveTrading,
				LongInPlay:    len(longInPlay),
				ShortInPlay:   len(shortInPlay),
				AvailableUSDT: acct.AvailableUSDT,
				Exec:          liveExecSnapshot{},
				Live:          liveAccountSnapshot{},
			}
			st.ScannerLongs, st.ScannerShorts, st.ScannerBias = topScanSnapshot(longInPlay, shortInPlay, metaBySymbol, 5)
			if execMgr != nil {
				st.Exec = execMgr.Snapshot(10)
				st.Live = execMgr.LiveAccountSnapshot(10)
			}
			if paper.enabled {
				st.PaperSummary = paper.Summary(metaBySymbol)
				st.Paper = buildLivePaperSnapshot(runtimeModeLive, paper, metaBySymbol, eventLog, 12)
			}
			if best, ok := selectTopRuntimeCandidate(cands); ok {
				st.TopSymbol = strings.ToUpper(aster.RawSymbol(best.Entry.Symbol))
				st.TopSide = best.Side
				st.TopGrade = best.Entry.CurrentGrade
				st.TopScore = best.Entry.CurrentScore
				st.TopSlope = best.Entry.ScoreSlope
				st.TopDecision = "live"
				st.TopDecisionWhy = "scanner_only_manual_execution"
				st.TopRejectReason = ""
			} else {
				st.TopDecision = "live"
				st.TopDecisionWhy = "scanner_only_no_candidates"
				st.TopRejectReason = ""
			}
			statusStore.Set(st)
			waitAndReport()
			continue
		}

		st := liveStatus{
			Generated:     now,
			Mode:          surfacedRuntimeMode(runtimeModePaper),
			ModeState:     "paper_enabled",
			DryRun:        dryRun,
			LiveEnabled:   false,
			LongInPlay:    len(longInPlay),
			ShortInPlay:   len(shortInPlay),
			AvailableUSDT: acct.AvailableUSDT,
			Exec:          liveExecSnapshot{},
			Live:          liveAccountSnapshot{},
		}
		st.ScannerLongs, st.ScannerShorts, st.ScannerBias = topScanSnapshot(longInPlay, shortInPlay, metaBySymbol, 5)
		if execMgr != nil {
			st.Exec = execMgr.Snapshot(10)
			st.Live = execMgr.LiveAccountSnapshot(10)
		}
		if paper != nil && paper.enabled {
			st.PaperSummary = paper.Summary(metaBySymbol)
			st.Paper = buildLivePaperSnapshot(runtimeModePaper, paper, metaBySymbol, eventLog, 12)
		}
		best, ok := selectTopRuntimeCandidate(cands)
		if !ok {
			st.TopDecision = "paper_enabled"
			st.TopDecisionWhy = "no_candidates"
			statusStore.Set(st)
			waitAndReport()
			continue
		}
		st.TopSymbol = strings.ToUpper(aster.RawSymbol(best.Entry.Symbol))
		st.TopSide = best.Side
		st.TopGrade = best.Entry.CurrentGrade
		st.TopScore = best.Entry.CurrentScore
		st.TopSlope = best.Entry.ScoreSlope
		st.TopTriggerState = best.TriggerState
		st.TopExitProfile = best.ExitProfile
		st.TopDiscovery = best.DiscoveryScore
		st.TopTrigger = best.TriggerScore
		st.TopExecution = best.ExecutionScore
		st.TopCombined = best.CombinedScore
		st.TopRegimeTag = best.Sig.RegimeTag
		currentSideEntries := longCurrent
		if strings.EqualFold(best.Side, "SELL") {
			currentSideEntries = shortCurrent
		}
		decision := buildPaperExecutionDecision(paperDecisionCtx{
			Now:                 now,
			LocalMaintNow:       localMaintNow,
			Candidate:           best,
			MetaBySymbol:        metaBySymbol,
			EntryDepth:          paperDepth,
			Paper:               paper,
			CurrentEntries:      currentSideEntries,
			SessionChurns:       sessionChurns,
			RiskShell:           riskShell,
			RiskFallbackStopPct: riskFallbackStopPct,
			RiskHoldHours:       riskHoldHours,
			LeverageMode:        leverageMode,
			LeverageFixed:       leverageFixed,
			LeverageMin:         leverageMin,
			MaxLeverage:         leverageMax,
			EffectiveReserve:    effectiveReserve,
			EffectiveMargin:     effectiveMargin,
			AvailableUSDT:       acct.AvailableUSDT,
			OBFilterEnable:      obFilterEnable,
			OBLevels:            obLevels,
			OBImbMin:            obImbMin,
			OBMaxSpreadBps:      obMaxSpreadBps,
			MaxOpenPos:          maxOpenPos,
			MaxOpenPerSide:      maxOpenPerSide,
			EventLog:            eventLog,
		})
		emitPaperDecisionEvent(eventLog, now, best, decision)
		recordCandidateDecision(cmdCtx, best, decision.RejectReason)
		dispatch := dispatchPaperDecision(operatingMode, now, decision, best, entryBps, effectiveMargin, computeLeverage(best, leverageMode, leverageFixed, leverageMin, leverageMax), metaBySymbol, paperDepth, currentSideEntries, paperDispatchHooks{
			Paper: func(now time.Time, c candidate, entryBps, margin float64, leverage int, meta map[string]symbolMeta, depth map[string]aster.OrderBook, current map[string]inplay.Entry) (*paperPosition, error) {
				if paper == nil {
					return nil, fmt.Errorf("paper_disabled")
				}
				return paper.MaybeEnter(now, c, entryBps, margin, leverage, meta, depth, current)
			},
		})
		if dispatch.Position != nil {
			annotatePaperPositionFromDecision(dispatch.Position, best, decision)
			_ = paper.save()
			emitPaperPositionOpenEvent(eventLog, now, best, dispatch.Position, decision)
		}
		applyPaperDecisionStatus(&st, decision, dispatch)
		paperLogDecision(best, decision, dispatch)
		if paper != nil && paper.enabled {
			st.PaperSummary = paper.Summary(metaBySymbol)
			st.Paper = buildLivePaperSnapshot(runtimeModePaper, paper, metaBySymbol, eventLog, 12)
		}
		statusStore.Set(st)
		waitAndReport()
		continue

		waitAndReport()
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
	longTop, shortTop, bias := topScanSnapshot(longInPlay, shortInPlay, meta, minInt(3, maxInt(1, limit)))
	tg.Sendf("%s", notify.BuildEventHTML("📡", "LIVE DIGEST",
		fmt.Sprintf("<b>Mode:</b> %s | <b>UTC:</b> %s", mode, now.Format("15:04")),
		fmt.Sprintf("<b>Session:</b> %s", sessionTag(now)),
		notify.BuildScannerSnapshotHTML(longTop, shortTop, bias),
	))
}

func buildHourlyDigest(now time.Time, p *paperTrader, m *liveExecManager, missed *missedTracker, meta map[string]symbolMeta, longInPlay, shortInPlay []inplay.Entry, topN int) string {
	if p != nil && p.enabled {
		return buildClassicDigest("Hourly Digest", now, p, meta, longInPlay, shortInPlay)
	}
	if m != nil {
		return buildLiveDigest("Hourly Digest", now, m, missed, meta, longInPlay, shortInPlay)
	}
	return tgPre(fmt.Sprintf("Hourly Digest (%s) session=%s\nno active paper/live manager", now.Format("15:04 MST"), sessionTag(now)))
}

func parseHHMM(raw string, defHour, defMin int) (int, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defHour, defMin
	}
	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return defHour, defMin
	}
	h, errH := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, errM := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return defHour, defMin
	}
	return h, m
}

func buildNotifySnapshot(modeLabel string, now time.Time, p *paperTrader, m *liveExecManager, meta map[string]symbolMeta, longInPlay, shortInPlay []inplay.Entry) notify.Snapshot {
	snap := notify.Snapshot{
		Mode:              firstNonEmpty(strings.TrimSpace(modeLabel), "LIVE"),
		Status:            fmt.Sprintf("session=%s", sessionTag(now)),
		RealizedToday:     0,
		UnrealizedNow:     0,
		FeesFundingToday:  0,
		OpenPositionLines: []string{},
		WatchlistLines:    []string{},
	}
	if p != nil && p.enabled {
		dayKey := now.In(p.reportLoc).Format("2006-01-02")
		if ds := p.dayStats[dayKey]; ds != nil {
			snap.RealizedToday = ds.Net
			snap.FeesFundingToday = ds.Fees
		}
		for raw, pos := range p.positions {
			if pos == nil {
				continue
			}
			mark := meta[raw].LastPrice
			upnl := 0.0
			if mark > 0 {
				if strings.EqualFold(pos.Side, "BUY") {
					upnl = (mark - pos.Entry) * pos.Qty
				} else {
					upnl = (pos.Entry - mark) * pos.Qty
				}
			}
			snap.UnrealizedNow += upnl
			snap.OpenPositionLines = append(snap.OpenPositionLines, fmt.Sprintf("%s %s | reason=%s | uPnL=%+.2f", raw, pos.Side, firstNonEmpty(pos.EntryReason, "n/a"), upnl))
		}
	}
	if m != nil {
		live := m.LiveAccountSnapshot(8)
		snap.RealizedToday = live.RealizedDay
		snap.UnrealizedNow = live.OpenPnL
		snap.FeesFundingToday = 0
		if len(live.Positions) > 0 {
			snap.OpenPositionLines = snap.OpenPositionLines[:0]
			for _, pos := range live.Positions {
				snap.OpenPositionLines = append(snap.OpenPositionLines,
					fmt.Sprintf("%s %s | reason=%s | uPnL=%+.2f", pos.Symbol, pos.Side, firstNonEmpty(pos.EntryReason, "unknown"), pos.UnrealizedPnL))
			}
		}
	}
	longTop, shortTop, _ := topScanSnapshot(longInPlay, shortInPlay, meta, 3)
	for _, row := range longTop {
		snap.WatchlistLines = append(snap.WatchlistLines, fmt.Sprintf("%s in-play long", row.Symbol))
	}
	for _, row := range shortTop {
		snap.WatchlistLines = append(snap.WatchlistLines, fmt.Sprintf("%s in-play short", row.Symbol))
	}
	if len(snap.WatchlistLines) > 6 {
		snap.WatchlistLines = snap.WatchlistLines[:6]
	}
	return snap
}

func buildClassicDigest(label string, now time.Time, p *paperTrader, meta map[string]symbolMeta, longInPlay, shortInPlay []inplay.Entry) string {
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
		mark := meta[raw].LastPrice
		if mark <= 0 {
			mark = pos.LastMark
		}
		if mark <= 0 {
			mark = pos.Entry
		}
		upnl, _ := realizedFromFill(pos.Side, pos.Entry, mark, pos.Qty)
		openPnL += upnl
	}
	eq := p.balance + openPnL
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) session=%s\n", strings.TrimSpace(label), now.Format("15:04 MST"), sessionTag(now))
	fmt.Fprintf(&b, "realized=%+.2f openPnL=%+.2f netDay=%+.2f bal=%.2f eq=%.2f\n\n",
		realized, openPnL, realized+openPnL, p.balance, eq)
	fmt.Fprintf(&b, "Paper Update (%s) session=%s\n", now.Format("15:04 MST"), sessionTag(now))
	b.WriteString(p.TradeUpdateMessage(meta, 10_000))
	b.WriteString("\n\nIn-Play Scanner\n")
	appendUnifiedInPlayRows(&b, longInPlay, shortInPlay, meta, 10_000)
	return tgPre(strings.TrimSpace(b.String()))
}

func buildSODReport(now time.Time, p *paperTrader, meta map[string]symbolMeta) string {
	if p == nil || !p.enabled {
		return tgPre(fmt.Sprintf("SOD Report (%s) session=%s\npaper disabled", now.Format("15:04 MST"), sessionTag(now)))
	}
	return buildClassicDigest("SOD Report", now, p, meta, nil, nil)
}

func buildLiveDigest(label string, now time.Time, m *liveExecManager, missed *missedTracker, meta map[string]symbolMeta, longInPlay, shortInPlay []inplay.Entry) string {
	if m == nil {
		return tgPre(fmt.Sprintf("%s (%s) session=%s\nlive disabled", strings.TrimSpace(label), now.Format("15:04 MST"), sessionTag(now)))
	}
	pulse, cards := buildLivePulseAndCards(strings.TrimSpace(label), now, m)
	lines := []string{
		fmt.Sprintf("<b>Session:</b> %s | <b>Time:</b> %s", strings.ToUpper(strings.TrimSpace(pulse.Session)), strings.TrimSpace(pulse.TimeLabel)),
		fmt.Sprintf("<b>Balance:</b> $%.2f | <b>Equity:</b> $%.2f | <b>Net Day:</b> %+.2f", pulse.Balance, pulse.Equity, pulse.NetDay),
	}
	if len(cards) > 0 {
		for i, card := range cards {
			if i >= 2 {
				break
			}
			lines = append(lines, notify.BuildPositionCard(card))
		}
	}
	if acct := strings.TrimSpace(m.AccountDigestSection()); acct != "" {
		lines = append(lines, acct)
	}
	degraded := []string{}
	if snap := m.LiveAccountSnapshot(32); len(snap.Positions) > 0 {
		for _, pos := range snap.Positions {
			if strings.EqualFold(strings.TrimSpace(pos.ProtectionState), "DEGRADED") {
				degraded = append(degraded, fmt.Sprintf("%s %s protection degraded (managed=%s protected=%s)",
					pos.Symbol, pos.Side, boolLabel(pos.Managed), boolLabel(pos.Protected)))
			}
		}
	}
	if len(degraded) > 0 {
		lines = append(lines, "<b>Protection Degraded:</b> "+strings.Join(degraded, " | "))
	}
	if missed != nil {
		if rows := missed.ReviewLines(now.UTC(), 4); len(rows) > 0 {
			lines = append(lines, "<b>Blocked/Missed:</b> "+strings.Join(rows, " | "))
		}
	}
	if rows := manualLivePositionLines(m, 3); len(rows) > 0 {
		lines = append(lines, "<b>Manual Live Positions:</b> "+strings.Join(rows, " | "))
	}
	longTop, shortTop, bias := topScanSnapshot(longInPlay, shortInPlay, meta, 3)
	lines = append(lines, notify.BuildScannerSnapshotHTML(longTop, shortTop, bias))
	return notify.BuildEventHTML("📡", strings.ToUpper(strings.TrimSpace(label)), lines...)
}

func scannerItemsForCommand(s liveStatus, which string) ([]notify.ScanItem, []notify.ScanItem, string) {
	switch strings.ToLower(strings.TrimSpace(which)) {
	case "long", "longs":
		return s.ScannerLongs, nil, "long"
	case "short", "shorts":
		return nil, s.ScannerShorts, "short"
	default:
		return s.ScannerLongs, s.ScannerShorts, s.ScannerBias
	}
}

func manualLivePositionLines(m *liveExecManager, limit int) []string {
	if m == nil {
		return nil
	}
	snap := m.LiveAccountSnapshot(16)
	if snap.ManualCount <= 0 || len(snap.Positions) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 3
	}
	lines := make([]string, 0, limit)
	for _, p := range snap.Positions {
		if strings.EqualFold(strings.TrimSpace(p.Source), "BOT") {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s qty=%.6f entry=%s upnl=%+.2f",
			cleanSymbol(p.Symbol), displayPositionSide(p.Side), p.Qty, fmtPrice(p.EntryPrice), p.UnrealizedPnL))
		if len(lines) >= limit {
			break
		}
	}
	return lines
}

func scannerViewFromStatus(s liveStatus, manualLines []string) notify.ScannerView {
	view := notify.ScannerView{
		Session:    sessionTag(s.Generated),
		Timestamp:  s.Generated.In(time.Local).Format("15:04:05 MST"),
		MarketBias: s.ScannerBias,
	}
	for i, row := range s.ScannerLongs {
		view.LongRows = append(view.LongRows, notify.ScannerRow{
			Rank:   i + 1,
			Symbol: cleanSymbol(row.Symbol),
			Grade:  row.Grade,
			Score:  row.Score,
			State:  row.State,
			Price:  row.Price,
			DayPct: row.DayUTC,
			H4Pct:  row.UTC4h,
			H1Pct:  row.UTC1h,
		})
	}
	for i, row := range s.ScannerShorts {
		view.ShortRows = append(view.ShortRows, notify.ScannerRow{
			Rank:   i + 1,
			Symbol: cleanSymbol(row.Symbol),
			Grade:  row.Grade,
			Score:  row.Score,
			State:  row.State,
			Price:  row.Price,
			DayPct: row.DayUTC,
			H4Pct:  row.UTC4h,
			H1Pct:  row.UTC1h,
		})
	}
	if len(manualLines) > 0 {
		view.LiveManualSection = "Live " + strings.Join(manualLines, " | ")
	}
	return view
}

func positionUpdatesHTML(mode string, account notify.AccountView, positions []notify.PositionView, topN int) string {
	if topN <= 0 {
		topN = 3
	}
	parts := []string{notify.FormatAccountSummary(account)}
	for i, pos := range positions {
		if i >= topN {
			break
		}
		parts = append(parts, notify.FormatPositionUpdate(pos))
	}
	if len(parts) == 1 {
		return notify.FormatAccountSummary(account)
	}
	return strings.Join(parts, "\n\n")
}

func safeRMultiple(entry, stop, exit float64, side string) float64 {
	risk := math.Abs(entry - stop)
	if risk <= 0 {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(side), "SELL") || strings.EqualFold(strings.TrimSpace(side), "SHORT") {
		return (entry - exit) / risk
	}
	return (exit - entry) / risk
}

func hasLiveProtectiveOrder(p *livePosition) bool {
	return p != nil && p.StopOrderID > 0 && !p.ProtectionPending
}

func buildLivePulseAndCards(title string, now time.Time, m *liveExecManager) (notify.PulseSnapshot, []notify.PositionCard) {
	snap := m.LiveAccountSnapshot(32)
	cards := make([]notify.PositionCard, 0, len(snap.Positions))
	for _, p := range snap.Positions {
		nextAction := "monitor"
		if strings.EqualFold(strings.TrimSpace(p.ProtectionState), "PENDING_PROTECTION") {
			nextAction = "await stop attach"
		} else if strings.EqualFold(strings.TrimSpace(p.ProtectionState), "DEGRADED") {
			nextAction = "operator review /protect"
		} else if strings.EqualFold(strings.TrimSpace(p.ProtectionState), "CONFLICT") {
			nextAction = "resolve live/manual mismatch"
		}
		cards = append(cards, notify.PositionCard{
			Symbol:           p.Symbol,
			Side:             p.Side,
			Source:           displayEntrySource(p.Source),
			Status:           p.ProtectionState,
			ManageState:      displayManageState(p.ManageState),
			Managed:          p.Managed,
			Protected:        p.Protected,
			NextAction:       nextAction,
			Qty:              p.Qty,
			EntryPrice:       p.EntryPrice,
			MarkPrice:        p.MarkPrice,
			LastPrice:        p.LastPrice,
			SpreadBps:        p.SpreadBps,
			UnrealizedPnL:    p.UnrealizedPnL,
			UnrealizedPnLPct: p.UnrealizedPnLPct,
			Leverage:         p.Leverage,
			Setup:            displayEntryReason(p.EntryReason),
			Confluence:       0,
			AgeMin:           int(p.HoldMin),
			StopLoss:         p.StopPrice,
		})
	}
	return notify.PulseSnapshot{
		Title:     title,
		TimeLabel: now.Format("15:04 MST"),
		Session:   sessionTag(now),
		Balance:   snap.AvailableUSDT,
		Equity:    snap.Equity,
		Realized:  snap.RealizedDay,
		OpenPnL:   snap.OpenPnL,
		NetDay:    snap.RealizedDay + snap.OpenPnL,
		OpenCount: snap.OpenCount,
		OpenCap:   0,
	}, cards
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
		OpenCap:   p.maxOpen,
		NetDayPct: netPct,
	}, cards
}

func topScanSnapshot(longInPlay, shortInPlay []inplay.Entry, meta map[string]symbolMeta, topN int) ([]notify.ScanItem, []notify.ScanItem, string) {
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
			raw := strings.ToUpper(aster.RawSymbol(rows[i].Symbol))
			m := meta[raw]
			out = append(out, notify.ScanItem{
				Symbol:    raw,
				Side:      normalizePositionSide(rows[i].SideBias),
				Grade:     rows[i].CurrentGrade,
				Score:     rows[i].CurrentScore,
				Slope:     rows[i].ScoreSlope,
				State:     displayState(rows[i].SideBias, rows[i].State),
				Price:     m.LastPrice,
				DayUTC:    m.DayUTC24h,
				UTC4h:     m.UTC4hPct,
				UTC1h:     m.UTC1hPct,
				VolumeUSD: m.VolumeUSD,
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

func cleanSymbol(sym string) string {
	s := strings.ToUpper(strings.TrimSpace(sym))
	s = strings.TrimSuffix(s, "USDT")
	s = strings.TrimSuffix(s, "-USD")
	if s == "" {
		return strings.ToUpper(strings.TrimSpace(sym))
	}
	return s
}

func summarizeOneLine(s string, max int) string {
	x := strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	x = strings.Join(strings.Fields(x), " ")
	if x == "" {
		return "n/a"
	}
	if max > 0 && len(x) > max {
		return x[:max-1] + "…"
	}
	return x
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
		status := colorStateTag(e.SideBias, e.State)
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
			i+1, raw, status, colorGradeTag(e.CurrentGrade), e.CurrentScore, slopeArrow, abs(e.ScoreSlope), price, m.Move24h)
	}
}

type unifiedInPlayRow struct {
	side  string
	entry inplay.Entry
	meta  symbolMeta
}

func validDisplayGrade(g string) bool {
	switch strings.ToUpper(strings.TrimSpace(g)) {
	case "A+", "A", "B", "C", "D":
		return true
	default:
		return false
	}
}

func buildUnifiedInPlayRows(longInPlay, shortInPlay []inplay.Entry, meta map[string]symbolMeta, limit int) []unifiedInPlayRow {
	rows := make([]unifiedInPlayRow, 0, len(longInPlay)+len(shortInPlay))
	add := func(side string, entries []inplay.Entry) {
		for _, e := range entries {
			if !validDisplayGrade(e.CurrentGrade) {
				continue
			}
			raw := strings.ToUpper(aster.RawSymbol(e.Symbol))
			rows = append(rows, unifiedInPlayRow{
				side:  side,
				entry: e,
				meta:  meta[raw],
			})
		}
	}
	add("LONG", longInPlay)
	add("SHORT", shortInPlay)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].entry.CurrentScore != rows[j].entry.CurrentScore {
			return rows[i].entry.CurrentScore > rows[j].entry.CurrentScore
		}
		if rows[i].meta.VolumeUSD != rows[j].meta.VolumeUSD {
			return rows[i].meta.VolumeUSD > rows[j].meta.VolumeUSD
		}
		if rows[i].entry.ScoreSlope != rows[j].entry.ScoreSlope {
			return abs(rows[i].entry.ScoreSlope) > abs(rows[j].entry.ScoreSlope)
		}
		return strings.ToUpper(aster.RawSymbol(rows[i].entry.Symbol)) < strings.ToUpper(aster.RawSymbol(rows[j].entry.Symbol))
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func appendUnifiedInPlayRows(b *strings.Builder, longInPlay, shortInPlay []inplay.Entry, meta map[string]symbolMeta, limit int) {
	rows := buildUnifiedInPlayRows(longInPlay, shortInPlay, meta, limit)
	fmt.Fprintf(b, "RANKED (%d)\n", len(rows))
	if len(rows) == 0 {
		b.WriteString("- none\n")
		return
	}
	for i, row := range rows {
		raw := strings.ToUpper(aster.RawSymbol(row.entry.Symbol))
		status := colorStateTag(row.entry.SideBias, row.entry.State)
		slopeArrow := "→"
		if row.entry.ScoreSlope > 0 {
			slopeArrow = "↑"
		} else if row.entry.ScoreSlope < 0 {
			slopeArrow = "↓"
		}
		price := "n/a"
		if row.meta.LastPrice > 0 {
			price = fmtPrice(row.meta.LastPrice)
		}
		fmt.Fprintf(b, "%d) [%s] %s %s g=%s s=%.2f %s%.3f px=%s dayUTC=%+.2f%% utc4h=%+.2f%% utc1h=%+.2f%% vol=%s\n",
			i+1,
			row.side,
			raw,
			status,
			colorGradeTag(row.entry.CurrentGrade),
			row.entry.CurrentScore,
			slopeArrow,
			abs(row.entry.ScoreSlope),
			price,
			row.meta.DayUTC24h,
			row.meta.UTC4hPct,
			row.meta.UTC1hPct,
			marketHumanUSD(row.meta.VolumeUSD),
		)
	}
}

func colorGradeTag(g string) string {
	switch strings.ToUpper(strings.TrimSpace(g)) {
	case "A+":
		return "A+"
	case "A":
		return "A"
	case "B":
		return "B"
	case "C":
		return "C"
	case "D":
		return "D"
	default:
		return "N/A"
	}
}

func displayState(side string, s inplay.State) string {
	// Side-aware naming: short scanner uses DUMPING as strong impulse.
	if strings.EqualFold(strings.TrimSpace(side), "short") {
		switch s {
		case inplay.StatePumping:
			return string(inplay.StateDumping)
		case inplay.StateDumping:
			return string(inplay.StatePumping)
		}
	}
	return string(s)
}

func colorStateTag(side string, s inplay.State) string {
	ds := inplay.State(displayState(side, s))
	switch ds {
	case inplay.StatePumping:
		return "PUMPING"
	case inplay.StateInPlay:
		return "IN_PLAY"
	case inplay.StateHeating:
		return "HEATING"
	case inplay.StateBalanced:
		return "BALANCED"
	case inplay.StateCooling:
		return "COOLING"
	case inplay.StateDumping:
		return "DUMPING"
	case inplay.StateExhausted:
		return "EXHAUSTED"
	default:
		return strings.ToUpper(strings.TrimSpace(string(ds)))
	}
}

func colorReasonTag(reason string) string {
	r := strings.ToUpper(strings.TrimSpace(reason))
	switch r {
	case "FA", "FAILED_AUCTION_MAGNET":
		return r
	case "VP_TREND", "VWAP_CONFLUENCE", "PD_LEVELS_RETEST":
		return r
	case "BOS_PB", "LSR", "OB_R", "FVG_C":
		return r
	case "MOMENTUM_FADE":
		return r
	case "SL", "STOP", "TRAIL_STOP", "EOD_FORCE_FLAT", "TG_FORCE_FLAT":
		return r
	case "", "-", "NONE":
		return "none"
	default:
		return r
	}
}

func activeMaintenanceWindow(now time.Time, enabled bool, windows ...maintenanceWindow) (maintenanceWindow, bool) {
	if !enabled {
		return maintenanceWindow{}, false
	}
	for _, w := range windows {
		if !w.Enabled {
			continue
		}
		if inMinuteWindow(now.Hour(), now.Minute(), w.StartHour, w.StartMin, w.EndHour, w.EndMin) {
			return w, true
		}
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

func paperProtectionState(pos *paperPosition) string {
	if pos == nil {
		return "none"
	}
	switch {
	case pos.ProtectionStage >= protectionStageLocked:
		return "locked"
	case pos.ProtectionStage >= protectionStageArmed:
		return "armed"
	case !stopStillOriginal(pos.Stop, pos.OriginalStop):
		return "tightened"
	default:
		return "original"
	}
}

func paperFastShortProtectionEnabled(pos *paperPosition) bool {
	if pos == nil || !strings.EqualFold(pos.Side, "SELL") {
		return false
	}
	switch strings.TrimSpace(pos.ShortBucket) {
	case "post_pump_fresh_breakdown", "failed_bounce_short":
		return true
	default:
		return false
	}
}

func liveProtectionState(pos *livePosition) string {
	if pos == nil {
		return "none"
	}
	switch {
	case pos.ProtectionStage >= protectionStageLocked:
		return "locked"
	case pos.ProtectionStage >= protectionStageArmed:
		return "armed"
	case pos.Protected || hasLiveProtectiveOrder(pos):
		return "protected"
	default:
		return "original"
	}
}

func logExitAudit(symbol, strategyID, rawReason, finalReason, triggerRef string, decisionPx, stopPx, maxRSeen, minRSeen float64, protectionState, entryTiming string, noProofTriggered, protectedAfterProof, winnerRevertedUnprotected bool) {
	fmt.Printf("exit_audit symbol=%s strategy_id=%s exit_reason_raw=%q exit_reason_final=%q trigger_ref=%q display_mark_at_decision=%.8f stop_at_decision=%.8f max_r_seen=%.4f min_r_seen=%.4f protection_state=%s entry_timing=%s no_proof_triggered=%t protected_after_proof=%t winner_reverted_unprotected=%t\n",
		symbol,
		firstNonEmpty(strings.TrimSpace(strategyID), "unknown"),
		rawReason,
		finalReason,
		strings.ToLower(strings.TrimSpace(triggerRef)),
		decisionPx,
		stopPx,
		maxRSeen,
		minRSeen,
		firstNonEmpty(strings.TrimSpace(protectionState), "unknown"),
		firstNonEmpty(strings.TrimSpace(entryTiming), "unknown"),
		noProofTriggered,
		protectedAfterProof,
		winnerRevertedUnprotected,
	)
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

func beLockPriceBuffered(side string, entry, currentStop, beLockBps float64) float64 {
	be := beLockPrice(side, entry, beLockBps)
	bufferR := envFloat("LIVE_BE_BUFFER_R", 0.0)
	if bufferR <= 0 || entry <= 0 || currentStop <= 0 {
		return be
	}
	risk := math.Abs(entry - currentStop)
	if risk <= 0 {
		return be
	}
	buffer := risk * bufferR
	if strings.EqualFold(side, "SELL") {
		return be + buffer
	}
	return be - buffer
}

func allowMoveToBreakEven(hitTP1 bool, unrealizedPct float64) bool {
	if !envBool("LIVE_BE_REQUIRE_TP1_OR_MIN_UPNL", true) {
		return true
	}
	if hitTP1 {
		return true
	}
	return unrealizedPct >= envFloat("LIVE_BE_MIN_UPNL_PCT", 5.0)
}

func targetHit(side string, mark, target float64) bool {
	if mark <= 0 || target <= 0 {
		return false
	}
	if strings.EqualFold(side, "BUY") {
		return mark >= target
	}
	return mark <= target
}

func stopPromotedAfterTP1(p *livePosition) bool {
	if p == nil || !p.HitTP1 || p.EntryPrice <= 0 || p.StopPrice <= 0 {
		return false
	}
	if strings.EqualFold(p.Side, "BUY") {
		return p.StopPrice >= p.EntryPrice
	}
	return p.StopPrice <= p.EntryPrice
}

func improvedStopPrice(side string, current, next float64) (float64, bool) {
	if next <= 0 {
		return current, false
	}
	if strings.EqualFold(side, "BUY") {
		if next > current {
			return next, true
		}
		return current, false
	}
	if current <= 0 || next < current {
		return next, true
	}
	return current, false
}

func ratchetStopTarget(side string, entry, currentStop, tp1, tp2 float64, beLockBps float64, stage int) (float64, bool) {
	switch stage {
	case 1:
		return improvedStopPrice(side, currentStop, beLockPrice(side, entry, beLockBps))
	case 2:
		return improvedStopPrice(side, currentStop, tp1)
	case 3:
		return improvedStopPrice(side, currentStop, tp2)
	default:
		return currentStop, false
	}
}

func liveProtectionLevels() (float64, float64) {
	stage1 := envFloat("LIVE_PROFIT_LOCK_STAGE1_R", 1.0)
	stage2 := envFloat("LIVE_PROFIT_LOCK_STAGE2_R", 2.0)
	if stage1 <= 0 {
		stage1 = 1.0
	}
	if stage2 < stage1 {
		stage2 = stage1
	}
	return stage1, stage2
}

func lockToRPrice(side string, entry, stop, r float64) float64 {
	risk := math.Abs(entry - stop)
	if risk <= 0 || r <= 0 {
		return stop
	}
	if strings.EqualFold(side, "BUY") {
		return entry + risk*r
	}
	return entry - risk*r
}

func applyLiveProtectionState(now time.Time, side string, entry, currentStop, mfeR float64, stage *protectionStage, firstProtectAt *time.Time, protectedStop *float64, beLockBps float64, allowBE bool) (float64, bool) {
	stage1R, stage2R := liveProtectionLevels()
	newStop := currentStop
	changed := false
	if allowBE && mfeR >= stage1R {
		be := beLockPriceBuffered(side, entry, currentStop, beLockBps)
		if stop, ok := improvedStopPrice(side, newStop, be); ok {
			newStop = stop
			changed = true
		}
		if stage != nil && *stage < protectionStageArmed {
			*stage = protectionStageArmed
		}
		if firstProtectAt != nil && firstProtectAt.IsZero() {
			*firstProtectAt = now
		}
	}
	if mfeR >= stage2R {
		if stage != nil && *stage < protectionStageLocked {
			*stage = protectionStageLocked
		}
		if firstProtectAt != nil && firstProtectAt.IsZero() {
			*firstProtectAt = now
		}
	}
	if protectedStop != nil && ((strings.EqualFold(side, "BUY") && newStop > *protectedStop) || (!strings.EqualFold(side, "BUY") && newStop < *protectedStop) || *protectedStop == 0) {
		*protectedStop = newStop
	}
	return newStop, changed
}

func pnlLockTarget(side string, entry, mark, lockFrac float64) float64 {
	lockFrac = clamp(lockFrac, 0, 0.95)
	if entry <= 0 || mark <= 0 || lockFrac <= 0 {
		return entry
	}
	move := mark - entry
	if strings.EqualFold(side, "SELL") || strings.EqualFold(side, "SHORT") {
		move = entry - mark
		if move <= 0 {
			return entry
		}
		return entry - move*lockFrac
	}
	if move <= 0 {
		return entry
	}
	return entry + move*lockFrac
}

func applyPnLProtectiveStop(side string, entry, currentStop, mark, upnlPct float64) (float64, bool) {
	armPct := envFloat("LIVE_PNL_PROTECT_ARM_PCT", 20.0)
	lockFrac := envFloat("LIVE_PNL_PROTECT_LOCK_FRAC", 0.75)
	if armPct <= 0 || lockFrac <= 0 || upnlPct < armPct {
		return currentStop, false
	}
	target := pnlLockTarget(side, entry, mark, lockFrac)
	return improvedStopPrice(side, currentStop, target)
}

func earlyContinuationReady(p *livePosition) bool {
	if p == nil {
		return false
	}
	minR := envFloat("LIVE_EARLY_CONTINUATION_MIN_R", 0.35)
	minHold := time.Duration(envInt("LIVE_EARLY_CONTINUATION_MIN_HOLD_MIN", 8)) * time.Minute
	if p.AddCount > 0 || p.HitTP1 || p.HitTP2 || p.HitTP3 {
		return true
	}
	if minR > 0 && p.MaxFavorableR >= minR {
		return true
	}
	if minHold > 0 && !p.CreatedAt.IsZero() && time.Since(p.CreatedAt) >= minHold && p.MaxFavorableR > 0 {
		return true
	}
	if p.Sponsored || confluenceRefreshActive(time.Now().UTC(), p.LastConfluenceRefresh) {
		return true
	}
	return false
}

func matureTrendForWinnerLifecycle(maxR float64, htfPersistent bool, hitTP1, hitTP2, hitTP3 bool) bool {
	if hitTP2 || hitTP3 {
		return true
	}
	return maxR >= envFloat("LIVE_EXIT_EARLY_TRAIL_R", 1.0) && (htfPersistent || hitTP1)
}

func lifecycleSoftExitsCanHardClose(raw string) bool {
	switch exitmgr.NormalizeWinnerLifecycle(raw) {
	case exitmgr.WinnerLifecycleWinnerLocked, exitmgr.WinnerLifecycleRunner, exitmgr.WinnerLifecycleLateTrail:
		return false
	default:
		return true
	}
}

func lifecycleNeedsManualTrendCaptureWarning(raw string) bool {
	switch exitmgr.NormalizeWinnerLifecycle(raw) {
	case exitmgr.WinnerLifecycleWinnerLocked, exitmgr.WinnerLifecycleRunner, exitmgr.WinnerLifecycleLateTrail:
		return true
	default:
		return false
	}
}

func manualTrendCaptureGuidance(raw string) string {
	if !lifecycleNeedsManualTrendCaptureWarning(raw) {
		return ""
	}
	stage := strings.ToUpper(strings.TrimSpace(raw))
	if stage == "" {
		stage = "WINNER_LOCKED"
	}
	return fmt.Sprintf("<b>Trend Capture:</b> %s protected. Avoid manual churn unless emergency or structure fails.", stage)
}

func syncLiveWinnerLifecycle(p *livePosition, htfPersistent bool) {
	if p == nil {
		return
	}
	currentR := unrealizedRiskR(p.Side, p.EntryPrice, p.StopPrice, p.LastMark)
	p.WinnerLifecycle = string(exitmgr.ResolveWinnerLifecycle(
		exitmgr.NormalizeWinnerLifecycle(p.WinnerLifecycle),
		exitmgr.WinnerLifecycleInput{
			MaxR:           p.MaxFavorableR,
			CurrentR:       currentR,
			ProofObserved:  p.MaxFavorableR >= envFloat("LIVE_EXIT_FIRST_PROOF_R", 0.15) || p.HitTP1 || p.HitTP2 || p.HitTP3 || earlyContinuationReady(p),
			MatureTrend:    matureTrendForWinnerLifecycle(p.MaxFavorableR, htfPersistent, p.HitTP1, p.HitTP2, p.HitTP3),
			TrailingActive: p.TrailOn,
		},
	))
}

func syncPaperWinnerLifecycle(pos *paperPosition, htfPersistent bool) {
	if pos == nil {
		return
	}
	currentR := unrealizedRiskR(pos.Side, pos.Entry, pos.Stop, pos.LastMark)
	pos.WinnerLifecycle = string(exitmgr.ResolveWinnerLifecycle(
		exitmgr.NormalizeWinnerLifecycle(pos.WinnerLifecycle),
		exitmgr.WinnerLifecycleInput{
			MaxR:           pos.MaxFavorableR,
			CurrentR:       currentR,
			ProofObserved:  pos.MaxFavorableR >= envFloat("LIVE_EXIT_FIRST_PROOF_R", 0.15) || pos.HitTP1 || pos.HitTP2 || pos.HitTP3 || paperAdvancedReady(pos),
			MatureTrend:    matureTrendForWinnerLifecycle(pos.MaxFavorableR, htfPersistent, pos.HitTP1, pos.HitTP2, pos.HitTP3),
			TrailingActive: pos.TrailOn,
		},
	))
}

func lifecycleTransitionReason(from, to, fallback string, trailingActive bool) string {
	if fallback != "" {
		return fallback
	}
	switch {
	case from == "" && to == "":
		return ""
	case to == "proof_armed":
		return "proof_observed"
	case to == "winner_locked":
		return "instant_be_lock"
	case to == "runner":
		return "early_profit_trail"
	case to == "late_trail":
		if trailingActive {
			return "trail_activated"
		}
		return "mature_trend"
	case to == "failed":
		return "real_invalidation"
	default:
		return "lifecycle_advance"
	}
}

func logWinnerLifecycleTransition(symbol, side, from, to, reason string) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == to || to == "" {
		return
	}
	fmt.Printf("WINNER_LIFECYCLE_TRANSITION symbol=%s side=%s from=%s to=%s reason=%s\n",
		symbol, side, firstNonEmpty(from, "starter"), to, firstNonEmpty(reason, "lifecycle_advance"))
}

func protectDecisionLogLine(symbol, side, action, currentLifecycle, nextLifecycle, reason string, computedStop, submittedStop, acceptedStop float64, triggerRef string, legalityAdjusted bool, mfe float64, htfState string, persistent, failed, caution bool) string {
	return fmt.Sprintf("PROTECT_DECISION symbol=%s side=%s winner_lifecycle=%s next_winner_lifecycle=%s action=%s reason=%s computed_stop=%.8f submitted_stop=%.8f accepted_stop=%.8f trigger_ref=%s legality_adjustment_applied=%t mfe=%.4f htf_state=%s persistent=%t failed=%t caution=%t",
		symbol,
		side,
		firstNonEmpty(currentLifecycle, "starter"),
		firstNonEmpty(nextLifecycle, firstNonEmpty(currentLifecycle, "starter")),
		action,
		firstNonEmpty(reason, "PROTECT"),
		computedStop,
		submittedStop,
		acceptedStop,
		strings.ToLower(strings.TrimSpace(triggerRef)),
		legalityAdjusted,
		mfe,
		htfState,
		persistent,
		failed,
		caution,
	)
}

func logProtectDecisionOnce(lastKey *string, line string) {
	if lastKey == nil {
		fmt.Println(line)
		return
	}
	if *lastKey == line {
		return
	}
	*lastKey = line
	fmt.Println(line)
}

func logPaperProtectDecision(symbol string, pos *paperPosition, action string, dec exitmgr.ProtectDecision, submittedStop, acceptedStop float64, legalityAdjusted bool) {
	if pos == nil {
		return
	}
	line := protectDecisionLogLine(symbol, pos.Side, action, dec.CurrentWinnerLifecycle, dec.WinnerLifecycle,
		firstNonEmpty(dec.ExitNowReason, dec.Reason, "PROTECT"),
		dec.ComputedStop, submittedStop, acceptedStop, dec.TriggerRef, legalityAdjusted, pos.MaxFavorableR,
		dec.HTFTrendState, dec.HTFPersistent, dec.HTFFailed, dec.HTFCaution)
	logProtectDecisionOnce(&pos.lastProtectDecisionKey, line)
}

func queueLiveProtectStopChain(p *livePosition, action string, dec exitmgr.ProtectDecision, computedStop float64) {
	if p == nil {
		return
	}
	p.pendingProtectAction = action
	p.pendingProtectReason = firstNonEmpty(dec.ExitNowReason, dec.Reason, "PROTECT")
	p.pendingProtectCurrent = dec.CurrentWinnerLifecycle
	p.pendingProtectNext = dec.WinnerLifecycle
	p.pendingProtectTrigger = dec.TriggerRef
	p.pendingProtectHTFState = dec.HTFTrendState
	p.pendingProtectMFE = p.MaxFavorableR
	p.pendingProtectPersistent = dec.HTFPersistent
	p.pendingProtectFailed = dec.HTFFailed
	p.pendingProtectCaution = dec.HTFCaution
	p.pendingProtectComputed = computedStop
}

func clearQueuedLiveProtectStopChain(p *livePosition) {
	if p == nil {
		return
	}
	p.pendingProtectAction = ""
	p.pendingProtectReason = ""
	p.pendingProtectCurrent = ""
	p.pendingProtectNext = ""
	p.pendingProtectTrigger = ""
	p.pendingProtectHTFState = ""
	p.pendingProtectMFE = 0
	p.pendingProtectPersistent = false
	p.pendingProtectFailed = false
	p.pendingProtectCaution = false
	p.pendingProtectComputed = 0
}

func (m *liveExecManager) logQueuedProtectStopChain(p *livePosition, submittedStop, acceptedStop float64, legalityAdjusted bool) {
	if m == nil || p == nil || strings.TrimSpace(p.pendingProtectAction) == "" {
		return
	}
	line := protectDecisionLogLine(p.Symbol, p.Side, p.pendingProtectAction, p.pendingProtectCurrent, p.pendingProtectNext,
		p.pendingProtectReason, p.pendingProtectComputed, submittedStop, acceptedStop, p.pendingProtectTrigger, legalityAdjusted,
		p.pendingProtectMFE, p.pendingProtectHTFState, p.pendingProtectPersistent, p.pendingProtectFailed, p.pendingProtectCaution)
	logProtectDecisionOnce(&p.lastProtectDecisionKey, line)
	clearQueuedLiveProtectStopChain(p)
}

func shouldAdvanceProtection(p *livePosition) bool {
	if p == nil {
		return false
	}
	switch p.ManagePhase {
	case managePhaseExhaustion, managePhaseContinuation:
		return true
	default:
		if importedManagedPosition(p) {
			return p.MaxFavorableR >= envFloat("LIVE_IMPORTED_PROTECTION_MIN_R", 0.35) || p.HitTP1 || p.HitTP2 || p.HitTP3
		}
		if earlyContinuationReady(p) {
			return true
		}
		return p.MaxFavorableR >= envFloat("LIVE_CONTINUATION_PROTECTION_MIN_R", 1.25)
	}
}

func runnerPreservePct() float64 {
	return clamp(envFloat("LIVE_RUNNER_PRESERVE_PCT", 0.30), 0.05, 0.50)
}

func runnerMinQtyForPosition(p *livePosition, starterUSDT float64) float64 {
	if p == nil || p.RemainingQty <= 0 || p.DeployedMargin <= 0 || starterUSDT <= 0 {
		return 0
	}
	if p.DeployedMargin < starterUSDT*2 {
		return 0
	}
	sizeModelQty := p.RemainingQty * (starterUSDT / maxFloat(p.DeployedMargin, starterUSDT))
	pctModelQty := p.RemainingQty * runnerPreservePct()
	return min(p.RemainingQty, maxFloat(sizeModelQty, pctModelQty))
}

func updateManagePhase(p *livePosition, exhaustion bool) {
	if p == nil {
		return
	}
	if exhaustion {
		p.ManagePhase = managePhaseExhaustion
		return
	}
	p.ManagePhase = managePhaseContinuation
}

func refreshRunnerReservation(p *livePosition, starterUSDT float64) {
	if p == nil {
		return
	}
	if p.ManagePhase != managePhaseContinuation && p.ManagePhase != managePhaseExhaustion {
		p.RunnerMinQty = 0
		return
	}
	p.RunnerMinQty = runnerMinQtyForPosition(p, starterUSDT)
}

func shouldPreserveRunnerOnExit(p *livePosition, reason string) bool {
	if p == nil || p.RemainingQty <= 0 || p.RunnerMinQty <= 0 {
		return false
	}
	if p.ManagePhase != managePhaseContinuation && p.ManagePhase != managePhaseExhaustion {
		return false
	}
	if strings.Contains(strings.ToUpper(strings.TrimSpace(reason)), "STRUCTURE") || strings.Contains(strings.ToUpper(strings.TrimSpace(reason)), "INVALID") {
		return false
	}
	return p.RemainingQty-p.RunnerMinQty > fillEpsilon(p.RemainingQty)
}

func runnerCaptureFailed(p *livePosition) bool {
	if p == nil {
		return false
	}
	minMFE := envFloat("LIVE_RUNNER_CAPTURE_FAIL_MIN_MFE_R", 2.0)
	maxCapture := envFloat("LIVE_RUNNER_CAPTURE_FAIL_MAX_CAPTURE_RATIO", 0.25)
	maxReturnPct := envFloat("LIVE_RUNNER_CAPTURE_FAIL_MAX_RETURN_PCT", 1.0)
	return p.MaxFavorableR >= minMFE &&
		p.CaptureRatio <= maxCapture &&
		math.Abs(tradeReturnPct(p)) <= maxReturnPct
}

func (m *liveExecManager) trimToRunner(now time.Time, p *livePosition, reason string) bool {
	if m == nil || p == nil || !shouldPreserveRunnerOnExit(p, reason) {
		return false
	}
	trimQty := maxFloat(0, p.RemainingQty-p.RunnerMinQty)
	if trimQty <= fillEpsilon(p.RemainingQty) {
		return false
	}
	if err := m.submitCloseLimit(p, trimQty, reason, "TRIM"); err != nil {
		return false
	}
	p.UpdatedAt = now
	return true
}

func updateGivebackMetrics(mfeR, riskR float64, captureRatio *float64, maxGivebackR *float64) {
	if captureRatio != nil && mfeR > 0 {
		*captureRatio = clamp(riskR/mfeR, -10, 10)
	}
	if maxGivebackR != nil && mfeR > riskR {
		*maxGivebackR = maxFloat(*maxGivebackR, mfeR-riskR)
	}
}

func unrealizedRiskR(side string, entry, stop, mark float64) float64 {
	risk := math.Abs(entry - stop)
	if risk <= 0 || mark <= 0 || entry <= 0 {
		return 0
	}
	if strings.EqualFold(side, "BUY") {
		return (mark - entry) / risk
	}
	return (entry - mark) / risk
}

func stopStillOriginal(stop, originalStop float64) bool {
	if stop <= 0 || originalStop <= 0 {
		return false
	}
	tol := math.Max(math.Abs(originalStop)*0.00005, 1e-9)
	return math.Abs(stop-originalStop) <= tol
}

func winnerProofR() float64 {
	return envFloat("LIVE_EXIT_PROOF_R", 1.0)
}

func protectAfterProofEnabled() bool {
	return envBool("LIVE_EXIT_PROTECT_AFTER_PROOF", true)
}

func enforceWinnerBEFloor(side string, entry, stop, maxR float64) (float64, bool) {
	if entry <= 0 || stop <= 0 || maxR < winnerProofR() {
		return stop, false
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		if stop < entry {
			return entry, true
		}
		return stop, false
	}
	if stop > entry {
		return entry, true
	}
	return stop, false
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

func sanitizeBracketGeometry(entry float64, side string, stop, tp1, tp2, tp3 float64) (float64, float64, float64, float64) {
	if entry <= 0 {
		return stop, tp1, tp2, tp3
	}
	minSep := math.Max(entry*0.0008, abs(entry-stop)*0.15) // 8 bps or 15% of risk
	minSep = math.Min(minSep, entry*0.10)                  // avoid excessive widening on tiny prices
	if minSep <= 0 {
		minSep = entry * 0.001
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		if stop >= entry {
			stop = entry - minSep
		}
		if tp1 <= entry {
			tp1 = entry + minSep
		}
		if tp2 <= tp1 {
			tp2 = tp1 + minSep
		}
		if tp3 <= tp2 {
			tp3 = tp2 + minSep
		}
	} else {
		if stop <= entry {
			stop = entry + minSep
		}
		if tp1 >= entry {
			tp1 = entry - minSep
		}
		if tp2 >= tp1 {
			tp2 = tp1 - minSep
		}
		if tp3 >= tp2 {
			tp3 = tp2 - minSep
		}
	}
	if stop <= 0 || tp1 <= 0 || tp2 <= 0 || tp3 <= 0 {
		// Hard safety fallback to preserve side geometry.
		if strings.EqualFold(strings.TrimSpace(side), "BUY") {
			stop = entry * (1 - 0.003)
			tp1 = entry * (1 + 0.003)
			tp2 = entry * (1 + 0.006)
			tp3 = entry * (1 + 0.009)
		} else {
			stop = entry * (1 + 0.003)
			tp1 = entry * (1 - 0.003)
			tp2 = entry * (1 - 0.006)
			tp3 = entry * (1 - 0.009)
		}
	}
	return stop, tp1, tp2, tp3
}

func marginRiskStopPct(margin float64, leverage int, riskMarginPct float64) float64 {
	if margin <= 0 || leverage <= 0 || riskMarginPct <= 0 {
		return 0
	}
	notional := margin * float64(leverage)
	if notional <= 0 {
		return 0
	}
	riskUSD := margin * (riskMarginPct / 100.0)
	if riskUSD <= 0 {
		return 0
	}
	return riskUSD / notional
}

func isIgnorableMarginTypeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "no need to change margin type") ||
		strings.Contains(msg, "no need to change to isolated") ||
		strings.Contains(msg, "margin type is isolated") ||
		strings.Contains(msg, "same margin type") ||
		strings.Contains(msg, "\"code\":-2014") ||
		strings.Contains(msg, "code=-2014")
}

func rrBelowMinimum(rr, minRR float64) bool {
	if minRR <= 0 {
		return false
	}
	const rrTol = 1e-6
	return rr+rrTol < minRR
}

func marginTypeAlreadySet(rows []map[string]any, symbol, want string) bool {
	symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	want = strings.ToUpper(strings.TrimSpace(want))
	if symbol == "" || want == "" {
		return false
	}
	for _, row := range rows {
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprint(row["symbol"])), symbol) {
			continue
		}
		rowType := strings.ToUpper(strings.TrimSpace(fmt.Sprint(row["marginType"])))
		if rowType == want {
			return true
		}
		if want == "ISOLATED" {
			if strings.EqualFold(strings.TrimSpace(fmt.Sprint(row["isolated"])), "true") {
				return true
			}
			if mapFloat(row["isolatedWallet"]) > 0 {
				return true
			}
		}
		if want == "CROSSED" {
			if strings.EqualFold(strings.TrimSpace(fmt.Sprint(row["isolated"])), "false") && mapFloat(row["isolatedWallet"]) <= 0 {
				return true
			}
		}
	}
	return false
}

func realizedFromFill(side string, entry, fillPx, qty float64) (float64, float64) {
	if entry <= 0 || fillPx <= 0 || qty <= 0 {
		return 0, 0
	}
	pnl := 0.0
	pct := 0.0
	if isLongSide(side) {
		pnl = (fillPx - entry) * qty
		pct = ((fillPx - entry) / entry) * 100
	} else {
		pnl = (entry - fillPx) * qty
		pct = ((entry - fillPx) / entry) * 100
	}
	return pnl, pct
}

func stateAwareStopMultiplier(reason, grade string, state inplay.State, conf, volumeUSD float64) float64 {
	mult := 1.0
	switch strings.ToUpper(strings.TrimSpace(grade)) {
	case "A+":
		mult *= 1.18
	case "A":
		mult *= 1.10
	case "B":
		mult *= 1.03
	case "C", "D":
		mult *= 0.96
	}
	switch state {
	case inplay.StatePumping:
		mult *= 1.22
	case inplay.StateInPlay:
		mult *= 1.14
	case inplay.StateHeating:
		mult *= 1.08
	case inplay.StateBalanced:
		mult *= 1.00
	case inplay.StateCooling:
		mult *= 0.94
	case inplay.StateDumping, inplay.StateExhausted:
		mult *= 0.90
	}
	r := strings.ToLower(strings.TrimSpace(reason))
	if strings.Contains(r, "continuation") {
		mult *= 1.06
	}
	if conf >= 0.75 {
		mult *= 1.04
	} else if conf > 0 && conf < 0.60 {
		mult *= 0.95
	}
	if volumeUSD >= envFloat("LIVE_STOP_SWING_VOL_USD", 100_000_000) {
		mult *= 1.05
	}
	return clamp(mult, envFloat("LIVE_STOP_STATE_MULT_MIN", 0.88), envFloat("LIVE_STOP_STATE_MULT_MAX", 1.55))
}

func adjustBracketParams(reason, grade string, state inplay.State, conf, volumeUSD, stopPct, tp1R, tp2R, tp3R, minStopPct, maxStopPct float64) (float64, float64, float64, float64) {
	tp1Max := envFloat("LIVE_TP1_MAX_R", 2.5)
	tp2Max := envFloat("LIVE_TP2_MAX_R", 4.0)
	tp3Max := envFloat("LIVE_TP3_MAX_R", 6.0)
	stopWiden := envFloat("LIVE_STOP_WIDEN_MULT", 1.32)
	softenConf := envFloat("LIVE_SOFTEN_CONF_MAX", 0.65)

	r := strings.ToLower(strings.TrimSpace(reason))
	if strings.EqualFold(r, "exhaustion_flip_short") {
		stopPct *= envFloat("LIVE_EXHAUSTION_STOP_MULT", 1.08)
		stopPct = clamp(stopPct, minStopPct, maxStopPct)
		tp1R = envFloat("LIVE_EXHAUSTION_TP1_R", 0.8)
		tp2R = envFloat("LIVE_EXHAUSTION_TP2_R", 1.6)
		tp3R = envFloat("LIVE_EXHAUSTION_TP3_R", 2.4)
		return stopPct, tp1R, tp2R, tp3R
	}
	if strings.EqualFold(r, "exhaustion_flip_long") {
		stopPct *= envFloat("LIVE_EXHAUSTION_LONG_STOP_MULT", envFloat("LIVE_EXHAUSTION_STOP_MULT", 1.08))
		stopPct = clamp(stopPct, minStopPct, maxStopPct)
		tp1R = envFloat("LIVE_EXHAUSTION_LONG_TP1_R", 0.8)
		tp2R = envFloat("LIVE_EXHAUSTION_LONG_TP2_R", 1.6)
		tp3R = envFloat("LIVE_EXHAUSTION_LONG_TP3_R", 2.4)
		return stopPct, tp1R, tp2R, tp3R
	}
	if strings.EqualFold(r, "momentum_ignite_long") || strings.EqualFold(r, "momentum_ignite_short") {
		stopPct *= envFloat("LIVE_IGNITE_STOP_MULT", 1.10)
		stopPct = clamp(stopPct, minStopPct, maxStopPct)
		tp1R = envFloat("LIVE_IGNITE_TP1_R", 1.0)
		tp2R = envFloat("LIVE_IGNITE_TP2_R", 2.4)
		tp3R = envFloat("LIVE_IGNITE_TP3_R", 4.2)
		return stopPct, tp1R, tp2R, tp3R
	}
	soften := strings.Contains(r, "failed_auction") || strings.Contains(r, "rejection") || conf <= softenConf
	if soften && stopWiden > 0 {
		stopPct *= stopWiden
	}
	stopPct *= stateAwareStopMultiplier(reason, grade, state, conf, volumeUSD)
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
	if m.dayRealized == nil {
		m.dayRealized = map[string]float64{}
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
			dayUTC := 0.0
			if r.DayUTC24h != nil {
				dayUTC = *r.DayUTC24h
			}
			utc4h := 0.0
			if r.UTC4hPct != nil {
				utc4h = *r.UTC4hPct
			}
			utc1h := 0.0
			if r.UTC1hPct != nil {
				utc1h = *r.UTC1hPct
			}
			out[raw] = symbolMeta{
				LastPrice:   r.LastPrice,
				OpenPrice:   r.OpenPrice,
				Move24h:     r.Change24h,
				DayUTC24h:   dayUTC,
				UTC4hPct:    utc4h,
				UTC1hPct:    utc1h,
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
	cfg.MinScore = envFloat("LIVE_GATE_MIN_SCORE", 70)
	cfg.MinSlope = envFloat("LIVE_GATE_MIN_SLOPE", 0.08)
	cfg.RequireVolumeSpike = envBool("LIVE_GATE_REQUIRE_VOLUME_SPIKE", false)
	cfg.MinVolumeRatio = envFloat("LIVE_GATE_MIN_VOLUME_RATIO", 1.2)
	cfg.RequireMTF = envBool("LIVE_GATE_REQUIRE_MTF", false)
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

func buildDiscoveryCandles(cache *featureRuntimeCache, longRows, shortRows []market.Scored, cfg discovery.Config) map[string][]types.Candle {
	if cache == nil {
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
		bars, err := cache.candleSeries(sym, types.TF1m, limit)
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

func loadWatchConfig() watchConfig {
	watchSec := envInt("LIVE_WATCHER_SEC", envInt("LIVE_WATCH_SEC", 1))
	every := time.Duration(watchSec) * time.Second
	if every <= 0 {
		every = time.Second
	}
	prioritySec := envInt("LIVE_PRIORITY_WATCH_EVERY_SEC", 1)
	priorityEvery := time.Duration(prioritySec) * time.Second
	if priorityEvery <= 0 {
		priorityEvery = time.Second
	}
	maxCandidates := envInt("LIVE_WATCHLIST_SIZE", envInt("WATCH_MAX_CANDIDATES", 20))
	if maxCandidates <= 0 {
		maxCandidates = 20
	}
	bookLevels := envInt("WATCH_BOOK_LEVELS", 5)
	if bookLevels <= 0 {
		bookLevels = 5
	}
	alpha := envFloat("LIVE_OFI_EWMA_ALPHA", 0.05)
	if alpha <= 0 || alpha >= 1 {
		alpha = 0.05
	}
	minSamples := envInt("LIVE_OFI_MIN_SAMPLES", 8)
	if minSamples < 1 {
		minSamples = 1
	}
	return watchConfig{
		Enable:          envBool("WATCH_ENABLE", true),
		Every:           every,
		PriorityEvery:   priorityEvery,
		MaxCandidates:   maxCandidates,
		TopNOnly:        envBool("LIVE_WATCH_TOPN_ONLY", true),
		WatchOpen:       envBool("LIVE_WATCH_OPEN_POSITIONS", true),
		BookLevels:      bookLevels,
		EnableOFI:       envBool("LIVE_ENABLE_OFI", true),
		OFIAlpha:        alpha,
		OFIMinSamples:   minSamples,
		IgniteMinOFIZ:   envFloat("LIVE_IGNITE_MIN_OFI_Z", 0.60),
		ContMinOFIZ:     envFloat("LIVE_CONT_FAST_MIN_OFI_Z", 0.35),
		RevLongMinOFIZ:  envFloat("LIVE_REV_LONG_MIN_OFI_Z", 0.80),
		RevShortMaxOFIZ: envFloat("LIVE_REV_SHORT_MAX_OFI_Z", -0.80),
	}
}

func loadHybridStopConfig() exitmgr.HybridStopConfig {
	cfg := exitmgr.DefaultHybridStopConfig()
	cfg.Enabled = envBool("LIVE_STOP_ENGINE_V2_ENABLE", true)
	cfg.TemplateMode = strings.ToLower(envStr("LIVE_STOP_TEMPLATE_MODE", "setup"))
	cfg.ATRMultCont = envFloat("LIVE_STOP_ATR_MULT_CONT", cfg.ATRMultCont)
	cfg.ATRMultPullback = envFloat("LIVE_STOP_ATR_MULT_PULLBACK", cfg.ATRMultPullback)
	cfg.ATRMultReversal = envFloat("LIVE_STOP_ATR_MULT_REVERSAL", cfg.ATRMultReversal)
	cfg.ATRMultMeanRevert = envFloat("LIVE_STOP_ATR_MULT_MEAN_REVERT", cfg.ATRMultMeanRevert)
	cfg.SweepBufferBps = envFloat("LIVE_STOP_SWEEP_BUFFER_BPS", cfg.SweepBufferBps)
	cfg.MinWidthPct = envFloat("LIVE_MIN_STOP_PCT", cfg.MinWidthPct)
	cfg.MaxWidthPct = envFloat("LIVE_STOP_MAX_WIDTH_PCT", envFloat("LIVE_MAX_STOP_PCT", cfg.MaxWidthPct))
	cfg.MinRRToTP1 = envFloat("LIVE_STOP_MIN_RR_TO_TP1", envFloat("LIVE_MIN_RR_TP1", cfg.MinRRToTP1))
	cfg.SoftRejectEnable = envBool("LIVE_STOP_SOFT_REJECT_ENABLE", cfg.SoftRejectEnable)
	cfg.SoftRejectMaxWidthPct = envFloat("LIVE_STOP_SOFT_REJECT_MAX_WIDTH_PCT", cfg.SoftRejectMaxWidthPct)
	cfg.SoftRejectMinRRToTP1 = envFloat("LIVE_STOP_SOFT_REJECT_MIN_RR_TO_TP1", cfg.SoftRejectMinRRToTP1)
	return cfg
}

func loadLadderConfig(defaultStarter float64) ladderConfig {
	tradeFallback := maxFloat(defaultStarter, 10)
	tradeUSDT := envFloat("LIVE_TRADE_MARGIN_USDT", envFloat("LIVE_STARTER_USDT", envFloat("LIVE_ENTRY_STARTER_USDT", tradeFallback)))
	cfg := ladderConfig{
		StarterUSDT:   tradeUSDT,
		StepUSDT:      0,
		MaxTotalUSDT:  tradeUSDT,
		OnlyIfGreen:   false,
		MinAddPnLPct:  0,
		MaxAdds:       0,
		OneSymbolOnly: envBool("LIVE_ONE_SYMBOL_ONLY", false),
	}
	if cfg.StarterUSDT <= 0 {
		cfg.StarterUSDT = tradeFallback
	}
	return cfg
}

func ladderAddsDisabled(cfg ladderConfig) bool {
	return true
}

func loadFundsManagerConfig() fundsManagerConfig {
	cfg := fundsManagerConfig{
		Enable:                    envBool("LIVE_FUNDS_MANAGER_ENABLE", false),
		PerpTargetUSDT:            envFloat("LIVE_PERP_BAL_TARGET_USDT", 100),
		PerpFloorUSDT:             envFloat("LIVE_PERP_BAL_FLOOR_USDT", 50),
		TopupMinUSDT:              envFloat("LIVE_TOPUP_MIN_USDT", 10),
		SweepMinUSDT:              envFloat("LIVE_SWEEP_MIN_USDT", 0.01),
		SweepProfitEnable:         envBool("LIVE_SWEEP_PROFIT_ENABLE", true),
		SweepExcessOverTargetOnly: envBool("LIVE_SWEEP_EXCESS_OVER_TARGET_ONLY", false),
	}
	if cfg.PerpTargetUSDT < 0 {
		cfg.PerpTargetUSDT = 0
	}
	if cfg.PerpFloorUSDT < 0 {
		cfg.PerpFloorUSDT = 0
	}
	if cfg.PerpTargetUSDT < cfg.PerpFloorUSDT {
		cfg.PerpTargetUSDT = cfg.PerpFloorUSDT
	}
	if cfg.TopupMinUSDT < 0 {
		cfg.TopupMinUSDT = 0
	}
	if cfg.SweepMinUSDT < 0 {
		cfg.SweepMinUSDT = 0
	}
	return cfg
}

func fundsMaintenanceEvery() time.Duration {
	d := time.Duration(envInt("LIVE_FUNDS_MAINTENANCE_SEC", 60)) * time.Second
	if d < 0 {
		return 0
	}
	return d
}

func perpSweepAmount(avail float64, cfg fundsManagerConfig) float64 {
	if avail <= 0 || avail <= cfg.PerpTargetUSDT {
		return 0
	}
	return maxFloat(0, avail-cfg.PerpTargetUSDT)
}

func autoSweepAmount(avail float64, cfg fundsManagerConfig) float64 {
	excess := perpSweepAmount(avail, cfg)
	if excess <= 0 {
		return 0
	}
	if excess < cfg.SweepMinUSDT {
		return 0
	}
	return excess
}

func perpTopupTarget(avail float64, cfg fundsManagerConfig) float64 {
	if avail >= cfg.PerpFloorUSDT {
		return 0
	}
	return cfg.PerpTargetUSDT
}

func loadReentryConfig(defaultSize float64) reentryConfig {
	cfg := reentryConfig{
		Enable:       false,
		SizeUSDT:     0,
		MaxPerSymbol: 0,
		Cooldown:     0,
	}
	_ = defaultSize
	return cfg
}

func runtimeMaintenanceWindows() []maintenanceWindow {
	windows := []maintenanceWindow{
		normalizeMaintenanceWindow(maintenanceWindow{
			Name:      "MAINTENANCE",
			Enabled:   envBool("LIVE_MAINT1_ENABLE", true),
			StartHour: envInt("LIVE_MAINT1_START_HOUR", 18),
			StartMin:  envInt("LIVE_MAINT1_START_MIN", 0),
			EndHour:   envInt("LIVE_MAINT1_END_HOUR", 19),
			EndMin:    envInt("LIVE_MAINT1_END_MIN", 0),
			ForceFlat: false,
		}),
	}
	out := make([]maintenanceWindow, 0, len(windows))
	for _, w := range windows {
		if w.Enabled {
			out = append(out, w)
		}
	}
	return out
}

func blockedWindowReason(w maintenanceWindow) string {
	return blockedMaintenanceWindowReason
}

func maintenanceLocation() *time.Location {
	name := envStr("LIVE_MAINT_TZ", "America/Chicago")
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}

func blockedNewRiskWindow(now time.Time, loc *time.Location) (maintenanceWindow, string, bool) {
	if loc == nil {
		loc = maintenanceLocation()
	}
	local := now.In(loc)
	window, active := activeMaintenanceWindow(local, true, runtimeMaintenanceWindows()...)
	if !active {
		return maintenanceWindow{}, "", false
	}
	return window, blockedWindowReason(window), true
}

func manageApprovalBlockedByWindow(now time.Time, action string) (string, bool) {
	_, reason, blocked := blockedNewRiskWindow(now, maintenanceLocation())
	if blocked && !strings.EqualFold(strings.TrimSpace(action), "FORCE_FLAT") {
		return reason, true
	}
	return "", false
}

func accountHealthBlocksNewRisk(report accountReport) bool {
	if report.Generated.IsZero() {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(report.Health), "healthy")
}

func reconcileMaxStale() time.Duration {
	d := time.Duration(envInt("LIVE_RECONCILE_MAX_STALE_SEC", 120)) * time.Second
	if d <= 0 {
		d = 120 * time.Second
	}
	return d
}

func orderLegalityQuarantineWindow() time.Duration {
	d := time.Duration(envInt("LIVE_ORDER_LEGALITY_QUARANTINE_SEC", 1800)) * time.Second
	if d <= 0 {
		d = 30 * time.Minute
	}
	return d
}

func orderLegalityFailLimit() int {
	n := envInt("LIVE_ORDER_LEGALITY_FAIL_LIMIT", 3)
	if n <= 0 {
		n = 3
	}
	return n
}

func (m *liveExecManager) userDataFresh() bool {
	if m == nil {
		return false
	}
	if !envBool("LIVE_USERDATA_STREAM_ENABLE", true) {
		return true
	}
	_, ok := userDataSnapshotFresh(m.userDataState)
	return ok
}

func (m *liveExecManager) reconcileFresh(now time.Time) bool {
	if m == nil {
		return false
	}
	if m.lastReconcileOKAt.IsZero() {
		return false
	}
	if now.Sub(m.lastReconcileOKAt) > reconcileMaxStale() {
		return false
	}
	return m.reconcileFailCount == 0
}

func (m *liveExecManager) symbolQuarantined(symbol string, now time.Time) bool {
	if m == nil {
		return false
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if raw == "" {
		return false
	}
	until := m.symbolQuarantineTill[raw]
	return !until.IsZero() && now.Before(until)
}

func (m *liveExecManager) recordOrderLegalityFailure(symbol, reason string, now time.Time) {
	if m == nil {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if raw == "" {
		return
	}
	m.legalityFailCount[raw]++
	limit := orderLegalityFailLimit()
	if m.legalityFailCount[raw] >= limit {
		m.symbolQuarantineTill[raw] = now.Add(orderLegalityQuarantineWindow())
	}
	quarantineUntil := m.symbolQuarantineTill[raw]
	freshBlocked := !quarantineUntil.IsZero() && now.Before(quarantineUntil)
	eventType := "ORDER_LEGALITY_WARNING"
	logPrefix := "LEGALITY_WARNING"
	if freshBlocked {
		eventType = "ORDER_LEGALITY_QUARANTINE"
		logPrefix = "LEGALITY_QUARANTINE"
	}
	fmt.Printf("%s symbol=%s reason=%s failure_count=%d quarantine_until=%s fresh_entries_blocked=%t\n",
		logPrefix, raw, strings.TrimSpace(reason), m.legalityFailCount[raw], quarantineUntil.Format(time.RFC3339), freshBlocked)
	if m.eventLog != nil {
		m.eventLog.Emit(stats.Event{
			Timestamp: now,
			Type:      eventType,
			Symbol:    raw,
			Reason: fmt.Sprintf("reason=%s failure_count=%d quarantine_until=%s fresh_entries_blocked=%t",
				strings.TrimSpace(reason), m.legalityFailCount[raw], quarantineUntil.Format(time.RFC3339), freshBlocked),
		})
	}
}

func (m *liveExecManager) clearOrderLegalityFailures(symbol string) {
	if m == nil {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if raw == "" {
		return
	}
	delete(m.legalityFailCount, raw)
	delete(m.symbolQuarantineTill, raw)
}

func (m *liveExecManager) degradedEntryReason(now time.Time, symbol string) string {
	if m == nil {
		return degradedReconcileStaleReason
	}
	report := m.ensureAccountReportFresh(now.UTC(), 30*time.Second)
	if _, blocked := entriesBlockedByAccountHealth(accountHealthSummary{
		State:                 strings.ToLower(strings.TrimSpace(report.Health)),
		SignedUserDataBackoff: signedUserDataBackoffActive(now.UTC()),
	}); blocked {
		return degradedAccountHealthPartialReason
	}
	if !m.userDataFresh() {
		return degradedUserDataStaleReason
	}
	if !m.reconcileFresh(now) {
		return degradedReconcileStaleReason
	}
	if m.symbolQuarantined(symbol, now) {
		return degradedOrderLegalityQuarantineReason
	}
	return ""
}

func roundToPrecision(v float64, precision int) float64 {
	if precision < 0 {
		return v
	}
	out, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', precision, 64), 64)
	if err != nil {
		return v
	}
	return out
}

func isMultipleOfStep(v, step float64) bool {
	if step <= 0 {
		return true
	}
	ratio := v / step
	return math.Abs(ratio-math.Round(ratio)) <= 1e-9
}

func validateOrderLegality(meta aster.SymbolMeta, qty, price float64) (float64, float64, string) {
	if price <= 0 {
		return 0, 0, orderIllegalTickSizeReason
	}
	if meta.TickSize > 0 {
		rounded := roundToPrecision(math.Floor(price/meta.TickSize)*meta.TickSize, meta.PricePrecision)
		if rounded <= 0 {
			return 0, 0, orderIllegalTickSizeReason
		}
		if math.Abs(rounded-price) > 1e-9 || !isMultipleOfStep(rounded, meta.TickSize) {
			return 0, 0, orderIllegalTickSizeReason
		}
		price = rounded
	}
	if qty <= 0 {
		return 0, 0, orderIllegalMinQtyReason
	}
	if meta.MaxQty > 0 && qty > meta.MaxQty {
		qty = roundToPrecision(math.Floor(meta.MaxQty/maxFloat(meta.StepSize, 1e-12))*maxFloat(meta.StepSize, 1e-12), meta.QtyPrecision)
		if qty <= 0 {
			return 0, 0, orderIllegalMaxQtyReason
		}
	}
	if meta.StepSize > 0 {
		if !isMultipleOfStep(qty, meta.StepSize) {
			return 0, 0, orderIllegalStepSizeReason
		}
	}
	if meta.MinQty > 0 && qty < meta.MinQty {
		return 0, 0, orderIllegalMinQtyReason
	}
	notional := qty * price
	if meta.MinNotional > 0 && notional < meta.MinNotional {
		return 0, 0, orderIllegalMinNotionalReason
	}
	if meta.MaxNotional > 0 && notional > meta.MaxNotional {
		return 0, 0, orderIllegalMaxQtyReason
	}
	return qty, price, ""
}

func loadPostWinCooldownConfig() postWinCooldownConfig {
	cfg := postWinCooldownConfig{
		Enable:       envBool("LIVE_POST_WIN_COOLDOWN_ENABLE", true),
		ThresholdPct: envFloat("LIVE_POST_WIN_THRESHOLD_PCT", 50),
		Cooldown:     time.Duration(envInt("LIVE_POST_WIN_COOLDOWN_SEC", 1800)) * time.Second,
	}
	if cfg.ThresholdPct < 0 {
		cfg.ThresholdPct = 0
	}
	if cfg.Cooldown < 0 {
		cfg.Cooldown = 0
	}
	return cfg
}

func newTransferManager(_ *aster.RESTAuth) TransferManager {
	return noopTransferManager{}
}

func (noopTransferManager) Supported() bool { return false }

func (noopTransferManager) SpotAvailableUSDT() (float64, error) {
	return 0, fmt.Errorf("spot<->perp internal transfers are not supported by the current aster adapter")
}

func (noopTransferManager) TransferSpotToPerp(amount float64) error {
	return fmt.Errorf("spot->perp transfer unavailable for amount %.2f: adapter support not implemented", amount)
}

func (noopTransferManager) TransferPerpToSpot(amount float64) error {
	return fmt.Errorf("perp->spot transfer unavailable for amount %.2f: adapter support not implemented", amount)
}

func copySymbolMetaMap(in map[string]symbolMeta) map[string]symbolMeta {
	if len(in) == 0 {
		return map[string]symbolMeta{}
	}
	out := make(map[string]symbolMeta, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func qualifiesEarlyDevWatchEntry(e inplay.Entry) bool {
	if gradeValue(e.CurrentGrade) < gradeValue("A") {
		return false
	}
	if !(e.State == inplay.StateHeating || e.State == inplay.StateInPlay || e.State == inplay.StatePumping) {
		return false
	}
	if e.ScoreSlope < envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02) {
		return false
	}
	if e.ExhaustionRisk >= envFloat("LIVE_EXHAUSTION_AVOID_CHASE_RISK", 4.5) {
		return false
	}
	if e.Rank > 4.0 {
		return false
	}
	if e.LongDemotionFlag || e.ShortDemotionFlag {
		return false
	}
	return true
}

func buildWatchSymbols(longInPlay, shortInPlay []inplay.Entry, extra []string, maxN int) []string {
	if maxN <= 0 {
		maxN = 20
	}
	type row struct {
		sym  string
		rank float64
	}
	rows := make([]row, 0, len(longInPlay)+len(shortInPlay))
	for _, e := range longInPlay {
		rows = append(rows, row{sym: strings.ToUpper(aster.RawSymbol(e.Symbol)), rank: e.Rank})
	}
	for _, e := range shortInPlay {
		rows = append(rows, row{sym: strings.ToUpper(aster.RawSymbol(e.Symbol)), rank: e.Rank})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].rank > rows[j].rank })

	out := make([]string, 0, maxN)
	seen := map[string]struct{}{}
	add := func(sym string) {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(sym)))
		if raw == "" {
			return
		}
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	for _, sym := range extra {
		add(sym)
		if len(out) >= maxN {
			return out
		}
	}
	for _, r := range rows {
		add(r.sym)
		if len(out) >= maxN {
			break
		}
	}
	return out
}

func topBookSnapshotFromOrderBook(now time.Time, ob aster.OrderBook) (topBookSnapshot, bool) {
	if len(ob.Bids) == 0 || len(ob.Asks) == 0 || ob.Bids[0][0] <= 0 || ob.Asks[0][0] <= 0 {
		return topBookSnapshot{}, false
	}
	return topBookSnapshot{
		Ts:    now,
		BidPx: ob.Bids[0][0],
		BidSz: ob.Bids[0][1],
		AskPx: ob.Asks[0][0],
		AskSz: ob.Asks[0][1],
	}, true
}

func orderBookDepthStats(ob aster.OrderBook, levels int) (bidDepth, askDepth, imb, spreadBps, mid float64) {
	if levels <= 0 {
		levels = 5
	}
	if len(ob.Bids) > 0 && len(ob.Asks) > 0 && ob.Bids[0][0] > 0 && ob.Asks[0][0] > 0 {
		mid = (ob.Bids[0][0] + ob.Asks[0][0]) / 2.0
		if mid > 0 {
			spreadBps = ((ob.Asks[0][0] - ob.Bids[0][0]) / mid) * 10000.0
		}
	}
	for i := 0; i < len(ob.Bids) && i < levels; i++ {
		if ob.Bids[i][0] <= 0 || ob.Bids[i][1] <= 0 {
			continue
		}
		bidDepth += ob.Bids[i][0] * ob.Bids[i][1]
	}
	for i := 0; i < len(ob.Asks) && i < levels; i++ {
		if ob.Asks[i][0] <= 0 || ob.Asks[i][1] <= 0 {
			continue
		}
		askDepth += ob.Asks[i][0] * ob.Asks[i][1]
	}
	total := bidDepth + askDepth
	if total > 0 {
		imb = (bidDepth - askDepth) / total
	}
	return bidDepth, askDepth, imb, spreadBps, mid
}

func computeOFIDelta(prev, cur topBookSnapshot) float64 {
	db := 0.0
	switch {
	case cur.BidPx > prev.BidPx:
		db = cur.BidSz
	case cur.BidPx < prev.BidPx:
		db = -prev.BidSz
	default:
		db = cur.BidSz - prev.BidSz
	}
	da := 0.0
	switch {
	case cur.AskPx < prev.AskPx:
		da = -cur.AskSz
	case cur.AskPx > prev.AskPx:
		da = prev.AskSz
	default:
		da = prev.AskSz - cur.AskSz
	}
	return db + da
}

func (t *ofiTracker) Update(cur topBookSnapshot, alpha float64) (float64, float64, int) {
	if !t.Init {
		t.Last = cur
		t.Mu = 0
		t.Var = 1e-9
		t.Init = true
		return 0, 0, 0
	}
	raw := computeOFIDelta(t.Last, cur)
	t.Last = cur
	if !t.Init {
		t.Init = true
		t.Mu = raw
		t.Var = 1e-9
	}
	t.Mu = alpha*raw + (1-alpha)*t.Mu
	diff := raw - t.Mu
	t.Var = alpha*(diff*diff) + (1-alpha)*t.Var
	t.Samples++
	sigma := math.Sqrt(math.Max(t.Var, 1e-12))
	return raw, (raw - t.Mu) / sigma, t.Samples
}

func newWatchRuntime(cfg watchConfig, client *aster.Client) *watchRuntime {
	if !cfg.Enable || client == nil {
		return nil
	}
	return &watchRuntime{
		cfg:      cfg,
		client:   client,
		ofi:      map[string]*ofiTracker{},
		flow:     map[string]flowMetrics{},
		walls:    newWallTracker(),
		meta:     map[string]symbolMeta{},
		priority: map[string]operatorSuggestion{},
	}
}

func (w *watchRuntime) SetSnapshot(longInPlay, shortInPlay []inplay.Entry, meta map[string]symbolMeta, extraSymbols []string, priority []operatorSuggestion) {
	if w == nil {
		return
	}
	w.longInPlay = append([]inplay.Entry(nil), longInPlay...)
	w.shortInPlay = append([]inplay.Entry(nil), shortInPlay...)
	w.meta = copySymbolMetaMap(meta)
	w.priority = map[string]operatorSuggestion{}
	for _, s := range priority {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(s.Symbol)))
		if raw == "" {
			continue
		}
		w.priority[raw] = s
	}
	w.symbols = buildWatchSymbols(longInPlay, shortInPlay, extraSymbols, w.cfg.MaxCandidates)
}

func (w *watchRuntime) FlowMetrics() map[string]flowMetrics {
	if w == nil || len(w.flow) == 0 {
		return map[string]flowMetrics{}
	}
	out := make(map[string]flowMetrics, len(w.flow))
	for k, v := range w.flow {
		out[k] = v
	}
	return out
}

func newWallTracker() *wallTracker {
	return &wallTracker{
		bidObs: map[string]wallObservation{},
		askObs: map[string]wallObservation{},
		ctx:    map[string]ta.OBContext{},
		sig:    map[string]wallSignal{},
	}
}

func (wt *wallTracker) update(now time.Time, books map[string]aster.OrderBook, flow map[string]flowMetrics, meta map[string]symbolMeta, levels int) {
	if wt == nil || len(books) == 0 {
		return
	}
	wt.mu.Lock()
	defer wt.mu.Unlock()
	for raw, ob := range books {
		ctx := ta.OrderBookContext(raw, ob.Bids, ob.Asks, levels)
		wt.ctx[raw] = ctx
		bidObs := wt.updateSide(now, raw, "bid", ctx.NearestBidWall)
		askObs := wt.updateSide(now, raw, "ask", ctx.NearestAskWall)
		wt.sig[raw] = buildWallSignal(raw, ctx, bidObs, askObs, flow[raw], meta[raw])
	}
}

func (wt *wallTracker) updateSide(now time.Time, raw, side string, wall *ta.OBWall) wallObservation {
	var store map[string]wallObservation
	if strings.EqualFold(side, "ask") {
		store = wt.askObs
	} else {
		store = wt.bidObs
	}
	prev := store[raw]
	if wall == nil || wall.Price <= 0 || wall.Size <= 0 {
		if !prev.LastSeenAt.IsZero() {
			prev.LastSeenAt = now
			store[raw] = prev
		}
		return prev
	}
	if prev.Price > 0 && math.Abs(prev.Price-wall.Price)/maxFloat(wall.Price, 1e-9) <= 0.0005 {
		if prev.FirstSeenAt.IsZero() {
			prev.FirstSeenAt = now
		}
		if wall.Size > prev.Size*1.02 {
			prev.Adds++
			if prev.Size > 0 && wall.Size > prev.Size*1.10 {
				prev.Refills++
			}
		} else if wall.Size < prev.Size*0.98 {
			prev.Pulls++
		}
		prev.Price = wall.Price
		prev.Size = wall.Size
		prev.SizeRatio = wall.SizeRatio
		prev.LastSeenAt = now
		prev.Samples++
		store[raw] = prev
		return prev
	}
	obs := wallObservation{
		Price:       wall.Price,
		Size:        wall.Size,
		SizeRatio:   wall.SizeRatio,
		FirstSeenAt: now,
		LastSeenAt:  now,
		Samples:     1,
	}
	store[raw] = obs
	return obs
}

func (wt *wallTracker) signalFor(raw string) wallSignal {
	if wt == nil {
		return wallSignal{}
	}
	wt.mu.RLock()
	defer wt.mu.RUnlock()
	return wt.sig[raw]
}

func buildWallSignal(raw string, ctx ta.OBContext, bidObs, askObs wallObservation, fm flowMetrics, meta symbolMeta) wallSignal {
	maxWallDist := envFloat("LIVE_WALL_MAX_DISTANCE_BPS", 18.0)
	minWallRatio := envFloat("LIVE_WALL_MIN_SIZE_RATIO", 2.5)
	minPersistentMs := float64(envInt("LIVE_WALL_MIN_PERSIST_MS", 3000))
	out := wallSignal{
		BidWall: ctx.NearestBidWall,
		AskWall: ctx.NearestAskWall,
	}
	choose := func(side string, wall *ta.OBWall, obs wallObservation) {
		if wall == nil || wall.Price <= 0 || wall.SizeRatio < minWallRatio || wall.DistanceBps > maxWallDist {
			return
		}
		persistence := obs.LastSeenAt.Sub(obs.FirstSeenAt)
		persistMs := persistence.Seconds() * 1000.0
		pullRate := 0.0
		addRate := 0.0
		if obs.Samples > 0 {
			pullRate = float64(obs.Pulls) / float64(obs.Samples)
			addRate = float64(obs.Adds) / float64(obs.Samples)
		}
		spoofRisk := 0.0
		if persistMs < minPersistentMs {
			spoofRisk += 0.45
		}
		spoofRisk += clamp(pullRate*1.5, 0, 0.45)
		if wall.SizeRatio < minWallRatio*1.25 {
			spoofRisk += 0.10
		}
		interaction := clamp(math.Abs(fm.OFIZ)/1.2, 0, 1)
		status := "defended"
		mode := "wall_defense"
		conf := 0.35 + clamp((wall.SizeRatio-minWallRatio)/4.0, 0, 0.25) + clamp((1.0-spoofRisk)*0.25, 0, 0.25)
		bias := 0.0
		reasons := []string{
			fmt.Sprintf("wall_side=%s", side),
			fmt.Sprintf("wall_dist_bps=%.1f", wall.DistanceBps),
			fmt.Sprintf("wall_ratio=%.2f", wall.SizeRatio),
		}
		if strings.EqualFold(side, "bid") {
			bias = clamp((wall.SizeRatio/4.0)+(fm.OFIZ/2.0), -1, 1)
			if fm.OFIZ > envFloat("LIVE_WALL_CONSUMPTION_MIN_OFI_Z", 0.35) && ctx.NearestAskWall != nil && ctx.NearestAskWall.DistanceBps <= maxWallDist {
				status = "absorbing"
				mode = "wall_consumption"
				conf = clamp(conf+0.12+interaction*0.15, 0, 0.95)
				reasons = append(reasons, "ask_wall_under_pressure")
			}
			if spoofRisk >= envFloat("LIVE_WALL_SPOOF_RISK_REJECT", 0.75) || (meta.LastPrice > 0 && meta.LastPrice < wall.Price && pullRate > 0.25) {
				status = "failed"
				mode = "wall_failure"
				conf = clamp(0.35+interaction*0.20, 0, 0.90)
				reasons = append(reasons, "bid_wall_failed")
			}
		} else {
			bias = clamp(-((wall.SizeRatio / 4.0) + (-fm.OFIZ / 2.0)), -1, 1)
			if fm.OFIZ < -envFloat("LIVE_WALL_CONSUMPTION_MIN_OFI_Z", 0.35) && ctx.NearestBidWall != nil && ctx.NearestBidWall.DistanceBps <= maxWallDist {
				status = "absorbing"
				mode = "wall_consumption"
				conf = clamp(conf+0.12+interaction*0.15, 0, 0.95)
				reasons = append(reasons, "bid_wall_under_pressure")
			}
			if spoofRisk >= envFloat("LIVE_WALL_SPOOF_RISK_REJECT", 0.75) || (meta.LastPrice > 0 && meta.LastPrice > wall.Price && pullRate > 0.25) {
				status = "failed"
				mode = "wall_failure"
				conf = clamp(0.35+interaction*0.20, 0, 0.90)
				reasons = append(reasons, "ask_wall_failed")
			}
		}
		if persistMs < minPersistentMs {
			reasons = append(reasons, "wall_not_persistent")
		}
		if pullRate > addRate && pullRate > 0.15 {
			reasons = append(reasons, "wall_pull_dominant")
		}
		if out.Confidence == 0 || math.Abs(bias) > math.Abs(out.BiasScore) {
			out.Mode = mode
			out.Status = status
			out.Confidence = conf
			out.BiasScore = bias
			out.SpoofRisk = clamp(spoofRisk, 0, 1)
			out.DistanceBps = wall.DistanceBps
			out.SizeRatio = wall.SizeRatio
			out.Persistence = persistence
			out.PullRate = pullRate
			out.AddRate = addRate
			out.RefillCount = obs.Refills
			out.Price = wall.Price
			out.Side = side
			out.Reasons = reasons
			out.Interaction = interaction
		}
	}
	choose("bid", ctx.NearestBidWall, bidObs)
	choose("ask", ctx.NearestAskWall, askObs)
	return out
}

func (w *watchRuntime) MetaSnapshot() map[string]symbolMeta {
	if w == nil || len(w.meta) == 0 {
		return map[string]symbolMeta{}
	}
	return copySymbolMetaMap(w.meta)
}

func (w *watchRuntime) WallSignals() map[string]wallSignal {
	if w == nil || w.walls == nil {
		return map[string]wallSignal{}
	}
	w.walls.mu.RLock()
	defer w.walls.mu.RUnlock()
	out := make(map[string]wallSignal, len(w.walls.sig))
	for k, v := range w.walls.sig {
		out[k] = v
	}
	return out
}

func (w *watchRuntime) Tick(now time.Time) bool {
	if w == nil || !w.cfg.Enable || w.client == nil || len(w.symbols) == 0 {
		return false
	}
	books := fetchOrderBooks(w.client, w.symbols, w.cfg.BookLevels)
	if len(books) == 0 {
		return false
	}
	for raw, ob := range books {
		snap, ok := topBookSnapshotFromOrderBook(now, ob)
		if !ok {
			continue
		}
		tr := w.ofi[raw]
		if tr == nil {
			tr = &ofiTracker{}
			w.ofi[raw] = tr
		}
		rawOFI, ofiZ, samples := tr.Update(snap, w.cfg.OFIAlpha)
		bidDepth, askDepth, imb, spreadBps, mid := orderBookDepthStats(ob, w.cfg.BookLevels)
		m := w.flow[raw]
		m.UpdatedAt = now
		m.OFIRaw = rawOFI
		m.OFIZ = ofiZ
		m.OFISamples = samples
		m.SpreadBps = spreadBps
		m.DepthBid = bidDepth
		m.DepthAsk = askDepth
		m.BookImbalance = imb
		m.Mid = mid
		w.flow[raw] = m
		if meta, ok := w.meta[raw]; ok {
			meta.Bid = snap.BidPx
			meta.Ask = snap.AskPx
			if mid > 0 {
				meta.LastPrice = mid
			}
			w.meta[raw] = meta
		}
	}
	if w.walls != nil {
		w.walls.update(now, books, w.flow, w.meta, w.cfg.BookLevels)
	}
	if now.Sub(w.lastUrgentAt) < w.cfg.Every {
		return false
	}
	if w.shouldWake(now) {
		w.lastUrgentAt = now
		return true
	}
	return false
}

func (w *watchRuntime) shouldWake(now time.Time) bool {
	if w == nil || !w.cfg.Enable || len(w.flow) == 0 {
		return false
	}
	priorityReady := func(raw string, side string) bool {
		if len(w.priority) == 0 {
			return false
		}
		s, ok := w.priority[raw]
		if !ok || !strings.EqualFold(strings.TrimSpace(s.Side), strings.TrimSpace(side)) {
			return false
		}
		if !w.lastPriorityAt.IsZero() && now.Sub(w.lastPriorityAt) < w.cfg.PriorityEvery {
			return false
		}
		w.lastPriorityAt = now
		return true
	}
	check := func(e inplay.Entry) bool {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(e.Symbol)))
		wantSide := "BUY"
		if strings.EqualFold(strings.TrimSpace(e.SideBias), "short") {
			wantSide = "SELL"
		}
		if priorityReady(raw, wantSide) {
			return true
		}
		fm, ok := w.flow[raw]
		if !ok || fm.OFISamples < w.cfg.OFIMinSamples {
			return false
		}
		switch strings.TrimSpace(e.EntryStyle) {
		case "momentum_ignite_long":
			return e.Momentum && (e.State == inplay.StateHeating || e.State == inplay.StateInPlay) && e.ScoreSlope >= envFloat("LIVE_IGNITE_MIN_SLOPE", 0.08) && fm.OFIZ >= w.cfg.IgniteMinOFIZ
		case "momentum_ignite_short":
			return e.Momentum && (e.State == inplay.StateHeating || e.State == inplay.StateInPlay) && e.ScoreSlope >= envFloat("LIVE_IGNITE_MIN_SLOPE", 0.08) && fm.OFIZ <= -w.cfg.IgniteMinOFIZ
		case "reversal_watch_short":
			return e.ReversalWatchFlag && (e.State == inplay.StateDumping || e.State == inplay.StateExhausted) && fm.OFIZ <= w.cfg.RevShortMaxOFIZ
		case "reversal_watch_long":
			return e.ReversalWatchFlag && (e.State == inplay.StateBalanced || e.State == inplay.StateHeating || e.State == inplay.StateInPlay) && fm.OFIZ >= w.cfg.RevLongMinOFIZ
		default:
			if e.State == inplay.StateHeating || e.State == inplay.StateInPlay || e.State == inplay.StatePumping {
				if strings.EqualFold(e.SideBias, "short") {
					return fm.OFIZ <= -w.cfg.ContMinOFIZ
				}
				return fm.OFIZ >= w.cfg.ContMinOFIZ
			}
			return false
		}
	}
	for _, e := range w.longInPlay {
		if check(e) {
			return true
		}
	}
	for _, e := range w.shortInPlay {
		if check(e) {
			return true
		}
	}
	return false
}

func newPaperTrader(dryRun bool, reserveUSDT float64, maxOpen int) *paperTrader {
	enabled := dryRun && envBool("LIVE_PAPER_ENABLE", true)
	start := envFloat("LIVE_PAPER_START_BALANCE", 1000)
	if start <= 0 {
		start = 1000
	}
	stopPct := envFloat("LIVE_PAPER_STOP_PCT", 3.0)
	if stopPct <= 0 {
		stopPct = 3.0
	}
	stopMode := strings.ToLower(envStr("LIVE_STOP_MODE", "hybrid"))
	atrLen := envInt("LIVE_ATR_LEN", 14)
	if atrLen < 2 {
		atrLen = 14
	}
	tp1R := envFloat("LIVE_PAPER_TP1_R", 1.20)
	tp2R := envFloat("LIVE_PAPER_TP2_R", 2.50)
	tp3R := envFloat("LIVE_PAPER_TP3_R", 4.00)
	if tp1R <= 0 {
		tp1R = 1.0
	}
	if tp2R < tp1R {
		tp2R = tp1R
	}
	if tp3R < tp2R {
		tp3R = tp2R
	}
	tp1Frac := envFloat("LIVE_PAPER_TP1_FRAC", 0.20)
	tp2Frac := envFloat("LIVE_PAPER_TP2_FRAC", 0.15)
	tp3Frac := envFloat("LIVE_PAPER_TP3_FRAC", 0.15)
	tpRatchetOnly := envBool("LIVE_PAPER_TP_RATCHET_ONLY", envBool("LIVE_TP_RATCHET_ONLY", true))
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
		tp1Frac, tp2Frac, tp3Frac = 0.35, 0.25, 0.20
	} else if sumFrac > 1.0 {
		tp1Frac /= sumFrac
		tp2Frac /= sumFrac
		tp3Frac /= sumFrac
	}
	trailAfterTP := envInt("LIVE_PAPER_TRAIL_AFTER_TP", 3)
	if trailAfterTP < 1 {
		trailAfterTP = 1
	}
	if trailAfterTP > 3 {
		trailAfterTP = 3
	}
	trailStopPct := envFloat("LIVE_PAPER_TRAIL_STOP_PCT", 1.50)
	if trailStopPct <= 0 {
		trailStopPct = 1.50
	}
	trailStopPctTP3 := envFloat("LIVE_PAPER_TRAIL_STOP_PCT_TP3", 3.25)
	if trailStopPctTP3 <= 0 {
		trailStopPctTP3 = trailStopPct
	}
	trailPctMin := envFloat("LIVE_TRAIL_PCT_MIN", 1.0)
	if trailPctMin <= 0 {
		trailPctMin = trailStopPct
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
	paperFundingEnabled := envBool("PAPER_FUNDING_ENABLED", true)
	paperFundingHazardSec := time.Duration(envInt("PAPER_FUNDING_HAZARD_SEC", 15)) * time.Second
	if paperFundingHazardSec < 0 {
		paperFundingHazardSec = 0
	}
	paperFundingSkipNew := time.Duration(envInt("PAPER_FUNDING_SKIP_NEW_POSITIONS_SEC", 45)) * time.Second
	if paperFundingSkipNew < 0 {
		paperFundingSkipNew = 0
	}
	paperFundingInMargin := envBool("PAPER_ALLOW_FUNDING_IN_MARGIN_MODEL", false)
	fundingExitEnable := envBool("LIVE_PAPER_PRE_FUNDING_EXIT_ENABLE", true)
	fundingExitMinAge := time.Duration(envInt("LIVE_PAPER_PRE_FUNDING_EXIT_MIN_AGE_MIN", 90)) * time.Minute
	if fundingExitMinAge < 0 {
		fundingExitMinAge = 0
	}
	fundingExitMaxUpnl := envFloat("LIVE_PAPER_PRE_FUNDING_EXIT_MAX_UPNL", 2.5)
	fundingExitMinMFER := envFloat("LIVE_PAPER_PRE_FUNDING_EXIT_MIN_MFE_R", 1.2)
	if fundingExitMinMFER < 0 {
		fundingExitMinMFER = 0
	}
	if maxOpen <= 0 {
		maxOpen = 1
	}
	locName := envStr("LIVE_REPORT_TZ", "America/Chicago")
	reportLoc, err := time.LoadLocation(locName)
	if err != nil {
		reportLoc = time.Local
	}
	minStopPct := envFloat("LIVE_MIN_STOP_PCT", 0.40)
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
	harvestLock := time.Duration(envInt("LIVE_PAPER_HARVEST_REENTRY_LOCK_MIN", 120)) * time.Minute
	if harvestLock < 0 {
		harvestLock = 0
	}
	harvestMinSlope := envFloat("LIVE_PAPER_HARVEST_REENTRY_MIN_SLOPE", 0.45)
	if harvestMinSlope < 0 {
		harvestMinSlope = 0
	}
	harvestMaxStateMin := envFloat("LIVE_PAPER_HARVEST_REENTRY_MAX_STATE_MIN", 12.0)
	if harvestMaxStateMin < 0 {
		harvestMaxStateMin = 0
	}
	maxTradesPerDay := envInt("LIVE_PAPER_SYMBOL_MAX_TRADES_PER_DAY", 5)
	if maxTradesPerDay < 0 {
		maxTradesPerDay = 0
	}
	slotReplaceEnable := envBool("LIVE_PAPER_SLOT_REPLACE_ENABLE", true)
	slotReplaceMinAge := time.Duration(envInt("LIVE_PAPER_SLOT_REPLACE_MIN_AGE_MIN", 90)) * time.Minute
	if slotReplaceMinAge < 0 {
		slotReplaceMinAge = 0
	}
	slotReplaceMinConf := envFloat("LIVE_PAPER_SLOT_REPLACE_MIN_CONF", 0.66)
	if slotReplaceMinConf < 0 {
		slotReplaceMinConf = 0
	}
	slotReplaceMinSlope := envFloat("LIVE_PAPER_SLOT_REPLACE_MIN_SLOPE", 0.10)
	if slotReplaceMinSlope < 0 {
		slotReplaceMinSlope = 0
	}
	slotReplaceMinScoreGap := envFloat("LIVE_PAPER_SLOT_REPLACE_MIN_SCORE_GAP", 8.0)
	if slotReplaceMinScoreGap < 0 {
		slotReplaceMinScoreGap = 0
	}
	slotReplaceMaxUpnl := envFloat("LIVE_PAPER_SLOT_REPLACE_MAX_UPNL", 4.0)
	slotReplaceMinGrade := envStr("LIVE_PAPER_SLOT_REPLACE_MIN_GRADE", "A")
	openCostMode := strings.ToLower(envStr("LIVE_PAPER_OPEN_COST_MODE", "aster"))
	stressRoundtripBps := envFloat("PAPER_STRESS_BPS_ROUNDTRIP", 0)
	if stressRoundtripBps < 0 {
		stressRoundtripBps = 0
	}
	riskOnMargin := envBool("LIVE_RISK_ON_MARGIN_ENABLE", true)
	riskMarginPct := envFloat("LIVE_RISK_MARGIN_PCT", 5.0)
	if riskMarginPct < 0 {
		riskMarginPct = 0
	}
	hybridStopCfg := loadHybridStopConfig()
	stopTriggerRef := strings.ToLower(envStr("LIVE_STOP_TRIGGER_REF", "mark"))
	tpTriggerRef := strings.ToLower(envStr("LIVE_TP_TRIGGER_REF", "mark"))
	markLastModel := strings.ToLower(envStr("PAPER_MARK_LAST_DIVERGENCE_MODEL", "orderbook_mid"))
	markLastDivBps := envFloat("PAPER_MARK_LAST_DIVERGENCE_BPS", 0)
	partialFillEnable := envBool("PAPER_PARTIAL_FILL_ENABLE", true)
	partialFillMinFrac := envFloat("PAPER_PARTIAL_FILL_MIN_FRAC", 0.35)
	stopMarketSlipBps := envFloat("PAPER_STOP_MARKET_ADVERSE_BPS", 8.0)
	p := &paperTrader{
		enabled:                enabled,
		startBal:               start,
		balance:                start,
		reserve:                reserveUSDT,
		feeBps:                 feeBps,
		makerFeeBps:            makerFeeBps,
		takerFeeBps:            takerFeeBps,
		stopMode:               stopMode,
		atrLen:                 atrLen,
		stopPct:                stopPct,
		tp1R:                   tp1R,
		tp2R:                   tp2R,
		tp3R:                   tp3R,
		tp1Frac:                tp1Frac,
		tp2Frac:                tp2Frac,
		tp3Frac:                tp3Frac,
		tpRatchetOnly:          tpRatchetOnly,
		trailAfterTP:           trailAfterTP,
		trailStopPct:           trailStopPct,
		trailStopPctTP3:        trailStopPctTP3,
		trailPctMin:            trailPctMin,
		stateFile:              envStr("LIVE_PAPER_STATE_FILE", "out/paper_state.json"),
		tradesCSV:              resolveStatePath(envStr("LIVE_PAPER_TRADES_FILE", "out/paper_trades.csv")),
		closedTradesJSONL:      resolveStatePath(envStr("LIVE_PAPER_CLOSED_TRADES_FILE", "out/paper_closed_trades.jsonl")),
		equityCSV:              resolveStatePath(envStr("LIVE_PAPER_EQUITY_FILE", "out/paper_equity.csv")),
		maxOpen:                maxOpen,
		positions:              map[string]*paperPosition{},
		closedTradeLedger:      map[string]paperClosedTradeRecord{},
		postExitTrackers:       map[string]*paperPostExitTracker{},
		reportLoc:              reportLoc,
		dayStats:               map[string]*paperDayStats{},
		minStopPct:             minStopPct,
		maxStopPct:             maxStopPct,
		minTP1RR:               minTP1RR,
		beLockBps:              beLockBps,
		fundingEvery:           fundingEvery,
		fundingBySym:           fundingBySym,
		fundingEnabled:         paperFundingEnabled,
		fundingHazardSec:       paperFundingHazardSec,
		fundingSkipNewPos:      paperFundingSkipNew,
		allowFundingInMargin:   paperFundingInMargin,
		fundingExitEnable:      fundingExitEnable,
		fundingExitMinAge:      fundingExitMinAge,
		fundingExitMaxUpnl:     fundingExitMaxUpnl,
		fundingExitMinMFER:     fundingExitMinMFER,
		lastFundKey:            map[string]string{},
		openCostMode:           openCostMode,
		hybridStopCfg:          hybridStopCfg,
		lossCooldown:           lossCooldown,
		lastExitAt:             map[string]time.Time{},
		lastExitLoss:           map[string]bool{},
		lastHarvestAt:          map[string]time.Time{},
		symbolTradeDay:         map[string]string{},
		symbolTradeCount:       map[string]int{},
		lossStreak:             map[string]int{},
		lockUntil:              map[string]time.Time{},
		lossDay:                map[string]string{},
		lossDayCount:           map[string]int{},
		recentLosses:           map[string][]time.Time{},
		setupLockUntil:         map[string]time.Time{},
		setupLosses:            map[string][]time.Time{},
		maxLossStreak:          maxLossStreak,
		lossLock:               lossLock,
		harvestLock:            harvestLock,
		harvestMinSlope:        harvestMinSlope,
		harvestMaxStateMin:     harvestMaxStateMin,
		maxTradesPerDay:        maxTradesPerDay,
		slotReplaceEnable:      slotReplaceEnable,
		slotReplaceMinAge:      slotReplaceMinAge,
		slotReplaceMinConf:     slotReplaceMinConf,
		slotReplaceMinSlope:    slotReplaceMinSlope,
		slotReplaceMinScoreGap: slotReplaceMinScoreGap,
		slotReplaceMaxUpnl:     slotReplaceMaxUpnl,
		slotReplaceMinGrade:    slotReplaceMinGrade,
		stressRoundtripBps:     stressRoundtripBps,
		exitManager: exitmgr.NewManager(exitmgr.Config{
			FrontRunPct:            envFloat("LIVE_TP_FRONT_RUN_PCT", 0.001),
			NoFollowThroughBars:    envInt("LIVE_EXIT_NO_FT_BARS", 36),
			NoFollowThroughMinMFER: envFloat("LIVE_EXIT_NO_FT_MIN_MFE_R", 1.00),
			NoFollowThroughMinMAER: envFloat("LIVE_EXIT_NO_FT_MIN_MAE_R", 0.80),
			ProfitLockArmR:         envFloat("LIVE_EXIT_PROFIT_LOCK_ARM_R", 1.40),
			ProfitGivebackPct:      envFloat("LIVE_EXIT_PROFIT_GIVEBACK_PCT", 0.55),
			SponsoredGivebackPct:   envFloat("LIVE_EXIT_SPONSOR_GIVEBACK_PCT", 0.28),
			WeakFlowArmBER:         envFloat("LIVE_EXIT_WEAK_FLOW_BE_R", 1.45),
			LiqSpikePartialPct:     envFloat("LIVE_EXIT_LIQ_SPIKE_PARTIAL_PCT", 0.35),
			StallBarsForTighten:    envInt("LIVE_EXIT_STALL_BARS", 3),
			StallTightenToR:        envFloat("LIVE_EXIT_STALL_TIGHTEN_TO_R", 0.40),
			SponsorshipGraceMin:    envInt("LIVE_EXIT_SPONSOR_FADE_HOLD_MIN", 120),
			UnsponsoredTightenR:    envFloat("LIVE_EXIT_UNSPONSORED_TIGHTEN_R", 0.18),
			UnsponsoredWeakStreak:  envInt("LIVE_EXIT_UNSPONSORED_WEAK_STREAK", 2),
			TightenAfterConfirm:    envBool("LIVE_MOMENTUM_FADE_TIGHTEN_AFTER_CONFIRM", true),
			RequireStructureLoss:   envBool("LIVE_MOMENTUM_FADE_REQUIRE_STRUCTURE_LOSS_AFTER_CONFIRM", true),
			ProfitLockTightenR:     envFloat("LIVE_PROFIT_LOCK_TIGHTEN_R", 0.35),
		}),
		riskOnMargin:       riskOnMargin,
		riskMarginPct:      riskMarginPct,
		stopTriggerRef:     stopTriggerRef,
		tpTriggerRef:       tpTriggerRef,
		markLastModel:      markLastModel,
		markLastDivBps:     markLastDivBps,
		partialFillEnable:  partialFillEnable,
		partialFillMinFrac: partialFillMinFrac,
		stopMarketSlipBps:  stopMarketSlipBps,
		htf1HBySymbol:      map[string]HTFStructureSnapshot{},
	}
	p.stateFile = resolveStatePath(p.stateFile)
	if p.enabled {
		if err := p.load(); err != nil {
			fmt.Printf("live: paper state load warning: %v\n", err)
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
	if st.ClosedTrades != nil {
		p.closedTradeLedger = st.ClosedTrades
	}
	if st.PostExitTrackers != nil {
		p.postExitTrackers = st.PostExitTrackers
	}
	if st.DayStats != nil {
		p.dayStats = st.DayStats
	}
	if st.GovernorRecords != nil {
		p.governorRecords = trimExecutionGovernorRecords(st.GovernorRecords, time.Now().UTC())
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
	if st.LastHarvest != nil {
		p.lastHarvestAt = st.LastHarvest
	}
	if st.SymbolTradeDay != nil {
		p.symbolTradeDay = st.SymbolTradeDay
	}
	if st.SymbolTradeCount != nil {
		p.symbolTradeCount = st.SymbolTradeCount
	}
	if st.LossStreak != nil {
		p.lossStreak = st.LossStreak
	}
	if st.LockUntil != nil {
		p.lockUntil = st.LockUntil
	}
	if st.LossDay != nil {
		p.lossDay = st.LossDay
	}
	if st.LossDayCount != nil {
		p.lossDayCount = st.LossDayCount
	}
	if st.RecentLosses != nil {
		p.recentLosses = st.RecentLosses
	}
	if st.SetupLockUntil != nil {
		p.setupLockUntil = st.SetupLockUntil
	}
	if st.SetupLosses != nil {
		p.setupLosses = st.SetupLosses
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
		StartBal:         p.startBal,
		Balance:          p.balance,
		Reserve:          p.reserve,
		Positions:        p.positions,
		ClosedTrades:     p.closedTradeLedger,
		PostExitTrackers: p.postExitTrackers,
		GovernorRecords:  trimExecutionGovernorRecords(p.governorRecords, time.Now().UTC()),
		DayStats:         p.dayStats,
		LastFund:         p.lastFundKey,
		LastExitAt:       p.lastExitAt,
		LastExitLoss:     p.lastExitLoss,
		LastHarvest:      p.lastHarvestAt,
		SymbolTradeDay:   p.symbolTradeDay,
		SymbolTradeCount: p.symbolTradeCount,
		LossStreak:       p.lossStreak,
		LockUntil:        p.lockUntil,
		LossDay:          p.lossDay,
		LossDayCount:     p.lossDayCount,
		RecentLosses:     p.recentLosses,
		SetupLockUntil:   p.setupLockUntil,
		SetupLosses:      p.setupLosses,
		UpdatedAt:        time.Now().UTC(),
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p.stateFile, b, 0o644); err != nil {
		return err
	}
	return p.writeClosedTradesJSONL()
}

func (p *paperTrader) writeClosedTradesJSONL() error {
	if p == nil || !p.enabled || strings.TrimSpace(p.closedTradesJSONL) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.closedTradesJSONL), 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(p.closedTradeLedger))
	for k := range p.closedTradeLedger {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	f, err := os.Create(p.closedTradesJSONL)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, k := range keys {
		if err := enc.Encode(p.closedTradeLedger[k]); err != nil {
			return err
		}
	}
	return nil
}

func newLiveExecManager(rest *aster.RESTAuth, tg *notify.Telegram) *liveExecManager {
	if rest == nil {
		return nil
	}
	tradeMargin := envFloat("LIVE_TRADE_MARGIN_USDT", 20)
	ladderCfg := loadLadderConfig(tradeMargin)
	fundsCfg := loadFundsManagerConfig()
	reentryCfg := loadReentryConfig(ladderCfg.StarterUSDT)
	postWinCooldownCfg := loadPostWinCooldownConfig()
	stopPct := envFloat("LIVE_STOP_PCT", 3.0)
	if stopPct <= 0 {
		stopPct = 3.0
	}
	stopMode := strings.ToLower(envStr("LIVE_STOP_MODE", "hybrid"))
	atrLen := envInt("LIVE_ATR_LEN", 14)
	if atrLen < 2 {
		atrLen = 14
	}
	tp1R := envFloat("LIVE_TP1_R", 1.20)
	tp2R := envFloat("LIVE_TP2_R", 2.50)
	tp3R := envFloat("LIVE_TP3_R", 4.00)
	if tp1R <= 0 {
		tp1R = 1.0
	}
	if tp2R < tp1R {
		tp2R = tp1R
	}
	if tp3R < tp2R {
		tp3R = tp2R
	}
	tp1Frac := envFloat("LIVE_TP1_FRAC", 0.20)
	tp2Frac := envFloat("LIVE_TP2_FRAC", 0.15)
	tp3Frac := envFloat("LIVE_TP3_FRAC", 0.15)
	tpRatchetOnly := envBool("LIVE_TP_RATCHET_ONLY", true)
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
		tp1Frac, tp2Frac, tp3Frac = 0.35, 0.25, 0.20
	} else if sumFrac > 1.0 {
		tp1Frac /= sumFrac
		tp2Frac /= sumFrac
		tp3Frac /= sumFrac
	}
	trailAfterTP := envInt("LIVE_TRAIL_AFTER_TP", 3)
	if trailAfterTP < 1 {
		trailAfterTP = 1
	}
	if trailAfterTP > 3 {
		trailAfterTP = 3
	}
	trailStopPct := envFloat("LIVE_TRAIL_STOP_PCT", 1.50)
	if trailStopPct <= 0 {
		trailStopPct = 1.50
	}
	trailStopPctTP3 := envFloat("LIVE_TRAIL_STOP_PCT_TP3", 3.25)
	if trailStopPctTP3 <= 0 {
		trailStopPctTP3 = trailStopPct
	}
	trailPctMin := envFloat("LIVE_TRAIL_PCT_MIN", 1.0)
	if trailPctMin <= 0 {
		trailPctMin = trailStopPct
	}
	trailStepBps := envFloat("LIVE_TRAIL_STEP_BPS", 10.0)
	if trailStepBps < 0 {
		trailStepBps = 0
	}
	minStopPct := envFloat("LIVE_MIN_STOP_PCT", 0.40)
	maxStopPct := envFloat("LIVE_MAX_STOP_PCT", 8.0)
	if maxStopPct < minStopPct {
		maxStopPct = minStopPct
	}
	minTP1RR := envFloat("LIVE_MIN_RR_TP1", 0.8)
	if minTP1RR <= 0 {
		minTP1RR = 0.8
	}
	beLockBps := envFloat("LIVE_BE_LOCK_BPS", 5)
	riskOnMargin := envBool("LIVE_RISK_ON_MARGIN_ENABLE", true)
	riskMarginPct := envFloat("LIVE_RISK_MARGIN_PCT", 5.0)
	if riskMarginPct < 0 {
		riskMarginPct = 0
	}
	hybridStopCfg := loadHybridStopConfig()
	accountReportCfg := loadAccountReportConfig()
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
	fundingEvery := time.Duration(envInt("LIVE_FUNDING_INTERVAL_MIN", 480)) * time.Minute
	if fundingEvery <= 0 {
		fundingEvery = 480 * time.Minute
	}
	fundingBySym := parseSymbolMinutesMap(envStr("LIVE_FUNDING_INTERVALS", ""))
	fundingHazardSec := time.Duration(envInt("LIVE_FUNDING_HAZARD_SEC", 15)) * time.Second
	if fundingHazardSec < 0 {
		fundingHazardSec = 0
	}
	fundingSkipNewPos := time.Duration(envInt("LIVE_FUNDING_SKIP_NEW_POSITIONS_SEC", 45)) * time.Second
	if fundingSkipNewPos < 0 {
		fundingSkipNewPos = 0
	}
	fundingExitMinAge := time.Duration(envInt("LIVE_PRE_FUNDING_EXIT_MIN_AGE_MIN", 90)) * time.Minute
	if fundingExitMinAge < 0 {
		fundingExitMinAge = 0
	}
	fundingExitWindow := time.Duration(envInt("LIVE_PRE_FUNDING_EXIT_WINDOW_MIN", 30)) * time.Minute
	if fundingExitWindow < 0 {
		fundingExitWindow = 0
	}
	stopTriggerRef := strings.ToLower(envStr("LIVE_STOP_TRIGGER_REF", "mark"))
	tpTriggerRef := strings.ToLower(envStr("LIVE_TP_TRIGGER_REF", "mark"))
	snapshotPoll := accountReportCfg.RefreshEvery
	if snapshotPoll <= 0 {
		snapshotPoll = 30 * time.Second
	}
	remoteImportEvery := time.Duration(envInt("LIVE_REMOTE_IMPORT_SEC", 30)) * time.Second
	if remoteImportEvery <= 0 {
		remoteImportEvery = 30 * time.Second
	}
	wsLevels := envInt("LIVE_ACCOUNT_WS_LEVELS", 20)
	if wsLevels != 5 && wsLevels != 10 && wsLevels != 20 {
		wsLevels = 20
	}
	wsSpeed := strings.TrimSpace(envStr("LIVE_ACCOUNT_WS_SPEED", "100ms"))
	accountAssets := envCSV("LIVE_ACCOUNT_ASSETS", "")
	enableUserDataStream := envBool("LIVE_USERDATA_STREAM_ENABLE", true)

	m := &liveExecManager{
		rest:                 rest,
		tg:                   tg,
		path:                 resolveStatePath(envStr("LIVE_STATE_FILE", "out/live_exec_state.json")),
		tradesCSV:            resolveStatePath(envStr("LIVE_TRADES_FILE", "out/live_trades.csv")),
		fillReceipt:          envBool("LIVE_TG_FILL_RECEIPT_ENABLE", true),
		entryTimeout:         time.Duration(envInt("LIVE_ENTRY_TIMEOUT_SEC", 90)) * time.Second,
		stopMode:             stopMode,
		atrLen:               atrLen,
		stopPct:              stopPct,
		tp1R:                 tp1R,
		tp2R:                 tp2R,
		tp3R:                 tp3R,
		tp1Frac:              tp1Frac,
		tp2Frac:              tp2Frac,
		tp3Frac:              tp3Frac,
		tpRatchetOnly:        tpRatchetOnly,
		trailAfterTP:         trailAfterTP,
		trailStopPct:         trailStopPct,
		trailStopPctTP3:      trailStopPctTP3,
		trailPctMin:          trailPctMin,
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
		riskOnMargin:         riskOnMargin,
		riskMarginPct:        riskMarginPct,
		hybridStopCfg:        hybridStopCfg,
		fundingEvery:         fundingEvery,
		fundingBySym:         fundingBySym,
		fundingHazardSec:     fundingHazardSec,
		fundingSkipNewPos:    fundingSkipNewPos,
		fundingExitEnable:    envBool("LIVE_PRE_FUNDING_EXIT_ENABLE", true),
		fundingExitMinAge:    fundingExitMinAge,
		fundingExitMaxUpnl:   envFloat("LIVE_PRE_FUNDING_EXIT_MAX_UPNL", 2.5),
		fundingExitMinMFER:   envFloat("LIVE_PRE_FUNDING_EXIT_MIN_MFE_R", 1.2),
		fundingExitWindow:    fundingExitWindow,
		expensiveFundingRate: envFloat("LIVE_PRE_FUNDING_EXPENSIVE_RATE", 0.0008),
		stopTriggerRef:       stopTriggerRef,
		tpTriggerRef:         tpTriggerRef,
		accountAssets:        accountAssets,
		snapshotPoll:         snapshotPoll,
		remoteImportEvery:    remoteImportEvery,
		wsLevels:             wsLevels,
		wsSpeed:              wsSpeed,
		marketStates:         map[string]*aster.MarketState{},
		marketCancels:        map[string]context.CancelFunc{},
		manualConfirm:        envBool("LIVE_MANUAL_CONFIRM_ENABLE", true),
		manualRequests:       map[string]manualManageRequest{},
		legalityFailCount:    map[string]int{},
		symbolQuarantineTill: map[string]time.Time{},
		unknownExecGuards:    map[string]UnknownExecutionGuard{},
		htf1HBySymbol:        map[string]HTFStructureSnapshot{},
		ladderCfg:            ladderCfg,
		fundsCfg:             fundsCfg,
		reentryCfg:           reentryCfg,
		postWinCooldownCfg:   postWinCooldownCfg,
		transferManager:      newTransferManager(rest),
		accountReportCfg:     accountReportCfg,
		exitManager: exitmgr.NewManager(exitmgr.Config{
			FrontRunPct:            envFloat("LIVE_TP_FRONT_RUN_PCT", 0.001),
			NoFollowThroughBars:    envInt("LIVE_EXIT_NO_FT_BARS", 36),
			NoFollowThroughMinMFER: envFloat("LIVE_EXIT_NO_FT_MIN_MFE_R", 1.00),
			NoFollowThroughMinMAER: envFloat("LIVE_EXIT_NO_FT_MIN_MAE_R", 0.80),
			ProfitLockArmR:         envFloat("LIVE_EXIT_PROFIT_LOCK_ARM_R", 1.40),
			ProfitGivebackPct:      envFloat("LIVE_EXIT_PROFIT_GIVEBACK_PCT", 0.55),
			SponsoredGivebackPct:   envFloat("LIVE_EXIT_SPONSOR_GIVEBACK_PCT", 0.28),
			WeakFlowArmBER:         envFloat("LIVE_EXIT_WEAK_FLOW_BE_R", 1.45),
			LiqSpikePartialPct:     envFloat("LIVE_EXIT_LIQ_SPIKE_PARTIAL_PCT", 0.35),
			StallBarsForTighten:    envInt("LIVE_EXIT_STALL_BARS", 3),
			StallTightenToR:        envFloat("LIVE_EXIT_STALL_TIGHTEN_TO_R", 0.40),
			SponsorshipGraceMin:    envInt("LIVE_EXIT_SPONSOR_FADE_HOLD_MIN", 120),
			UnsponsoredTightenR:    envFloat("LIVE_EXIT_UNSPONSORED_TIGHTEN_R", 0.18),
			UnsponsoredWeakStreak:  envInt("LIVE_EXIT_UNSPONSORED_WEAK_STREAK", 2),
			TightenAfterConfirm:    envBool("LIVE_MOMENTUM_FADE_TIGHTEN_AFTER_CONFIRM", true),
			RequireStructureLoss:   envBool("LIVE_MOMENTUM_FADE_REQUIRE_STRUCTURE_LOSS_AFTER_CONFIRM", true),
			ProfitLockTightenR:     envFloat("LIVE_PROFIT_LOCK_TIGHTEN_R", 0.35),
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
	for _, p := range m.positions {
		if p == nil {
			continue
		}
		if p.DeployedMargin <= 0 {
			p.DeployedMargin = maxFloat(p.Margin, p.DeployedMargin)
		}
		if p.EntrySource == "" {
			p.EntrySource = "BOT"
		}
	}
	if enableUserDataStream {
		state := aster.NewUserDataState()
		ctx, cancel := context.WithCancel(context.Background())
		m.userDataState = state
		m.userDataCancel = cancel
		client := aster.NewUserDataStreamClient(rest, state)
		go func() {
			_ = client.Run(ctx)
		}()
	}
	go m.runLiveAccountSnapshotLoop()
	go m.runAccountReportingLoop()
	return m
}

func manualManageFingerprint(symbol, side string, qty, entry float64) string {
	return positionLookupKey(symbol, side)
}

func (m *liveExecManager) queueManualManagementRequest(symbol, side string, qty, entry, margin float64, lev int, now time.Time) bool {
	if m == nil || !m.manualConfirm {
		return false
	}
	key := positionLookupKey(symbol, side)
	req := manualManageRequest{
		Key:         key,
		Fingerprint: manualManageFingerprint(symbol, side, qty, entry),
		Symbol:      strings.ToUpper(strings.TrimSpace(symbol)),
		Side:        normalizePositionSide(side),
		Qty:         qty,
		Entry:       entry,
		Margin:      margin,
		Leverage:    maxInt(1, lev),
		Action:      "MANAGE",
		DetectedAt:  now,
		PromptedAt:  now,
		Status:      manualRequestPending,
	}
	m.mu.Lock()
	existing, ok := m.manualRequests[key]
	switch {
	case ok && (existing.Status == manualRequestPending || existing.Status == manualRequestDeclined):
		existing.Fingerprint = req.Fingerprint
		existing.Qty = req.Qty
		existing.Entry = req.Entry
		existing.Margin = req.Margin
		existing.Leverage = req.Leverage
		existing.DetectedAt = now
		m.manualRequests[key] = existing
		m.mu.Unlock()
		return true
	case ok && (existing.Status == manualRequestApproved || existing.Status == manualRequestPassive):
		existing.Fingerprint = req.Fingerprint
		existing.Qty = req.Qty
		existing.Entry = req.Entry
		existing.Margin = req.Margin
		existing.Leverage = req.Leverage
		existing.DetectedAt = now
		m.manualRequests[key] = existing
		m.mu.Unlock()
		return false
	default:
		m.manualRequests[key] = req
		m.mu.Unlock()
	}
	if m.tg != nil {
		m.tg.Sendf("%s", notify.BuildManagementStatusCard(notify.ManageStateDetected, req.Symbol, req.Side,
			fmt.Sprintf("<b>%s %s</b>", req.Symbol, displayPositionSide(req.Side)),
			fmt.Sprintf("<b>Qty:</b> %.6f | <b>Entry:</b> %s | <b>Lev:</b> %dx", req.Qty, fmtPrice(req.Entry), req.Leverage),
			"Let the bot manage this trade?",
			"<b>Status:</b> awaiting operator approval",
			fmt.Sprintf("Reply <code>/manage %s y</code> or <code>/manage %s n</code>", req.Symbol, req.Symbol),
			"If there is only one pending manual trade, you can also just reply <code>y</code> or <code>n</code>.",
		))
	}
	return true
}

func (m *liveExecManager) queueManualForceFlatRequest(req manualManageRequest, cause string, now time.Time) {
	if m == nil || !m.manualConfirm {
		return
	}
	req.Action = "FORCE_FLAT"
	req.Failure = strings.TrimSpace(cause)
	req.PromptedAt = now
	req.DecidedAt = time.Time{}
	req.Status = manualRequestPending
	m.mu.Lock()
	m.manualRequests[req.Key] = req
	m.mu.Unlock()
	if m.tg != nil {
		lines := []string{
			fmt.Sprintf("<b>%s %s</b>", req.Symbol, displayPositionSide(req.Side)),
			fmt.Sprintf("<b>Qty:</b> %.6f | <b>Entry:</b> %s | <b>Lev:</b> %dx", req.Qty, fmtPrice(req.Entry), req.Leverage),
			"<b>Bot management could not attach protection.</b>",
		}
		if req.Failure != "" {
			lines = append(lines, fmt.Sprintf("<b>Failure:</b> %s", summarizeOneLine(req.Failure, 140)))
		}
		lines = append(lines,
			"Force close this manual trade?",
			fmt.Sprintf("Reply <code>/manage %s y</code> or <code>/manage %s n</code>", req.Symbol, req.Symbol),
		)
		m.tg.Sendf("%s", notify.BuildManagementStatusCard(notify.ManageStateAwaitingOperator, req.Symbol, req.Side, lines...))
	}
}

func manualRequestTimeout(req manualManageRequest) time.Duration {
	if strings.EqualFold(strings.TrimSpace(req.Action), "MANAGE") {
		return 2 * time.Minute
	}
	if strings.EqualFold(strings.TrimSpace(req.Action), "FORCE_FLAT") {
		return 2 * time.Minute
	}
	return 0
}

func (m *liveExecManager) expirePendingManualRequests(now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, req := range m.manualRequests {
		if req.Status != manualRequestPending || req.PromptedAt.IsZero() {
			continue
		}
		timeout := manualRequestTimeout(req)
		if timeout <= 0 || now.Sub(req.PromptedAt) < timeout {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(req.Action), "MANAGE") {
			req.Status = manualRequestPassive
		} else {
			req.Status = manualRequestDeclined
		}
		req.DecidedAt = now
		m.manualRequests[key] = req
	}
}

func (m *liveExecManager) pendingManualRequests(limit int) []manualManageRequest {
	if m == nil {
		return nil
	}
	m.expirePendingManualRequests(time.Now().UTC())
	m.mu.RLock()
	out := make([]manualManageRequest, 0, len(m.manualRequests))
	for _, req := range m.manualRequests {
		if req.Status != manualRequestPending {
			continue
		}
		out = append(out, req)
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return out[i].DetectedAt.Before(out[j].DetectedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (m *liveExecManager) pendingManualRequest(symbol string) (manualManageRequest, bool) {
	if m == nil {
		return manualManageRequest{}, false
	}
	m.expirePendingManualRequests(time.Now().UTC())
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	base := canonicalSymbolBase(raw)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, req := range m.manualRequests {
		if req.Status != manualRequestPending {
			continue
		}
		if canonicalSymbolBase(req.Symbol) == base {
			return req, true
		}
	}
	return manualManageRequest{}, false
}

func (m *liveExecManager) passiveManualPositionBySymbol(symbol string) (*livePosition, bool) {
	if m == nil {
		return nil, false
	}
	base := canonicalSymbolBase(strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol))))
	if base == "" {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.positions {
		if p == nil || !m.isActive(p) || !manualPassivePosition(p) {
			continue
		}
		if canonicalSymbolBase(p.Symbol) == base {
			return p, true
		}
	}
	return nil, false
}

func (m *liveExecManager) managedManualPositionBySymbol(symbol string) (*livePosition, bool) {
	if m == nil {
		return nil, false
	}
	base := canonicalSymbolBase(strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol))))
	if base == "" {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.positions {
		if p == nil || !m.isActive(p) || !manualManagedTrade(p) {
			continue
		}
		if canonicalSymbolBase(p.Symbol) == base {
			return p, true
		}
	}
	return nil, false
}

func (m *liveExecManager) deactivateManualManagement(symbol string, now time.Time) (*livePosition, bool) {
	if m == nil {
		return nil, false
	}
	base := canonicalSymbolBase(strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol))))
	if base == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.positions {
		if p == nil || !m.isActive(p) || !manualManagedTrade(p) {
			continue
		}
		if canonicalSymbolBase(p.Symbol) != base {
			continue
		}
		p.EntryReason = manualEntryReasonPassive
		p.EntrySource = manualEntrySourcePassive
		p.ManualManageState = manualManageStatePassive
		p.Managed = false
		p.ProtectionPending = false
		p.ProtectionRetryAfter = time.Time{}
		p.ProtectionRetryCount = 0
		p.ProtectionFailCount = 0
		p.ForceProtectionNow = false
		p.ManageFailSuppressCount = 0
		p.LastManageFailAt = time.Time{}
		p.LastManageFailCause = ""
		p.LastManageNotifyState = ""
		p.LastManageNotifyCause = ""
		p.LastManageNotifyAt = time.Time{}
		p.Protected = hasLiveProtectiveOrder(p)
		p.UpdatedAt = now
		return p, true
	}
	return nil, false
}

func manualManageRequestFromPosition(p *livePosition) manualManageRequest {
	if p == nil {
		return manualManageRequest{}
	}
	return manualManageRequest{
		Key:         positionLookupKey(p.Symbol, p.Side),
		Fingerprint: manualManageFingerprint(p.Symbol, p.Side, p.RemainingQty, p.EntryPrice),
		Symbol:      strings.ToUpper(strings.TrimSpace(p.Symbol)),
		Side:        normalizePositionSide(p.Side),
		Qty:         maxFloat(p.RemainingQty, p.FilledQty),
		Entry:       p.EntryPrice,
		Margin:      p.Margin,
		Leverage:    maxInt(1, p.Leverage),
		Action:      "MANAGE",
		Status:      manualRequestApproved,
	}
}

func (m *liveExecManager) approvedManualRequest(symbol, side string, qty, entry float64) (manualManageRequest, bool) {
	if m == nil {
		return manualManageRequest{}, false
	}
	key := positionLookupKey(symbol, side)
	m.mu.RLock()
	defer m.mu.RUnlock()
	req, ok := m.manualRequests[key]
	if !ok || req.Status != manualRequestApproved {
		return manualManageRequest{}, false
	}
	return req, true
}

func (m *liveExecManager) passiveManualRequest(symbol, side string, qty, entry float64) (manualManageRequest, bool) {
	if m == nil {
		return manualManageRequest{}, false
	}
	key := positionLookupKey(symbol, side)
	m.mu.RLock()
	defer m.mu.RUnlock()
	req, ok := m.manualRequests[key]
	if !ok || req.Status != manualRequestPassive {
		return manualManageRequest{}, false
	}
	return req, true
}

func (m *liveExecManager) markManualRequestApproved(req manualManageRequest, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.manualRequests[req.Key]
	if !ok {
		return
	}
	cur.Status = manualRequestApproved
	cur.DecidedAt = now
	m.manualRequests[req.Key] = cur
}

func (m *liveExecManager) markManualRequestPassive(req manualManageRequest, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.manualRequests[req.Key]
	if !ok {
		return
	}
	cur.Status = manualRequestPassive
	cur.DecidedAt = now
	m.manualRequests[req.Key] = cur
}

func (m *liveExecManager) markManualRequestDeclined(req manualManageRequest, now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.manualRequests[req.Key]
	if !ok {
		return
	}
	cur.Status = manualRequestDeclined
	cur.DecidedAt = now
	m.manualRequests[req.Key] = cur
}

func (m *liveExecManager) pruneManualRequests(remoteKeys map[string]string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	activeKeys := map[string]*livePosition{}
	for sym, pos := range m.positions {
		if m.isActive(pos) {
			activeKeys[positionLookupKey(sym, pos.Side)] = pos
		}
	}
	for key, req := range m.manualRequests {
		if pos, ok := activeKeys[positionLookupKey(req.Symbol, req.Side)]; ok {
			if manualPassivePosition(pos) && req.Status == manualRequestPending {
				continue
			}
			delete(m.manualRequests, key)
			continue
		}
		fp, ok := remoteKeys[key]
		if !ok || fp != req.Fingerprint {
			delete(m.manualRequests, key)
		}
	}
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
	m.governorRecords = trimExecutionGovernorRecords(st.GovernorRecords, time.Now().UTC())
	return nil
}

func (m *liveExecManager) save() error {
	if m == nil || m.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	st := liveExecStore{
		Positions:       m.positions,
		GovernorRecords: trimExecutionGovernorRecords(m.governorRecords, time.Now().UTC()),
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, b, 0o644)
}

func (m *liveExecManager) trackedPosition(symbol string) (*livePosition, bool) {
	if m == nil {
		return nil, false
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if raw == "" {
		return nil, false
	}
	p, ok := m.positions[raw]
	if !ok || p == nil {
		return nil, false
	}
	return p, true
}

func (m *liveExecManager) EnsureEntryFunds(required, currentAvail float64) (float64, bool) {
	if m == nil || !m.fundsCfg.Enable || required <= 0 || currentAvail >= required {
		return currentAvail, false
	}
	if m.transferManager == nil || !m.transferManager.Supported() {
		m.recordTransferStatus(fmt.Sprintf("topup unavailable required=%.2f avail=%.2f", required, currentAvail))
		fmt.Printf("live: funds manager top-up unavailable required=%.2f avail=%.2f reason=transfer_unsupported\n", required, currentAvail)
		return currentAvail, false
	}
	shortfall := required - currentAvail
	if shortfall <= 0 {
		return currentAvail, false
	}
	transferAmt := shortfall
	if targetGap := m.fundsCfg.PerpTargetUSDT - currentAvail; targetGap > transferAmt {
		transferAmt = targetGap
	}
	if transferAmt < m.fundsCfg.TopupMinUSDT {
		m.recordTransferStatus(fmt.Sprintf("topup skipped amount=%.2f min=%.2f", transferAmt, m.fundsCfg.TopupMinUSDT))
		fmt.Printf("live: funds manager top-up skipped amount=%.2f below_min=%.2f\n", transferAmt, m.fundsCfg.TopupMinUSDT)
		return currentAvail, false
	}
	spotAvail, err := m.transferManager.SpotAvailableUSDT()
	if err != nil {
		m.recordTransferStatus(fmt.Sprintf("topup spot balance unavailable: %v", err))
		fmt.Printf("live: funds manager spot balance unavailable: %v\n", err)
		return currentAvail, false
	}
	if transferAmt > spotAvail {
		transferAmt = spotAvail
	}
	if transferAmt < m.fundsCfg.TopupMinUSDT {
		m.recordTransferStatus(fmt.Sprintf("topup skipped spot=%.2f amount=%.2f min=%.2f", spotAvail, transferAmt, m.fundsCfg.TopupMinUSDT))
		fmt.Printf("live: funds manager top-up skipped spot_available=%.2f amount=%.2f min=%.2f\n", spotAvail, transferAmt, m.fundsCfg.TopupMinUSDT)
		return currentAvail, false
	}
	if err := m.transferManager.TransferSpotToPerp(transferAmt); err != nil {
		m.recordTransferStatus(fmt.Sprintf("topup failed %.2f: %v", transferAmt, err))
		fmt.Printf("live: funds manager top-up failed amount=%.2f err=%v\n", transferAmt, err)
		return currentAvail, false
	}
	refreshed, err := availableUSDT(m.rest)
	if err != nil {
		m.recordTransferStatus(fmt.Sprintf("topup refresh failed %.2f: %v", transferAmt, err))
		fmt.Printf("live: funds manager top-up refresh failed after amount=%.2f err=%v\n", transferAmt, err)
		return currentAvail, false
	}
	m.recordTransferStatus(fmt.Sprintf("topup %.2f to perp (%.2f -> %.2f)", transferAmt, currentAvail, refreshed))
	fmt.Printf("live: funds manager top-up amount=%.2f avail_before=%.2f avail_after=%.2f\n", transferAmt, currentAvail, refreshed)
	return refreshed, true
}

func (m *liveExecManager) maybeSweepTradeProfit(now time.Time, p *livePosition) {
	if m == nil || p == nil || !m.fundsCfg.Enable || !m.fundsCfg.SweepProfitEnable {
		return
	}
	if p.RealizedPnL <= p.ProfitSweptUSDT || p.RealizedPnL <= 0 {
		return
	}
	if m.transferManager == nil || !m.transferManager.Supported() {
		m.recordTransferStatus(fmt.Sprintf("sweep unavailable %s realized=%.2f", p.Symbol, p.RealizedPnL))
		fmt.Printf("live: profit sweep unavailable symbol=%s realized=%.2f reason=transfer_unsupported\n", p.Symbol, p.RealizedPnL)
		return
	}
	avail, err := availableUSDT(m.rest)
	if err != nil {
		m.recordTransferStatus(fmt.Sprintf("sweep refresh failed %s: %v", p.Symbol, err))
		fmt.Printf("live: profit sweep balance refresh failed symbol=%s err=%v\n", p.Symbol, err)
		return
	}
	sweepable := maxFloat(0, avail-m.fundsCfg.PerpFloorUSDT)
	if m.fundsCfg.SweepExcessOverTargetOnly {
		sweepable = min(sweepable, maxFloat(0, avail-m.fundsCfg.PerpTargetUSDT))
	}
	profitLeft := p.RealizedPnL - p.ProfitSweptUSDT
	amount := min(profitLeft, sweepable)
	if amount <= 0 {
		m.recordTransferStatus(fmt.Sprintf("sweep skipped %s profit_left=%.2f avail=%.2f", p.Symbol, profitLeft, avail))
		return
	}
	if err := m.transferManager.TransferPerpToSpot(amount); err != nil {
		m.recordTransferStatus(fmt.Sprintf("sweep failed %s %.2f: %v", p.Symbol, amount, err))
		fmt.Printf("live: profit sweep failed symbol=%s amount=%.2f err=%v\n", p.Symbol, amount, err)
		return
	}
	p.ProfitSweptUSDT += amount
	m.recordTransferStatus(fmt.Sprintf("sweep %.2f from %s to spot", amount, p.Symbol))
	fmt.Printf("live: profit sweep symbol=%s amount=%.2f realized=%.2f closed=%s\n",
		p.Symbol, amount, p.RealizedPnL, now.UTC().Format(time.RFC3339))
	m.emitNotify(notify.Event{
		Key:      "PROFIT_SWEPT",
		Title:    "PROFIT SWEPT",
		Class:    notify.ClassLifecycle,
		Severity: notify.SeverityInfo,
		Route:    notify.RouteNormal,
		Symbol:   p.Symbol,
		Message:  "profit swept to spot",
		Metadata: map[string]string{
			"side":           displayPositionSide(p.Side),
			"amount":         fmt.Sprintf("%.2f", amount),
			"trade_realized": fmt.Sprintf("%+.2f", p.RealizedPnL),
		},
	})
}

func (m *liveExecManager) MaintainPerpBalance(now time.Time) {
	if m == nil || !m.fundsCfg.Enable || m.rest == nil {
		return
	}
	every := fundsMaintenanceEvery()
	if every > 0 && !m.lastFundsCheckAt.IsZero() && now.Sub(m.lastFundsCheckAt) < every {
		return
	}
	m.lastFundsCheckAt = now
	if m.transferManager == nil || !m.transferManager.Supported() {
		return
	}
	avail, err := availableUSDT(m.rest)
	if err != nil {
		m.recordTransferStatus(fmt.Sprintf("funds maintenance refresh failed: %v", err))
		return
	}
	if excess := autoSweepAmount(avail, m.fundsCfg); excess > 0 {
		if err := m.transferManager.TransferPerpToSpot(excess); err != nil {
			m.recordTransferStatus(fmt.Sprintf("auto sweep failed %.2f: %v", excess, err))
			fmt.Printf("live: funds manager auto sweep failed amount=%.2f err=%v\n", excess, err)
		} else {
			m.recordTransferStatus(fmt.Sprintf("auto sweep %.2f to spot (avail %.2f > target %.2f)", excess, avail, m.fundsCfg.PerpTargetUSDT))
			fmt.Printf("live: funds manager auto sweep amount=%.2f avail=%.2f target=%.2f\n", excess, avail, m.fundsCfg.PerpTargetUSDT)
		}
		return
	}
	if target := perpTopupTarget(avail, m.fundsCfg); target > 0 {
		_, _ = m.EnsureEntryFunds(target, avail)
	}
}

func (m *liveExecManager) clearPendingAdd(p *livePosition) {
	if p == nil {
		return
	}
	p.PendingAddOrderID = 0
	p.PendingAddPrice = 0
	p.PendingAddQty = 0
	p.PendingAddFilledQty = 0
	p.PendingAddMargin = 0
	p.PendingAddCreatedAt = time.Time{}
	p.PendingAddEntryReason = ""
}

func (m *liveExecManager) clearPendingExit(p *livePosition) {
	if p == nil {
		return
	}
	p.PendingExitOrderID = 0
	p.PendingExitPrice = 0
	p.PendingExitQty = 0
	p.PendingExitFilledQty = 0
	p.PendingExitCreatedAt = time.Time{}
	p.PendingExitReason = ""
	p.PendingExitAction = ""
}

func (m *liveExecManager) applyAddFill(now time.Time, p *livePosition, deltaQty, fillPx, deltaMargin float64, reason string) error {
	if m == nil || p == nil || deltaQty <= 0 {
		return nil
	}
	if fillPx <= 0 {
		fillPx = p.EntryPrice
	}
	if deltaMargin <= 0 && fillPx > 0 && p.Leverage > 0 {
		deltaMargin = (fillPx * deltaQty) / float64(maxInt(1, p.Leverage))
	}
	oldQty := maxFloat(p.RemainingQty, 0)
	totalQty := oldQty + deltaQty
	if oldQty > 0 && totalQty > 0 {
		p.EntryPrice = ((p.EntryPrice * oldQty) + (fillPx * deltaQty)) / totalQty
	} else if fillPx > 0 {
		p.EntryPrice = fillPx
	}
	p.Qty += deltaQty
	p.FilledQty += deltaQty
	p.RemainingQty = totalQty
	p.Margin += deltaMargin
	p.DeployedMargin += deltaMargin
	if p.PendingAddFilledQty <= fillEpsilon(p.PendingAddQty) {
		p.AddCount++
	}
	if strings.EqualFold(strings.TrimSpace(reason), "CONFIRMED_ADD") {
		p.StarterOnly = false
		p.AddLockedUntilConfirm = false
		if !importedManagedPosition(p) {
			p.EntryReason = "continuation_fast"
		}
	}
	if importedManagedPosition(p) && fillPx > 0 {
		p.ManageAnchorPrice = fillPx
	}
	updateManagePhase(p, false)
	refreshRunnerReservation(p, m.ladderCfg.StarterUSDT)
	p.State = execOpen
	p.UpdatedAt = now
	p.TP1FilledQty = 0
	p.TP2FilledQty = 0
	p.TP3FilledQty = 0
	p.StopFilledQty = 0
	p.HitTP1 = false
	p.HitTP2 = false
	p.HitTP3 = false
	p.UnknownExitChecks = 0
	p.ProtectedStop = 0
	_ = m.cancelRemainingExits(p)
	if err := m.placeInitialBrackets(p); err != nil {
		return err
	}
	_ = m.logFill(now, p, "ADD", reason, deltaQty, fillPx, 0, 0)
	m.sendFillReceipt(now, p, "ADD", reason, deltaQty, fillPx, 0, 0)
	return nil
}

func (m *liveExecManager) logFill(now time.Time, p *livePosition, action, reason string, qty, fillPx, pnl, pct float64) error {
	if m == nil || strings.TrimSpace(m.tradesCSV) == "" || p == nil {
		return nil
	}
	if err := ensureCSVWithHeader(m.tradesCSV, []string{
		"ts", "symbol", "side", "source", "action", "reason", "qty", "fill_px", "entry_px", "pnl", "pnl_pct", "state", "hold_min",
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
		nonEmpty(strings.ToUpper(strings.TrimSpace(p.EntrySource)), "BOT"),
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
		holdMin := now.Sub(p.CreatedAt).Minutes()
		m.eventLog.Emit(stats.Event{
			Timestamp:    now,
			Type:         evtType,
			Symbol:       p.Symbol,
			Side:         p.Side,
			Source:       nonEmpty(strings.ToUpper(strings.TrimSpace(p.EntrySource)), "BOT"),
			TF:           "1m",
			Strategy:     p.EntryReason,
			TriggerState: p.EntryTrigger,
			ExitProfile:  p.ExitProfile,
			Discovery:    p.DiscoveryScore,
			Trigger:      p.TriggerScore,
			Execution:    p.ExecutionScore,
			Combined:     p.CombinedScore,
			StopDistPct:  p.StopDistancePct,
			EntryPx:      p.EntryPrice,
			ExitPx:       fillPx,
			HoldMin:      holdMin,
			MFER:         p.MaxFavorableR,
			MAER:         p.MaxAdverseR,
			CaptureRatio: p.CaptureRatio,
			MaxGivebackR: p.MaxGivebackR,
			MarkPx:       p.LastMark,
			PnLUSD:       pnl,
			PnLPct:       pct,
			Reason:       strings.ToUpper(strings.TrimSpace(reason)),
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
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "ENTRY", "ADD":
		m.tg.Sendf("%s", notify.FormatEntry(notify.EntryView{
			Mode:     "live",
			Symbol:   cleanSymbol(p.Symbol),
			Side:     p.Side,
			Setup:    firstNonEmpty(p.EntrySetupFamily, p.EntryReason),
			Strategy: firstNonEmpty(p.EntryReason, "execution"),
			Grade:    p.EntryGrade,
			Margin:   p.Margin,
			Leverage: maxInt(1, p.Leverage),
			Entry:    fillPx,
			Stop:     p.StopPrice,
			TP1:      p.TP1Price,
			TP2:      p.TP2Price,
			TP3:      p.TP3Price,
		}))
		return
	}
	exitView := notify.ExitView{
		Symbol:      cleanSymbol(p.Symbol),
		Side:        p.Side,
		ExitReason:  reason,
		HoldTime:    fmt.Sprintf("%.0fm", holdMin),
		RealizedPnL: pnl,
		RMultiple:   safeRMultiple(p.EntryPrice, p.StopPrice, fillPx, p.Side),
		FillPrice:   fillPx,
		Stop:        p.StopPrice,
		Entry:       p.EntryPrice,
		ExitPrice:   fillPx,
	}
	if p.RemainingQty > 0 && !strings.EqualFold(strings.TrimSpace(action), "CLOSE") && !strings.EqualFold(strings.TrimSpace(action), "FORCE_CLOSE") {
		exitView.RemainingPositionLine = fmt.Sprintf("Remaining %.4f · Day %+.2f", p.RemainingQty, dayRealized)
	}
	actionU := strings.ToUpper(strings.TrimSpace(action))
	reasonU := strings.ToUpper(strings.TrimSpace(reason))
	switch {
	case actionU == "TP" || strings.HasPrefix(reasonU, "TP"):
		m.tg.Sendf("%s", notify.FormatTPHit(exitView))
	case actionU == "STOP" || strings.Contains(reasonU, "STOP") || reasonU == "SL":
		m.tg.Sendf("%s", notify.FormatSLHit(exitView))
	default:
		m.tg.Sendf("%s", notify.FormatTradeClosed(exitView))
	}
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
		source string
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
			source: csvValue(rec, idx, "source"),
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
	b.WriteString("| Time | Sym | Side | Src | Action | Qty | Fill | PnL | PnL% | Hold(m) | Reason |\n")
	b.WriteString("|---|---|---|---|---|---:|---:|---:|---:|---:|---|\n")
	for _, x := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			x.ts.In(m.reportLoc).Format("15:04"),
			strings.ToUpper(strings.TrimSpace(x.symbol)),
			strings.ToUpper(strings.TrimSpace(x.side)),
			nonEmpty(strings.ToUpper(strings.TrimSpace(x.source)), "BOT"),
			strings.ToUpper(strings.TrimSpace(x.action)),
			x.qty, x.fill, x.pnl, x.pct, x.hold, strings.ToUpper(strings.TrimSpace(x.reason)))
	}
	return strings.TrimSpace(b.String()), true
}

func (m *liveExecManager) DailyReportMessage(dayKey string) (string, bool) {
	if m == nil || m.reportLoc == nil {
		return "", false
	}
	eventsPath := resolveStatePath(envStr("LIVE_EVENTS_LOG", "logs/events.jsonl"))
	fromLocal, err := time.ParseInLocation("2006-01-02 15:04:05", dayKey+" 00:00:00", m.reportLoc)
	if err != nil {
		return "", false
	}
	toLocal := fromLocal.Add(24*time.Hour - time.Nanosecond)
	events, err := stats.LoadEvents(eventsPath, ptrTime(fromLocal.UTC()), ptrTime(toLocal.UTC()))
	if err != nil {
		return "", false
	}
	if len(events) == 0 {
		return notify.FormatDailyRecap(notify.RecapView{
			Mode:              "LIVE",
			Date:              dayKey,
			RealizedPnL:       0,
			TradeCount:        0,
			WinRate:           0,
			BestTrade:         "-",
			WorstTrade:        "-",
			RiskNoteOrSummary: "No trades",
		}), true
	}
	report := stats.Aggregate(events)
	if report.TotalTrades == 0 {
		return notify.FormatDailyRecap(notify.RecapView{
			Mode:              "LIVE",
			Date:              dayKey,
			RealizedPnL:       0,
			TradeCount:        0,
			WinRate:           0,
			BestTrade:         "-",
			WorstTrade:        "-",
			RiskNoteOrSummary: "No trades",
		}), true
	}
	gross := 0.0
	for _, row := range report.BySymbol {
		gross += row.PnL
	}
	bestTrade := "-"
	worstTrade := "-"
	if len(report.BySymbol) > 0 {
		best := report.BySymbol[0]
		worst := report.BySymbol[0]
		for _, row := range report.BySymbol[1:] {
			if row.PnL > best.PnL {
				best = row
			}
			if row.PnL < worst.PnL {
				worst = row
			}
		}
		bestTrade = fmt.Sprintf("%s %+.2f", cleanSymbol(best.Name), best.PnL)
		worstTrade = fmt.Sprintf("%s %+.2f", cleanSymbol(worst.Name), worst.PnL)
	}
	return notify.FormatDailyRecap(notify.RecapView{
		Mode:              "LIVE",
		Date:              dayKey,
		RealizedPnL:       gross - report.Fees,
		TradeCount:        report.TotalTrades,
		WinRate:           report.WinRate,
		BestTrade:         bestTrade,
		WorstTrade:        worstTrade,
		RiskNoteOrSummary: fmt.Sprintf("Capture %.2f · GivebackR %.2f", report.AvgCapture, report.AvgGivebackR),
	}), true
}

func (m *liveExecManager) ReconcileBootState() (closedLocal int, importedRemote int, err error) {
	if m == nil || m.rest == nil {
		return 0, 0, nil
	}
	rows, err := cachedPositionRisk(m.rest, "")
	if err != nil {
		return 0, 0, err
	}
	type remotePos struct {
		amt    float64
		entry  float64
		mark   float64
		lev    int
		margin float64
	}
	remote := map[string]remotePos{}
	remoteKeys := map[string]string{}
	for _, row := range rows {
		sym := strings.ToUpper(strings.TrimSpace(fmt.Sprint(row["symbol"])))
		amt := mapFloat(row["positionAmt"])
		if sym == "" || abs(amt) <= 1e-10 {
			continue
		}
		side := "BUY"
		if amt < 0 {
			side = "SELL"
		}
		entry := mapFloat(row["entryPrice"])
		mark := mapFloat(row["markPrice"])
		if entry <= 0 {
			entry = mark
		}
		remote[sym] = remotePos{
			amt:    amt,
			entry:  entry,
			mark:   mark,
			lev:    int(mapFloat(row["leverage"])),
			margin: maxFloat(mapFloat(row["isolatedWallet"]), mapFloat(row["positionInitialMargin"])),
		}
		remoteKeys[positionLookupKey(sym, side)] = manualManageFingerprint(sym, side, abs(amt), entry)
	}
	m.pruneManualRequests(remoteKeys)
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
		m.markPositionClosed(now, p, "POSITION_FLAT_RECOVERED")
		closedLocal++
	}
	for sym, rp := range remote {
		existing := m.positions[sym]
		if m.isActive(existing) {
			side := "BUY"
			if rp.amt < 0 {
				side = "SELL"
			}
			if samePositionSide(existing.Side, side) {
				sync := syncImportedRemotePosition(existing, abs(rp.amt), rp.entry, rp.margin, rp.lev, now)
				if sync.materialManualMutation() && manualManagedTrade(existing) {
					m.recalibrateManagedManualPosition(existing, now, sync)
				}
			}
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
		req := manualManageRequest{
			Key:         positionLookupKey(sym, side),
			Fingerprint: manualManageFingerprint(sym, side, qty, entry),
			Symbol:      sym,
			Side:        normalizePositionSide(side),
			Qty:         qty,
			Entry:       entry,
			Margin:      rp.margin,
			Leverage:    rp.lev,
		}
		if approvedReq, ok := m.approvedManualRequest(sym, side, qty, entry); ok {
			if _, err := m.activateManualManagement(approvedReq, now, "MANUAL_APPROVED_RETRY"); err == nil {
				importedRemote++
				continue
			}
		}
		if passiveReq, ok := m.passiveManualRequest(sym, side, qty, entry); ok {
			if _, err := m.activatePassiveManualImport(passiveReq, now, "MANUAL_TIMEOUT_IMPORT", false); err == nil {
				importedRemote++
				continue
			}
		}
		if m.queueManualManagementRequest(sym, side, qty, entry, rp.margin, rp.lev, now) {
			if _, err := m.activatePassiveManualImport(req, now, "MANUAL_PENDING_IMPORT", true); err == nil {
				importedRemote++
			}
			continue
		}
		if _, err := m.activateManualManagement(req, now, "REMOTE_POSITION_MONITORED"); err == nil {
			importedRemote++
			continue
		}
		p, err := m.activatePassiveManualImport(req, now, "REMOTE_POSITION_PASSIVE_FALLBACK", false)
		if err == nil {
			if m.tg != nil && p != nil {
				m.tg.Sendf("%s", notify.FormatManualDetected(notify.ManualView{
					Symbol:   cleanSymbol(p.Symbol),
					Side:     p.Side,
					Quantity: p.RemainingQty,
					Entry:    p.EntryPrice,
					Margin:   p.Margin,
					Leverage: maxInt(1, p.Leverage),
				}))
			}
			importedRemote++
		}
	}
	_ = m.save()
	return closedLocal, importedRemote, nil
}

func (m *liveExecManager) newImportedRemotePosition(symbol, side string, qty, entry, margin float64, lev int, now time.Time, source string) *livePosition {
	p := &livePosition{
		Symbol:         symbol,
		Side:           normalizePositionSide(side),
		State:          execOpen,
		CreatedAt:      now,
		UpdatedAt:      now,
		EntryPrice:     entry,
		Qty:            qty,
		FilledQty:      qty,
		RemainingQty:   qty,
		Margin:         margin,
		DeployedMargin: margin,
		Leverage:       maxInt(1, lev),
		EntryReason:    manualEntryReasonPassive,
		EntrySource:    source,
		CloseReason:    "RECOVERED_POSITION",
	}
	switch strings.ToUpper(strings.TrimSpace(source)) {
	case manualEntrySourcePassive:
		p.ManualManageState = manualManageStatePassive
		p.Managed = false
		p.Protected = false
	case manualEntrySourceManaged:
		p.ManualManageState = manualManageStatePendingProtection
		p.Managed = true
		p.Protected = false
	}
	updateManagePhase(p, false)
	if p.Margin <= 0 && p.EntryPrice > 0 && p.Leverage > 0 {
		p.Margin = (p.EntryPrice * p.FilledQty) / float64(p.Leverage)
	}
	stopPct := clamp(m.stopPct/100.0, m.minStopPct/100.0, m.maxStopPct/100.0)
	if atrPct := estimateATRPctWithCache(m.featureCache, symbol, 64, 14); atrPct > 0 {
		stopPct = clamp(atrPct*m.recoverATRMult, m.minStopPct/100.0, m.maxStopPct/100.0)
	}
	if isLongSide(side) {
		p.StopPrice = entry * (1 - stopPct)
	} else {
		p.StopPrice = entry * (1 + stopPct)
	}
	return p
}

func (m *liveExecManager) upsertPassiveOperatorPosition(symbol, side string, qty, entry, margin float64, lev int, now time.Time) *livePosition {
	if m == nil {
		return nil
	}
	sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if sym == "" {
		return nil
	}
	if existing := m.positions[sym]; m.isActive(existing) && samePositionSide(existing.Side, side) {
		syncImportedRemotePosition(existing, qty, entry, margin, lev, now)
		existing.EntrySource = manualEntrySourcePassive
		existing.EntryReason = operatorEntryReason
		existing.ManualManageState = manualManageStatePassive
		existing.Managed = false
		return existing
	}
	p := m.newImportedRemotePosition(sym, side, qty, entry, margin, lev, now, manualEntrySourcePassive)
	if p == nil {
		return nil
	}
	p.EntryReason = operatorEntryReason
	m.positions[sym] = p
	return p
}

func (m *liveExecManager) attachOperatorBracket(p *livePosition, qty float64, tpPrice, slPrice float64) (string, error) {
	if m == nil || m.rest == nil || p == nil {
		return "", fmt.Errorf("execution manager not ready")
	}
	meta, err := m.rest.SymbolMeta(p.Symbol, true)
	if err != nil {
		return "", err
	}
	trader := &aster.Trader{
		Symbol:   p.Symbol,
		SymbolLC: strings.ToLower(p.Symbol),
		Rest:     m.rest,
		Meta:     meta,
	}
	var tpPtr, slPtr *float64
	if tpPrice > 0 {
		tpPtr = &tpPrice
	}
	if slPrice > 0 {
		slPtr = &slPrice
	}
	out, err := trader.Bracket(isLongSide(p.Side), qty, tpPtr, slPtr)
	if err != nil {
		return "", err
	}
	parts := []string{}
	if tpRaw := out["tp"]; tpRaw != nil {
		p.TP1Price = tpPrice
		p.TP1Qty = qty
		p.TP1OrderID = mapInt64(tpRaw["orderId"])
		parts = append(parts, "tp_submitted")
	}
	if slRaw := out["sl"]; slRaw != nil {
		p.StopPrice = slPrice
		p.StopOrderID = mapInt64(slRaw["orderId"])
		p.Protected = true
		parts = append(parts, "sl_submitted")
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, ", "), nil
}

func (m *liveExecManager) submitOperatorOrder(req operatorOrderRequest, metaBySymbol map[string]symbolMeta) (operatorOrderResult, error) {
	var result operatorOrderResult
	if m == nil || m.rest == nil {
		return result, fmt.Errorf("execution manager not ready")
	}
	sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(req.Symbol)))
	if sym == "" {
		return result, fmt.Errorf("symbol required")
	}
	result.Symbol = sym
	result.Side = normalizePositionSide(req.Side)
	result.USD = req.USD
	result.LimitPrice = req.LimitPrice
	result.StopLoss = req.StopLoss
	result.TakeProfit = req.TakeProfit

	if err := signedUserDataBackoffCheck(time.Now().UTC()); err != nil {
		return result, err
	}

	meta, err := m.rest.SymbolMeta(sym, true)
	if err != nil {
		return result, err
	}
	if snap, ok := metaBySymbol[sym]; ok {
		ref := snap.LastPrice
		if ref <= 0 {
			ref = snap.OpenPrice
		}
		if req.HasLimit && ref > 0 {
			if _, sanitized := sanitizeSnapshotPrice(ref, req.LimitPrice); sanitized {
				return result, fmt.Errorf("limit price %.8f is too far from snapshot %.8f", req.LimitPrice, ref)
			}
		}
	}
	var levPtr *int
	if req.Leverage > 0 {
		lev := req.Leverage
		levPtr = &lev
	}
	trader := &aster.Trader{
		Symbol:   sym,
		SymbolLC: strings.ToLower(sym),
		Rest:     m.rest,
		Meta:     meta,
		State:    m.marketStates[sym],
	}
	var submit map[string]any
	if req.HasLimit {
		submit, err = trader.EnterLimitUSD(req.Side, req.USD, req.LimitPrice, levPtr)
	} else {
		var refPrice *float64
		if snap, ok := metaBySymbol[sym]; ok && snap.LastPrice > 0 {
			rp := snap.LastPrice
			refPrice = &rp
		}
		submit, err = trader.EnterMarketUSD(req.Side, req.USD, refPrice, levPtr)
	}
	if err != nil {
		signedUserDataBackoffObserve(time.Now().UTC(), err)
		return result, err
	}
	result.OrderID = mapInt64(submit["orderId"])
	if result.OrderID <= 0 {
		return result, fmt.Errorf("missing order id from venue response")
	}
	progressOrder, err := trader.WaitForFill(result.OrderID, operatorFillTimeout(), operatorFillPoll())
	if err != nil {
		return result, err
	}
	progress := parseOrderProgress(progressOrder)
	result.Status = firstNonEmpty(progress.Status, fmt.Sprint(submit["status"]))
	result.Working = progress.Working
	result.Filled = progress.Filled
	result.Rejected = progress.Rejected
	result.Qty = progress.ExecQty
	result.AvgPrice = progress.AvgPx

	if !result.Filled {
		return result, nil
	}

	now := time.Now().UTC()
	rows, err := m.rest.PositionRisk(sym)
	if err != nil {
		return result, err
	}
	view := remotePositionForSide(rows, req.Side)
	if view.QtyAbs <= 0 {
		return result, fmt.Errorf("order filled but live position not yet visible on venue")
	}
	lev := maxInt(1, req.Leverage)
	for _, row := range rows {
		amt := mapFloat(row["positionAmt"])
		if mathAbs(amt) <= 1e-10 {
			continue
		}
		rowSide := "BUY"
		if amt < 0 {
			rowSide = "SELL"
		}
		if !samePositionSide(rowSide, req.Side) {
			continue
		}
		lev = maxInt(1, int(maxFloat(1, mapFloat(row["leverage"]))))
		break
	}
	p := m.upsertPassiveOperatorPosition(sym, req.Side, view.QtyAbs, view.EntryPrice, view.Margin, lev, now)
	if p == nil {
		return result, fmt.Errorf("filled position could not be tracked locally")
	}
	if req.HasTakeProfit || req.HasStopLoss {
		bracketStatus, err := m.attachOperatorBracket(p, view.QtyAbs, req.TakeProfit, req.StopLoss)
		if err != nil {
			result.BracketStatus = "submit_failed: " + err.Error()
		} else {
			result.BracketStatus = bracketStatus
		}
	}
	if mark, err := m.currentMark(sym); err == nil && mark > 0 {
		p.LastMark = mark
		result.PositionPnL, result.PositionPnLPct = realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
	}
	_ = m.save()
	return result, nil
}

func manageAnchorPrice(p *livePosition) float64 {
	if p == nil {
		return 0
	}
	if !strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT") && p.ManageAnchorPrice > 0 {
		return p.ManageAnchorPrice
	}
	return p.EntryPrice
}

func botManagedPosition(p *livePosition) bool {
	if p == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT") {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT") &&
		p.ManageAnchorPrice > 0 &&
		strings.EqualFold(strings.TrimSpace(p.EntryReason), manualEntryReasonManaged)
}

func importedManagedPosition(p *livePosition) bool {
	if p == nil {
		return false
	}
	return botManagedPosition(p) && !strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT")
}

func manualPassivePosition(p *livePosition) bool {
	if p == nil {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT") &&
		strings.EqualFold(strings.TrimSpace(p.EntryReason), manualEntryReasonPassive) &&
		strings.EqualFold(strings.TrimSpace(p.EntrySource), manualEntrySourcePassive)
}

type importedRemoteSync struct {
	QtyChanged      bool
	QtyIncreased    bool
	EntryChanged    bool
	MarginChanged   bool
	LeverageChanged bool
}

func (s importedRemoteSync) materialManualMutation() bool {
	return s.QtyIncreased || (s.EntryChanged && s.QtyChanged) || s.LeverageChanged
}

func relDiffFloat(a, b float64) float64 {
	if a <= 0 && b <= 0 {
		return 0
	}
	return math.Abs(a-b) / maxFloat(maxFloat(math.Abs(a), math.Abs(b)), 1e-9)
}

func syncImportedRemotePosition(p *livePosition, qty, entry, margin float64, lev int, now time.Time) importedRemoteSync {
	var sync importedRemoteSync
	if p == nil {
		return sync
	}
	prevQty := maxFloat(p.RemainingQty, p.FilledQty)
	if prevQty <= 0 {
		prevQty = p.Qty
	}
	prevEntry := p.EntryPrice
	prevMargin := maxFloat(p.Margin, p.DeployedMargin)
	prevLev := p.Leverage
	if qty > 0 {
		sync.QtyChanged = relDiffFloat(prevQty, qty) > 0.002
		sync.QtyIncreased = qty > prevQty && sync.QtyChanged
		p.Qty = qty
		p.FilledQty = qty
		p.RemainingQty = qty
	}
	if entry > 0 {
		sync.EntryChanged = relDiffFloat(prevEntry, entry) > 0.0005
		p.EntryPrice = entry
	}
	if margin > 0 {
		sync.MarginChanged = relDiffFloat(prevMargin, margin) > 0.02
		p.Margin = margin
		p.DeployedMargin = margin
	}
	if lev > 0 {
		sync.LeverageChanged = prevLev > 0 && lev != prevLev
		p.Leverage = maxInt(1, lev)
	}
	p.UpdatedAt = now
	return sync
}

func resetManagedProtectionForRemoteMutation(p *livePosition) {
	if p == nil {
		return
	}
	p.HitTP1 = false
	p.HitTP2 = false
	p.HitTP3 = false
	p.TrailOn = false
	p.TrailRef = 0
	p.TrailStop = 0
	p.MaxFavorableR = 0
	p.MaxAdverseR = 0
	p.MaxGivebackR = 0
	p.CaptureRatio = 0
	p.ProtectionStage = protectionStageNone
	p.FirstProtectAt = time.Time{}
	p.ProtectedStop = 0
	p.Protected = false
	p.Managed = true
	p.ManualManageState = manualManageStatePendingProtection
	p.ProtectionPending = false
	p.ProtectionRetryAfter = time.Time{}
	p.ProtectionRetryCount = 0
	p.ProtectionFailCount = 0
	p.ManageFailSuppressCount = 0
	p.LastManageFailAt = time.Time{}
	p.LastManageFailCause = ""
}

func (m *liveExecManager) recalibrateManagedManualPosition(p *livePosition, now time.Time, sync importedRemoteSync) {
	if m == nil || p == nil || !manualManagedTrade(p) || strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT") {
		return
	}
	resetManagedProtectionForRemoteMutation(p)
	currentMark := 0.0
	if m.rest != nil {
		if mark, err := m.currentMark(p.Symbol); err == nil && mark > 0 {
			currentMark = mark
		}
	}
	if currentMark <= 0 {
		currentMark = maxFloat(p.LastMark, p.EntryPrice)
	}
	if currentMark > 0 {
		p.ManageAnchorPrice = currentMark
	}
	if err := m.initializeBracketLevels(p); err != nil {
		if currentMark > 0 {
			p.LastManageFailCause = fmt.Sprintf("recalibrate_brackets_failed:%v", err)
			markProtectionPending(p, now, "recalibrate_brackets_failed")
		}
		return
	}
	if currentMark > 0 {
		m.reconstructManualManagedState(now, p, currentMark)
	}
	armManualProtectionAfterReconstruct(now, p)
	if manageDebugLogging() {
		fmt.Printf("live: manual-recalibrated symbol=%s side=%s qty=%.6f entry=%s mark=%s qty_increased=%v entry_changed=%v lev_changed=%v\n",
			p.Symbol, p.Side, p.RemainingQty, fmtPrice(p.EntryPrice), fmtPrice(currentMark),
			sync.QtyIncreased, sync.EntryChanged, sync.LeverageChanged)
	}
}

func manualProtectionRetryDelay(cause string) time.Duration {
	delay := time.Duration(envInt("LIVE_MANUAL_PROTECTION_RETRY_SEC", 300)) * time.Second
	if delay <= 0 {
		delay = 5 * time.Minute
	}
	switch strings.TrimSpace(strings.ToLower(cause)) {
	case "awaiting_conviction", "manage_awaiting_conviction":
		delay = minDuration(delay, 90*time.Second)
	case "mark_unavailable":
		delay = maxDuration(delay, 3*time.Minute)
	case "exchange_immediate_trigger_mark_unavailable", "exchange_immediate_trigger_retry_failed":
		delay = maxDuration(delay, 5*time.Minute)
	}
	return delay
}

func manualProtectionConvictionTimeout() time.Duration {
	sec := envInt("LIVE_MANUAL_PROTECTION_CONVICTION_TIMEOUT_SEC", 900)
	if sec <= 0 {
		sec = 900
	}
	return time.Duration(sec) * time.Second
}

func manualProtectionConvictionTimedOut(p *livePosition, now time.Time) bool {
	if p == nil {
		return false
	}
	anchor := p.CreatedAt
	if !p.UpdatedAt.IsZero() && p.UpdatedAt.After(anchor) {
		anchor = p.UpdatedAt
	}
	if anchor.IsZero() {
		anchor = now
	}
	return now.Sub(anchor) >= manualProtectionConvictionTimeout()
}

func manualProtectionAlertCooldown() time.Duration {
	delay := time.Duration(envInt("LIVE_ALERT_SUPPRESSION_WINDOW_SEC", envInt("LIVE_MANUAL_PROTECTION_ALERT_COOLDOWN_SEC", 300))) * time.Second
	if delay <= 0 {
		delay = 5 * time.Minute
	}
	return delay
}

func markProtectionPending(p *livePosition, now time.Time, cause string) {
	if p == nil {
		return
	}
	p.ProtectionPending = true
	if manualManagedTrade(p) && strings.TrimSpace(p.ManualManageState) == "" {
		p.ManualManageState = manualManageStatePendingProtection
	}
	retryDelay := manualProtectionRetryDelay(cause)
	if strings.EqualFold(strings.TrimSpace(p.LastManageFailCause), strings.TrimSpace(cause)) && !p.ProtectionRetryAfter.IsZero() && p.ProtectionRetryAfter.After(now) {
		retryDelay = minDuration(maxDuration(retryDelay, 2*time.Duration(p.ProtectionRetryAfter.Sub(now))), 30*time.Minute)
	}
	p.ProtectionRetryAfter = now.Add(retryDelay)
	if strings.TrimSpace(cause) != "" && strings.TrimSpace(p.LastManageFailCause) == "" {
		p.LastManageFailCause = strings.TrimSpace(cause)
	}
}

func clearProtectionPending(p *livePosition) {
	if p == nil {
		return
	}
	p.ProtectionPending = false
	p.ProtectionRetryAfter = time.Time{}
	p.ProtectionRetryCount = 0
	p.ProtectionFailCount = 0
	p.ForceProtectionNow = false
	p.ManageFailSuppressCount = 0
	p.LastManageFailAt = time.Time{}
	p.LastManageFailCause = ""
	p.LastManageNotifyState = ""
	p.LastManageNotifyCause = ""
	p.LastManageNotifyAt = time.Time{}
	if manualManagedTrade(p) {
		p.ManualManageState = manualManageStateLive
		p.Managed = true
		p.Protected = true
	}
}

func manualProtectionRetryBudget() int {
	n := envInt("LIVE_MANUAL_PROTECT_RETRY_MAX", envInt("LIVE_MANUAL_PROTECTION_RETRY_BUDGET", 4))
	if n < 1 {
		n = 4
	}
	return n
}

func manualProtectionRetryBackoff(attempt int) time.Duration {
	if raw := strings.TrimSpace(envStr("LIVE_MANUAL_PROTECT_RETRY_SCHEDULE_SEC", "")); raw != "" {
		parts := strings.Split(raw, ",")
		if attempt >= 1 && attempt <= len(parts) {
			if sec, err := strconv.Atoi(strings.TrimSpace(parts[attempt-1])); err == nil && sec >= 0 {
				return time.Duration(sec) * time.Second
			}
		}
	}
	switch attempt {
	case 1:
		return 0
	case 2:
		return 30 * time.Second
	case 3:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}

func manualProtectionRetryEnabled() bool {
	return envBool("LIVE_MANUAL_PROTECT_RETRY_ENABLE", true)
}

func degradedReminderInterval() time.Duration {
	sec := envInt("LIVE_MANUAL_PROTECT_DEGRADED_REMINDER_SEC", 1800)
	if sec <= 0 {
		sec = 1800
	}
	return time.Duration(sec) * time.Second
}

func manualProtectionStatus(p *livePosition) string {
	if p == nil {
		return ""
	}
	if manualPassivePosition(p) {
		return "UNMANAGED"
	}
	if strings.TrimSpace(p.ManualManageState) == manualManageStateConflict {
		return "CONFLICT"
	}
	if !manualManagedTrade(p) {
		if hasLiveProtectiveOrder(p) || p.Protected {
			return "PROTECTED"
		}
		return "UNPROTECTED"
	}
	switch strings.TrimSpace(p.ManualManageState) {
	case manualManageStateForceClose:
		return "FORCE_CLOSE"
	case manualManageStateCritical:
		if p.ProtectionPending {
			return "PROTECTING"
		}
		return "CRITICAL_UNPROTECTED"
	case manualManageStateLive:
		if hasLiveProtectiveOrder(p) || p.Protected {
			return "PROTECTED"
		}
		return "PROTECTING"
	default:
		return "PROTECTING"
	}
}

func manageStatusNotifyInterval(state string) time.Duration {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case notify.ManageStateDegraded:
		return degradedReminderInterval()
	default:
		return manualProtectionAlertCooldown()
	}
}

func shouldNotifyManageStatus(p *livePosition, state, cause string, now time.Time) bool {
	if p == nil {
		return false
	}
	state = strings.ToUpper(strings.TrimSpace(state))
	cause = strings.TrimSpace(cause)
	if p.LastManageNotifyState == "" {
		p.LastManageNotifyState = state
		p.LastManageNotifyCause = cause
		p.LastManageNotifyAt = now
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(p.LastManageNotifyState), state) ||
		!strings.EqualFold(strings.TrimSpace(p.LastManageNotifyCause), cause) {
		p.LastManageNotifyState = state
		p.LastManageNotifyCause = cause
		p.LastManageNotifyAt = now
		return true
	}
	if p.LastManageNotifyAt.IsZero() || now.Sub(p.LastManageNotifyAt) >= manageStatusNotifyInterval(state) {
		p.LastManageNotifyAt = now
		return true
	}
	return false
}

func manualManagedProtectionBroken(p *livePosition) bool {
	if p == nil || !manualManagedTrade(p) || p.State == execClosed || p.RemainingQty <= 0 {
		return false
	}
	state := strings.TrimSpace(p.ManualManageState)
	if state == manualManageStateForceClose || state == manualManageStateCritical {
		return true
	}
	if p.Protected || hasLiveProtectiveOrder(p) {
		return false
	}
	if !envBool("LIVE_IMPORT_BLOCK_IF_UNPROTECTED", true) {
		return false
	}
	if state == manualManageStatePendingProtection {
		timeout := time.Duration(envInt("LIVE_IMPORT_PROTECT_TIMEOUT_SEC", 15)) * time.Second
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		anchor := p.UpdatedAt
		if anchor.IsZero() {
			anchor = p.CreatedAt
		}
		return time.Since(anchor) >= timeout || p.ProtectionPending
	}
	return p.ProtectionPending
}

func (m *liveExecManager) hasBlockingManagedProtectionFailure() bool {
	if m == nil || !envBool("LIVE_DEGRADED_BLOCK_NEW_ENTRIES", true) {
		return false
	}
	for _, p := range m.positions {
		if manualManagedProtectionBroken(p) {
			return true
		}
	}
	return false
}

func (m *liveExecManager) hasCriticalProtectionState() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.positions {
		if p == nil || !m.isActive(p) || !manualManagedTrade(p) {
			continue
		}
		state := manualProtectionStatus(p)
		if state == "PROTECTING" || state == "CRITICAL_UNPROTECTED" || state == "FORCE_CLOSE" {
			return true
		}
	}
	return false
}

func (m *liveExecManager) criticalProtectionSummaryLines(limit int) []string {
	if m == nil {
		return nil
	}
	if limit <= 0 {
		limit = 3
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	rows := make([]string, 0, limit)
	for _, p := range m.positions {
		if p == nil || !m.isActive(p) || !manualManagedTrade(p) {
			continue
		}
		state := manualProtectionStatus(p)
		if state != "PROTECTING" && state != "CRITICAL_UNPROTECTED" && state != "FORCE_CLOSE" {
			continue
		}
		rows = append(rows, fmt.Sprintf("<b>%s %s</b> status=%s retries=%d/%d cause=%s",
			cleanSymbol(p.Symbol),
			displayPositionSide(p.Side),
			state,
			p.ProtectionRetryCount,
			manualProtectionRetryBudget(),
			firstNonEmpty(strings.TrimSpace(p.LastManageFailCause), "pending_protection"),
		))
		if len(rows) >= limit {
			break
		}
	}
	return rows
}

func recordManualProtectionFailure(p *livePosition, now time.Time, cause string) bool {
	if p == nil {
		return false
	}
	cause = strings.TrimSpace(cause)
	p.ProtectionPending = true
	p.LastManageFailAt = now
	p.LastManageFailCause = cause
	p.ProtectionFailCount++
	p.ProtectionRetryCount++
	p.Managed = true
	p.Protected = false
	if !manualProtectionRetryEnabled() {
		p.ManualManageState = manualManageStateCritical
		p.ProtectionRetryAfter = time.Time{}
		return true
	}
	if p.ProtectionRetryCount >= manualProtectionRetryBudget() {
		p.ManualManageState = manualManageStateCritical
		p.ProtectionRetryAfter = time.Time{}
		return true
	}
	p.ManualManageState = manualManageStatePendingProtection
	p.ProtectionRetryAfter = now.Add(manualProtectionRetryBackoff(p.ProtectionRetryCount))
	return false
}

func (m *liveExecManager) handleManualProtectionFailure(p *livePosition, cause string, now time.Time) {
	if m == nil || p == nil || !manualManagedTrade(p) || p.State == execClosed || p.RemainingQty <= 0 {
		return
	}
	cause = strings.TrimSpace(cause)
	if m.shouldEmergencyForceCloseManagedPosition(p, cause) {
		if err := m.emergencyForceCloseManagedPosition(p, cause, now); err == nil {
			return
		}
	}
	p.Managed = true
	p.Protected = false
	if strings.TrimSpace(p.ManualManageState) == "" {
		p.ManualManageState = manualManageStatePendingProtection
	}
	if strings.EqualFold(cause, "awaiting_conviction_timeout") {
		p.ManualManageState = manualManageStateCritical
	}
	if !p.ProtectionPending {
		markProtectionPending(p, now, cause)
	} else if p.ProtectionRetryAfter.IsZero() || !p.ProtectionRetryAfter.After(now) {
		p.ProtectionRetryAfter = now.Add(manualProtectionRetryBackoff(maxInt(1, p.ProtectionRetryCount)))
	}
	if strings.EqualFold(strings.TrimSpace(p.ManualManageState), manualManageStateCritical) {
		m.notifyCriticalManagedProtection(p, cause, "Protection retries are exhausted. The bot is no longer auto-rearming this stop path.")
	}
	if m.manualConfirm && m.shouldEmergencyForceCloseManagedPosition(p, cause) {
		m.queueManualForceFlatRequest(manualManageRequestFromPosition(p), cause, now)
	}
}

func (m *liveExecManager) notifyCriticalManagedProtection(p *livePosition, cause, summary string) {
	if m == nil || m.tg == nil || p == nil {
		return
	}
	now := time.Now().UTC()
	notifyCause := "critical:" + strings.TrimSpace(cause)
	if !shouldNotifyManageStatus(p, notify.ManageStateDegraded, notifyCause, now) {
		return
	}
	lines := []string{
		fmt.Sprintf("<b>%s %s</b>", cleanSymbol(p.Symbol), displayPositionSide(p.Side)),
		fmt.Sprintf("<b>Cause:</b> %s", firstNonEmpty(strings.TrimSpace(cause), "managed_unprotected")),
		fmt.Sprintf("<b>Protection:</b> %s | <b>Retries:</b> %d/%d", manualProtectionStatus(p), p.ProtectionRetryCount, manualProtectionRetryBudget()),
		"<b>Critical state:</b> stop retries are frozen until operator action or force-close conditions are met.",
	}
	if strings.TrimSpace(summary) != "" {
		lines = append(lines, summary)
	}
	if p.EntryPrice > 0 || p.LastManageProtectRef > 0 {
		refText := "unavailable"
		if p.LastManageProtectRef > 0 {
			refText = fmtPrice(p.LastManageProtectRef)
		}
		lines = append(lines, fmt.Sprintf("<b>Protect Ref:</b> %s | <b>Entry:</b> %s", refText, fmtPrice(p.EntryPrice)))
	}
	if p.LastManageComputedStop > 0 || p.LastManageNormalizedStop > 0 {
		lines = append(lines, fmt.Sprintf("<b>Computed Stop:</b> %s | <b>Normalized:</b> %s", fmtPrice(p.LastManageComputedStop), fmtPrice(p.LastManageNormalizedStop)))
	}
	if p.LastMark > 0 {
		lines = append(lines, fmt.Sprintf("<b>Last Mark:</b> %s", fmtPrice(p.LastMark)))
	}
	if strings.TrimSpace(p.WinnerLifecycle) != "" {
		lines = append(lines, fmt.Sprintf("<b>Winner Lifecycle:</b> %s", strings.ToUpper(strings.TrimSpace(p.WinnerLifecycle))))
	}
	if guidance := manualTrendCaptureGuidance(p.WinnerLifecycle); guidance != "" {
		lines = append(lines, guidance)
	}
	m.tg.Sendf("%s", notify.BuildManagementStatusCard(notify.ManageStateDegraded, p.Symbol, p.Side, lines...))
}

func managedProtectionUnrealizedLossPct(p *livePosition) float64 {
	if p == nil || p.EntryPrice <= 0 || p.LastMark <= 0 {
		return 0
	}
	_, pnlPct := realizedFromFill(p.Side, p.EntryPrice, p.LastMark, maxFloat(p.RemainingQty, 1))
	if pnlPct >= 0 {
		return 0
	}
	return -pnlPct
}

func (m *liveExecManager) shouldEmergencyForceCloseManagedPosition(p *livePosition, cause string) bool {
	if m == nil || p == nil || !manualManagedTrade(p) || p.State == execClosed || p.RemainingQty <= 0 {
		return false
	}
	if !envBool("LIVE_IMPORT_FORCE_CLOSE_ON_PROTECT_FAIL", false) {
		return false
	}
	maxLossPct := envFloat("LIVE_IMPORT_FORCE_CLOSE_MAX_LOSS_PCT", 2.5)
	if maxLossPct <= 0 {
		return false
	}
	lossPct := managedProtectionUnrealizedLossPct(p)
	if lossPct >= maxLossPct {
		return true
	}
	return false
}

func (m *liveExecManager) emergencyForceCloseManagedPosition(p *livePosition, cause string, now time.Time) error {
	if m == nil || p == nil || m.rest == nil || !manualManagedTrade(p) || p.State == execClosed || p.RemainingQty <= 0 {
		return nil
	}
	if !envBool("LIVE_IMPORT_FORCE_CLOSE_ON_PROTECT_FAIL", false) {
		p.ManualManageState = manualManageStateCritical
		p.Managed = true
		p.Protected = false
		return fmt.Errorf("force_close_disabled")
	}
	p.ManualManageState = manualManageStateForceClose
	maxRetries := envInt("LIVE_FORCE_CLOSE_RETRY_MAX", 5)
	if maxRetries < 1 {
		maxRetries = 1
	}
	retryDelay := time.Duration(envInt("LIVE_FORCE_CLOSE_RETRY_SEC", 2)) * time.Second
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if ok, err := m.ForceCloseSymbol(p.Symbol, firstNonEmpty(cause, "MANUAL_PROTECTION_FAILED")); ok && err == nil {
			if m.tg != nil {
				m.tg.Sendf("%s", notify.BuildManagementStatusCard(notify.ManageStateForceCloseTriggered, p.Symbol, p.Side,
					fmt.Sprintf("<b>%s %s</b>", cleanSymbol(p.Symbol), displayPositionSide(p.Side)),
					fmt.Sprintf("<b>Cause:</b> %s", firstNonEmpty(cause, "manual_protection_failed")),
					"The bot could not attach legal protection, so it force-closed the managed position before it could stay unprotected.",
				))
			}
			return nil
		} else if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("force close not acknowledged")
		}
		if attempt+1 < maxRetries {
			time.Sleep(retryDelay)
		}
	}
	p.ManualManageState = manualManageStateCritical
	p.Managed = true
	p.Protected = false
	p.ProtectionPending = true
	p.ProtectionRetryAfter = now.Add(time.Duration(envInt("LIVE_CRITICAL_PROTECTION_ALERT_SEC", 300)) * time.Second)
	if m.tg != nil {
		m.notifyCriticalManagedProtection(p, firstNonEmpty(cause, "manual_protection_failed"),
			"Emergency protection failed and force-close also failed. New entries are blocked until this position is handled.")
	}
	return lastErr
}

func manualWouldAddCapital(p *livePosition, mark float64, minAddPnLPct float64) bool {
	if p == nil || mark <= 0 || p.EntryPrice <= 0 {
		return false
	}
	_, pnlPct := realizedFromFill(p.Side, p.EntryPrice, mark, maxFloat(p.RemainingQty, 1))
	if pnlPct < minAddPnLPct {
		return false
	}
	return true
}

func manualManagedTrade(p *livePosition) bool {
	if p == nil {
		return false
	}
	return p.Managed || (!strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT") &&
		strings.EqualFold(strings.TrimSpace(p.EntryReason), manualEntryReasonManaged))
}

func manualProtectionConvictionReady(p *livePosition) bool {
	if !manualManagedTrade(p) {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT") && envBool("LIVE_IMPORT_REQUIRE_PROTECTION", true) {
		return true
	}
	if p.HitTP1 || p.HitTP2 || p.HitTP3 || p.ProtectionStage >= protectionStageArmed {
		return true
	}
	minR := envFloat("LIVE_MANUAL_PROTECTION_MIN_R", 0.35)
	if minR > 0 && p.MaxFavorableR >= minR {
		return true
	}
	minPnL := envFloat("LIVE_MANUAL_PROTECTION_MIN_PNL_PCT", 1.0)
	if p.LastMark > 0 && manualWouldAddCapital(p, p.LastMark, minPnL) {
		return true
	}
	return false
}

func (m *liveExecManager) reconstructManualManagedState(now time.Time, p *livePosition, mark float64) {
	if m == nil || p == nil || mark <= 0 || p.EntryPrice <= 0 {
		return
	}
	p.ManageAnchorPrice = mark
	p.LastMark = mark
	updateFavorableRLive(p, mark)
	m.updateLiveTargetHits(p, mark)
	_, upct := realizedFromFill(p.Side, p.EntryPrice, mark, maxFloat(p.RemainingQty, 1))
	allowBE := allowMoveToBreakEven(p.HitTP1, upct)
	if newStop, tightened := applyLiveProtectionState(now, p.Side, p.EntryPrice, p.StopPrice, p.MaxFavorableR, &p.ProtectionStage, &p.FirstProtectAt, &p.ProtectedStop, m.beLockBps, allowBE); tightened {
		p.StopPrice = newStop
	}
	tp1R := tp1RFromBracket(p.EntryPrice, p.StopPrice, p.TP1Price)
	beArmR := beArmThreshold(envFloat("LIVE_BE_ARM_R", 1.35), tp1R)
	if m.beLockBps > 0 && beArmR > 0 && p.MaxFavorableR >= beArmR && allowBE {
		if be := beLockPriceBuffered(p.Side, p.EntryPrice, p.StopPrice, m.beLockBps); be > 0 {
			if stop, improved := improvedStopPrice(p.Side, p.StopPrice, be); improved {
				p.StopPrice = stop
				p.ProtectedStop = stop
				if p.ProtectionStage < protectionStageArmed {
					p.ProtectionStage = protectionStageArmed
				}
				if p.FirstProtectAt.IsZero() {
					p.FirstProtectAt = now
				}
			}
		}
	}
	updateGivebackMetrics(p.MaxFavorableR, unrealizedRiskR(p.Side, p.EntryPrice, p.StopPrice, mark), &p.CaptureRatio, &p.MaxGivebackR)
	trailMinR := envFloat("LIVE_IMPORTED_TRAIL_MIN_R", 1.5)
	if p.MaxFavorableR >= maxFloat(trailMinR, tp1R) {
		m.maybeEnableTrail(p, 2)
	}
	prevLifecycle := p.WinnerLifecycle
	syncLiveWinnerLifecycle(p, false)
	logWinnerLifecycleTransition(p.Symbol, p.Side, prevLifecycle, p.WinnerLifecycle, lifecycleTransitionReason(prevLifecycle, p.WinnerLifecycle, "", p.TrailOn))
	updateManagePhase(p, false)
	refreshRunnerReservation(p, m.ladderCfg.StarterUSDT)
	if p.TrailOn {
		sideBuy := isLongSide(p.Side)
		if p.TrailRef <= 0 || (sideBuy && mark > p.TrailRef) || (!sideBuy && mark < p.TrailRef) {
			p.TrailRef = mark
			p.TrailStop = m.calcTrailStopForPosition(p, sideBuy, mark, p.HitTP3)
			if stop, improved := improvedStopPrice(p.Side, p.StopPrice, p.TrailStop); improved {
				p.StopPrice = stop
			}
		}
	}
	fmt.Printf("live: manual-stage symbol=%s side=%s entry=%s mark=%s pnl=%+.2f%% mfe_r=%.2f stage=%d hit_tp1=%v hit_tp2=%v hit_tp3=%v would_add=%v\n",
		p.Symbol, p.Side, fmtPrice(p.EntryPrice), fmtPrice(mark),
		func() float64 {
			_, pct := realizedFromFill(p.Side, p.EntryPrice, mark, maxFloat(p.RemainingQty, 1))
			return pct
		}(),
		p.MaxFavorableR, p.ProtectionStage, p.HitTP1, p.HitTP2, p.HitTP3,
		manualWouldAddCapital(p, mark, m.ladderCfg.MinAddPnLPct))
}

func armManualProtectionAfterReconstruct(now time.Time, p *livePosition) {
	if p == nil {
		return
	}
	p.ProtectionPending = true
	p.ManualManageState = manualManageStatePendingProtection
	p.Managed = true
	p.Protected = false
	if manualProtectionConvictionReady(p) {
		p.ProtectionRetryAfter = now.Add(5 * time.Second)
		p.LastManageFailCause = ""
		return
	}
	markProtectionPending(p, now, "manage_awaiting_conviction")
}

func (m *liveExecManager) importRemotePositions(now time.Time) (int, error) {
	if m == nil || m.rest == nil {
		return 0, nil
	}
	rows, err := cachedPositionRisk(m.rest, "")
	if err != nil {
		return 0, err
	}
	imported := 0
	remoteKeys := map[string]string{}
	for _, row := range rows {
		sym := strings.ToUpper(strings.TrimSpace(fmt.Sprint(row["symbol"])))
		amt := mapFloat(row["positionAmt"])
		if sym == "" || abs(amt) <= 1e-10 {
			continue
		}
		side := "BUY"
		if amt < 0 {
			side = "SELL"
		}
		entry := mapFloat(row["entryPrice"])
		mark := mapFloat(row["markPrice"])
		if entry <= 0 {
			entry = mark
		}
		if entry <= 0 {
			continue
		}
		qty := abs(amt)
		margin := maxFloat(mapFloat(row["isolatedWallet"]), mapFloat(row["positionInitialMargin"]))
		lev := int(maxFloat(1, mapFloat(row["leverage"])))
		remoteKeys[positionLookupKey(sym, side)] = manualManageFingerprint(sym, side, qty, entry)
		existing := m.positions[sym]
		if m.isActive(existing) {
			if samePositionSide(existing.Side, side) {
				sync := syncImportedRemotePosition(existing, qty, entry, margin, lev, now)
				if sync.materialManualMutation() && manualManagedTrade(existing) {
					m.recalibrateManagedManualPosition(existing, now, sync)
				}
				if manualManagedTrade(existing) || manualPassivePosition(existing) {
					continue
				}
			}
			continue
		}
		if approvedReq, ok := m.approvedManualRequest(sym, side, qty, entry); ok {
			if _, err := m.activateManualManagement(approvedReq, now, "MANUAL_APPROVED_RETRY"); err == nil {
				imported++
				continue
			}
		}
		if passiveReq, ok := m.passiveManualRequest(sym, side, qty, entry); ok {
			if _, err := m.activatePassiveManualImport(passiveReq, now, "MANUAL_TIMEOUT_IMPORT", false); err == nil {
				imported++
				continue
			}
		}
		req := manualManageRequest{
			Key:         positionLookupKey(sym, side),
			Fingerprint: manualManageFingerprint(sym, side, qty, entry),
			Symbol:      sym,
			Side:        normalizePositionSide(side),
			Qty:         qty,
			Entry:       entry,
			Margin:      margin,
			Leverage:    lev,
		}
		if m.queueManualManagementRequest(sym, side, qty, entry, margin, lev, now) {
			if _, err := m.activatePassiveManualImport(req, now, "MANUAL_PENDING_IMPORT", true); err == nil {
				imported++
			}
			continue
		}
		if _, err := m.activateManualManagement(req, now, "MANUAL_DETECTED_MONITORED"); err == nil {
			imported++
			continue
		}
		if _, err := m.activatePassiveManualImport(req, now, "MANUAL_DETECTED_PASSIVE_FALLBACK", false); err == nil {
			imported++
		}
	}
	m.pruneManualRequests(remoteKeys)
	return imported, nil
}

func qtyWithinTolerance(expect, actual float64) bool {
	if expect <= 0 || actual <= 0 {
		return false
	}
	tol := clamp(envFloat("LIVE_MANUAL_QTY_TOLERANCE_PCT", 5.0), 0.1, 100.0) / 100.0
	diff := math.Abs(expect-actual) / maxFloat(expect, actual)
	return diff <= tol
}

func entryWithinTolerance(expect, actual float64) bool {
	if expect <= 0 || actual <= 0 {
		return false
	}
	tol := clamp(envFloat("LIVE_MANUAL_ENTRY_TOLERANCE_PCT", 1.0), 0.1, 25.0) / 100.0
	diff := math.Abs(expect-actual) / maxFloat(expect, actual)
	return diff <= tol
}

func (m *liveExecManager) setManualConflict(sym, side, cause string, now time.Time) {
	if m == nil {
		return
	}
	p, ok := m.trackedPosition(sym)
	if !ok || p == nil {
		p = m.newImportedRemotePosition(sym, side, 0, 0, 0, 1, now, manualEntrySourceManaged)
		m.positions[sym] = p
	}
	p.ManualManageState = manualManageStateConflict
	p.Managed = false
	p.Protected = false
	p.ProtectionPending = false
	p.LastManageFailCause = strings.TrimSpace(cause)
	p.LastManageFailAt = now
}

func (m *liveExecManager) validateManualManageRequest(req manualManageRequest, now time.Time) error {
	if m == nil || m.rest == nil {
		return nil
	}
	sym := strings.ToUpper(strings.TrimSpace(req.Symbol))
	rows, err := cachedPositionRisk(m.rest, sym)
	if err != nil {
		return err
	}
	view := remotePositionForSide(rows, req.Side)
	if view.QtyAbs <= 1e-10 {
		m.setManualConflict(sym, req.Side, "remote_position_missing", now)
		return fmt.Errorf("manual_state_conflict: remote position missing")
	}
	if req.Qty > 0 && !qtyWithinTolerance(req.Qty, view.QtyAbs) {
		m.setManualConflict(sym, req.Side, "remote_qty_mismatch", now)
		return fmt.Errorf("manual_state_conflict: remote qty mismatch")
	}
	if req.Entry > 0 && !entryWithinTolerance(req.Entry, view.EntryPrice) {
		m.setManualConflict(sym, req.Side, "remote_entry_mismatch", now)
		return fmt.Errorf("manual_state_conflict: remote entry mismatch")
	}
	if existing := m.positions[sym]; m.isActive(existing) && !samePositionSide(existing.Side, req.Side) {
		m.setManualConflict(sym, req.Side, "local_conflicting_position", now)
		return fmt.Errorf("manual_state_conflict: local conflicting position")
	}
	return nil
}

func configuredLiveLeverage() int {
	lev := envInt("LIVE_LEVERAGE_FIXED", 3)
	maxLev := envInt("LIVE_MAX_LEVERAGE", 20)
	if maxLev < 1 {
		maxLev = 20
	}
	return clampInt(maxInt(1, lev), 1, maxLev)
}

func configuredMinLiveLeverage() int {
	lev := envInt("LIVE_LEVERAGE_MIN", 2)
	maxLev := envInt("LIVE_MAX_LEVERAGE", 20)
	if lev < 1 {
		lev = 1
	}
	if maxLev >= 1 && lev > maxLev {
		lev = maxLev
	}
	return lev
}

func applyLeverageWithFallback(startLev, minLev int, apply func(int) error) (int, error) {
	seq := leverageRetrySequence(startLev, minLev)
	if len(seq) == 0 {
		return 0, fmt.Errorf("invalid leverage sequence")
	}
	var lastErr error
	for _, lev := range seq {
		if err := apply(lev); err == nil {
			return lev, nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("unable to apply leverage")
	}
	return 0, lastErr
}

func (m *liveExecManager) alignManualManagedLeverage(p *livePosition) {
	if m == nil || m.rest == nil || p == nil || p.Symbol == "" {
		return
	}
	targetLev := configuredLiveLeverage()
	minLev := configuredMinLiveLeverage()
	if targetLev <= 0 || p.Leverage == targetLev {
		return
	}
	rawSym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(p.Symbol)))
	if rawSym == "" {
		rawSym = strings.ToUpper(strings.TrimSpace(p.Symbol))
	}
	if usedLev, err := applyLeverageWithFallback(targetLev, minLev, func(lev int) error {
		_, err := m.rest.ChangeLeverage(rawSym, lev)
		return err
	}); err == nil {
		p.Leverage = usedLev
		if p.EntryPrice > 0 && p.FilledQty > 0 {
			p.Margin = (p.EntryPrice * p.FilledQty) / float64(maxInt(1, p.Leverage))
		}
	}
}

func (m *liveExecManager) activatePassiveManualImport(req manualManageRequest, now time.Time, reason string, keepRequest bool) (*livePosition, error) {
	if m == nil {
		return nil, fmt.Errorf("live execution manager unavailable")
	}
	sym := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if sym == "" {
		return nil, fmt.Errorf("invalid symbol")
	}
	if existing := m.positions[sym]; m.isActive(existing) {
		if strings.EqualFold(strings.TrimSpace(existing.Side), strings.TrimSpace(req.Side)) && manualPassivePosition(existing) {
			_ = syncImportedRemotePosition(existing, req.Qty, req.Entry, req.Margin, req.Leverage, now)
			if !keepRequest {
				m.mu.Lock()
				delete(m.manualRequests, req.Key)
				m.mu.Unlock()
			}
			_ = m.save()
			return existing, nil
		}
		if !keepRequest {
			m.mu.Lock()
			delete(m.manualRequests, req.Key)
			m.mu.Unlock()
		}
		return existing, nil
	}
	p := m.newImportedRemotePosition(sym, req.Side, req.Qty, req.Entry, req.Margin, req.Leverage, now, manualEntrySourcePassive)
	m.positions[sym] = p
	if !keepRequest {
		m.mu.Lock()
		delete(m.manualRequests, req.Key)
		m.mu.Unlock()
	}
	_ = m.save()
	_ = m.logFill(now, p, "ENTRY", reason, p.FilledQty, p.EntryPrice, 0, 0)
	m.sendFillReceipt(now, p, "ENTRY", reason, p.FilledQty, p.EntryPrice, 0, 0)
	return p, nil
}

func (m *liveExecManager) placeOrReplaceStopWithRetry(p *livePosition) error {
	if p != nil && manualManagedTrade(p) {
		return m.placeOrReplaceStop(p)
	}
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

func (m *liveExecManager) activateManualForceFlat(req manualManageRequest, now time.Time) error {
	if m == nil {
		return fmt.Errorf("live execution manager unavailable")
	}
	sym := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if sym == "" {
		return fmt.Errorf("invalid symbol")
	}
	p := m.newImportedRemotePosition(sym, req.Side, req.Qty, req.Entry, req.Margin, req.Leverage, now, "MANUAL")
	if err := m.forceFlatRecovered(p); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.manualRequests, req.Key)
	m.mu.Unlock()
	return nil
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

func (m *liveExecManager) ActiveSymbols() []string {
	if m == nil || len(m.positions) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.positions))
	seen := map[string]struct{}{}
	for _, p := range m.positions {
		if p == nil || !m.isActive(p) {
			continue
		}
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(p.Symbol)))
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	sort.Strings(out)
	return out
}

func (m *liveExecManager) PendingProtectionSymbols() []string {
	if m == nil || len(m.positions) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.positions))
	seen := map[string]struct{}{}
	for _, p := range m.positions {
		if p == nil || !m.isActive(p) || !p.ProtectionPending {
			continue
		}
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(p.Symbol)))
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	sort.Strings(out)
	return out
}

func (m *liveExecManager) ActiveCountBySide(side string) int {
	if m == nil {
		return 0
	}
	want := strings.ToUpper(strings.TrimSpace(side))
	n := 0
	for _, p := range m.positions {
		if !m.isActive(p) {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(p.Side)) == want {
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

func (m *liveExecManager) liveTradeUpdateMessage(meta map[string]symbolMeta) string {
	if m == nil {
		return "live disabled"
	}
	snap := m.LiveAccountSnapshot(32)
	localNow := time.Now().In(m.reportLoc)
	positions := make([]notify.PositionView, 0, len(snap.Positions))
	for _, pos := range snap.Positions {
		positions = append(positions, notify.PositionView{
			Symbol:   cleanSymbol(pos.Symbol),
			Side:     pos.Side,
			PnL:      pos.UnrealizedPnL,
			Entry:    pos.EntryPrice,
			Price:    firstPositive(pos.MarkPrice, pos.LastPrice, pos.EntryPrice),
			DayPct:   pos.UnrealizedPnLPct,
			Stop:     pos.StopPrice,
			NextTP:   0,
			HoldTime: fmt.Sprintf("%.0fm", pos.HoldMin),
			Margin:   pos.Margin,
			Leverage: maxInt(1, pos.Leverage),
		})
	}
	if len(positions) == 0 {
		return "no live positions"
	}
	sort.Slice(positions, func(i, j int) bool { return abs(positions[i].PnL) > abs(positions[j].PnL) })
	return positionUpdatesHTML("LIVE", notify.AccountView{
		Mode:          "LIVE",
		Timestamp:     localNow.Format("15:04 MST"),
		AvailableUSDT: snap.AvailableUSDT,
		Equity:        snap.Equity,
		PaperPnL:      0,
		LivePnL:       snap.RealizedDay + snap.OpenPnL,
		OpenPositions: snap.OpenCount,
	}, positions, 3)
}

func (m *liveExecManager) liveTradeUpdateSignature(meta map[string]symbolMeta) string {
	if m == nil {
		return ""
	}
	snap := m.LiveAccountSnapshot(32)
	if len(snap.Positions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(snap.Positions)+1)
	for _, pos := range snap.Positions {
		parts = append(parts, fmt.Sprintf("%s|%s|%s|%s|%.6f|%.6f|%.6f|%.6f|%dx|%.2f|%.2f|%.2f",
			pos.Symbol,
			pos.Side,
			pos.Source,
			nonEmpty(pos.ProtectionState, "-"),
			pos.Qty,
			pos.EntryPrice,
			pos.MarkPrice,
			pos.LastPrice,
			maxInt(1, pos.Leverage),
			pos.UnrealizedPnL,
			pos.RealizedPnL,
			pos.StopPrice,
		))
	}
	parts = append(parts, fmt.Sprintf("totals|%d|%.2f|%.2f|%.2f", snap.OpenCount, snap.OpenPnL, snap.RealizedDay, snap.Equity))
	sort.Strings(parts[:len(parts)-1])
	return strings.Join(parts, ";")
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

func (m *liveExecManager) confluencePreferredLimitPrice(c candidate, mid float64) float64 {
	if m == nil {
		return 0
	}
	if !envBool("LIVE_CONFLUENCE_LIMIT_REF_ENABLE", true) {
		return 0
	}
	side := toFeatureSide(c.Side)
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if raw == "" {
		return 0
	}
	levelCandidates := make([]float64, 0, 4)
	if fib50 := m.fib50Level(raw, side); fib50 > 0 {
		levelCandidates = append(levelCandidates, fib50)
	}
	if c.Sig.VPLevel > 0 {
		levelCandidates = append(levelCandidates, c.Sig.VPLevel)
	}
	if c.Sig.VPTargetLevel > 0 {
		levelCandidates = append(levelCandidates, c.Sig.VPTargetLevel)
	}
	if c.Sig.Entry > 0 {
		levelCandidates = append(levelCandidates, c.Sig.Entry)
	}
	if len(levelCandidates) == 0 {
		return 0
	}
	maxDevBps := envFloat("LIVE_CONFLUENCE_LIMIT_MAX_DEV_BPS", 250.0)
	best := 0.0
	if side == features.SideLong {
		// Prefer pullback entries below/near mid for longs.
		bestBelow := 0.0
		for _, px := range levelCandidates {
			if px <= 0 {
				continue
			}
			if px <= mid && (bestBelow == 0 || px > bestBelow) {
				bestBelow = px
			}
		}
		if bestBelow > 0 {
			best = bestBelow
		}
	} else {
		// Prefer pullback entries above/near mid for shorts.
		bestAbove := 0.0
		for _, px := range levelCandidates {
			if px <= 0 {
				continue
			}
			if px >= mid && (bestAbove == 0 || px < bestAbove) {
				bestAbove = px
			}
		}
		if bestAbove > 0 {
			best = bestAbove
		}
	}
	if best <= 0 {
		best = levelCandidates[0]
		minDist := math.Abs(levelCandidates[0] - mid)
		for i := 1; i < len(levelCandidates); i++ {
			d := math.Abs(levelCandidates[i] - mid)
			if d < minDist {
				minDist = d
				best = levelCandidates[i]
			}
		}
	}
	if best <= 0 || mid <= 0 {
		return 0
	}
	devBps := 10000.0 * math.Abs(best-mid) / mid
	if maxDevBps > 0 && devBps > maxDevBps {
		return 0
	}
	return best
}

func (m *liveExecManager) fib50Level(symbol string, side features.Side) float64 {
	if m == nil {
		return 0
	}
	limit := envInt("LIVE_CONFLUENCE_FIB_LOOKBACK_15M", 96)
	if limit < 24 {
		limit = 24
	}
	var bars []types.Candle
	var err error
	if m.featureCache != nil {
		bars, err = m.featureCache.candleSeries(symbol, types.TF15m, limit)
	} else {
		bars, err = aster.LoadCandles(symbol, types.TF15m, limit)
	}
	if err != nil || len(bars) < 8 {
		return 0
	}
	fc := make([]features.Candle, 0, len(bars))
	for _, b := range bars {
		fc = append(fc, features.Candle{
			Ts: b.T,
			O:  b.O,
			H:  b.H,
			L:  b.L,
			C:  b.C,
			V:  b.V,
		})
	}
	imp, ok := strategies.DetectImpulseAndFib(fc, side, limit)
	if !ok || imp.Level50 <= 0 {
		return 0
	}
	return imp.Level50
}

func (m *liveExecManager) PlaceEntry(c candidate, entryBps, margin float64, lev int, plan ladderPlan) error {
	if !isExecutableStrategy(c.Strat) {
		return fmt.Errorf("strategy_unresolved")
	}
	c.Strat = canonicalExecutionStrategy(c.Strat, c.Side)
	if m == nil || m.rest == nil {
		return fmt.Errorf("execution manager not ready")
	}
	rawSym := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	if plan.IsReentry && !m.reentryCfg.Enable {
		return fmt.Errorf("reentry disabled")
	}
	if reason := m.degradedEntryReason(time.Now().UTC(), rawSym); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	existing, hasExisting := m.trackedPosition(rawSym)
	if hasExisting && m.isActive(existing) {
		if !strings.EqualFold(existing.Side, c.Side) {
			return fmt.Errorf("active opposite-side state already exists for %s", rawSym)
		}
		if !botManagedPosition(existing) {
			return fmt.Errorf("manual position already active for %s", rawSym)
		}
		if existing.PendingAddOrderID > 0 {
			return fmt.Errorf("pending add already exists for %s", rawSym)
		}
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
	if preferred := m.confluencePreferredLimitPrice(c, mid); preferred > 0 {
		price = preferred
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
	qty, price, legalityReason := validateOrderLegality(meta, qty, price)
	if legalityReason != "" {
		m.recordOrderLegalityFailure(rawSym, legalityReason, time.Now().UTC())
		return fmt.Errorf("%s", legalityReason)
	}
	stopReason := ""
	stopDistancePct := 0.0
	if m.hybridStopCfg.Enabled {
		stopRes := exitmgr.ComputeHybridStop(m.hybridStopCfg, hybridStopInputForCandidate(c, price, c.Sig.TP1))
		if stopRes.Rejected {
			return fmt.Errorf("%s", stopRes.RejectReason)
		}
		if stopRes.StopPrice > 0 {
			stopReason = stopRes.StopReason
			stopDistancePct = stopRes.StopDistancePct
		}
	}
	if lev <= 0 {
		lev = 1
	}
	usedLev, err := applyLeverageWithFallback(lev, configuredMinLiveLeverage(), func(tryLev int) error {
		_, err := m.rest.ChangeLeverage(rawSym, tryLev)
		return err
	})
	if err != nil {
		return fmt.Errorf("set leverage failed: %w", err)
	}
	if usedLev != lev {
		fmt.Printf("live: leverage fallback applied %s %s %dx->%dx\n", rawSym, c.Side, lev, usedLev)
		lev = usedLev
		qty = margin * float64(maxInt(lev, 1)) / price
		qty, _, err = m.rest.RoundQty(rawSym, qty)
		if err != nil {
			return err
		}
		qty, price, legalityReason = validateOrderLegality(meta, qty, price)
		if legalityReason != "" {
			m.recordOrderLegalityFailure(rawSym, legalityReason, time.Now().UTC())
			return fmt.Errorf("%s", legalityReason)
		}
	}
	if m.marginType != "" {
		alreadySet := false
		if rows, err := cachedPositionRisk(m.rest, rawSym); err == nil {
			alreadySet = marginTypeAlreadySet(rows, rawSym, m.marginType)
		}
		if !alreadySet {
			if _, err := m.rest.ChangeMarginType(rawSym, m.marginType); err != nil && m.enforceIsolated && !isIgnorableMarginTypeError(err) {
				return fmt.Errorf("set margin type %s failed: %w", m.marginType, err)
			}
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

	now := time.Now().UTC()
	intentID := buildIntentID(rawSym, firstNonEmpty(strings.TrimSpace(c.StrategyID), strings.TrimSpace(c.Strat)), price, now)
	if m.isUnknownExecutionFrozen(rawSym) {
		logUnknownExecution(rawSym, intentID, "submit_skipped", "symbol frozen by unknown execution guard")
		return fmt.Errorf("unknown_execution_frozen")
	}

	out, err := m.rest.PlaceOrder(vals)
	if err != nil {
		if isAmbiguousVenueOutcome(err) {
			m.handleUnknownExecution(rawSym, intentID)
			time.Sleep(time.Duration(envInt("LIVE_EXEC_RECONCILE_AFTER_UNKNOWN_MS", 800)) * time.Millisecond)
			_ = m.reconcileAfterUnknown(rawSym, intentID)
		}
		if isVenueOrderLegalityError(err) {
			m.recordOrderLegalityFailure(rawSym, err.Error(), now)
		}
		return err
	}
	m.clearOrderLegalityFailures(rawSym)
	orderID := mapInt64(out["orderId"])
	if orderID == 0 {
		return fmt.Errorf("missing orderId from place response")
	}
	if hasExisting && m.isActive(existing) {
		existing.PendingAddOrderID = orderID
		existing.PendingAddPrice = price
		existing.PendingAddQty = qty
		existing.PendingAddMargin = margin
		existing.PendingAddCreatedAt = now
		existing.PendingAddEntryReason = c.Strat
		existing.UpdatedAt = now
		_ = m.save()
		fmt.Printf("live: add submitted %s %s qty=%s px=%s orderId=%d add_count=%d deployed=%.2f\n",
			rawSym, existing.Side, vals.Get("quantity"), vals.Get("price"), orderID, existing.AddCount, existing.DeployedMargin)
		m.emitNotify(notify.Event{
			Key:      "ADD_SUBMITTED",
			Title:    "ADD SUBMITTED",
			Class:    notify.ClassLifecycle,
			Severity: notify.SeverityInfo,
			Route:    notify.RouteNormal,
			Symbol:   rawSym,
			Message:  "additional size submitted",
			Metadata: map[string]string{
				"side":     displayPositionSide(existing.Side),
				"qty":      vals.Get("quantity"),
				"limit":    vals.Get("price"),
				"deployed": fmt.Sprintf("%.2f->%.2f", existing.DeployedMargin, existing.DeployedMargin+margin),
			},
		})
		return nil
	}
	entryReason := c.Strat
	entryStrategyID := firstNonEmpty(strings.TrimSpace(c.StrategyID), "unknown")
	entryTags := append([]string{}, c.Sig.Tags...)
	entryReasons := append([]string{}, c.Sig.Reasons...)
	reentryCount := 0
	if plan.IsReentry {
		entryTags = append(entryTags, "reentry")
		entryReasons = append(entryReasons, "structured_reentry")
		if plan.Previous != nil {
			reentryCount = plan.Previous.ReentryCount + 1
		} else {
			reentryCount = 1
		}
	}
	p := &livePosition{
		Symbol:                 rawSym,
		Side:                   strings.ToUpper(c.Side),
		State:                  execPendingEntry,
		CreatedAt:              now,
		UpdatedAt:              now,
		EntryOrderID:           orderID,
		EntryPrice:             price,
		Qty:                    qty,
		Margin:                 margin,
		DeployedMargin:         margin,
		Leverage:               lev,
		VPSetup:                c.Sig.VPSetup,
		VPLevel:                c.Sig.VPLevel,
		VPTargetLevel:          c.Sig.VPTargetLevel,
		VPStopMode:             c.Sig.StopMode,
		VPTargetMode:           c.Sig.TargetMode,
		RejectReason:           c.RejectReason,
		EntryReason:            entryReason,
		EntryStrategyID:        entryStrategyID,
		EntrySource:            "BOT",
		EntryGrade:             c.Entry.CurrentGrade,
		EntryState:             string(c.Entry.State),
		EntryTrigger:           c.TriggerState,
		ExitProfile:            c.ExitProfile,
		EntryConf:              c.Conf,
		DiscoveryScore:         c.DiscoveryScore,
		TriggerScore:           c.TriggerScore,
		ExecutionScore:         c.ExecutionScore,
		CombinedScore:          c.CombinedScore,
		EntryTags:              entryTags,
		EntryReasons:           entryReasons,
		EntryVolumeUSD:         c.VolumeUSD,
		EntryATRPct:            c.ATRPct,
		EntryATRExtension:      c.ExtensionATR,
		EntrySession:           c.SessionLabel,
		EntryTiming:            c.EntryTiming,
		CandidateAgeSeconds:    c.CandidateAgeSeconds,
		EntryDistanceToVWAPPct: c.DistanceToVWAPPct,
		StopReason:             stopReason,
		StopDistancePct:        stopDistancePct,
		RegimeTag:              c.Sig.RegimeTag,
		EntrySetupFamily:       c.SetupFamily,
		EntrySetupSource:       c.SetupSource,
		EntryTradeHorizon:      c.TradeHorizon,
		ExecBucket:             executionGovernorBucketForCandidate(c),
		StarterOnly:            false,
		AddLockedUntilConfirm:  false,
		ReentryCount:           reentryCount,
		ManagePhase:            managePhaseContinuation,
	}
	if stopDistancePct > 0 {
		p.CustomRiskPct = stopDistancePct / 100.0
	} else if c.Sig.Entry > 0 && c.Sig.Stop > 0 {
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
	m.recordExecutionGovernorEntry(now, c)
	_ = m.save()
	fmt.Printf("live: entry submitted %s %s qty=%s px=%s orderId=%d reason=%s stop_reason=%s\n",
		rawSym, p.Side, vals.Get("quantity"), vals.Get("price"), orderID, firstNonEmpty(strings.TrimSpace(p.EntryReason), "manual"), firstNonEmpty(stopReason, "generic"))
	if m.tg != nil {
		title := "ENTRY SUBMITTED"
		if plan.IsReentry {
			title = "RE-ENTRY SUBMITTED"
		}
		m.tg.Sendf("%s", notify.BuildEventHTML("📨", title,
			fmt.Sprintf("<b>%s %s</b>", rawSym, displayPositionSide(p.Side)),
			fmt.Sprintf("<b>Qty:</b> %s | <b>Limit:</b> %s", vals.Get("quantity"), vals.Get("price")),
			fmt.Sprintf("<b>Order ID:</b> %d", orderID),
		))
	}
	return nil
}

func (m *liveExecManager) Reconcile(now time.Time, mom map[string]momentumView, flow map[string]flowMetrics, meta map[string]symbolMeta) {
	if m == nil || m.rest == nil {
		return
	}
	changed := false
	hadError := false
	if m.lastRemoteImportAt.IsZero() || now.Sub(m.lastRemoteImportAt) >= m.remoteImportEvery {
		m.lastRemoteImportAt = now
		if nImported, err := m.importRemotePositions(now); err != nil {
			fmt.Printf("live: import remote positions error: %v\n", err)
			hadError = true
		} else if nImported > 0 {
			changed = true
		}
	}
	if len(m.positions) == 0 {
		if hadError {
			m.reconcileFailCount++
			m.lastReconcileErrAt = now
		} else {
			m.reconcileFailCount = 0
			m.lastReconcileOKAt = now
		}
		if changed {
			_ = m.save()
		}
		return
	}
	for sym, p := range m.positions {
		if p == nil || p.State == execClosed {
			continue
		}
		switch p.State {
		case execPendingEntry:
			ch, err := m.reconcilePendingEntry(now, p)
			if err != nil {
				fmt.Printf("live: reconcile pending %s error: %v\n", sym, err)
				hadError = true
			}
			changed = changed || ch
		case execOpen, execPartialTP1, execPartialTP2:
			ch, err := m.reconcileOpen(now, p, mom, flow, meta)
			if err != nil {
				if !strings.Contains(strings.ToLower(err.Error()), "manage-failed-safe") {
					fmt.Printf("live: reconcile open %s error: %v\n", sym, err)
				}
				hadError = true
			}
			changed = changed || ch
		}
	}
	if hadError {
		m.reconcileFailCount++
		m.lastReconcileErrAt = now
	} else {
		m.reconcileFailCount = 0
		m.lastReconcileOKAt = now
	}
	if changed {
		_ = m.save()
	}
}

func (m *liveExecManager) fundingIntervalForSymbol(symbol string) time.Duration {
	if m == nil {
		return 0
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if m.fundingBySym != nil {
		if d, ok := m.fundingBySym[raw]; ok && d > 0 {
			return d
		}
	}
	return m.fundingEvery
}

func (m *liveExecManager) fundingEntryBlocked(now time.Time, symbol, side string, fundingRate float64) bool {
	if m == nil || m.fundingSkipNewPos <= 0 || !fundingCostsPosition(side, fundingRate) {
		return false
	}
	interval := m.fundingIntervalForSymbol(symbol)
	return fundingHazardWindow(now, interval, m.fundingHazardSec, m.fundingSkipNewPos)
}

func (m *liveExecManager) reconcilePendingEntry(now time.Time, p *livePosition) (bool, error) {
	order, err := m.rest.GetOrder(p.Symbol, p.EntryOrderID)
	if err != nil {
		if qty, avgPx, ok := m.syncPendingEntryFromRemote(p); ok {
			return m.transitionPendingToOpen(now, p, qty, avgPx, "ENTRY_RECOVERED")
		}
		return false, err
	}
	prog := parseOrderProgress(order)
	if prog.ExecQty > p.FilledQty {
		p.FilledQty = prog.ExecQty
	}
	if prog.Filled {
		if prog.ExecQty <= 0 {
			prog.ExecQty = p.Qty
		}
		return m.transitionPendingToOpen(now, p, prog.ExecQty, prog.AvgPx, "ENTRY_FILLED")
	}
	if prog.Terminal {
		if p.FilledQty > fillEpsilon(p.Qty) || prog.ExecQty > fillEpsilon(p.Qty) {
			qty := maxFloat(p.FilledQty, prog.ExecQty)
			return m.transitionPendingToOpen(now, p, qty, prog.AvgPx, "ENTRY_PARTIAL_RECOVERED")
		}
		return m.closePendingWithoutFill(now, p, "ENTRY_CANCELED")
	}
	if now.Sub(p.CreatedAt) >= m.entryTimeout {
		_, _ = m.rest.CancelOrder(p.Symbol, p.EntryOrderID)
		if p.FilledQty > fillEpsilon(p.Qty) || prog.ExecQty > fillEpsilon(p.Qty) {
			qty := maxFloat(p.FilledQty, prog.ExecQty)
			return m.transitionPendingToOpen(now, p, qty, prog.AvgPx, "ENTRY_TIMEOUT_PARTIAL")
		}
		return m.closePendingWithoutFill(now, p, "ENTRY_TIMEOUT")
	}
	if prog.Working && prog.ExecQty > fillEpsilon(p.Qty) {
		p.UpdatedAt = now
		return true, nil
	}
	p.UnknownEntryChecks++
	return false, nil
}

func (m *liveExecManager) reconcilePendingAdd(now time.Time, p *livePosition) (bool, error) {
	if m == nil || p == nil || p.PendingAddOrderID == 0 {
		return false, nil
	}
	order, err := m.rest.GetOrder(p.Symbol, p.PendingAddOrderID)
	if err != nil {
		if rows, syncErr := cachedPositionRisk(m.rest, p.Symbol); syncErr == nil {
			changed, _, applyErr := m.syncOpenFromRemote(now, p, rows)
			if applyErr == nil && changed {
				return true, nil
			}
		}
		if !p.PendingAddCreatedAt.IsZero() && now.Sub(p.PendingAddCreatedAt) >= m.entryTimeout {
			_, _ = m.rest.CancelOrder(p.Symbol, p.PendingAddOrderID)
			m.clearPendingAdd(p)
			return true, nil
		}
		return false, err
	}
	prog := parseOrderProgress(order)
	changed := false
	if delta := maxFloat(0, prog.ExecQty-p.PendingAddFilledQty); delta > fillEpsilon(p.PendingAddQty) {
		reason := "ADD_PARTIAL"
		if prog.Filled && strings.EqualFold(strings.TrimSpace(p.PendingAddEntryReason), "continuation_fast") {
			reason = "CONFIRMED_ADD"
		}
		deltaMargin := p.PendingAddMargin
		if p.PendingAddQty > 0 {
			deltaMargin = p.PendingAddMargin * (delta / p.PendingAddQty)
		}
		if err := m.applyAddFill(now, p, delta, prog.AvgPx, deltaMargin, reason); err != nil {
			return true, err
		}
		p.PendingAddFilledQty = prog.ExecQty
		changed = true
	}
	if prog.Filled {
		if strings.EqualFold(strings.TrimSpace(p.PendingAddEntryReason), "continuation_fast") {
			p.StarterOnly = false
			p.AddLockedUntilConfirm = false
			p.EntryReason = "continuation_fast"
		}
		m.clearPendingAdd(p)
		return true, nil
	}
	if prog.Terminal {
		m.clearPendingAdd(p)
		return true, nil
	}
	if !p.PendingAddCreatedAt.IsZero() && now.Sub(p.PendingAddCreatedAt) >= m.entryTimeout {
		_, _ = m.rest.CancelOrder(p.Symbol, p.PendingAddOrderID)
		m.clearPendingAdd(p)
		return true, nil
	}
	return changed, nil
}

func (m *liveExecManager) reconcilePendingExit(now time.Time, p *livePosition) (bool, error) {
	if m == nil || p == nil || p.PendingExitOrderID == 0 {
		return false, nil
	}
	order, err := m.rest.GetOrder(p.Symbol, p.PendingExitOrderID)
	if err != nil {
		if rows, syncErr := cachedPositionRisk(m.rest, p.Symbol); syncErr == nil {
			changed, closed, applyErr := m.syncOpenFromRemote(now, p, rows)
			if applyErr == nil && (changed || closed) {
				return true, nil
			}
		}
		if !p.PendingExitCreatedAt.IsZero() && now.Sub(p.PendingExitCreatedAt) >= m.entryTimeout {
			_, _ = m.rest.CancelOrder(p.Symbol, p.PendingExitOrderID)
			m.clearPendingExit(p)
			if p.RemainingQty > fillEpsilon(p.Qty) {
				return true, m.ensureExitOrders(p)
			}
			return true, nil
		}
		return false, err
	}
	prog := parseOrderProgress(order)
	changed := false
	if delta := maxFloat(0, prog.ExecQty-p.PendingExitFilledQty); delta > fillEpsilon(p.PendingExitQty) {
		if err := m.applyPendingExitProgress(now, p, delta, prog.AvgPx, prog.Filled); err != nil {
			return true, err
		}
		changed = true
	}
	if prog.Filled {
		m.clearPendingExit(p)
		if p.State != execClosed && p.RemainingQty > fillEpsilon(p.Qty) {
			return true, m.ensureExitOrders(p)
		}
		return true, nil
	}
	if prog.Terminal {
		m.clearPendingExit(p)
		if p.State != execClosed && p.RemainingQty > fillEpsilon(p.Qty) {
			return true, m.ensureExitOrders(p)
		}
		return true, nil
	}
	if !p.PendingExitCreatedAt.IsZero() && now.Sub(p.PendingExitCreatedAt) >= m.entryTimeout {
		_, _ = m.rest.CancelOrder(p.Symbol, p.PendingExitOrderID)
		m.clearPendingExit(p)
		if p.RemainingQty > fillEpsilon(p.Qty) {
			return true, m.ensureExitOrders(p)
		}
		return true, nil
	}
	return changed, nil
}

func (m *liveExecManager) reconcileOpen(now time.Time, p *livePosition, mom map[string]momentumView, flow map[string]flowMetrics, meta map[string]symbolMeta) (bool, error) {
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
	var remoteRows []map[string]any
	if rows, err := cachedPositionRisk(m.rest, p.Symbol); err == nil {
		remoteRows = rows
		synced, closed, err := m.syncOpenFromRemote(now, p, rows)
		if err != nil {
			return changed, err
		}
		if synced {
			changed = true
		}
		if closed || p.State == execClosed {
			return true, nil
		}
	}
	if addChanged, err := m.reconcilePendingAdd(now, p); err != nil {
		return changed, err
	} else if addChanged {
		changed = true
	}
	if exitChanged, err := m.reconcilePendingExit(now, p); err != nil {
		return changed, err
	} else if exitChanged {
		changed = true
	}
	if p.PendingExitOrderID > 0 {
		return changed, nil
	}
	if p.RemainingQty > 0 {
		mark, err := m.currentMark(p.Symbol)
		if (err != nil || mark <= 0) && len(remoteRows) > 0 {
			mark = remotePositionForSide(remoteRows, p.Side).MarkPrice
		}
		if (err != nil || mark <= 0) && meta != nil {
			mark = meta[strings.ToUpper(strings.TrimSpace(aster.RawSymbol(p.Symbol)))].LastPrice
		}
		if mark > 0 {
			if p.LastMark > 0 && abs(mark-p.LastMark)/maxFloat(p.EntryPrice, 1e-9) < 0.0006 {
				p.StallBars++
			} else {
				p.StallBars = 0
			}
			p.LastMark = mark
			if manualPassivePosition(p) {
				p.UpdatedAt = now
				return true, nil
			}
			updateFavorableRLive(p, mark)
			if m.updateLiveTargetHits(p, mark) {
				changed = true
			}
			_, upct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
			allowBE := allowMoveToBreakEven(p.HitTP1, upct)
			if shouldAdvanceProtection(p) {
				if newStop, tightened := applyLiveProtectionState(now, p.Side, p.EntryPrice, p.StopPrice, p.MaxFavorableR, &p.ProtectionStage, &p.FirstProtectAt, &p.ProtectedStop, m.beLockBps, allowBE); tightened {
					p.StopPrice = newStop
					if err := m.placeOrReplaceStop(p); err == nil {
						changed = true
					}
				}
			}
			updateGivebackMetrics(p.MaxFavorableR, unrealizedRiskR(p.Side, p.EntryPrice, p.StopPrice, mark), &p.CaptureRatio, &p.MaxGivebackR)
			if updated, err := m.handleRatchetTargets(p, mark); err != nil {
				return changed, err
			} else if updated {
				changed = true
			}
			prevSponsored := p.Sponsored
			prevSponsorScore := p.SponsorshipScore
			sponsorSnap := classifySponsorship(p.Side, p.Symbol, mom, flow)
			updateLiveSponsorship(p, sponsorSnap)
			if maybeRefreshLiveConfluence(now, p, sponsorSnap, prevSponsored, prevSponsorScore) {
				fmt.Printf("live: confluence refresh %s %s score=%.2f slope=%.3f state=%s count=%d\n",
					p.Symbol, p.Side, sponsorSnap.Score, sponsorSnap.Slope, sponsorSnap.State, p.ConfluenceRefreshCount)
				changed = true
			}
			if stop, tightened := applyPnLProtectiveStop(p.Side, p.EntryPrice, p.StopPrice, mark, upct); tightened {
				p.StopReason = "PNL_PROTECT_LOCK"
				p.StopPrice = stop
				if err := m.placeOrReplaceStop(p); err == nil {
					changed = true
				}
			}
			tp1R := tp1RFromBracket(p.EntryPrice, p.StopPrice, p.TP1Price)
			beArmR := beArmThreshold(envFloat("LIVE_BE_ARM_R", 1.35), tp1R)
			if m.beLockBps > 0 && beArmR > 0 && p.MaxFavorableR >= beArmR && allowBE {
				be := beLockPriceBuffered(p.Side, p.EntryPrice, p.StopPrice, m.beLockBps)
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
			rawSym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(p.Symbol)))
			currentMV := mom[rawSym]
			currentRunnerState := evaluateRunnerExitStateWithFlow(p.Side, currentMV, flow[rawSym], flowfeed.ExternalSignal{})
			htf := m.htfSnapshot(rawSym, p.Side, sameSideMomentumEntry(p.Side, currentMV))
			prevLifecycle := p.WinnerLifecycle
			syncLiveWinnerLifecycle(p, htfPersistent(p.Side, htf))
			logWinnerLifecycleTransition(p.Symbol, p.Side, prevLifecycle, p.WinnerLifecycle, lifecycleTransitionReason(prevLifecycle, p.WinnerLifecycle, "", p.TrailOn))
			updateManagePhase(p, currentRunnerState.ExhaustionConfirmed && !currentRunnerState.StructureBroken)
			refreshRunnerReservation(p, m.ladderCfg.StarterUSDT)
			if m.exitManager != nil {
				mv := m.exitManager.EvaluateProtect(exitmgr.ProtectInput{
					Side:               p.Side,
					Entry:              p.EntryPrice,
					Stop:               p.StopPrice,
					Mark:               mark,
					MFER:               p.MaxFavorableR,
					MAER:               p.MaxAdverseR,
					BarsHeld:           int(now.Sub(p.CreatedAt) / time.Minute),
					StallBars:          p.StallBars,
					NearFriction:       p.VPTargetLevel > 0 && abs(mark-p.VPTargetLevel)/maxFloat(mark, 1e-9) < 0.002,
					UnrealizedPct:      abs((mark-p.EntryPrice)/maxFloat(p.EntryPrice, 1e-9)) * 100,
					Sponsored:          p.Sponsored,
					HitTP1:             p.HitTP1,
					HitTP2:             p.HitTP2,
					HitTP3:             p.HitTP3,
					WeakSponsorStreak:  p.WeakSponsorStreak,
					EntryReason:        p.EntryReason,
					EntryStrategyID:    p.EntryStrategyID,
					StarterEntry:       false,
					AdvancedReady:      p.ManagePhase == managePhaseContinuation || p.ManagePhase == managePhaseExhaustion || earlyContinuationReady(p),
					HTFTrendState:      string(htf.State),
					HTFTrendPersistent: htfPersistent(p.Side, htf),
					HTFTrendFailed:     htfFailed(p.Side, htf),
					HTFCaution:         htfCaution(p.Side, htf),
					TriggerRef:         m.stopTriggerRef,
					ComputedStop:       p.StopPrice,
					SubmittedStop:      p.StopPrice,
					AcceptedStop:       p.StopPrice,
					LegalityAdjusted:   false,
					WinnerLifecycle:    p.WinnerLifecycle,
					TrailingActive:     p.TrailOn,
					MatureTrend:        matureTrendForWinnerLifecycle(p.MaxFavorableR, htfPersistent(p.Side, htf), p.HitTP1, p.HitTP2, p.HitTP3),
					RealInvalidation:   htfFailed(p.Side, htf),
				})
				logWinnerLifecycleTransition(p.Symbol, p.Side, p.WinnerLifecycle, mv.WinnerLifecycle, lifecycleTransitionReason(p.WinnerLifecycle, mv.WinnerLifecycle, firstNonEmpty(mv.ExitNowReason, mv.Reason), p.TrailOn))
				p.WinnerLifecycle = mv.WinnerLifecycle
				runnerState := currentRunnerState
				if mv.MoveStopToBE && allowBE {
					be := beLockPriceBuffered(p.Side, p.EntryPrice, p.StopPrice, m.beLockBps)
					if (strings.EqualFold(p.Side, "BUY") && be > p.StopPrice) || (strings.EqualFold(p.Side, "SELL") && be < p.StopPrice) {
						p.StopPrice = be
						queueLiveProtectStopChain(p, "BE", mv, be)
						_ = m.placeOrReplaceStop(p)
						changed = true
					}
				}
				if mv.TightenStop {
					if (strings.EqualFold(p.Side, "BUY") && mv.TightenToPrice > p.StopPrice) || (strings.EqualFold(p.Side, "SELL") && mv.TightenToPrice < p.StopPrice) {
						p.StopPrice = mv.TightenToPrice
						queueLiveProtectStopChain(p, "TIGHTEN", mv, mv.TightenToPrice)
						_ = m.placeOrReplaceStop(p)
						changed = true
					}
				}
				if correctedStop, corrected := enforceWinnerBEFloor(p.Side, p.EntryPrice, p.StopPrice, p.MaxFavorableR); corrected {
					p.StopReason = "forced_be_correction"
					p.StopPrice = correctedStop
					queueLiveProtectStopChain(p, "TIGHTEN", exitmgr.ProtectDecision{
						CurrentWinnerLifecycle: p.WinnerLifecycle,
						WinnerLifecycle:        p.WinnerLifecycle,
						Reason:                 "forced_be_correction",
						TriggerRef:             m.stopTriggerRef,
						HTFTrendState:          string(htf.State),
						HTFPersistent:          htfPersistent(p.Side, htf),
						HTFFailed:              htfFailed(p.Side, htf),
						HTFCaution:             htfCaution(p.Side, htf),
					}, correctedStop)
					_ = m.placeOrReplaceStop(p)
					changed = true
				}
				if mv.ImmediateExit {
					logProtectDecisionOnce(&p.lastProtectDecisionKey, protectDecisionLogLine(p.Symbol, p.Side, "IMMEDIATE_EXIT", mv.CurrentWinnerLifecycle, mv.WinnerLifecycle, firstNonEmpty(mv.ExitNowReason, mv.Reason, "IMMEDIATE_EXIT"), mv.ComputedStop, mv.SubmittedStop, mv.AcceptedStop, mv.TriggerRef, mv.LegalityAdjusted, p.MaxFavorableR, mv.HTFTrendState, mv.HTFPersistent, mv.HTFFailed, mv.HTFCaution))
					reason := firstNonEmpty(mv.ExitNowReason, mv.Reason, "IMMEDIATE_EXIT")
					_ = m.cancelRemainingExits(p)
					if err := m.submitCloseLimit(p, p.RemainingQty, reason, "CLOSE"); err == nil {
						changed = true
					}
					return changed, nil
				}
				if runnerState.ExhaustionConfirmed && !runnerState.StructureBroken {
					if m.trimToRunner(now, p, "RUNNER_EXHAUST_TRIM") {
						changed = true
					}
					if m.tightenRunnerStop(p, runnerState.TightenReason) {
						changed = true
					}
				}
				if mv.FullExit {
					if !lifecycleSoftExitsCanHardClose(p.WinnerLifecycle) {
					} else {
						logProtectDecisionOnce(&p.lastProtectDecisionKey, protectDecisionLogLine(p.Symbol, p.Side, "FULL_EXIT", mv.CurrentWinnerLifecycle, mv.WinnerLifecycle, firstNonEmpty(runnerState.FullExitReason, mv.Reason), mv.ComputedStop, mv.SubmittedStop, mv.AcceptedStop, mv.TriggerRef, mv.LegalityAdjusted, p.MaxFavorableR, mv.HTFTrendState, mv.HTFPersistent, mv.HTFFailed, mv.HTFCaution))
						if !runnerState.StructureBroken {
							if runnerState.ExhaustionConfirmed && m.tightenRunnerStop(p, runnerState.TightenReason) {
								changed = true
							}
						} else {
							reason := firstNonEmpty(runnerState.FullExitReason, mv.Reason)
							_ = m.cancelRemainingExits(p)
							if err := m.submitCloseLimit(p, p.RemainingQty, reason, "CLOSE"); err == nil {
								changed = true
							}
						}
					}
				}
			}
		}
	}
	// Ensure there is always a protective stop while position is live.
	if p.RemainingQty > 0 && p.StopOrderID == 0 && p.PendingExitOrderID == 0 && !manualPassivePosition(p) {
		if err := m.placeOrReplaceStop(p); err != nil {
			return changed, err
		}
		changed = true
	}
	// If exchange position is flat, close local state and cancel leftovers.
	if len(remoteRows) == 0 {
		rows, err := cachedPositionRisk(m.rest, p.Symbol)
		if err == nil {
			remoteRows = rows
		}
	}
	if len(remoteRows) > 0 {
		view := remotePositionForSide(remoteRows, p.Side)
		if view.QtyAbs <= 1e-10 {
			return m.closeFromRemoteSnapshot(now, p, view.MarkPrice, "POSITION_FLAT")
		}
	}
	return changed, nil
}

func (m *liveExecManager) reconcileExitOrders(now time.Time, p *livePosition) (bool, error) {
	changed := false
	repairChecks := envInt("LIVE_UNKNOWN_EXIT_REPAIR_CHECKS", 3)
	if repairChecks < 1 {
		repairChecks = 3
	}
	successfulCheck := false
	hadUnknown := false
	if p.TP1OrderID > 0 {
		o, err := m.rest.GetOrder(p.Symbol, p.TP1OrderID)
		if err != nil {
			hadUnknown = true
			p.UnknownExitChecks++
			if p.UnknownExitChecks >= repairChecks {
				p.TP1OrderID = 0
				changed = true
			}
		} else {
			successfulCheck = true
			prog := parseOrderProgress(o)
			if delta := maxFloat(0, prog.ExecQty-p.TP1FilledQty); delta > fillEpsilon(p.TP1Qty) {
				if err := m.applyTPProgress(now, p, 1, delta, prog.AvgPx, prog.Filled); err != nil {
					return true, err
				}
				changed = true
			}
			if prog.Terminal && !prog.Filled {
				p.TP1OrderID = 0
				changed = true
			}
		}
	}
	if p.TP2OrderID > 0 {
		o, err := m.rest.GetOrder(p.Symbol, p.TP2OrderID)
		if err != nil {
			hadUnknown = true
			p.UnknownExitChecks++
			if p.UnknownExitChecks >= repairChecks {
				p.TP2OrderID = 0
				changed = true
			}
		} else {
			successfulCheck = true
			prog := parseOrderProgress(o)
			if delta := maxFloat(0, prog.ExecQty-p.TP2FilledQty); delta > fillEpsilon(p.TP2Qty) {
				if err := m.applyTPProgress(now, p, 2, delta, prog.AvgPx, prog.Filled); err != nil {
					return true, err
				}
				changed = true
			}
			if prog.Terminal && !prog.Filled {
				p.TP2OrderID = 0
				changed = true
			}
		}
	}
	if p.TP3OrderID > 0 {
		o, err := m.rest.GetOrder(p.Symbol, p.TP3OrderID)
		if err != nil {
			hadUnknown = true
			p.UnknownExitChecks++
			if p.UnknownExitChecks >= repairChecks {
				p.TP3OrderID = 0
				changed = true
			}
		} else {
			successfulCheck = true
			prog := parseOrderProgress(o)
			if delta := maxFloat(0, prog.ExecQty-p.TP3FilledQty); delta > fillEpsilon(p.TP3Qty) {
				if err := m.applyTPProgress(now, p, 3, delta, prog.AvgPx, prog.Filled); err != nil {
					return true, err
				}
				changed = true
			}
			if prog.Terminal && !prog.Filled {
				p.TP3OrderID = 0
				changed = true
			}
		}
	}
	if p.StopOrderID > 0 {
		o, err := m.rest.GetOrder(p.Symbol, p.StopOrderID)
		if err != nil {
			hadUnknown = true
			p.UnknownExitChecks++
			if p.UnknownExitChecks >= repairChecks {
				p.StopOrderID = 0
				changed = true
			}
		} else {
			successfulCheck = true
			prog := parseOrderProgress(o)
			if delta := maxFloat(0, prog.ExecQty-p.StopFilledQty); delta > fillEpsilon(p.RemainingQty) {
				if err := m.applyStopProgress(now, p, delta, prog.AvgPx, prog.Filled); err != nil {
					return true, err
				}
				changed = true
			}
			if p.State == execClosed {
				_ = m.cancelRemainingExits(p)
				return true, nil
			}
			if prog.Terminal && !prog.Filled {
				p.StopOrderID = 0
				changed = true
			}
		}
	}
	if p.RemainingQty <= 1e-10 {
		_ = m.cancelRemainingExits(p)
		m.markPositionClosed(now, p, "TP_DONE")
		m.maybeSweepTradeProfit(now, p)
		return true, nil
	}
	if err := m.ensureExitOrders(p); err != nil {
		return changed, err
	}
	if successfulCheck && !hadUnknown {
		p.UnknownExitChecks = 0
	}
	if changed && p.State != execClosed && p.RemainingQty > 0 {
		if err := m.ensureExitOrders(p); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func (m *liveExecManager) initializeBracketLevels(p *livePosition) error {
	sideBuy := strings.EqualFold(p.Side, "BUY")
	anchor := p.EntryPrice
	if importedManagedPosition(p) && p.ManageAnchorPrice > 0 {
		anchor = p.ManageAnchorPrice
	}
	stopPct := m.stopPct / 100.0
	if stopPct <= 0 {
		stopPct = 0.02
	}
	if m.riskOnMargin {
		if riskPct := marginRiskStopPct(p.Margin, p.Leverage, m.riskMarginPct); riskPct > 0 {
			stopPct = riskPct
		}
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
		p.EntryGrade,
		inplay.State(p.EntryState),
		p.EntryConf,
		p.EntryVolumeUSD,
		stopPct,
		tp1R,
		tp2R,
		tp3R,
		m.minStopPct/100.0,
		m.maxStopPct/100.0,
	)
	stopPct = clamp(widenStopPctForVolatility(stopPct, p.EntryATRPct, p.EntryVolumeUSD), m.minStopPct/100.0, m.maxStopPct/100.0)
	if sideBuy {
		p.StopPrice = anchor * (1 - stopPct)
		p.TP1Price = anchor * (1 + stopPct*tp1R)
		p.TP2Price = anchor * (1 + stopPct*tp2R)
		p.TP3Price = anchor * (1 + stopPct*tp3R)
	} else {
		p.StopPrice = anchor * (1 + stopPct)
		p.TP1Price = anchor * (1 - stopPct*tp1R)
		p.TP2Price = anchor * (1 - stopPct*tp2R)
		p.TP3Price = anchor * (1 - stopPct*tp3R)
	}
	if m.exitManager != nil {
		p.TP1Price = m.exitManager.FrontRunTarget(p.Side, p.TP1Price, p.VPTargetLevel)
		p.TP2Price = m.exitManager.FrontRunTarget(p.Side, p.TP2Price, p.VPTargetLevel)
		p.TP3Price = m.exitManager.FrontRunTarget(p.Side, p.TP3Price, p.VPTargetLevel)
	}
	p.TP1Price, p.TP2Price, p.TP3Price = enforceTPProgression(p.Side, p.TP1Price, p.TP2Price, p.TP3Price)
	p.StopPrice, p.TP1Price, p.TP2Price, p.TP3Price = sanitizeBracketGeometry(anchor, p.Side, p.StopPrice, p.TP1Price, p.TP2Price, p.TP3Price)
	if p.StopPrice <= 0 || p.TP1Price <= 0 || p.TP2Price <= 0 || p.TP3Price <= 0 {
		return fmt.Errorf("invalid bracket levels stop=%.6f tp1=%.6f tp2=%.6f tp3=%.6f",
			p.StopPrice, p.TP1Price, p.TP2Price, p.TP3Price)
	}
	risk := abs(anchor - p.StopPrice)
	reward := abs(p.TP1Price - anchor)
	rr := reward / maxFloat(risk, 1e-9)
	if risk <= 0 || rrBelowMinimum(rr, m.minTP1RR) {
		return fmt.Errorf("tp1 rr below minimum: rr=%.3f min=%.3f", rr, m.minTP1RR)
	}
	p.TrailRef = anchor
	p.TrailStop = p.StopPrice
	q1 := p.FilledQty * m.tp1Frac
	q2 := p.FilledQty * m.tp2Frac
	q3 := p.FilledQty * m.tp3Frac
	if m.rest == nil {
		p.TP1Qty = q1
		p.TP2Qty = q2
		maxTP3 := maxFloat(0, p.FilledQty-p.TP1Qty-p.TP2Qty)
		if q3 <= 0 || q3 > maxTP3 {
			q3 = maxTP3
		}
		p.TP3Qty = q3
	} else {
		var err error
		if q1 > 0 {
			p.TP1Qty, err = m.roundQty(p.Symbol, q1)
			if err != nil {
				if benignZeroRoundedQtyErr(err) {
					p.TP1Qty = 0
				} else {
					return err
				}
			}
		} else {
			p.TP1Qty = 0
		}
		if q2 > 0 {
			p.TP2Qty, err = m.roundQty(p.Symbol, q2)
			if err != nil {
				if benignZeroRoundedQtyErr(err) {
					p.TP2Qty = 0
				} else {
					return err
				}
			}
		} else {
			p.TP2Qty = 0
		}
		maxTP3 := maxFloat(0, p.FilledQty-p.TP1Qty-p.TP2Qty)
		if q3 <= 0 || q3 > maxTP3 {
			q3 = maxTP3
		}
		if q3 > 0 {
			p.TP3Qty, err = m.roundQty(p.Symbol, q3)
			if err != nil {
				if benignZeroRoundedQtyErr(err) {
					p.TP3Qty = 0
				} else {
					return err
				}
			}
		} else {
			p.TP3Qty = 0
		}
	}
	return nil
}

func (m *liveExecManager) placeInitialBrackets(p *livePosition) error {
	if err := m.initializeBracketLevels(p); err != nil {
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
	if !m.tpRatchetOnly {
		var err error
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
	intentID := buildIntentID(p.Symbol, firstNonEmpty(strings.TrimSpace(p.EntryStrategyID), strings.TrimSpace(p.EntryReason)), price, time.Now().UTC())
	if m.isUnknownExecutionFrozen(p.Symbol) {
		logUnknownExecution(p.Symbol, intentID, "submit_skipped", "symbol frozen by unknown execution guard")
		return 0, fmt.Errorf("unknown_execution_frozen")
	}
	out, err := m.rest.PlaceOrder(vals)
	if err != nil {
		if isAmbiguousVenueOutcome(err) {
			m.handleUnknownExecution(p.Symbol, intentID)
			time.Sleep(time.Duration(envInt("LIVE_EXEC_RECONCILE_AFTER_UNKNOWN_MS", 800)) * time.Millisecond)
			_ = m.reconcileAfterUnknown(p.Symbol, intentID)
		}
		return 0, err
	}
	return mapInt64(out["orderId"]), nil
}

func protectiveStopValid(side string, entry, mark, stop float64) bool {
	if entry <= 0 || mark <= 0 || stop <= 0 {
		return false
	}
	if isLongSide(side) {
		return stop < mark
	}
	return stop > mark
}

func protectiveStopExchangeSafe(side string, entry, mark, stop, tickSize float64) bool {
	if !protectiveStopValid(side, entry, mark, stop) {
		return false
	}
	ref := mark
	if ref <= 0 {
		ref = entry
	}
	if ref <= 0 {
		return false
	}
	minGapPct := envFloat("LIVE_STOP_LEGALIZE_MIN_GAP_PCT", envFloat("LIVE_MANUAL_PROTECTION_MIN_GAP_PCT", 0.0035))
	minGapAbs := ref * minGapPct
	if tickSize > 0 {
		minGapAbs = maxFloat(minGapAbs, tickSize*8)
	}
	if isLongSide(side) {
		return stop <= mark-minGapAbs
	}
	return stop >= mark+minGapAbs
}

func replacementProtectiveStopExchangeSafe(side string, entry, mark, stop, tickSize float64, winnerLifecycle string) bool {
	if !protectiveStopExchangeSafe(side, entry, mark, stop, tickSize) {
		return false
	}
	ref := mark
	if ref <= 0 {
		ref = entry
	}
	if ref <= 0 {
		return false
	}
	minGapPct := envFloat("LIVE_STOP_LEGALIZE_REPLACE_MIN_GAP_PCT",
		envFloat("LIVE_STOP_LEGALIZE_MIN_GAP_PCT", envFloat("LIVE_MANUAL_PROTECTION_MIN_GAP_PCT", 0.0035))*1.75)
	switch strings.ToLower(strings.TrimSpace(winnerLifecycle)) {
	case "late_trail":
		minGapPct = maxFloat(minGapPct, envFloat("LIVE_STOP_LEGALIZE_REPLACE_LATE_TRAIL_MIN_GAP_PCT", 0.0075))
	case "runner", "winner_locked":
		minGapPct = maxFloat(minGapPct, envFloat("LIVE_STOP_LEGALIZE_REPLACE_WINNER_MIN_GAP_PCT", 0.0060))
	}
	minGapAbs := ref * minGapPct
	if tickSize > 0 {
		minGapAbs = maxFloat(minGapAbs, tickSize*12)
	}
	if isLongSide(side) {
		return stop <= mark-minGapAbs
	}
	return stop >= mark+minGapAbs
}

func widenedProtectiveStop(side string, entry, mark, tickSize float64) float64 {
	ref := maxFloat(entry, mark)
	if isLongSide(side) {
		ref = min(entry, mark)
	}
	bufferPct := 0.0025
	if tickSize > 0 && ref > 0 {
		bufferPct = maxFloat(bufferPct, (tickSize*2)/ref)
	}
	if isLongSide(side) {
		return ref * (1 - bufferPct)
	}
	return ref * (1 + bufferPct)
}

func widenedImmediateTriggerStop(side string, entry, mark, tickSize float64) float64 {
	return widenedImmediateTriggerStopPct(side, entry, mark, tickSize, 0.0050)
}

func widenedImmediateTriggerStopPct(side string, entry, mark, tickSize, basePct float64) float64 {
	ref := maxFloat(entry, mark)
	if isLongSide(side) {
		ref = min(entry, mark)
	}
	bufferPct := basePct
	if tickSize > 0 && ref > 0 {
		bufferPct = maxFloat(bufferPct, (tickSize*4)/ref)
	}
	if isLongSide(side) {
		return ref * (1 - bufferPct)
	}
	return ref * (1 + bufferPct)
}

func chooseProtectiveReference(side string, bid, ask float64) float64 {
	if isLongSide(side) {
		if bid > 0 {
			return bid
		}
		if ask > 0 {
			return ask
		}
	} else {
		if ask > 0 {
			return ask
		}
		if bid > 0 {
			return bid
		}
	}
	if bid > 0 && ask > 0 {
		return (bid + ask) / 2
	}
	return maxFloat(bid, ask)
}

func chooseManagedProtectiveStop(side string, entry, mark, computedStop, protectedStop float64) float64 {
	if computedStop > 0 && protectiveStopValid(side, entry, mark, computedStop) {
		return computedStop
	}
	if protectedStop > 0 && protectiveStopValid(side, entry, mark, protectedStop) {
		return protectedStop
	}
	if protectedStop > 0 {
		return protectedStop
	}
	return computedStop
}

func manualStopRetryCandidates(side string, entry, mark, tickSize float64) []float64 {
	if !envBool("LIVE_STOP_LEGALIZE_ENABLE", true) {
		return nil
	}
	minGapPct := envFloat("LIVE_STOP_LEGALIZE_MIN_GAP_PCT", envFloat("LIVE_MANUAL_PROTECTION_MIN_GAP_PCT", 0.0035))
	basePcts := []float64{
		maxFloat(0.0025, minGapPct),
		maxFloat(0.0025, minGapPct*1.5),
		maxFloat(0.0025, minGapPct*2.0),
		maxFloat(0.0035, minGapPct*3.0),
		maxFloat(0.0050, minGapPct*5.0),
		0.0100,
		0.0150,
		0.0200,
	}
	base := make([]float64, 0, len(basePcts)+1)
	base = append(base, widenedProtectiveStop(side, entry, mark, tickSize))
	for _, pct := range basePcts {
		base = append(base, widenedImmediateTriggerStopPct(side, entry, mark, tickSize, pct))
	}
	out := make([]float64, 0, len(base))
	for _, px := range base {
		if px <= 0 {
			continue
		}
		dup := false
		for _, seen := range out {
			if math.Abs(seen-px) <= 1e-9 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, px)
		}
	}
	return out
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

func (m *liveExecManager) currentProtectiveReference(symbol, side string) (float64, error) {
	bid, ask, err := m.rest.BookTicker(symbol)
	if err != nil {
		return 0, err
	}
	ref := chooseProtectiveReference(side, bid, ask)
	if ref <= 0 {
		return 0, fmt.Errorf("invalid protective reference")
	}
	return ref, nil
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

func (m *liveExecManager) updateLiveTargetHits(p *livePosition, mark float64) bool {
	if m == nil || p == nil || mark <= 0 {
		return false
	}
	changed := false
	sideBuy := strings.EqualFold(p.Side, "BUY")
	if !p.HitTP1 && targetHit(p.Side, mark, p.TP1Price) {
		p.HitTP1 = true
		changed = true
	}
	if !p.HitTP2 && targetHit(p.Side, mark, p.TP2Price) {
		p.HitTP2 = true
		changed = true
		m.maybeEnableTrail(p, 2)
	}
	if !p.HitTP3 && targetHit(p.Side, mark, p.TP3Price) {
		p.HitTP3 = true
		changed = true
		m.maybeEnableTrail(p, 3)
	}
	if p.TrailOn {
		if p.TrailRef <= 0 || (sideBuy && mark > p.TrailRef) || (!sideBuy && mark < p.TrailRef) {
			p.TrailRef = mark
			p.TrailStop = m.calcTrailStopForPosition(p, sideBuy, mark, p.HitTP3)
			changed = true
		}
	}
	return changed
}

func (m *liveExecManager) handleRatchetTargets(p *livePosition, mark float64) (bool, error) {
	if m == nil || p == nil || mark <= 0 {
		return false, nil
	}
	m.updateLiveTargetHits(p, mark)
	if !m.tpRatchetOnly {
		return false, nil
	}
	changed := false
	if p.HitTP1 {
		if stop, ok := ratchetStopTarget(p.Side, p.EntryPrice, p.StopPrice, p.TP1Price, p.TP2Price, m.beLockBps, 1); ok {
			p.StopPrice = stop
			changed = true
		}
	}
	if p.HitTP2 {
		if stop, ok := ratchetStopTarget(p.Side, p.EntryPrice, p.StopPrice, p.TP1Price, p.TP2Price, m.beLockBps, 2); ok {
			p.StopPrice = stop
			changed = true
		}
	}
	if p.HitTP3 {
		if stop, ok := ratchetStopTarget(p.Side, p.EntryPrice, p.StopPrice, p.TP1Price, p.TP2Price, m.beLockBps, 3); ok {
			p.StopPrice = stop
			changed = true
		}
		m.maybeEnableTrail(p, 3)
		if p.TrailOn {
			if p.TrailRef <= 0 || (strings.EqualFold(p.Side, "BUY") && mark > p.TrailRef) || (!strings.EqualFold(p.Side, "BUY") && mark < p.TrailRef) {
				p.TrailRef = mark
				p.TrailStop = m.calcTrailStopForPosition(p, strings.EqualFold(p.Side, "BUY"), mark, true)
				changed = true
			}
		}
	}
	if changed {
		if err := m.placeOrReplaceStop(p); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func isSponsoredMomentum(side string, mv momentumView, minScore, minSlope float64) bool {
	var e *inplay.Entry
	if strings.EqualFold(side, "BUY") {
		e = mv.Long
	} else {
		e = mv.Short
	}
	if e == nil {
		return false
	}
	if e.CurrentScore < minScore {
		return false
	}
	if strings.EqualFold(side, "BUY") {
		if e.ScoreSlope < minSlope {
			return false
		}
	} else {
		if e.ScoreSlope > -minSlope {
			return false
		}
	}
	switch e.State {
	case inplay.StateInPlay, inplay.StateHeating, inplay.StatePumping:
		return true
	default:
		return false
	}
}

func sameSideMomentumEntry(side string, mv momentumView) *inplay.Entry {
	if strings.EqualFold(side, "BUY") {
		return mv.Long
	}
	return mv.Short
}

func evaluateSwingHold(side string, mv momentumView, sponsored bool, refreshed bool, maxFavorableR float64, hitTP1, hitTP2, hitTP3 bool) (float64, string, bool) {
	e := sameSideMomentumEntry(side, mv)
	if e == nil {
		return 0, "scanner_missing", false
	}
	if e.LongDemotionFlag || e.ShortDemotionFlag {
		return 0, "scanner_demoted", false
	}
	score := 0.0
	switch e.State {
	case inplay.StateHeating:
		score += 0.22
	case inplay.StateInPlay:
		score += 0.30
	case inplay.StatePumping:
		score += 0.34
	default:
		return score, "state_not_persistent", false
	}
	if abs(e.ScoreSlope) >= envFloat("LIVE_SWING_HOLD_MIN_SLOPE", 0.02) {
		score += 0.18
	} else if e.Momentum {
		score += 0.12
	}
	if e.TimeInStateMin >= envFloat("LIVE_SWING_HOLD_MIN_STATE_MIN", 20.0) {
		score += 0.14
	}
	if e.CurrentScore >= envFloat("LIVE_SWING_HOLD_MIN_SCORE", 88.0) {
		score += 0.16
	}
	if abs(e.DayUTCPct) >= envFloat("LIVE_SWING_HOLD_MIN_DAYUTC_PCT", 5.0) {
		score += 0.08
	}
	if e.ScoreOffPeakPct <= envFloat("LIVE_SWING_HOLD_MAX_SCORE_OFF_PEAK_PCT", 18.0) {
		score += 0.08
	}
	if sponsored {
		score += 0.12
	}
	if refreshed {
		score += 0.10
	}
	if maxFavorableR >= envFloat("LIVE_SWING_HOLD_MIN_MFE_R", 0.80) {
		score += 0.06
	}
	if hitTP1 || hitTP2 || hitTP3 {
		score += 0.06
	}
	minScore := envFloat("LIVE_SWING_HOLD_MIN_SCORE_TOTAL", 0.58)
	if score >= minScore {
		return score, "persistent_same_thesis", true
	}
	return score, "hold_score_too_low", false
}

func (m *liveExecManager) tightenRunnerStop(p *livePosition, reason string) bool {
	if m == nil || p == nil {
		return false
	}
	nextStop := lockToRPrice(p.Side, p.EntryPrice, p.StopPrice, envFloat("LIVE_PROFIT_LOCK_TIGHTEN_R", 0.35))
	if strings.EqualFold(p.Side, "BUY") {
		if nextStop <= p.StopPrice {
			return false
		}
	} else if nextStop >= p.StopPrice {
		return false
	}
	p.StopReason = reason
	p.StopPrice = nextStop
	if err := m.placeOrReplaceStop(p); err != nil {
		return false
	}
	return hasLiveProtectiveOrder(p)
}

func (m *liveExecManager) updateTrailingStop(p *livePosition, mark float64) (bool, error) {
	if p == nil || !p.TrailOn || p.RemainingQty <= 0 || mark <= 0 {
		return false, nil
	}
	if envBool("LIVE_TRAIL_PROTECTED_15M_ENABLE", true) {
		return m.updateProtectedTrailingStop(p, mark)
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
	newStop := m.calcTrailStopForPosition(p, sideBuy, newRef, p.HitTP3)
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
	if trailNeedsConfirmation() {
		level := p.TrailRef
		if level <= 0 {
			level = p.EntryPrice
		}
		if p.TrailCandidateAt.IsZero() || (sideBuy && newStop > p.TrailCandidateStop) || (!sideBuy && newStop < p.TrailCandidateStop) {
			p.TrailCandidateRef = newRef
			p.TrailCandidateStop = newStop
			p.TrailCandidateLevel = level
			p.TrailCandidateAt = time.Now().UTC()
		}
		confirmed, err := m.trailCandidateConfirmed(p, sideBuy)
		if err != nil || !confirmed {
			return false, err
		}
		newRef = p.TrailCandidateRef
		newStop = p.TrailCandidateStop
	}
	p.TrailRef = newRef
	p.TrailStop = newStop
	p.StopPrice = newStop
	if err := m.placeOrReplaceStop(p); err != nil {
		return false, err
	}
	if !hasLiveProtectiveOrder(p) {
		return false, nil
	}
	clearTrailCandidate(p)
	m.emitNotify(notify.Event{
		Key:      "TRAIL_MOVE",
		Title:    "TRAIL MOVE",
		Class:    notify.ClassLifecycle,
		Severity: notify.SeverityInfo,
		Route:    notify.RouteNormal,
		Symbol:   p.Symbol,
		Message:  "trailing stop tightened",
		Metadata: map[string]string{
			"stop": fmtPrice(p.StopPrice),
			"mark": fmtPrice(mark),
		},
	})
	return true, nil
}

func (m *liveExecManager) updateProtectedTrailingStop(p *livePosition, mark float64) (bool, error) {
	if m == nil || p == nil {
		return false, nil
	}
	sideBuy := strings.EqualFold(p.Side, "BUY")
	protectedTF := strings.ToLower(strings.TrimSpace(envStr("LIVE_TRAIL_PROTECTED_TF", "5m")))
	protectedLimit := envInt("LIVE_TRAIL_PROTECTED_BARS", 0)
	if protectedLimit <= 0 {
		// Backward compatibility with older env names.
		protectedLimit = envInt("LIVE_TRAIL_PROTECTED_15M_BARS", 48)
	}
	closedBars, err := m.trailingClosedBarsProtected(p.Symbol, protectedTF, protectedLimit)
	if err != nil || len(closedBars) < 20 {
		return false, nil
	}
	trailState := trailx.NewTrailState(
		p.Symbol,
		trailSideForPosition(p.Side),
		p.EntryPrice,
		maxFloat(p.StopPrice, p.EntryPrice*0.0001),
		trailx.TrailConfig{HardStopPct: envFloat("LIVE_HARD_STOP_PCT", trailx.DefaultHardStopPct)},
	)
	trailState.TacticalStop = p.StopPrice
	trailState.Last15mClosedCandle = p.TrailProtectedLastClose
	trailState.AdvancedReady = p.ProtectionStage >= protectionStageArmed || earlyContinuationReady(p)
	trailState.HitTP1 = p.HitTP1
	atrMult := envFloat("LIVE_TRAIL_PROTECTED_ATR_MULT", 0)
	if atrMult <= 0 {
		atrMult = envFloat("LIVE_TRAIL_PROTECTED_15M_ATR_MULT", 1.5)
	}
	upd := trailx.UpdateProtectedTrailOn15mClose(&trailState, closedBars, atrMult)
	p.TrailProtectedLastClose = trailState.Last15mClosedCandle
	if !upd.TacticalStopUpdated || upd.CurrentTacticalStop <= 0 {
		return false, nil
	}
	newStop := upd.CurrentTacticalStop
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
	p.TrailRef = mark
	p.TrailStop = newStop
	p.StopReason = firstNonEmpty(upd.Reason, "tactical_trail_ema20_close")
	p.StopPrice = newStop
	if err := m.placeOrReplaceStop(p); err != nil {
		return false, err
	}
	if !hasLiveProtectiveOrder(p) {
		return false, nil
	}
	clearTrailCandidate(p)
	m.emitNotify(notify.Event{
		Key:      "TRAIL_MOVE",
		Title:    "TRAIL MOVE",
		Class:    notify.ClassLifecycle,
		Severity: notify.SeverityInfo,
		Route:    notify.RouteNormal,
		Symbol:   p.Symbol,
		Message:  "protected trail updated",
		Metadata: map[string]string{
			"stop": fmtPrice(p.StopPrice),
			"mark": fmtPrice(mark),
			"mode": protectedTF + "_close_ema20_atr",
		},
	})
	return true, nil
}

func trailSideForPosition(side string) trailx.TrailSide {
	if strings.EqualFold(strings.TrimSpace(side), "SELL") {
		return trailx.SideSell
	}
	return trailx.SideBuy
}

func trailNeedsConfirmation() bool {
	return envBool("LIVE_TRAIL_CONFIRM_ON_CLOSE", true)
}

func trailConfirmBars() int {
	n := envInt("LIVE_TRAIL_CONFIRM_BARS", 1)
	if n <= 0 {
		n = 1
	}
	if n > 3 {
		n = 3
	}
	return n
}

func trailRetestEnabled() bool {
	return envBool("LIVE_TRAIL_RETEST_ENABLE", true)
}

func clearTrailCandidate(p *livePosition) {
	if p == nil {
		return
	}
	p.TrailCandidateRef = 0
	p.TrailCandidateStop = 0
	p.TrailCandidateLevel = 0
	p.TrailCandidateAt = time.Time{}
}

func (m *liveExecManager) trailCandidateConfirmed(p *livePosition, sideBuy bool) (bool, error) {
	if p == nil || p.Symbol == "" || p.TrailCandidateAt.IsZero() {
		return false, nil
	}
	bars, err := trailingClosedBars(m.featureCache, p.Symbol, 8)
	if err != nil {
		return false, err
	}
	return trailCandidateConfirmedFromBars(sideBuy, bars, p.TrailCandidateAt, p.TrailCandidateLevel), nil
}

func trailingClosedBars(cache *featureRuntimeCache, symbol string, limit int) ([]types.Candle, error) {
	if cache == nil {
		return nil, fmt.Errorf("feature cache unavailable")
	}
	if limit < 3 {
		limit = 3
	}
	bars, err := cache.candleSeries(symbol, types.TF1m, limit)
	if err != nil {
		return nil, err
	}
	bars = types.EnsureSorted(append([]types.Candle(nil), bars...))
	if len(bars) <= 1 {
		return nil, nil
	}
	return bars[:len(bars)-1], nil
}

func (m *liveExecManager) trailingClosedBarsProtected(symbol, timeframe string, limit int) ([]features.Candle, error) {
	if m == nil {
		return nil, fmt.Errorf("manager unavailable")
	}
	tf := types.TF5m
	switch strings.ToLower(strings.TrimSpace(timeframe)) {
	case "15m":
		tf = types.TF15m
	case "5m":
		tf = types.TF5m
	default:
		tf = types.TF5m
	}
	if limit < 24 {
		limit = 24
	}
	var bars []types.Candle
	var err error
	if m.featureCache != nil {
		bars, err = m.featureCache.candleSeries(symbol, tf, limit)
	} else {
		bars, err = aster.LoadCandles(symbol, tf, limit)
	}
	if err != nil {
		return nil, err
	}
	bars = types.EnsureSorted(append([]types.Candle(nil), bars...))
	if len(bars) <= 1 {
		return nil, nil
	}
	closed := bars[:len(bars)-1]
	out := make([]features.Candle, 0, len(closed))
	for _, b := range closed {
		if b.C <= 0 || b.H <= 0 || b.L <= 0 || b.T.IsZero() {
			continue
		}
		out = append(out, features.Candle{
			Ts: b.T,
			O:  b.O,
			H:  b.H,
			L:  b.L,
			C:  b.C,
			V:  b.V,
		})
	}
	return out, nil
}

func trailCandidateConfirmedFromBars(sideBuy bool, bars []types.Candle, candidateAt time.Time, level float64) bool {
	if len(bars) == 0 || level <= 0 || candidateAt.IsZero() {
		return false
	}
	candidateMinute := candidateAt.Truncate(time.Minute)
	eligible := make([]types.Candle, 0, len(bars))
	for _, b := range bars {
		if b.T.After(candidateMinute) {
			eligible = append(eligible, b)
		}
	}
	if len(eligible) < trailConfirmBars() {
		return false
	}
	checkBars := eligible[len(eligible)-trailConfirmBars():]
	bufferBps := envFloat("LIVE_TRAIL_CONFIRM_LEVEL_BUFFER_BPS", 5.0) / 10000.0
	if bufferBps < 0 {
		bufferBps = 0
	}
	levelUp := level * (1 + bufferBps)
	levelDown := level * (1 - bufferBps)
	for _, b := range checkBars {
		if sideBuy {
			if b.C <= levelUp {
				return false
			}
			if trailRetestEnabled() && b.L <= levelUp && b.C <= levelUp {
				return false
			}
		} else {
			if b.C >= levelDown {
				return false
			}
			if trailRetestEnabled() && b.H >= levelDown && b.C >= levelDown {
				return false
			}
		}
	}
	return true
}

func (m *liveExecManager) calcTrailStop(sideBuy bool, ref float64) float64 {
	return m.calcTrailStopForPosition(nil, sideBuy, ref, false)
}

func (m *liveExecManager) calcTrailStopForPosition(p *livePosition, sideBuy bool, ref float64, postTP3 bool) float64 {
	if ref <= 0 {
		return 0
	}
	trailMode := strings.ToLower(strings.TrimSpace(envStr("LIVE_TRAIL_MODE", "hybrid")))
	pct := m.trailStopPct / 100.0
	if postTP3 && m.trailStopPctTP3 > 0 {
		pct = m.trailStopPctTP3 / 100.0
	}
	if pct <= 0 {
		pct = 0.01
	}
	dist := ref * pct
	floorDist := ref * (m.trailPctMin / 100.0)
	atrDist := 0.0
	if p != nil && p.Symbol != "" {
		atrPct := estimateATRPctWithCache(m.featureCache, p.Symbol, maxInt(m.atrLen*4, 64), m.atrLen)
		if atrPct > 0 {
			atrDist = ref * atrPct * trailATRMultForContext(p.EntryReason, atrPct, p.EntryVolumeUSD)
		}
	}
	structDist := 0.0
	if p != nil {
		structDist = structureTrailDistance(ref, p.VPTargetLevel)
	}
	switch trailMode {
	case "structure":
		if structDist > 0 {
			dist = maxFloat(floorDist, structDist)
		}
	case "atr":
		if atrDist > 0 {
			dist = maxFloat(floorDist, atrDist)
		}
	default:
		if atrDist > 0 {
			dist = maxFloat(floorDist, atrDist)
		}
		if structDist > 0 {
			dist = maxFloat(dist, structDist)
		}
	}
	if p != nil {
		dist *= trailProfileMultiplier(p.ExitProfile)
		if p.Sponsored {
			dist *= envFloat("LIVE_TRAIL_SPONSORED_MULT", 1.15)
		} else if p.HitTP1 {
			dist *= envFloat("LIVE_TRAIL_UNSPONSORED_MULT", 0.85)
			if p.WeakSponsorStreak >= envInt("LIVE_TRAIL_WEAK_SPONSOR_STREAK", 2) {
				dist *= envFloat("LIVE_TRAIL_WEAK_SPONSOR_MULT", 0.75)
			}
		}
		if confluenceRefreshActive(time.Now().UTC(), p.LastConfluenceRefresh) {
			dist *= envFloat("LIVE_TRAIL_CONFLUENCE_REFRESH_MULT", 1.30)
		}
	}
	if postTP3 {
		if p != nil && p.Sponsored {
			dist *= envFloat("LIVE_TRAIL_SPONSORED_POST_TP3_MULT", 1.25)
		} else {
			dist *= envFloat("LIVE_TRAIL_UNSPONSORED_POST_TP3_MULT", 0.95)
		}
	}
	if sideBuy {
		return ref - dist
	}
	return ref + dist
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
	if m == nil || p == nil {
		return nil
	}
	if m.rest == nil {
		p.TP1OrderID, p.TP2OrderID, p.TP3OrderID, p.StopOrderID = 0, 0, 0, 0
		return nil
	}
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

func benignZeroRoundedQtyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "qty must be > 0")
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
		dayRealized := m.dayRealizedAt(now)
		_ = m.cancelRemainingExits(p)
		_, _ = m.rest.CancelAllOrders(sym)
		if err := m.submitCloseLimit(p, p.RemainingQty, reason, "FORCE_CLOSE"); err != nil {
			continue
		}
		if m.tg != nil {
			m.tg.Sendf("%s", notify.FormatRiskAlert(notify.RiskView{
				RiskState:      "LIMITED",
				SymbolOrScope:  fmt.Sprintf("%s %s", cleanSymbol(sym), displayPositionSide(p.Side)),
				RiskMessage:    fmt.Sprintf("Forced close queued at %s · PnL %+.2f (%+.2f%%)", fmtPrice(p.PendingExitPrice), pnl, pct),
				OperatorAction: fmt.Sprintf("%s · Day %+.2f", reason, dayRealized),
			}))
		}
	}
	_ = m.save()
	return nil
}

func (m *liveExecManager) ForceCloseNonWinners(reason string) (int, int, error) {
	if m == nil || m.rest == nil {
		return 0, 0, nil
	}
	now := time.Now().UTC()
	closed := 0
	keptWinners := 0
	for sym, p := range m.positions {
		if p == nil || p.State == execClosed {
			continue
		}
		mark, _ := m.currentMark(sym)
		if mark <= 0 {
			mark = p.LastMark
		}
		if mark <= 0 {
			mark = p.EntryPrice
		}
		pnl, pct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
		if pnl > 0 {
			keptWinners++
			continue
		}
		dayRealized := m.dayRealizedAt(now)
		_ = m.cancelRemainingExits(p)
		_, _ = m.rest.CancelAllOrders(sym)
		if err := m.submitCloseLimit(p, p.RemainingQty, reason, "FORCE_CLOSE"); err != nil {
			continue
		}
		closed++
		if m.tg != nil {
			m.tg.Sendf("%s", notify.FormatRiskAlert(notify.RiskView{
				RiskState:      "LIMITED",
				SymbolOrScope:  fmt.Sprintf("%s %s", cleanSymbol(sym), displayPositionSide(p.Side)),
				RiskMessage:    fmt.Sprintf("Forced close queued at %s · PnL %+.2f (%+.2f%%)", fmtPrice(p.PendingExitPrice), pnl, pct),
				OperatorAction: fmt.Sprintf("%s · Day %+.2f", reason, dayRealized),
			}))
		}
	}
	_ = m.save()
	return closed, keptWinners, nil
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
	dayRealized := m.dayRealizedAt(now)
	_ = m.cancelRemainingExits(p)
	_, _ = m.rest.CancelAllOrders(raw)
	if err := m.submitCloseLimit(p, p.RemainingQty, reason, "FORCE_CLOSE"); err != nil {
		return false, err
	}
	if m.tg != nil {
		m.tg.Sendf("%s", notify.FormatRiskAlert(notify.RiskView{
			RiskState:      "LIMITED",
			SymbolOrScope:  fmt.Sprintf("%s %s", cleanSymbol(raw), displayPositionSide(p.Side)),
			RiskMessage:    fmt.Sprintf("Forced close queued at %s · PnL %+.2f (%+.2f%%)", fmtPrice(p.PendingExitPrice), pnl, pct),
			OperatorAction: fmt.Sprintf("%s · Day %+.2f", reason, dayRealized),
		}))
	}
	_ = m.save()
	return true, nil
}

func (m *liveExecManager) ApplyMomentumExit(now time.Time, mom map[string]momentumView, flow map[string]flowMetrics, ext map[string]flowfeed.ExternalSignal) {
	if m == nil || m.rest == nil || !envBool("LIVE_MOMENTUM_EXIT_ENABLE", false) || len(m.positions) == 0 {
		return
	}
	slopeMax := envFloat("LIVE_MOMENTUM_EXIT_SLOPE_MAX", 0.0)
	minHold := time.Duration(envInt("LIVE_MOMENTUM_EXIT_MIN_HOLD_MIN", 35)) * time.Minute
	minUpnlPct := envFloat("LIVE_MOMENTUM_EXIT_MIN_UPNL_PCT", 0.25)
	minMFER := envFloat("LIVE_MOMENTUM_EXIT_MIN_MFE_R", 1.75)
	sponsorMinScore := envFloat("LIVE_EXIT_SPONSOR_MIN_SCORE", 70.0)
	sponsorMinSlope := envFloat("LIVE_EXIT_SPONSOR_MIN_SLOPE", 0.02)
	sponsorFadeHoldMin := time.Duration(envInt("LIVE_EXIT_SPONSOR_FADE_HOLD_MIN", 120)) * time.Minute
	swingHoldEnable := envBool("LIVE_SWING_HOLD_GUARD_ENABLE", true)
	swingHoldLog := envBool("LIVE_SWING_HOLD_GUARD_LOG", true)
	changed := false
	for sym, p := range m.positions {
		if p == nil || p.State == execClosed || p.RemainingQty <= 0 {
			continue
		}
		mv := mom[sym]
		if !shouldExitOnMomentumFade(p.Side, mv, slopeMax) {
			continue
		}
		sponsored := isSponsoredMomentum(p.Side, mv, sponsorMinScore, sponsorMinSlope)
		if sponsored && now.Sub(p.CreatedAt) < sponsorFadeHoldMin {
			continue
		}
		if confluenceRefreshActive(now, p.LastConfluenceRefresh) {
			continue
		}
		if swingHoldEnable {
			if holdScore, holdReason, hold := evaluateSwingHold(p.Side, mv, sponsored, confluenceRefreshActive(now, p.LastConfluenceRefresh), p.MaxFavorableR, p.HitTP1, p.HitTP2, p.HitTP3); hold {
				if swingHoldLog {
					fmt.Printf("live: swing hold %s %s score=%.2f reason=%s state=%s slope=%.3f\n",
						sym, p.Side, holdScore, holdReason, sameSideMomentumEntry(p.Side, mv).State, sameSideMomentumEntry(p.Side, mv).ScoreSlope)
				}
				continue
			}
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
		runnerState := evaluateRunnerExitStateWithFlow(p.Side, mv, flow[sym], ext[sym])
		htf := m.htfSnapshot(sym, p.Side, sameSideMomentumEntry(p.Side, mv))
		prevLifecycle := p.WinnerLifecycle
		syncLiveWinnerLifecycle(p, htfPersistent(p.Side, htf))
		logWinnerLifecycleTransition(sym, p.Side, prevLifecycle, p.WinnerLifecycle, lifecycleTransitionReason(prevLifecycle, p.WinnerLifecycle, "", p.TrailOn))
		updateManagePhase(p, runnerState.ExhaustionConfirmed && !runnerState.StructureBroken)
		refreshRunnerReservation(p, m.ladderCfg.StarterUSDT)
		if m.exitManager != nil {
			dec := m.exitManager.EvaluateProtect(exitmgr.ProtectInput{
				Side:               p.Side,
				Entry:              p.EntryPrice,
				Stop:               p.StopPrice,
				Mark:               mark,
				MFER:               p.MaxFavorableR,
				MAER:               p.MaxAdverseR,
				BarsHeld:           int(now.Sub(p.CreatedAt) / time.Minute),
				StallBars:          p.StallBars,
				WeakFlow:           shouldExitOnMomentumFade(p.Side, mv, slopeMax),
				LiqSpike:           ext[sym].LiqSpike,
				UnrealizedPct:      upct,
				Sponsored:          sponsored,
				HitTP1:             p.HitTP1,
				HitTP2:             p.HitTP2,
				HitTP3:             p.HitTP3,
				WeakSponsorStreak:  p.WeakSponsorStreak,
				EntryReason:        p.EntryReason,
				EntryStrategyID:    p.EntryStrategyID,
				StarterEntry:       false,
				AdvancedReady:      p.ManagePhase == managePhaseContinuation || p.ManagePhase == managePhaseExhaustion || earlyContinuationReady(p),
				HTFTrendState:      string(htf.State),
				HTFTrendPersistent: htfPersistent(p.Side, htf),
				HTFTrendFailed:     htfFailed(p.Side, htf),
				HTFCaution:         htfCaution(p.Side, htf),
				TriggerRef:         m.stopTriggerRef,
				ComputedStop:       p.StopPrice,
				SubmittedStop:      p.StopPrice,
				AcceptedStop:       p.StopPrice,
				LegalityAdjusted:   false,
				WinnerLifecycle:    p.WinnerLifecycle,
				TrailingActive:     p.TrailOn,
				MatureTrend:        matureTrendForWinnerLifecycle(p.MaxFavorableR, htfPersistent(p.Side, htf), p.HitTP1, p.HitTP2, p.HitTP3),
				RealInvalidation:   runnerState.StructureBroken || htfFailed(p.Side, htf),
			})
			logWinnerLifecycleTransition(sym, p.Side, p.WinnerLifecycle, dec.WinnerLifecycle, lifecycleTransitionReason(p.WinnerLifecycle, dec.WinnerLifecycle, firstNonEmpty(dec.ExitNowReason, dec.Reason), p.TrailOn))
			p.WinnerLifecycle = dec.WinnerLifecycle
			if dec.PartialExitPct > 0 && p.RemainingQty > 0 {
				q := p.RemainingQty * dec.PartialExitPct
				if q > 0 && q < p.RemainingQty {
					logProtectDecisionOnce(&p.lastProtectDecisionKey, protectDecisionLogLine(sym, p.Side, "PARTIAL", dec.CurrentWinnerLifecycle, dec.WinnerLifecycle, dec.Reason, dec.ComputedStop, dec.SubmittedStop, dec.AcceptedStop, dec.TriggerRef, dec.LegalityAdjusted, p.MaxFavorableR, dec.HTFTrendState, dec.HTFPersistent, dec.HTFFailed, dec.HTFCaution))
					if err := m.submitCloseLimit(p, q, dec.Reason, "TRIM"); err == nil {
						changed = true
					}
				}
			}
			if dec.MoveStopToBE && allowMoveToBreakEven(p.HitTP1, upct) {
				be := beLockPriceBuffered(p.Side, p.EntryPrice, p.StopPrice, m.beLockBps)
				if (strings.EqualFold(p.Side, "BUY") && be > p.StopPrice) || (strings.EqualFold(p.Side, "SELL") && be < p.StopPrice) {
					p.StopPrice = be
					queueLiveProtectStopChain(p, "BE", dec, be)
					_ = m.placeOrReplaceStop(p)
				}
			}
			if dec.TightenStop {
				if (strings.EqualFold(p.Side, "BUY") && dec.TightenToPrice > p.StopPrice) || (strings.EqualFold(p.Side, "SELL") && dec.TightenToPrice < p.StopPrice) {
					p.StopReason = dec.Reason
					p.StopPrice = dec.TightenToPrice
					queueLiveProtectStopChain(p, "TIGHTEN", dec, dec.TightenToPrice)
					_ = m.placeOrReplaceStop(p)
					changed = true
				}
			}
			if correctedStop, corrected := enforceWinnerBEFloor(p.Side, p.EntryPrice, p.StopPrice, p.MaxFavorableR); corrected {
				p.StopReason = "forced_be_correction"
				p.StopPrice = correctedStop
				queueLiveProtectStopChain(p, "TIGHTEN", exitmgr.ProtectDecision{
					CurrentWinnerLifecycle: p.WinnerLifecycle,
					WinnerLifecycle:        p.WinnerLifecycle,
					Reason:                 "forced_be_correction",
					TriggerRef:             m.stopTriggerRef,
					HTFTrendState:          string(htf.State),
					HTFPersistent:          htfPersistent(p.Side, htf),
					HTFFailed:              htfFailed(p.Side, htf),
					HTFCaution:             htfCaution(p.Side, htf),
				}, correctedStop)
				_ = m.placeOrReplaceStop(p)
				changed = true
			}
			if dec.ImmediateExit {
				logProtectDecisionOnce(&p.lastProtectDecisionKey, protectDecisionLogLine(sym, p.Side, "IMMEDIATE_EXIT", dec.CurrentWinnerLifecycle, dec.WinnerLifecycle, firstNonEmpty(dec.ExitNowReason, dec.Reason, "IMMEDIATE_EXIT"), dec.ComputedStop, dec.SubmittedStop, dec.AcceptedStop, dec.TriggerRef, dec.LegalityAdjusted, p.MaxFavorableR, dec.HTFTrendState, dec.HTFPersistent, dec.HTFFailed, dec.HTFCaution))
				reason := firstNonEmpty(dec.ExitNowReason, dec.Reason, "IMMEDIATE_EXIT")
				_ = m.cancelRemainingExits(p)
				if err := m.submitCloseLimit(p, p.RemainingQty, reason, "CLOSE"); err == nil {
					changed = true
				}
				continue
			}
			if runnerState.ExhaustionConfirmed && !runnerState.StructureBroken {
				if m.trimToRunner(now, p, "RUNNER_EXHAUST_TRIM") {
					changed = true
				}
				if m.tightenRunnerStop(p, runnerState.TightenReason) {
					changed = true
				}
			}
			if dec.FullExit {
				if !lifecycleSoftExitsCanHardClose(p.WinnerLifecycle) {
					continue
				}
				logProtectDecisionOnce(&p.lastProtectDecisionKey, protectDecisionLogLine(sym, p.Side, "FULL_EXIT", dec.CurrentWinnerLifecycle, dec.WinnerLifecycle, firstNonEmpty(runnerState.FullExitReason, dec.Reason), dec.ComputedStop, dec.SubmittedStop, dec.AcceptedStop, dec.TriggerRef, dec.LegalityAdjusted, p.MaxFavorableR, dec.HTFTrendState, dec.HTFPersistent, dec.HTFFailed, dec.HTFCaution))
				if !runnerState.StructureBroken {
					if runnerState.ExhaustionConfirmed && m.tightenRunnerStop(p, runnerState.TightenReason) {
						changed = true
					}
					continue
				}
				reason := firstNonEmpty(runnerState.FullExitReason, dec.Reason)
				pnl, pct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
				dayRealized := m.dayRealizedAt(now)
				_ = m.cancelRemainingExits(p)
				if err := m.submitCloseLimit(p, p.RemainingQty, reason, "CLOSE"); err != nil {
					continue
				}
				m.emitNotify(notify.Event{
					Key:      "MOMENTUM_EXIT_SUBMITTED",
					Title:    "MOMENTUM EXIT SUBMITTED",
					Class:    notify.ClassLifecycle,
					Severity: notify.SeverityInfo,
					Route:    notify.RouteNormal,
					Symbol:   sym,
					Message:  "momentum exit submitted",
					Metadata: map[string]string{
						"qty":  fmt.Sprintf("%.6f", p.PendingExitQty),
						"px":   fmtPrice(p.PendingExitPrice),
						"pnl":  fmt.Sprintf("%+.2f", pnl),
						"upnl": fmt.Sprintf("%+.2f%%", pct),
						"day":  fmt.Sprintf("%+.2f", dayRealized),
					},
				})
				changed = true
				continue
			}
		}
		if runnerState.ExhaustionConfirmed && !runnerState.StructureBroken {
			if m.trimToRunner(now, p, "RUNNER_EXHAUST_TRIM") {
				changed = true
			}
			if m.tightenRunnerStop(p, runnerState.TightenReason) {
				changed = true
			}
			continue
		}
		if envBool("LIVE_MOMENTUM_FADE_TIGHTEN_AFTER_CONFIRM", true) &&
			(p.HitTP1 || p.HitTP2 || p.HitTP3 || p.ProtectionStage >= protectionStageArmed || earlyContinuationReady(p)) {
			if !(envBool("LIVE_MOMENTUM_FADE_REQUIRE_STRUCTURE_LOSS_AFTER_CONFIRM", true) && (p.Sponsored || confluenceRefreshActive(now, p.LastConfluenceRefresh))) {
				if stop, tightened := applyLiveProtectionState(now, p.Side, p.EntryPrice, p.StopPrice, maxFloat(p.MaxFavorableR, envFloat("LIVE_PROFIT_LOCK_STAGE1_R", 1.0)), &p.ProtectionStage, &p.FirstProtectAt, &p.ProtectedStop, m.beLockBps, allowMoveToBreakEven(p.HitTP1, upct)); tightened {
					p.StopReason = "MOMENTUM_FADE_TIGHTEN"
					p.StopPrice = stop
					_ = m.placeOrReplaceStop(p)
					changed = true
				}
			}
			continue
		}
		if envBool("LIVE_EXIT_SOFT_SIGNALS_MANAGE_ONLY", true) &&
			htfPersistent(p.Side, htf) && !htfFailed(p.Side, htf) {
			if m.tightenRunnerStop(p, "MOMENTUM_FADE_TIGHTEN_HTF") {
				changed = true
			}
			continue
		}
		if !runnerState.StructureBroken {
			continue
		}
		if !lifecycleSoftExitsCanHardClose(p.WinnerLifecycle) {
			continue
		}
		reason := firstNonEmpty(runnerState.FullExitReason, "MOMENTUM_FADE")
		pnl, pct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
		dayRealized := m.dayRealizedAt(now)
		_ = m.cancelRemainingExits(p)
		if err := m.submitCloseLimit(p, p.RemainingQty, reason, "CLOSE"); err != nil {
			continue
		}
		m.emitNotify(notify.Event{
			Key:      "MOMENTUM_EXIT_SUBMITTED",
			Title:    "MOMENTUM EXIT SUBMITTED",
			Class:    notify.ClassLifecycle,
			Severity: notify.SeverityInfo,
			Route:    notify.RouteNormal,
			Symbol:   sym,
			Message:  "momentum exit submitted",
			Metadata: map[string]string{
				"qty":  fmt.Sprintf("%.6f", p.PendingExitQty),
				"px":   fmtPrice(p.PendingExitPrice),
				"pnl":  fmt.Sprintf("%+.2f", pnl),
				"upnl": fmt.Sprintf("%+.2f%%", pct),
				"day":  fmt.Sprintf("%+.2f", dayRealized),
			},
		})
		changed = true
	}
	if changed {
		_ = m.save()
	}
}

func (m *liveExecManager) ApplyFundingExit(now time.Time, meta map[string]symbolMeta) {
	if m == nil || m.rest == nil || !m.fundingExitEnable || len(m.positions) == 0 || m.fundingEvery <= 0 {
		return
	}
	nextFunding := now.UTC().Truncate(m.fundingEvery).Add(m.fundingEvery)
	if m.fundingExitWindow > 0 && nextFunding.Sub(now.UTC()) > m.fundingExitWindow {
		return
	}
	changed := false
	for sym, p := range m.positions {
		if p == nil || p.State == execClosed || p.RemainingQty <= 0 {
			continue
		}
		fr := meta[sym].FundingRate
		if fr == 0 || !fundingCostsPosition(p.Side, fr) {
			continue
		}
		age := now.Sub(p.CreatedAt)
		if age < m.fundingExitMinAge || p.HitTP3 {
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
		hitTP2 := p.State == execPartialTP2 || p.HitTP3
		weakHold := upct <= m.fundingExitMaxUpnl && p.MaxFavorableR < m.fundingExitMinMFER && !hitTP2
		staleCarry := age >= maxDuration(m.fundingExitMinAge*2, m.fundingEvery*2) &&
			upct <= m.fundingExitMaxUpnl*1.5 &&
			p.MaxFavorableR < m.fundingExitMinMFER*1.5
		expensiveCarry := m.expensiveFundingRate > 0 && abs(fr) >= m.expensiveFundingRate
		if !(weakHold || (expensiveCarry && staleCarry)) {
			continue
		}
		pnl, pct := realizedFromFill(p.Side, p.EntryPrice, mark, p.RemainingQty)
		dayRealized := m.dayRealizedAt(now)
		_ = m.cancelRemainingExits(p)
		if err := m.submitCloseLimit(p, p.RemainingQty, "FUNDING", "CLOSE"); err != nil {
			continue
		}
		m.emitNotify(notify.Event{
			Key:      "PRE_FUNDING_EXIT_SUBMITTED",
			Title:    "PRE-FUNDING EXIT SUBMITTED",
			Class:    notify.ClassLifecycle,
			Severity: notify.SeverityInfo,
			Route:    notify.RouteNormal,
			Symbol:   sym,
			Message:  "funding-aware pre-exit submitted",
			Metadata: map[string]string{
				"qty":  fmt.Sprintf("%.6f", p.PendingExitQty),
				"px":   fmtPrice(p.PendingExitPrice),
				"pnl":  fmt.Sprintf("%+.2f", pnl),
				"upnl": fmt.Sprintf("%+.2f%%", pct),
				"day":  fmt.Sprintf("%+.2f", dayRealized),
			},
		})
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
		dayRealized := m.dayRealizedAt(now)
		_ = m.cancelRemainingExits(p)
		if err := m.submitCloseLimit(p, p.RemainingQty, reason, "CLOSE"); err != nil {
			continue
		}
		m.emitNotify(notify.Event{
			Key:      "PRE_EOD_EXIT_SUBMITTED",
			Title:    "PRE-EOD EXIT SUBMITTED",
			Class:    notify.ClassLifecycle,
			Severity: notify.SeverityInfo,
			Route:    notify.RouteNormal,
			Symbol:   sym,
			Message:  "pre-EOD exit submitted",
			Metadata: map[string]string{
				"qty":    fmt.Sprintf("%.6f", p.PendingExitQty),
				"px":     fmtPrice(p.PendingExitPrice),
				"pnl":    fmt.Sprintf("%+.2f", pnl),
				"upnl":   fmt.Sprintf("%+.2f%%", pct),
				"reason": reason,
				"day":    fmt.Sprintf("%+.2f", dayRealized),
			},
		})
		changed = true
	}
	if changed {
		_ = m.save()
	}
}

func exitLimitCrossBps() float64 {
	return maxFloat(0, envFloat("LIVE_EXIT_LIMIT_CROSS_BPS", 8.0))
}

func (m *liveExecManager) exitLimitPrice(symbol, side string) (float64, error) {
	bid, ask, err := m.rest.BookTicker(symbol)
	if err != nil {
		return 0, err
	}
	price := 0.0
	cross := exitLimitCrossBps() / 10000.0
	if isLongSide(side) {
		if bid <= 0 {
			return 0, fmt.Errorf("invalid bid for exit")
		}
		price = bid * (1 - cross)
	} else {
		if ask <= 0 {
			return 0, fmt.Errorf("invalid ask for exit")
		}
		price = ask * (1 + cross)
	}
	price, _, err = m.rest.RoundPrice(symbol, price)
	if err != nil {
		return 0, err
	}
	if price <= 0 {
		return 0, fmt.Errorf("invalid exit price")
	}
	return price, nil
}

func (m *liveExecManager) submitCloseLimit(p *livePosition, qty float64, reason, action string) error {
	if m == nil || m.rest == nil || p == nil {
		return fmt.Errorf("execution manager not ready")
	}
	if p.State == execClosed || p.RemainingQty <= 0 {
		return nil
	}
	if p.PendingExitOrderID > 0 {
		return nil
	}
	_ = m.cancelRemainingExits(p)
	meta, err := m.rest.SymbolMeta(p.Symbol, true)
	if err != nil {
		return err
	}
	qty, _, err = m.rest.RoundQty(p.Symbol, min(maxFloat(0, qty), p.RemainingQty))
	if err != nil {
		return err
	}
	if qty <= 0 {
		return nil
	}
	price, err := m.exitLimitPrice(p.Symbol, p.Side)
	if err != nil {
		return err
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
	intentID := buildIntentID(p.Symbol, firstNonEmpty(strings.TrimSpace(p.EntryStrategyID), strings.TrimSpace(p.EntryReason)), price, time.Now().UTC())
	if m.isUnknownExecutionFrozen(p.Symbol) {
		logUnknownExecution(p.Symbol, intentID, "submit_skipped", "symbol frozen by unknown execution guard")
		return fmt.Errorf("unknown_execution_frozen")
	}
	out, err := m.rest.PlaceOrder(vals)
	if err != nil {
		if isAmbiguousVenueOutcome(err) {
			m.handleUnknownExecution(p.Symbol, intentID)
			time.Sleep(time.Duration(envInt("LIVE_EXEC_RECONCILE_AFTER_UNKNOWN_MS", 800)) * time.Millisecond)
			_ = m.reconcileAfterUnknown(p.Symbol, intentID)
		}
		return err
	}
	orderID := mapInt64(out["orderId"])
	if orderID == 0 {
		return fmt.Errorf("missing orderId from exit limit response")
	}
	p.PendingExitOrderID = orderID
	p.PendingExitPrice = price
	p.PendingExitQty = qty
	p.PendingExitFilledQty = 0
	p.PendingExitCreatedAt = time.Now().UTC()
	p.PendingExitReason = strings.ToUpper(strings.TrimSpace(reason))
	p.PendingExitAction = strings.ToUpper(strings.TrimSpace(action))
	p.UpdatedAt = p.PendingExitCreatedAt
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

func (p *paperTrader) ConsoleSummary(meta map[string]symbolMeta) string {
	if p == nil || !p.enabled {
		return ""
	}
	openPnL := 0.0
	openCount := 0
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		openCount++
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
	dayKey := time.Now().In(p.reportLoc).Format("2006-01-02")
	realized := 0.0
	if ds := p.dayStats[dayKey]; ds != nil {
		realized = ds.Net
	}
	eq := p.balance + openPnL
	return fmt.Sprintf("PAPER   eq=%.2f bal=%.2f pnl=%+.2f day=%+.2f open=%d/%d",
		eq, p.balance, openPnL, realized+openPnL, openCount, p.maxOpen)
}

func (p *paperTrader) ConsolePositions(meta map[string]symbolMeta) []string {
	if p == nil || !p.enabled {
		return nil
	}
	type row struct {
		sym   string
		side  string
		size  float64
		entry float64
		mark  float64
		upnl  float64
		lev   int
	}
	rows := make([]row, 0, len(p.positions))
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		mark := meta[raw].LastPrice
		upnl := 0.0
		if mark > 0 {
			if strings.EqualFold(pos.Side, "BUY") {
				upnl = (mark - pos.Entry) * pos.Qty
			} else {
				upnl = (pos.Entry - mark) * pos.Qty
			}
		}
		rows = append(rows, row{
			sym:   raw,
			side:  pos.Side,
			size:  pos.Qty,
			entry: pos.Entry,
			mark:  mark,
			upnl:  upnl,
			lev:   maxInt(pos.Leverage, 1),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].upnl > rows[j].upnl })
	if len(rows) == 0 {
		return nil
	}
	lines := make([]string, 0, len(rows)+2)
	lines = append(lines, "  +------+------------+-------+-----------+------------+------------+----------+---------+-----+")
	lines = append(lines, "  | src  | symbol     | side  | qty       | entry      | mark       | uPnL     | uPnL%   | lev |")
	for i := 0; i < len(rows); i++ {
		r := rows[i]
		lines = append(lines, formatConsolePositionLine("paper", r.sym, r.side, r.size, r.entry, r.mark, r.upnl, r.lev))
	}
	lines = append(lines, "  +------+------------+-------+-----------+------------+------------+----------+---------+-----+")
	return lines
}

func paperPositionMarginUsed(pos *paperPosition) float64 {
	if pos == nil || pos.Qty <= 0 {
		return 0
	}
	if pos.InitialQty > 0 && pos.Margin > 0 {
		used := pos.Margin * (pos.Qty / pos.InitialQty)
		return clamp(used, 0, pos.Margin)
	}
	return maxFloat(pos.Margin, 0)
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
		marginUsed := paperPositionMarginUsed(pos)
		totalUPnL += upnl
		totalMargin += marginUsed
		rows = append(rows, row{
			sym:    raw,
			side:   pos.Side,
			margin: marginUsed,
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
		return tgPre(fmt.Sprintf("%s (%s) session=%s\npaper disabled", strings.TrimSpace(label), now.Format("15:04 MST"), sessionTag(now)))
	}
	return buildClassicDigest(strings.TrimSpace(label), now, p, meta, longInPlay, shortInPlay)
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
		used += paperPositionMarginUsed(pos)
	}
	free := p.balance - p.reserve - used
	if free < 0 {
		return 0
	}
	return free
}

func fundingCostsPosition(side string, fundingRate float64) bool {
	if fundingRate == 0 {
		return false
	}
	if strings.EqualFold(side, "BUY") {
		return fundingRate > 0
	}
	return fundingRate < 0
}

func paperCurrentEntryForSide(side, raw string, longCurrent, shortCurrent map[string]inplay.Entry) (inplay.Entry, bool) {
	raw = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(raw)))
	if strings.EqualFold(side, "BUY") {
		e, ok := longCurrent[raw]
		return e, ok
	}
	e, ok := shortCurrent[raw]
	return e, ok
}

func paperTrendLikeState(st inplay.State) bool {
	switch st {
	case inplay.StateHeating, inplay.StateInPlay, inplay.StatePumping:
		return true
	default:
		return false
	}
}

func paperEntryStyleSupportsSide(side string, e inplay.Entry) bool {
	switch strings.TrimSpace(e.EntryStyle) {
	case "", "breakout_hold", "pullback_only":
		return true
	case "none", "avoid_chase", "reversal_watch_short", "reversal_watch_long":
		return false
	case "momentum_ignite_long", "breakout_hold_long", "pullback_long":
		return strings.EqualFold(side, "BUY")
	case "momentum_ignite_short", "breakout_hold_short", "pullback_short":
		return !strings.EqualFold(side, "BUY")
	default:
		return true
	}
}

func paperEntryOpposesSide(side string, e inplay.Entry) bool {
	if !paperEntryStyleSupportsSide(side, e) {
		return true
	}
	if strings.EqualFold(side, "BUY") {
		return e.LongDemotionFlag || strings.TrimSpace(e.EntryStyle) == "reversal_watch_short" || strings.TrimSpace(e.MetaState) == "long_exhausting"
	}
	return e.ShortDemotionFlag || strings.TrimSpace(e.EntryStyle) == "reversal_watch_long" || strings.TrimSpace(e.MetaState) == "short_exhausting"
}

func paperEntryTrendProtected(side string, e inplay.Entry, scoreMin, slopeMin, maxStateMin float64) bool {
	if paperEntryOpposesSide(side, e) || !paperTrendLikeState(e.State) {
		return false
	}
	if scoreMin > 0 && e.CurrentScore < scoreMin {
		return false
	}
	if e.ScoreSlope < slopeMin && !e.Momentum {
		return false
	}
	if maxStateMin > 0 && e.TimeInStateMin > maxStateMin && !e.Momentum {
		return false
	}
	return true
}

func paperDegradedHoldExitReason(now time.Time, pos *paperPosition, mark float64, longCurrent, shortCurrent map[string]inplay.Entry) string {
	if pos == nil || mark <= 0 || !envBool("LIVE_PAPER_DEGRADED_EXIT_ENABLE", true) {
		return ""
	}
	minHold := time.Duration(envInt("LIVE_PAPER_DEGRADED_EXIT_MIN_HOLD_MIN", 20)) * time.Minute
	if minHold > 0 && now.Sub(pos.OpenedAt) < minHold {
		return ""
	}
	maxUpnlPct := envFloat("LIVE_PAPER_DEGRADED_EXIT_MAX_UPNL_PCT", 1.50)
	maxMFER := envFloat("LIVE_PAPER_DEGRADED_EXIT_MAX_MFE_R", 1.10)
	if pos.HitTP2 || (maxMFER > 0 && pos.MaxFavorableR > maxMFER) {
		return ""
	}
	_, upnlPct := realizedFromFill(pos.Side, pos.Entry, mark, pos.Qty)
	if upnlPct > maxUpnlPct {
		return ""
	}
	cur, ok := paperCurrentEntryForSide(pos.Side, pos.Symbol, longCurrent, shortCurrent)
	slopeMax := envFloat("LIVE_PAPER_DEGRADED_EXIT_SLOPE_MAX", 0.05)
	if ok && !paperEntryOpposesSide(pos.Side, cur) && paperTrendLikeState(cur.State) && (cur.Momentum || cur.ScoreSlope > slopeMax) {
		return ""
	}
	strongDegrade := !ok
	if ok && (paperEntryOpposesSide(pos.Side, cur) || cur.State == inplay.StateExhausted || cur.State == inplay.StateDumping) {
		strongDegrade = true
	}
	if !strongDegrade {
		minStallBars := envInt("LIVE_PAPER_DEGRADED_EXIT_MIN_STALL_BARS", 3)
		if pos.StallBars < minStallBars {
			return ""
		}
		if !ok || (cur.State != inplay.StateBalanced && cur.State != inplay.StateCooling && !(cur.ScoreSlope <= slopeMax && !cur.Momentum)) {
			return ""
		}
	}
	if pos.HitTP1 || pos.MaxFavorableR >= 0.75 || upnlPct > 0 {
		return "MOMENTUM_FADE"
	}
	return ""
}

func paperAdvancedReady(pos *paperPosition) bool {
	if pos == nil {
		return false
	}
	if pos.HitTP1 || pos.HitTP2 || pos.HitTP3 {
		return true
	}
	minR := envFloat("LIVE_EARLY_CONTINUATION_MIN_R", 0.35)
	if minR > 0 && pos.MaxFavorableR >= minR {
		return true
	}
	minHold := time.Duration(envInt("LIVE_EARLY_CONTINUATION_MIN_HOLD_MIN", 8)) * time.Minute
	if minHold > 0 && !pos.OpenedAt.IsZero() && time.Since(pos.OpenedAt) >= minHold && pos.MaxFavorableR > 0 {
		return true
	}
	if pos.Sponsored || confluenceRefreshActive(time.Now().UTC(), pos.LastConfluenceRefresh) {
		return true
	}
	return false
}

func (p *paperTrader) applyPaperProtectDecision(now time.Time, raw string, pos *paperPosition, mark float64, dec exitmgr.ProtectDecision, meta map[string]symbolMeta, depth map[string]aster.OrderBook) bool {
	if p == nil || pos == nil {
		return false
	}
	changed := false
	if dec.MoveStopToBE && allowMoveToBreakEven(pos.HitTP1, maxFloat(0, pos.MaxFavorableR*100.0)) {
		be := beLockPriceBuffered(pos.Side, pos.Entry, pos.Stop, p.beLockBps)
		if (strings.EqualFold(pos.Side, "BUY") && be > pos.Stop) || (!strings.EqualFold(pos.Side, "BUY") && be < pos.Stop) {
			pos.StopReason = firstNonEmpty(dec.Reason, "WEAK_FLOW_BE")
			pos.Stop = be
			changed = true
		}
	}
	if dec.TightenStop {
		if (strings.EqualFold(pos.Side, "BUY") && dec.TightenToPrice > pos.Stop) || (!strings.EqualFold(pos.Side, "BUY") && dec.TightenToPrice < pos.Stop) {
			pos.StopReason = firstNonEmpty(dec.Reason, "PROTECT_TIGHTEN")
			pos.Stop = dec.TightenToPrice
			changed = true
		}
	}
	if correctedStop, corrected := enforceWinnerBEFloor(pos.Side, pos.Entry, pos.Stop, pos.MaxFavorableR); corrected {
		pos.StopReason = "forced_be_correction"
		pos.Stop = correctedStop
		changed = true
	}
	if dec.PartialExitPct > 0 && pos.Qty > 0 {
		q := pos.Qty * dec.PartialExitPct
		if q > 0 && q < pos.Qty {
			logPaperProtectDecision(raw, pos, "PARTIAL", dec, pos.Stop, pos.Stop, false)
			p.exitPortion(now, pos, firstNonEmpty(dec.Reason, "SOFT_PARTIAL"), mark, q, meta[raw], depth[raw])
			return true
		}
	}
	if dec.ImmediateExit {
		reason := firstNonEmpty(dec.ExitNowReason, dec.Reason, "IMMEDIATE_EXIT")
		dec.Reason = reason
		logPaperProtectDecision(raw, pos, "IMMEDIATE_EXIT", dec, dec.SubmittedStop, dec.AcceptedStop, dec.LegalityAdjusted)
		p.exitPortion(now, pos, reason, mark, pos.Qty, meta[raw], depth[raw])
		return true
	}
	if dec.FullExit {
		if !lifecycleSoftExitsCanHardClose(pos.WinnerLifecycle) {
			if !changed {
				logPaperProtectDecision(raw, pos, "TIGHTEN", dec, pos.Stop, pos.Stop, false)
			}
			return changed
		}
		logPaperProtectDecision(raw, pos, "FULL_EXIT", dec, dec.SubmittedStop, dec.AcceptedStop, dec.LegalityAdjusted)
		p.exitPortion(now, pos, firstNonEmpty(dec.Reason, "DEGRADED_EXIT"), mark, pos.Qty, meta[raw], depth[raw])
		return true
	}
	if changed {
		action := "TIGHTEN"
		if dec.MoveStopToBE && !dec.TightenStop {
			action = "BE"
		}
		logPaperProtectDecision(raw, pos, action, dec, pos.Stop, pos.Stop, false)
	}
	return changed
}

func (p *paperTrader) ApplyFunding(now time.Time, meta map[string]symbolMeta, longCurrent, shortCurrent map[string]inplay.Entry) {
	if p == nil || !p.enabled || !p.fundingEnabled || len(p.positions) == 0 || p.fundingEvery <= 0 {
		return
	}
	expensiveFundingRate := envFloat("LIVE_PAPER_PRE_FUNDING_EXPENSIVE_RATE", 0.0008)
	trendScoreMin := envFloat("LIVE_PAPER_PRE_FUNDING_TREND_SCORE_MIN", 90.0)
	trendSlopeMin := envFloat("LIVE_PAPER_PRE_FUNDING_TREND_SLOPE_MIN", 0.10)
	trendMaxStateMin := envFloat("LIVE_PAPER_PRE_FUNDING_TREND_MAX_STATE_MIN", 45.0)
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
		if p.fundingExitEnable && fundingCostsPosition(pos.Side, m.FundingRate) {
			age := now.Sub(pos.OpenedAt)
			upnl := 0.0
			if strings.EqualFold(pos.Side, "BUY") {
				upnl = (mark - pos.Entry) * pos.Qty
			} else {
				upnl = (pos.Entry - mark) * pos.Qty
			}
			cur, ok := paperCurrentEntryForSide(pos.Side, raw, longCurrent, shortCurrent)
			trendProtected := ok && paperEntryTrendProtected(pos.Side, cur, trendScoreMin, trendSlopeMin, trendMaxStateMin)
			weakHold := upnl <= p.fundingExitMaxUpnl && pos.MaxFavorableR < p.fundingExitMinMFER
			staleCarry := age >= maxDuration(p.fundingExitMinAge*2, p.fundingEvery*2) &&
				upnl <= p.fundingExitMaxUpnl*1.5 &&
				pos.MaxFavorableR < p.fundingExitMinMFER*1.5
			expensiveCarry := expensiveFundingRate > 0 && abs(m.FundingRate) >= expensiveFundingRate
			if age >= p.fundingExitMinAge && !pos.HitTP2 && !trendProtected && (weakHold || (expensiveCarry && staleCarry)) {
				p.exitPortion(now, pos, "FUNDING", mark, pos.Qty, m, aster.OrderBook{})
				continue
			}
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

func (p *paperTrader) blocksHarvestReentry(symbol string, now time.Time, c candidate) (bool, string) {
	if p == nil || p.harvestLock <= 0 || p.lastHarvestAt == nil {
		return false, ""
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	last := p.lastHarvestAt[raw]
	if last.IsZero() || now.Sub(last) >= p.harvestLock {
		return false, ""
	}
	if strings.EqualFold(c.Strat, "mom_reversal") || strings.EqualFold(c.Strat, "mom_reversal_short") {
		return true, "recent_harvest_reentry_reversal"
	}
	if c.Entry.State != inplay.StateInPlay && c.Entry.State != inplay.StatePumping {
		return true, fmt.Sprintf("recent_harvest_reentry_state:%s", c.Entry.State)
	}
	if c.Entry.ScoreSlope < p.harvestMinSlope && !c.Entry.Momentum {
		return true, fmt.Sprintf("recent_harvest_reentry_slope:%.3f<%.3f", c.Entry.ScoreSlope, p.harvestMinSlope)
	}
	if p.harvestMaxStateMin > 0 && c.Entry.TimeInStateMin > p.harvestMaxStateMin {
		return true, fmt.Sprintf("recent_harvest_reentry_stale:%.1f>%.1f", c.Entry.TimeInStateMin, p.harvestMaxStateMin)
	}
	return false, ""
}

func (p *paperTrader) blocksSymbolTradeBudget(symbol string, now time.Time, c candidate) (bool, string) {
	if p == nil || p.maxTradesPerDay <= 0 {
		return false, ""
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	dayKey := currentSessionDayKey(now)
	if p.symbolTradeDay == nil {
		p.symbolTradeDay = map[string]string{}
	}
	if p.symbolTradeCount == nil {
		p.symbolTradeCount = map[string]int{}
	}
	if p.symbolTradeDay[raw] != dayKey {
		p.symbolTradeDay[raw] = dayKey
		p.symbolTradeCount[raw] = 0
	}
	if p.symbolTradeCount[raw] >= p.maxTradesPerDay {
		// Advisory-only: keep tracking budget saturation for logs/analysis, but do not hard-block entries.
		return false, fmt.Sprintf("symbol trade budget advisory (%d/day)", p.maxTradesPerDay)
	}
	_ = c
	return false, ""
}

func (p *paperTrader) setupLockKey(symbol string, c candidate) string {
	return strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol))) + "|" + strings.ToLower(strings.TrimSpace(firstNonEmpty(c.Strat, c.Entry.EntryStyle)))
}

func (p *paperTrader) blockSymbolForLosses(raw string, now time.Time) {
	if p == nil {
		return
	}
	if p.recentLosses == nil {
		p.recentLosses = map[string][]time.Time{}
	}
	times := append(trimTimesWithin(p.recentLosses[raw], now.Add(-4*time.Hour)), now)
	p.recentLosses[raw] = times
	twoLossCount := maxInt(2, envInt("LIVE_SYMBOL_TWO_LOSS_WINDOW_COUNT", 2))
	if len(times) >= twoLossCount {
		last := times[len(times)-1]
		prev := times[len(times)-2]
		if last.Sub(prev) <= time.Duration(envInt("LIVE_SYMBOL_TWO_LOSS_WINDOW_MIN", 60))*time.Minute {
			if p.lockUntil == nil {
				p.lockUntil = map[string]time.Time{}
			}
			p.lockUntil[raw] = now.Add(time.Duration(envInt("LIVE_SYMBOL_TWO_LOSS_LOCK_MIN", 120)) * time.Minute)
		}
	}
}

func (p *paperTrader) registerPaperLoss(now time.Time, pos *paperPosition) {
	if p == nil || pos == nil {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(pos.Symbol)))
	if raw == "" {
		return
	}
	dayKey := currentSessionDayKey(now)
	if p.lossDay == nil {
		p.lossDay = map[string]string{}
	}
	if p.lossDayCount == nil {
		p.lossDayCount = map[string]int{}
	}
	if p.lossDay[raw] != dayKey {
		p.lossDay[raw] = dayKey
		p.lossDayCount[raw] = 0
	}
	p.lossDayCount[raw]++
	p.blockSymbolForLosses(raw, now)
	if p.lossDayCount[raw] >= maxInt(1, envInt("LIVE_SYMBOL_DAY_LOSS_DISABLE_COUNT", 3)) {
		if p.lockUntil == nil {
			p.lockUntil = map[string]time.Time{}
		}
		endOfDay := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		p.lockUntil[raw] = endOfDay
	}
	setupKey := p.setupLockKey(raw, candidate{Strat: pos.EntryStrategyID, Entry: inplay.Entry{EntryStyle: pos.EntryStyle}})
	if strings.HasSuffix(setupKey, "|") {
		return
	}
	if p.setupLosses == nil {
		p.setupLosses = map[string][]time.Time{}
	}
	times := append(trimTimesWithin(p.setupLosses[setupKey], now.Add(-4*time.Hour)), now)
	p.setupLosses[setupKey] = times
	if len(times) >= maxInt(2, envInt("LIVE_SYMBOL_SETUP_LOSS_COUNT", 2)) {
		last := times[len(times)-1]
		prev := times[len(times)-2]
		if !prev.IsZero() {
			if p.setupLockUntil == nil {
				p.setupLockUntil = map[string]time.Time{}
			}
			p.setupLockUntil[setupKey] = last.Add(time.Duration(envInt("LIVE_SYMBOL_SETUP_LOSS_LOCK_MIN", 240)) * time.Minute)
		}
	}
}

func (p *paperTrader) symbolLossBlockReason(raw string, now time.Time, c candidate) string {
	if p == nil {
		return ""
	}
	if t := p.lockUntil[raw]; !t.IsZero() && now.Before(t) {
		dayKey := currentSessionDayKey(now)
		if p.lossDay != nil && p.lossDay[raw] == dayKey && p.lossDayCount[raw] >= maxInt(1, envInt("LIVE_SYMBOL_DAY_LOSS_DISABLE_COUNT", 3)) {
			return "symbol_day_loss_lock"
		}
		return "symbol_loss_cooldown"
	}
	setupKey := p.setupLockKey(raw, c)
	if t := p.setupLockUntil[setupKey]; !t.IsZero() && now.Before(t) {
		return "symbol_setup_loss_lock"
	}
	return ""
}

func (p *paperTrader) slotReplacementCandidate(now time.Time, c candidate, meta map[string]symbolMeta, current map[string]inplay.Entry) (*paperPosition, string) {
	if p == nil || !p.slotReplaceEnable || len(p.positions) < p.maxOpen {
		return nil, ""
	}
	anchorKeepMFER := envFloat("LIVE_PAPER_SLOT_REPLACE_KEEP_MFE_R", 1.50)
	if gradeValue(c.Entry.CurrentGrade) < gradeValue(p.slotReplaceMinGrade) {
		return nil, ""
	}
	if c.Conf < p.slotReplaceMinConf || c.Entry.ScoreSlope < p.slotReplaceMinSlope {
		return nil, ""
	}
	switch c.Entry.State {
	case inplay.StateHeating, inplay.StateInPlay, inplay.StatePumping:
	default:
		return nil, ""
	}
	var chosen *paperPosition
	chosenReason := ""
	chosenWeakness := -1.0
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		if strings.EqualFold(raw, strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))) {
			continue
		}
		if p.slotReplaceMinAge > 0 && now.Sub(pos.OpenedAt) < p.slotReplaceMinAge {
			continue
		}
		if pos.HitTP2 || pos.MaxFavorableR >= anchorKeepMFER {
			continue
		}
		cur, ok := current[raw]
		if !ok {
			cur = inplay.Entry{CurrentGrade: pos.EntryGrade, CurrentScore: c.Entry.CurrentScore, State: pos.EntryState}
		}
		sideMismatch := !strings.EqualFold(pos.Side, c.Side)
		if !sideMismatch {
			// Preserve aligned strong winners unless they have clearly cooled.
			switch cur.State {
			case inplay.StateHeating, inplay.StateInPlay, inplay.StatePumping:
				continue
			}
		}
		upnl := 0.0
		mark := meta[raw].LastPrice
		if mark > 0 {
			if strings.EqualFold(pos.Side, "BUY") {
				upnl = (mark - pos.Entry) * pos.Qty
			} else {
				upnl = (pos.Entry - mark) * pos.Qty
			}
		}
		if upnl > p.slotReplaceMaxUpnl {
			continue
		}
		scoreGap := c.Entry.CurrentScore - cur.CurrentScore
		if scoreGap < p.slotReplaceMinScoreGap && !sideMismatch {
			continue
		}
		stateWeak := 0.0
		switch cur.State {
		case inplay.StateBalanced:
			stateWeak = 1.0
		case inplay.StateCooling:
			stateWeak = 1.25
		case inplay.StateExhausted, inplay.StateDumping:
			stateWeak = 1.5
		default:
			if sideMismatch {
				stateWeak = 1.1
			}
		}
		if stateWeak == 0 {
			continue
		}
		fundingWeak := 0.0
		if fundingCostsPosition(pos.Side, meta[raw].FundingRate) {
			fundingWeak = 0.35
		}
		lossWeak := 0.0
		if p.lossStreak != nil {
			lossWeak = min(0.60, float64(p.lossStreak[raw])*0.20)
		}
		upnlWeak := 0.0
		if upnl <= 0 {
			upnlWeak = 0.25
		}
		weakness := stateWeak + maxFloat(0, scoreGap)/10.0 + maxFloat(0, c.Conf-pos.EntryConf)*0.5 + maxFloat(0, now.Sub(pos.OpenedAt).Minutes()-p.slotReplaceMinAge.Minutes())/240.0 + fundingWeak + lossWeak + upnlWeak
		if chosen == nil || weakness > chosenWeakness {
			chosen = pos
			chosenWeakness = weakness
			chosenReason = fmt.Sprintf("slot_replace:%s:%s:score_gap=%.2f:loss=%0.1f:funding=%0.2f", raw, cur.State, scoreGap, lossWeak, fundingWeak)
		}
	}
	return chosen, chosenReason
}

func (p *paperTrader) MaybeEnter(now time.Time, c candidate, entryBps, margin float64, leverage int, meta map[string]symbolMeta, depth map[string]aster.OrderBook, current map[string]inplay.Entry) (*paperPosition, error) {
	if p == nil || !p.enabled {
		return nil, nil
	}
	if !isExecutableStrategy(c.Strat) {
		return nil, fmt.Errorf("strategy_unresolved")
	}
	shortCtx := shortPhase2ContextForCandidate(c)
	if strings.EqualFold(c.Side, "SELL") {
		if !shortCtx.DirectShortAllowed {
			return nil, fmt.Errorf("%s", firstNonEmpty(shortCtx.FilterReason, "short_blocked"))
		}
		if shortCtx.SizeMultiplier > 0 && shortCtx.SizeMultiplier < 1.0 {
			margin *= shortCtx.SizeMultiplier
		}
	}
	raw := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	if len(p.positions) >= p.maxOpen {
		if replacePos, reason := p.slotReplacementCandidate(now, c, meta, current); replacePos != nil {
			p.exitPortion(now, replacePos, "SLOT_REPLACE", meta[strings.ToUpper(aster.RawSymbol(replacePos.Symbol))].LastPrice, replacePos.Qty, meta[strings.ToUpper(aster.RawSymbol(replacePos.Symbol))], depth[strings.ToUpper(aster.RawSymbol(replacePos.Symbol))])
			_ = p.save()
			fmt.Printf("paper slot replace: closed %s %s reason=%s\n", replacePos.Symbol, replacePos.Side, reason)
		} else {
			fmt.Printf("paper enter advisory: max paper positions reached (%d), continuing\n", p.maxOpen)
		}
	}
	free := p.freeForEntries()
	if free < margin {
		fmt.Printf("paper enter advisory: insufficient usable paper balance free=%.4f margin=%.4f, continuing\n", free, margin)
	}
	if _, exists := p.positions[raw]; exists {
		fmt.Printf("paper enter advisory: symbol already open %s, continuing\n", raw)
	}
	if reason := p.symbolLossBlockReason(raw, now, c); reason != "" {
		return nil, fmt.Errorf("%s", reason)
	}
	if blocked, reason := p.blocksHarvestReentry(raw, now, c); blocked {
		fmt.Printf("paper enter advisory: %s, continuing\n", strings.TrimSpace(reason))
	}
	if blocked, reason := p.blocksSymbolTradeBudget(raw, now, c); blocked {
		fmt.Printf("paper enter advisory: %s, continuing\n", strings.TrimSpace(reason))
	} else if strings.TrimSpace(reason) != "" {
		fmt.Printf("paper enter advisory: %s\n", strings.TrimSpace(reason))
	}
	if p.lossCooldown > 0 {
		if t := p.lastExitAt[raw]; !t.IsZero() && p.lastExitLoss[raw] && now.Sub(t) < p.lossCooldown {
			fmt.Printf("paper enter advisory: symbol loss cooldown active %s until=%s, continuing\n", raw, t.Add(p.lossCooldown).Format(time.RFC3339))
		}
	}
	if strings.TrimSpace(c.Strat) == "" {
		c.Strat = "manual"
	}
	entryStrategyID := firstNonEmpty(strings.TrimSpace(c.StrategyID), "unknown")
	m := meta[raw]
	if m.LastPrice <= 0 {
		return nil, fmt.Errorf("no price for %s", raw)
	}
	if paperFundingEntryBlocked(now, raw, c.Side, m, p) {
		return nil, fmt.Errorf("paper funding hazard")
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
	if p.partialFillEnable {
		fillQty := applyPaperPartialFill(qty, strings.ToUpper(c.Side), ob, p.partialFillEnable, p.partialFillMinFrac)
		if fillQty > 0 && fillQty < qty {
			fillFrac := fillQty / qty
			qty = fillQty
			margin *= fillFrac
			notional = margin * float64(lev)
		}
	}
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
		if p.allowFundingInMargin && fundingCostsPosition(c.Side, m.FundingRate) {
			interval := p.fundingEvery
			if p.fundingBySym != nil {
				if d, ok := p.fundingBySym[raw]; ok && d > 0 {
					interval = d
				}
			}
			required += estimatedFundingReserve(notional, m.FundingRate, interval)
		}
		if free < required {
			fmt.Printf("paper enter advisory: open cost %.4f exceeds free %.4f, continuing\n", required, free)
		}
	} else if free < margin+entryFee {
		fmt.Printf("paper enter advisory: paper margin+fee exceeds free balance, continuing\n")
	}
	stopPct := p.stopPct / 100.0
	if p.riskOnMargin {
		if riskPct := marginRiskStopPct(margin, lev, p.riskMarginPct); riskPct > 0 {
			stopPct = riskPct
		}
	}
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
		c.Entry.CurrentGrade,
		c.Entry.State,
		c.Conf,
		m.VolumeUSD,
		stopPct,
		tp1R,
		tp2R,
		tp3R,
		p.minStopPct/100.0,
		p.maxStopPct/100.0,
	)
	if c.Sig.Stop <= 0 {
		stopPct = clamp(widenStopPctForVolatility(stopPct, c.ATRPct, m.VolumeUSD), p.minStopPct/100.0, p.maxStopPct/100.0)
	}
	stopReason := ""
	stopDistancePct := stopPct * 100.0
	if p.hybridStopCfg.Enabled {
		stopRes := exitmgr.ComputeHybridStop(p.hybridStopCfg, hybridStopInputForCandidate(c, entry, c.Sig.TP1))
		if stopRes.Rejected {
			return nil, fmt.Errorf("%s", stopRes.RejectReason)
		}
		if stopRes.StopPrice > 0 {
			stopPct = clamp(stopRes.StopDistancePct/100.0, p.minStopPct/100.0, p.maxStopPct/100.0)
			stopReason = stopRes.StopReason
			stopDistancePct = stopRes.StopDistancePct
		}
	}
	if envBool("LIVE_REJECT_FRAGILE_STOPS", true) {
		stopReasonL := strings.ToLower(strings.TrimSpace(stopReason))
		if strings.Contains(stopReasonL, "rr_low") || strings.Contains(stopReasonL, "max_width") {
			return nil, fmt.Errorf("fragile stop rejected: %s", firstNonEmpty(stopReason, "unknown"))
		}
	}
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
	stop, tp1, tp2, tp3 = sanitizeBracketGeometry(entry, c.Side, stop, tp1, tp2, tp3)
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
		Symbol:                 raw,
		Side:                   strings.ToUpper(c.Side),
		TradeID:                newPaperTradeID(now, raw, c.Side),
		Entry:                  entry,
		Qty:                    qty,
		InitialQty:             qty,
		Margin:                 margin,
		Leverage:               lev,
		Stop:                   stop,
		TP1:                    tp1,
		TP2:                    tp2,
		TP3:                    tp3,
		TrailRef:               entry,
		OpenedAt:               now,
		EntryReason:            c.Strat,
		RawEntryReason:         c.Strat,
		EntryStrategyID:        entryStrategyID,
		EntryStyle:             c.Entry.EntryStyle,
		EntryStrategyFamily:    strategyFamily(c),
		EntryGrade:             c.Entry.CurrentGrade,
		EntryState:             c.Entry.State,
		EntryTrigger:           c.TriggerState,
		ExitProfile:            c.ExitProfile,
		EntryConf:              c.Conf,
		EntryConfluenceScore:   firstPositive(c.Sig.ConfluenceScore.TotalScore, c.CombinedScore),
		EntrySignalReasons:     append([]string(nil), c.Sig.Reasons...),
		EntrySignalSources:     append([]string(nil), c.Sig.SignalSource...),
		DiscoveryScore:         c.DiscoveryScore,
		TriggerScore:           c.TriggerScore,
		ExecutionScore:         c.ExecutionScore,
		CombinedScore:          c.CombinedScore,
		EntryVolumeUSD:         c.VolumeUSD,
		EntryATRPct:            c.ATRPct,
		EntryATRExtension:      c.ExtensionATR,
		EntrySession:           c.SessionLabel,
		EntryTiming:            c.EntryTiming,
		CandidateAgeSeconds:    c.CandidateAgeSeconds,
		EntryDistanceToVWAPPct: c.DistanceToVWAPPct,
		EntryPct24h:            shortCtx.Pct24hAtEntry,
		EntryPct4h:             shortCtx.Pct4hAtEntry,
		EntryPct1h:             shortCtx.Pct1hAtEntry,
		ShortBucket:            shortCtx.Bucket,
		ShortFilterReason:      shortCtx.FilterReason,
		ShortRequireConfirm:    shortCtx.RequireConfirmation,
		DirectShortAllowed:     shortCtx.DirectShortAllowed,
		BounceFromLocalLowPct:  shortCtx.BounceFromLocalLowPct,
		FailedBounceConfirmed:  shortCtx.FailedBounceConfirmed,
		PostPumpBreakdown:      shortCtx.PostPumpBreakdown,
		LateChaseBlocked:       shortCtx.LateChaseBlocked,
		OpposingFriction:       c.Sig.VPTargetLevel,
		StopReason:             stopReason,
		StopDistancePct:        stopDistancePct,
		EntrySetupFamily:       c.SetupFamily,
		EntrySetupSource:       c.SetupSource,
		EntryTradeHorizon:      c.TradeHorizon,
		ExecBucket:             executionGovernorBucketForCandidate(c),
		OriginalStop:           stop,
		OriginalTP1:            tp1,
		OriginalTP2:            tp2,
		OriginalTP3:            tp3,
	}
	p.positions[raw] = pos
	_ = p.save()
	fmt.Printf("paper entered %s %s entry=%.6f qty=%.6f lev=%dx tp1=%.6f tp2=%.6f tp3=%.6f sl=%.6f fee=%.4f setup=%s setup_source=%s trade_horizon=%s strategy=%s session=%s entry_timing=%s candidate_age_seconds=%.0f distance_to_vwap=%.4f atr_extension=%.3f reason=%s stop_reason=%s\n",
		raw, c.Side, entry, qty, lev, tp1, tp2, tp3, stop, entryFee,
		firstNonEmpty(strings.TrimSpace(c.SetupFamily), "none"),
		firstNonEmpty(strings.TrimSpace(c.SetupSource), "unknown"),
		firstNonEmpty(strings.TrimSpace(c.TradeHorizon), "unknown"),
		firstNonEmpty(strings.TrimSpace(c.Strat), "manual"),
		firstNonEmpty(strings.TrimSpace(c.SessionLabel), "unknown"),
		firstNonEmpty(strings.TrimSpace(c.EntryTiming), "unknown"),
		c.CandidateAgeSeconds,
		c.DistanceToVWAPPct,
		c.ExtensionATR,
		firstNonEmpty(strings.TrimSpace(c.Strat), "manual"),
		firstNonEmpty(stopReason, "generic"))
	return pos, nil
}

func (p *paperTrader) CheckExit(now time.Time, meta map[string]symbolMeta, depth map[string]aster.OrderBook, longCurrent, shortCurrent map[string]inplay.Entry, mom map[string]momentumView, flow map[string]flowMetrics) {
	if p == nil || !p.enabled || len(p.positions) == 0 {
		if p != nil {
			p.updatePostExitTrackers(now, meta)
		}
		return
	}
	p.updatePostExitTrackers(now, meta)
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
		if m.LastPrice <= 0 && (m.Bid <= 0 || m.Ask <= 0) {
			continue
		}
		sideBuy := strings.EqualFold(pos.Side, "BUY")
		markPx, lastPx := paperMarkLastPrices(m, depth[raw], p.markLastModel, p.markLastDivBps)
		mark := markPx
		if pos.LastMark > 0 && abs(mark-pos.LastMark)/maxFloat(pos.Entry, 1e-9) < 0.0006 {
			pos.StallBars++
		} else {
			pos.StallBars = 0
		}
		pos.LastMark = mark
		updateFavorableRPaper(pos, mark)
		_, upctMark := realizedFromFill(pos.Side, pos.Entry, mark, maxFloat(pos.Qty, 1))
		if newStop, tightened := applyLiveProtectionState(now, pos.Side, pos.Entry, pos.Stop, pos.MaxFavorableR, &pos.ProtectionStage, &pos.FirstProtectAt, &pos.ProtectedStop, p.beLockBps, allowMoveToBreakEven(pos.HitTP1, upctMark)); tightened {
			pos.Stop = newStop
		}
		if paperFastShortProtectionEnabled(pos) && pos.MaxFavorableR >= 0.75 {
			if newStop, tightened := applyLiveProtectionState(now, pos.Side, pos.Entry, pos.Stop, maxFloat(pos.MaxFavorableR, 1.0), &pos.ProtectionStage, &pos.FirstProtectAt, &pos.ProtectedStop, p.beLockBps, true); tightened {
				pos.StopReason = "phase2_short_fast_protect"
				pos.Stop = newStop
			}
			if pos.MaxFavorableR >= 1.0 {
				if correctedStop, corrected := enforceWinnerBEFloor(pos.Side, pos.Entry, pos.Stop, pos.MaxFavorableR); corrected {
					pos.StopReason = "phase2_short_forced_be"
					pos.Stop = correctedStop
				}
			}
		}
		if pos.MaxFavorableR >= winnerProofR() && stopStillOriginal(pos.Stop, pos.OriginalStop) {
			fmt.Printf("winner_unprotected_warning symbol=%s strategy_id=%s max_r=%.4f stop=%.8f original_stop=%.8f\n",
				raw, firstNonEmpty(strings.TrimSpace(pos.EntryStrategyID), strings.TrimSpace(pos.EntryReason), "unknown"), pos.MaxFavorableR, pos.Stop, pos.OriginalStop)
		}
		updateGivebackMetrics(pos.MaxFavorableR, unrealizedRiskR(pos.Side, pos.Entry, pos.Stop, mark), &pos.CaptureRatio, &pos.MaxGivebackR)
		prevSponsored := pos.Sponsored
		prevSponsorScore := pos.SponsorshipScore
		sponsorSnap := classifySponsorship(pos.Side, raw, mom, flow)
		updatePaperSponsorship(pos, sponsorSnap)
		if maybeRefreshPaperConfluence(now, pos, sponsorSnap, prevSponsored, prevSponsorScore) {
			fmt.Printf("paper: confluence refresh %s %s score=%.2f slope=%.3f state=%s count=%d\n",
				raw, pos.Side, sponsorSnap.Score, sponsorSnap.Slope, sponsorSnap.State, pos.ConfluenceRefreshCount)
		}
		stopCheckPx := triggerPriceForRef(p.stopTriggerRef, markPx, lastPx)
		tpCheckPx := triggerPriceForRef(p.tpTriggerRef, markPx, lastPx)
		_, upctStop := realizedFromFill(pos.Side, pos.Entry, stopCheckPx, maxFloat(pos.Qty, 1))
		allowBE := allowMoveToBreakEven(pos.HitTP1, upctStop)
		tp1R := tp1RFromBracket(pos.Entry, pos.Stop, pos.TP1)
		beArmR := beArmThreshold(envFloat("LIVE_PAPER_BE_ARM_R", 1.10), tp1R)
		if p.beLockBps > 0 && beArmR > 0 && pos.MaxFavorableR >= beArmR && allowBE {
			be := beLockPriceBuffered(pos.Side, pos.Entry, pos.Stop, p.beLockBps)
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
			cur, _ := paperCurrentEntryForSide(pos.Side, raw, longCurrent, shortCurrent)
			htf := p.htfSnapshot(raw, pos.Side, &cur)
			prevLifecycle := pos.WinnerLifecycle
			syncPaperWinnerLifecycle(pos, htfPersistent(pos.Side, htf))
			logWinnerLifecycleTransition(raw, pos.Side, prevLifecycle, pos.WinnerLifecycle, lifecycleTransitionReason(prevLifecycle, pos.WinnerLifecycle, "", pos.TrailOn))
			dec := p.exitManager.EvaluateProtect(exitmgr.ProtectInput{
				Side:               pos.Side,
				Entry:              pos.Entry,
				Stop:               pos.Stop,
				Mark:               stopCheckPx,
				MFER:               pos.MaxFavorableR,
				MAER:               pos.MaxAdverseR,
				BarsHeld:           int(now.Sub(pos.OpenedAt) / time.Minute),
				StallBars:          pos.StallBars,
				NearFriction:       p.hitPrice(sideBuy, tpCheckPx, pos.OpposingFriction),
				UnrealizedPct:      upctStop,
				Sponsored:          pos.Sponsored,
				HitTP1:             pos.HitTP1,
				HitTP2:             pos.HitTP2,
				HitTP3:             pos.HitTP3,
				WeakSponsorStreak:  pos.WeakSponsorStreak,
				EntryReason:        pos.EntryReason,
				EntryStrategyID:    pos.EntryStrategyID,
				StarterEntry:       false,
				AdvancedReady:      paperAdvancedReady(pos),
				HTFTrendState:      string(htf.State),
				HTFTrendPersistent: htfPersistent(pos.Side, htf),
				HTFTrendFailed:     htfFailed(pos.Side, htf),
				HTFCaution:         htfCaution(pos.Side, htf),
				TriggerRef:         p.stopTriggerRef,
				ComputedStop:       pos.Stop,
				SubmittedStop:      pos.Stop,
				AcceptedStop:       pos.Stop,
				LegalityAdjusted:   false,
				WinnerLifecycle:    pos.WinnerLifecycle,
				TrailingActive:     pos.TrailOn,
				MatureTrend:        matureTrendForWinnerLifecycle(pos.MaxFavorableR, htfPersistent(pos.Side, htf), pos.HitTP1, pos.HitTP2, pos.HitTP3),
				RealInvalidation:   htfFailed(pos.Side, htf),
			})
			logWinnerLifecycleTransition(raw, pos.Side, pos.WinnerLifecycle, dec.WinnerLifecycle, lifecycleTransitionReason(pos.WinnerLifecycle, dec.WinnerLifecycle, firstNonEmpty(dec.ExitNowReason, dec.Reason), pos.TrailOn))
			pos.WinnerLifecycle = dec.WinnerLifecycle
			if dec.MoveStopToBE && allowBE {
				be := beLockPriceBuffered(pos.Side, pos.Entry, pos.Stop, p.beLockBps)
				if (sideBuy && be > pos.Stop) || (!sideBuy && be < pos.Stop) {
					pos.Stop = be
					logPaperProtectDecision(raw, pos, "BE", dec, pos.Stop, pos.Stop, false)
				}
			}
			if dec.TightenStop {
				if (sideBuy && dec.TightenToPrice > pos.Stop) || (!sideBuy && dec.TightenToPrice < pos.Stop) {
					pos.Stop = dec.TightenToPrice
					logPaperProtectDecision(raw, pos, "TIGHTEN", dec, pos.Stop, pos.Stop, false)
				}
			}
			if correctedStop, corrected := enforceWinnerBEFloor(pos.Side, pos.Entry, pos.Stop, pos.MaxFavorableR); corrected {
				pos.StopReason = "forced_be_correction"
				pos.Stop = correctedStop
				logPaperProtectDecision(raw, pos, "TIGHTEN", exitmgr.ProtectDecision{
					CurrentWinnerLifecycle: pos.WinnerLifecycle,
					WinnerLifecycle:        pos.WinnerLifecycle,
					Reason:                 "forced_be_correction",
					TriggerRef:             p.stopTriggerRef,
					HTFTrendState:          string(htf.State),
					HTFPersistent:          htfPersistent(pos.Side, htf),
					HTFFailed:              htfFailed(pos.Side, htf),
					HTFCaution:             htfCaution(pos.Side, htf),
				}, pos.Stop, pos.Stop, false)
			}
			if dec.ImmediateExit {
				logPaperProtectDecision(raw, pos, "IMMEDIATE_EXIT", dec, dec.SubmittedStop, dec.AcceptedStop, dec.LegalityAdjusted)
				p.exitPortion(now, pos, firstNonEmpty(dec.ExitNowReason, dec.Reason, "IMMEDIATE_EXIT"), stopCheckPx, pos.Qty, meta[raw], depth[raw])
				continue
			}
			if dec.FullExit {
				if !lifecycleSoftExitsCanHardClose(pos.WinnerLifecycle) {
					continue
				}
				logPaperProtectDecision(raw, pos, "FULL_EXIT", dec, dec.SubmittedStop, dec.AcceptedStop, dec.LegalityAdjusted)
				p.exitPortion(now, pos, dec.Reason, stopCheckPx, pos.Qty, meta[raw], depth[raw])
				continue
			}
		}

		// 1) Scale-out targets first so TP wins when TP/SL are both touched in one cycle.
		if !pos.HitTP1 && p.hitPrice(sideBuy, tpCheckPx, frTP1) {
			pos.HitTP1 = true
			if p.tpRatchetOnly {
				if stop, ok := ratchetStopTarget(pos.Side, pos.Entry, pos.Stop, pos.TP1, pos.TP2, p.beLockBps, 1); ok {
					pos.Stop = stop
				}
			} else {
				q := p.targetQty(pos.InitialQty, p.tp1Frac, pos.Qty)
				p.exitPortion(now, pos, "TP1", frTP1, q, meta[raw], depth[raw])
				pos = p.positions[raw]
				if pos == nil {
					continue
				}
				pos.HitTP1 = true
				if p.beLockBps > 0 {
					pos.Stop = beLockPriceBuffered(pos.Side, pos.Entry, pos.Stop, p.beLockBps)
				}
			}
		}
		if pos == nil {
			continue
		}
		if !pos.HitTP2 && p.hitPrice(sideBuy, tpCheckPx, frTP2) {
			pos.HitTP2 = true
			if p.tpRatchetOnly {
				if stop, ok := ratchetStopTarget(pos.Side, pos.Entry, pos.Stop, pos.TP1, pos.TP2, p.beLockBps, 2); ok {
					pos.Stop = stop
				}
			} else {
				q := p.targetQty(pos.InitialQty, p.tp2Frac, pos.Qty)
				p.exitPortion(now, pos, "TP2", frTP2, q, meta[raw], depth[raw])
				pos = p.positions[raw]
				if pos == nil {
					continue
				}
				pos.HitTP2 = true
			}
			if p.trailAfterTP <= 2 {
				pos.TrailOn = true
				pos.TrailRef = stopCheckPx
				pos.TrailStop = p.calcTrailStopForPosition(pos, sideBuy, stopCheckPx, false)
			}
		}
		if pos == nil {
			continue
		}
		if !pos.HitTP3 && p.hitPrice(sideBuy, tpCheckPx, frTP3) {
			pos.HitTP3 = true
			if p.tpRatchetOnly {
				if stop, ok := ratchetStopTarget(pos.Side, pos.Entry, pos.Stop, pos.TP1, pos.TP2, p.beLockBps, 3); ok {
					pos.Stop = stop
				}
			} else {
				q := p.targetQty(pos.InitialQty, p.tp3Frac, pos.Qty)
				p.exitPortion(now, pos, "TP3", frTP3, q, meta[raw], depth[raw])
				pos = p.positions[raw]
				if pos == nil {
					continue
				}
				pos.HitTP3 = true
			}
			if p.trailAfterTP <= 3 {
				pos.TrailOn = true
				pos.TrailRef = stopCheckPx
				pos.TrailStop = p.calcTrailStopForPosition(pos, sideBuy, stopCheckPx, true)
			}
		}
		if pos == nil {
			continue
		}

		// 2) Hard stop after TP checks.
		if (sideBuy && stopCheckPx <= pos.Stop) || (!sideBuy && stopCheckPx >= pos.Stop) {
			if protectAfterProofEnabled() && pos.MaxFavorableR >= winnerProofR() && stopStillOriginal(pos.Stop, pos.OriginalStop) {
				be := beLockPriceBuffered(pos.Side, pos.Entry, pos.Stop, maxFloat(p.beLockBps, 1.0))
				if (sideBuy && be > pos.Stop) || (!sideBuy && be < pos.Stop) {
					pos.Stop = be
					fmt.Printf("winner_reverted_unprotected symbol=%s strategy_id=%s max_r=%.4f old_stop=%.8f new_stop=%.8f\n",
						raw, firstNonEmpty(strings.TrimSpace(pos.EntryStrategyID), strings.TrimSpace(pos.EntryReason), "unknown"), pos.MaxFavorableR, pos.OriginalStop, pos.Stop)
					continue
				}
			}
			p.exitPortion(now, pos, "SL", stopCheckPx, pos.Qty, meta[raw], depth[raw])
			continue
		}

		// 3) Trail remaining position once activated.
		if pos.TrailOn {
			if (sideBuy && stopCheckPx > pos.TrailRef) || (!sideBuy && stopCheckPx < pos.TrailRef) {
				pos.TrailRef = stopCheckPx
				pos.TrailStop = p.calcTrailStopForPosition(pos, sideBuy, stopCheckPx, pos.HitTP3)
			}
			if (sideBuy && stopCheckPx <= pos.TrailStop) || (!sideBuy && stopCheckPx >= pos.TrailStop) {
				p.exitPortion(now, pos, "TRAIL_STOP", stopCheckPx, pos.Qty, meta[raw], depth[raw])
				continue
			}
		}

		if reason := paperDegradedHoldExitReason(now, pos, stopCheckPx, longCurrent, shortCurrent); reason != "" {
			if p.exitManager != nil {
				upnlPct := 0.0
				if pos.Qty > 0 && stopCheckPx > 0 {
					_, upnlPct = realizedFromFill(pos.Side, pos.Entry, stopCheckPx, pos.Qty)
				}
				weakFlow := strings.EqualFold(reason, "MOMENTUM_FADE")
				if !weakFlow {
					if cur, ok := paperCurrentEntryForSide(pos.Side, pos.Symbol, longCurrent, shortCurrent); ok {
						slopeMax := envFloat("LIVE_PAPER_DEGRADED_EXIT_SLOPE_MAX", 0.05)
						weakFlow = paperTrendLikeState(cur.State) && cur.ScoreSlope <= slopeMax && !cur.Momentum
					}
				}
				cur, _ := paperCurrentEntryForSide(pos.Side, pos.Symbol, longCurrent, shortCurrent)
				htf := p.htfSnapshot(raw, pos.Side, &cur)
				syncPaperWinnerLifecycle(pos, htfPersistent(pos.Side, htf))
				dec := p.exitManager.EvaluateProtect(exitmgr.ProtectInput{
					Side:               pos.Side,
					Entry:              pos.Entry,
					Stop:               pos.Stop,
					Mark:               stopCheckPx,
					MFER:               pos.MaxFavorableR,
					MAER:               pos.MaxAdverseR,
					BarsHeld:           int(now.Sub(pos.OpenedAt) / time.Minute),
					StallBars:          pos.StallBars,
					WeakFlow:           weakFlow,
					NearFriction:       false,
					LiqSpike:           false,
					UnrealizedPct:      upnlPct,
					Sponsored:          pos.Sponsored,
					HitTP1:             pos.HitTP1,
					HitTP2:             pos.HitTP2,
					HitTP3:             pos.HitTP3,
					WeakSponsorStreak:  pos.WeakSponsorStreak,
					EntryReason:        pos.EntryReason,
					EntryStrategyID:    pos.EntryStrategyID,
					StarterEntry:       false,
					AdvancedReady:      paperAdvancedReady(pos),
					HTFTrendState:      string(htf.State),
					HTFTrendPersistent: htfPersistent(pos.Side, htf),
					HTFTrendFailed:     htfFailed(pos.Side, htf),
					HTFCaution:         htfCaution(pos.Side, htf),
					TriggerRef:         p.stopTriggerRef,
					ComputedStop:       pos.Stop,
					SubmittedStop:      pos.Stop,
					AcceptedStop:       pos.Stop,
					LegalityAdjusted:   false,
					WinnerLifecycle:    pos.WinnerLifecycle,
					TrailingActive:     pos.TrailOn,
					MatureTrend:        matureTrendForWinnerLifecycle(pos.MaxFavorableR, htfPersistent(pos.Side, htf), pos.HitTP1, pos.HitTP2, pos.HitTP3),
					RealInvalidation:   htfFailed(pos.Side, htf),
				})
				pos.WinnerLifecycle = dec.WinnerLifecycle
				if p.applyPaperProtectDecision(now, raw, pos, stopCheckPx, dec, meta, depth) {
					continue
				}
			}
			cur, _ := paperCurrentEntryForSide(pos.Side, pos.Symbol, longCurrent, shortCurrent)
			htf := p.htfSnapshot(raw, pos.Side, &cur)
			if envBool("LIVE_EXIT_SOFT_SIGNALS_MANAGE_ONLY", true) &&
				htfPersistent(pos.Side, htf) && !htfFailed(pos.Side, htf) {
				continue
			}
			if !lifecycleSoftExitsCanHardClose(pos.WinnerLifecycle) {
				continue
			}
			p.exitPortion(now, pos, reason, stopCheckPx, pos.Qty, meta[raw], depth[raw])
			continue
		}

		if pos.Qty <= 1e-10 {
			delete(p.positions, raw)
		}
	}
}

func (p *paperTrader) ApplyMomentumExit(now time.Time, mom map[string]momentumView, meta map[string]symbolMeta, depth map[string]aster.OrderBook, ext map[string]flowfeed.ExternalSignal) {
	if p == nil || !p.enabled || !envBool("LIVE_MOMENTUM_EXIT_ENABLE", false) || len(p.positions) == 0 {
		if p != nil {
			p.updatePostExitTrackers(now, meta)
		}
		return
	}
	p.updatePostExitTrackers(now, meta)
	slopeMax := envFloat("LIVE_MOMENTUM_EXIT_SLOPE_MAX", 0.0)
	minHold := time.Duration(envInt("LIVE_MOMENTUM_EXIT_MIN_HOLD_MIN", 35)) * time.Minute
	minUpnlPct := envFloat("LIVE_MOMENTUM_EXIT_MIN_UPNL_PCT", 0.25)
	minMFER := envFloat("LIVE_MOMENTUM_EXIT_MIN_MFE_R", 1.75)
	sponsorMinScore := envFloat("LIVE_EXIT_SPONSOR_MIN_SCORE", 70.0)
	sponsorMinSlope := envFloat("LIVE_EXIT_SPONSOR_MIN_SLOPE", 0.02)
	sponsorFadeHoldMin := time.Duration(envInt("LIVE_EXIT_SPONSOR_FADE_HOLD_MIN", 120)) * time.Minute
	swingHoldEnable := envBool("LIVE_SWING_HOLD_GUARD_ENABLE", true)
	swingHoldLog := envBool("LIVE_SWING_HOLD_GUARD_LOG", true)
	changed := false
	for raw, pos := range p.positions {
		if pos == nil || pos.Qty <= 0 {
			continue
		}
		mv := mom[raw]
		if !shouldExitOnMomentumFade(pos.Side, mv, slopeMax) {
			continue
		}
		sponsored := isSponsoredMomentum(pos.Side, mv, sponsorMinScore, sponsorMinSlope)
		if sponsored && now.Sub(pos.OpenedAt) < sponsorFadeHoldMin {
			continue
		}
		if confluenceRefreshActive(now, pos.LastConfluenceRefresh) {
			continue
		}
		if swingHoldEnable {
			if holdScore, holdReason, hold := evaluateSwingHold(pos.Side, mv, sponsored, confluenceRefreshActive(now, pos.LastConfluenceRefresh), pos.MaxFavorableR, pos.HitTP1, pos.HitTP2, pos.HitTP3); hold {
				if swingHoldLog {
					fmt.Printf("paper: swing hold %s %s score=%.2f reason=%s state=%s slope=%.3f\n",
						raw, pos.Side, holdScore, holdReason, sameSideMomentumEntry(pos.Side, mv).State, sameSideMomentumEntry(pos.Side, mv).ScoreSlope)
				}
				continue
			}
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
		htf := p.htfSnapshot(raw, pos.Side, sameSideMomentumEntry(pos.Side, mv))
		prevLifecycle := pos.WinnerLifecycle
		syncPaperWinnerLifecycle(pos, htfPersistent(pos.Side, htf))
		logWinnerLifecycleTransition(raw, pos.Side, prevLifecycle, pos.WinnerLifecycle, lifecycleTransitionReason(prevLifecycle, pos.WinnerLifecycle, "", pos.TrailOn))
		if p.exitManager != nil {
			dec := p.exitManager.EvaluateProtect(exitmgr.ProtectInput{
				Side:               pos.Side,
				Entry:              pos.Entry,
				Stop:               pos.Stop,
				Mark:               mark,
				MFER:               pos.MaxFavorableR,
				MAER:               pos.MaxAdverseR,
				BarsHeld:           int(now.Sub(pos.OpenedAt) / time.Minute),
				StallBars:          pos.StallBars,
				WeakFlow:           shouldExitOnMomentumFade(pos.Side, mv, slopeMax),
				LiqSpike:           ext[raw].LiqSpike,
				UnrealizedPct:      upnlPct,
				Sponsored:          sponsored,
				HitTP1:             pos.HitTP1,
				HitTP2:             pos.HitTP2,
				HitTP3:             pos.HitTP3,
				WeakSponsorStreak:  pos.WeakSponsorStreak,
				EntryReason:        pos.EntryReason,
				EntryStrategyID:    pos.EntryStrategyID,
				StarterEntry:       false,
				AdvancedReady:      paperAdvancedReady(pos),
				HTFTrendState:      string(htf.State),
				HTFTrendPersistent: htfPersistent(pos.Side, htf),
				HTFTrendFailed:     htfFailed(pos.Side, htf),
				HTFCaution:         htfCaution(pos.Side, htf),
				TriggerRef:         p.stopTriggerRef,
				ComputedStop:       pos.Stop,
				SubmittedStop:      pos.Stop,
				AcceptedStop:       pos.Stop,
				LegalityAdjusted:   false,
				WinnerLifecycle:    pos.WinnerLifecycle,
				TrailingActive:     pos.TrailOn,
				MatureTrend:        matureTrendForWinnerLifecycle(pos.MaxFavorableR, htfPersistent(pos.Side, htf), pos.HitTP1, pos.HitTP2, pos.HitTP3),
				RealInvalidation:   htfFailed(pos.Side, htf),
			})
			logWinnerLifecycleTransition(raw, pos.Side, pos.WinnerLifecycle, dec.WinnerLifecycle, lifecycleTransitionReason(pos.WinnerLifecycle, dec.WinnerLifecycle, firstNonEmpty(dec.ExitNowReason, dec.Reason), pos.TrailOn))
			pos.WinnerLifecycle = dec.WinnerLifecycle
			if dec.MoveStopToBE && allowMoveToBreakEven(pos.HitTP1, upnlPct) {
				be := beLockPriceBuffered(pos.Side, pos.Entry, pos.Stop, p.beLockBps)
				if (strings.EqualFold(pos.Side, "BUY") && be > pos.Stop) || (!strings.EqualFold(pos.Side, "BUY") && be < pos.Stop) {
					pos.Stop = be
					logPaperProtectDecision(raw, pos, "BE", dec, pos.Stop, pos.Stop, false)
				}
			}
			if dec.PartialExitPct > 0 && pos.Qty > 0 {
				q := pos.Qty * dec.PartialExitPct
				if q > 0 && q < pos.Qty {
					logPaperProtectDecision(raw, pos, "PARTIAL", dec, pos.Stop, pos.Stop, false)
					p.exitPortion(now, pos, "SOFT_LIQ_SPIKE_PARTIAL", mark, q, m, depth[raw])
					changed = true
					continue
				}
			}
			if dec.TightenStop {
				if (strings.EqualFold(pos.Side, "BUY") && dec.TightenToPrice > pos.Stop) || (!strings.EqualFold(pos.Side, "BUY") && dec.TightenToPrice < pos.Stop) {
					pos.StopReason = dec.Reason
					pos.Stop = dec.TightenToPrice
					logPaperProtectDecision(raw, pos, "TIGHTEN", dec, pos.Stop, pos.Stop, false)
					changed = true
				}
			}
			if correctedStop, corrected := enforceWinnerBEFloor(pos.Side, pos.Entry, pos.Stop, pos.MaxFavorableR); corrected {
				pos.StopReason = "forced_be_correction"
				pos.Stop = correctedStop
				logPaperProtectDecision(raw, pos, "TIGHTEN", exitmgr.ProtectDecision{
					CurrentWinnerLifecycle: pos.WinnerLifecycle,
					WinnerLifecycle:        pos.WinnerLifecycle,
					Reason:                 "forced_be_correction",
					TriggerRef:             p.stopTriggerRef,
					HTFTrendState:          string(htf.State),
					HTFPersistent:          htfPersistent(pos.Side, htf),
					HTFFailed:              htfFailed(pos.Side, htf),
					HTFCaution:             htfCaution(pos.Side, htf),
				}, pos.Stop, pos.Stop, false)
				changed = true
			}
			if dec.ImmediateExit {
				logPaperProtectDecision(raw, pos, "IMMEDIATE_EXIT", dec, dec.SubmittedStop, dec.AcceptedStop, dec.LegalityAdjusted)
				p.exitPortion(now, pos, firstNonEmpty(dec.ExitNowReason, dec.Reason, "IMMEDIATE_EXIT"), mark, pos.Qty, m, depth[raw])
				changed = true
				continue
			}
			if dec.FullExit {
				if !lifecycleSoftExitsCanHardClose(pos.WinnerLifecycle) {
					continue
				}
				logPaperProtectDecision(raw, pos, "FULL_EXIT", dec, dec.SubmittedStop, dec.AcceptedStop, dec.LegalityAdjusted)
				p.exitPortion(now, pos, dec.Reason, mark, pos.Qty, m, depth[raw])
				changed = true
				continue
			}
		}
		if envBool("LIVE_MOMENTUM_FADE_TIGHTEN_AFTER_CONFIRM", true) &&
			(pos.HitTP1 || pos.HitTP2 || pos.HitTP3 || pos.ProtectionStage >= protectionStageArmed ||
				pos.MaxFavorableR >= envFloat("LIVE_EARLY_CONTINUATION_MIN_R", 0.35)) {
			if !(envBool("LIVE_MOMENTUM_FADE_REQUIRE_STRUCTURE_LOSS_AFTER_CONFIRM", true) && (pos.Sponsored || confluenceRefreshActive(now, pos.LastConfluenceRefresh))) {
				if stop, tightened := applyLiveProtectionState(now, pos.Side, pos.Entry, pos.Stop, maxFloat(pos.MaxFavorableR, envFloat("LIVE_PROFIT_LOCK_STAGE1_R", 1.0)), &pos.ProtectionStage, &pos.FirstProtectAt, &pos.ProtectedStop, p.beLockBps, allowMoveToBreakEven(pos.HitTP1, upnlPct)); tightened {
					pos.StopReason = "MOMENTUM_FADE_TIGHTEN"
					pos.Stop = stop
					changed = true
				}
			}
			continue
		}
		if envBool("LIVE_EXIT_SOFT_SIGNALS_MANAGE_ONLY", true) &&
			htfPersistent(pos.Side, htf) && !htfFailed(pos.Side, htf) {
			if stop, tightened := applyLiveProtectionState(now, pos.Side, pos.Entry, pos.Stop, maxFloat(pos.MaxFavorableR, envFloat("LIVE_PROFIT_LOCK_STAGE1_R", 1.0)), &pos.ProtectionStage, &pos.FirstProtectAt, &pos.ProtectedStop, p.beLockBps, allowMoveToBreakEven(pos.HitTP1, upnlPct)); tightened {
				pos.StopReason = "MOMENTUM_FADE_TIGHTEN_HTF"
				pos.Stop = stop
				changed = true
			}
			continue
		}
		if !lifecycleSoftExitsCanHardClose(pos.WinnerLifecycle) {
			continue
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
		if p != nil {
			p.updatePostExitTrackers(now, meta)
		}
		return
	}
	p.updatePostExitTrackers(now, meta)
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

func (p *paperTrader) ForceCloseNonWinners(now time.Time, meta map[string]symbolMeta, depth map[string]aster.OrderBook, reason string) (int, int) {
	if p == nil || !p.enabled {
		return 0, 0
	}
	closed := 0
	keptWinners := 0
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		mark := meta[raw].LastPrice
		if mark <= 0 {
			mark = pos.LastMark
		}
		if mark <= 0 {
			mark = pos.Entry
		}
		pnl, _ := realizedFromFill(pos.Side, pos.Entry, mark, pos.Qty)
		if pnl > 0 {
			keptWinners++
			continue
		}
		p.exitPortion(now, pos, reason, mark, pos.Qty, meta[raw], depth[raw])
		closed++
	}
	_ = p.save()
	return closed, keptWinners
}

func newPaperTradeID(now time.Time, symbol, side string) string {
	return fmt.Sprintf("paper-%s-%s-%s", now.UTC().Format("20060102T150405.000000000Z"), strings.ToUpper(strings.TrimSpace(symbol)), strings.ToUpper(strings.TrimSpace(side)))
}

func normalizeClosedTradeStrategy(raw string) (string, bool) {
	if !isExecutableStrategy(raw) {
		return "unknown", true
	}
	return strings.ToLower(strings.TrimSpace(raw)), false
}

func normalizedLedgerExitReason(raw string, net float64, pos *paperPosition) string {
	r := strings.ToUpper(strings.TrimSpace(raw))
	switch r {
	case "TRAIL_STOP":
		return "Trailing Stop"
	case "TP1", "TP2", "TP3":
		return r
	case "FUNDING":
		return "Funding Exit"
	case "SL":
		if net > 0 {
			if pos != nil && pos.TrailOn {
				return "Trailing Stop"
			}
			return "Protected Stop"
		}
		return "Stop Loss"
	default:
		if r == "" {
			return "Unknown Exit"
		}
		return strings.ReplaceAll(strings.Title(strings.ToLower(r)), "_", " ")
	}
}

func plannedRiskPrice(entry, stop float64) float64 {
	if entry <= 0 || stop <= 0 {
		return 0
	}
	return math.Abs(entry - stop)
}

func plannedRiskPct(entry, stop float64) float64 {
	if entry <= 0 || stop <= 0 {
		return 0
	}
	return plannedRiskPrice(entry, stop) / entry * 100.0
}

func sideAwareR(side string, entry, stop, price float64, best bool) float64 {
	risk := plannedRiskPrice(entry, stop)
	if risk <= 0 || entry <= 0 || price <= 0 {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		return (price - entry) / risk
	}
	if best {
		return (entry - price) / risk
	}
	return (entry - price) / risk
}

func updatePaperPostExitWindow(side string, entry, stop, price float64, w *paperPostExitWindowState, ts time.Time) {
	if w == nil || price <= 0 {
		return
	}
	if w.PeakPrice <= 0 {
		w.PeakPrice = price
		w.PeakAt = ts
	}
	if w.TroughPrice <= 0 {
		w.TroughPrice = price
		w.TroughAt = ts
	}
	if price > w.PeakPrice {
		w.PeakPrice = price
		w.PeakAt = ts
	}
	if price < w.TroughPrice {
		w.TroughPrice = price
		w.TroughAt = ts
	}
}

func buildPaperPostExitSection(side string, entry, stop, exitPx, tp1, tp2, tp3 float64, windows map[string]paperPostExitWindowState) paperClosedTradePostExit {
	post := paperClosedTradePostExit{}
	if w, ok := windows["15m"]; ok {
		post.PeakPrice15m = w.PeakPrice
		post.TroughPrice15m = w.TroughPrice
		post.BestR15m = sideAwareBestR(side, entry, stop, w)
		post.WorstR15m = sideAwareWorstR(side, entry, stop, w)
	}
	if w, ok := windows["30m"]; ok {
		post.PeakPrice30m = w.PeakPrice
		post.TroughPrice30m = w.TroughPrice
		post.BestR30m = sideAwareBestR(side, entry, stop, w)
		post.WorstR30m = sideAwareWorstR(side, entry, stop, w)
	}
	if w, ok := windows["60m"]; ok {
		post.PeakPrice60m = w.PeakPrice
		post.TroughPrice60m = w.TroughPrice
		post.BestR60m = sideAwareBestR(side, entry, stop, w)
		post.WorstR60m = sideAwareWorstR(side, entry, stop, w)
		post.MissedTP1 = sideAwareMissedTarget(side, exitPx, tp1, w)
		post.MissedTP2 = sideAwareMissedTarget(side, exitPx, tp2, w)
		post.MissedTP3 = sideAwareMissedTarget(side, exitPx, tp3, w)
	}
	post.ExitVsTP1 = sideAwareExitVsTarget(side, exitPx, tp1)
	post.ExitVsTP2 = sideAwareExitVsTarget(side, exitPx, tp2)
	post.ExitVsTP3 = sideAwareExitVsTarget(side, exitPx, tp3)
	return post
}

func sideAwareBestR(side string, entry, stop float64, w paperPostExitWindowState) float64 {
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		return sideAwareR(side, entry, stop, w.PeakPrice, true)
	}
	return sideAwareR(side, entry, stop, w.TroughPrice, true)
}

func sideAwareWorstR(side string, entry, stop float64, w paperPostExitWindowState) float64 {
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		return sideAwareR(side, entry, stop, w.TroughPrice, false)
	}
	return sideAwareR(side, entry, stop, w.PeakPrice, false)
}

func sideAwareMissedTarget(side string, exitPx, target float64, w paperPostExitWindowState) bool {
	if exitPx <= 0 || target <= 0 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		return exitPx < target && w.PeakPrice >= target
	}
	return exitPx > target && w.TroughPrice <= target
}

func sideAwareExitVsTarget(side string, exitPx, target float64) float64 {
	if exitPx <= 0 || target <= 0 {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(side), "BUY") {
		return exitPx - target
	}
	return target - exitPx
}

func (p *paperTrader) startPostExitObservation(rec paperClosedTradeRecord) {
	if p == nil {
		return
	}
	if p.postExitTrackers == nil {
		p.postExitTrackers = map[string]*paperPostExitTracker{}
	}
	initial := paperPostExitWindowState{
		PeakPrice:   rec.Exit.RealizedExitPrice,
		PeakAt:      rec.Exit.ExitTs,
		TroughPrice: rec.Exit.RealizedExitPrice,
		TroughAt:    rec.Exit.ExitTs,
	}
	p.postExitTrackers[rec.TradeID] = &paperPostExitTracker{
		TradeID:           rec.TradeID,
		Symbol:            rec.Symbol,
		Side:              rec.Side,
		EntryPrice:        rec.Entry.EntryPrice,
		OriginalStop:      rec.Plan.OriginalStop,
		OriginalTP1:       rec.Plan.OriginalTP1,
		OriginalTP2:       rec.Plan.OriginalTP2,
		OriginalTP3:       rec.Plan.OriginalTP3,
		RealizedExitPrice: rec.Exit.RealizedExitPrice,
		ExitTs:            rec.Exit.ExitTs,
		Windows: map[string]paperPostExitWindowState{
			"15m": initial,
			"30m": initial,
			"60m": initial,
		},
	}
}

func (p *paperTrader) updatePostExitTrackers(now time.Time, meta map[string]symbolMeta) {
	if p == nil || len(p.postExitTrackers) == 0 {
		return
	}
	changed := false
	for tradeID, tracker := range p.postExitTrackers {
		if tracker == nil {
			delete(p.postExitTrackers, tradeID)
			continue
		}
		m := meta[strings.ToUpper(strings.TrimSpace(aster.RawSymbol(tracker.Symbol)))]
		price := m.LastPrice
		if price <= 0 {
			price = tracker.RealizedExitPrice
		}
		if price <= 0 {
			continue
		}
		for label, dur := range map[string]time.Duration{"15m": 15 * time.Minute, "30m": 30 * time.Minute, "60m": 60 * time.Minute} {
			if now.Sub(tracker.ExitTs) <= dur {
				win := tracker.Windows[label]
				updatePaperPostExitWindow(tracker.Side, tracker.EntryPrice, tracker.OriginalStop, price, &win, now)
				tracker.Windows[label] = win
				changed = true
			}
		}
		if rec, ok := p.closedTradeLedger[tradeID]; ok {
			rec.PostExit = buildPaperPostExitSection(tracker.Side, tracker.EntryPrice, tracker.OriginalStop, tracker.RealizedExitPrice, tracker.OriginalTP1, tracker.OriginalTP2, tracker.OriginalTP3, tracker.Windows)
			p.closedTradeLedger[tradeID] = rec
			changed = true
		}
		if now.Sub(tracker.ExitTs) > 60*time.Minute {
			delete(p.postExitTrackers, tradeID)
			changed = true
		}
	}
	if changed {
		_ = p.writeClosedTradesJSONL()
	}
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

func (p *paperTrader) shouldMarkHarvest(pos *paperPosition, reason string, net float64) bool {
	if p == nil || pos == nil || net <= 0 {
		return false
	}
	reason = strings.ToUpper(strings.TrimSpace(reason))
	if reason == "TP3" {
		return true
	}
	if reason == "TRAIL_STOP" && pos.HitTP3 {
		return true
	}
	return false
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
	qty = applyPaperPartialFill(qty, exitSideForPosition(pos.Side), ob, p.partialFillEnable, p.partialFillMinFrac)
	if qty <= 0 {
		return
	}
	fillPx := paperSimFillPrice(exitSideForPosition(pos.Side), qty, m, ob, data.CurrentRegimeCT(now), false)
	if fillPx > 0 {
		exitPrice = fillPx
	}
	reasonU := strings.ToUpper(strings.TrimSpace(reason))
	if (reasonU == "SL" || reasonU == "TRAIL_STOP" || strings.Contains(reasonU, "STOP")) && p.stopMarketSlipBps > 0 {
		exitPrice = applyBpsAdverse(exitPrice, exitSideForPosition(pos.Side), p.stopMarketSlipBps)
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
	pos.GrossRealized += gross
	pos.FeesRealized += fee
	p.balance += net
	p.recordDayStat(now, reason, gross, fee, net)
	loc := p.reportLoc
	if loc == nil {
		loc = time.Local
	}
	dayKeyForStats := now.In(loc).Format("2006-01-02")
	ds := p.dayStats[dayKeyForStats]
	protectedAfterProof := pos.MaxFavorableR >= winnerProofR() && !stopStillOriginal(pos.Stop, pos.OriginalStop)
	winnerRevertedUnprotected := pos.MaxFavorableR >= winnerProofR() && net < 0 && stopStillOriginal(pos.Stop, pos.OriginalStop)
	if ds != nil && net < 0 {
		ds.SumMaxROnLosers += pos.MaxFavorableR
		ds.LosingTradesWithMaxR++
		if pos.MaxFavorableR > 0 {
			ds.StoppedAfterPositiveOpenCount++
		}
		if pos.MaxFavorableR >= winnerProofR() {
			ds.WinnerRevertedToLossCount++
			if net > -0.25 {
				ds.WinnerRevertedToSmallLossCount++
			}
			if stopStillOriginal(pos.Stop, pos.OriginalStop) {
				ds.TradesHit1RButNotProtected++
			}
		}
	}
	logExitAudit(
		symbol,
		firstNonEmpty(strings.TrimSpace(pos.EntryStrategyID), strings.TrimSpace(pos.EntryReason), "unknown"),
		reasonU,
		reasonU,
		p.stopTriggerRef,
		exitPrice,
		pos.Stop,
		pos.MaxFavorableR,
		-pos.MaxAdverseR,
		paperProtectionState(pos),
		pos.EntryTiming,
		false,
		protectedAfterProof,
		winnerRevertedUnprotected,
	)
	if p.lastExitAt != nil {
		p.lastExitAt[symbol] = now
	}
	if p.lastExitLoss != nil {
		p.lastExitLoss[symbol] = net < 0
	}
	if p.lastHarvestAt != nil && p.shouldMarkHarvest(pos, reason, net) {
		p.lastHarvestAt[symbol] = now
	}
	strategyID := firstNonEmpty(strings.TrimSpace(pos.EntryStrategyID), strings.TrimSpace(pos.EntryReason))
	registerReentryExit(symbol, strategyID, pos.Side, reasonU, pos.CombinedScore, pos.MaxFavorableR, net < 0, now)
	p.recordExecutionGovernorExit(now, pos, reasonU, net)
	if net < 0 {
		if ds != nil {
			ds.SameSymbolReentryLossCount++
		}
		p.registerPaperLoss(now, pos)
		if p.lossStreak != nil {
			p.lossStreak[symbol] = p.lossStreak[symbol] + 1
			if p.maxLossStreak > 0 && p.lossLock > 0 && p.lossStreak[symbol] >= p.maxLossStreak && p.lockUntil != nil {
				p.lockUntil[symbol] = now.Add(p.lossLock)
			}
		}
	} else {
		if !isSoftChurnExit(reasonU) {
			clearReentryLoss(symbol)
		}
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
	fmt.Printf("paper exit %s %s reason=%s qty=%.6f entry=%.6f exit=%.6f pnl=%+.4f realized=%+.4f rem=%.6f balance=%.2f hold=%.1fm max_r_seen=%.4f min_r_seen=%.4f protection_state=%s entry_timing=%s no_proof_triggered=%t\n",
		symbol, pos.Side, reason, qty, pos.Entry, exitPrice, net, pos.Realized, pos.Qty, p.balance, holdMin,
		pos.MaxFavorableR, -pos.MaxAdverseR, paperProtectionState(pos), firstNonEmpty(strings.TrimSpace(pos.EntryTiming), "unknown"), false)
	if p.onExit != nil {
		loc := p.reportLoc
		if loc == nil {
			loc = time.Local
		}
		dayKey := currentSessionDayKey(now)
		realizedToday := 0.0
		if ds := p.dayStats[dayKey]; ds != nil {
			realizedToday = ds.Net
		}
		p.onExit(fmt.Sprintf(
			"%s <b>PAPER EXIT | %s %s</b>\n• <b>Qty:</b> %.6f | <b>Exit:</b> %s\n• <b>PnL:</b> %+.2f (%+.2f%%)\n• <b>Reason:</b> %s | <b>Hold:</b> %.1fm\n• <b>Remaining:</b> %.6f | <b>Session:</b> %s\n• <b>Realized Today:</b> %+.2f | <b>Balance:</b> $%.2f",
			exitAlertEmoji(reasonU), symbol, pos.Side, qty, fmtPrice(exitPrice), net, pct, reasonU, holdMin, pos.Qty, sessionTag(now.In(loc)),
			realizedToday, p.balance,
		))
	}
	_ = p.logTrade(now, pos, exitPrice, qty, reason, gross, fee, net, holdMin, m, ob)
	if pos.Qty <= 1e-10 {
		p.recordClosedTrade(now, pos, exitPrice, reason, holdMin)
		loc := p.reportLoc
		if loc == nil {
			loc = time.Local
		}
		dayKey := currentSessionDayKey(now)
		if p.symbolTradeDay != nil && p.symbolTradeCount != nil {
			if p.symbolTradeDay[symbol] != dayKey {
				p.symbolTradeDay[symbol] = dayKey
				p.symbolTradeCount[symbol] = 0
			}
			p.symbolTradeCount[symbol] = p.symbolTradeCount[symbol] + 1
		}
		delete(p.positions, symbol)
	}
	_ = p.save()
}

func (p *paperTrader) recordClosedTrade(now time.Time, pos *paperPosition, exitPrice float64, rawReason string, holdMin float64) {
	if p == nil || pos == nil {
		return
	}
	if strings.TrimSpace(pos.TradeID) == "" {
		pos.TradeID = newPaperTradeID(pos.OpenedAt, pos.Symbol, pos.Side)
	}
	if p.closedTradeLedger == nil {
		p.closedTradeLedger = map[string]paperClosedTradeRecord{}
	}
	strategy, missing := normalizeClosedTradeStrategy(pos.EntryReason)
	rec := paperClosedTradeRecord{
		TradeID: pos.TradeID,
		Mode:    "paper",
		Symbol:  strings.ToUpper(strings.TrimSpace(aster.RawSymbol(pos.Symbol))),
		Side:    strings.ToUpper(strings.TrimSpace(pos.Side)),
		Identity: paperClosedTradeIdentity{
			Strategy:         strategy,
			RawStrategy:      firstNonEmpty(strings.TrimSpace(pos.RawEntryReason), strings.TrimSpace(pos.EntryReason)),
			StrategyMissing:  missing,
			SetupFamily:      firstNonEmpty(strings.TrimSpace(pos.EntrySetupFamily), "unknown"),
			SetupSource:      firstNonEmpty(strings.TrimSpace(pos.EntrySetupSource), "unknown"),
			TradeHorizon:     firstNonEmpty(strings.TrimSpace(pos.EntryTradeHorizon), "unknown"),
			ExecBucket:       firstNonEmpty(strings.TrimSpace(pos.ExecBucket), "unknown"),
			EntryStyle:       firstNonEmpty(strings.TrimSpace(pos.EntryStyle), "unknown"),
			StrategyFamily:   firstNonEmpty(strings.TrimSpace(pos.EntryStrategyFamily), "unknown"),
			Session:          firstNonEmpty(strings.TrimSpace(pos.EntrySession), "unknown"),
			Grade:            firstNonEmpty(strings.TrimSpace(pos.EntryGrade), "unknown"),
			ConfluenceScore:  pos.EntryConfluenceScore,
			EntryTiming:      firstNonEmpty(strings.TrimSpace(pos.EntryTiming), "unknown"),
			CandidateAgeSecs: pos.CandidateAgeSeconds,
			DistanceToVWAP:   pos.EntryDistanceToVWAPPct,
			ATRExension:      pos.EntryATRExtension,
		},
		Entry: paperClosedTradeEntry{
			EntryTs:    pos.OpenedAt.UTC(),
			EntryPrice: pos.Entry,
			Qty:        pos.InitialQty,
			Leverage:   pos.Leverage,
			MarginUsed: pos.Margin,
		},
		Plan: paperClosedTradePlan{
			OriginalStop:          pos.OriginalStop,
			OriginalTP1:           pos.OriginalTP1,
			OriginalTP2:           pos.OriginalTP2,
			OriginalTP3:           pos.OriginalTP3,
			PlannedRiskPrice:      plannedRiskPrice(pos.Entry, pos.OriginalStop),
			PlannedRiskPct:        plannedRiskPct(pos.Entry, pos.OriginalStop),
			ShortBucket:           pos.ShortBucket,
			ShortFilterReason:     pos.ShortFilterReason,
			DirectShortAllowed:    pos.DirectShortAllowed,
			RequireConfirmation:   pos.ShortRequireConfirm,
			Pct24hAtEntry:         pos.EntryPct24h,
			Pct4hAtEntry:          pos.EntryPct4h,
			Pct1hAtEntry:          pos.EntryPct1h,
			BounceFromLocalLowPct: pos.BounceFromLocalLowPct,
			FailedBounceConfirmed: pos.FailedBounceConfirmed,
			PostPumpBreakdown:     pos.PostPumpBreakdown,
			LateChaseBlocked:      pos.LateChaseBlocked,
		},
		Exit: paperClosedTradeExit{
			ExitTs:            now.UTC(),
			RealizedExitPrice: exitPrice,
			ExitReason:        normalizedLedgerExitReason(rawReason, pos.Realized, pos),
			RawExitReason:     strings.ToUpper(strings.TrimSpace(rawReason)),
			GrossPnL:          pos.GrossRealized,
			Fees:              pos.FeesRealized,
			NetPnL:            pos.Realized,
			HoldMinutes:       holdMin,
			MaxRSeen:          pos.MaxFavorableR,
			MinRSeen:          -pos.MaxAdverseR,
			ProtectionState:   paperProtectionState(pos),
			NoProofTriggered:  false,
			CloseType:         "full_close",
		},
	}
	rec.PostExit = buildPaperPostExitSection(rec.Side, rec.Entry.EntryPrice, rec.Plan.OriginalStop, rec.Exit.RealizedExitPrice, rec.Plan.OriginalTP1, rec.Plan.OriginalTP2, rec.Plan.OriginalTP3, map[string]paperPostExitWindowState{
		"15m": {PeakPrice: rec.Exit.RealizedExitPrice, TroughPrice: rec.Exit.RealizedExitPrice},
		"30m": {PeakPrice: rec.Exit.RealizedExitPrice, TroughPrice: rec.Exit.RealizedExitPrice},
		"60m": {PeakPrice: rec.Exit.RealizedExitPrice, TroughPrice: rec.Exit.RealizedExitPrice},
	})
	p.closedTradeLedger[rec.TradeID] = rec
	p.startPostExitObservation(rec)
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
		rpnl   float64
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
			rpnl:   pos.Realized,
			margin: paperPositionMarginUsed(pos),
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
	b.WriteString("| Sym | Side | Margin | Qty | Entry | Mark | Lev | uPnL | rPnL | uPnL% | Age(m) | Reason |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | $%.2f | %.4f | %s | %s | %dx | %+.2f | %+.2f | %+.2f%% | %d | %s |\n",
			r.sym, r.side, r.margin, r.qty, fmtPrice(r.entry), fmtPrice(r.mark), r.lev, r.upnl, r.rpnl, r.upct, r.ageMin, colorReasonTag(r.reason))
	}
	fmt.Fprintf(&b, "\nTotals: openPnL=%+.2f realizedToday=%+.2f netDay=%+.2f", totalUPnL, realizedToday, realizedToday+totalUPnL)
	return strings.TrimSpace(b.String())
}

func (p *paperTrader) TradeUpdateSignature(meta map[string]symbolMeta, topN int) string {
	if p == nil || !p.enabled || len(p.positions) == 0 {
		return ""
	}
	type row struct {
		sym   string
		side  string
		entry float64
		mark  float64
		qty   float64
		upnl  float64
		lev   int
		stop  float64
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
		rows = append(rows, row{
			sym:   sym,
			side:  pos.Side,
			entry: pos.Entry,
			mark:  mark,
			qty:   pos.Qty,
			upnl:  upnl,
			lev:   maxInt(pos.Leverage, 1),
			stop:  pos.Stop,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].sym < rows[j].sym })
	parts := make([]string, 0, len(rows)+1)
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("%s|%s|%.6f|%.6f|%.6f|%dx|%.2f|%.6f", r.sym, r.side, r.entry, r.mark, r.qty, r.lev, r.upnl, r.stop))
	}
	parts = append(parts, fmt.Sprintf("totals|%d|%.2f|%.2f", len(rows), totalUPnL, p.balance+totalUPnL))
	return strings.Join(parts, ";")
}

func (p *paperTrader) DailyReportMessage(dayKey string) (string, bool) {
	if p == nil || !p.enabled {
		return "", false
	}
	ds := p.dayStats[dayKey]
	if ds == nil || ds.Trades == 0 {
		return notify.FormatDailyRecap(notify.RecapView{
			Mode:              "PAPER",
			Date:              dayKey,
			RealizedPnL:       0,
			TradeCount:        0,
			WinRate:           0,
			BestTrade:         "-",
			WorstTrade:        "-",
			RiskNoteOrSummary: "No trades",
		}), true
	}
	winRate := 0.0
	if ds.Trades > 0 {
		winRate = (float64(ds.Wins) / float64(ds.Trades)) * 100.0
	}
	return notify.FormatDailyRecap(notify.RecapView{
		Mode:              "PAPER",
		Date:              dayKey,
		RealizedPnL:       ds.Net,
		TradeCount:        ds.Trades,
		WinRate:           winRate,
		BestTrade:         fmt.Sprintf("Gross %+.2f", ds.Gross),
		WorstTrade:        fmt.Sprintf("Fees %.2f", ds.Fees),
		RiskNoteOrSummary: fmt.Sprintf("Winner guard hits %d", ds.TradesHit1RButNotProtected),
	}), true
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
	return p.calcTrailStopForPosition(nil, sideBuy, ref, false)
}

func (p *paperTrader) calcTrailStopForPosition(pos *paperPosition, sideBuy bool, ref float64, postTP3 bool) float64 {
	if ref <= 0 {
		return 0
	}
	trailMode := strings.ToLower(strings.TrimSpace(envStr("LIVE_PAPER_TRAIL_MODE", envStr("LIVE_TRAIL_MODE", "hybrid"))))
	pct := p.trailStopPct / 100.0
	if postTP3 && p.trailStopPctTP3 > 0 {
		pct = p.trailStopPctTP3 / 100.0
	}
	if pct <= 0 {
		pct = 0.01
	}
	dist := ref * pct
	floorDist := ref * (p.trailPctMin / 100.0)
	atrDist := 0.0
	if pos != nil && pos.Symbol != "" {
		atrPct := estimateATRPctWithCache(p.featureCache, pos.Symbol, maxInt(p.atrLen*4, 64), p.atrLen)
		if atrPct > 0 {
			atrDist = ref * atrPct * trailATRMultForContext(pos.EntryReason, atrPct, pos.EntryVolumeUSD)
		}
	}
	structDist := 0.0
	if pos != nil {
		structDist = structureTrailDistance(ref, pos.OpposingFriction)
	}
	switch trailMode {
	case "structure":
		if structDist > 0 {
			dist = maxFloat(floorDist, structDist)
		}
	case "atr":
		if atrDist > 0 {
			dist = maxFloat(floorDist, atrDist)
		}
	default:
		if atrDist > 0 {
			dist = maxFloat(floorDist, atrDist)
		}
		if structDist > 0 {
			dist = maxFloat(dist, structDist)
		}
	}
	if pos != nil {
		dist *= trailProfileMultiplier(pos.ExitProfile)
		if pos.Sponsored {
			dist *= envFloat("LIVE_TRAIL_SPONSORED_MULT", 1.15)
		} else if pos.HitTP1 {
			dist *= envFloat("LIVE_TRAIL_UNSPONSORED_MULT", 0.85)
			if pos.WeakSponsorStreak >= envInt("LIVE_TRAIL_WEAK_SPONSOR_STREAK", 2) {
				dist *= envFloat("LIVE_TRAIL_WEAK_SPONSOR_MULT", 0.75)
			}
		}
		if confluenceRefreshActive(time.Now().UTC(), pos.LastConfluenceRefresh) {
			dist *= envFloat("LIVE_TRAIL_CONFLUENCE_REFRESH_MULT", 1.30)
		}
	}
	if postTP3 {
		if pos != nil && pos.Sponsored {
			dist *= envFloat("LIVE_TRAIL_SPONSORED_POST_TP3_MULT", 1.25)
		} else {
			dist *= envFloat("LIVE_TRAIL_UNSPONSORED_POST_TP3_MULT", 0.95)
		}
	}
	if sideBuy {
		return ref - dist
	}
	return ref + dist
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
	if isLongSide(posSide) {
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
	if isLongSide(side) {
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
	if isLongSide(side) {
		return px * (1 + d)
	}
	return px * (1 - d)
}

func applyBpsAdverse(px float64, side string, bps float64) float64 {
	if px <= 0 || bps <= 0 {
		return px
	}
	d := bps / 10000.0
	if isLongSide(side) {
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
	if !isLongSide(side) {
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

func (p *paperTrader) logTrade(now time.Time, pos *paperPosition, exit, qty float64, reason string, gross, fee, net, holdMin float64, m symbolMeta, ob aster.OrderBook) error {
	if p == nil || !p.enabled {
		return nil
	}
	if pos == nil {
		return fmt.Errorf("nil paper position")
	}
	symbol := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(pos.Symbol)))
	side := strings.ToUpper(strings.TrimSpace(pos.Side))
	entry := pos.Entry
	lev := pos.Leverage
	margin := pos.Margin
	stop := pos.Stop
	if err := ensureCSVWithHeader(p.tradesCSV, []string{
		"exit_ts", "symbol", "side", "entry", "exit", "qty", "lev", "margin", "stop", "tp", "reason", "gross_pnl", "fees", "net_pnl", "balance", "hold_min",
		"trade_id", "strategy", "setup_family", "setup_source", "trade_horizon", "exec_bucket", "entry_style", "strategy_family",
		"original_stop", "original_tp1", "original_tp2", "original_tp3",
		"realized_exit_price", "raw_exit_reason", "normalized_exit_reason",
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
		fmt.Sprintf("%.8f", pos.OriginalTP1),
		reason,
		fmt.Sprintf("%.8f", gross),
		fmt.Sprintf("%.8f", fee),
		fmt.Sprintf("%.8f", net),
		fmt.Sprintf("%.8f", p.balance),
		fmt.Sprintf("%.2f", holdMin),
		firstNonEmpty(strings.TrimSpace(pos.TradeID), newPaperTradeID(pos.OpenedAt, symbol, side)),
		firstNonEmpty(strings.TrimSpace(pos.EntryReason), "unknown"),
		firstNonEmpty(strings.TrimSpace(pos.EntrySetupFamily), "unknown"),
		firstNonEmpty(strings.TrimSpace(pos.EntrySetupSource), "unknown"),
		firstNonEmpty(strings.TrimSpace(pos.EntryTradeHorizon), "unknown"),
		firstNonEmpty(strings.TrimSpace(pos.ExecBucket), "unknown"),
		firstNonEmpty(strings.TrimSpace(pos.EntryStyle), "unknown"),
		firstNonEmpty(strings.TrimSpace(pos.EntryStrategyFamily), "unknown"),
		fmt.Sprintf("%.8f", pos.OriginalStop),
		fmt.Sprintf("%.8f", pos.OriginalTP1),
		fmt.Sprintf("%.8f", pos.OriginalTP2),
		fmt.Sprintf("%.8f", pos.OriginalTP3),
		fmt.Sprintf("%.8f", exit),
		strings.ToUpper(strings.TrimSpace(reason)),
		normalizedLedgerExitReason(reason, net, pos),
	}
	if p != nil && p.eventLog != nil {
		markPx, lastPx := paperMarkLastPrices(m, ob, p.markLastModel, p.markLastDivBps)
		triggerRef := p.stopTriggerRef
		reasonU := strings.ToUpper(strings.TrimSpace(reason))
		if strings.HasPrefix(reasonU, "TP") {
			triggerRef = p.tpTriggerRef
		}
		pnlPct := 0.0
		if entry > 0 {
			if strings.EqualFold(side, "BUY") {
				pnlPct = ((exit - entry) / entry) * 100.0
			} else {
				pnlPct = ((entry - exit) / entry) * 100.0
			}
		}
		proofMin := 0.0
		if !pos.FirstProtectAt.IsZero() && !pos.OpenedAt.IsZero() && !pos.FirstProtectAt.Before(pos.OpenedAt) {
			proofMin = pos.FirstProtectAt.Sub(pos.OpenedAt).Minutes()
		}
		failureMin := 0.0
		if net < 0 {
			failureMin = holdMin
		}
		riskR := 0.0
		if entry > 0 && stop > 0 {
			risk := math.Abs(entry-stop) * qty
			if risk > 0 {
				riskR = net / risk
			}
		}
		p.eventLog.Emit(stats.Event{
			Timestamp:       now,
			Type:            "POSITION_CLOSE",
			Simulated:       true,
			Symbol:          symbol,
			Side:            side,
			Source:          firstNonEmpty(strings.TrimSpace(pos.EntryMode), "paper"),
			Mode:            firstNonEmpty(strings.TrimSpace(pos.EntryMode), "paper"),
			TF:              "1m",
			Strategy:        pos.EntryReason,
			SetupFamily:     pos.EntrySetupFamily,
			SetupSource:     pos.EntrySetupSource,
			TradeHorizon:    pos.EntryTradeHorizon,
			Grade:           pos.EntryGrade,
			State:           string(pos.EntryState),
			TriggerState:    pos.EntryTrigger,
			ExitProfile:     pos.ExitProfile,
			ConfluenceScore: pos.EntryConfluenceScore,
			StrategyReasons: append([]string(nil), pos.EntrySignalReasons...),
			StrategySources: append([]string(nil), pos.EntrySignalSources...),
			EntryPx:         entry,
			ExitPx:          exit,
			MarkPx:          markPx,
			LastPx:          lastPx,
			TriggerRef:      triggerRef,
			RiskR:           riskR,
			HoldMin:         holdMin,
			ProofMin:        proofMin,
			FailureMin:      failureMin,
			MFER:            pos.MaxFavorableR,
			MAER:            pos.MaxAdverseR,
			CaptureRatio:    pos.CaptureRatio,
			MaxGivebackR:    pos.MaxGivebackR,
			PnLUSD:          net,
			PnLPct:          pnlPct,
			Fees:            fee,
			Discovery:       pos.DiscoveryScore,
			Trigger:         pos.TriggerScore,
			Execution:       pos.ExecutionScore,
			Combined:        pos.CombinedScore,
			StopDistPct:     pos.StopDistancePct,
			Reason:          reason,
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
		grade := strings.TrimSpace(r.Grade)
		if grade == "" || strings.EqualFold(grade, "N/A") {
			grade = market.FallbackGradeForMarket(r.Score, r.Market, side)
		}
		conf[r.Symbol] = grade
	}
	sort.Slice(eligibleByScore, func(i, j int) bool { return eligibleByScore[i].Score > eligibleByScore[j].Score })
	for i := 0; i < len(eligibleByScore) && i < gradeTopN; i++ {
		sym := eligibleByScore[i].Symbol
		lbl := confluenceLabel(c, sym, side)
		if lbl == "" || lbl == "_" || lbl == "C" {
			lbl = market.FallbackGradeForMarket(eligibleByScore[i].Score, eligibleByScore[i].Market, side)
		}
		conf[sym] = lbl
	}
	return out, conf
}

type candidateSelectConfig struct {
	UseContinuous           bool
	MinNormalizedScore      float64
	MinCompleteness         float64
	MinConfidence           float64
	ReversalMinScore        float64
	ReversalMinConfidence   float64
	ReversalMinComplete     float64
	ReversalMinStateMin     float64
	ReversalShortCoolingMin float64
	ReversalShortMinSlope   float64
}

type rankSortConfig struct {
	UseConfidence      bool
	ConfidenceWeight   float64
	UseCompleteness    bool
	CompletenessWeight float64
	UseReliability     bool
	ReliabilityWeight  float64
	UseVolume          bool
	VolumeWeight       float64
}

func chooseCandidates(longInPlay, shortInPlay []inplay.Entry, minGrade string, enableMomentumReversal bool, reversalMinGrade string, reversalSlopeMin float64, bNearAOnly bool, bNearAScoreMin float64, reversalTopLongN int, cfg candidateSelectConfig) []candidate {
	minVal := gradeValue(minGrade)
	revMinVal := gradeValue(reversalMinGrade)
	longByRaw := make(map[string]inplay.Entry, len(longInPlay))
	for _, e := range longInPlay {
		longByRaw[strings.ToUpper(aster.RawSymbol(e.Symbol))] = e
	}
	if reversalSlopeMin < 0 {
		reversalSlopeMin = -reversalSlopeMin
	}
	if cfg.ReversalShortCoolingMin < cfg.ReversalMinStateMin {
		cfg.ReversalShortCoolingMin = cfg.ReversalMinStateMin
	}
	if cfg.ReversalShortMinSlope < reversalSlopeMin {
		cfg.ReversalShortMinSlope = reversalSlopeMin
	}
	reversalReady := func(e inplay.Entry) bool {
		if e.State == inplay.StateExhausted {
			return e.TimeInStateMin >= cfg.ReversalMinStateMin && !e.Momentum
		}
		if e.State != inplay.StateCooling && e.State != inplay.StateDumping {
			return false
		}
		if e.TimeInStateMin < cfg.ReversalMinStateMin {
			return false
		}
		if e.Momentum {
			return false
		}
		return e.ScoreSlope <= -reversalSlopeMin
	}
	reversalShortReady := func(e inplay.Entry) bool {
		if e.Momentum {
			return false
		}
		switch e.State {
		case inplay.StateDumping:
			return e.TimeInStateMin >= cfg.ReversalMinStateMin && e.ScoreSlope <= -cfg.ReversalShortMinSlope
		case inplay.StateExhausted:
			return e.TimeInStateMin >= cfg.ReversalMinStateMin && e.ScoreSlope <= -reversalSlopeMin
		case inplay.StateCooling:
			return e.TimeInStateMin >= cfg.ReversalShortCoolingMin && e.ScoreSlope <= -cfg.ReversalShortMinSlope
		default:
			return false
		}
	}
	exhaustionFlipReady := func(e inplay.Entry) bool {
		minRevScore := envFloat("LIVE_EXHAUSTION_REVERSAL_SCORE_MIN", 5.5)
		minDrawdown := envFloat("LIVE_EXHAUSTION_MIN_DRAWDOWN_PCT", -8.0)
		minSlope := envFloat("LIVE_EXHAUSTION_MIN_SLOPE", -0.75)
		if !inplay.EarlyShortAdmission(e, maxFloat(5.0, minRevScore-0.5)) {
			return false
		}
		if e.DrawdownFromPeakPct > minDrawdown {
			return false
		}
		if e.ScoreSlope > minSlope {
			return false
		}
		if e.State != inplay.StateDumping && e.State != inplay.StateExhausted {
			return false
		}
		return e.EntryStyle == "reversal_watch_short" || e.MetaState == "long_exhausting" || e.BearReversalScore >= minRevScore
	}
	exhaustionFlipLongReady := func(e inplay.Entry) bool {
		minBullScore := envFloat("LIVE_EXHAUSTION_LONG_BULL_SCORE_MIN", 4.5)
		minDrawup := envFloat("LIVE_EXHAUSTION_LONG_MIN_DRAWUP_PCT", 6.0)
		minSlope := envFloat("LIVE_EXHAUSTION_LONG_MIN_SLOPE", 0.50)
		if !inplay.EarlyLongAdmissionFromShortLeader(e, minBullScore) {
			return false
		}
		if e.DrawupFromTroughPct < minDrawup {
			return false
		}
		if e.ScoreSlope < minSlope {
			return false
		}
		if e.State != inplay.StateBalanced && e.State != inplay.StateHeating && e.State != inplay.StateInPlay {
			return false
		}
		return e.EntryStyle == "reversal_watch_long" || e.MetaState == "short_exhausting" || e.BullReversalScore >= minBullScore
	}
	stillOpposingLeaderStrong := func(e inplay.Entry) bool {
		if e.LongDemotionFlag {
			return false
		}
		if e.Momentum {
			return true
		}
		switch e.State {
		case inplay.StatePumping, inplay.StateInPlay, inplay.StateHeating:
			return true
		}
		return e.ScoreSlope > 0
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
	allowByQuality := func(e inplay.Entry, forReversal bool) bool {
		if !cfg.UseContinuous {
			return true
		}
		minScore := cfg.MinNormalizedScore
		minConf := cfg.MinConfidence
		minComp := cfg.MinCompleteness
		if forReversal {
			if cfg.ReversalMinScore > 0 {
				minScore = cfg.ReversalMinScore
			}
			if cfg.ReversalMinConfidence > 0 {
				minConf = cfg.ReversalMinConfidence
			}
			if cfg.ReversalMinComplete > 0 {
				minComp = cfg.ReversalMinComplete
			}
		}
		if e.CurrentScore < minScore {
			return false
		}
		if e.MarketConfidence < minConf {
			return false
		}
		if e.Completeness < minComp {
			return false
		}
		return true
	}
	leaderUnwindShortReady := func(shortE, longE inplay.Entry, hasLong bool) bool {
		if !hasLong {
			return false
		}
		minScore := envFloat("LIVE_LEADER_UNWIND_SHORT_MIN_SCORE", 88.0)
		minDayUTC := envFloat("LIVE_LEADER_UNWIND_SHORT_MIN_DAYUTC_PCT", -20.0)
		minSlope := envFloat("LIVE_LEADER_UNWIND_SHORT_MIN_SLOPE", 0.35)
		minGap := envFloat("LIVE_LEADER_UNWIND_SHORT_MIN_SCORE_GAP", 12.0)
		maxOppSlope := envFloat("LIVE_LEADER_UNWIND_OPPOSING_MAX_SLOPE", 0.10)
		if shortE.CurrentScore < minScore {
			return false
		}
		if shortE.DayUTCPct > minDayUTC {
			return false
		}
		switch shortE.State {
		case inplay.StateHeating, inplay.StateInPlay, inplay.StatePumping:
		default:
			return false
		}
		if shortE.ScoreSlope < minSlope && !shortE.Momentum {
			return false
		}
		longWeakState := longE.State == inplay.StateBalanced || longE.State == inplay.StateCooling || longE.State == inplay.StateDumping || longE.State == inplay.StateExhausted
		longWeakStyle := longE.EntryStyle == "none" || longE.EntryStyle == "avoid_chase" || longE.EntryStyle == "reversal_watch_short"
		longScoreGap := shortE.CurrentScore - longE.CurrentScore
		if longE.LongDemotionFlag {
			return true
		}
		if !longE.Momentum && (longWeakState || longWeakStyle || longE.ScoreSlope <= maxOppSlope || longScoreGap >= minGap) {
			return true
		}
		return false
	}
	leaderUnwindShortRankBoost := func(shortE, longE inplay.Entry) float64 {
		boost := envFloat("LIVE_LEADER_UNWIND_SHORT_RANK_BOOST", 12.0)
		minDayUTC := math.Abs(envFloat("LIVE_LEADER_UNWIND_SHORT_MIN_DAYUTC_PCT", -20.0))
		boost += min(6.0, maxFloat(0.0, math.Abs(shortE.DayUTCPct)-minDayUTC)*0.25)
		boost += min(4.0, maxFloat(0.0, shortE.CurrentScore-longE.CurrentScore)/6.0)
		boost += min(3.0, maxFloat(0.0, shortE.ScoreSlope-envFloat("LIVE_LEADER_UNWIND_SHORT_MIN_SLOPE", 0.35))*4.0)
		return boost
	}
	out := make([]candidate, 0, len(longInPlay)+len(shortInPlay))
	for _, e := range longInPlay {
		if !allow(e) {
			continue
		}
		if exhaustionFlipReady(e) {
			flip := e
			flip.Rank = e.Rank + 10 + 5*abs(e.ScoreSlope) + e.IntradayReversalScore
			out = append(out, candidate{
				Entry: flip,
				Side:  "SELL",
				Strat: "exhaustion_flip_short",
			})
		}
		if e.State == inplay.StateExhausted || e.LongDemotionFlag || e.EntryStyle == "reversal_watch_short" || e.EntryStyle == "avoid_chase" {
			continue
		}
		longContinuationOK := (e.State == inplay.StatePumping || e.State == inplay.StateInPlay || e.State == inplay.StateHeating) && (e.ScoreSlope > 0 || e.Momentum)
		if cfg.UseContinuous {
			longContinuationOK = longContinuationOK && allowByQuality(e, false)
		} else {
			longContinuationOK = longContinuationOK && gradeValue(e.CurrentGrade) >= minVal
		}
		if longContinuationOK {
			out = append(out, candidate{Entry: e, Side: "BUY"})
		}
		reversalOK := enableMomentumReversal &&
			reversalReady(e)
		if cfg.UseContinuous {
			reversalOK = reversalOK && allowByQuality(e, true)
		} else {
			reversalOK = reversalOK && gradeValue(e.CurrentGrade) >= revMinVal
		}
		if reversalOK {
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
		if exhaustionFlipLongReady(e) {
			flip := e
			flip.Rank = e.Rank + 10 + 5*abs(e.ScoreSlope) + e.BullReversalScore
			out = append(out, candidate{
				Entry: flip,
				Side:  "BUY",
				Strat: "exhaustion_flip_long",
			})
		}
		if e.State == inplay.StateExhausted || e.ShortDemotionFlag || e.EntryStyle == "reversal_watch_long" || e.EntryStyle == "avoid_chase" {
			continue
		}
		shortContinuationOK := (e.State == inplay.StatePumping || e.State == inplay.StateInPlay || e.State == inplay.StateHeating) && (e.ScoreSlope > 0 || e.Momentum)
		if cfg.UseContinuous {
			shortContinuationOK = shortContinuationOK && allowByQuality(e, false)
		} else {
			shortContinuationOK = shortContinuationOK && gradeValue(e.CurrentGrade) >= minVal
		}
		if shortContinuationOK {
			candEntry := e
			if longPeer, ok := longByRaw[strings.ToUpper(aster.RawSymbol(e.Symbol))]; ok && leaderUnwindShortReady(e, longPeer, true) {
				candEntry.Rank = e.Rank + leaderUnwindShortRankBoost(e, longPeer)
			}
			out = append(out, candidate{Entry: candEntry, Side: "SELL"})
		}
		reversalOK := enableMomentumReversal &&
			reversalReady(e)
		if cfg.UseContinuous {
			reversalOK = reversalOK && allowByQuality(e, true)
		} else {
			reversalOK = reversalOK && gradeValue(e.CurrentGrade) >= revMinVal
		}
		if reversalOK {
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
			if !allow(e) {
				continue
			}
			if exhaustionFlipReady(e) {
				continue
			}
			if !reversalShortReady(e) || stillOpposingLeaderStrong(e) {
				continue
			}
			if cfg.UseContinuous {
				if !allowByQuality(e, true) {
					continue
				}
			} else if gradeValue(e.CurrentGrade) < revMinVal {
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

func applyCandidateLifecycle(in []candidate, now time.Time, mem map[string]candidateMemory, cfg candidateLifecycleConfig) []candidate {
	if !cfg.Enable {
		return in
	}
	seenKeys := make(map[string]struct{}, len(in))
	out := make([]candidate, 0, len(in))
	for _, c := range in {
		key := candidateKey(c)
		seenKeys[key] = struct{}{}
		m := mem[key]
		if now.Sub(m.LastSeen) > cfg.ExpireAfter {
			m = candidateMemory{}
		}
		m.SeenScans++
		m.LastSeen = now
		stage := "WATCH"
		if m.SeenScans >= cfg.ArmScans {
			stage = "ARMED"
		}
		if m.SeenScans >= cfg.ReadyScans &&
			c.Entry.CurrentScore >= cfg.ReadyMinScore &&
			(c.Entry.ScoreSlope >= cfg.ReadyMinSlope || c.Entry.Momentum) {
			stage = "READY"
		}
		if c.Entry.State == inplay.StateBalanced && c.Entry.TimeInStateMin > cfg.ExpireAfter.Minutes() {
			stage = "EXPIRED"
		}
		m.Stage = stage
		mem[key] = m
		c.LifecycleStage = stage
		c.LifecycleScans = m.SeenScans
		if stage == "EXPIRED" {
			if c.RejectReason == "" {
				c.RejectReason = "candidate_expired"
			}
		}
		out = append(out, c)
	}
	for k, v := range mem {
		if _, ok := seenKeys[k]; ok {
			continue
		}
		if now.Sub(v.LastSeen) > cfg.ExpireAfter {
			delete(mem, k)
		}
	}
	return out
}

func candidateKey(c candidate) string {
	return strings.ToUpper(aster.RawSymbol(c.Entry.Symbol)) + "|" + strings.ToUpper(strings.TrimSpace(c.Side))
}

func recentRejectKey(c candidate) string {
	return strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol))) + "|" + strings.ToUpper(strings.TrimSpace(c.Side))
}

func suppressibleRepeatReject(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "weak_slope", "spread_too_wide", "extended", "not_top_leader":
		return true
	default:
		return false
	}
}

func materiallyChangedAfterReject(prev recentRejectMemory, c candidate) bool {
	if strings.TrimSpace(prev.State) != strings.TrimSpace(string(c.Entry.State)) {
		return true
	}
	if math.Abs(prev.Score-c.Entry.CurrentScore) >= envFloat("LIVE_REPEAT_REJECT_SCORE_DELTA", 2.5) {
		return true
	}
	if math.Abs(prev.Slope-c.Entry.ScoreSlope) >= envFloat("LIVE_REPEAT_REJECT_SLOPE_DELTA", 0.05) {
		return true
	}
	if math.Abs(prev.SpreadBps-c.SpreadBps) >= envFloat("LIVE_REPEAT_REJECT_SPREAD_DELTA_BPS", 2.0) {
		return true
	}
	if math.Abs(prev.ExtensionATR-c.ExtensionATR) >= envFloat("LIVE_REPEAT_REJECT_EXTENSION_DELTA", 0.20) {
		return true
	}
	if math.Abs(prev.FinalRank-c.FinalRank) >= envFloat("LIVE_REPEAT_REJECT_FINAL_RANK_DELTA", 5.0) {
		return true
	}
	return false
}

func shouldSuppressDuplicateReject(mem map[string]recentRejectMemory, now time.Time, c candidate, reason string, cfg acceptanceQueueConfig) bool {
	if cfg.RecentRejectTTL <= 0 || !suppressibleRepeatReject(reason) {
		return false
	}
	key := recentRejectKey(c)
	rec, ok := mem[key]
	if !ok {
		return false
	}
	if now.After(rec.ExpiresAt) {
		delete(mem, key)
		return false
	}
	if strings.TrimSpace(rec.Reject) != strings.TrimSpace(reason) {
		return false
	}
	if materiallyChangedAfterReject(rec, c) {
		return false
	}
	rec.LastAttempt = now
	rec.ExpiresAt = now.Add(cfg.RecentRejectTTL)
	mem[key] = rec
	return true
}

func sideEntryMap(entries []inplay.Entry) map[string]inplay.Entry {
	out := make(map[string]inplay.Entry, len(entries))
	for _, e := range entries {
		out[strings.ToUpper(strings.TrimSpace(aster.RawSymbol(e.Symbol)))] = e
	}
	return out
}

func currentEntryMap(longInPlay, shortInPlay []inplay.Entry) map[string]inplay.Entry {
	out := sideEntryMap(longInPlay)
	for _, e := range shortInPlay {
		out[strings.ToUpper(strings.TrimSpace(aster.RawSymbol(e.Symbol)))] = e
	}
	return out
}

func rememberRecentReject(mem map[string]recentRejectMemory, now time.Time, c candidate, reason string, cfg acceptanceQueueConfig) {
	if len(mem) == 0 && cfg.RecentRejectTTL <= 0 {
		return
	}
	if cfg.RecentRejectTTL <= 0 {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	if raw == "" {
		return
	}
	if c.DiscoveryScore < 0.60 && c.CombinedScore < 0.55 {
		return
	}
	mem[recentRejectKey(c)] = recentRejectMemory{
		Symbol:       raw,
		Side:         strings.ToUpper(strings.TrimSpace(c.Side)),
		Reject:       reason,
		ExpiresAt:    now.Add(cfg.RecentRejectTTL),
		Discovery:    c.DiscoveryScore,
		Combined:     c.CombinedScore,
		Score:        c.Entry.CurrentScore,
		Slope:        c.Entry.ScoreSlope,
		State:        string(c.Entry.State),
		SpreadBps:    c.SpreadBps,
		ExtensionATR: c.ExtensionATR,
		FinalRank:    c.FinalRank,
		LastAttempt:  now,
	}
}

func activeRecentRejectSymbols(now time.Time, mem map[string]recentRejectMemory) []string {
	if len(mem) == 0 {
		return nil
	}
	out := make([]string, 0, len(mem))
	for sym, rec := range mem {
		if now.After(rec.ExpiresAt) {
			delete(mem, sym)
			continue
		}
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}

func suppressOrRememberReject(mem map[string]recentRejectMemory, now time.Time, c candidate, reason string, cfg acceptanceQueueConfig) bool {
	if shouldSuppressDuplicateReject(mem, now, c, reason, cfg) {
		return true
	}
	rememberRecentReject(mem, now, c, reason, cfg)
	return false
}

func trimRecentTimes(now time.Time, in []time.Time, window time.Duration) []time.Time {
	if len(in) == 0 {
		return in
	}
	if window <= 0 {
		return in
	}
	cut := now.Add(-window)
	out := in[:0]
	for _, ts := range in {
		if !ts.Before(cut) {
			out = append(out, ts)
		}
	}
	return out
}

func strategyFamily(c candidate) string {
	if bp, ok := resolveSetupBlueprint(c, time.Now().UTC()); ok && strings.TrimSpace(bp.StrategyFamily) != "" {
		return bp.StrategyFamily
	}
	strat := strings.ToLower(strings.TrimSpace(c.Strat))
	switch {
	case strings.HasPrefix(strat, "impulse_"):
		return "ignite"
	case strings.Contains(strat, "ignite"):
		return "ignite"
	case strings.Contains(strat, "reset_impulse"):
		return "ignite"
	case strings.Contains(strat, "reversal"), strings.Contains(strat, "flip"):
		return "rev"
	}
	switch strings.TrimSpace(c.Entry.EntryStyle) {
	case "momentum_ignite_long", "momentum_ignite_short":
		return "ignite"
	case "reversal_watch_short", "reversal_watch_long":
		return "rev"
	default:
		return "cont"
	}
}

func setupBlueprintForLabel(label string) (setupBlueprint, bool) {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return setupBlueprint{}, false
	}
	bp, ok := setupBlueprints[label]
	return bp, ok
}

func resolveSetupBlueprint(c candidate, now time.Time) (setupBlueprint, bool) {
	_ = now
	labels := []string{
		c.Strat,
		c.SetupFamily,
		c.Entry.EntryStyle,
	}
	for _, label := range labels {
		if bp, ok := setupBlueprintForLabel(label); ok {
			return bp, true
		}
	}
	style := strings.ToLower(strings.TrimSpace(c.Entry.EntryStyle))
	switch style {
	case "breakout_hold_long", "breakout_hold_short":
		return setupBlueprints["breakout_retest"], true
	case "pullback_long", "pullback_short":
		return setupBlueprints["micro_pullback_continuation"], true
	case "reversal_watch_long", "reversal_watch_short", "leader_unwind_short":
		return setupBlueprints["reversal_exhaustion"], true
	}
	return setupBlueprint{}, false
}

func annotateCandidateSetupBlueprint(c *candidate, now time.Time) {
	if c == nil {
		return
	}
	if bp, ok := resolveSetupBlueprint(*c, now); ok {
		if strings.TrimSpace(c.SetupFamily) == "" {
			c.SetupFamily = bp.SetupFamily
		}
		if strings.TrimSpace(c.SetupSource) == "" {
			c.SetupSource = bp.SetupSource
		}
		if strings.TrimSpace(c.TradeHorizon) == "" {
			c.TradeHorizon = bp.TradeHorizon
		}
	}
}

func candidateTradeHorizon(c candidate, now time.Time) string {
	if strings.TrimSpace(c.TradeHorizon) != "" {
		return strings.ToLower(strings.TrimSpace(c.TradeHorizon))
	}
	if bp, ok := resolveSetupBlueprint(c, now); ok {
		return strings.ToLower(strings.TrimSpace(bp.TradeHorizon))
	}
	return ""
}

func canonicalExecutionStrategy(strat, side string) string {
	strat = strings.ToLower(strings.TrimSpace(strat))
	_ = side
	return strat
}

func isStarterOnlyStrategyName(strat string) bool {
	_ = strat
	return false
}

func postureForcesStarter(c candidate) bool {
	return strings.EqualFold(strings.TrimSpace(c.EntryPosture), "STARTER_NOW")
}

func postureForcesAttack(c candidate) bool {
	return strings.EqualFold(strings.TrimSpace(c.EntryPosture), "ATTACK_NOW")
}

func starterLaneEligible(c candidate) bool {
	if isStarterOnlyStrategyName(c.Strat) {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(c.Sig.Name)), "starter")
}

func starterLaneQualityReady(c candidate) bool {
	if hasFreshStructureReset(c) || continuationStructureConfirmed(c) {
		return true
	}
	if c.PriorDayLeaderBoost >= envFloat("LIVE_PRIOR_DAY_STARTER_READY_MIN_BOOST", 0.45) &&
		!candidateExhaustionActive(c) &&
		!continuationDeteriorating(c) {
		return true
	}
	minSeen := maxInt(1, envInt("LIVE_STARTER_PERSIST_MIN_SEEN", 2))
	minTopN := maxInt(1, envInt("LIVE_STARTER_PERSIST_MIN_TOPN", 1))
	return c.PersistenceSeenCount >= minSeen && c.PersistenceTopNCount >= minTopN
}

func continuationLaneRejectReason(c candidate) string {
	if strategyFamily(c) != "cont" {
		return ""
	}
	if reason := liquidityRiskRejectReason(c); reason != "" {
		return reason
	}
	if candidateExhaustionActive(c) {
		return "continuation_exhaustion_active"
	}
	if continuationDeteriorating(c) {
		return "continuation_impulse_fading"
	}
	if candidateRapidExpansion(c) {
		return "continuation_rapid_expansion"
	}
	if candidateExtendedForBotAdd(c) {
		return "continuation_extended_wait_reset"
	}
	if !hasFreshStructureReset(c) {
		maxVWAPExtPct := envFloat("LIVE_CONT_MAX_VWAP_EXTENSION_PCT", 1.25)
		if maxVWAPExtPct > 0 && c.SessionVWAP > 0 {
			extVWAPPct := math.Abs((c.LastClose-c.SessionVWAP)/c.SessionVWAP) * 100.0
			if extVWAPPct >= maxVWAPExtPct {
				return "continuation_vwap_extension"
			}
		}
		maxEMAExtPct := envFloat("LIVE_CONT_MAX_EMA_EXTENSION_PCT", 1.00)
		if maxEMAExtPct > 0 && c.EMA9 > 0 {
			extEMAPct := math.Abs((c.LastClose-c.EMA9)/c.EMA9) * 100.0
			if extEMAPct >= maxEMAExtPct {
				return "continuation_structure_extension"
			}
		}
	}
	if !hasFreshStructureReset(c) && candidateDirectionalMovePct(c) >= envFloat("LIVE_CONTINUATION_MAX_DIRECTIONAL_EXT_PCT", envFloat("LIVE_ADD_MAX_DIRECTIONAL_PCT", 6.0)) {
		return "continuation_directional_extension"
	}
	if !(c.ReclaimHold || c.RetestHold || c.ClosedBreakHold || hasFreshStructureReset(c)) {
		return "continuation_requires_reclaim_retest"
	}
	if !continuationStructureConfirmed(c) && !hasFreshStructureReset(c) {
		return "continuation_no_structure_confirm"
	}
	return ""
}

func qualifiesImpulsiveLongStarter(c candidate, fails []string) (float64, []string, bool) {
	if !effectiveImpulsiveLongStarterEnabled() || !strings.EqualFold(c.Side, "BUY") {
		return 0, nil, false
	}
	if c.Entry.LongDemotionFlag || candidateExhaustionActive(c) {
		return 0, nil, false
	}
	allowedFails := 0
	for _, fail := range fails {
		fail = strings.TrimSpace(fail)
		if fail == "" {
			continue
		}
		switch {
		case strings.HasPrefix(fail, "vol_ratio:"),
			fail == "continuation_no_structure_confirm",
			fail == "below_vwap_ema",
			strings.HasPrefix(fail, "ofi_z:"):
			allowedFails++
		default:
			return 0, nil, false
		}
	}
	if allowedFails == 0 {
		return 0, nil, false
	}
	minScore := envFloat("LIVE_IMPULSIVE_LONG_MIN_SCORE", 90.0)
	minSlope := envFloat("LIVE_IMPULSIVE_LONG_MIN_SLOPE", 0.08)
	maxRank := envFloat("LIVE_IMPULSIVE_LONG_MAX_RANK", 2.5)
	minDayUTC := envFloat("LIVE_IMPULSIVE_LONG_MIN_DAYUTC_PCT", 18.0)
	minVolumeUSD := envFloat("LIVE_IMPULSIVE_LONG_MIN_VOL_USD", 8000000.0)
	minVolRatioFallback := envFloat("LIVE_IMPULSIVE_LONG_MIN_VOL_RATIO_FALLBACK", 0.55)
	minAbsOFI := envFloat("LIVE_IMPULSIVE_LONG_MIN_ABS_OFI_Z", 0.05)
	maxStateMin := envFloat("LIVE_IMPULSIVE_LONG_MAX_STATE_MIN", 40.0)
	minConf := envFloat("LIVE_IMPULSIVE_LONG_BASE_CONF", 0.57)
	stateOK := c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay || c.Entry.State == inplay.StatePumping || c.Entry.State == inplay.StateCooling
	if !stateOK || c.Entry.TimeInStateMin > maxStateMin {
		return 0, nil, false
	}
	if c.Entry.CurrentScore < minScore || c.Entry.ScoreSlope < minSlope {
		return 0, nil, false
	}
	if maxRank > 0 && c.Entry.Rank > maxRank {
		return 0, nil, false
	}
	if minDayUTC > 0 && c.DayUTC24h < minDayUTC {
		return 0, nil, false
	}
	volumeOK := c.VolumeUSD >= minVolumeUSD || c.VolumeRatio >= minVolRatioFallback
	if !volumeOK {
		return 0, nil, false
	}
	structureOK := starterDirectionalContextOK(c) || starterStructureContextOK(c)
	if !structureOK {
		return 0, nil, false
	}
	if c.OFISamples >= envInt("LIVE_OFI_MIN_SAMPLES", 8) && c.OFIZ < -envFloat("LIVE_IMPULSIVE_LONG_MAX_CONTRARY_OFI_Z", minAbsOFI) {
		return 0, nil, false
	}
	styleOK := c.Entry.Momentum ||
		c.Entry.EntryStyle == "pullback_long" ||
		c.Entry.EntryStyle == "breakout_hold_long" ||
		c.Entry.EntryStyle == "momentum_ignite_long"
	if !styleOK {
		return 0, nil, false
	}
	confBoost := min(0.16,
		maxFloat(0.0, c.Entry.ScoreSlope-minSlope)*0.30+
			maxFloat(0.0, c.DayUTC24h-minDayUTC)*0.003+
			maxFloat(0.0, c.VolumeRatio-minVolRatioFallback)*0.06)
	conf := clamp(minConf+confBoost, 0.45, 0.76)
	reasons := []string{
		fmt.Sprintf("score=%.2f", c.Entry.CurrentScore),
		fmt.Sprintf("slope=%.3f", c.Entry.ScoreSlope),
		fmt.Sprintf("dayutc=%.1f", c.DayUTC24h),
		fmt.Sprintf("vol_usd=%.2fM", c.VolumeUSD/1_000_000.0),
		fmt.Sprintf("rank=%.2f", c.Entry.Rank),
		"impulsive_long_scanner_move",
	}
	return conf, reasons, true
}

func qualifiesImpulsiveShortStarter(c candidate, fails []string) (float64, []string, bool) {
	if !effectiveImpulsiveShortStarterEnabled() || !strings.EqualFold(c.Side, "SELL") {
		return 0, nil, false
	}
	if c.Entry.ShortDemotionFlag || candidateExhaustionActive(c) {
		return 0, nil, false
	}
	allowedFails := 0
	for _, fail := range fails {
		fail = strings.TrimSpace(fail)
		if fail == "" {
			continue
		}
		switch {
		case strings.HasPrefix(fail, "vol_ratio:"),
			fail == "continuation_no_structure_confirm",
			fail == "above_vwap_ema",
			strings.HasPrefix(fail, "ofi_z:"):
			allowedFails++
		default:
			return 0, nil, false
		}
	}
	if allowedFails == 0 {
		return 0, nil, false
	}
	minScore := envFloat("LIVE_IMPULSIVE_SHORT_MIN_SCORE", 90.0)
	minSlope := envFloat("LIVE_IMPULSIVE_SHORT_MIN_SLOPE", 0.08)
	maxRank := envFloat("LIVE_IMPULSIVE_SHORT_MAX_RANK", 2.5)
	minDayUTC := envFloat("LIVE_IMPULSIVE_SHORT_MIN_DAYUTC_PCT", 18.0)
	minVolumeUSD := envFloat("LIVE_IMPULSIVE_SHORT_MIN_VOL_USD", 8000000.0)
	minVolRatioFallback := envFloat("LIVE_IMPULSIVE_SHORT_MIN_VOL_RATIO_FALLBACK", 0.55)
	minAbsOFI := envFloat("LIVE_IMPULSIVE_SHORT_MIN_ABS_OFI_Z", 0.05)
	maxStateMin := envFloat("LIVE_IMPULSIVE_SHORT_MAX_STATE_MIN", 40.0)
	minConf := envFloat("LIVE_IMPULSIVE_SHORT_BASE_CONF", 0.57)
	stateOK := c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay || c.Entry.State == inplay.StatePumping || c.Entry.State == inplay.StateCooling
	if !stateOK || c.Entry.TimeInStateMin > maxStateMin {
		return 0, nil, false
	}
	if c.Entry.CurrentScore < minScore || c.Entry.ScoreSlope < minSlope {
		return 0, nil, false
	}
	if maxRank > 0 && c.Entry.Rank > maxRank {
		return 0, nil, false
	}
	if minDayUTC > 0 && c.DayUTC24h > -minDayUTC {
		return 0, nil, false
	}
	volumeOK := c.VolumeUSD >= minVolumeUSD || c.VolumeRatio >= minVolRatioFallback
	if !volumeOK {
		return 0, nil, false
	}
	structureOK := starterDirectionalContextOK(c) || starterStructureContextOK(c)
	if !structureOK {
		return 0, nil, false
	}
	if c.OFISamples >= envInt("LIVE_OFI_MIN_SAMPLES", 8) && c.OFIZ > envFloat("LIVE_IMPULSIVE_SHORT_MAX_CONTRARY_OFI_Z", minAbsOFI) {
		return 0, nil, false
	}
	styleOK := c.Entry.Momentum ||
		c.Entry.EntryStyle == "pullback_short" ||
		c.Entry.EntryStyle == "breakout_hold_short" ||
		c.Entry.EntryStyle == "momentum_ignite_short" ||
		c.Entry.EntryStyle == "leader_unwind_short"
	if !styleOK {
		return 0, nil, false
	}
	confBoost := min(0.16,
		maxFloat(0.0, c.Entry.ScoreSlope-minSlope)*0.30+
			maxFloat(0.0, math.Abs(c.DayUTC24h)-minDayUTC)*0.003+
			maxFloat(0.0, c.VolumeRatio-minVolRatioFallback)*0.06)
	conf := clamp(minConf+confBoost, 0.45, 0.76)
	reasons := []string{
		fmt.Sprintf("score=%.2f", c.Entry.CurrentScore),
		fmt.Sprintf("slope=%.3f", c.Entry.ScoreSlope),
		fmt.Sprintf("dayutc=%.1f", c.DayUTC24h),
		fmt.Sprintf("vol_usd=%.2fM", c.VolumeUSD/1_000_000.0),
		fmt.Sprintf("rank=%.2f", c.Entry.Rank),
		"impulsive_short_scanner_move",
	}
	return conf, reasons, true
}

func minQualityForStrategy(c candidate, cfg entryQualityConfig) float64 {
	switch strategyFamily(c) {
	case "ignite":
		if cfg.MinQualityIgnite > 0 {
			return cfg.MinQualityIgnite
		}
	case "rev":
		if cfg.MinQualityRev > 0 {
			return cfg.MinQualityRev
		}
	default:
		if cfg.MinQualityCont > 0 {
			return cfg.MinQualityCont
		}
	}
	return cfg.MinQuality
}

func minEntryConfForStrategy(c candidate, cfg entryQualityConfig) float64 {
	switch strategyFamily(c) {
	case "ignite":
		if cfg.MinEntryConfIgnite > 0 {
			return cfg.MinEntryConfIgnite
		}
	case "rev":
		if cfg.MinEntryConfRev > 0 {
			return cfg.MinEntryConfRev
		}
	default:
		if cfg.MinEntryConfCont > 0 {
			return cfg.MinEntryConfCont
		}
	}
	return cfg.MinEntryConf
}

func computeTradeQuality(c candidate, cfg entryQualityConfig) (float64, []string) {
	discoveryN, triggerN, executionN, total, reasons := computeEntryScoreBreakdown(c, cfg)
	c.DiscoveryScore = discoveryN
	c.TriggerScore = triggerN
	c.ExecutionScore = executionN
	c.CombinedScore = total
	return total, reasons
}

func applyWallSignal(c candidate, ws wallSignal) candidate {
	if ws.Price <= 0 {
		return c
	}
	c.WallMode = ws.Mode
	c.WallStatus = ws.Status
	c.WallConfidence = ws.Confidence
	c.WallBiasScore = ws.BiasScore
	c.WallSpoofRisk = ws.SpoofRisk
	c.WallDistanceBps = ws.DistanceBps
	c.WallSizeRatio = ws.SizeRatio
	c.WallPersistence = ws.Persistence
	c.WallPullRate = ws.PullRate
	c.WallAddRate = ws.AddRate
	c.WallRefillCount = ws.RefillCount
	c.WallPrice = ws.Price
	c.WallSide = ws.Side
	c.WallReasons = append([]string(nil), ws.Reasons...)
	return c
}

func computeEntryScoreBreakdown(c candidate, cfg entryQualityConfig) (float64, float64, float64, float64, []string) {
	reasons := make([]string, 0, 8)
	scoreN := clamp(c.Entry.CurrentScore/100.0, 0, 1)
	rankN := clamp(c.Entry.Rank/120.0, 0, 1)
	volN := clamp(c.VolumeRatio/1.8, 0, 1)
	gradeN := clamp(float64(gradeValue(c.Entry.CurrentGrade))/6.0, 0, 1)
	freshnessN := clamp(1.0-c.Entry.TimeInStateMin/35.0, 0, 1)
	dayUTCN := directionalDayUTCScore(c.Side, c.DayUTC24h, cfg.DayUTCMinAbsPct, cfg.DayUTCScalePct)
	dayUTCWeight := effectiveDayUTCWeight(time.Now(), cfg.DayUTCWeight)
	baseDiscoveryWeight := clamp(1.0-dayUTCWeight, 0, 1)
	discoveryBase := 0.40*scoreN + 0.20*rankN + 0.20*volN + 0.20*gradeN
	discoveryN := clamp(baseDiscoveryWeight*discoveryBase+dayUTCWeight*dayUTCN, 0, 1)

	slopeN := clamp((c.Entry.ScoreSlope+0.15)/0.50, 0, 1)
	confN := clamp(c.Conf, 0, 1)
	structureN := scoreStructureAlignment(c)
	stageN := lifecycleStageScore(c.LifecycleStage)
	triggerStageN := triggerStageScore(c.TriggerStage)
	triggerStateN := c.TriggerStateN
	if triggerStateN <= 0 {
		_, triggerStateN, _ = deriveTriggerState(c)
	}
	triggerN := clamp(0.22*slopeN+0.18*confN+0.18*structureN+0.12*stageN+0.12*triggerStageN+0.08*freshnessN+0.10*triggerStateN, 0, 1)
	if c.WallConfidence > 0 {
		triggerN = clamp(triggerN+c.WallConfidence*0.12+c.WallBiasScore*0.08-c.WallSpoofRisk*0.18, 0, 1)
	}
	if c.PatternBias != 0 {
		triggerN = clamp(triggerN+c.PatternBias, 0, 1)
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Strat)), "reset_impulse_") {
		triggerN = maxFloat(triggerN, envFloat("LIVE_RESET_IMPULSE_TRIGGER_FLOOR", 0.82))
	}

	executionN := scoreExecutionFit(c)
	if c.WallConfidence > 0 {
		executionN = clamp(executionN+c.WallConfidence*0.10-c.WallSpoofRisk*0.15, 0, 1)
	}

	dw, tw, ew := normalizedEntryScoreWeights(cfg)
	total := clamp(dw*discoveryN+tw*triggerN+ew*executionN, 0, 1)

	if cfg.RequireStrategyMatch && (strings.TrimSpace(c.Strat) == "" || strings.EqualFold(c.Strat, "none")) {
		reasons = append(reasons, "strategy_none")
	}
	minConf := minEntryConfForStrategy(c, cfg)
	if confN < minConf {
		reasons = append(reasons, "low_conf")
	}
	if c.TriggerStage != "" && c.TriggerStage != "READY" {
		reasons = append(reasons, "trigger_not_persistent")
	}
	if c.TriggerState != "" && c.TriggerState != string(triggerNone) {
		reasons = append(reasons, "trigger_"+strings.ToLower(c.TriggerState))
	}
	if c.WallMode != "" {
		reasons = append(reasons, "wall_"+strings.ToLower(c.WallMode))
	}
	if c.WallSpoofRisk >= envFloat("LIVE_WALL_SPOOF_RISK_REJECT", 0.75) {
		reasons = append(reasons, "wall_spoof_risk")
	}
	if cfg.EnableScoreGate {
		if discoveryN < cfg.MinDiscovery {
			reasons = append(reasons, fmt.Sprintf("discovery_score:%.2f<%.2f", discoveryN, cfg.MinDiscovery))
		}
		if triggerN < cfg.MinTrigger {
			reasons = append(reasons, fmt.Sprintf("trigger_score:%.2f<%.2f", triggerN, cfg.MinTrigger))
		}
		if executionN < cfg.MinExecution {
			reasons = append(reasons, fmt.Sprintf("execution_score:%.2f<%.2f", executionN, cfg.MinExecution))
		}
	}
	return discoveryN, triggerN, executionN, total, reasons
}

func directionalDayUTCScore(side string, dayUTC, minAbsPct, scalePct float64) float64 {
	if minAbsPct < 0 {
		minAbsPct = -minAbsPct
	}
	if scalePct <= 0 {
		scalePct = 20.0
	}
	if scalePct < minAbsPct {
		scalePct = minAbsPct
	}
	normalize := func(move float64) float64 {
		if move <= minAbsPct {
			return 0
		}
		if scalePct == minAbsPct {
			return 1
		}
		return clamp((move-minAbsPct)/(scalePct-minAbsPct), 0, 1)
	}
	if strings.EqualFold(side, "BUY") {
		return normalize(dayUTC)
	}
	if strings.EqualFold(side, "SELL") {
		return normalize(-dayUTC)
	}
	return normalize(math.Abs(dayUTC))
}

func dayUTCResetAnchor(now time.Time) time.Time {
	locName := envStr("LIVE_DAYUTC_RESET_TZ", "America/Chicago")
	loc, err := time.LoadLocation(locName)
	if err != nil {
		loc = time.Local
	}
	localNow := now.In(loc)
	hour := envInt("LIVE_DAYUTC_RESET_HOUR", 19)
	minute := envInt("LIVE_DAYUTC_RESET_MIN", 0)
	anchor := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	if localNow.Before(anchor) {
		anchor = anchor.Add(-24 * time.Hour)
	}
	return anchor
}

func dayUTCResetProgress(now time.Time) float64 {
	rampMin := envFloat("LIVE_DAYUTC_RESET_RAMP_MIN", 90.0)
	if rampMin <= 0 {
		return 1
	}
	floor := envFloat("LIVE_DAYUTC_RESET_WEIGHT_FLOOR", 0.35)
	if floor < 0 {
		floor = 0
	}
	if floor > 1 {
		floor = 1
	}
	ageMin := now.Sub(dayUTCResetAnchor(now)).Minutes()
	if ageMin <= 0 {
		return floor
	}
	progress := clamp(ageMin/rampMin, 0, 1)
	return clamp(floor+(1.0-floor)*progress, 0, 1)
}

func effectiveDayUTCWeight(now time.Time, baseWeight float64) float64 {
	return clamp(baseWeight*dayUTCResetProgress(now), 0, 1)
}

func dayUTCResetKey(now time.Time) string {
	return dayUTCResetAnchor(now).Format(time.RFC3339)
}

func minutesSinceDayUTCReset(now time.Time) float64 {
	return maxFloat(0, now.Sub(dayUTCResetAnchor(now)).Minutes())
}

func resetImpulseWindowActive(now time.Time) bool {
	windowMin := envFloat("LIVE_RESET_IMPULSE_WINDOW_MIN", 15.0)
	if windowMin <= 0 {
		return false
	}
	return minutesSinceDayUTCReset(now) <= windowMin
}

func canonicalSymbolBase(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	s = strings.TrimSuffix(s, "USDT")
	s = strings.TrimSuffix(s, "-USD")
	s = strings.TrimSuffix(s, "USD")
	return s
}

func positionLookupKey(symbol, side string) string {
	return canonicalSymbolBase(symbol) + "|" + normalizePositionSide(side)
}

func qualifiesResetImpulse(c candidate, now time.Time) (string, float64, []string) {
	if !effectiveResetImpulseEnabled() || !resetImpulseWindowActive(now) {
		return "", 0, nil
	}
	side := strings.ToUpper(strings.TrimSpace(c.Side))
	if side != "BUY" && side != "SELL" {
		return "", 0, nil
	}
	stateOK := c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay || c.Entry.State == inplay.StatePumping
	if !stateOK || c.Entry.TimeInStateMin > envFloat("LIVE_RESET_IMPULSE_MAX_STATE_MIN", 25.0) {
		return "", 0, nil
	}
	if c.Entry.CurrentScore < envFloat("LIVE_RESET_IMPULSE_MIN_SCORE", 72.0) {
		return "", 0, nil
	}
	minSlope := envFloat("LIVE_RESET_IMPULSE_MIN_SLOPE", 0.08)
	minDayUTC := envFloat("LIVE_RESET_IMPULSE_MIN_DAYUTC_PCT", 8.0)
	if side == "BUY" {
		if c.Entry.ScoreSlope < minSlope || c.DayUTC24h < minDayUTC {
			return "", 0, nil
		}
		if c.SessionVWAP > 0 && c.LastClose < c.SessionVWAP {
			return "", 0, nil
		}
		if c.EMA9 > 0 && c.LastClose < c.EMA9 {
			return "", 0, nil
		}
	} else {
		if c.Entry.ScoreSlope > -minSlope || c.DayUTC24h > -minDayUTC {
			return "", 0, nil
		}
		if c.SessionVWAP > 0 && c.LastClose > c.SessionVWAP {
			return "", 0, nil
		}
		if c.EMA9 > 0 && c.LastClose > c.EMA9 {
			return "", 0, nil
		}
	}
	if c.VolumeRatio < envFloat("LIVE_RESET_IMPULSE_MIN_VOL_RATIO", 1.20) {
		return "", 0, nil
	}
	switch c.TriggerState {
	case string(triggerImpulseCont), string(triggerOFReclaim), string(triggerStackedBid), string(triggerStackedAsk):
	default:
		return "", 0, nil
	}
	if c.TriggerStage == "INVALIDATED" {
		return "", 0, nil
	}
	progress := dayUTCResetProgress(now)
	confBase := envFloat("LIVE_RESET_IMPULSE_BASE_CONF", 0.62)
	confBoost := min(0.20,
		maxFloat(0.0, math.Abs(c.DayUTC24h)-minDayUTC)*0.008+
			maxFloat(0.0, c.VolumeRatio-envFloat("LIVE_RESET_IMPULSE_MIN_VOL_RATIO", 1.20))*0.08+
			maxFloat(0.0, math.Abs(c.Entry.ScoreSlope)-minSlope)*0.40)
	conf := clamp(confBase+confBoost+(1.0-progress)*envFloat("LIVE_RESET_IMPULSE_EARLY_BONUS", 0.08), 0, 0.90)
	strat := "reset_impulse_long"
	if side == "SELL" {
		strat = "reset_impulse_short"
	}
	reasons := []string{
		fmt.Sprintf("reset_age_min=%.1f", minutesSinceDayUTCReset(now)),
		fmt.Sprintf("dayutc=%+.2f", c.DayUTC24h),
		fmt.Sprintf("vol_ratio=%.2f", c.VolumeRatio),
		fmt.Sprintf("trigger_state=%s", c.TriggerState),
	}
	return strat, conf, reasons
}

func classifySetupFamily(c candidate, now time.Time) string {
	if bp, ok := resolveSetupBlueprint(c, now); ok && strings.TrimSpace(bp.SetupFamily) != "" {
		return bp.SetupFamily
	}
	strat := strings.ToLower(strings.TrimSpace(c.Strat))
	style := strings.ToLower(strings.TrimSpace(c.Entry.EntryStyle))
	if strings.HasPrefix(strat, "reset_impulse_") || (resetImpulseWindowActive(now) && c.TriggerState == string(triggerImpulseCont) && c.ClosedBreakHold) {
		return "reset_impulse_breakout"
	}
	if strategyFamily(c) == "rev" || strings.Contains(strat, "reversal") || strings.Contains(strat, "flip") {
		return "reversal_exhaustion"
	}
	if c.RetestHold || c.ResetRebreak || strings.Contains(style, "breakout_hold") {
		return "breakout_retest"
	}
	if c.ReclaimHold && c.ExtensionATR >= envFloat("LIVE_DEEP_PULLBACK_EXTENSION_ATR", 1.35) {
		return "deep_pullback_reclaim"
	}
	if c.ReclaimHold || c.ClosedBreakHold || strings.Contains(style, "pullback") || c.TriggerState == string(triggerOFReclaim) {
		return "micro_pullback_continuation"
	}
	return ""
}

func ensureCandidateSetupFamily(c *candidate, now time.Time) {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.SetupFamily) == "" {
		c.SetupFamily = classifySetupFamily(*c, now)
	}
	annotateCandidateSetupBlueprint(c, now)
}

func withUnresolvedSource(reason, source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return strings.TrimSpace(reason)
	}
	tag := unresolvedSourceTagPrefix + source
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return tag
	case strings.Contains(reason, tag):
		return reason
	default:
		return reason + "|" + tag
	}
}

func unresolvedSourceFromReason(reason string) string {
	for _, part := range strings.Split(strings.TrimSpace(reason), "|") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), unresolvedSourceTagPrefix) {
			return strings.TrimPrefix(strings.ToLower(part), unresolvedSourceTagPrefix)
		}
	}
	return ""
}

func unresolvedStrategySource(c candidate) string {
	if isExecutableStrategy(c.Strat) {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(c.Strat)) {
	case "none":
		return "explicit_none"
	case "no_strategy":
		return "explicit_no_strategy"
	case "unknown":
		return "explicit_unknown"
	case "unresolved":
		return "explicit_unresolved"
	}
	if source := unresolvedSourceFromReason(c.RejectReason); source != "" {
		return source
	}
	if strings.TrimSpace(c.SetupFamily) != "" {
		return "setup_family_unmapped"
	}
	if strings.TrimSpace(c.Entry.EntryStyle) != "" {
		return "continuation_fallback_unmapped"
	}
	return "blank_strategy"
}

func resolveExecutableStrategyForProfile(c candidate) string {
	current := strings.ToLower(strings.TrimSpace(c.Strat))
	if isExecutableStrategy(current) && !isStarterOnlyStrategyName(current) {
		return current
	}
	setup := strings.ToLower(strings.TrimSpace(c.SetupFamily))
	if resolveRuntimeProfileConfig().Name == runtimeProfilePaperContinuationClean {
		if !isExecutableStrategy(current) || isStarterOnlyStrategyName(current) {
			switch setup {
			case "micro_pullback_continuation", "breakout_retest", "deep_pullback_reclaim":
				return setup
			}
		}
		if isStarterOnlyStrategyName(current) {
			return ""
		}
	}
	if isStarterOnlyStrategyName(current) {
		return current
	}
	return ""
}

func finalizeCandidateExecutionLabels(c *candidate, now time.Time) {
	if c == nil {
		return
	}
	ensureCandidateSetupFamily(c, now)
	if resolved := strings.TrimSpace(resolveExecutableStrategyForProfile(*c)); resolved != "" {
		c.Strat = resolved
		return
	}
	if resolveRuntimeProfileConfig().Name == runtimeProfilePaperContinuationClean && isStarterOnlyStrategyName(c.Strat) {
		c.Strat = ""
	}
}

func continuationDeteriorating(c candidate) bool {
	if strings.EqualFold(c.Side, "BUY") {
		lostTrend := (c.SessionVWAP > 0 && c.LastClose < c.SessionVWAP) || (c.EMA9 > 0 && c.LastClose < c.EMA9)
		weakSlope := c.Entry.ScoreSlope <= envFloat("LIVE_CONT_DETERIORATE_MIN_SLOPE", 0.01)
		return c.TriggerState == string(triggerFailReclaim) || (c.TriggerState == string(triggerExhaustion) && lostTrend && weakSlope)
	}
	lostTrend := (c.SessionVWAP > 0 && c.LastClose > c.SessionVWAP) || (c.EMA9 > 0 && c.LastClose > c.EMA9)
	weakSlope := c.Entry.ScoreSlope >= -envFloat("LIVE_CONT_DETERIORATE_MIN_SLOPE", 0.01)
	return c.TriggerState == string(triggerFailReclaim) || (c.TriggerState == string(triggerExhaustion) && lostTrend && weakSlope)
}

func applyPatternModifiers(cand *candidate, bars []features.Candle) {
	if cand == nil || !envBool("LIVE_PATTERN_CONFIRM_ENABLE", true) || len(bars) < 2 {
		return
	}
	typed := make([]types.Candle, 0, len(bars))
	for _, b := range bars {
		typed = append(typed, types.Candle{T: b.Ts, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V})
	}
	pats := ta.DetectPatterns(typed)
	if len(pats) == 0 {
		return
	}
	lastIdx := len(typed) - 1
	reclaimProxPct := envFloat("LIVE_PATTERN_RECLAIM_PROX_PCT", 0.35)
	nearReclaim := cand.ReclaimHold || cand.RetestHold ||
		(cand.SessionVWAP > 0 && math.Abs(relativePct(cand.LastClose, cand.SessionVWAP)) <= reclaimProxPct) ||
		(cand.EMA9 > 0 && math.Abs(relativePct(cand.LastClose, cand.EMA9)) <= reclaimProxPct)
	extended := cand.ExtensionATR >= envFloat("LIVE_PATTERN_EXTENDED_ATR", 1.40)
	bias := 0.0
	reasons := make([]string, 0, 4)
	for _, p := range pats {
		if p.Index < lastIdx-1 {
			continue
		}
		strength := clamp(p.Strength, 0, 1)
		switch strings.ToUpper(strings.TrimSpace(cand.Side)) {
		case "BUY":
			switch p.Name {
			case ta.PatBullEngulf, ta.PatHammer, ta.PatPiercing, ta.PatMarubozuBull:
				if nearReclaim || cand.ClosedBreakHold {
					bias += 0.08 * strength
					reasons = append(reasons, "pattern_bull_confirm:"+string(p.Name))
				}
			case ta.PatDoji, ta.PatSpinningTop:
				if extended {
					bias -= 0.05 * strength
					reasons = append(reasons, "pattern_indecision:"+string(p.Name))
				}
			case ta.PatBearEngulf, ta.PatShootingStar, ta.PatDarkCloud, ta.PatMarubozuBear:
				if extended || cand.TriggerState == string(triggerExhaustion) {
					bias -= 0.08 * strength
					reasons = append(reasons, "pattern_bear_warn:"+string(p.Name))
				}
			}
		case "SELL":
			switch p.Name {
			case ta.PatBearEngulf, ta.PatShootingStar, ta.PatDarkCloud, ta.PatMarubozuBear:
				if nearReclaim || cand.ClosedBreakHold {
					bias += 0.08 * strength
					reasons = append(reasons, "pattern_bear_confirm:"+string(p.Name))
				}
			case ta.PatDoji, ta.PatSpinningTop:
				if extended {
					bias -= 0.05 * strength
					reasons = append(reasons, "pattern_indecision:"+string(p.Name))
				}
			case ta.PatBullEngulf, ta.PatHammer, ta.PatPiercing, ta.PatMarubozuBull:
				if extended || cand.TriggerState == string(triggerExhaustion) {
					bias -= 0.08 * strength
					reasons = append(reasons, "pattern_bull_warn:"+string(p.Name))
				}
			}
		}
	}
	cand.PatternBias = clamp(bias, -0.14, 0.14)
	cand.PatternReasons = reasons
}

func isContinuationStrategy(c candidate) bool {
	switch strategyFamily(c) {
	case "cont", "ignite":
		return true
	default:
		return false
	}
}

func requiresFreshPullback(c candidate) bool {
	switch c.TriggerState {
	case string(triggerOFReclaim), string(triggerStackedBid), string(triggerStackedAsk):
		return true
	default:
		return false
	}
}

func continuationGuardReason(c candidate, cfg entryQualityConfig) string {
	if !isContinuationStrategy(c) {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Strat)), "reset_impulse_") {
		return ""
	}
	if cfg.BlockContExhaustion && c.TriggerState == string(triggerExhaustion) && continuationDeteriorating(c) {
		return "continuation_exhausted"
	}
	if !cfg.DayUTCMaturityBrake {
		return ""
	}
	if math.Abs(c.DayUTC24h) < cfg.DayUTCMaturityPct {
		if effectiveContinuationStructureConfirmationRequired() && !continuationStructureConfirmed(c) && c.SetupFamily == "" {
			return "continuation_no_structure_confirm"
		}
		return ""
	}
	if !hasFreshStructureReset(c) {
		if c.SetupFamily == "micro_pullback_continuation" || c.SetupFamily == "breakout_retest" {
			leaderScoreMin := envFloat("LIVE_LATE_ENTRY_LEADER_SCORE_MIN", 96.0)
			leaderSlopeMin := envFloat("LIVE_LATE_ENTRY_LEADER_SLOPE_MIN", 0.14)
			leaderRankMax := envFloat("LIVE_LATE_ENTRY_LEADER_RANK_MAX", 1.5)
			if c.Entry.CurrentScore >= leaderScoreMin &&
				math.Abs(c.Entry.ScoreSlope) >= leaderSlopeMin &&
				c.Entry.Rank <= leaderRankMax &&
				(c.Entry.State == inplay.StateHeating || c.Entry.State == inplay.StateInPlay || c.Entry.State == inplay.StatePumping) {
				return ""
			}
		}
		if math.Abs(c.DayUTC24h) >= envFloat("LIVE_LATE_ENTRY_DAYUTC_BRAKE_PCT", cfg.DayUTCMaturityPct) {
			return "late_extension_no_reset"
		}
	}
	if cfg.RequireFreshPullback && !requiresFreshPullback(c) && !continuationStructureConfirmed(c) && c.SetupFamily == "" {
		return "extended_reentry_lock"
	}
	if strings.EqualFold(c.Side, "SELL") && c.DayUTC24h <= -envFloat("LIVE_LATE_ENTRY_DAYUTC_BRAKE_PCT", cfg.DayUTCMaturityPct) {
		lateMinSlope := envFloat("LIVE_CONT_FAST_LATE_MIN_SLOPE", 0.16)
		if c.Entry.ScoreSlope < lateMinSlope && !hasFreshStructureReset(c) {
			return "late_cycle_short_weak_slope"
		}
	}
	return ""
}

func directionallyConflicting(c candidate, minAbsPct float64) (bool, float64) {
	if minAbsPct < 0 {
		minAbsPct = -minAbsPct
	}
	move := c.DayUTC24h
	switch strings.ToUpper(strings.TrimSpace(c.Side)) {
	case "BUY":
		if move <= -minAbsPct {
			return true, math.Abs(move)
		}
	case "SELL":
		if move >= minAbsPct {
			return true, math.Abs(move)
		}
	}
	return false, math.Abs(move)
}

func directionalConflictRejectReason(c candidate) string {
	if !envBool("LIVE_DIRECTIONAL_CONFLICT_BLOCK_ENABLE", true) {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Strat)), "reset_impulse_") {
		return ""
	}
	if strategyFamily(c) == "rev" {
		return ""
	}
	blockPct := envFloat("LIVE_DIRECTIONAL_CONFLICT_BLOCK_PCT", 3.0)
	if ok, absMove := directionallyConflicting(c, blockPct); ok {
		if effectiveDirectionalConflictPenaltyOnly() {
			if !effectiveDirectionalConflictExtremeOnly() {
				return ""
			}
			penaltyPct := envFloat("LIVE_PENALTY_DIRECTIONAL_CONFLICT_PCT", blockPct)
			extremePct := maxFloat(blockPct*2.0, penaltyPct+blockPct)
			if absMove >= extremePct {
				return "directional_dayutc_conflict_extreme"
			}
			return ""
		}
		return "directional_dayutc_conflict"
	}
	return ""
}

func sessionChurnKey(symbol, side string) string {
	return strings.ToUpper(aster.RawSymbol(symbol)) + "|" + strings.ToUpper(strings.TrimSpace(side))
}

func currentSessionDayKey(now time.Time) string {
	return now.UTC().Format("2006-01-02")
}

func churnStateFor(mem map[string]*sessionChurn, now time.Time, symbol, side string) *sessionChurn {
	if mem == nil {
		return &sessionChurn{
			DayKey:         currentSessionDayKey(now),
			SetupLossTimes: map[string][]time.Time{},
		}
	}
	key := sessionChurnKey(symbol, side)
	st := mem[key]
	dayKey := currentSessionDayKey(now)
	if st == nil || st.DayKey != dayKey {
		st = &sessionChurn{DayKey: dayKey}
		mem[key] = st
	}
	if st.SetupLossTimes == nil {
		st.SetupLossTimes = map[string][]time.Time{}
	}
	return st
}

func markSessionEntry(mem map[string]*sessionChurn, now time.Time, c candidate) {
	st := churnStateFor(mem, now, c.Entry.Symbol, c.Side)
	st.EntryCount++
	st.LastEntryAt = now
	st.LastStyle = nonEmpty(c.Strat, c.Entry.EntryStyle)
	st.LastDayUTCPct = c.DayUTC24h
}

func trimTimesWithin(times []time.Time, cutoff time.Time) []time.Time {
	if len(times) == 0 {
		return nil
	}
	dst := times[:0]
	for _, ts := range times {
		if !ts.Before(cutoff) {
			dst = append(dst, ts)
		}
	}
	return dst
}

func markSessionStop(mem map[string]*sessionChurn, now time.Time, symbol, side, setup string, holdMin, pnlPct, dayUTCPct float64) {
	st := churnStateFor(mem, now, symbol, side)
	st.StopCount++
	st.LastStopAt = now
	st.LastDayUTCPct = dayUTCPct
	if holdMin <= envFloat("LIVE_CHURN_LOCK_QUICK_LOSS_MAX_HOLD_MIN", 30) || pnlPct <= -envFloat("LIVE_CHURN_LOCK_QUICK_LOSS_MIN_PCT", 2.5) {
		st.QuickLossCount++
	}
	if pnlPct < 0 {
		st.DayLossCount++
		st.LossTimes = append(trimTimesWithin(st.LossTimes, now.Add(-4*time.Hour)), now)
		if key := strings.ToLower(strings.TrimSpace(setup)); key != "" {
			st.SetupLossTimes[key] = append(trimTimesWithin(st.SetupLossTimes[key], now.Add(-4*time.Hour)), now)
		}
	}
}

func churnRejectReason(mem map[string]*sessionChurn, now time.Time, c candidate) string {
	if !envBool("LIVE_CHURN_LOCK_ENABLE", true) {
		return ""
	}
	st := churnStateFor(mem, now, c.Entry.Symbol, c.Side)
	maxFails := envInt("LIVE_CHURN_LOCK_MAX_FAILS_PER_SESSION", 2)
	if maxFails < 1 {
		maxFails = 1
	}
	window := time.Duration(envInt("LIVE_CHURN_LOCK_WINDOW_MIN", 240)) * time.Minute
	if window <= 0 {
		window = 4 * time.Hour
	}
	if st.LastStopAt.IsZero() || now.Sub(st.LastStopAt) > window {
		st.LossTimes = trimTimesWithin(st.LossTimes, now.Add(-4*time.Hour))
		for key, times := range st.SetupLossTimes {
			st.SetupLossTimes[key] = trimTimesWithin(times, now.Add(-4*time.Hour))
		}
	}
	if st.DayLossCount >= envInt("LIVE_SYMBOL_DAY_LOSS_DISABLE_COUNT", 3) {
		return "symbol_day_loss_lock"
	}
	if len(st.LossTimes) >= envInt("LIVE_SYMBOL_TWO_LOSS_WINDOW_COUNT", 2) {
		recentWindow := time.Duration(envInt("LIVE_SYMBOL_TWO_LOSS_WINDOW_MIN", 60)) * time.Minute
		lockFor := time.Duration(envInt("LIVE_SYMBOL_TWO_LOSS_LOCK_MIN", 120)) * time.Minute
		last := st.LossTimes[len(st.LossTimes)-1]
		prev := st.LossTimes[len(st.LossTimes)-2]
		if last.Sub(prev) <= recentWindow && now.Sub(last) <= lockFor {
			return "symbol_loss_cooldown"
		}
	}
	setupKey := strings.ToLower(strings.TrimSpace(firstNonEmpty(c.Strat, c.Entry.EntryStyle)))
	if times := st.SetupLossTimes[setupKey]; len(times) >= envInt("LIVE_SYMBOL_SETUP_LOSS_COUNT", 2) {
		lockFor := time.Duration(envInt("LIVE_SYMBOL_SETUP_LOSS_LOCK_MIN", 240)) * time.Minute
		last := times[len(times)-1]
		prev := times[len(times)-2]
		if !prev.IsZero() && now.Sub(last) <= lockFor {
			return "symbol_setup_loss_lock"
		}
	}
	quickLockMin := time.Duration(envInt("LIVE_SYMBOL_QUICK_LOSS_LOCK_MIN", 60)) * time.Minute
	quickLockCount := envInt("LIVE_SYMBOL_QUICK_LOSS_LOCK_COUNT", 1)
	if quickLockCount < 1 {
		quickLockCount = 1
	}
	quickLossExtremePct := envFloat("LIVE_SYMBOL_QUICK_LOSS_DAYUTC_PCT", 25.0)
	quickLossCountNeeded := maxInt(2, quickLockCount+1)
	if math.Abs(maxFloat(math.Abs(c.DayUTC24h), math.Abs(st.LastDayUTCPct))) >= quickLossExtremePct {
		quickLossCountNeeded = quickLockCount
	}
	if quickLockMin > 0 && now.Sub(st.LastStopAt) <= quickLockMin && st.QuickLossCount >= quickLossCountNeeded && !hasFreshStructureReset(c) {
		return "quick_loss_symbol_lock"
	}
	if st.StopCount >= maxFails || st.QuickLossCount >= maxFails {
		if math.Abs(maxFloat(math.Abs(c.DayUTC24h), math.Abs(st.LastDayUTCPct))) >= envFloat("LIVE_DAYUTC_MATURITY_BRAKE_PCT", 25.0) {
			return "extended_reentry_lock"
		}
		return "symbol_churn_lock"
	}
	return ""
}

func candidateSelectionRank(c candidate) float64 {
	boost := clamp(c.PriorDayLeaderBoost, 0, 1) * envFloat("LIVE_PRIOR_DAY_RANK_BOOST_POINTS", 4.0)
	if c.FinalRank > 0 {
		return c.FinalRank + boost
	}
	if c.CombinedScore > 0 {
		return c.CombinedScore*100.0 + boost
	}
	return c.Entry.Rank + boost
}

func sideDominanceRejectReason(c candidate, ranked []candidate) string {
	if !envBool("LIVE_SIDE_DOMINANCE_ENABLE", true) {
		return ""
	}
	if strategyFamily(c) == "rev" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Strat)), "reset_impulse_") {
		return ""
	}
	minStronger := envInt("LIVE_SIDE_DOMINANCE_MIN_STRONGER", 2)
	if minStronger < 1 {
		minStronger = 1
	}
	minGap := envFloat("LIVE_SIDE_DOMINANCE_MIN_RANK_GAP", 10.0)
	maxScoreAllow := envFloat("LIVE_SIDE_DOMINANCE_MAX_SCORE_ALLOW", 96.0)
	conflictPct := envFloat("LIVE_SIDE_DOMINANCE_CONFLICT_DAYUTC_PCT", 2.5)
	cRank := candidateSelectionRank(c)
	bestOpp := 0.0
	strongerOpp := 0
	for _, other := range ranked {
		if strings.EqualFold(aster.RawSymbol(other.Entry.Symbol), aster.RawSymbol(c.Entry.Symbol)) && strings.EqualFold(other.Side, c.Side) {
			continue
		}
		if strings.EqualFold(other.Side, c.Side) {
			continue
		}
		oRank := candidateSelectionRank(other)
		if oRank <= cRank {
			continue
		}
		strongerOpp++
		if oRank > bestOpp {
			bestOpp = oRank
		}
	}
	if strongerOpp < minStronger || bestOpp-cRank < minGap {
		return ""
	}
	if conflicting, _ := directionallyConflicting(c, conflictPct); conflicting {
		return "side_dominance_block"
	}
	if c.Entry.CurrentScore <= maxScoreAllow {
		return "side_dominance_block"
	}
	return ""
}

func currentEntryBySide(side string, raw string, longCurrent, shortCurrent map[string]inplay.Entry) (inplay.Entry, bool) {
	if strings.EqualFold(side, "BUY") {
		e, ok := longCurrent[raw]
		return e, ok
	}
	e, ok := shortCurrent[raw]
	return e, ok
}

func activeWinnerRejectReason(now time.Time, c candidate, execMgr *liveExecManager, paper *paperTrader, meta map[string]symbolMeta, longCurrent, shortCurrent map[string]inplay.Entry) string {
	if !envBool("LIVE_ACTIVE_WINNER_PRIORITY_ENABLE", true) {
		return ""
	}
	minScore := envFloat("LIVE_ACTIVE_WINNER_MIN_SCORE", 88.0)
	minPnLPct := envFloat("LIVE_ACTIVE_WINNER_MIN_PNL_PCT", 1.0)
	suppressDelta := envFloat("LIVE_ACTIVE_WINNER_SUPPRESS_DELTA", 0.08)
	candidateStrength := clamp(c.Entry.CurrentScore/100.0, 0, 1) + 0.25*clamp(c.Conf, 0, 1)
	bestReason := ""
	bestStrength := 0.0
	checkPos := func(symbol, side, source string, entry, lastMark float64, sponsored bool, refreshAt time.Time) {
		raw := strings.ToUpper(aster.RawSymbol(symbol))
		candidateRaw := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
		if raw == candidateRaw && strings.EqualFold(strings.TrimSpace(side), strings.TrimSpace(c.Side)) {
			return
		}
		px := lastMark
		if m, ok := meta[raw]; ok && m.LastPrice > 0 {
			px = m.LastPrice
		}
		if entry <= 0 || px <= 0 {
			return
		}
		_, pnlPct := realizedFromFill(side, entry, px, 1)
		if pnlPct < minPnLPct {
			return
		}
		e, ok := currentEntryBySide(side, raw, longCurrent, shortCurrent)
		if !ok || e.CurrentScore < minScore {
			return
		}
		if !(sponsored || confluenceRefreshActive(now, refreshAt)) {
			return
		}
		strength := clamp(e.CurrentScore/100.0, 0, 1) + clamp(pnlPct/10.0, 0, 0.30)
		if strength < candidateStrength+suppressDelta || strength <= bestStrength {
			return
		}
		bestStrength = strength
		if strings.TrimSpace(source) != "" && !strings.EqualFold(source, "BOT") {
			bestReason = "manual_position_stronger"
		} else {
			bestReason = "active_winner_stronger"
		}
	}
	if execMgr != nil {
		for _, p := range execMgr.positions {
			if !execMgr.isActive(p) || p.RemainingQty <= 0 {
				continue
			}
			checkPos(p.Symbol, p.Side, p.EntrySource, p.EntryPrice, p.LastMark, p.Sponsored, p.LastConfluenceRefresh)
		}
	}
	if paper != nil && paper.enabled {
		for _, p := range paper.positions {
			if p == nil || p.Qty <= 0 {
				continue
			}
			checkPos(p.Symbol, p.Side, "PAPER", p.Entry, p.LastMark, p.Sponsored, p.LastConfluenceRefresh)
		}
	}
	return bestReason
}

func continuationStateTrending(st inplay.State) bool {
	switch st {
	case inplay.StateHeating, inplay.StateInPlay, inplay.StatePumping:
		return true
	default:
		return false
	}
}

func continuationFastOFIAgrees(c candidate, minOFIZ float64) bool {
	ofiEnabled := envBool("LIVE_ENABLE_OFI", true)
	ofiMinSamples := envInt("LIVE_OFI_MIN_SAMPLES", 8)
	if ofiMinSamples < 1 {
		ofiMinSamples = 1
	}
	if !ofiEnabled || c.OFISamples < ofiMinSamples {
		return true
	}
	if strings.EqualFold(c.Side, "BUY") {
		return c.OFIZ >= minOFIZ
	}
	return c.OFIZ <= -minOFIZ
}

func starterDirectionalContextOK(c candidate) bool {
	if candidatePriceConfirmsDirection(c) {
		return true
	}
	if strings.EqualFold(c.Side, "BUY") {
		return c.ReclaimHold || c.Entry.FailedBreakdownCount > 0 || c.Entry.FailedBreakLowCount > 0 ||
			c.SetupFamily == "deep_pullback_reclaim" || c.Entry.EntryStyle == "pullback_long"
	}
	return c.ReclaimHold || c.Entry.FailedBounceCount > 0 || c.Entry.FailedReclaimCount > 0 ||
		c.Entry.EntryStyle == "pullback_short"
}

func starterStructureContextOK(c candidate) bool {
	if continuationStructureConfirmed(c) || hasFreshStructureReset(c) {
		return true
	}
	if strings.EqualFold(c.Side, "BUY") {
		return c.Entry.FailedBreakdownCount > 0 || c.Entry.FailedBreakLowCount > 0 ||
			c.SetupFamily == "deep_pullback_reclaim" || c.Entry.EntryStyle == "pullback_long"
	}
	return c.Entry.FailedBounceCount > 0 || c.Entry.FailedReclaimCount > 0 ||
		c.Entry.EntryStyle == "pullback_short"
}

func starterOFIAgrees(c candidate, minOFIZ float64) bool {
	ofiEnabled := envBool("LIVE_ENABLE_OFI", true)
	ofiMinSamples := maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8))
	if !ofiEnabled || c.OFISamples < ofiMinSamples {
		return true
	}
	if strings.EqualFold(c.Side, "BUY") {
		return c.OFIZ >= minOFIZ
	}
	return c.OFIZ <= -minOFIZ
}

func starterOFIToleratesEliteNoise(c candidate, tolerance float64) bool {
	ofiEnabled := envBool("LIVE_ENABLE_OFI", true)
	ofiMinSamples := maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8))
	if !ofiEnabled || c.OFISamples < ofiMinSamples {
		return true
	}
	if strings.EqualFold(c.Side, "BUY") {
		return c.OFIZ >= -tolerance
	}
	return c.OFIZ <= tolerance
}

func qualifiesEliteStarterCandidate(c candidate) bool {
	if gradeValue(c.Entry.CurrentGrade) < gradeValue("A") {
		return false
	}
	if c.CombinedScore < envFloat("LIVE_STARTER_FINAL_RANK_MIN", 0.72) {
		return false
	}
	if !continuationStateTrending(c.Entry.State) {
		return false
	}
	fastMinScore := envFloat("LIVE_CONT_FAST_MIN_SCORE", 65.0)
	fastMinSlope := envFloat("LIVE_CONT_FAST_MIN_SLOPE", 0.02)
	fastMinOFIZ := envFloat("LIVE_CONT_FAST_MIN_OFI_Z", 0.35)
	if c.Entry.CurrentScore < fastMinScore || c.Entry.ScoreSlope < fastMinSlope {
		return false
	}
	if candidateExhaustionActive(c) {
		return false
	}
	eliteOFITolerance := envFloat("LIVE_ELITE_STARTER_OFI_TOLERANCE_Z", 0.10)
	if eliteOFITolerance <= 0 {
		eliteOFITolerance = min(0.10, fastMinOFIZ)
	}
	if c.Entry.Rank <= envFloat("LIVE_ELITE_STARTER_MAX_RANK", 2.0) && c.Entry.CurrentScore >= envFloat("LIVE_ELITE_STARTER_MIN_SCORE", 95.0) {
		return starterOFIToleratesEliteNoise(c, eliteOFITolerance)
	}
	return continuationFastOFIAgrees(c, fastMinOFIZ)
}

func classifyExhaustionRisk(c candidate, side string) string {
	if candidateExhaustionActive(c) || candidateSpikeCandle(c) {
		return "high"
	}
	movePct := candidateDirectionalMovePct(c)
	highMovePct := envFloat("LIVE_ELITE_STARTER_HIGH_EXHAUST_DAYUTC_PCT", 25.0)
	moderateMovePct := envFloat("LIVE_ELITE_STARTER_MODERATE_EXHAUST_DAYUTC_PCT", 18.0)
	highExtATR := envFloat("LIVE_ELITE_STARTER_HIGH_EXHAUST_ATR", 1.60)
	moderateExtATR := envFloat("LIVE_ELITE_STARTER_MODERATE_EXHAUST_ATR", 1.20)
	if c.ExtensionATR >= highExtATR || movePct >= highMovePct {
		return "high"
	}
	if c.Entry.TimeInStateMin >= envFloat("LIVE_ELITE_STARTER_HIGH_EXHAUST_STATE_MIN", 35.0) && c.Entry.ScoreSlope <= envFloat("LIVE_ELITE_STARTER_HIGH_EXHAUST_MAX_SLOPE", 0.02) {
		return "high"
	}
	if c.ExtensionATR >= moderateExtATR || movePct >= moderateMovePct {
		return "moderate"
	}
	if c.Entry.TimeInStateMin >= envFloat("LIVE_ELITE_STARTER_MODERATE_EXHAUST_STATE_MIN", 20.0) && c.Entry.ScoreSlope <= envFloat("LIVE_ELITE_STARTER_MODERATE_EXHAUST_MAX_SLOPE", 0.04) {
		return "moderate"
	}
	return "low"
}

func classifyStarterLane(c candidate, side string) string {
	if strings.TrimSpace(side) == "" {
		side = c.Side
	}
	if directionalConflictRejectReason(c) != "" {
		return "reject"
	}
	if !continuationStateTrending(c.Entry.State) {
		return "reject"
	}
	switch classifyExhaustionRisk(c, side) {
	case "high":
		return "reject"
	case "moderate":
		if !qualifiesEliteStarterCandidate(c) {
			return "extended_wait_reset"
		}
	}
	if qualifiesEliteStarterCandidate(c) {
		minEliteScore := envFloat("LIVE_ELITE_STARTER_MIN_SCORE", 92.0)
		minEliteSlope := envFloat("LIVE_ELITE_STARTER_MIN_SLOPE", 0.05)
		minEliteVol := envFloat("LIVE_ELITE_STARTER_MIN_VOL_RATIO", 0.95)
		if c.Entry.Rank <= envFloat("LIVE_ELITE_STARTER_MAX_RANK", 2.0) &&
			gradeValue(c.Entry.CurrentGrade) >= gradeValue("A") &&
			c.Entry.CurrentScore >= minEliteScore &&
			c.Entry.ScoreSlope >= minEliteSlope &&
			c.VolumeRatio >= minEliteVol &&
			starterDirectionalContextOK(c) &&
			starterStructureContextOK(c) {
			return "continuation_fast"
		}
	}
	if c.Entry.CurrentScore >= 72 &&
		c.Entry.ScoreSlope >= 0.04 &&
		c.VolumeRatio >= 1.20 &&
		continuationStructureConfirmed(c) &&
		candidatePriceConfirmsDirection(c) &&
		classifyExhaustionRisk(c, side) == "low" {
		return "clean_continuation"
	}
	if candidateDirectionalMovePct(c) >= envFloat("LIVE_LATE_ENTRY_DAYUTC_BRAKE_PCT", 25.0) || c.ExtensionATR >= envFloat("LIVE_ADD_MAX_EXTENSION_ATR", 1.35) {
		return "extended_wait_reset"
	}
	return "reject"
}

func starterSubtypeName(c candidate) string {
	if strings.EqualFold(c.Side, "BUY") && (c.ReclaimHold || c.Entry.FailedBreakdownCount > 0 || c.Entry.FailedBreakLowCount > 0 || c.SetupFamily == "deep_pullback_reclaim") {
		return "deep_pullback_reclaim"
	}
	if strings.EqualFold(c.Side, "SELL") && (c.Entry.FailedBounceCount > 0 || c.Entry.FailedReclaimCount > 0 || c.Entry.EntryStyle == "pullback_short") {
		return "breakout_retest"
	}
	return "continuation_fast"
}

func candidatePriceConfirmsDirection(c candidate) bool {
	if strings.EqualFold(c.Side, "BUY") {
		aboveVWAP := c.SessionVWAP <= 0 || c.LastClose >= c.SessionVWAP
		aboveEMA := c.EMA9 <= 0 || c.LastClose >= c.EMA9
		return aboveVWAP && aboveEMA
	}
	belowVWAP := c.SessionVWAP <= 0 || c.LastClose <= c.SessionVWAP
	belowEMA := c.EMA9 <= 0 || c.LastClose <= c.EMA9
	return belowVWAP && belowEMA
}

func candidateExhaustionActive(c candidate) bool {
	if c.Entry.ExhaustionRisk >= envFloat("LIVE_EXHAUSTION_AVOID_CHASE_RISK", 4.5) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(c.Entry.EntryStyle), "avoid_chase") {
		return true
	}
	metaState := strings.ToLower(strings.TrimSpace(c.Entry.MetaState))
	if strings.Contains(metaState, "exhaust") {
		return true
	}
	if c.Entry.ReversalWatchFlag && !hasFreshStructureReset(c) {
		return true
	}
	return false
}

func candidateRapidExpansion(c candidate) bool {
	chasePct := envFloat("LIVE_MAX_CHASE_PCT", 8)
	if strings.EqualFold(c.Side, "BUY") {
		return c.Entry.State == inplay.StatePumping && c.Entry.BarsSinceTrough <= 2 && c.DayUTC24h >= chasePct
	}
	return c.Entry.State == inplay.StatePumping && c.Entry.BarsSincePeak <= 2 && -c.DayUTC24h >= chasePct
}

func candidateSpikeCandle(c candidate) bool {
	return c.VolumeRatio >= 2.50 && c.ExtensionATR >= 1.60
}

func candidateDirectionalMovePct(c candidate) float64 {
	if strings.EqualFold(c.Side, "BUY") {
		return maxFloat(c.DayUTC24h, maxFloat(c.UTC4hPct, c.UTC1hPct))
	}
	return maxFloat(-c.DayUTC24h, maxFloat(-c.UTC4hPct, -c.UTC1hPct))
}

func classifyImpulseQuality(c candidate) string {
	switch classifyExhaustionRisk(c, c.Side) {
	case "high":
		return "likely_exhaustion"
	case "moderate":
		if qualifiesEliteStarterCandidate(c) || starterStructureContextOK(c) {
			return "extended_but_valid"
		}
	}
	dirMove := candidateDirectionalMovePct(c)
	structureOK := starterStructureContextOK(c)
	priceOK := starterDirectionalContextOK(c)
	ofiMinSamples := maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8))
	directionalOFI := true
	if c.OFISamples >= ofiMinSamples {
		ofiThreshold := 0.0
		if gradeValue(c.Entry.CurrentGrade) >= gradeValue("A") && c.Entry.Rank <= 2 && c.Entry.CurrentScore >= 95 {
			ofiThreshold = envFloat("LIVE_ELITE_STARTER_OFI_TOLERANCE_Z", 0.10)
		}
		if strings.EqualFold(c.Side, "BUY") {
			directionalOFI = c.OFIZ >= -ofiThreshold
		} else {
			directionalOFI = c.OFIZ <= ofiThreshold
		}
	}
	if !structureOK || !priceOK || !directionalOFI {
		return "weak/noisy"
	}
	if gradeValue(c.Entry.CurrentGrade) >= gradeValue("A") &&
		c.Entry.CurrentScore >= 95 &&
		c.Entry.Rank <= 2 &&
		c.Entry.ScoreSlope >= 0.04 &&
		c.VolumeRatio >= 0.95 &&
		c.Entry.State != inplay.StateExhausted {
		return "elite_breakout"
	}
	if c.Entry.CurrentScore >= 72 &&
		c.Entry.ScoreSlope >= 0.04 &&
		c.VolumeRatio >= 1.20 &&
		c.Entry.State != inplay.StateCooling {
		if dirMove >= envFloat("LIVE_ADD_MAX_DIRECTIONAL_PCT", 6.0) || c.ExtensionATR >= envFloat("LIVE_ADD_MAX_EXTENSION_ATR", 1.35) {
			return "extended_but_valid"
		}
		return "healthy_continuation"
	}
	if dirMove >= envFloat("LIVE_LATE_ENTRY_DAYUTC_BRAKE_PCT", 25.0) && structureOK && priceOK {
		return "extended_but_valid"
	}
	return "weak/noisy"
}

func qualifiesEarlyDevEntry(c candidate, now time.Time) bool {
	_ = c
	_ = now
	return false
}

func qualifiesUTCOffHoursEntry(c candidate) bool {
	if gradeValue(c.Entry.CurrentGrade) < gradeValue("A") {
		return false
	}
	if candidateExhaustionActive(c) || strings.EqualFold(strings.TrimSpace(c.Entry.EntryStyle), "avoid_chase") {
		return false
	}
	if c.CombinedScore < envFloat("LIVE_STARTER_FINAL_RANK_MIN", 0.72) && c.Conf < 0.55 {
		return false
	}
	if strings.EqualFold(c.Side, "BUY") && c.Entry.LongDemotionFlag {
		return false
	}
	if strings.EqualFold(c.Side, "SELL") && c.Entry.ShortDemotionFlag {
		return false
	}
	return continuationStateTrending(c.Entry.State) || c.Entry.Momentum || hasFreshStructureReset(c)
}

func continuationFastStarterSoftRejectAllowed(c candidate, reject string) bool {
	switch {
	case strings.HasPrefix(reject, "vol_ratio:"):
		return c.VolumeRatio >= envFloat("LIVE_STARTER_MIN_VOL_RATIO", 0.80)
	case reject == "continuation_no_structure_confirm" || reject == strings.TrimSpace(c.StructureReason):
		return envBool("LIVE_STARTER_ALLOW_STRUCTURE_SOFT", true) && starterStructureContextOK(c)
	case reject == "below_vwap_ema", reject == "above_vwap_ema":
		return envBool("LIVE_STARTER_ALLOW_BELOW_VWAP_EMA_SOFT", true) && (starterDirectionalContextOK(c) || qualifiesEliteStarterCandidate(c))
	case strings.HasPrefix(reject, "ofi_z:"):
		return qualifiesEliteStarterCandidate(c)
	case strings.HasPrefix(reject, "hybrid_stop_too_wide"), strings.HasPrefix(reject, "hybrid_stop_rr_too_low"):
		return true
	default:
		return false
	}
}

type ladderPlan struct {
	IsAdd        bool
	IsReentry    bool
	MarginUSDT   float64
	RejectReason string
	SessionBand  string
	SessionMult  float64
	Active       *livePosition
	Previous     *livePosition
}

func manualCatchUpAddAllowed(p *livePosition) bool {
	return false
}

func candidateExtendedForBotAdd(c candidate) bool {
	if hasFreshStructureReset(c) {
		return false
	}
	maxATR := envFloat("LIVE_ADD_MAX_EXTENSION_ATR", 1.35)
	maxMovePct := envFloat("LIVE_ADD_MAX_DIRECTIONAL_PCT", 6.0)
	return c.ExtensionATR >= maxATR || candidateDirectionalMovePct(c) >= maxMovePct
}

func resolveLadderPlan(now time.Time, c candidate, execMgr *liveExecManager, meta map[string]symbolMeta) ladderPlan {
	plan := ladderPlan{
		MarginUSDT: envFloat("LIVE_TRADE_MARGIN_USDT", envFloat("LIVE_STARTER_USDT", envFloat("LIVE_ENTRY_STARTER_USDT", 10))),
	}
	if execMgr == nil {
		return plan
	}
	cfg := execMgr.ladderCfg
	if cfg.StarterUSDT > 0 {
		plan.MarginUSDT = cfg.StarterUSDT
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	var activeSame *livePosition
	for _, p := range execMgr.positions {
		if p == nil {
			continue
		}
		pRaw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(p.Symbol)))
		if pRaw == raw {
			if execMgr.isActive(p) && p.RemainingQty > 0 {
				activeSame = p
			}
		}
	}
	if activeSame != nil {
		if !strings.EqualFold(activeSame.Side, c.Side) {
			plan.RejectReason = "symbol_active_opposite_side"
			return plan
		}
		plan.RejectReason = "max_open_same_symbol"
		return plan
	}
	_ = now
	_ = meta
	return plan
}

func chaseRejectReason(c candidate, allowStructuredReset bool) string {
	if allowStructuredReset && hasFreshStructureReset(c) {
		return ""
	}
	if candidateExhaustionActive(c) {
		return "avoid_chase_exhaustion"
	}
	if continuationDeteriorating(c) && !hasFreshStructureReset(c) {
		return "late_chase_fading_impulse"
	}
	if candidateRapidExpansion(c) && !hasFreshStructureReset(c) {
		if allowStructuredReset && (c.ReclaimHold || c.RetestHold || continuationStructureConfirmed(c)) {
			return ""
		}
		return "late_chase_rapid_expansion"
	}
	if candidateExtendedForBotAdd(c) && !hasFreshStructureReset(c) {
		if allowStructuredReset && (c.ReclaimHold || c.RetestHold || continuationStructureConfirmed(c)) {
			return ""
		}
		return "late_chase_extended_no_reset"
	}
	return ""
}

func postWinCooldownRejectReason(now time.Time, c candidate, execMgr *liveExecManager) string {
	if execMgr == nil || !execMgr.postWinCooldownCfg.Enable || execMgr.postWinCooldownCfg.Cooldown <= 0 {
		return ""
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
	p, ok := execMgr.trackedPosition(raw)
	if !ok || p == nil || p.State != execClosed || p.ClosedAt.IsZero() {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(p.Side), strings.TrimSpace(c.Side)) {
		return ""
	}
	if tradeReturnPct(p) < execMgr.postWinCooldownCfg.ThresholdPct {
		return ""
	}
	if now.Sub(p.ClosedAt) >= execMgr.postWinCooldownCfg.Cooldown {
		return ""
	}
	return "post_win_opposite_cooldown"
}

func liveSessionPolicyLocation() *time.Location {
	loc, err := time.LoadLocation(envStr("LIVE_REPORT_TZ", "America/Chicago"))
	if err != nil {
		return time.Local
	}
	return loc
}

func sessionClockMinute(now time.Time) int {
	localNow := now.In(liveSessionPolicyLocation())
	return localNow.Hour()*60 + localNow.Minute()
}

func sessionClockWindow(raw string, defHour, defMin int) int {
	h, m := parseHHMM(raw, defHour, defMin)
	return h*60 + m
}

func sessionClockInRange(cur, start, end int) bool {
	if start <= end {
		return cur >= start && cur <= end
	}
	return cur >= start || cur <= end
}

func sessionRiskBand(now time.Time) (string, float64) {
	cur := sessionClockMinute(now)
	fullStart := sessionClockWindow(envStr("LIVE_SESSION_FULL_RISK_START_CT", "16:00"), 16, 0)
	fullEnd := sessionClockWindow(envStr("LIVE_SESSION_FULL_RISK_END_CT", "23:00"), 23, 0)
	if sessionClockInRange(cur, fullStart, fullEnd) {
		return "full", 1.0
	}
	cautionStart := sessionClockWindow(envStr("LIVE_SESSION_CAUTION_START_CT", "07:00"), 7, 0)
	cautionEnd := sessionClockWindow(envStr("LIVE_SESSION_CAUTION_END_CT", "15:59"), 15, 59)
	if sessionClockInRange(cur, cautionStart, cautionEnd) {
		return "caution", clamp(envFloat("LIVE_SESSION_CAUTION_RISK_MULT", 0.50), 0.05, 1.0)
	}
	return "overnight", clamp(envFloat("LIVE_SESSION_OVERNIGHT_MAX_RISK_MULT", 0.25), 0.05, 1.0)
}

func sessionAdjustedMarginUSDT(now time.Time, c candidate, plan ladderPlan, margin float64) (float64, string, float64) {
	if margin <= 0 {
		band, mult := sessionRiskBand(now)
		return margin, band, mult
	}
	band, mult := sessionRiskBand(now)
	if band == "full" {
		return margin, band, mult
	}
	if plan.IsAdd {
		return margin, band, 1.0
	}
	return maxFloat(0.0, margin*mult), band, mult
}

func sessionEntryRejectReason(now time.Time, c candidate, plan ladderPlan) string {
	if plan.IsAdd {
		return ""
	}
	band, _ := sessionRiskBand(now)
	if band == "overnight" && envBool("LIVE_SESSION_A_PLUS_ONLY_OVERNIGHT", true) {
		if gradeValue(c.Entry.CurrentGrade) < gradeValue("A+") {
			return "session_overnight_requires_a_plus"
		}
	}
	return ""
}

func lifecycleStageScore(stage string) float64 {
	switch stage {
	case "READY":
		return 1.0
	case "ARMED":
		return 0.70
	case "WATCH":
		return 0.45
	case "EXPIRED":
		return 0.0
	default:
		return 0.20
	}
}

func scoreStructureAlignment(c candidate) float64 {
	score := 0.30
	if strings.EqualFold(c.Side, "BUY") {
		if c.SessionVWAP > 0 && c.LastClose >= c.SessionVWAP {
			score += 0.30
		}
		if c.EMA9 > 0 && c.LastClose >= c.EMA9 {
			score += 0.20
		}
		if c.Entry.Momentum {
			score += 0.20
		}
	} else {
		if c.SessionVWAP > 0 && c.LastClose <= c.SessionVWAP {
			score += 0.30
		}
		if c.EMA9 > 0 && c.LastClose <= c.EMA9 {
			score += 0.20
		}
		if c.Entry.Momentum {
			score += 0.20
		}
	}
	if c.WallConfidence > 0 {
		score += c.WallConfidence * 0.20
		score -= c.WallSpoofRisk * 0.20
	}
	return clamp(score, 0, 1)
}

func scoreExecutionFit(c candidate) float64 {
	spreadN := 1.0 - clamp(c.SpreadBps/25.0, 0, 1)
	depth := c.DepthBid
	if c.DepthAsk < depth || depth == 0 {
		depth = maxFloat(c.DepthAsk, depth)
	}
	depthN := clamp(depth/50000.0, 0, 1)
	imbN := 0.50
	if c.BookImbalance != 0 {
		if strings.EqualFold(c.Side, "BUY") {
			imbN = clamp((c.BookImbalance+1.0)/2.0, 0, 1)
		} else {
			imbN = clamp((1.0-c.BookImbalance)/2.0, 0, 1)
		}
	}
	fundingN := 1.0 - clamp(math.Abs(c.FundingRate)*1500.0, 0, 1)
	extPenalty := 1.0
	if envBool("LIVE_PULLBACK_ENTRY_BIAS", true) {
		extVWAP := math.Abs(relativePct(c.LastClose, c.SessionVWAP))
		extEMA := math.Abs(relativePct(c.LastClose, c.EMA9))
		extPenalty = 1.0 - clamp(maxFloat(extVWAP/envFloat("LIVE_MAX_EXTENSION_FROM_VWAP_PCT", 1.25), extEMA/envFloat("LIVE_MAX_EXTENSION_FROM_EMA_PCT", 1.00))-1.0, 0, 0.55)
	}
	return clamp((0.35*spreadN+0.30*depthN+0.20*imbN+0.15*fundingN)*extPenalty, 0, 1)
}

func normalizedEntryScoreWeights(cfg entryQualityConfig) (float64, float64, float64) {
	dw := cfg.ScoreWeightDiscovery
	tw := cfg.ScoreWeightTrigger
	ew := cfg.ScoreWeightExecution
	if dw <= 0 && tw <= 0 && ew <= 0 {
		return 0.35, 0.40, 0.25
	}
	total := dw + tw + ew
	if total <= 0 {
		return 0.35, 0.40, 0.25
	}
	return dw / total, tw / total, ew / total
}

func closesAboveLevel(c []features.Candle, n int, level float64) bool {
	if len(c) == 0 || n <= 0 || level <= 0 || len(c) < n {
		return false
	}
	for i := len(c) - n; i < len(c); i++ {
		if c[i].C < level {
			return false
		}
	}
	return true
}

func closesBelowLevel(c []features.Candle, n int, level float64) bool {
	if len(c) == 0 || n <= 0 || level <= 0 || len(c) < n {
		return false
	}
	for i := len(c) - n; i < len(c); i++ {
		if c[i].C > level {
			return false
		}
	}
	return true
}

func recentPivotHigh(c []features.Candle, pivotBars, confirmBars int) float64 {
	if len(c) < confirmBars+2 {
		return 0
	}
	end := len(c) - confirmBars
	if end <= 1 {
		end = len(c) - 1
	}
	start := end - pivotBars
	if start < 0 {
		start = 0
	}
	hi := 0.0
	for i := start; i < end; i++ {
		hi = maxFloat(hi, c[i].H)
	}
	return hi
}

func recentPivotLow(c []features.Candle, pivotBars, confirmBars int) float64 {
	if len(c) < confirmBars+2 {
		return 0
	}
	end := len(c) - confirmBars
	if end <= 1 {
		end = len(c) - 1
	}
	start := end - pivotBars
	if start < 0 {
		start = 0
	}
	lo := 0.0
	for i := start; i < end; i++ {
		if c[i].L <= 0 {
			continue
		}
		if lo == 0 || c[i].L < lo {
			lo = c[i].L
		}
	}
	return lo
}

func deriveContinuationStructureSignals(cand *candidate, bars []features.Candle) {
	if cand == nil || len(bars) < 6 {
		return
	}
	confirmBars := envInt("LIVE_CONT_CONFIRM_BARS", 2)
	if confirmBars < 1 {
		confirmBars = 1
	}
	reclaimBars := envInt("LIVE_RECLAIM_HOLD_BARS", 1)
	if reclaimBars < 1 {
		reclaimBars = 1
	}
	retestBars := envInt("LIVE_BREAK_RETEST_MAX_BARS", 3)
	if retestBars < 1 {
		retestBars = 1
	}
	pivotBars := maxInt(confirmBars+2, envInt("LIVE_STOP_LOOKBACK_CONT_BARS", 6))
	if pivotBars < 4 {
		pivotBars = 4
	}

	last := bars[len(bars)-1]
	prev := bars[len(bars)-2]
	if strings.EqualFold(cand.Side, "BUY") {
		pivot := recentPivotHigh(bars, pivotBars, confirmBars)
		if pivot > 0 && closesAboveLevel(bars, confirmBars, pivot) {
			cand.ClosedBreakHold = true
			cand.StructureReason = "break_hold"
		}
		if cand.SessionVWAP > 0 && closesAboveLevel(bars, reclaimBars, cand.SessionVWAP) && prev.C < cand.SessionVWAP {
			cand.ReclaimHold = true
			if cand.StructureReason == "" {
				cand.StructureReason = "reclaim_hold"
			}
		}
		if !cand.ReclaimHold && cand.EMA9 > 0 && closesAboveLevel(bars, reclaimBars, cand.EMA9) && prev.C < cand.EMA9 {
			cand.ReclaimHold = true
			if cand.StructureReason == "" {
				cand.StructureReason = "reclaim_hold"
			}
		}
		if pivot > 0 && last.C > pivot {
			start := maxInt(0, len(bars)-retestBars-1)
			for i := start; i < len(bars)-1; i++ {
				if bars[i].L <= pivot && bars[i].C >= pivot {
					cand.RetestHold = true
					cand.ResetRebreak = true
					if cand.StructureReason == "" {
						cand.StructureReason = "retest_hold"
					}
					break
				}
			}
		}
		anchor := maxFloat(cand.SessionVWAP, cand.EMA9)
		if anchor > 0 && cand.ATR > 0 {
			cand.ExtensionATR = math.Abs(last.C-anchor) / cand.ATR
		}
	} else {
		pivot := recentPivotLow(bars, pivotBars, confirmBars)
		if pivot > 0 && closesBelowLevel(bars, confirmBars, pivot) {
			cand.ClosedBreakHold = true
			cand.StructureReason = "break_hold"
		}
		if cand.SessionVWAP > 0 && closesBelowLevel(bars, reclaimBars, cand.SessionVWAP) && prev.C > cand.SessionVWAP {
			cand.ReclaimHold = true
			if cand.StructureReason == "" {
				cand.StructureReason = "reclaim_hold"
			}
		}
		if !cand.ReclaimHold && cand.EMA9 > 0 && closesBelowLevel(bars, reclaimBars, cand.EMA9) && prev.C > cand.EMA9 {
			cand.ReclaimHold = true
			if cand.StructureReason == "" {
				cand.StructureReason = "reclaim_hold"
			}
		}
		if pivot > 0 && last.C < pivot {
			start := maxInt(0, len(bars)-retestBars-1)
			for i := start; i < len(bars)-1; i++ {
				if bars[i].H >= pivot && bars[i].C <= pivot {
					cand.RetestHold = true
					cand.ResetRebreak = true
					if cand.StructureReason == "" {
						cand.StructureReason = "retest_hold"
					}
					break
				}
			}
		}
		anchor := minPositive(cand.SessionVWAP, cand.EMA9)
		if anchor > 0 && cand.ATR > 0 {
			cand.ExtensionATR = math.Abs(last.C-anchor) / cand.ATR
		}
	}
}

func continuationStructureConfirmed(c candidate) bool {
	return c.ClosedBreakHold || c.ReclaimHold || c.RetestHold
}

func hasFreshStructureReset(c candidate) bool {
	return c.ReclaimHold || c.RetestHold || c.ResetRebreak
}

func nearbyLiquidityRiskPool(c candidate, pools []features.LiquidityPool, lastClose float64) (features.LiquidityPool, bool) {
	if lastClose <= 0 || len(pools) == 0 {
		return features.LiquidityPool{}, false
	}
	nearBps := envFloat("LIVE_LIQUIDITY_RISK_NEARBY_BPS", 35.0)
	minTouches := envInt("LIVE_LIQUIDITY_RISK_MIN_TOUCHES", 2)
	bestDist := math.MaxFloat64
	var best features.LiquidityPool
	found := false
	for _, p := range pools {
		if p.Level <= 0 || p.Count < minTouches {
			continue
		}
		if strings.EqualFold(c.Side, "BUY") {
			if p.Side != features.SideLong || p.Level >= lastClose {
				continue
			}
			dist := ((lastClose - p.Level) / lastClose) * 10000.0
			if dist <= nearBps && dist < bestDist {
				bestDist = dist
				best = p
				found = true
			}
			continue
		}
		if p.Side != features.SideShort || p.Level <= lastClose {
			continue
		}
		dist := ((p.Level - lastClose) / lastClose) * 10000.0
		if dist <= nearBps && dist < bestDist {
			bestDist = dist
			best = p
			found = true
		}
	}
	return best, found
}

func detectRecentLiquiditySweep(bars []features.Candle, p features.LiquidityPool) (bool, float64, int) {
	if len(bars) == 0 || p.Level <= 0 {
		return false, 0, 0
	}
	lookback := maxInt(1, envInt("LIVE_LIQUIDITY_SWEEP_LOOKBACK_BARS", 4))
	start := maxInt(0, len(bars)-lookback)
	bestStrength := 0.0
	bestBarsAgo := 0
	found := false
	for i := len(bars) - 1; i >= start; i-- {
		b := bars[i]
		if p.Side == features.SideLong {
			if b.L < p.Level && b.C > p.Level {
				wickPct := ((p.Level - b.L) / p.Level) * 100.0
				if !found || wickPct > bestStrength {
					bestStrength = wickPct + 0.5
					bestBarsAgo = len(bars) - 1 - i
					found = true
				}
			}
			continue
		}
		if b.H > p.Level && b.C < p.Level {
			wickPct := ((b.H - p.Level) / p.Level) * 100.0
			if !found || wickPct > bestStrength {
				bestStrength = wickPct + 0.5
				bestBarsAgo = len(bars) - 1 - i
				found = true
			}
		}
	}
	return found, bestStrength, bestBarsAgo
}

func applyLiquidityRiskSignals(cand *candidate, snap features.Snapshot, bars []features.Candle) {
	if cand == nil || !envBool("LIVE_LIQUIDITY_RISK_ENABLE", true) {
		return
	}
	pool, ok := nearbyLiquidityRiskPool(*cand, snap.Pools, cand.LastClose)
	if !ok {
		return
	}
	cand.LiquidityRisk = true
	cand.LiquidityPoolLevel = pool.Level
	cand.LiquidityPoolCount = pool.Count
	cand.LiquidityPoolSide = string(pool.Side)
	if strings.EqualFold(cand.Side, "BUY") {
		cand.LiquidityRiskReason = "weak_low_below_entry"
	} else {
		cand.LiquidityRiskReason = "weak_high_above_entry"
	}
	sweepSeen, sweepStrength, barsAgo := detectRecentLiquiditySweep(bars, pool)
	if !sweepSeen && snap.Sweep != nil && snap.Sweep.Side == pool.Side && math.Abs(relativePct(snap.Sweep.Level, pool.Level)) <= envFloat("LIVE_LIQUIDITY_SAME_LEVEL_TOL_PCT", 0.20) {
		sweepSeen = true
		sweepStrength = snap.Sweep.Strength
		barsAgo = 0
	}
	cand.LiquiditySweepSeen = sweepSeen
	cand.LiquiditySweepStrength = sweepStrength
	cand.LiquiditySweepBarsAgo = barsAgo
}

func liquiditySweepAcceptanceConfirmed(c candidate) bool {
	if !c.LiquidityRisk {
		return true
	}
	structureOK := hasFreshStructureReset(c) || c.ReclaimHold || c.RetestHold || c.ClosedBreakHold
	if !structureOK {
		return false
	}
	if strings.EqualFold(c.Side, "BUY") {
		return c.LastClose > c.LiquidityPoolLevel
	}
	return c.LastClose < c.LiquidityPoolLevel
}

func liquiditySweepOFIConfirmed(c candidate) bool {
	ofiEnabled := envBool("LIVE_ENABLE_OFI", true)
	ofiMinSamples := maxInt(1, envInt("LIVE_OFI_MIN_SAMPLES", 8))
	if !ofiEnabled || c.OFISamples < ofiMinSamples {
		return true
	}
	minOFIZ := envFloat("LIVE_LIQUIDITY_SWEEP_MIN_OFI_Z", 0.15)
	if strings.EqualFold(c.Side, "BUY") {
		return c.OFIZ >= minOFIZ
	}
	return c.OFIZ <= -minOFIZ
}

func liquidityRiskRejectReason(c candidate) string {
	if !envBool("LIVE_LIQUIDITY_RISK_ENABLE", true) || !c.LiquidityRisk {
		return ""
	}
	if !c.LiquiditySweepSeen {
		return "liquidity_risk_wait_sweep"
	}
	if !liquiditySweepAcceptanceConfirmed(c) {
		return "liquidity_risk_wait_reclaim_hold"
	}
	if !liquiditySweepOFIConfirmed(c) {
		return "liquidity_risk_wait_ofi_confirm"
	}
	return ""
}

func stopTemplateForCandidate(c candidate) exitmgr.StopTemplate {
	switch c.SetupFamily {
	case "reset_impulse_breakout":
		return exitmgr.StopTemplateContinuationImpulse
	case "micro_pullback_continuation", "breakout_retest", "deep_pullback_reclaim":
		return exitmgr.StopTemplateReclaimPullback
	case "reversal_exhaustion":
		return exitmgr.StopTemplateReversalExhaustion
	}
	switch strings.ToLower(strings.TrimSpace(c.Entry.EntryStyle)) {
	case "leader_unwind_short", "reversal_watch_short", "reversal_watch_long":
		return exitmgr.StopTemplateReversalExhaustion
	case "pullback_long", "pullback_short", "breakout_hold_long", "breakout_hold_short":
		return exitmgr.StopTemplateReclaimPullback
	}
	switch strategyFamily(c) {
	case "ignite":
		return exitmgr.StopTemplateContinuationImpulse
	case "rev":
		return exitmgr.StopTemplateReversalExhaustion
	}
	switch strings.ToLower(strings.TrimSpace(c.Strat)) {
	case "continuation_fast", "impulse_long", "impulse_short", "momentum_ignite_long", "momentum_ignite_short", "reset_impulse_long", "reset_impulse_short":
		return exitmgr.StopTemplateContinuationImpulse
	case "fa", "failed_auction_magnet", "vwap_confluence", "bos_pb", "open_drive":
		return exitmgr.StopTemplateReclaimPullback
	case "mom_reversal", "mom_reversal_short", "exhaustion_flip_long", "exhaustion_flip_short":
		return exitmgr.StopTemplateReversalExhaustion
	default:
		return exitmgr.StopTemplateMeanRevertRotation
	}
}

func hybridStopInputForCandidate(c candidate, entry, tp1 float64) exitmgr.HybridStopInput {
	return exitmgr.HybridStopInput{
		Side:           c.Side,
		Entry:          entry,
		SignalStop:     c.Sig.Stop,
		StructureLow:   c.Entry.TroughPriceLookback,
		StructureHigh:  c.Entry.PeakPriceLookback,
		SessionVWAP:    c.SessionVWAP,
		EMA9:           c.EMA9,
		ATR:            c.ATR,
		TargetPrice:    tp1,
		Template:       stopTemplateForCandidate(c),
		EliteCandidate: qualifiesEliteStarterCandidate(c),
	}
}

func confidenceRejectReason(c candidate, minConf float64) string {
	base := fmt.Sprintf("conf:%.2f<%.2f", c.Conf, minConf)
	if c.Conf > 0 {
		return base
	}
	reasons := make([]string, 0, 3)
	if rr := strings.TrimSpace(c.RejectReason); rr != "" {
		reasons = append(reasons, rr)
	}
	if strat := strings.TrimSpace(c.Strat); strat == "" || strings.EqualFold(strat, "none") {
		reasons = append(reasons, "strategy_none")
	}
	if rr := strings.TrimSpace(c.Sig.RejectReason); rr != "" && !containsString(reasons, rr) {
		reasons = append(reasons, rr)
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "zero_conf_root_unknown")
	}
	return base + " (" + strings.Join(reasons, ",") + ")"
}

func passesAsiaEntryQuality(now time.Time, c candidate, asiaMinGrade string, asiaStrongConfMin float64, asiaMinSlope float64) bool {
	_ = now
	_ = c
	_ = asiaMinGrade
	_ = asiaStrongConfMin
	_ = asiaMinSlope
	return true
}

func rankWithStrategy(cache *featureRuntimeCache, in []candidate, topN int, stopMode, targetMode string, vpMinTargetPct float64, inertiaEnable bool, inertiaScoreMin, inertiaSlowMin, inertiaFastMax float64, inertiaSlowN, inertiaFastN int, reversalVolSpike float64, sortCfg rankSortConfig, rel reliability.Store, flow map[string]flowMetrics) []candidate {
	if len(in) == 0 {
		return in
	}
	out := make([]candidate, len(in))
	copy(out, in)
	topN = normalizeStrategyTopN(topN, len(out))
	for i := 0; i < topN; i++ {
		out[i] = enrichCandidate(cache, out[i], stopMode, targetMode, vpMinTargetPct, inertiaEnable, inertiaScoreMin, inertiaSlowMin, inertiaFastMax, inertiaSlowN, inertiaFastN, reversalVolSpike, flow)
	}
	for i := topN; i < len(out); i++ {
		if strings.EqualFold(out[i].Strat, "mom_reversal_short") {
			out[i] = enrichCandidate(cache, out[i], stopMode, targetMode, vpMinTargetPct, inertiaEnable, inertiaScoreMin, inertiaSlowMin, inertiaFastMax, inertiaSlowN, inertiaFastN, reversalVolSpike, flow)
		}
	}
	for i := range out {
		out[i].FinalRank = finalSortRank(out[i], sortCfg, rel)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].FinalRank > out[j].FinalRank
	})
	return out
}

func normalizeStrategyTopN(topN, total int) int {
	if total <= 0 {
		return 0
	}
	if topN <= 0 || topN > total {
		return total
	}
	return topN
}

func finalSortRank(c candidate, cfg rankSortConfig, rel reliability.Store) float64 {
	r := c.Entry.Rank
	if cfg.UseConfidence {
		w := cfg.ConfidenceWeight
		if w <= 0 {
			w = 1.0
		}
		r *= 1 + clamp(c.Conf, 0, 1)*w
	}
	if cfg.UseCompleteness {
		w := cfg.CompletenessWeight
		if w < 0 {
			w = 0
		}
		compBoost := 0.80 + 0.20*clamp(c.Entry.Completeness, 0, 1)
		r *= 1 + ((compBoost - 1.0) * w)
	}
	if cfg.UseReliability && rel != nil {
		adj := rel.Adjustment(strings.ToUpper(aster.RawSymbol(c.Entry.Symbol)))
		c.ReliabilityAdj = adj
		w := cfg.ReliabilityWeight
		if w <= 0 {
			w = 1.0
		}
		r += adj * w
	}
	if cfg.UseVolume {
		w := cfg.VolumeWeight
		if w < 0 {
			w = 0
		}
		r += clamp(c.VolumeRatio/2.5, 0, 1) * w
	}
	if strategyFamily(c) != "rev" && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Strat)), "reset_impulse_") {
		if conflicting, magnitude := directionallyConflicting(c, envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY_PCT", 2.0)); conflicting {
			penalty := envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY", 18.0)
			penalty += maxFloat(0.0, magnitude-envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY_PCT", 2.0)) * envFloat("LIVE_DIRECTIONAL_CONFLICT_PENALTY_PER_PCT", 2.0)
			r -= penalty
		}
	}
	r += sessionScannerBoost(c, time.Now().In(time.Local))
	return r
}

func sessionScannerBoost(c candidate, now time.Time) float64 {
	if !envBool("LIVE_ENTRY_SESSION_BOOST_ENABLE", true) {
		return 0
	}
	startHour, startMin := parseHHMM(envStr("LIVE_ENTRY_SESSION_BOOST_START", "05:00"), 5, 0)
	endHour, endMin := parseHHMM(envStr("LIVE_ENTRY_SESSION_BOOST_END", "07:00"), 7, 0)
	minutes := now.Hour()*60 + now.Minute()
	startMinutes := startHour*60 + startMin
	endMinutes := endHour*60 + endMin
	if minutes < startMinutes || minutes > endMinutes {
		return 0
	}
	maxRank := envFloat("LIVE_ENTRY_SESSION_BOOST_MAX_RANK", 5.0)
	if maxRank > 0 && c.Entry.Rank > maxRank {
		return 0
	}
	minScore := envFloat("LIVE_ENTRY_SESSION_BOOST_MIN_SCORE", 88.0)
	if c.Entry.CurrentScore < minScore {
		return 0
	}
	boost := envFloat("LIVE_ENTRY_SESSION_BOOST_POINTS", 12.0)
	if gradeValue(c.Entry.CurrentGrade) >= gradeValue("A") {
		boost *= envFloat("LIVE_ENTRY_SESSION_BOOST_A_MULT", 1.15)
	}
	return boost
}

func enrichCandidate(cache *featureRuntimeCache, cand candidate, stopMode, targetMode string, vpMinTargetPct float64, inertiaEnable bool, inertiaScoreMin, inertiaSlowMin, inertiaFastMax float64, inertiaSlowN, inertiaFastN int, reversalVolSpike float64, flow map[string]flowMetrics) candidate {
	raw := strings.ToUpper(aster.RawSymbol(cand.Entry.Symbol))
	_ = reversalVolSpike
	if cache == nil {
		cand.RejectReason = withUnresolvedSource(cand.RejectReason, "feature_cache_nil")
		return cand
	}
	snapView, bars, err := cache.microSnapshot(raw, 240, envInt("LIVE_ATR_LEN", 14), inertiaFastN, inertiaSlowN, 20)
	if err != nil || len(bars) < 30 {
		cand.RejectReason = withUnresolvedSource(cand.RejectReason, "feature_bars_insufficient")
		return cand
	}
	fc := make([]features.Candle, 0, len(bars))
	for _, b := range bars {
		fc = append(fc, features.Candle{Ts: b.T, O: b.O, H: b.H, L: b.L, C: b.C, V: b.V})
	}
	fe := features.NewEngine(features.Config{})
	snap := fe.Eval(fc)
	cand.LastClose = snapView.LastClose
	cand.EMA9 = snapView.EMA9
	cand.SessionVWAP = snapView.SessionVWAP
	cand.SlowSlope = snapView.SlowSlopePct
	cand.FastSlope = snapView.FastSlopePct
	cand.VolumeRatio = snapView.VolumeRatio
	cand.ATR = snapView.ATR
	cand.ATRPct = snapView.ATRPct
	deriveContinuationStructureSignals(&cand, fc)
	applyLiquidityRiskSignals(&cand, snap, fc)
	applyPatternModifiers(&cand, fc)
	if fm, ok := flow[raw]; ok {
		cand.OFIRaw = fm.OFIRaw
		cand.OFIZ = fm.OFIZ
		cand.OFISamples = fm.OFISamples
		cand.SpreadBps = fm.SpreadBps
		cand.DepthBid = fm.DepthBid
		cand.DepthAsk = fm.DepthAsk
		cand.BookImbalance = fm.BookImbalance
	}
	annotateCandidateEntryContext(&cand, time.Now().UTC())
	ensureCandidateSetupFamily(&cand, time.Now().UTC())
	finalizeCandidateExecutionLabels(&cand, time.Now().UTC())
	if inertiaEnable &&
		strings.EqualFold(cand.Side, "BUY") &&
		strings.EqualFold(cand.Strat, "") &&
		cand.Entry.CurrentScore >= inertiaScoreMin &&
		cand.SlowSlope > inertiaSlowMin &&
		cand.FastSlope < inertiaFastMax {
		cand.Conf = 0
		cand.RejectReason = withUnresolvedSource("STATE_INERTIA_KILL", "inertia_kill")
		return cand
	}
	rtCtx := &strategies.RuntimeSignalContext{
		RequestedStrategy:     cand.Strat,
		Side:                  toFeatureSide(cand.Side),
		CandidateState:        string(cand.Entry.State),
		LastClose:             cand.LastClose,
		EMA9:                  cand.EMA9,
		SessionVWAP:           cand.SessionVWAP,
		FastSlope:             cand.FastSlope,
		SlowSlope:             cand.SlowSlope,
		OFIZ:                  cand.OFIZ,
		OFISamples:            cand.OFISamples,
		ATRPct:                cand.ATRPct,
		FailedReclaimCount:    cand.Entry.FailedReclaimCount,
		FailedBounceCount:     cand.Entry.FailedBounceCount,
		FailedBreakdownCount:  cand.Entry.FailedBreakdownCount,
		FailedBreakLowCount:   cand.Entry.FailedBreakLowCount,
		BarsSincePeak:         cand.Entry.BarsSincePeak,
		BarsSinceTrough:       cand.Entry.BarsSinceTrough,
		DrawdownFromPeakPct:   cand.Entry.DrawdownFromPeakPct,
		DrawupFromTroughPct:   cand.Entry.DrawupFromTroughPct,
		IntradayReversalScore: cand.Entry.IntradayReversalScore,
		BullReversalScore:     cand.Entry.BullReversalScore,
	}
	if strings.EqualFold(cand.Strat, "exhaustion_flip_short") ||
		strings.EqualFold(cand.Strat, "exhaustion_flip_long") ||
		strings.EqualFold(cand.Strat, "mom_reversal_short") {
		sig, handled := strategies.EvaluateRuntimeSignal(strategies.Context{
			Symbol:       raw,
			TF:           "1m",
			ScannerScore: cand.Entry.CurrentScore,
			ScannerGrade: cand.Entry.CurrentGrade,
			ScoreSlope:   cand.Entry.ScoreSlope,
			DayUTCPct:    cand.DayUTC24h,
			UTC4hPct:     cand.UTC4hPct,
			UTC1hPct:     cand.UTC1hPct,
			EntryStyle:   cand.Entry.EntryStyle,
			MetaState:    cand.Entry.MetaState,
			Snapshot:     snap,
			Candles:      fc,
			Runtime:      rtCtx,
		})
		if handled {
			if !sig.Active || strings.TrimSpace(sig.RejectReason) != "" {
				cand.Sig = sig
				cand.Strat = firstNonEmpty(strings.TrimSpace(sig.Name), strings.TrimSpace(cand.Strat))
				cand.Conf = 0
				cand.RejectReason = withUnresolvedSource(firstNonEmpty(strings.TrimSpace(sig.RejectReason), "runtime_signal_rejected"), "runtime_signal_rejected")
				finalizeCandidateExecutionLabels(&cand, time.Now().UTC())
				return cand
			}
			cand.Sig = sig
			cand.Strat = sig.Name
			cand.Conf = sig.Confidence
			cand.RejectReason = sig.RejectReason
			finalizeCandidateExecutionLabels(&cand, time.Now().UTC())
			return cand
		}
	}
	if envBool("LIVE_SIMPLE_MODE", false) {
		return applySimpleContinuationFallback(cand)
	}
	rt := strategies.NewRouter(strategies.RouterConfig{
		MinGrade:                 "B",
		MinScore:                 0,
		MinWhaleDelta:            -1e18,
		AllowWarmup:              true,
		WarmupSlopeMin:           0,
		MaxOne:                   true,
		EnableVPSetups:           effectiveVPSetupsEnabled(),
		MinVPConfidence:          envFloat("LIVE_MIN_VP_CONFIDENCE", 0.55),
		UseVPReversal:            envBool("LIVE_USE_VP_REVERSAL", true),
		EnableInstitutionalPA:    effectiveInstitutionalPAEnabled(),
		UseSessionRegimeRisk:     true,
		MinConfluenceScore:       envFloat("LIVE_MIN_CONFLUENCE_SCORE", 0.48),
		StrategyWeight:           envFloat("LIVE_CONFLUENCE_STRATEGY_WEIGHT", 0.50),
		FlowWeight:               envFloat("LIVE_CONFLUENCE_FLOW_WEIGHT", 0.30),
		StructureWeight:          envFloat("LIVE_CONFLUENCE_STRUCTURE_WEIGHT", 0.20),
		ContinuationDayUTCPct:    envFloat("LIVE_LATE_ENTRY_DAYUTC_BRAKE_PCT", 25.0),
		ContinuationReset1hPct:   0,
		ContinuationLateSlopeMin: envFloat("LIVE_CONT_FAST_LATE_MIN_SLOPE", 0.16),
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
		DayUTCPct:    cand.DayUTC24h,
		UTC4hPct:     cand.UTC4hPct,
		UTC1hPct:     cand.UTC1hPct,
		EntryStyle:   cand.Entry.EntryStyle,
		MetaState:    cand.Entry.MetaState,
		Snapshot:     snap,
		Candles:      fc,
		Runtime:      rtCtx,
	}
	cs := rt.Eval(ctx)
	if len(cs) == 0 {
		if cand.Strat == "mom_reversal" {
			sig, handled := strategies.EvaluateRuntimeSignal(ctx)
			if handled {
				if !sig.Active || strings.TrimSpace(sig.RejectReason) != "" {
					cand.Sig = sig
					cand.Strat = firstNonEmpty(strings.TrimSpace(sig.Name), strings.TrimSpace(cand.Strat))
					cand.Conf = 0
					cand.RejectReason = withUnresolvedSource(firstNonEmpty(strings.TrimSpace(sig.RejectReason), "mom_reversal_runtime_rejected"), "mom_reversal_runtime_rejected")
					finalizeCandidateExecutionLabels(&cand, time.Now().UTC())
					return cand
				}
				cand.Sig = sig
				cand.Strat = sig.Name
				cand.Conf = sig.Confidence
				cand.RejectReason = sig.RejectReason
				finalizeCandidateExecutionLabels(&cand, time.Now().UTC())
				return cand
			}
		}
		return applySimpleContinuationFallback(cand)
	}
	chosen := cs[0].Signal
	targetSide := toFeatureSide(cand.Side)
	for _, x := range cs {
		if x.Signal.Side == targetSide {
			chosen = x.Signal
			break
		}
	}
	chosen = strategies.ApplySharedInvalidations(ctx, chosen)
	if strings.TrimSpace(chosen.RejectReason) != "" || !chosen.Active {
		cand.Sig = chosen
		cand.Strat = firstNonEmpty(strings.TrimSpace(chosen.Name), strings.TrimSpace(cand.Strat))
		cand.Conf = 0
		cand.RejectReason = withUnresolvedSource(firstNonEmpty(strings.TrimSpace(chosen.RejectReason), "router_rejected"), "router_rejected")
		finalizeCandidateExecutionLabels(&cand, time.Now().UTC())
		return cand
	}
	cand.Sig = chosen
	cand.Strat = chosen.Name
	cand.Conf = chosen.Confidence
	cand.RejectReason = chosen.RejectReason
	finalizeCandidateExecutionLabels(&cand, time.Now().UTC())
	return cand
}

func applySimpleContinuationFallback(cand candidate) candidate {
	return applySimpleContinuationFallbackAt(cand, time.Now().UTC())
}

func applySimpleContinuationFallbackAt(cand candidate, now time.Time) candidate {
	annotateCandidateEntryContext(&cand, now)
	finalizeCandidateExecutionLabels(&cand, now)
	if !isExecutableStrategy(cand.Strat) {
		source := "continuation_fallback_unmapped"
		if strings.TrimSpace(cand.SetupFamily) != "" {
			source = "setup_family_unmapped"
		}
		cand.RejectReason = withUnresolvedSource(cand.RejectReason, source)
	}
	return cand
}

func annotateCandidateEntryContext(c *candidate, now time.Time) {
	if c == nil {
		return
	}
	ensureCandidateSetupFamily(c, now)
	c.SessionLabel = string(sessionPhaseUTC(now.UTC()))
	c.CandidateAgeSeconds = candidateAgeSeconds(c.Entry, now)
	c.DistanceToVWAPPct = distanceToVWAPPct(c.LastClose, c.SessionVWAP)
	c.EntryTiming = classifyEntryTiming(*c)
}

func candidateAgeSeconds(e inplay.Entry, now time.Time) float64 {
	anchor := e.FirstSeen
	if anchor.IsZero() {
		anchor = e.StateSince
	}
	if anchor.IsZero() {
		return 0
	}
	age := now.Sub(anchor).Seconds()
	if age < 0 {
		return 0
	}
	return age
}

func distanceToVWAPPct(lastClose, vwap float64) float64 {
	if lastClose <= 0 || vwap <= 0 {
		return 0
	}
	return math.Abs((lastClose-vwap)/vwap) * 100.0
}

func classifyEntryTiming(c candidate) string {
	ext := c.ExtensionATR
	distVWAP := c.DistanceToVWAPPct
	if distVWAP <= 0 {
		distVWAP = distanceToVWAPPct(c.LastClose, c.SessionVWAP)
	}
	movePct := candidateDirectionalMovePct(c)
	ageMin := c.Entry.TimeInStateMin
	if ageMin <= 0 && c.CandidateAgeSeconds > 0 {
		ageMin = c.CandidateAgeSeconds / 60.0
	}
	if ext == 0 && distVWAP == 0 && movePct == 0 && ageMin == 0 {
		return "unknown"
	}
	if ext >= 1.35 || distVWAP >= 1.20 || movePct >= 20.0 || ageMin >= 25.0 {
		return "late"
	}
	if ext <= 0.60 && distVWAP <= 0.45 && movePct <= 8.0 && ageMin <= 8.0 {
		return "early"
	}
	return "mid"
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

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
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
	return ta.SnapshotFromTypesCandles(bars, atrN, 3, 15, 20).ATRPct
}

func atrLast(c []features.Candle, n int) float64 {
	if len(c) < 2 {
		return 0
	}
	if n <= 1 {
		n = 14
	}
	trs := make([]float64, 0, len(c)-1)
	for i := 1; i < len(c); i++ {
		hi := c[i].H
		lo := c[i].L
		pc := c[i-1].C
		tr := maxFloat(hi-lo, maxFloat(abs(hi-pc), abs(lo-pc)))
		trs = append(trs, tr)
	}
	if len(trs) == 0 {
		return 0
	}
	if len(trs) < n {
		n = len(trs)
	}
	sum := 0.0
	for i := len(trs) - n; i < len(trs); i++ {
		sum += trs[i]
	}
	return sum / float64(n)
}

func strategyRiskFamily(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(r, "ignite"):
		return "ignite"
	case strings.Contains(r, "reversal"), strings.Contains(r, "flip"):
		return "rev"
	default:
		return "cont"
	}
}

func stopATRMultForReason(reason string) float64 {
	switch strategyRiskFamily(reason) {
	case "ignite":
		return envFloat("LIVE_STOP_ATR_MULT_IGNITE", 1.4)
	case "rev":
		return envFloat("LIVE_STOP_ATR_MULT_REV", 1.2)
	default:
		return envFloat("LIVE_STOP_ATR_MULT_CONT", 1.8)
	}
}

func trailATRMultForReason(reason string) float64 {
	switch strategyRiskFamily(reason) {
	case "rev":
		return envFloat("LIVE_TRAIL_ATR_MULT_REV", 1.9)
	default:
		return envFloat("LIVE_TRAIL_ATR_MULT_CONT", 2.6)
	}
}

func trendCaptureVolatilityMultiplier(atrPct, volumeUSD float64) float64 {
	if atrPct <= 0 {
		return 1.0
	}
	if volumeUSD > envFloat("LIVE_VOLATILE_MAX_VOLUME_USD", 75_000_000.0) {
		return 1.0
	}
	high := envFloat("LIVE_VOLATILE_ATR_PCT_HIGH", 0.050)
	moderate := envFloat("LIVE_VOLATILE_ATR_PCT_MODERATE", 0.035)
	switch {
	case atrPct >= high:
		return envFloat("LIVE_VOLATILE_STOP_MULT_HIGH", 1.60)
	case atrPct >= moderate:
		return envFloat("LIVE_VOLATILE_STOP_MULT_MODERATE", 1.25)
	default:
		return 1.0
	}
}

func widenStopPctForVolatility(stopPct, atrPct, volumeUSD float64) float64 {
	if stopPct <= 0 {
		return stopPct
	}
	return stopPct * trendCaptureVolatilityMultiplier(atrPct, volumeUSD)
}

func trailATRMultForContext(reason string, atrPct, volumeUSD float64) float64 {
	base := trailATRMultForReason(reason)
	if effectiveSharedManagementEnabled() {
		base = envFloat("LIVE_TRAIL_ATR_MULT_CONT", 2.6)
	}
	return base * trendCaptureVolatilityMultiplier(atrPct, volumeUSD)
}

func distForStopMode(price, atr, pctFallback, pctFloor, atrMult float64, mode string) float64 {
	if price <= 0 {
		return 0
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if pctFallback <= 0 {
		pctFallback = envFloat("LIVE_STOP_PCT", 3.0) / 100.0
	}
	if pctFloor <= 0 {
		pctFloor = envFloat("LIVE_STOP_PCT_MIN", 0.40) / 100.0
	}
	pctDist := price * maxFloat(pctFallback, pctFloor)
	atrDist := 0.0
	if atr > 0 && atrMult > 0 {
		atrDist = atr * atrMult
	}
	switch mode {
	case "atr":
		if atrDist > 0 {
			return atrDist
		}
		return pctDist
	case "pct":
		return pctDist
	default:
		if atrDist > 0 {
			return maxFloat(price*pctFloor, atrDist)
		}
		return pctDist
	}
}

func applySignalRiskGeometry(cand candidate, name string) strategies.Signal {
	side := toFeatureSide(cand.Side)
	entryPx := cand.LastClose
	if entryPx <= 0 {
		entryPx = cand.Sig.Entry
	}
	if entryPx <= 0 {
		return cand.Sig
	}
	stopDist := distForStopMode(
		entryPx,
		cand.ATR,
		envFloat("LIVE_STOP_PCT", 3.0)/100.0,
		envFloat("LIVE_STOP_PCT_MIN", 0.40)/100.0,
		stopATRMultForReason(name),
		envStr("LIVE_STOP_MODE", "hybrid"),
	)
	if stopDist <= 0 {
		return cand.Sig
	}
	tp1R := envFloat("LIVE_TP1_R", 1.20)
	tp2R := envFloat("LIVE_TP2_R", 2.50)
	tp3R := envFloat("LIVE_TP3_R", 4.00)
	if strings.Contains(strings.ToLower(name), "ignite") {
		tp1R = envFloat("LIVE_IGNITE_TP1_R", 1.0)
		tp2R = envFloat("LIVE_IGNITE_TP2_R", 2.4)
		tp3R = envFloat("LIVE_IGNITE_TP3_R", 4.2)
	}
	stopPx := entryPx
	tp1Px := entryPx
	tp2Px := entryPx
	tp3Px := entryPx
	profile := chooseExitProfile(cand)
	provisionalTP1 := entryPx
	if side == features.SideLong {
		stopPx = entryPx - stopDist
		provisionalTP1 = entryPx + stopDist*tp1R
	} else {
		stopPx = entryPx + stopDist
		provisionalTP1 = entryPx - stopDist*tp1R
	}
	hybridCfg := loadHybridStopConfig()
	if hybridCfg.Enabled {
		stopPlan := exitmgr.ComputeHybridStop(hybridCfg, hybridStopInputForCandidate(cand, entryPx, provisionalTP1))
		if !stopPlan.Rejected && stopPlan.StopPrice > 0 {
			cand.StopPlan = stopPlan
			stopPx = stopPlan.StopPrice
			stopDist = math.Abs(entryPx - stopPx)
		}
	}
	if envBool("LIVE_DYNAMIC_TP_ENABLE", true) {
		profile, tp1Px, tp2Px, tp3Px = computeDynamicTargetLadder(cand, entryPx, stopDist, tp1R, tp2R, tp3R)
	} else if side == features.SideLong {
		profile, tp1R, tp2R, tp3R = profileTargetRs(cand, tp1R, tp2R, tp3R)
		tp1Px = entryPx + stopDist*tp1R
		tp2Px = entryPx + stopDist*tp2R
		tp3Px = entryPx + stopDist*tp3R
	} else {
		profile, tp1R, tp2R, tp3R = profileTargetRs(cand, tp1R, tp2R, tp3R)
		tp1Px = entryPx - stopDist*tp1R
		tp2Px = entryPx - stopDist*tp2R
		tp3Px = entryPx - stopDist*tp3R
	}
	sig := cand.Sig
	sig.Active = true
	sig.Name = name
	sig.Side = side
	sig.Entry = entryPx
	sig.Stop = stopPx
	sig.TP1 = tp1Px
	sig.TP2 = tp2Px
	sig.TP3 = tp3Px
	sig.Tags = append(sig.Tags, "exit_profile:"+strings.ToLower(profile))
	if cand.StopPlan.StopReason != "" {
		sig.Tags = append(sig.Tags, "stop_anchor:"+cand.StopPlan.StopReason)
	}
	return sig
}

func printUnifiedInPlay(longInPlay, shortInPlay []inplay.Entry, meta map[string]symbolMeta) {
	rows := buildUnifiedInPlayRows(longInPlay, shortInPlay, meta, 0)
	fmt.Printf("IN-PLAY (RANKED)\n")
	fmt.Println("+------+------------+-------+---------+---------+-----------+---------+---------+---------+------------+------------+----------+")
	fmt.Println("| side | sym        | grade | score   | slope   | state     | dayutc% | utc4h%  | utc1h%  | open       | mark       | vol($)   |")
	fmt.Println("+------+------------+-------+---------+---------+-----------+---------+---------+---------+------------+------------+----------+")
	if len(rows) == 0 {
		fmt.Println("| (none)                                                                                                             |")
		fmt.Println("+------+------------+-------+---------+---------+-----------+---------+---------+---------+------------+------------+----------+")
		return
	}
	for _, row := range rows {
		e := row.entry
		sym := strings.ToUpper(aster.RawSymbol(e.Symbol))
		if sym == "" {
			sym = strings.ToUpper(strings.TrimSpace(e.Symbol))
		}
		grade := strings.ToUpper(strings.TrimSpace(e.CurrentGrade))
		state := strings.ToUpper(strings.TrimSpace(displayState(e.SideBias, e.State)))
		gColor := market.GradeColor(grade)
		sColor := inPlayStateColor(inplay.State(strings.ToLower(state)))
		m := row.meta
		dayUTC := "-"
		if m.DayUTC24h != 0 {
			dayUTC = fmt.Sprintf("%+6.1f", m.DayUTC24h)
		}
		utc4h := "-"
		if m.UTC4hPct != 0 {
			utc4h = fmt.Sprintf("%+6.1f", m.UTC4hPct)
		}
		utc1h := "-"
		if m.UTC1hPct != 0 {
			utc1h = fmt.Sprintf("%+6.1f", m.UTC1hPct)
		}
		openPx := "-"
		if m.OpenPrice > 0 {
			openPx = fmtPrice(m.OpenPrice)
		}
		markPx := "-"
		if m.LastPrice > 0 {
			markPx = fmtPrice(m.LastPrice)
		}
		vol := "-"
		if m.VolumeUSD > 0 {
			vol = marketHumanUSD(m.VolumeUSD)
		}
		fmt.Printf("| %-4s | %-10s | %s%-5s%s | %7.2f | %7.3f | %s%-9s%s | %7s | %7s | %7s | %10s | %10s | %8s |\n",
			row.side,
			sym,
			gColor, grade, market.ResetColor(),
			e.CurrentScore, e.ScoreSlope,
			sColor, strings.ToLower(state), market.ResetColor(),
			dayUTC, utc4h, utc1h, openPx, markPx, vol,
		)
	}
	fmt.Println("+------+------------+-------+---------+---------+-----------+---------+---------+---------+------------+------------+----------+")
}

func printTradeIntent(c candidate, entryBps, margin float64, lev int) {
	sym := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	if lev <= 0 {
		lev = 1
	}
	fmt.Printf("DRY_RUN intent: symbol=%s side=%s margin=$%.2f leverage=%dx entry=LIMIT(mid %+0.2fbps in direction) trigger_state=%s exit_profile=%s disc=%.2f trig=%.2f exec=%.2f combo=%.2f\n",
		sym, c.Side, margin, lev, entryBps, c.TriggerState, c.ExitProfile, c.DiscoveryScore, c.TriggerScore, c.ExecutionScore, c.CombinedScore)
}

func marketHumanUSD(x float64) string {
	ax := abs(x)
	switch {
	case ax >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", x/1_000_000_000)
	case ax >= 1_000_000:
		return fmt.Sprintf("%.2fM", x/1_000_000)
	case ax >= 1_000:
		return fmt.Sprintf("%.2fK", x/1_000)
	default:
		return fmt.Sprintf("%.0f", x)
	}
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

type runnerExitState struct {
	ExhaustionConfirmed bool
	StructureBroken     bool
	TightenReason       string
	FullExitReason      string
}

type HTFStructureState string

const (
	HTFUnknown     HTFStructureState = "unknown"
	HTFLongHHHL    HTFStructureState = "long_hh_hl"
	HTFLongRange   HTFStructureState = "long_range"
	HTFLongBroken  HTFStructureState = "long_broken"
	HTFShortLHLL   HTFStructureState = "short_lh_ll"
	HTFShortRange  HTFStructureState = "short_range"
	HTFShortBroken HTFStructureState = "short_broken"
)

type HTFStructureSnapshot struct {
	Symbol                string
	TF                    string
	State                 HTFStructureState
	LastSwingHigh         float64
	LastSwingLow          float64
	PriorSwingHigh        float64
	PriorSwingLow         float64
	StructureBreakUp      bool
	StructureBreakDown    bool
	BreakConfirmCountUp   int
	BreakConfirmCountDown int
	Delta1H               float64
	DeltaBias             string
	TrendScore            float64
	LastClose             float64
	UpdatedAt             time.Time
}

func htfRefreshInterval() time.Duration {
	sec := envInt("LIVE_HTF_REFRESH_SEC", 300)
	if sec <= 0 {
		sec = 300
	}
	return time.Duration(sec) * time.Second
}

func htfMaxStaleness() time.Duration {
	sec := envInt("LIVE_HTF_MAX_STALENESS_SEC", 900)
	if sec <= 0 {
		sec = 900
	}
	return time.Duration(sec) * time.Second
}

func htfBreakConfirmCloses() int {
	n := envInt("LIVE_HTF_BREAK_CONFIRM_CLOSES", 2)
	if n <= 0 {
		n = 2
	}
	return n
}

func htfSnapshotStale(s HTFStructureSnapshot, now time.Time) bool {
	if s.UpdatedAt.IsZero() {
		return true
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.Sub(s.UpdatedAt) > htfMaxStaleness()
}

func htfClosed1HCandles(candles []types.Candle, now time.Time) []types.Candle {
	if len(candles) == 0 {
		return candles
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	last := candles[len(candles)-1]
	if last.T.Add(time.Hour).After(now) {
		return candles[:len(candles)-1]
	}
	return candles
}

func htfDetectSwings1H(candles []types.Candle, leftRight int) (highs []int, lows []int) {
	if leftRight < 1 {
		leftRight = 1
	}
	if len(candles) < leftRight*2+3 {
		return nil, nil
	}
	for i := leftRight; i < len(candles)-leftRight; i++ {
		isHigh := true
		isLow := true
		for j := i - leftRight; j <= i+leftRight; j++ {
			if j == i {
				continue
			}
			if candles[j].H >= candles[i].H {
				isHigh = false
			}
			if candles[j].L <= candles[i].L {
				isLow = false
			}
		}
		if isHigh {
			highs = append(highs, i)
		}
		if isLow {
			lows = append(lows, i)
		}
	}
	return highs, lows
}

func htfDeltaFromCandles(candles []types.Candle) float64 {
	if len(candles) == 0 {
		return 0
	}
	lookback := envInt("LIVE_HTF_DELTA_LOOKBACK", 12)
	if lookback <= 0 {
		lookback = 12
	}
	if len(candles) > lookback {
		candles = candles[len(candles)-lookback:]
	}
	delta := 0.0
	for _, c := range candles {
		price := maxFloat(c.C, 0)
		qvol := c.V * price
		switch {
		case c.C > c.O:
			delta += qvol
		case c.C < c.O:
			delta -= qvol
		}
	}
	return delta
}

func htfDeltaBias(delta float64) string {
	pos := envFloat("LIVE_HTF_DELTA_POS", 250000)
	neg := envFloat("LIVE_HTF_DELTA_NEG", -250000)
	if pos <= 0 {
		pos = 250000
	}
	if neg >= 0 {
		neg = -250000
	}
	switch {
	case delta >= pos:
		return "bullish"
	case delta <= neg:
		return "bearish"
	default:
		return "neutral"
	}
}

func htfConsecutiveCloseBreak(candles []types.Candle, level float64, breakDown bool) int {
	if level <= 0 || len(candles) == 0 {
		return 0
	}
	count := 0
	for i := len(candles) - 1; i >= 0; i-- {
		c := candles[i]
		if breakDown {
			if c.C < level {
				count++
				continue
			}
			break
		}
		if c.C > level {
			count++
			continue
		}
		break
	}
	return count
}

func buildHTFSnapshotFromCandles(symbol, side string, candles []types.Candle, now time.Time) HTFStructureSnapshot {
	snap := HTFStructureSnapshot{
		Symbol:    strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol))),
		TF:        "1h",
		State:     HTFUnknown,
		DeltaBias: "neutral",
		UpdatedAt: now,
	}
	if now.IsZero() {
		now = time.Now().UTC()
		snap.UpdatedAt = now
	}
	if len(candles) == 0 {
		return snap
	}
	closed := htfClosed1HCandles(candles, now)
	if len(closed) < 8 {
		return snap
	}
	last := closed[len(closed)-1]
	snap.LastClose = last.C
	snap.Delta1H = htfDeltaFromCandles(closed)
	snap.DeltaBias = htfDeltaBias(snap.Delta1H)

	lr := envInt("LIVE_HTF_SWING_LR", 3)
	if lr < 1 {
		lr = 3
	}
	highs, lows := htfDetectSwings1H(closed, lr)
	if len(highs) >= 2 {
		snap.PriorSwingHigh = closed[highs[len(highs)-2]].H
		snap.LastSwingHigh = closed[highs[len(highs)-1]].H
	} else if len(highs) == 1 {
		snap.LastSwingHigh = closed[highs[len(highs)-1]].H
	}
	if len(lows) >= 2 {
		snap.PriorSwingLow = closed[lows[len(lows)-2]].L
		snap.LastSwingLow = closed[lows[len(lows)-1]].L
	} else if len(lows) == 1 {
		snap.LastSwingLow = closed[lows[len(lows)-1]].L
	}
	hasHigherHigh := snap.LastSwingHigh > 0 && snap.PriorSwingHigh > 0 && snap.LastSwingHigh > snap.PriorSwingHigh
	hasHigherLow := snap.LastSwingLow > 0 && snap.PriorSwingLow > 0 && snap.LastSwingLow > snap.PriorSwingLow
	hasLowerHigh := snap.LastSwingHigh > 0 && snap.PriorSwingHigh > 0 && snap.LastSwingHigh < snap.PriorSwingHigh
	hasLowerLow := snap.LastSwingLow > 0 && snap.PriorSwingLow > 0 && snap.LastSwingLow < snap.PriorSwingLow

	snap.BreakConfirmCountDown = htfConsecutiveCloseBreak(closed, snap.LastSwingLow, true)
	snap.BreakConfirmCountUp = htfConsecutiveCloseBreak(closed, snap.LastSwingHigh, false)
	confirmNeed := htfBreakConfirmCloses()
	snap.StructureBreakDown = snap.BreakConfirmCountDown >= confirmNeed
	snap.StructureBreakUp = snap.BreakConfirmCountUp >= confirmNeed

	normalizedSide := strings.ToUpper(strings.TrimSpace(side))
	switch normalizedSide {
	case "BUY", "LONG":
		switch {
		case snap.StructureBreakDown:
			snap.State = HTFLongBroken
			snap.TrendScore = -2.0
		case hasHigherHigh && hasHigherLow:
			snap.State = HTFLongHHHL
			snap.TrendScore = 2.0
		case hasLowerHigh && hasLowerLow:
			snap.State = HTFShortLHLL
			snap.TrendScore = -1.0
		default:
			snap.State = HTFLongRange
			snap.TrendScore = 0.5
		}
	default:
		switch {
		case snap.StructureBreakUp:
			snap.State = HTFShortBroken
			snap.TrendScore = -2.0
		case hasLowerHigh && hasLowerLow:
			snap.State = HTFShortLHLL
			snap.TrendScore = 2.0
		case hasHigherHigh && hasHigherLow:
			snap.State = HTFLongHHHL
			snap.TrendScore = -1.0
		default:
			snap.State = HTFShortRange
			snap.TrendScore = 0.5
		}
	}
	if snap.DeltaBias == "bullish" {
		snap.TrendScore += 0.5
	} else if snap.DeltaBias == "bearish" {
		snap.TrendScore -= 0.5
	}
	return snap
}

func htfSnapshotFromEntry(side string, e *inplay.Entry) HTFStructureSnapshot {
	snap := HTFStructureSnapshot{
		Symbol:    "",
		TF:        "1h",
		State:     HTFUnknown,
		DeltaBias: "neutral",
		UpdatedAt: time.Now().UTC(),
	}
	if e == nil {
		return snap
	}
	snap.Symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(e.Symbol)))
	minStateMin := envFloat("LIVE_HTF_MIN_STATE_MIN", 30.0)
	if strings.EqualFold(side, "BUY") {
		switch {
		case e.LongDemotionFlag || e.State == inplay.StateExhausted || e.State == inplay.StateDumping:
			snap.State = HTFLongBroken
			snap.TrendScore = -2
		case e.TimeInStateMin >= minStateMin &&
			(e.State == inplay.StateInPlay || e.State == inplay.StateHeating || e.State == inplay.StatePumping) &&
			e.ScoreSlope >= -0.05:
			snap.State = HTFLongHHHL
			snap.TrendScore = 2
		default:
			snap.State = HTFLongRange
			snap.TrendScore = 0.75
		}
		return snap
	}
	switch {
	case e.ShortDemotionFlag || e.State == inplay.StateExhausted || e.State == inplay.StatePumping:
		snap.State = HTFShortBroken
		snap.TrendScore = -2
	case e.TimeInStateMin >= minStateMin &&
		(e.State == inplay.StateInPlay || e.State == inplay.StateHeating || e.State == inplay.StateDumping || e.State == inplay.StateCooling) &&
		e.ScoreSlope <= 0.05:
		snap.State = HTFShortLHLL
		snap.TrendScore = 2
	default:
		snap.State = HTFShortRange
		snap.TrendScore = 0.75
	}
	return snap
}

func htfPersistent(side string, s HTFStructureSnapshot) bool {
	if strings.EqualFold(side, "BUY") {
		switch s.State {
		case HTFLongHHHL, HTFLongRange:
			if s.StructureBreakDown {
				return false
			}
			return true
		default:
			return false
		}
	}
	switch s.State {
	case HTFShortLHLL, HTFShortRange:
		if s.StructureBreakUp {
			return false
		}
		return true
	default:
		return false
	}
}

func htfFailed(side string, s HTFStructureSnapshot) bool {
	if htfSnapshotStale(s, time.Now().UTC()) {
		return false
	}
	if strings.EqualFold(side, "BUY") {
		if s.State == HTFLongBroken || s.StructureBreakDown {
			return true
		}
		if s.State != HTFLongHHHL && s.State != HTFLongRange && s.DeltaBias == "bearish" {
			return true
		}
		return false
	}
	if s.State == HTFShortBroken || s.StructureBreakUp {
		return true
	}
	if s.State != HTFShortLHLL && s.State != HTFShortRange && s.DeltaBias == "bullish" {
		return true
	}
	return false
}

func htfCaution(side string, s HTFStructureSnapshot) bool {
	if htfSnapshotStale(s, time.Now().UTC()) {
		return false
	}
	if strings.EqualFold(side, "BUY") {
		return htfPersistent(side, s) && s.DeltaBias == "bearish"
	}
	return htfPersistent(side, s) && s.DeltaBias == "bullish"
}

func (m *liveExecManager) htfSnapshot(symbol, side string, fallback *inplay.Entry) HTFStructureSnapshot {
	if m == nil {
		return htfSnapshotFromEntry(side, fallback)
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if raw == "" && fallback != nil {
		raw = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(fallback.Symbol)))
	}
	if raw == "" {
		return htfSnapshotFromEntry(side, fallback)
	}
	now := time.Now().UTC()
	if m.htf1HBySymbol != nil {
		if snap, ok := m.htf1HBySymbol[raw]; ok && !snap.UpdatedAt.IsZero() && now.Sub(snap.UpdatedAt) < htfRefreshInterval() {
			return snap
		}
	}
	limit := envInt("LIVE_HTF_CANDLE_LOOKBACK", 72)
	if limit < 24 {
		limit = 72
	}
	var (
		bars []types.Candle
		err  error
	)
	if m.featureCache != nil {
		bars, err = m.featureCache.candleSeries(raw, types.TF1h, limit)
	} else {
		bars, err = aster.LoadCandles(raw, types.TF1h, limit)
	}
	if err != nil || len(bars) < 8 {
		if m.htf1HBySymbol != nil {
			if snap, ok := m.htf1HBySymbol[raw]; ok {
				return snap
			}
		}
		return htfSnapshotFromEntry(side, fallback)
	}
	snap := buildHTFSnapshotFromCandles(raw, side, bars, now)
	if m.htf1HBySymbol == nil {
		m.htf1HBySymbol = map[string]HTFStructureSnapshot{}
	}
	m.htf1HBySymbol[raw] = snap
	return snap
}

func (p *paperTrader) htfSnapshot(symbol, side string, fallback *inplay.Entry) HTFStructureSnapshot {
	if p == nil {
		return htfSnapshotFromEntry(side, fallback)
	}
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	if raw == "" && fallback != nil {
		raw = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(fallback.Symbol)))
	}
	if raw == "" {
		return htfSnapshotFromEntry(side, fallback)
	}
	now := time.Now().UTC()
	if p.htf1HBySymbol != nil {
		if snap, ok := p.htf1HBySymbol[raw]; ok && !snap.UpdatedAt.IsZero() && now.Sub(snap.UpdatedAt) < htfRefreshInterval() {
			return snap
		}
	}
	limit := envInt("LIVE_HTF_CANDLE_LOOKBACK", 72)
	if limit < 24 {
		limit = 72
	}
	var (
		bars []types.Candle
		err  error
	)
	if p.featureCache != nil {
		bars, err = p.featureCache.candleSeries(raw, types.TF1h, limit)
	} else {
		bars, err = aster.LoadCandles(raw, types.TF1h, limit)
	}
	if err != nil || len(bars) < 8 {
		if p.htf1HBySymbol != nil {
			if snap, ok := p.htf1HBySymbol[raw]; ok {
				return snap
			}
		}
		return htfSnapshotFromEntry(side, fallback)
	}
	snap := buildHTFSnapshotFromCandles(raw, side, bars, now)
	if p.htf1HBySymbol == nil {
		p.htf1HBySymbol = map[string]HTFStructureSnapshot{}
	}
	p.htf1HBySymbol[raw] = snap
	return snap
}

func evaluateRunnerExitState(side string, mv momentumView, ext flowfeed.ExternalSignal) runnerExitState {
	return evaluateRunnerExitStateWithFlow(side, mv, flowMetrics{}, ext)
}

func runnerFlowBias(side string, fm flowMetrics) (bool, bool) {
	if fm.OFISamples < envInt("LIVE_RUNNER_FLOW_MIN_SAMPLES", 8) && fm.BookImbalance <= 0 {
		return false, false
	}
	ofiMin := envFloat("LIVE_RUNNER_FLOW_MIN_OFI_Z", 0.35)
	if ofiMin <= 0 {
		ofiMin = 0.35
	}
	imbMin := envFloat("LIVE_RUNNER_FLOW_MIN_IMB", 1.15)
	if imbMin <= 1 {
		imbMin = 1.15
	}
	bookLongSupport := fm.BookImbalance >= imbMin
	bookLongAdverse := fm.BookImbalance > 0 && fm.BookImbalance <= (1.0/imbMin)
	bookShortSupport := fm.BookImbalance > 0 && fm.BookImbalance <= (1.0/imbMin)
	bookShortAdverse := fm.BookImbalance >= imbMin
	ofiLongSupport := fm.OFIZ >= ofiMin
	ofiLongAdverse := fm.OFIZ <= -ofiMin
	ofiShortSupport := fm.OFIZ <= -ofiMin
	ofiShortAdverse := fm.OFIZ >= ofiMin
	if strings.EqualFold(side, "SELL") || strings.EqualFold(side, "SHORT") {
		return ofiShortSupport || bookShortSupport, ofiShortAdverse || bookShortAdverse
	}
	return ofiLongSupport || bookLongSupport, ofiLongAdverse || bookLongAdverse
}

func evaluateRunnerExitStateWithFlow(side string, mv momentumView, fm flowMetrics, ext flowfeed.ExternalSignal) runnerExitState {
	e := sameSideMomentumEntry(side, mv)
	if e == nil {
		return runnerExitState{StructureBroken: true, FullExitReason: "RUNNER_STRUCTURE_LOST"}
	}
	supportiveFlow, adverseFlow := runnerFlowBias(side, fm)
	pullbackSlopeMin := envFloat("LIVE_RUNNER_PULLBACK_SLOPE_MIN", -0.12)
	exhaustSlopeMax := envFloat("LIVE_RUNNER_EXHAUST_TIGHTEN_SLOPE_MAX", 0.04)
	deteriorateSlopeMin := envFloat("LIVE_CONT_DETERIORATE_MIN_SLOPE", 0.01)
	slopeCollapseMin := envFloat("LIVE_RUNNER_SLOPE_COLLAPSE_MIN", -0.08)
	maxHealthyDrawdown := envFloat("LIVE_RUNNER_MAX_HEALTHY_DRAWDOWN_PCT", 4.5)
	minTrendHoldDistancePct := envFloat("LIVE_RUNNER_TREND_HOLD_DISTANCE_PCT", -0.20)
	state := runnerExitState{}
	if e.LongDemotionFlag || e.ShortDemotionFlag || e.State == inplay.StateExhausted {
		state.StructureBroken = true
		state.FullExitReason = "RUNNER_STRUCTURE_LOST"
	}
	if shouldExitOnMomentumFade(side, mv, envFloat("LIVE_MOMENTUM_EXIT_SLOPE_MAX", 0.0)) && (adverseFlow || (!supportiveFlow && !e.Momentum)) {
		state.StructureBroken = true
		if state.FullExitReason == "" {
			state.FullExitReason = "RUNNER_STRUCTURE_LOST"
		}
	}
	metaState := strings.ToLower(strings.TrimSpace(e.MetaState))
	switch {
	case (ext.LiqSpike || ext.WhaleSpike) && adverseFlow && !e.Momentum && e.ScoreSlope <= deteriorateSlopeMin:
		state.ExhaustionConfirmed = true
		state.TightenReason = "RUNNER_EXHAUST_LIQ_NO_CONT"
	case strings.Contains(metaState, "exhaust") || e.ReversalWatchFlag:
		if adverseFlow ||
			((!e.Momentum || e.ScoreSlope <= exhaustSlopeMax) &&
				(e.State == inplay.StateCooling || e.State == inplay.StateDumping || e.State == inplay.StateExhausted)) {
			state.ExhaustionConfirmed = true
			state.TightenReason = "RUNNER_EXHAUST_WICK_REJECTION"
		}
	case e.ScoreSlope <= slopeCollapseMin &&
		(e.State == inplay.StateCooling || e.State == inplay.StateDumping || e.State == inplay.StateExhausted) &&
		(adverseFlow || !supportiveFlow):
		state.ExhaustionConfirmed = true
		state.TightenReason = "RUNNER_EXHAUST_SLOPE_COLLAPSE"
	}
	healthyPullback := !adverseFlow &&
		e.ScoreSlope >= pullbackSlopeMin &&
		(e.State == inplay.StateBalanced || e.State == inplay.StateHeating || e.State == inplay.StateInPlay || e.State == inplay.StatePumping)
	trendStillAccepted := !adverseFlow &&
		e.DrawdownFromPeakPct >= -maxHealthyDrawdown &&
		e.VWAPDistancePct >= minTrendHoldDistancePct &&
		e.EMADistancePct >= minTrendHoldDistancePct &&
		(e.State == inplay.StateBalanced || e.State == inplay.StateHeating || e.State == inplay.StateInPlay || e.State == inplay.StatePumping || e.State == inplay.StateCooling)
	if healthyPullback {
		state.StructureBroken = false
		if strings.HasPrefix(state.TightenReason, "RUNNER_EXHAUST") {
			state.ExhaustionConfirmed = false
			state.TightenReason = ""
		}
	}
	if trendStillAccepted {
		state.StructureBroken = false
		if strings.HasPrefix(state.TightenReason, "RUNNER_EXHAUST") {
			state.ExhaustionConfirmed = false
			state.TightenReason = ""
		}
	}
	if supportiveFlow && e.Momentum && (e.State == inplay.StateHeating || e.State == inplay.StateInPlay || e.State == inplay.StatePumping) {
		state.StructureBroken = false
		if strings.HasPrefix(state.TightenReason, "RUNNER_EXHAUST") {
			state.ExhaustionConfirmed = false
			state.TightenReason = ""
		}
	}
	if state.StructureBroken && state.FullExitReason == "" {
		state.FullExitReason = "RUNNER_STRUCTURE_LOST"
	}
	if state.ExhaustionConfirmed && state.TightenReason == "" {
		state.TightenReason = "RUNNER_EXHAUSTION_TIGHTEN"
	}
	return state
}

func preEODExitReason(side string, mv momentumView, upnlPct, upnlPctMax float64) string {
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

func userDataSnapshotFresh(state *aster.UserDataState) (aster.UserDataSnapshot, bool) {
	if state == nil {
		return aster.UserDataSnapshot{}, false
	}
	snap := state.Snapshot()
	if snap.UpdatedAt.IsZero() {
		return aster.UserDataSnapshot{}, false
	}
	maxAge := time.Duration(envInt("LIVE_USERDATA_STREAM_MAX_STALE_SEC", 120)) * time.Second
	if maxAge <= 0 {
		maxAge = 120 * time.Second
	}
	if time.Since(snap.UpdatedAt) > maxAge {
		return aster.UserDataSnapshot{}, false
	}
	if len(snap.Balances) == 0 && len(snap.Positions) == 0 {
		return aster.UserDataSnapshot{}, false
	}
	return snap, true
}

func accountSnapshotFromUserData(snap aster.UserDataSnapshot, assets []string) accountSnapshot {
	out := accountSnapshot{}
	for _, asset := range assets {
		_ = asset
	}
	if usdt, ok := snap.Balances["USDT"]; ok {
		out.AvailableUSDT = firstPositive(usdt.CrossWallet, usdt.WalletBalance)
		out.Balances = append(out.Balances, aster.Balance{
			Asset:             "USDT",
			Balance:           usdt.WalletBalance,
			AvailableBalance:  firstPositive(usdt.CrossWallet, usdt.WalletBalance),
			CrossUnPnl:        0,
			MaxWithdrawAmount: firstPositive(usdt.CrossWallet, usdt.WalletBalance),
		})
	}
	for _, p := range snap.Positions {
		amt := p.PositionAmt
		if abs(amt) <= 1e-10 {
			continue
		}
		side := "LONG"
		if amt < 0 {
			side = "SHORT"
		}
		if p.PositionSide == "SHORT" {
			side = "SHORT"
		} else if p.PositionSide == "LONG" {
			side = "LONG"
		}
		out.Positions = append(out.Positions, positionView{
			Symbol:   strings.ToUpper(strings.TrimSpace(p.Symbol)),
			Side:     side,
			Margin:   maxFloat(p.IsolatedWallet, 0),
			SizeAbs:  abs(amt),
			Entry:    p.EntryPrice,
			Mark:     0,
			Unreal:   p.UnrealizedPnL,
			Leverage: 0,
		})
	}
	sort.Slice(out.Positions, func(i, j int) bool {
		return out.Positions[i].Unreal > out.Positions[j].Unreal
	})
	return out
}

func fetchAccountSnapshot(rest *aster.RESTAuth, userData *aster.UserDataState, assets []string) (accountSnapshot, error) {
	if snap, ok := userDataSnapshotFresh(userData); ok {
		return accountSnapshotFromUserData(snap, assets), nil
	}
	snap := accountSnapshot{}
	bals, err := cachedBalances(rest)
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
	rows, err := cachedPositionRisk(rest, "")
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

func (m *liveExecManager) runLiveAccountSnapshotLoop() {
	if m == nil || m.rest == nil {
		return
	}
	m.refreshLiveAccountSnapshot(time.Now().UTC())
	ticker := time.NewTicker(m.snapshotPoll)
	defer ticker.Stop()
	for now := range ticker.C {
		m.refreshLiveAccountSnapshot(now.UTC())
	}
}

func (m *liveExecManager) refreshLiveAccountSnapshot(now time.Time) {
	if m == nil || m.rest == nil {
		return
	}
	snap, err := fetchAccountSnapshot(m.rest, m.userDataState, m.accountAssets)
	if err != nil {
		return
	}
	symbols := make([]string, 0, len(snap.Positions))
	for _, p := range snap.Positions {
		if strings.TrimSpace(p.Symbol) != "" {
			symbols = append(symbols, strings.ToUpper(strings.TrimSpace(p.Symbol)))
		}
	}
	m.syncMarketStreams(symbols)
	merged := m.mergeLiveAccountSnapshot(now, snap)
	m.mu.Lock()
	m.liveAccount = merged
	m.mu.Unlock()
}

func (m *liveExecManager) syncMarketStreams(symbols []string) {
	if m == nil {
		return
	}
	want := map[string]struct{}{}
	for _, sym := range symbols {
		raw := strings.ToUpper(strings.TrimSpace(sym))
		if raw != "" {
			want[raw] = struct{}{}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for sym, cancel := range m.marketCancels {
		if _, ok := want[sym]; ok {
			continue
		}
		cancel()
		delete(m.marketCancels, sym)
		delete(m.marketStates, sym)
	}
	for sym := range want {
		if _, ok := m.marketStates[sym]; ok {
			continue
		}
		state := aster.NewMarketState(sym, 50)
		client := aster.NewStreamClient(sym, m.wsLevels, m.wsSpeed, state)
		ctx, cancel := context.WithCancel(context.Background())
		m.marketStates[sym] = state
		m.marketCancels[sym] = cancel
		go func() {
			_ = client.Run(ctx)
		}()
	}
}

func (m *liveExecManager) priceQuoteForSymbol(symbol string, remoteMark float64) livePriceQuote {
	raw := strings.ToUpper(strings.TrimSpace(symbol))
	q := livePriceQuote{Symbol: raw, MarkPrice: remoteMark}
	if m == nil || raw == "" {
		return q
	}
	m.mu.RLock()
	state := m.marketStates[raw]
	m.mu.RUnlock()
	if state == nil {
		return q
	}
	bid, ask, mid, _, _, trades := state.SnapshotTop(5)
	q.BidPrice = bid
	q.AskPrice = ask
	if len(trades) > 0 {
		q.LastPrice = trades[0].Price
	}
	if q.LastPrice <= 0 {
		q.LastPrice = mid
	}
	if bid > 0 && ask > 0 {
		q.SpreadBps = ((ask - bid) / ((ask + bid) / 2)) * 10000.0
	}
	q.UpdatedAt = time.Now().UTC()
	return q
}

func (m *liveExecManager) mergeLiveAccountSnapshot(now time.Time, acct accountSnapshot) liveAccountSnapshot {
	out := liveAccountSnapshot{
		Generated:     now,
		AvailableUSDT: acct.AvailableUSDT,
		RealizedDay:   m.dayRealizedAt(now),
		Positions:     make([]liveAccountPosition, 0, len(acct.Positions)),
	}
	priceGuarded := make([]string, 0)
	localBySymbol := map[string]*livePosition{}
	if m != nil {
		for sym, pos := range m.positions {
			if pos == nil || !m.isActive(pos) {
				continue
			}
			key := positionLookupKey(sym, pos.Side)
			localBySymbol[key] = pos
		}
	}
	for _, rp := range acct.Positions {
		raw := strings.ToUpper(strings.TrimSpace(rp.Symbol))
		if raw == "" {
			continue
		}
		lp := localBySymbol[positionLookupKey(raw, rp.Side)]
		src := "MANUAL"
		manageState := ""
		protectionState := ""
		stop := 0.0
		holdMin := 0.0
		entryReason := ""
		if lp != nil {
			src = displayEntrySource(lp.EntrySource)
			manageState = strings.TrimSpace(lp.ManualManageState)
			protectionState = manualProtectionStatus(lp)
			if protectionState == "" {
				if lp.StopOrderID > 0 || lp.Protected {
					protectionState = "PROTECTED"
				} else {
					protectionState = "UNPROTECTED"
				}
			}
			stop = lp.StopPrice
			holdMin = now.Sub(lp.CreatedAt).Minutes()
			if holdMin < 0 {
				holdMin = 0
			}
			entryReason = displayEntryReason(lp.EntryReason)
		}
		quote := m.priceQuoteForSymbol(raw, rp.Mark)
		markPx := rp.Mark
		if markPx <= 0 {
			markPx = quote.LastPrice
		}
		lastPx := quote.LastPrice
		if lastPx <= 0 {
			lastPx = markPx
		}
		refPx := firstPositive(func() float64 {
			if lp != nil && lp.LastMark > 0 {
				return lp.LastMark
			}
			return 0
		}(), rp.Entry, markPx, lastPx)
		if fixed, guarded := sanitizeSnapshotPrice(refPx, markPx); guarded {
			markPx = fixed
			priceGuarded = appendUniqueDecisionReason(priceGuarded, raw)
		}
		if fixed, guarded := sanitizeSnapshotPrice(refPx, lastPx); guarded {
			lastPx = fixed
			priceGuarded = appendUniqueDecisionReason(priceGuarded, raw)
		}
		unreal := rp.Unreal
		if lastPx > 0 {
			unreal, _ = realizedFromFill(rp.Side, rp.Entry, lastPx, rp.SizeAbs)
		}
		unrealPct := 0.0
		if rp.Margin > 0 {
			unrealPct = (unreal / rp.Margin) * 100.0
		} else if rp.Entry > 0 && rp.SizeAbs > 0 {
			notional := rp.Entry * rp.SizeAbs
			if notional > 0 {
				unrealPct = (unreal / notional) * 100.0
			}
		}
		pos := liveAccountPosition{
			Symbol:           raw,
			Side:             rp.Side,
			Source:           src,
			ManageState:      manageState,
			ProtectionState:  protectionState,
			Managed:          lp != nil && (lp.Managed || botManagedPosition(lp)),
			Protected:        lp != nil && (lp.Protected || hasLiveProtectiveOrder(lp)),
			Qty:              rp.SizeAbs,
			EntryPrice:       rp.Entry,
			MarkPrice:        markPx,
			LastPrice:        lastPx,
			SpreadBps:        quote.SpreadBps,
			UnrealizedPnL:    unreal,
			UnrealizedPnLPct: unrealPct,
			RealizedPnL: func() float64 {
				if lp != nil {
					return lp.RealizedPnL
				}
				return 0
			}(),
			ExchangeUnreal: rp.Unreal,
			Leverage:       maxInt(int(rp.Leverage), 1),
			Margin:         rp.Margin,
			StopPrice:      stop,
			HoldMin:        holdMin,
			EntryReason:    entryReason,
		}
		out.OpenPnL += unreal
		if src == "MANUAL" {
			out.ManualCount++
		} else {
			out.BotCount++
		}
		out.Positions = append(out.Positions, pos)
	}
	out.OpenCount = len(out.Positions)
	sort.Slice(out.Positions, func(i, j int) bool {
		return abs(out.Positions[i].UnrealizedPnL) > abs(out.Positions[j].UnrealizedPnL)
	})
	if len(priceGuarded) > 0 {
		sort.Strings(priceGuarded)
		out.Health = "partial_price_guard"
		out.HealthDetail = strings.Join(priceGuarded, ",")
	}
	out.Equity = acct.AvailableUSDT + out.OpenPnL
	return out
}

func (m *liveExecManager) LiveAccountSnapshot(limit int) liveAccountSnapshot {
	if m == nil {
		return liveAccountSnapshot{Generated: time.Now().UTC()}
	}
	m.mu.RLock()
	out := m.liveAccount
	m.mu.RUnlock()
	if limit > 0 && len(out.Positions) > limit {
		out.Positions = append([]liveAccountPosition(nil), out.Positions[:limit]...)
	}
	return out
}

func (m *liveExecManager) LivePositionBySymbol(symbol string) (liveAccountPosition, bool) {
	if m == nil {
		return liveAccountPosition{}, false
	}
	input := strings.ToUpper(strings.TrimSpace(symbol))
	raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(input)))
	if raw == "" {
		raw = input
	}
	rawBase := strings.TrimSuffix(strings.TrimSuffix(raw, "USDT"), "USD")
	snap := m.LiveAccountSnapshot(0)
	for _, p := range snap.Positions {
		posRaw := strings.ToUpper(strings.TrimSpace(p.Symbol))
		posBase := strings.TrimSuffix(strings.TrimSuffix(posRaw, "USDT"), "USD")
		if strings.EqualFold(posRaw, raw) || strings.EqualFold(posBase, rawBase) {
			return p, true
		}
	}
	return liveAccountPosition{}, false
}

func printAccountSnapshot(snap accountSnapshot, realizedToday float64) {
	openPnL := 0.0
	for _, p := range snap.Positions {
		openPnL += p.Unreal
	}
	fmt.Printf("ACCOUNT avail=%.2f eq=%.2f pnl=%+.2f day=%+.2f open=%d\n",
		snap.AvailableUSDT, accountEquity(snap), openPnL, realizedToday+openPnL, len(snap.Positions))
	if len(snap.Positions) == 0 {
		return
	}
	fmt.Println("  +------+------------+-------+-----------+------------+------------+----------+---------+-----+")
	fmt.Println("  | src  | symbol     | side  | qty       | entry      | mark       | uPnL     | uPnL%   | lev |")
	for i := 0; i < len(snap.Positions); i++ {
		p := snap.Positions[i]
		fmt.Println(formatConsolePositionLine("live", p.Symbol, p.Side, p.SizeAbs, p.Entry, p.Mark, p.Unreal, int(p.Leverage)))
	}
	fmt.Println("  +------+------------+-------+-----------+------------+------------+----------+---------+-----+")
}

func printScanHeader(now time.Time) {
	fmt.Printf("\n[%s %s]\n", now.Format("15:04:05"), sessionTag(now))
}

func formatInPlaySummary(tag string, entries []inplay.Entry) string {
	if len(entries) == 0 {
		return fmt.Sprintf("%-6s none", tag+":")
	}
	parts := make([]string, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		parts = append(parts, formatInPlayEntry(entries[i]))
	}
	return fmt.Sprintf("%-6s %s", tag+":", strings.Join(parts, " | "))
}

func formatInPlayEntry(e inplay.Entry) string {
	sym := strings.ToUpper(aster.RawSymbol(e.Symbol))
	if sym == "" {
		sym = strings.ToUpper(strings.TrimSpace(e.Symbol))
	}
	state := strings.ToUpper(strings.TrimSpace(displayState(e.SideBias, e.State)))
	stateClr := inPlayStateColor(inplay.State(displayState(e.SideBias, e.State)))
	grade := strings.ToUpper(strings.TrimSpace(e.CurrentGrade))
	if grade != "" && grade != "N/A" {
		return fmt.Sprintf("%s %s%s%s %.1f %s%s%s",
			sym,
			market.GradeColor(grade), grade, market.ResetColor(),
			e.CurrentScore,
			stateClr, state, market.ResetColor(),
		)
	}
	return fmt.Sprintf("%s %.1f %s%s%s", sym, e.CurrentScore, stateClr, state, market.ResetColor())
}

func inPlayStateColor(s inplay.State) string {
	switch s {
	case inplay.StatePumping:
		return "\033[31m" // red
	case inplay.StateInPlay:
		return "\033[32m" // green
	case inplay.StateHeating:
		return "\033[38;5;214m" // amber
	case inplay.StateBalanced:
		return "\033[36m" // cyan
	case inplay.StateCooling:
		return "\033[37m" // gray
	case inplay.StateDumping:
		return "\033[35m" // magenta
	case inplay.StateExhausted:
		return "\033[90m" // dark gray
	default:
		return "\033[37m"
	}
}

func formatConsolePositionLine(scope, symbol, side string, size, entry, mark, upnl float64, lev int) string {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if lev <= 0 {
		lev = 1
	}
	denom := entry * size
	upnlPct := 0.0
	if denom > 0 {
		upnlPct = (upnl / denom) * 100.0
	}
	pnlColor := "\033[37m"
	if upnl > 0 {
		pnlColor = "\033[32m"
	} else if upnl < 0 {
		pnlColor = "\033[31m"
	}
	return fmt.Sprintf("  | %-4s | %-10s | %-5s | %9.4f | %10s | %10s | %s%+8.2f%s | %s%+7.2f%%%s | %3dx |",
		scope, sym, strings.ToUpper(strings.TrimSpace(side)), size, fmtPrice(entry), fmtPrice(mark), pnlColor, upnl, market.ResetColor(), pnlColor, upnlPct, market.ResetColor(), lev)
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
	bals, err := cachedBalances(rest)
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
	rows, err := cachedPositionRisk(rest, "")
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

func countOpenPositionsBySide(acct accountSnapshot, side string) int {
	want := strings.ToUpper(strings.TrimSpace(side))
	if want == "" {
		return 0
	}
	accountSide := "LONG"
	if want == "SELL" || want == "SHORT" {
		accountSide = "SHORT"
	}
	n := 0
	for _, p := range acct.Positions {
		if strings.EqualFold(strings.TrimSpace(p.Side), accountSide) {
			n++
		}
	}
	return n
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
	fmt.Printf("live: placed entry %s %s qty=%s price=%s -> %v\n",
		rawSym, c.Side, vals.Get("quantity"), vals.Get("price"), out)
	return nil
}

func loadSafetyConfig(reserveUSDT, tradeMargin float64) safetyConfig {
	starterMargin := envFloat("LIVE_STARTER_USDT", envFloat("LIVE_ENTRY_STARTER_USDT", maxFloat(tradeMargin, 10)))
	if starterMargin <= 0 {
		starterMargin = maxFloat(tradeMargin, 10)
	}
	minAvail := envFloat("LIVE_MIN_AVAILABLE_USDT", starterMargin)
	maxLev := envInt("LIVE_MAX_LEVERAGE", 2)
	if maxLev <= 0 {
		maxLev = 2
	}
	maxOrders := envInt("LIVE_MAX_ORDERS_PER_DAY", 2)
	if maxOrders < 0 {
		maxOrders = 0
	}
	maxOrdersHour := envInt("LIVE_MAX_ORDERS_PER_HOUR", 1)
	if maxOrdersHour < 0 {
		maxOrdersHour = 0
	}
	coolSec := envInt("LIVE_ORDER_COOLDOWN_SEC", 180)
	if coolSec < 0 {
		coolSec = 0
	}
	sameSideCoolSec := envInt("LIVE_SYMBOL_COOLDOWN_SAME_SIDE_SEC", envInt("LIVE_SYMBOL_COOLDOWN_SEC", 900))
	if sameSideCoolSec < 0 {
		sameSideCoolSec = 0
	}
	flipSideCoolSec := envInt("LIVE_SYMBOL_COOLDOWN_FLIP_SIDE_SEC", 120)
	if flipSideCoolSec < 0 {
		flipSideCoolSec = 0
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
	contextOnly := envCSV("LIVE_CONTEXT_ONLY_SYMBOLS", "")
	contextOnlyMap := make(map[string]struct{}, len(contextOnly))
	for _, s := range contextOnly {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(s)))
		if raw != "" {
			contextOnlyMap[raw] = struct{}{}
		}
	}
	// Symbols listed here are always tradable even if accidentally present in context-only.
	contextTradable := envCSV("LIVE_CONTEXT_TRADEABLE_SYMBOLS", "BTCUSDT,ETHUSDT,SOLUSDT")
	for _, s := range contextTradable {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(s)))
		if raw != "" {
			delete(contextOnlyMap, raw)
		}
	}
	return safetyConfig{
		enableLiveTrading:      envBool("LIVE_ENABLE_LIVE_TRADING", false),
		maxLeverage:            maxLev,
		minAvailUSDT:           minAvail,
		maxOrdersPerDay:        maxOrders,
		maxOrdersPerHour:       maxOrdersHour,
		orderCooldown:          time.Duration(coolSec) * time.Second,
		symbolCooldownSameSide: time.Duration(sameSideCoolSec) * time.Second,
		symbolCooldownFlipSide: time.Duration(flipSideCoolSec) * time.Second,
		stopoutWindow:          time.Duration(stopoutWindowMin) * time.Minute,
		stopoutLock:            time.Duration(stopoutLockMin) * time.Minute,
		stopoutCount:           stopoutCount,
		pauseFile:              envStr("LIVE_PAUSE_FILE", "/tmp/live.pause"),
		allowSymbols:           allowMap,
		blockSymbols:           blockMap,
		contextOnlySymbols:     contextOnlyMap,
		allowShorts:            envBool("LIVE_ALLOW_SHORTS", true),
		maxDailyLossPct:        maxDailyLossPct,
		killClose:              envBool("LIVE_KILL_CLOSE_POSITIONS", false),
	}
}

func safetyReject(cfg safetyConfig, c candidate, now, lastOrderAt time.Time, lastBySymbol map[string]time.Time, lastBySymbolSide map[string]time.Time, byDay, byHour map[string]int, stopoutLockUntil map[string]time.Time) string {
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
	if envBool("LIVE_CONTEXT_ONLY_ENFORCE", false) {
		if _, contextOnly := cfg.contextOnlySymbols[sym]; contextOnly {
			return "context_only_symbol"
		}
	}
	if len(cfg.allowSymbols) > 0 {
		if _, ok := cfg.allowSymbols[sym]; !ok {
			return "symbol not in allowlist"
		}
	}
	if cfg.orderCooldown > 0 && !lastOrderAt.IsZero() && now.Sub(lastOrderAt) < cfg.orderCooldown {
		return "order cooldown active"
	}
	if cfg.symbolCooldownSameSide > 0 {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
		sideKey := raw + "|" + strings.ToUpper(strings.TrimSpace(c.Side))
		if t := lastBySymbolSide[sideKey]; !t.IsZero() && now.Sub(t) < cfg.symbolCooldownSameSide {
			return "symbol same-side cooldown active"
		}
	}
	if cfg.symbolCooldownFlipSide > 0 {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol)))
		if t := lastBySymbol[raw]; !t.IsZero() && now.Sub(t) < cfg.symbolCooldownFlipSide {
			return "symbol flip-side cooldown active"
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

func restAuthConfigFromConfig() (aster.RESTAuthConfig, bool) {
	fileKV := getConfigKV()

	key := cfgGet(fileKV, "ASTER_API_KEY", "aster_api_key", "api_key", "key")
	sec := cfgGet(fileKV, "ASTER_API_SECRET", "aster_api_secret", "api_secret", "secret")
	authMode := strings.ToLower(cfgGet(fileKV, "ASTER_AUTH_MODE", "aster_auth_mode", "auth_mode"))
	if authMode == "" {
		authMode = "auto"
	}
	user := cfgGet(fileKV, "ASTER_USER", "aster_user", "user")
	signer := cfgGet(fileKV, "ASTER_SIGNER", "aster_signer", "signer")
	priv := cfgGet(fileKV, "ASTER_PRIVATE_KEY", "aster_private_key", "private_key")
	chainID := int64(0)
	rawChainID := cfgGet(fileKV, "ASTER_CHAIN_ID", "aster_chain_id", "chain_id")
	chainIDSet := strings.TrimSpace(rawChainID) != ""
	if rawChainID != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(rawChainID), 10, 64); err == nil {
			chainID = n
		}
	}
	hasHMAC := key != "" && sec != ""
	hasAgent := user != "" && signer != "" && priv != ""
	if !hasHMAC && !hasAgent {
		return aster.RESTAuthConfig{}, false
	}
	baseURL := effectiveRESTBaseURL()
	return aster.RESTAuthConfig{
		APIKey:     key,
		APISecret:  sec,
		User:       user,
		Signer:     signer,
		PrivateKey: priv,
		AuthMode:   authMode,
		ChainID:    chainID,
		ChainIDSet: chainIDSet,
		BaseURL:    baseURL,
	}, true
}

func buildRESTFromConfig() *aster.RESTAuth {
	cfg, ok := restAuthConfigFromConfig()
	if !ok {
		return nil
	}
	rest := aster.NewRESTAuthWithConfig(cfg)
	summary := rest.StartupAuthSummary()
	fmt.Printf("live auth: mode=%v source=%v explicit=%v base=%v chain=%v user=%v signer=%v\n",
		summary["auth_mode"], summary["auth_source"], summary["auth_explicit"], summary["base_url"], summary["chain_id"], summary["user"], summary["signer"])
	if cfgPath := resolveConfigPath(); cfgPath != "" {
		fmt.Printf("live auth: config_path=%s\n", cfgPath)
	}
	if err := rest.ConfigError(); err != nil {
		fmt.Println("live auth config error:", err)
		return nil
	}
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
		fmt.Printf("live: overriding testnet URL %q with mainnet https://fapi.asterdex.com (set LIVE_ALLOW_TESTNET=1 to bypass)\n", baseURL)
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

var signedUserDataBackoffState struct {
	mu    sync.Mutex
	until time.Time
}

func signedUserDataBackoffDuration() time.Duration {
	sec := envInt("LIVE_SIGNED_USERDATA_BACKOFF_SEC", 60)
	if sec < 5 {
		sec = 5
	}
	return time.Duration(sec) * time.Second
}

func signedUserDataBackoffCheck(now time.Time) error {
	signedUserDataBackoffState.mu.Lock()
	defer signedUserDataBackoffState.mu.Unlock()
	if now.Before(signedUserDataBackoffState.until) {
		return fmt.Errorf("signed user-data backoff active until %s", signedUserDataBackoffState.until.UTC().Format(time.RFC3339))
	}
	return nil
}

func signedUserDataBackoffActive(now time.Time) bool {
	signedUserDataBackoffState.mu.Lock()
	defer signedUserDataBackoffState.mu.Unlock()
	return now.Before(signedUserDataBackoffState.until)
}

func signedUserDataBackoffObserve(now time.Time, err error) {
	if !isSignedUserDataRateLimitErr(err) {
		return
	}
	signedUserDataBackoffState.mu.Lock()
	defer signedUserDataBackoffState.mu.Unlock()
	until := now.Add(signedUserDataBackoffDuration())
	if until.After(signedUserDataBackoffState.until) {
		signedUserDataBackoffState.until = until
	}
}

func isSignedUserDataRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	raw := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(raw, "http 429") || strings.Contains(raw, "rate limit")
}

func scannerRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	raw := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(raw, "status 429") ||
		strings.Contains(raw, "status 418") ||
		strings.Contains(raw, "http 429") ||
		strings.Contains(raw, "http 418") ||
		strings.Contains(raw, "rate limit") ||
		strings.Contains(raw, "waf limit")
}

func waitForNextCycle(cycleStart time.Time, scanEvery, reconEvery time.Duration, execMgr *liveExecManager) {
	next := cycleStart.Add(scanEvery)
	for {
		rem := time.Until(next)
		if rem <= 0 {
			liveWakeReason = "scan"
			return
		}
		sleepFor := rem
		if execMgr != nil && reconEvery > 0 && sleepFor > reconEvery {
			sleepFor = reconEvery
		}
		if liveWatchTick != nil && liveWatchEvery > 0 && sleepFor > liveWatchEvery {
			sleepFor = liveWatchEvery
		}
		if livePriorityActive != nil && livePriorityEvery > 0 && livePriorityActive() && sleepFor > livePriorityEvery {
			sleepFor = livePriorityEvery
		}
		time.Sleep(sleepFor)
		if execMgr != nil {
			execMgr.Reconcile(time.Now().UTC(), nil, nil, nil)
		}
		if liveWatchTick != nil && liveWatchEvery > 0 {
			if liveWatchTick(time.Now().UTC()) {
				liveWakeReason = "watcher"
				return
			}
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

func riskSizedMarginUSDT(entry, stop float64, lev int, equity float64) float64 {
	if entry <= 0 || stop <= 0 || lev <= 0 || equity <= 0 {
		return 0
	}
	riskPct := math.Abs(entry-stop) / entry
	if riskPct <= 0 {
		return 0
	}
	riskPerTradePct := envFloat("LIVE_RISK_PER_TRADE_PCT", 1.0)
	if riskPerTradePct <= 0 {
		riskPerTradePct = 1.0
	}
	riskBudgetUSDT := equity * (riskPerTradePct / 100.0)
	if riskBudgetUSDT <= 0 {
		return 0
	}
	margin := riskBudgetUSDT / (float64(lev) * riskPct)
	if !isFiniteFloat(margin) || margin <= 0 {
		return 0
	}
	maxMargin := envFloat("LIVE_RISK_SIZING_MAX_MARGIN_USDT", 0)
	if maxMargin > 0 && margin > maxMargin {
		margin = maxMargin
	}
	return margin
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func priceSanityGuardEnabled() bool {
	return envBool("LIVE_PRICE_SANITY_GUARD_ENABLE", true)
}

func priceSanityMaxDeviation() float64 {
	pct := envFloat("LIVE_PRICE_SANITY_MAX_DEVIATION_PCT", 25.0)
	if pct <= 0 {
		pct = 25.0
	}
	return pct / 100.0
}

func sanitizeSnapshotPrice(ref, candidate float64) (float64, bool) {
	if ref <= 0 || candidate <= 0 || !priceSanityGuardEnabled() {
		return candidate, false
	}
	if math.Abs(candidate-ref)/maxFloat(ref, candidate) > priceSanityMaxDeviation() {
		return ref, true
	}
	return candidate, false
}

func normalizePositionSide(side string) string {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "BUY", "LONG":
		return "LONG"
	case "SELL", "SHORT":
		return "SHORT"
	default:
		return strings.ToUpper(strings.TrimSpace(side))
	}
}

func isLongSide(side string) bool {
	return normalizePositionSide(side) == "LONG"
}

func samePositionSide(a, b string) bool {
	return normalizePositionSide(a) == normalizePositionSide(b)
}

func displayPositionSide(side string) string {
	return normalizePositionSide(side)
}

func parseUserPositionSide(side string) (string, bool) {
	switch normalizePositionSide(side) {
	case "LONG":
		return "BUY", true
	case "SHORT":
		return "SELL", true
	default:
		return "", false
	}
}

func displayEntrySource(source string) string {
	switch strings.ToUpper(strings.TrimSpace(source)) {
	case "", "BOT":
		return "BOT"
	case manualEntrySourcePassive:
		return "MANUAL"
	case manualEntrySourceManaged:
		return "MANUAL_MANAGED"
	default:
		return strings.ToUpper(strings.TrimSpace(source))
	}
}

func displayManageState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case manualManageStatePassive:
		return "PASSIVE"
	case manualManageStatePendingProtection:
		return "PENDING_PROTECTION"
	case manualManageStateDegraded:
		return "PROTECT_RETRY"
	case manualManageStateLive:
		return "LIVE"
	case manualManageStateConflict:
		return "CONFLICT"
	default:
		return strings.ToUpper(strings.TrimSpace(state))
	}
}

func isAmbiguousVenueOutcome(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(err.Error()))
	if s == "" {
		return false
	}
	return strings.Contains(s, "503") ||
		strings.Contains(s, "service unavailable") ||
		strings.Contains(s, "unknown") ||
		strings.Contains(s, "timeout")
}

func logUnknownExecution(symbol, intentID, phase, detail string) {
	log.Printf("exec_unknown symbol=%s intent_id=%s phase=%q detail=%q",
		strings.ToUpper(strings.TrimSpace(symbol)),
		strings.TrimSpace(intentID),
		phase,
		detail)
}

func logUnknownReconcile(symbol, intentID, result string) {
	log.Printf("exec_unknown_reconcile symbol=%s intent_id=%s result=%q",
		strings.ToUpper(strings.TrimSpace(symbol)),
		strings.TrimSpace(intentID),
		result)
}

func boolLabel(v bool) string {
	if v {
		return "YES"
	}
	return "NO"
}

func displayEntryReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "":
		return "none"
	case manualEntryReasonManaged:
		return "MANUAL_MANAGED"
	case operatorEntryReason:
		return "OPERATOR_EXECUTION"
	default:
		return strings.TrimSpace(reason)
	}
}

func sessionPhaseUTC(ts time.Time) utcSessionPhase {
	utc := ts.UTC()
	hour, min := utc.Hour(), utc.Minute()
	switch {
	case inMinuteWindow(hour, min, 7, 0, 10, 0):
		return sessionLondonOpen
	case inMinuteWindow(hour, min, 10, 0, 13, 0):
		return sessionLondonExpand
	case inMinuteWindow(hour, min, 13, 0, 16, 0):
		return sessionNYOpen
	case inMinuteWindow(hour, min, 16, 0, 20, 0):
		return sessionNYExpand
	case inMinuteWindow(hour, min, 2, 0, 5, 0):
		return sessionAsiaBreakout
	case inMinuteWindow(hour, min, 5, 0, 8, 0):
		return sessionAsiaContinue
	case inMinuteWindow(hour, min, 0, 0, 2, 0):
		return sessionAsiaDev
	default:
		return sessionUTCOffHours
	}
}

var majorMarketVenues = []majorMarketVenue{
	{Name: "ASX", Timezone: "Australia/Sydney", OpenHour: 10, OpenMin: 0, CloseHour: 16, CloseMin: 0},
	{Name: "TSE", Timezone: "Asia/Tokyo", OpenHour: 9, OpenMin: 0, CloseHour: 15, CloseMin: 0, BreakStart: 11*60 + 30, BreakEnd: 12*60 + 30},
	{Name: "HKEX", Timezone: "Asia/Hong_Kong", OpenHour: 9, OpenMin: 30, CloseHour: 16, CloseMin: 0, BreakStart: 12 * 60, BreakEnd: 13 * 60},
	{Name: "SSE", Timezone: "Asia/Shanghai", OpenHour: 9, OpenMin: 30, CloseHour: 15, CloseMin: 0, BreakStart: 11*60 + 30, BreakEnd: 13 * 60},
	{Name: "SZSE", Timezone: "Asia/Shanghai", OpenHour: 9, OpenMin: 30, CloseHour: 15, CloseMin: 0, BreakStart: 11*60 + 30, BreakEnd: 13 * 60},
	{Name: "SGX", Timezone: "Asia/Singapore", OpenHour: 9, OpenMin: 0, CloseHour: 17, CloseMin: 0},
	{Name: "NSE", Timezone: "Asia/Kolkata", OpenHour: 9, OpenMin: 15, CloseHour: 15, CloseMin: 30},
	{Name: "LSE", Timezone: "Europe/London", OpenHour: 8, OpenMin: 0, CloseHour: 16, CloseMin: 30},
	{Name: "XETRA", Timezone: "Europe/Berlin", OpenHour: 9, OpenMin: 0, CloseHour: 17, CloseMin: 30},
	{Name: "EURONEXT", Timezone: "Europe/Paris", OpenHour: 9, OpenMin: 0, CloseHour: 17, CloseMin: 30},
	{Name: "NYSE", Timezone: "America/New_York", OpenHour: 9, OpenMin: 30, CloseHour: 16, CloseMin: 0},
	{Name: "NASDAQ", Timezone: "America/New_York", OpenHour: 9, OpenMin: 30, CloseHour: 16, CloseMin: 0},
	{Name: "TSX", Timezone: "America/Toronto", OpenHour: 9, OpenMin: 30, CloseHour: 16, CloseMin: 0},
}

func marketVenueStatus(ts time.Time, venue majorMarketVenue) string {
	loc, err := time.LoadLocation(venue.Timezone)
	if err != nil {
		return "closed"
	}
	local := ts.In(loc)
	switch local.Weekday() {
	case time.Saturday, time.Sunday:
		return "closed"
	}
	curMin := local.Hour()*60 + local.Minute()
	openMin := venue.OpenHour*60 + venue.OpenMin
	closeMin := venue.CloseHour*60 + venue.CloseMin
	if venue.BreakEnd > venue.BreakStart && curMin >= venue.BreakStart && curMin < venue.BreakEnd {
		return "break"
	}
	if curMin >= openMin && curMin < closeMin {
		return "open"
	}
	return "closed"
}

func marketVenueRecentlyClosed(ts time.Time, venue majorMarketVenue, within time.Duration) bool {
	loc, err := time.LoadLocation(venue.Timezone)
	if err != nil {
		return false
	}
	local := ts.In(loc)
	switch local.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	closeAt := time.Date(local.Year(), local.Month(), local.Day(), venue.CloseHour, venue.CloseMin, 0, 0, loc)
	if local.Before(closeAt) {
		return false
	}
	return local.Sub(closeAt) <= within
}

func regionStats(ts time.Time, venues ...majorMarketVenue) regionSessionStats {
	stats := regionSessionStats{}
	for _, venue := range venues {
		switch marketVenueStatus(ts, venue) {
		case "open":
			stats.OpenCount++
		case "break":
			stats.BreakCount++
		}
		if marketVenueRecentlyClosed(ts, venue, 90*time.Minute) {
			stats.RecentlyClosed = true
		}
	}
	return stats
}

func sessionTagFromMajorMarkets(ts time.Time) string {
	switch ts.Weekday() {
	case time.Saturday, time.Sunday:
		return "WEEKEND"
	}
	asia := regionStats(ts,
		majorMarketVenues[0], majorMarketVenues[1], majorMarketVenues[2], majorMarketVenues[3],
		majorMarketVenues[4], majorMarketVenues[5], majorMarketVenues[6],
	)
	london := regionStats(ts, majorMarketVenues[7], majorMarketVenues[8], majorMarketVenues[9])
	us := regionStats(ts, majorMarketVenues[10], majorMarketVenues[11], majorMarketVenues[12])
	switch {
	case london.OpenCount > 0 && us.OpenCount > 0:
		return "LONDON_US_OVERLAP"
	case asia.OpenCount > 0 && london.OpenCount > 0:
		return "ASIA_LONDON_OVERLAP"
	case asia.BreakCount > 0:
		return "ASIA_BREAK"
	case us.OpenCount > 0:
		return "US_OPEN"
	case london.OpenCount > 0:
		return "LONDON_OPEN"
	case asia.OpenCount > 0:
		return "ASIA_OPEN"
	case us.RecentlyClosed:
		return "US_CLOSE"
	case london.RecentlyClosed:
		return "LONDON_CLOSE"
	case asia.RecentlyClosed:
		return "ASIA_CLOSE"
	default:
		return "OFF_HOURS"
	}
}

func sessionLowLiquidity(phase utcSessionPhase) bool {
	return phase == sessionAsiaDev || phase == sessionUTCOffHours
}

func isStopCloseReason(reason string) bool {
	r := strings.ToUpper(strings.TrimSpace(reason))
	return r == "STOP_HIT" || r == "SL" || r == "TRAIL_STOP" || strings.Contains(r, "STOP")
}

func isExhaustionCloseReason(reason string) bool {
	r := strings.ToUpper(strings.TrimSpace(reason))
	return strings.Contains(r, "EXHAUST") || strings.Contains(r, "RUNNER_EXHAUST") || strings.Contains(r, "LIQ_SPIKE")
}

func tradeReturnPct(p *livePosition) float64 {
	if p == nil {
		return 0
	}
	if p.DeployedMargin > 0 {
		return (p.RealizedPnL / p.DeployedMargin) * 100.0
	}
	if p.Margin > 0 {
		return (p.RealizedPnL / p.Margin) * 100.0
	}
	return 0
}

func markLivePositionClosed(p *livePosition, now time.Time, reason string) {
	if p == nil {
		return
	}
	p.State = execClosed
	p.CloseReason = reason
	p.ClosedAt = now
	p.UpdatedAt = now
	p.ExhaustionExit = isExhaustionCloseReason(reason)
	p.RunnerCaptureFailed = runnerCaptureFailed(p)
}

func (m *liveExecManager) markPositionClosed(now time.Time, p *livePosition, reason string) {
	markLivePositionClosed(p, now, reason)
	if m != nil {
		m.recordExecutionGovernorExit(now, p, reason)
	}
}

func sessionTag(ts time.Time) string {
	return sessionTagFromMajorMarkets(ts)
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

func newLiveStatusStore() *liveStatusStore { return &liveStatusStore{} }

func (s *liveStatusStore) Set(v liveStatus) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cur = v
	s.mu.Unlock()
}

func (s *liveStatusStore) Snapshot() liveStatus {
	if s == nil {
		return liveStatus{}
	}
	s.mu.RLock()
	cur := s.cur
	s.mu.RUnlock()
	cur.Paper = cloneLivePaperSnapshot(cur.Paper)
	return cur
}

func startLiveStatusServer(addr string, s *liveStatusStore) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Println("live status server:", addr)
	go func() {
		_ = http.Serve(ln, newLiveStatusMux(s))
	}()
	return nil
}

func newLiveStatusMux(s *liveStatusStore) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		defer func() {
			if recover() != nil {
				http.Error(w, `{"error":"status_snapshot_panic"}`+"\n", http.StatusInternalServerError)
			}
		}()
		_ = json.NewEncoder(w).Encode(s.Snapshot())
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		defer func() {
			if recover() != nil {
				http.Error(w, "status_snapshot_panic\n", http.StatusInternalServerError)
			}
		}()
		b, _ := json.MarshalIndent(s.Snapshot(), "", "  ")
		_, _ = w.Write(b)
	})
	return mux
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

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func csvValue(rec []string, idx map[string]int, key string) string {
	i, ok := idx[key]
	if !ok || i < 0 || i >= len(rec) {
		return ""
	}
	return rec[i]
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minPositive(a, b float64) float64 {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
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

func clampInt(v, lo, hi int) int {
	if hi < lo {
		lo, hi = hi, lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
	tradeable := base - reserve
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
		if tradeable <= 0 {
			return minV
		}
		m = tradeable / float64(slots)
	case "dynamic", "auto":
		// Dynamic mode is account-aware:
		// - byPct: scales with account (trade size % of base)
		// - bySlots: prevents oversizing against reserved capital/open-slot model
		if pct <= 0 {
			pct = 12
		}
		if slots <= 0 {
			slots = 5
		}
		if tradeable <= 0 {
			return minV
		}
		byPct := base * (pct / 100.0)
		bySlots := tradeable / float64(slots)
		if byPct <= 0 {
			byPct = bySlots
		}
		m = math.Min(byPct, bySlots)
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
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "percent":
		// handled below
	case "dynamic", "auto":
		// Dynamic reserve defaults to 50% of sizing base unless pct override is provided.
		if base <= 0 {
			return fixed
		}
		if pct <= 0 {
			pct = 50
		}
		if pct < 0 {
			pct = 0
		}
		if pct > 95 {
			pct = 95
		}
		r := base * (pct / 100.0)
		if r < fixed {
			r = fixed
		}
		if r > base {
			r = base
		}
		return r
	default:
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
		minLev = 3
	}
	if maxLev <= 0 {
		maxLev = 20
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

func isSymbolNotionalLimitError(err error) bool {
	if err == nil {
		return false
	}
	raw := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(raw, "\"code\":-5018") ||
		strings.Contains(raw, "maximum notional value limit") ||
		strings.Contains(raw, "reached the maximum notional value limit")
}

func isVenueOrderLegalityError(err error) bool {
	if err == nil {
		return false
	}
	raw := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(raw, "greater than max quantity") ||
		strings.Contains(raw, "less than min quantity") ||
		strings.Contains(raw, "step size") ||
		strings.Contains(raw, "min notional") ||
		strings.Contains(raw, "tick size") ||
		strings.Contains(raw, orderIllegalMaxQtyReason) ||
		strings.Contains(raw, orderIllegalMinQtyReason) ||
		strings.Contains(raw, orderIllegalStepSizeReason) ||
		strings.Contains(raw, orderIllegalMinNotionalReason) ||
		strings.Contains(raw, orderIllegalTickSizeReason)
}

func nextReducedLeverage(lev int) int {
	switch {
	case lev > 10:
		return 10
	case lev > 5:
		return 5
	case lev == 5:
		return 4
	case lev == 4:
		return 3
	case lev > 3:
		return 3
	default:
		return 0
	}
}

func leverageRetrySequence(startLev, minLev int) []int {
	if startLev <= 0 {
		return nil
	}
	if minLev <= 0 {
		minLev = 1
	}
	seen := map[int]bool{}
	out := []int{startLev}
	seen[startLev] = true
	cur := startLev
	for {
		next := nextReducedLeverage(cur)
		if next <= 0 || next < minLev || seen[next] {
			break
		}
		out = append(out, next)
		seen[next] = true
		cur = next
	}
	return out
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
		if wd, err := os.Getwd(); err == nil {
			if repo := findRepoRoot(wd); repo != "" {
				base = repo
			} else {
				base = wd
			}
		}
	}
	if base == "" {
		if exe, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exe)
			if filepath.Base(exeDir) == "bin" {
				exeDir = filepath.Dir(exeDir)
			}
			if !strings.HasPrefix(exeDir, os.TempDir()) && !strings.Contains(exeDir, "go-build") {
				base = exeDir
			}
		}
	}
	if base == "" {
		return filepath.Clean(s)
	}
	return filepath.Clean(filepath.Join(base, s))
}

func findRepoRoot(start string) string {
	dir := strings.TrimSpace(start)
	for dir != "" && dir != string(filepath.Separator) {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func symbolNamesFromEntries(entries []inplay.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Symbol)
	}
	return out
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

func activeRuntimeProfile() runtimeProfile {
	return runtimeProfile(strings.ToLower(strings.TrimSpace(envStr("LIVE_RUNTIME_PROFILE", ""))))
}

func resolveRuntimeProfileConfig() runtimeProfileConfig {
	cfg := runtimeProfileConfig{
		Name:                   activeRuntimeProfile(),
		EffectiveVPEnabled:     envBool("LIVE_ENABLE_VP_SETUPS", true),
		EffectiveInstitutional: envBool("LIVE_ENABLE_INSTITUTIONAL_PA", true),
		EffectiveReversal:      envBool("LIVE_ENABLE_MOMENTUM_REVERSAL", true),
		EffectiveImpulse: envBool("LIVE_ENABLE_RESET_IMPULSE", true) &&
			envBool("LIVE_ENABLE_IMPULSIVE_LONG_STARTER", true) &&
			envBool("LIVE_ENABLE_IMPULSIVE_SHORT_STARTER", true),
		EffectiveReentry:          envBool("LIVE_REENTRY_ENABLE", false),
		EffectiveRequireStructure: envBool("LIVE_CONT_REQUIRE_STRUCTURE_CONFIRM", true),
		EffectiveCandidateMemory:  envBool("LIVE_CANDIDATE_MEMORY_ENABLE", true),
		EffectiveTriggerMemory:    true,
		EffectiveSharedManagement: false,
	}
	switch cfg.Name {
	case runtimeProfilePaperContinuationClean:
		cfg.EffectiveVPEnabled = true
		cfg.EffectiveInstitutional = true
		cfg.EffectiveReversal = false
		cfg.EffectiveImpulse = false
		cfg.EffectiveReentry = false
		cfg.EffectiveRequireStructure = true
		cfg.EffectiveCandidateMemory = true
		cfg.EffectiveTriggerMemory = true
		cfg.EffectiveSharedManagement = true
	}
	return cfg
}

func effectiveVPSetupsEnabled() bool {
	return resolveRuntimeProfileConfig().EffectiveVPEnabled
}

func effectiveInstitutionalPAEnabled() bool {
	return resolveRuntimeProfileConfig().EffectiveInstitutional
}

func effectiveMomentumReversalEnabled() bool {
	return resolveRuntimeProfileConfig().EffectiveReversal
}

func effectiveResetImpulseEnabled() bool {
	cfg := resolveRuntimeProfileConfig()
	switch cfg.Name {
	case runtimeProfilePaperContinuationClean:
		return false
	default:
		return envBool("LIVE_ENABLE_RESET_IMPULSE", true)
	}
}

func effectiveImpulsiveLongStarterEnabled() bool {
	cfg := resolveRuntimeProfileConfig()
	switch cfg.Name {
	case runtimeProfilePaperContinuationClean:
		return false
	default:
		return envBool("LIVE_ENABLE_IMPULSIVE_LONG_STARTER", true)
	}
}

func effectiveImpulsiveShortStarterEnabled() bool {
	cfg := resolveRuntimeProfileConfig()
	switch cfg.Name {
	case runtimeProfilePaperContinuationClean:
		return false
	default:
		return envBool("LIVE_ENABLE_IMPULSIVE_SHORT_STARTER", true)
	}
}

func effectiveReentryEnabled() bool {
	return resolveRuntimeProfileConfig().EffectiveReentry
}

func effectiveContinuationStructureConfirmationRequired() bool {
	return resolveRuntimeProfileConfig().EffectiveRequireStructure
}

func effectiveCandidateMemoryEnabled() bool {
	return resolveRuntimeProfileConfig().EffectiveCandidateMemory
}

func effectiveTriggerMemoryEnabled() bool {
	return resolveRuntimeProfileConfig().EffectiveTriggerMemory
}

func effectiveSharedManagementEnabled() bool {
	return resolveRuntimeProfileConfig().EffectiveSharedManagement
}

func effectiveDirectionalConflictPenaltyOnly() bool {
	return resolveRuntimeProfileConfig().Name == runtimeProfilePaperContinuationClean
}

func effectiveDirectionalConflictExtremeOnly() bool {
	return resolveRuntimeProfileConfig().Name == runtimeProfilePaperContinuationClean
}

func effectiveRuntimeProfileSummary() string {
	cfg := resolveRuntimeProfileConfig()
	return fmt.Sprintf(
		"runtime_profile=%s | effective_strategy_paths=vp=%t,institutional_pa=%t,reversal=%t,impulse=%t,unresolved_watch=%t,unresolved_execution=%t | effective_quality_policy=penalty_based=%t,require_structure=%t,directional_conflict_penalty_only=%t,extreme_conflict_hard_block=%t | effective_reentry_policy=enabled=%t | effective_management_policy=shared=%t,trigger_memory=%t,candidate_memory=%t",
		firstNonEmpty(strings.TrimSpace(string(cfg.Name)), "none"),
		cfg.EffectiveVPEnabled,
		cfg.EffectiveInstitutional,
		cfg.EffectiveReversal,
		cfg.EffectiveImpulse,
		cfg.EffectiveCandidateMemory,
		true,
		true,
		cfg.EffectiveRequireStructure,
		effectiveDirectionalConflictPenaltyOnly(),
		effectiveDirectionalConflictExtremeOnly(),
		cfg.EffectiveReentry,
		cfg.EffectiveSharedManagement,
		cfg.EffectiveTriggerMemory,
		cfg.EffectiveCandidateMemory,
	)
}

func isExecutableStrategy(strat string) bool {
	switch strings.ToLower(strings.TrimSpace(strat)) {
	case "", "none", "no_strategy", "unknown", "unresolved":
		return false
	default:
		return true
	}
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

func emitNotifyEvent(dispatcher *notify.Dispatcher, event notify.Event) {
	if dispatcher == nil {
		return
	}
	_ = dispatcher.Emit(context.Background(), event)
}

func (m *liveExecManager) emitNotify(event notify.Event) {
	if m == nil {
		return
	}
	if event.Symbol == "" {
		event.Symbol = strings.ToUpper(strings.TrimSpace(event.Symbol))
	}
	if m.dispatcher != nil {
		_ = m.dispatcher.Emit(context.Background(), event)
		return
	}
	if m.tg == nil {
		return
	}
	lines := []string{}
	if strings.TrimSpace(event.Message) != "" {
		lines = append(lines, event.Message)
	}
	if v := strings.TrimSpace(event.Metadata["detail"]); v != "" {
		lines = append(lines, v)
	}
	m.tg.Sendf("%s", notify.BuildEventHTML("ℹ️", firstNonEmpty(strings.TrimSpace(event.Title), "EVENT"), lines...))
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

func (c *telegramCommandCtx) setDecision(d operatorDecision) {
	if c == nil || strings.TrimSpace(d.Symbol) == "" {
		return
	}
	d.Symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(d.Symbol)))
	d.UpdatedAt = time.Now().UTC()
	c.decisionMu.Lock()
	if c.decisions == nil {
		c.decisions = map[string]operatorDecision{}
	}
	c.decisions[d.Symbol] = d
	c.decisionMu.Unlock()
}

func (c *telegramCommandCtx) getDecision(symbol string) (operatorDecision, bool) {
	if c == nil {
		return operatorDecision{}, false
	}
	symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	c.decisionMu.RLock()
	defer c.decisionMu.RUnlock()
	d, ok := c.decisions[symbol]
	return d, ok
}

func (c *telegramCommandCtx) addSuggestion(symbol, side, source string, preferredLev int, preferredMargin float64) operatorSuggestion {
	ttl := c.suggestTTL
	if strings.Contains(strings.ToLower(strings.TrimSpace(source)), "trade") {
		ttl = time.Duration(envInt("LIVE_PRIORITY_WATCH_TTL_MIN", int(c.suggestTTL/time.Minute))) * time.Minute
		if ttl <= 0 {
			ttl = c.suggestTTL
		}
	}
	s := operatorSuggestion{
		Symbol:          strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol))),
		Side:            strings.ToUpper(strings.TrimSpace(side)),
		Source:          source,
		PreferredLev:    preferredLev,
		PreferredMargin: preferredMargin,
		CreatedAt:       time.Now().UTC(),
		ExpiresAt:       time.Now().UTC().Add(ttl),
	}
	c.suggestMu.Lock()
	if c.suggestions == nil {
		c.suggestions = map[string]operatorSuggestion{}
	}
	c.suggestions[s.Symbol] = s
	c.suggestMu.Unlock()
	return s
}

func (c *telegramCommandCtx) activeSuggestions() []operatorSuggestion {
	if c == nil {
		return nil
	}
	now := time.Now().UTC()
	c.suggestMu.Lock()
	defer c.suggestMu.Unlock()
	if len(c.suggestions) == 0 {
		return nil
	}
	out := make([]operatorSuggestion, 0, len(c.suggestions))
	for sym, s := range c.suggestions {
		if !s.ExpiresAt.IsZero() && now.After(s.ExpiresAt) {
			delete(c.suggestions, sym)
			continue
		}
		out = append(out, s)
	}
	return out
}

func (c *telegramCommandCtx) hasActiveSuggestions() bool {
	return len(c.activeSuggestions()) > 0
}

func (c *telegramCommandCtx) getSuggestion(symbol string) (operatorSuggestion, bool) {
	if c == nil {
		return operatorSuggestion{}, false
	}
	symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(symbol)))
	now := time.Now().UTC()
	c.suggestMu.Lock()
	defer c.suggestMu.Unlock()
	if c.suggestions == nil {
		return operatorSuggestion{}, false
	}
	s, ok := c.suggestions[symbol]
	if !ok {
		return operatorSuggestion{}, false
	}
	if !s.ExpiresAt.IsZero() && now.After(s.ExpiresAt) {
		delete(c.suggestions, symbol)
		return operatorSuggestion{}, false
	}
	return s, true
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func classifyRejectReason(reason string) RejectClass {
	raw := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case raw == "":
		return ""
	case strings.Contains(raw, "min_available"), strings.Contains(raw, "insufficient_usable"),
		strings.Contains(raw, "reserve_lock"), strings.Contains(raw, "exec_manager_unavailable"),
		strings.Contains(raw, "account"), strings.Contains(raw, "balance"), strings.Contains(raw, "transfer"),
		strings.Contains(raw, "position_check_error"), strings.Contains(raw, "order_error"):
		return rejectClassCapacity
	case strings.Contains(raw, "cooldown"),
		strings.Contains(raw, "max_open"),
		strings.Contains(raw, "pending_add_order"), strings.Contains(raw, "symbol_active_opposite_side"),
		strings.Contains(raw, "reentry_"),
		strings.Contains(raw, "intent_dedupe"), strings.Contains(raw, "shadow_gate_active"),
		strings.Contains(raw, "event_lockout"),
		strings.Contains(raw, "throttle_"), strings.Contains(raw, "post_sl_cooldown"),
		strings.Contains(raw, "max_tracked_entries"):
		return rejectClassStateCooldown
	case strings.Contains(raw, "wall_not_persistent"), strings.Contains(raw, "meta_quality"),
		strings.Contains(raw, "continuation_no_structure_confirm"), strings.Contains(raw, "below_vwap_ema"),
		strings.Contains(raw, "above_vwap_ema"), strings.Contains(raw, "vol_ratio"),
		strings.Contains(raw, "avoid_chase"), strings.Contains(raw, "late_chase_"),
		strings.Contains(raw, "weak_slope"), strings.Contains(raw, "weak slope"),
		strings.Contains(raw, "weak_ofi"), strings.Contains(raw, "weak ofi"),
		strings.Contains(raw, "directional_dayutc_conflict"),
		strings.Contains(raw, "minor_extension"), strings.Contains(raw, "extension"),
		strings.Contains(raw, "weaker_structure"), strings.Contains(raw, "imperfect_confluence"),
		strings.Contains(raw, "expired"),
		strings.Contains(raw, "missed_opportunity_ready"),
		strings.Contains(raw, "asia_quality_gate"), strings.Contains(raw, "strategy_none_reject"),
		strings.Contains(raw, "ny_open_requires_strong_setup"), strings.Contains(raw, "utc_offhours_requires_a_grade"),
		strings.Contains(raw, "asia_dev_starter_only"), strings.Contains(raw, "asia_continue_no_fresh_entry"),
		strings.Contains(raw, "conf_"), strings.Contains(raw, "quality"), strings.Contains(raw, "signal:none"),
		strings.Contains(raw, "first_green_candle_no_structure"):
		return rejectClassSoftConfirm
	default:
		return rejectClassHardSafety
	}
}

func isHardReject(reason string) bool     { return classifyRejectReason(reason) == rejectClassHardSafety }
func isSoftReject(reason string) bool     { return classifyRejectReason(reason) == rejectClassSoftConfirm }
func isCapacityReject(reason string) bool { return classifyRejectReason(reason) == rejectClassCapacity }
func isStateReject(reason string) bool {
	return classifyRejectReason(reason) == rejectClassStateCooldown
}

func fastLaneCandidate(c candidate) bool {
	if !envBool("LIVE_A_PLUS_FASTLANE_ENABLE", true) {
		return false
	}
	minGrade := strings.TrimSpace(envStr("LIVE_A_PLUS_FASTLANE_MIN_GRADE", "A+"))
	if minGrade == "" {
		minGrade = "A+"
	}
	if gradeValue(c.Entry.CurrentGrade) < gradeValue(minGrade) {
		return false
	}
	minScore := envFloat("LIVE_A_PLUS_FASTLANE_MIN_SCORE", 120.0)
	if c.Entry.CurrentScore < minScore {
		return false
	}
	minSlope := envFloat("LIVE_A_PLUS_FASTLANE_MIN_SLOPE", -0.10)
	if c.Entry.ScoreSlope < minSlope {
		return false
	}
	return true
}

func fastLaneReasonEligible(reason string) bool {
	raw := strings.ToLower(strings.TrimSpace(reason))
	if raw == "" {
		return false
	}
	return strings.Contains(raw, "cooldown") ||
		strings.Contains(raw, "persistence") ||
		strings.Contains(raw, "reentry_") ||
		strings.Contains(raw, "post_win_opposite_cooldown")
}

func shouldBypassWithFastLane(c candidate, reason string) bool {
	return fastLaneCandidate(c) && fastLaneReasonEligible(reason)
}

func persistenceEligibilityScore(c candidate) float64 {
	score := clamp(c.CombinedScore, 0, 1)
	if c.PersistenceSeenCount > 0 {
		score += clamp(float64(c.PersistenceSeenCount)/5.0, 0, 0.25)
	}
	if c.PersistenceTopNCount > 0 {
		score += clamp(float64(c.PersistenceTopNCount)/3.0, 0, 0.25)
	}
	if c.PersistenceVolumeTrend {
		score += 0.10
	}
	if c.PersistenceMomentum {
		score += 0.10
	}
	if c.PriorDayLeaderBoost > 0 {
		score += clamp(c.PriorDayLeaderBoost*envFloat("LIVE_PRIOR_DAY_PERSISTENCE_BOOST_WEIGHT", 0.12), 0, 0.18)
	}
	return clamp(score, 0, 1)
}

func newEligibilitySummary(c candidate) EntryEligibilitySummary {
	summary := EntryEligibilitySummary{
		Symbol:             strings.ToUpper(strings.TrimSpace(aster.RawSymbol(c.Entry.Symbol))),
		Side:               c.Side,
		Strat:              c.Strat,
		Rank:               c.FinalRank,
		Grade:              c.Entry.CurrentGrade,
		PersistenceScore:   persistenceEligibilityScore(c),
		AdjustedConfidence: clamp(c.Conf, 0, 1),
	}
	if !isExecutableStrategy(c.Strat) {
		addEligibilityBlock(&summary, "strategy_unresolved")
	}
	if rr := strings.TrimSpace(c.RejectReason); rr != "" {
		addEligibilityBlock(&summary, rr)
	}
	refreshEligibilityQuality(&summary, c)
	return summary
}

func refreshEligibilityQuality(summary *EntryEligibilitySummary, c candidate) {
	if summary == nil {
		return
	}
	quality := buildEntryQualityAccumulator(c, summary.SoftBlocks)
	summary.Quality = quality
	summary.AdjustedConfidence = quality.ScoreAfterPenalties
	summary.ConfidencePenaltyReasons = formatEligibilityQualityPenalties(quality)
	for _, reason := range quality.HardBlockReasons {
		summary.HardBlocks = appendUniqueDecisionReason(summary.HardBlocks, reason)
	}
}

func formatEligibilityQualityPenalties(quality strategies.EntryQualityAccumulator) []string {
	if len(quality.QualityFlags) == 0 {
		return nil
	}
	out := make([]string, 0, len(quality.QualityFlags)+1)
	for _, flag := range quality.QualityFlags {
		out = append(out, flag)
	}
	out = append(out, fmt.Sprintf("penalty_total=%.2f", quality.PenaltyTotal))
	return out
}

func appendUniqueDecisionReason(dst []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" || containsString(dst, reason) {
		return dst
	}
	return append(dst, reason)
}

func addEligibilityBlock(summary *EntryEligibilitySummary, reason string) {
	if summary == nil {
		return
	}
	switch classifyRejectReason(reason) {
	case rejectClassCapacity:
		summary.CapacityBlocks = appendUniqueDecisionReason(summary.CapacityBlocks, reason)
	case rejectClassStateCooldown:
		summary.StateBlocks = appendUniqueDecisionReason(summary.StateBlocks, reason)
	case rejectClassSoftConfirm:
		summary.SoftBlocks = appendUniqueDecisionReason(summary.SoftBlocks, reason)
	case rejectClassHardSafety:
		summary.HardBlocks = appendUniqueDecisionReason(summary.HardBlocks, reason)
	}
}

func compressSoftBlockConfidence(summary *EntryEligibilitySummary, c candidate) {
	if summary == nil || !envBool("LIVE_SOFT_SCORE_COMPRESSION_ENABLE", true) {
		return
	}
	refreshEligibilityQuality(summary, c)
}

func chooseFinalDecision(summary *EntryEligibilitySummary, plan ladderPlan) {
	if summary == nil {
		return
	}
	_ = plan
	switch {
	case len(summary.HardBlocks) > 0:
		summary.FinalDecision = "reject"
		summary.FinalReason = summary.HardBlocks[0]
	case len(summary.CapacityBlocks) > 0:
		summary.FinalDecision = "reject"
		summary.FinalReason = summary.CapacityBlocks[0]
	case len(summary.StateBlocks) > 0:
		summary.FinalDecision = "reject"
		summary.FinalReason = summary.StateBlocks[0]
	case strings.TrimSpace(summary.Quality.BlockReason) == "quality_score_too_low":
		summary.FinalDecision = "reject"
		summary.FinalReason = summary.Quality.BlockReason
	case summary.FullEntryAllowed:
		summary.FinalDecision = "full_entry"
		summary.FinalReason = firstNonEmpty(summary.FinalReason, summary.Strat)
	default:
		summary.FinalDecision = "reject"
		summary.FinalReason = firstNonEmpty(summary.FinalReason, "not_eligible")
	}
}

func logEligibilitySummary(summary EntryEligibilitySummary) {
	_ = summary
}

func logFinalDecision(summary EntryEligibilitySummary) {
	_ = summary
}

func applyDecisionToStatus(st *liveStatus, summary EntryEligibilitySummary) {
	if st == nil {
		return
	}
	st.TopDecision = summary.FinalDecision
	st.TopDecisionWhy = summary.FinalReason
	if summary.FinalDecision == "reject" || summary.FinalDecision == "watch_only" {
		st.TopRejectReason = summary.FinalReason
	}
}

func finalizeEligibilityDecision(summary *EntryEligibilitySummary, plan ladderPlan, status *liveStatus) {
	chooseFinalDecision(summary, plan)
	logEligibilitySummary(*summary)
	logFinalDecision(*summary)
	applyDecisionToStatus(status, *summary)
}

func boolState(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func verboseRejectLogging() bool {
	return envBool("LIVE_VERBOSE_REJECT_LOGS", false)
}

func manageDebugLogging() bool {
	return envBool("LIVE_MANAGE_DEBUG_LOGS", false)
}

func startupSummaryLines(modeLabel string, scanEvery time.Duration, watchCfg watchConfig, ladderCfg ladderConfig, reentryCfg reentryConfig, safety safetyConfig, execMgr *liveExecManager) []string {
	lines := []string{
		fmt.Sprintf("mode=%s | scan=%s | watch=%s | priority=%s", modeLabel, scanEvery, watchCfg.Every, watchCfg.PriorityEvery),
		fmt.Sprintf("margin=%.2f | adds=disabled | min_available=%.2f | max_open=%d", ladderCfg.StarterUSDT, safety.minAvailUSDT, envInt("LIVE_MAX_OPEN_POS", 1)),
		fmt.Sprintf("sizing=%s | reentry=%s | persistence=%s | max_per_side=%d",
			"fixed",
			"off",
			boolState(missedOpportunitiesEnabled()),
			envInt("LIVE_MAX_OPEN_PER_SIDE", 1)),
		effectiveRuntimeProfileSummary(),
	}
	_ = reentryCfg
	if execMgr != nil && execMgr.fundsCfg.Enable {
		lines = append(lines, fmt.Sprintf("perp_target=%.2f | perp_floor=%.2f | funds_check=%ds",
			execMgr.fundsCfg.PerpTargetUSDT, execMgr.fundsCfg.PerpFloorUSDT, envInt("LIVE_FUNDS_MAINTENANCE_SEC", 60)))
	}
	if len(safety.contextOnlySymbols) > 0 {
		syms := make([]string, 0, len(safety.contextOnlySymbols))
		for sym := range safety.contextOnlySymbols {
			syms = append(syms, sym)
		}
		sort.Strings(syms)
		lines = append(lines, fmt.Sprintf("context_only=%s", strings.Join(syms, ",")))
	}
	if execMgr != nil {
		report := execMgr.ensureAccountReportFresh(time.Now().UTC(), 30*time.Second)
		lines = append(lines, compactAccountSummaryLine(report))
	}
	return lines
}

func startupWarningLines(ladderCfg ladderConfig, safety safetyConfig, execMgr *liveExecManager) []string {
	warnings := []string{}
	if safety.minAvailUSDT > maxFloat(ladderCfg.StarterUSDT, 10)*1.5 {
		warnings = append(warnings, fmt.Sprintf("warning min_available_high=%.2f", safety.minAvailUSDT))
	}
	if !ladderCfg.OneSymbolOnly {
		warnings = append(warnings, "note multi-symbol mode enabled")
	}
	if execMgr != nil && !execMgr.fundsCfg.Enable {
		warnings = append(warnings, "warning funds_manager_disabled")
	}
	return warnings
}

func missedOpportunitiesEnabled() bool {
	return envBool("LIVE_OPP_TRACK_ENABLE", true) || envBool("LIVE_PERSISTENCE_ENTRY_ENABLE", true)
}

func allowedOperatorLeverage(raw string) (int, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.TrimSuffix(raw, "x")
	switch raw {
	case "20":
		return 20, true
	case "10":
		return 10, true
	case "5":
		return 5, true
	case "3":
		return 3, true
	default:
		return 0, false
	}
}

type operatorOrderRequest struct {
	Symbol        string
	Side          string
	USD           float64
	LimitPrice    float64
	StopLoss      float64
	TakeProfit    float64
	Leverage      int
	HasLimit      bool
	HasStopLoss   bool
	HasTakeProfit bool
}

type operatorOrderResult struct {
	Symbol         string
	Side           string
	USD            float64
	OrderID        int64
	Status         string
	Working        bool
	Filled         bool
	Rejected       bool
	Qty            float64
	AvgPrice       float64
	LimitPrice     float64
	StopLoss       float64
	TakeProfit     float64
	BracketStatus  string
	PositionPnL    float64
	PositionPnLPct float64
}

func operatorExecutionEnabled() bool {
	return envBool("LIVE_OPERATOR_EXECUTION_ENABLE", false)
}

func operatorFillTimeout() time.Duration {
	sec := envInt("LIVE_OPERATOR_FILL_TIMEOUT_SEC", 15)
	if sec < 1 {
		sec = 15
	}
	return time.Duration(sec) * time.Second
}

func operatorFillPoll() time.Duration {
	ms := envInt("LIVE_OPERATOR_FILL_POLL_MS", 350)
	if ms < 100 {
		ms = 350
	}
	return time.Duration(ms) * time.Millisecond
}

func operatorDefaultLeverage() int {
	if lev, ok := allowedOperatorLeverage(envStr("LIVE_OPERATOR_DEFAULT_LEVERAGE", "")); ok {
		return lev
	}
	return 0
}

func parseOperatorOrderCommand(rawMsg string) (operatorOrderRequest, error) {
	fields := strings.Fields(strings.TrimSpace(rawMsg))
	if len(fields) < 3 {
		return operatorOrderRequest{}, fmt.Errorf("usage: /long SYMBOL usd=10 [limit=123] [sl=120] [tp=129] [lev=5]")
	}
	var req operatorOrderRequest
	switch strings.ToLower(strings.TrimSpace(fields[0])) {
	case "/long":
		req.Side = "BUY"
	case "/short":
		req.Side = "SELL"
	default:
		return operatorOrderRequest{}, fmt.Errorf("unsupported operator command")
	}
	req.Symbol = strings.ToUpper(strings.TrimSpace(aster.RawSymbol(fields[1])))
	if req.Symbol == "" {
		return operatorOrderRequest{}, fmt.Errorf("symbol required")
	}
	req.Leverage = operatorDefaultLeverage()
	for _, field := range fields[2:] {
		part := strings.TrimSpace(field)
		if part == "" {
			continue
		}
		if strings.EqualFold(part, "market") {
			req.HasLimit = false
			req.LimitPrice = 0
			continue
		}
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			return operatorOrderRequest{}, fmt.Errorf("invalid token %q", part)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		if val == "" {
			return operatorOrderRequest{}, fmt.Errorf("missing value for %s", key)
		}
		switch key {
		case "usd", "usdt", "size", "notional":
			f, err := strconv.ParseFloat(val, 64)
			if err != nil || f <= 0 {
				return operatorOrderRequest{}, fmt.Errorf("invalid usd value")
			}
			req.USD = f
		case "limit", "price":
			f, err := strconv.ParseFloat(val, 64)
			if err != nil || f <= 0 {
				return operatorOrderRequest{}, fmt.Errorf("invalid limit price")
			}
			req.LimitPrice = f
			req.HasLimit = true
		case "sl", "stop", "stoploss":
			f, err := strconv.ParseFloat(val, 64)
			if err != nil || f <= 0 {
				return operatorOrderRequest{}, fmt.Errorf("invalid stop-loss price")
			}
			req.StopLoss = f
			req.HasStopLoss = true
		case "tp", "takeprofit":
			f, err := strconv.ParseFloat(val, 64)
			if err != nil || f <= 0 {
				return operatorOrderRequest{}, fmt.Errorf("invalid take-profit price")
			}
			req.TakeProfit = f
			req.HasTakeProfit = true
		case "lev", "leverage":
			lev, ok := allowedOperatorLeverage(val)
			if !ok {
				return operatorOrderRequest{}, fmt.Errorf("invalid leverage %q (allowed: 3,5,10,20)", val)
			}
			req.Leverage = lev
		default:
			return operatorOrderRequest{}, fmt.Errorf("unknown field %q", key)
		}
	}
	if req.USD <= 0 {
		return operatorOrderRequest{}, fmt.Errorf("usd=... is required")
	}
	return req, nil
}

func operatorOrderHTML(res operatorOrderResult) string {
	lines := []string{
		fmt.Sprintf("<b>%s %s</b>", cleanSymbol(res.Symbol), displayPositionSide(res.Side)),
		fmt.Sprintf("<b>Notional:</b> $%.2f", res.USD),
		fmt.Sprintf("<b>Order ID:</b> %d", res.OrderID),
		fmt.Sprintf("<b>Status:</b> %s", firstNonEmpty(strings.ToUpper(strings.TrimSpace(res.Status)), "UNKNOWN")),
	}
	if res.LimitPrice > 0 {
		lines = append(lines, fmt.Sprintf("<b>Limit:</b> %s", fmtPrice(res.LimitPrice)))
	} else {
		lines = append(lines, "<b>Execution:</b> MARKET")
	}
	if res.Qty > 0 || res.AvgPrice > 0 {
		lines = append(lines, fmt.Sprintf("<b>Filled Qty:</b> %.6f | <b>Avg:</b> %s", res.Qty, fmtPrice(res.AvgPrice)))
	}
	if res.StopLoss > 0 || res.TakeProfit > 0 {
		lines = append(lines, fmt.Sprintf("<b>TP/SL:</b> tp=%s sl=%s", fmtPrice(res.TakeProfit), fmtPrice(res.StopLoss)))
	}
	if strings.TrimSpace(res.BracketStatus) != "" {
		lines = append(lines, fmt.Sprintf("<b>Bracket:</b> %s", res.BracketStatus))
	}
	if res.Filled {
		lines = append(lines, fmt.Sprintf("<b>Position PnL:</b> %+.2f (%+.2f%%)", res.PositionPnL, res.PositionPnLPct))
	}
	return notify.BuildEventHTML("🧾", "OPERATOR ORDER", lines...)
}

func recordCandidateDecision(ctx *telegramCommandCtx, c candidate, reject string) {
	if ctx == nil {
		return
	}
	quality := buildEntryQualityAccumulator(c, []string{reject})
	topBlockers := []string{}
	for _, part := range strings.Split(strings.TrimSpace(reject), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		topBlockers = append(topBlockers, part)
		if len(topBlockers) >= 3 {
			break
		}
	}
	persistenceStatus := "none"
	if c.PersistenceSeenCount > 0 || strings.Contains(strings.ToLower(strings.TrimSpace(c.Strat)), "persistence") {
		persistenceStatus = firstNonEmpty(c.PersistenceReason, "tracking")
	}
	ctx.setDecision(operatorDecision{
		Symbol:              c.Entry.Symbol,
		Side:                c.Side,
		Grade:               c.Entry.CurrentGrade,
		Score:               c.Entry.CurrentScore,
		Slope:               c.Entry.ScoreSlope,
		Strategy:            c.Strat,
		Confidence:          c.Conf,
		RawConfidence:       c.Conf,
		AdjustedConfidence:  c.Conf,
		RejectReason:        reject,
		BlockerClass:        string(classifyRejectReason(reject)),
		TopBlockers:         topBlockers,
		StarterAllowed:      false,
		PersistenceStatus:   persistenceStatus,
		State:               string(c.Entry.State),
		QualityFlags:        quality.QualityFlags,
		PenaltyTotal:        quality.PenaltyTotal,
		ScoreBefore:         quality.ScoreBefore,
		ScoreAfterPenalties: quality.ScoreAfterPenalties,
		MinScore:            quality.MinScore,
		HardBlockReasons:    quality.HardBlockReasons,
		BlockReason:         quality.BlockReason,
	})
}

func (c *telegramCommandCtx) run() {
	if c == nil || c.tg == nil || !c.tg.Enabled() {
		return
	}
	c.tg.Listen(context.Background(), c.handleCommand)
}

func operatorModeEnabled() bool {
	return envBool("LIVE_TG_OPERATOR_MODE", true)
}

func debugModeEnabled() bool {
	return envBool("LIVE_TG_DEBUG_MODE", false)
}

func topBlocker(reason string) string {
	for _, part := range strings.Split(strings.TrimSpace(reason), ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			return part
		}
	}
	return ""
}

func manualAssistStance(d operatorDecision) string {
	reason := strings.ToLower(strings.TrimSpace(d.RejectReason))
	switch {
	case strings.Contains(reason, "reclaim"):
		return "wait for reclaim"
	case strings.Contains(reason, "bounce"):
		return "wait for failed bounce"
	case strings.Contains(reason, "late_cycle"):
		return "avoid / late cycle"
	default:
		return "wait for confirmation"
	}
}

func manualAssistResponse(sym string, side string, d operatorDecision, meta symbolMeta) string {
	entryNow := "NO"
	if d.AdjustedConfidence >= envFloat("LIVE_SOFT_SCORE_STARTER_MIN", 0.52) && d.BlockerClass != string(rejectClassHardSafety) {
		entryNow = "YES"
	}
	softs := []string{}
	hards := []string{}
	for _, b := range d.TopBlockers {
		if isSoftReject(b) {
			softs = append(softs, b)
		} else if b != "" {
			hards = append(hards, b)
		}
	}
	lines := []string{
		fmt.Sprintf("<b>MANUAL ENTRY ASSIST — %s %s</b>", cleanSymbol(sym), displayPositionSide(side)),
		fmt.Sprintf("<b>State:</b> %s | <b>Rank:</b> %.2f | <b>Entry now:</b> %s", firstNonEmpty(strings.ToUpper(strings.TrimSpace(d.State)), "UNKNOWN"), d.Score, entryNow),
		fmt.Sprintf("<b>Persistence:</b> %s | <b>Decision:</b> %s", firstNonEmpty(d.PersistenceStatus, "none"), firstNonEmpty(d.Strategy, "watch_only")),
	}
	if len(hards) > 0 {
		lines = append(lines, fmt.Sprintf("<b>Hard blockers:</b> %s", strings.Join(hards, ", ")))
	} else {
		lines = append(lines, "<b>Hard blockers:</b> none")
	}
	if len(softs) > 0 {
		lines = append(lines, fmt.Sprintf("<b>Soft blockers:</b> %s", strings.Join(softs, ", ")))
	}
	lines = append(lines,
		fmt.Sprintf("<b>Best operator stance:</b> %s", manualAssistStance(d)),
		"<b>Avoid:</b> full size chase",
		fmt.Sprintf("<b>Bot status:</b> %s", firstNonEmpty(d.Strategy, "watch_only")),
	)
	if meta.LastPrice > 0 {
		lines = append(lines, fmt.Sprintf("<b>Last:</b> %s | <b>24h:</b> %.2f%% | <b>Vol:</b> %.2fM", fmtPrice(meta.LastPrice), meta.Move24h, meta.VolumeUSD/1_000_000.0))
	}
	return notify.BuildEventHTML("🧭", "MANUAL ENTRY", lines...)
}

func (c *telegramCommandCtx) blockedStrongLines(limit int) []string {
	if c == nil {
		return nil
	}
	c.decisionMu.RLock()
	defer c.decisionMu.RUnlock()
	type row struct {
		d operatorDecision
	}
	rows := make([]row, 0, len(c.decisions))
	for _, d := range c.decisions {
		if strings.TrimSpace(d.Symbol) == "" || strings.EqualFold(strings.TrimSpace(d.BlockerClass), string(rejectClassHardSafety)) {
			continue
		}
		if d.Score < 70 && d.AdjustedConfidence < 0.50 {
			continue
		}
		rows = append(rows, row{d: d})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].d.Score == rows[j].d.Score {
			return rows[i].d.AdjustedConfidence > rows[j].d.AdjustedConfidence
		}
		return rows[i].d.Score > rows[j].d.Score
	})
	out := make([]string, 0, minInt(limit, len(rows)))
	for _, r := range rows {
		d := r.d
		out = append(out, fmt.Sprintf("%s %s state=%s blocker_class=%s top=%s executable_now=%s",
			cleanSymbol(d.Symbol), displayPositionSide(d.Side), strings.ToUpper(strings.TrimSpace(d.State)),
			firstNonEmpty(d.BlockerClass, "none"), firstNonEmpty(topBlocker(d.RejectReason), "none"), boolLabel(d.StarterAllowed)))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (c *telegramCommandCtx) handleCommand(_ string, msg string) string {
	rawMsg := strings.TrimSpace(msg)
	cmd := strings.ToLower(rawMsg)
	fields := strings.Fields(rawMsg)
	switch {
	case strings.HasPrefix(cmd, "/help"), strings.HasPrefix(cmd, "/start"):
		lines := []string{
			"<b>Scanner</b> <code>/status</code> <code>/scanner</code> <code>/longs</code> <code>/shorts</code> <code>/why SYMBOL</code>",
			"<b>Account</b> <code>/balance</code> <code>/acct</code> <code>/summary</code> <code>/positions</code> <code>/position SYMBOL</code>",
		}
		if operatorExecutionEnabled() {
			lines = append(lines, "<b>Trading</b> <code>/long SYMBOL usd=10 [limit=123] [sl=120] [tp=129]</code> <code>/short SYMBOL usd=10 ...</code>")
		} else {
			lines = append(lines, "<b>Trading:</b> disabled (ground-zero mode)")
		}
		lines = append(lines, "<b>More</b> <code>/hotkeys</code>")
		return notify.BuildEventHTML("📘", "COMMANDS", lines...)
	case strings.HasPrefix(cmd, "/hotkeys"):
		if operatorExecutionEnabled() {
			return notify.BuildEventHTML("⌨️", "HOTKEYS",
				"<code>/long OUSDT usd=10 limit=0.123 sl=0.118 tp=0.129</code>",
				"<code>/short OUSDT usd=10 limit=0.123 sl=0.126 tp=0.118</code>",
			)
		}
		return notify.BuildEventHTML("⌨️", "HOTKEYS",
			"Trading hotkeys are disabled in ground-zero mode.",
			"Use scanner/status commands only.",
		)
	case strings.HasPrefix(cmd, "/long "), strings.HasPrefix(cmd, "/short "):
		if !operatorExecutionEnabled() {
			return notify.BuildEventHTML("🛑", "TRADING DISABLED",
				"Operator execution is disabled.",
				"Set LIVE_OPERATOR_EXECUTION_ENABLE=1 to allow /long and /short commands.",
			)
		}
		if c.execMgr == nil {
			return notify.BuildEventHTML("🛑", "TRADING DISABLED", "Execution manager unavailable")
		}
		dryRun, liveEnabled, _ := true, false, false
		if c.mode != nil {
			dryRun, liveEnabled, _ = c.mode.snapshot()
		}
		if dryRun || !liveEnabled {
			return notify.BuildEventHTML("🛑", "TRADING DISABLED",
				"Live order entry is not armed.",
				"Start the bot with LIVE_ENABLE_LIVE_TRADING=1 and LIVE_DRY_RUN=0.",
			)
		}
		req, err := parseOperatorOrderCommand(rawMsg)
		if err != nil {
			return notify.BuildEventHTML("❓", "USAGE", err.Error())
		}
		res, err := c.execMgr.submitOperatorOrder(req, c.getMeta())
		if err != nil {
			return notify.BuildEventHTML("❌", "ORDER FAILED",
				fmt.Sprintf("<b>%s %s</b>", cleanSymbol(req.Symbol), displayPositionSide(req.Side)),
				fmt.Sprintf("<b>Reason:</b> %s", err.Error()),
			)
		}
		return operatorOrderHTML(res)
	case strings.HasPrefix(cmd, "/l "), strings.HasPrefix(cmd, "/s "), strings.HasPrefix(cmd, "/l3 "), strings.HasPrefix(cmd, "/l5 "), strings.HasPrefix(cmd, "/l10 "), strings.HasPrefix(cmd, "/l20 "), strings.HasPrefix(cmd, "/s3 "), strings.HasPrefix(cmd, "/s5 "), strings.HasPrefix(cmd, "/s10 "), strings.HasPrefix(cmd, "/s20 "), strings.HasPrefix(cmd, "/m "), strings.HasPrefix(cmd, "/p "), strings.HasPrefix(cmd, "/c "), strings.HasPrefix(cmd, "/execute "), strings.HasPrefix(cmd, "/trade "), strings.HasPrefix(cmd, "/protect "), strings.HasPrefix(cmd, "/close "), strings.HasPrefix(cmd, "/flatten "), strings.HasPrefix(cmd, "/closeall"), cmd == "/mode live" || cmd == "/live" || cmd == "/mode paper" || cmd == "/paper":
		return notify.BuildEventHTML("🛑", "TRADING DISABLED",
			"Ground-zero mode is active.",
			"All order/trade commands are disabled.",
		)
	case strings.HasPrefix(cmd, "/status"):
		s := c.status.Snapshot()
		livePnL := 0.0
		pendingManual := 0
		if c.execMgr != nil {
			ls := c.execMgr.LiveAccountSnapshot(3)
			livePnL = ls.RealizedDay + ls.OpenPnL
			pendingManual = len(c.execMgr.pendingManualRequests(0))
		}
		paperPnL := 0.0
		openPositions := s.Exec.Open
		if s.Paper != nil {
			paperPnL = s.Paper.RealizedToday + s.Paper.OpenPnL
			if strings.EqualFold(s.Mode, "paper") || s.DryRun {
				openPositions = s.Paper.OpenCount
			}
		}
		enabledState := "DISABLED"
		if (!s.DryRun && s.LiveEnabled) || (s.DryRun && s.Paper != nil) {
			enabledState = "ENABLED"
		}
		issuesLine := ""
		if pendingManual > 0 {
			issuesLine = fmt.Sprintf("Pending: %d manual approval", pendingManual)
			if pendingManual > 1 {
				issuesLine += "s"
			}
		}
		return notify.FormatBotStatus(notify.StatusView{
			Mode:          firstNonEmpty(s.Mode, "live"),
			EnabledState:  enabledState,
			TopSymbol:     cleanSymbol(s.TopSymbol),
			TopSide:       s.TopSide,
			MarketBias:    s.ScannerBias,
			OpenPositions: openPositions,
			AvailableUSDT: s.AvailableUSDT,
			PaperPnL:      paperPnL,
			LivePnL:       livePnL,
			IssuesLine:    issuesLine,
		})
	case strings.HasPrefix(cmd, "/scanner"), strings.HasPrefix(cmd, "/longs"), strings.HasPrefix(cmd, "/shorts"):
		s := c.status.Snapshot()
		which := "scanner"
		if strings.HasPrefix(cmd, "/longs") {
			which = "longs"
		} else if strings.HasPrefix(cmd, "/shorts") {
			which = "shorts"
		}
		longs, shorts, bias := scannerItemsForCommand(s, which)
		if len(longs) == 0 && len(shorts) == 0 {
			return notify.BuildEventHTML("📡", "SCANNER", "No current scanner snapshot is available yet")
		}
		view := scannerViewFromStatus(liveStatus{
			Generated:     s.Generated,
			ScannerBias:   bias,
			ScannerLongs:  longs,
			ScannerShorts: shorts,
		}, manualLivePositionLines(c.execMgr, 3))
		return notify.FormatScannerSnapshot(view)
	case strings.HasPrefix(cmd, "/balance"):
		s := c.status.Snapshot()
		if c.execMgr != nil {
			ls := c.execMgr.LiveAccountSnapshot(5)
			if len(ls.Positions) > 0 || ls.AvailableUSDT > 0 || ls.Equity > 0 {
				paperPnL := 0.0
				if c.paper != nil && c.paper.enabled {
					now := time.Now().In(c.paper.reportLoc)
					dayKey := now.Format("2006-01-02")
					if ds := c.paper.dayStats[dayKey]; ds != nil {
						paperPnL = ds.Net
					}
				}
				return notify.FormatAccountSummary(notify.AccountView{
					Mode:          "LIVE",
					Timestamp:     time.Now().In(time.Local).Format("15:04:05 MST"),
					AvailableUSDT: ls.AvailableUSDT,
					Equity:        ls.Equity,
					PaperPnL:      paperPnL,
					LivePnL:       ls.RealizedDay + ls.OpenPnL,
					OpenPositions: ls.OpenCount,
				})
			}
		}
		if c.rest != nil {
			assets := envCSV("LIVE_ACCOUNT_ASSETS", "")
			var userData *aster.UserDataState
			if c.execMgr != nil {
				userData = c.execMgr.userDataState
			}
			snap, err := fetchAccountSnapshot(c.rest, userData, assets)
			if err == nil {
				eq := accountEquity(snap)
				return notify.FormatAccountSummary(notify.AccountView{
					Mode:          firstNonEmpty(s.Mode, "live"),
					Timestamp:     time.Now().In(time.Local).Format("15:04:05 MST"),
					AvailableUSDT: snap.AvailableUSDT,
					Equity:        eq,
					PaperPnL:      0,
					LivePnL:       0,
					OpenPositions: len(snap.Positions),
				})
			}
		}
		paperPnL := 0.0
		livePnL := 0.0
		openPositions := s.Exec.Open
		equity := s.AvailableUSDT
		if s.Paper != nil {
			paperPnL = s.Paper.RealizedToday + s.Paper.OpenPnL
			if strings.EqualFold(s.Mode, "paper") || s.DryRun {
				equity = s.Paper.Equity
				openPositions = s.Paper.OpenCount
			}
		}
		livePnL = s.Live.RealizedDay + s.Live.OpenPnL
		if !strings.EqualFold(s.Mode, "paper") && !s.DryRun && s.Live.Equity > 0 {
			equity = s.Live.Equity
		}
		return notify.FormatAccountSummary(notify.AccountView{
			Mode:          firstNonEmpty(s.Mode, "live"),
			Timestamp:     time.Now().In(time.Local).Format("15:04:05 MST"),
			AvailableUSDT: s.AvailableUSDT,
			Equity:        equity,
			PaperPnL:      paperPnL,
			LivePnL:       livePnL,
			OpenPositions: openPositions,
		})
	case strings.HasPrefix(cmd, "/acct"), strings.HasPrefix(cmd, "/growth"), strings.HasPrefix(cmd, "/summary"):
		if c.execMgr == nil {
			return notify.BuildEventHTML("💼", "ACCOUNT SUMMARY", "Execution manager unavailable")
		}
		report := c.execMgr.ensureAccountReportFresh(time.Now().UTC(), 2*time.Minute)
		includeMissed := []string(nil)
		if strings.HasPrefix(cmd, "/summary") && c.missed != nil {
			if rows := c.missed.ReviewLines(time.Now().UTC(), 3); len(rows) > 0 {
				includeMissed = append(includeMissed, "<b>Missed Opportunities:</b>")
				for _, row := range rows {
					includeMissed = append(includeMissed, row)
				}
			}
			if rows := c.blockedStrongLines(4); len(rows) > 0 {
				includeMissed = append(includeMissed, "<b>Blocked but Strong:</b>")
				includeMissed = append(includeMissed, rows...)
			}
		}
		return buildAccountHTML(report, strings.HasPrefix(cmd, "/growth"), includeMissed)
	case strings.HasPrefix(cmd, "/health"):
		report := accountReport{}
		if c.execMgr != nil {
			report = c.execMgr.ensureAccountReportFresh(time.Now().UTC(), 2*time.Minute)
			snap := c.execMgr.LiveAccountSnapshot(6)
			return notify.BuildEventHTML("🩺", "HEALTH",
				fmt.Sprintf("<b>Account:</b> %s | <b>Detail:</b> %s", firstNonEmpty(report.Health, "failed"), firstNonEmpty(report.HealthDetail, "none")),
				fmt.Sprintf("<b>Live snapshot:</b> %s | <b>Detail:</b> %s", firstNonEmpty(snap.Health, "healthy"), firstNonEmpty(snap.HealthDetail, "none")),
				fmt.Sprintf("<b>Open:</b> %d | <b>Manual:</b> %d | <b>Bot:</b> %d", snap.OpenCount, snap.ManualCount, snap.BotCount),
			)
		}
		return notify.BuildEventHTML("🩺", "HEALTH", "Execution manager unavailable")
	case strings.HasPrefix(cmd, "/positions"), cmd == "/pos":
		if c.paper != nil && c.paper.enabled {
			meta := c.getMeta()
			now := time.Now().In(c.paper.reportLoc)
			pulse, cards := buildPaperPulseAndCards("POSITIONS", now, c.paper, meta)
			if len(cards) == 0 {
				return notify.BuildEventHTML("📦", "POSITIONS", "No active paper positions")
			}
			var b strings.Builder
			b.WriteString(notify.BuildSessionPulseHTML(pulse))
			for _, card := range cards {
				b.WriteString("\n\n")
				b.WriteString(notify.BuildPositionCard(card))
			}
			return b.String()
		}
		if c.execMgr != nil {
			ls := c.execMgr.LiveAccountSnapshot(10)
			if len(ls.Positions) == 0 {
				pending := c.execMgr.pendingManualRequests(5)
				if len(pending) == 0 {
					return notify.BuildEventHTML("📦", "POSITIONS", "No active live positions")
				}
				lines := []string{"No active live positions"}
				for _, req := range pending {
					lines = append(lines, fmt.Sprintf("<b>Pending manual:</b> %s %s | qty=%.6f | entry=%s",
						cleanSymbol(req.Symbol), displayPositionSide(req.Side), req.Qty, fmtPrice(req.Entry)))
				}
				lines = append(lines, "Reply <code>/manage SYMBOL y</code> to let the bot manage one.")
				return notify.BuildEventHTML("📦", "POSITIONS", lines...)
			}
			now := time.Now().In(c.execMgr.reportLoc)
			pulse, cards := buildLivePulseAndCards("LIVE POSITIONS", now, c.execMgr)
			var b strings.Builder
			b.WriteString(notify.BuildSessionPulseHTML(pulse))
			for i, card := range cards {
				b.WriteString("\n\n")
				if i >= 10 {
					break
				}
				b.WriteString(notify.BuildPositionCard(card))
			}
			return strings.TrimSpace(b.String())
		}
		return notify.BuildEventHTML("📦", "POSITIONS", "unavailable")
	case strings.HasPrefix(cmd, "/position"):
		if len(fields) < 2 {
			return notify.BuildEventHTML("❓", "USAGE", "<code>/position SYMBOL</code>")
		}
		sym := strings.ToUpper(strings.TrimSpace(fields[1]))
		if c.execMgr == nil {
			return notify.BuildEventHTML("📍", "POSITION", "live execution manager unavailable")
		}
		p, ok := c.execMgr.LivePositionBySymbol(sym)
		if !ok {
			if req, pending := c.execMgr.pendingManualRequest(sym); pending {
				return notify.BuildEventHTML("📍", "POSITION DETAIL",
					fmt.Sprintf("<b>Symbol:</b> %s | <b>Side:</b> %s | <b>Src:</b> MANUAL", cleanSymbol(req.Symbol), displayPositionSide(req.Side)),
					fmt.Sprintf("<b>Qty:</b> %.6f | <b>Lev:</b> %dx | <b>Margin:</b> $%.2f", req.Qty, maxInt(1, req.Leverage), req.Margin),
					fmt.Sprintf("<b>Entry:</b> %s", fmtPrice(req.Entry)),
					"<b>Bot Management:</b> pending approval",
					fmt.Sprintf("Reply <code>/manage %s y</code> or <code>/manage %s n</code>", cleanSymbol(req.Symbol), cleanSymbol(req.Symbol)),
				)
			}
			return notify.BuildEventHTML("📍", "POSITION", fmt.Sprintf("%s is not an active live position", cleanSymbol(sym)))
		}
		lines := []string{
			fmt.Sprintf("<b>Symbol:</b> %s | <b>Side:</b> %s | <b>Src:</b> %s", cleanSymbol(p.Symbol), displayPositionSide(p.Side), displayEntrySource(p.Source)),
			fmt.Sprintf("<b>Manage:</b> %s | <b>Protection:</b> %s", nonEmpty(strings.ToUpper(strings.TrimSpace(p.ManageState)), "-"), nonEmpty(strings.ToUpper(strings.TrimSpace(p.ProtectionState)), "-")),
			fmt.Sprintf("<b>Managed:</b> %s | <b>Exchange Stop:</b> %s", boolLabel(p.Managed), func() string {
				if p.Protected {
					return "LIVE"
				}
				return "NO"
			}()),
			fmt.Sprintf("<b>Qty:</b> %.6f | <b>Lev:</b> %dx | <b>Margin:</b> $%.2f", p.Qty, maxInt(1, p.Leverage), p.Margin),
			fmt.Sprintf("<b>Entry:</b> %s | <b>Mark:</b> %s | <b>Last:</b> %s", fmtPrice(p.EntryPrice), fmtPrice(p.MarkPrice), fmtPrice(p.LastPrice)),
			fmt.Sprintf("<b>uPnL:</b> %+.2f (%+.2f%%) | <b>Exchange uPnL:</b> %+.2f", p.UnrealizedPnL, p.UnrealizedPnLPct, p.ExchangeUnreal),
			fmt.Sprintf("<b>Stop:</b> %s | <b>Spread:</b> %.1fbps | <b>Hold:</b> %.1fm", fmtPrice(p.StopPrice), p.SpreadBps, p.HoldMin),
			fmt.Sprintf("<b>Reason:</b> <code>%s</code>", displayEntryReason(p.EntryReason)),
		}
		if debugModeEnabled() || !operatorModeEnabled() {
			lines = append(lines, fmt.Sprintf("<b>Debug:</b> rawSide=%s | normalized=%s | pnlDelta=%+.2f", strings.ToUpper(strings.TrimSpace(p.Side)), normalizePositionSide(p.Side), p.UnrealizedPnL-p.ExchangeUnreal))
		}
		return notify.BuildEventHTML("📍", "POSITION DETAIL", lines...)
	case cmd == "y" || cmd == "yes" || cmd == "n" || cmd == "no", strings.HasPrefix(cmd, "/manage"):
		return notify.BuildEventHTML("🛑", "TRADING DISABLED",
			"Ground-zero mode is active.",
			"Management/trade commands are disabled.",
		)
	case cmd == "/mode":
		modeDryRun := true
		modeLiveEnabled := false
		modePaperEnabled := c.paper != nil && c.paper.enabled
		if c.mode != nil {
			modeDryRun, modeLiveEnabled, modePaperEnabled = c.mode.snapshot()
		}
		modeLabel := "PAPER"
		if !modeDryRun && modeLiveEnabled {
			modeLabel = "LIVE"
		}
		effectiveMode := "paper"
		if !modeDryRun && modeLiveEnabled {
			effectiveMode = "live"
		}
		if c.mode != nil {
			effectiveMode = string(c.mode.operatingMode())
		}
		return notify.BuildEventHTML("🎛️", "MODE",
			fmt.Sprintf("<b>Execution posture:</b> %s", effectiveMode),
			fmt.Sprintf("<b>Account mode:</b> %s", modeLabel),
			fmt.Sprintf("<b>Dry Run:</b> %v", modeDryRun),
			fmt.Sprintf("<b>Live Entries Enabled:</b> %v", modeLiveEnabled),
			fmt.Sprintf("<b>Paper Entries Enabled:</b> %v", modePaperEnabled),
			"Manual trades opened on the exchange can be approved for bot management from Telegram.",
		)
	case strings.HasPrefix(cmd, "/why "):
		if len(fields) < 2 {
			return notify.BuildEventHTML("❓", "USAGE", "<code>/why SYMBOL</code>")
		}
		sym := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(fields[1])))
		if sym == "" {
			return notify.BuildEventHTML("❓", "USAGE", "<code>/why SYMBOL</code>")
		}
		if d, ok := c.getDecision(sym); ok {
			lines := []string{
				fmt.Sprintf("<b>Symbol:</b> %s", cleanSymbol(sym)),
				fmt.Sprintf("<b>Decision:</b> %s", firstNonEmpty(d.RejectReason, "eligible")),
				fmt.Sprintf("<b>Side:</b> %s | <b>Grade:</b> %s | <b>Score:</b> %.2f", displayPositionSide(d.Side), d.Grade, d.Score),
				fmt.Sprintf("<b>Slope:</b> %+.3f | <b>Setup:</b> <code>%s</code>", d.Slope, d.Strategy),
				fmt.Sprintf("<b>Conf:</b> %.2f | <b>Adj:</b> %.2f | <b>State:</b> %s", d.Confidence, d.AdjustedConfidence, d.State),
				fmt.Sprintf("<b>Blocker class:</b> %s | <b>Executable now:</b> %s", firstNonEmpty(d.BlockerClass, "none"), boolLabel(d.StarterAllowed)),
				fmt.Sprintf("<b>Top blockers:</b> %s", firstNonEmpty(strings.Join(d.TopBlockers, ", "), "none")),
				fmt.Sprintf("<b>Persistence:</b> %s", firstNonEmpty(d.PersistenceStatus, "none")),
				fmt.Sprintf("<b>Updated:</b> %s", d.UpdatedAt.In(time.Local).Format("15:04:05 MST")),
			}
			if s, ok := c.getSuggestion(sym); ok {
				lines = append(lines, fmt.Sprintf("<b>Operator watch:</b> %s until %s", displayPositionSide(s.Side), s.ExpiresAt.In(time.Local).Format("15:04 MST")))
			}
			return notify.BuildEventHTML("🔎", "WHY", lines...)
		}
		meta := c.getMeta()
		if m, ok := meta[sym]; ok {
			return notify.BuildEventHTML("🔎", "WHY",
				fmt.Sprintf("<b>Symbol:</b> %s", cleanSymbol(sym)),
				"Symbol is visible in market data but has no current candidate decision",
				fmt.Sprintf("<b>Price:</b> %s | <b>DayUTC:</b> %.2f%% | <b>UTC4h:</b> %.2f%% | <b>UTC1h:</b> %.2f%% | <b>24h:</b> %.2f%% | <b>Vol:</b> %.2fM",
					fmtPrice(m.LastPrice), m.DayUTC24h, m.UTC4hPct, m.UTC1hPct, m.Move24h, m.VolumeUSD/1_000_000.0),
			)
		}
		return notify.BuildEventHTML("🔎", "WHY", fmt.Sprintf("%s is not in the current market snapshot", cleanSymbol(sym)))
	case strings.HasPrefix(cmd, "/manual "), strings.HasPrefix(cmd, "/entry "), strings.HasPrefix(cmd, "/suggest "), strings.HasPrefix(cmd, "/unmanage "), strings.HasPrefix(cmd, "/pause"), strings.HasPrefix(cmd, "/resume"):
		return notify.BuildEventHTML("🛑", "TRADING DISABLED",
			"Ground-zero mode is active.",
			"Trade/management commands are disabled.",
		)
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
