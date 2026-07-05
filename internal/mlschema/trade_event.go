package mlschema

import "time"

const CurrentSchemaVersion = "mltrade.v1"

type MLTradeEvent struct {
	SchemaVersion string       `json:"schema_version"`
	EventType     string       `json:"event_type"`
	EventTime     time.Time    `json:"event_time"`
	TradeID       string       `json:"trade_id,omitempty"`
	CandidateID   string       `json:"candidate_id,omitempty"`
	ParentID      string       `json:"parent_id,omitempty"`
	Symbol        string       `json:"symbol"`
	Side          string       `json:"side"`
	ScannerState  ScannerState `json:"scanner_state,omitempty"`
	EntryContext  EntryContext `json:"entry_context,omitempty"`
	Management    Management   `json:"management,omitempty"`
	ExitOutcome   ExitOutcome  `json:"exit_outcome,omitempty"`
	PostExit      PostExit     `json:"post_exit,omitempty"`
}

type ScannerState struct {
	DayUTC24hPct      float64 `json:"day_utc_24h_pct"`
	UTC4hPct          float64 `json:"utc_4h_pct"`
	UTC1hPct          float64 `json:"utc_1h_pct"`
	FastSlope         float64 `json:"fast_slope"`
	SlowSlope         float64 `json:"slow_slope"`
	VolumeUSD         float64 `json:"volume_usd"`
	VolumeRatio       float64 `json:"volume_ratio"`
	LastClose         float64 `json:"last_close"`
	SessionVWAP       float64 `json:"session_vwap"`
	DistanceToVWAPPct float64 `json:"distance_to_vwap_pct"`
	ATR               float64 `json:"atr"`
	ATRPct            float64 `json:"atr_pct"`
	ExtensionATR      float64 `json:"extension_atr"`
	SpreadBps         float64 `json:"spread_bps"`
	BookImbalance     float64 `json:"book_imbalance"`
	OFIZ              float64 `json:"ofi_z"`
}

type EntryContext struct {
	StrategyID          string   `json:"strategy_id"`
	SetupFamily         string   `json:"setup_family"`
	SetupSource         string   `json:"setup_source"`
	TradeHorizon        string   `json:"trade_horizon"`
	Side                string   `json:"side"`
	Conf                float64  `json:"conf"`
	FinalRank           float64  `json:"final_rank"`
	DiscoveryScore      float64  `json:"discovery_score"`
	TriggerScore        float64  `json:"trigger_score"`
	ExecutionScore      float64  `json:"execution_score"`
	CombinedScore       float64  `json:"combined_score"`
	TradeQuality        float64  `json:"trade_quality"`
	SessionLabel        string   `json:"session_label"`
	EntryTiming         string   `json:"entry_timing"`
	CandidateAgeSeconds float64  `json:"candidate_age_seconds"`
	ExitProfile         string   `json:"exit_profile"`
	EntryPosture        string   `json:"entry_posture"`
	EntryPostureReason  string   `json:"entry_posture_reason"`
	QualityReasons      []string `json:"quality_reasons"`
}

type Management struct {
	TP1Hit          bool    `json:"tp1_hit"`
	TP2Hit          bool    `json:"tp2_hit"`
	TP3Hit          bool    `json:"tp3_hit"`
	MaxRBeforeExit  float64 `json:"max_r_before_exit"`
	MinRBeforeExit  float64 `json:"min_r_before_exit"`
	StopMovedToBE   bool    `json:"stop_moved_to_be"`
	StopMoveCount   int     `json:"stop_move_count"`
	ProtectionState string  `json:"protection_state"`
	FinalStopPrice  float64 `json:"final_stop_price"`
	HoldSeconds     float64 `json:"hold_seconds"`
	ManagePhase     string  `json:"manage_phase"`
}

type ExitOutcome struct {
	RawExitReason        string  `json:"raw_exit_reason"`
	NormalizedExitReason string  `json:"normalized_exit_reason"`
	StopType             string  `json:"stop_type"`
	RealizedPnL          float64 `json:"realized_pnl"`
	RealizedR            float64 `json:"realized_r"`
	FeeUSD               float64 `json:"fee_usd"`
	ClosedAt             string  `json:"closed_at"`
	ExitPrice            float64 `json:"exit_price"`
	Win                  bool    `json:"win"`
}

type PostExit struct {
	PostExitPeakR       float64 `json:"post_exit_peak_r"`
	PostExitMaxAdverseR float64 `json:"post_exit_max_adverse_r"`
	EODR                float64 `json:"eod_r"`
	StopThenReclaim     bool    `json:"stop_then_reclaim"`
	ReversalAfterExit   bool    `json:"reversal_after_exit"`
	ReentryWouldWin     bool    `json:"reentry_would_win"`
	Markout5mR          float64 `json:"markout_5m_r"`
	Markout15mR         float64 `json:"markout_15m_r"`
	Markout1hR          float64 `json:"markout_1h_r"`
	Markout4hR          float64 `json:"markout_4h_r"`
}
