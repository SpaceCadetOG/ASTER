package paperreport

import "time"

type ClosedTradeRecord struct {
	TradeID  string              `json:"trade_id"`
	Mode     string              `json:"mode"`
	Symbol   string              `json:"symbol"`
	Side     string              `json:"side"`
	Identity ClosedTradeIdentity `json:"identity"`
	Entry    ClosedTradeEntry    `json:"entry"`
	Plan     ClosedTradePlan     `json:"plan"`
	Exit     ClosedTradeExit     `json:"exit"`
	PostExit ClosedTradePostExit `json:"post_exit"`
}

type ClosedTradeIdentity struct {
	Strategy         string              `json:"strategy"`
	RawStrategy      string              `json:"raw_strategy"`
	StrategyMissing  bool                `json:"strategy_missing"`
	SetupFamily      string              `json:"setup_family"`
	SetupSource      string              `json:"setup_source"`
	TradeHorizon     string              `json:"trade_horizon"`
	ExecBucket       string              `json:"exec_bucket"`
	EntryStyle       string              `json:"entry_style"`
	StrategyFamily   string              `json:"strategy_family"`
	Session          string              `json:"session"`
	Grade            string              `json:"grade"`
	ConfluenceScore  float64             `json:"confluence_score"`
	EntryTiming      string              `json:"entry_timing"`
	CandidateAgeSecs float64             `json:"candidate_age_seconds"`
	DistanceToVWAP   float64             `json:"distance_to_vwap_pct"`
	ATRExension      float64             `json:"atr_extension"`
	EntryScore       EntryScoreBreakdown `json:"entry_score,omitempty"`
}

type ClosedTradeEntry struct {
	EntryTs    time.Time `json:"entry_ts"`
	EntryPrice float64   `json:"entry_price"`
	Qty        float64   `json:"qty"`
	Leverage   int       `json:"leverage"`
	MarginUsed float64   `json:"margin_used"`
}

