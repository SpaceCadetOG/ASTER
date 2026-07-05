package mlschema

import (
	"strconv"
	"strings"
	"time"

	"go-machine/internal/paperreport"
)

type FeatureRow struct {
	TradeID              string
	EntryTs              time.Time
	Symbol               string
	Side                 string
	StrategyID           string
	SetupFamily          string
	SessionLabel         string
	EntryTiming          string
	DayUTC24hPct         float64
	UTC4hPct             float64
	UTC1hPct             float64
	VolumeRatio          float64
	DistanceToVWAPPct    float64
	ATRPct               float64
	ExtensionATR         float64
	SpreadBps            float64
	BookImbalance        float64
	OFIZ                 float64
	CombinedScore        float64
	TradeQuality         float64
	CandidateAgeSeconds  float64
	TP1Hit               bool
	TP2Hit               bool
	TP3Hit               bool
	HoldSeconds          float64
	MaxRBeforeExit       float64
	RealizedR            float64
	Win                  bool
	NormalizedExitReason string
	PostExitPeakR        float64
	StopThenReclaim      bool
	ReentryWouldWin      bool
}

func FeatureCSVHeader() []string {
	return []string{
		"trade_id", "entry_ts", "symbol", "side", "strategy_id", "setup_family", "session_label", "entry_timing",
		"day_utc_24h_pct", "utc_4h_pct", "utc_1h_pct", "volume_ratio", "distance_to_vwap_pct",
		"atr_pct", "extension_atr", "spread_bps", "book_imbalance", "ofi_z",
		"combined_score", "trade_quality", "candidate_age_seconds",
		"tp1_hit", "tp2_hit", "tp3_hit", "hold_seconds", "max_r_before_exit",
		"realized_r", "win", "normalized_exit_reason", "post_exit_peak_r", "stop_then_reclaim", "reentry_would_win",
	}
}

func BuildFeatureRow(rec paperreport.ClosedTradeRecord) FeatureRow {
	realizedR := realizedRFromRecord(rec)
	return FeatureRow{
		TradeID:              strings.TrimSpace(rec.TradeID),
		EntryTs:              rec.Entry.EntryTs.UTC(),
		Symbol:               strings.ToUpper(strings.TrimSpace(rec.Symbol)),
		Side:                 NormalizeSide(rec.Side),
		StrategyID:           firstNonEmpty(rec.Identity.Strategy, rec.Identity.RawStrategy, "unknown"),
		SetupFamily:          firstNonEmpty(rec.Identity.SetupFamily, "unknown"),
		SessionLabel:         firstNonEmpty(rec.Identity.Session, "unknown"),
		EntryTiming:          firstNonEmpty(rec.Identity.EntryTiming, "unknown"),
		DayUTC24hPct:         rec.Plan.Pct24hAtEntry,
		UTC4hPct:             rec.Plan.Pct4hAtEntry,
		UTC1hPct:             rec.Plan.Pct1hAtEntry,
		VolumeRatio:          0,
		DistanceToVWAPPct:    rec.Identity.DistanceToVWAP,
		ATRPct:               0,
		ExtensionATR:         rec.Identity.ATRExension,
		SpreadBps:            0,
		BookImbalance:        0,
		OFIZ:                 0,
		CombinedScore:        rec.Identity.ConfluenceScore,
		TradeQuality:         0,
		CandidateAgeSeconds:  rec.Identity.CandidateAgeSecs,
		TP1Hit:               rec.Exit.HitTP1,
		TP2Hit:               rec.Exit.HitTP2,
		TP3Hit:               rec.Exit.HitTP3,
		HoldSeconds:          rec.Exit.HoldMinutes * 60.0,
		MaxRBeforeExit:       rec.Exit.MaxRSeen,
		RealizedR:            realizedR,
		Win:                  WinFromRealizedR(realizedR),
		NormalizedExitReason: firstNonEmpty(rec.Exit.ExitReason, rec.Exit.RawExitReason, "unknown"),
		PostExitPeakR:        rec.PostExit.PostExitPeakR,
		StopThenReclaim:      rec.PostExit.StoppedThenReclaim,
		ReentryWouldWin:      rec.PostExit.ReentryWouldWork,
	}
}

func (r FeatureRow) CSVRecord() []string {
	return []string{
		r.TradeID,
		r.EntryTs.Format(time.RFC3339),
		r.Symbol,
		r.Side,
		r.StrategyID,
		r.SetupFamily,
		r.SessionLabel,
		r.EntryTiming,
		f(r.DayUTC24hPct),
		f(r.UTC4hPct),
		f(r.UTC1hPct),
		f(r.VolumeRatio),
		f(r.DistanceToVWAPPct),
		f(r.ATRPct),
		f(r.ExtensionATR),
		f(r.SpreadBps),
		f(r.BookImbalance),
		f(r.OFIZ),
		f(r.CombinedScore),
		f(r.TradeQuality),
		f(r.CandidateAgeSeconds),
		b(r.TP1Hit),
		b(r.TP2Hit),
		b(r.TP3Hit),
		f(r.HoldSeconds),
		f(r.MaxRBeforeExit),
		f(r.RealizedR),
		b(r.Win),
		r.NormalizedExitReason,
		f(r.PostExitPeakR),
		b(r.StopThenReclaim),
		b(r.ReentryWouldWin),
	}
}

func realizedRFromRecord(rec paperreport.ClosedTradeRecord) float64 {
	risk := rec.Plan.PlannedRiskPrice
	if risk <= 0 && rec.Entry.EntryPrice > 0 && rec.Plan.OriginalStop > 0 {
		risk = abs(rec.Entry.EntryPrice - rec.Plan.OriginalStop)
	}
	if risk <= 0 || rec.Entry.Qty <= 0 {
		return 0
	}
	return rec.Exit.NetPnL / (risk * rec.Entry.Qty)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func f(v float64) string {
	return strconv.FormatFloat(Rounded(v), 'f', -1, 64)
}

func b(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