type ClosedTradePlan struct {
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

type ClosedTradeExit struct {
	ExitTs            time.Time `json:"exit_ts"`
	RealizedExitPrice float64   `json:"realized_exit_price"`
	Pct24hAtExit      float64   `json:"pct24h_at_exit,omitempty"`
	Pct4hAtExit       float64   `json:"pct4h_at_exit,omitempty"`
	Pct1hAtExit       float64   `json:"pct1h_at_exit,omitempty"`
	ExitReason        string    `json:"exit_reason"`
	RawExitReason     string    `json:"raw_exit_reason"`
	GrossPnL          float64   `json:"gross_pnl"`
	Fees              float64   `json:"fees"`
	NetPnL            float64   `json:"net_pnl"`
	HoldMinutes       float64   `json:"hold_minutes"`
	MaxRSeen          float64   `json:"max_r_seen"`
	MinRSeen          float64   `json:"min_r_seen"`
	StopOutType       string    `json:"stop_out_type,omitempty"`
	StopPriceAtExit   float64   `json:"stop_price_at_exit,omitempty"`
	FinalStopPrice    float64   `json:"final_stop_price,omitempty"`
	HitTP1            bool      `json:"hit_tp1,omitempty"`
	HitTP2            bool      `json:"hit_tp2,omitempty"`
	HitTP3            bool      `json:"hit_tp3,omitempty"`
	TPRatchetOnly     bool      `json:"tp_ratchet_only"`
	ProtectionState   string    `json:"protection_state,omitempty"`
	NoProofTriggered  bool      `json:"no_proof_triggered"`
	CloseType         string    `json:"close_type"`
	EntryOutcomeLabel string    `json:"entry_outcome_label,omitempty"`
}

type EntryScoreBreakdown struct {
	TrendScore      float64  `json:"trend_score,omitempty"`
	LocationScore   float64  `json:"location_score,omitempty"`
	TriggerScore    float64  `json:"trigger_score,omitempty"`
	FlowScore       float64  `json:"flow_score,omitempty"`
	RiskRewardScore float64  `json:"risk_reward_score,omitempty"`
	PenaltyScore    float64  `json:"penalty_score,omitempty"`
	FinalScore      float64  `json:"final_score,omitempty"`
	TrendLabel      string   `json:"trend_label,omitempty"`
	LocationLabel   string   `json:"location_label,omitempty"`
	TriggerLabel    string   `json:"trigger_label,omitempty"`
	FlowLabel       string   `json:"flow_label,omitempty"`
	RiskRewardLabel string   `json:"risk_reward_label,omitempty"`
	PenaltyReasons  []string `json:"penalty_reasons,omitempty"`
}

type ClosedTradePostExit struct {
	PeakPrice15m        float64   `json:"peak_price_15m,omitempty"`
	PeakPrice30m        float64   `json:"peak_price_30m,omitempty"`
	PeakPrice60m        float64   `json:"peak_price_60m,omitempty"`
	TroughPrice15m      float64   `json:"trough_price_15m,omitempty"`
	TroughPrice30m      float64   `json:"trough_price_30m,omitempty"`
	TroughPrice60m      float64   `json:"trough_price_60m,omitempty"`
	BestR15m            float64   `json:"best_r_15m,omitempty"`
	BestR30m            float64   `json:"best_r_30m,omitempty"`
	BestR60m            float64   `json:"best_r_60m,omitempty"`
	WorstR15m           float64   `json:"worst_r_15m,omitempty"`
	WorstR30m           float64   `json:"worst_r_30m,omitempty"`
	WorstR60m           float64   `json:"worst_r_60m,omitempty"`
	MissedTP1           bool      `json:"missed_tp1_after_exit,omitempty"`
	MissedTP2           bool      `json:"missed_tp2_after_exit,omitempty"`
	MissedTP3           bool      `json:"missed_tp3_after_exit,omitempty"`
	ExitVsTP1           float64   `json:"exit_vs_tp1_price_diff,omitempty"`
	ExitVsTP2           float64   `json:"exit_vs_tp2_price_diff,omitempty"`
	ExitVsTP3           float64   `json:"exit_vs_tp3_price_diff,omitempty"`
	PostExitPeakPrice   float64   `json:"post_exit_peak_price,omitempty"`
	PostExitPeakR       float64   `json:"post_exit_peak_r,omitempty"`
	EODPriceCST185959   float64   `json:"eod_price_cst_185959,omitempty"`
	EODPct24h           float64   `json:"eod_pct24h,omitempty"`
	EODPct4h            float64   `json:"eod_pct4h,omitempty"`
	EODPct1h            float64   `json:"eod_pct1h,omitempty"`
	EODTimestampCST     time.Time `json:"eod_timestamp_cst,omitempty"`
	EODTimestampUTC     time.Time `json:"eod_timestamp_utc,omitempty"`
	EODCapturePriceDiff float64   `json:"eod_vs_exit_price_diff,omitempty"`
	EODCaptureR         float64   `json:"eod_r,omitempty"`
	StoppedThenReclaim  bool      `json:"stopped_then_reclaim,omitempty"`
	ReentryWouldWork    bool      `json:"reentry_would_have_worked,omitempty"`
}

type TradeLabel struct {
	TradeID                    string            `json:"trade_id"`
	Symbol                     string            `json:"symbol"`
	Side                       string            `json:"side"`
	Setup                      string            `json:"setup"`
	SetupFamily                string            `json:"setup_family"`
	StrategyFamily             string            `json:"strategy_family"`
	EntryStyle                 string            `json:"entry_style"`
	ExecBucket                 string            `json:"exec_bucket"`
	Session                    string            `json:"session"`
	EntryTiming                string            `json:"entry_timing"`
	EntryOutcomeLabel          string            `json:"entry_outcome_label"`
	EntryFinalScore            float64           `json:"entry_final_score"`
	EntryScoreBucket           string            `json:"entry_score_bucket"`
	EntryTime                  time.Time         `json:"entry_ts"`
	ExitTime                   time.Time         `json:"exit_ts"`
	ScannerPatternEntry        string            `json:"scanner_pattern_entry"`
	ScannerPatternExit         string            `json:"scanner_pattern_exit"`
	ScannerPatternEOD          string            `json:"scanner_pattern_eod"`
	RealizedR                  float64           `json:"realized_r"`
	MaxRSeen                   float64           `json:"max_r_seen"`
	MinRSeen                   float64           `json:"min_r_seen"`
	PostExitPeakR              float64           `json:"post_exit_peak_r"`
	EODR                       float64           `json:"eod_r"`
	TPPath                     string            `json:"tp_path"`
	ExitQuality                string            `json:"exit_quality"`
	StopQuality                string            `json:"stop_quality"`
	StopOutType                string            `json:"stop_out_type"`
	ShakeoutCandidate          bool              `json:"shakeout_candidate"`
	ReentryCandidate           bool              `json:"reentry_candidate"`
	ReversalCandidate          bool              `json:"reversal_candidate"`
	WickThroughStopThenReclaim bool              `json:"wick_through_stop_then_reclaim"`
	StoppedThenReclaim         bool              `json:"stopped_then_reclaim"`
	ReentryWouldWork           bool              `json:"reentry_would_have_worked"`
	OppositeMoveR15m           float64           `json:"opposite_move_r_15m"`
	OppositeMoveR60m           float64           `json:"opposite_move_r_60m"`
	OppositeMoveREOD           float64           `json:"opposite_move_r_eod"`
	RuleCandidate              string            `json:"rule_candidate"`
	SampleWarning              string            `json:"sample_warning,omitempty"`
	Record                     ClosedTradeRecord `json:"-"`
}

type Summary struct {
	TradeCount                 int     `json:"trade_count"`
	WinRate                    float64 `json:"win_rate"`
	NetRealized                float64 `json:"net_realized"`
	AvgRealizedR               float64 `json:"avg_realized_r"`
	MedianRealizedR            float64 `json:"median_realized_r"`
	ExpectancyR                float64 `json:"expectancy_r"`
	ProfitFactor               float64 `json:"profit_factor"`
	AvgWinR                    float64 `json:"avg_win_r"`
	AvgLossR                   float64 `json:"avg_loss_r"`
	AvgHoldMin                 float64 `json:"avg_hold_min"`
	TP1TouchRate               float64 `json:"tp1_touch_rate"`
	TP2TouchRate               float64 `json:"tp2_touch_rate"`
	TP3TouchRate               float64 `json:"tp3_touch_rate"`
	ProfitLockStopRate         float64 `json:"profit_lock_stop_rate"`
	BreakevenStopRate          float64 `json:"breakeven_stop_rate"`
	LossStopRate               float64 `json:"loss_stop_rate"`
	StoppedThenReclaimRate     float64 `json:"stopped_then_reclaim_rate"`
	ReentryWouldHaveWorkedRate float64 `json:"reentry_would_have_worked_rate"`
	AvgPostExitPeakR           float64 `json:"avg_post_exit_peak_r"`
	AvgEODR                    float64 `json:"avg_eod_r"`
}

type GroupRow struct {
	Label                      string  `json:"label"`
	Side                       string  `json:"side,omitempty"`
	Trades                     int     `json:"trades"`
	Wins                       int     `json:"wins"`
	Losses                     int     `json:"losses"`
	WinRate                    float64 `json:"win_rate"`
	NetRealized                float64 `json:"net_realized"`
	AvgRealizedR               float64 `json:"avg_realized_r"`
	MedianRealizedR            float64 `json:"median_realized_r"`
	AvgHoldMin                 float64 `json:"avg_hold_min"`
	TP1TouchRate               float64 `json:"tp1_touch_rate"`
	TP2TouchRate               float64 `json:"tp2_touch_rate"`
	TP3TouchRate               float64 `json:"tp3_touch_rate"`
	LossStopRate               float64 `json:"loss_stop_rate"`
	BreakevenStopRate          float64 `json:"breakeven_stop_rate"`
	ProfitLockStopRate         float64 `json:"profit_lock_stop_rate"`
	AvgPostExitPeakR           float64 `json:"avg_post_exit_peak_r"`
	AvgEODR                    float64 `json:"avg_eod_r"`
	StoppedThenReclaimRate     float64 `json:"stopped_then_reclaim_rate"`
	ReentryWouldHaveWorkedRate float64 `json:"reentry_would_have_worked_rate"`
	OppositeSideCandidateRate  float64 `json:"opposite_side_candidate_rate,omitempty"`
	SampleWarning              string  `json:"sample_warning,omitempty"`
}

type RuleCandidates struct {
	AvoidSetups              []string `json:"avoid_setups"`
	PromoteSetups            []string `json:"promote_setups"`
	ReentryCandidates        []string `json:"reentry_candidates"`
	ReversalCandidates       []string `json:"reversal_candidates"`
	ExitAdjustmentCandidates []string `json:"exit_adjustment_candidates"`
}

type Outputs struct {
	Summary            Summary        `json:"summary"`
	BySetupFamily      []GroupRow     `json:"by_setup_family"`
	ByStrategyFamily   []GroupRow     `json:"by_strategy_family"`
	BySetup            []GroupRow     `json:"by_setup"`
	BySymbolSideSetup  []GroupRow     `json:"by_symbol_side_setup"`
	ByEntryTiming      []GroupRow     `json:"by_entry_timing"`
	ByEntryOutcome     []GroupRow     `json:"by_entry_outcome"`
	ByEntryScoreBucket []GroupRow     `json:"by_entry_score_bucket"`
	ByScannerPattern   []GroupRow     `json:"by_scanner_pattern"`
	ByStopOutType      []GroupRow     `json:"by_stop_out_type"`
	TradeLabels        []TradeLabel   `json:"trade_labels"`
	RuleCandidates     RuleCandidates `json:"rule_candidates"`
}
