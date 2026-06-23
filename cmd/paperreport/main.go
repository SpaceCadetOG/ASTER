package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type closedTradeRecord struct {
	TradeID  string                   `json:"trade_id"`
	Symbol   string                   `json:"symbol"`
	Side     string                   `json:"side"`
	Identity closedTradeIdentity      `json:"identity"`
	Entry    closedTradeEntry         `json:"entry"`
	Plan     closedTradePlan          `json:"plan"`
	Exit     closedTradeExit          `json:"exit"`
	PostExit closedTradePostExitStats `json:"post_exit"`
}

type closedTradeIdentity struct {
	Strategy         string  `json:"strategy"`
	SetupFamily      string  `json:"setup_family"`
	SetupSource      string  `json:"setup_source"`
	TradeHorizon     string  `json:"trade_horizon"`
	ExecBucket       string  `json:"exec_bucket"`
	EntryStyle       string  `json:"entry_style"`
	StrategyFamily   string  `json:"strategy_family"`
	Session          string  `json:"session"`
	Grade            string  `json:"grade"`
	ConfluenceScore  float64 `json:"confluence_score"`
	EntryTiming      string  `json:"entry_timing"`
	CandidateAgeSecs float64 `json:"candidate_age_seconds"`
	DistanceToVWAP   float64 `json:"distance_to_vwap_pct"`
	ATRExension      float64 `json:"atr_extension"`
}

type closedTradeEntry struct {
	EntryTs    time.Time `json:"entry_ts"`
	EntryPrice float64   `json:"entry_price"`
	Qty        float64   `json:"qty"`
	Leverage   int       `json:"leverage"`
	MarginUsed float64   `json:"margin_used"`
}

type closedTradePlan struct {
	OriginalStop float64 `json:"original_stop"`
	OriginalTP1  float64 `json:"original_tp1"`
	OriginalTP2  float64 `json:"original_tp2"`
	OriginalTP3  float64 `json:"original_tp3"`
}

type closedTradeExit struct {
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
	ProtectionState   string    `json:"protection_state"`
}

type closedTradePostExitStats struct {
	PeakPrice15m   float64 `json:"peak_price_15m"`
	PeakPrice30m   float64 `json:"peak_price_30m"`
	PeakPrice60m   float64 `json:"peak_price_60m"`
	TroughPrice15m float64 `json:"trough_price_15m"`
	TroughPrice30m float64 `json:"trough_price_30m"`
	TroughPrice60m float64 `json:"trough_price_60m"`
	BestR15m       float64 `json:"best_r_15m"`
	BestR30m       float64 `json:"best_r_30m"`
	BestR60m       float64 `json:"best_r_60m"`
	WorstR15m      float64 `json:"worst_r_15m"`
	WorstR30m      float64 `json:"worst_r_30m"`
	WorstR60m      float64 `json:"worst_r_60m"`
	MissedTP1      bool    `json:"missed_tp1_after_exit"`
	MissedTP2      bool    `json:"missed_tp2_after_exit"`
	MissedTP3      bool    `json:"missed_tp3_after_exit"`
	ExitVsTP1      float64 `json:"exit_vs_tp1_price_diff"`
	ExitVsTP2      float64 `json:"exit_vs_tp2_price_diff"`
	ExitVsTP3      float64 `json:"exit_vs_tp3_price_diff"`
}

type tradeRow struct {
	TradeID                         string    `json:"trade_id"`
	Symbol                          string    `json:"symbol"`
	Side                            string    `json:"side"`
	EntryTs                         time.Time `json:"entry_ts"`
	ExitTs                          time.Time `json:"exit_ts"`
	Strategy                        string    `json:"strategy"`
	SetupFamily                     string    `json:"setup_family"`
	SetupSource                     string    `json:"setup_source"`
	TradeHorizon                    string    `json:"trade_horizon"`
	ExecBucket                      string    `json:"exec_bucket"`
	Session                         string    `json:"session"`
	EntryStyle                      string    `json:"entry_style"`
	StrategyFamily                  string    `json:"strategy_family"`
	EntryTiming                     string    `json:"entry_timing"`
	EntryPrice                      float64   `json:"entry_price"`
	ExitPrice                       float64   `json:"exit_price"`
	OriginalStop                    float64   `json:"original_stop"`
	OriginalTP1                     float64   `json:"original_tp1"`
	OriginalTP2                     float64   `json:"original_tp2"`
	OriginalTP3                     float64   `json:"original_tp3"`
	Qty                             float64   `json:"qty"`
	Leverage                        int       `json:"leverage"`
	MarginUsed                      float64   `json:"margin_used"`
	NetPnL                          float64   `json:"net_pnl"`
	HoldMinutes                     float64   `json:"hold_minutes"`
	ExitReason                      string    `json:"exit_reason"`
	RawExitReason                   string    `json:"raw_exit_reason"`
	CandidateAgeSecs                float64   `json:"candidate_age_seconds"`
	DistanceToVWAP                  float64   `json:"distance_to_vwap"`
	ATRExension                     float64   `json:"atr_extension"`
	ConfluenceScore                 float64   `json:"confluence_score"`
	ProtectionState                 string    `json:"protection_state"`
	MaxRSeen                        float64   `json:"max_r_seen"`
	MinRSeen                        float64   `json:"min_r_seen"`
	PostExitBestR15m                float64   `json:"post_exit_best_r_15m"`
	PostExitBestR30m                float64   `json:"post_exit_best_r_30m"`
	PostExitBestR60m                float64   `json:"post_exit_best_r_60m"`
	PostExitWorstR60m               float64   `json:"post_exit_worst_r_60m"`
	PeakPrice60m                    float64   `json:"peak_price_60m"`
	TroughPrice60m                  float64   `json:"trough_price_60m"`
	MissedTP1                       bool      `json:"missed_tp1"`
	MissedTP2                       bool      `json:"missed_tp2"`
	MissedTP3                       bool      `json:"missed_tp3"`
	ExitVsTP1                       float64   `json:"exit_vs_tp1"`
	ExitVsTP2                       float64   `json:"exit_vs_tp2"`
	ExitVsTP3                       float64   `json:"exit_vs_tp3"`
	TP1DistancePct                  float64   `json:"tp1_distance_pct"`
	TP2DistancePct                  float64   `json:"tp2_distance_pct"`
	TP3DistancePct                  float64   `json:"tp3_distance_pct"`
	PostExitPeakPrice60m            float64   `json:"post_exit_peak_price_60m"`
	PostExitOpportunityPriceDiff60m float64   `json:"post_exit_opportunity_price_diff_60m"`
	PostExitOpportunityPct60m       float64   `json:"post_exit_opportunity_pct_60m"`
	PostExitOpportunityPnL60m       float64   `json:"post_exit_opportunity_pnl_60m"`
	PostExitOpportunityR60m         float64   `json:"post_exit_opportunity_r_60m"`
	EntryImprovementAction          string    `json:"entry_improvement_action"`
	ExitImprovementAction           string    `json:"exit_improvement_action"`
	TradeSource                     string    `json:"trade_source"`
}

type bucketSummary struct {
	Label        string  `json:"label"`
	Trades       int     `json:"trades"`
	Wins         int     `json:"wins"`
	Losses       int     `json:"losses"`
	WinRate      float64 `json:"win_rate"`
	NetPnL       float64 `json:"net_pnl"`
	AvgHoldMin   float64 `json:"avg_hold_min"`
	AvgBestR15m  float64 `json:"avg_best_r_15m"`
	AvgBestR60m  float64 `json:"avg_best_r_60m"`
	AvgExitVsTP1 float64 `json:"avg_exit_vs_tp1"`
}

type filterImpact struct {
	Label        string  `json:"label"`
	Removed      int     `json:"removed"`
	Remaining    int     `json:"remaining"`
	NetPnL       float64 `json:"net_pnl"`
	WinRate      float64 `json:"win_rate"`
	RemovedNet   float64 `json:"removed_net"`
	RemovedWinRt float64 `json:"removed_win_rate"`
}

type report struct {
	GeneratedAtUTC    string          `json:"generated_at_utc"`
	Input             string          `json:"input"`
	Source            string          `json:"source"`
	Overall           bucketSummary   `json:"overall"`
	BySetup           []bucketSummary `json:"by_setup"`
	BySetupSource     []bucketSummary `json:"by_setup_source"`
	ByTradeHorizon    []bucketSummary `json:"by_trade_horizon"`
	ByStrategy        []bucketSummary `json:"by_strategy"`
	BySession         []bucketSummary `json:"by_session"`
	ByExitReason      []bucketSummary `json:"by_exit_reason"`
	ByEntryTiming     []bucketSummary `json:"by_entry_timing"`
	ByEntryAction     []bucketSummary `json:"by_entry_action"`
	ByExitAction      []bucketSummary `json:"by_exit_action"`
	FilterImpacts     []filterImpact  `json:"filter_impacts"`
	WorstTrades       []tradeRow      `json:"worst_trades"`
	BestTrades        []tradeRow      `json:"best_trades"`
	BiggestMissedRuns []tradeRow      `json:"biggest_missed_runs"`
}

func main() {
	var (
		closedJSONL = flag.String("closed-trades-jsonl", "", "Path to paper closed trades JSONL")
		tradesCSV   = flag.String("trades-csv", "", "Path to legacy trades CSV")
		outDir      = flag.String("out-dir", "", "Optional output directory")
		topN        = flag.Int("top-n", 8, "How many best/worst trades to show")
	)
	flag.Parse()

	rows, source, input, err := loadTrades(strings.TrimSpace(*closedJSONL), strings.TrimSpace(*tradesCSV))
	if err != nil {
		fail("%v", err)
	}
	if len(rows) == 0 {
		fail("no trades found")
	}
	rep := buildReport(rows, source, input, *topN)
	printConsoleReport(rep)
	if strings.TrimSpace(*outDir) != "" {
		if err := writeOutputs(strings.TrimSpace(*outDir), rep); err != nil {
			fail("write outputs: %v", err)
		}
		fmt.Printf("wrote: %s\n", strings.TrimSpace(*outDir))
	}
}

func loadTrades(closedJSONL, tradesCSV string) ([]tradeRow, string, string, error) {
	switch {
	case closedJSONL != "":
		rows, err := loadClosedTradesJSONL(closedJSONL)
		return rows, "closed_trades_jsonl", closedJSONL, err
	case tradesCSV != "":
		rows, err := loadLegacyTradesCSV(tradesCSV)
		return rows, "legacy_trades_csv", tradesCSV, err
	default:
		return nil, "", "", fmt.Errorf("set -closed-trades-jsonl or -trades-csv")
	}
}

func loadClosedTradesJSONL(path string) ([]tradeRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	rows := make([]tradeRow, 0, 256)
	for dec.More() {
		var rec closedTradeRecord
		if err := dec.Decode(&rec); err != nil {
			return nil, err
		}
		rows = append(rows, tradeRow{
			TradeID:           rec.TradeID,
			Symbol:            strings.ToUpper(strings.TrimSpace(rec.Symbol)),
			Side:              strings.ToUpper(strings.TrimSpace(rec.Side)),
			EntryTs:           rec.Entry.EntryTs.UTC(),
			ExitTs:            rec.Exit.ExitTs.UTC(),
			Strategy:          normalizedLabel(rec.Identity.Strategy),
			SetupFamily:       normalizedLabel(rec.Identity.SetupFamily),
			SetupSource:       normalizedLabel(rec.Identity.SetupSource),
			TradeHorizon:      normalizedLabel(rec.Identity.TradeHorizon),
			ExecBucket:        normalizedLabel(rec.Identity.ExecBucket),
			Session:           normalizedLabel(rec.Identity.Session),
			EntryStyle:        normalizedLabel(rec.Identity.EntryStyle),
			StrategyFamily:    normalizedLabel(rec.Identity.StrategyFamily),
			EntryTiming:       normalizedLabel(rec.Identity.EntryTiming),
			EntryPrice:        rec.Entry.EntryPrice,
			ExitPrice:         rec.Exit.RealizedExitPrice,
			OriginalStop:      rec.Plan.OriginalStop,
			OriginalTP1:       rec.Plan.OriginalTP1,
			OriginalTP2:       rec.Plan.OriginalTP2,
			OriginalTP3:       rec.Plan.OriginalTP3,
			Qty:               rec.Entry.Qty,
			Leverage:          rec.Entry.Leverage,
			MarginUsed:        rec.Entry.MarginUsed,
			NetPnL:            rec.Exit.NetPnL,
			HoldMinutes:       rec.Exit.HoldMinutes,
			ExitReason:        normalizedLabel(rec.Exit.ExitReason),
			RawExitReason:     normalizedLabel(rec.Exit.RawExitReason),
			CandidateAgeSecs:  rec.Identity.CandidateAgeSecs,
			DistanceToVWAP:    rec.Identity.DistanceToVWAP,
			ATRExension:       rec.Identity.ATRExension,
			ConfluenceScore:   rec.Identity.ConfluenceScore,
			ProtectionState:   normalizedLabel(rec.Exit.ProtectionState),
			MaxRSeen:          rec.Exit.MaxRSeen,
			MinRSeen:          rec.Exit.MinRSeen,
			PostExitBestR15m:  rec.PostExit.BestR15m,
			PostExitBestR30m:  rec.PostExit.BestR30m,
			PostExitBestR60m:  rec.PostExit.BestR60m,
			PostExitWorstR60m: rec.PostExit.WorstR60m,
			PeakPrice60m:      rec.PostExit.PeakPrice60m,
			TroughPrice60m:    rec.PostExit.TroughPrice60m,
			MissedTP1:         rec.PostExit.MissedTP1,
			MissedTP2:         rec.PostExit.MissedTP2,
			MissedTP3:         rec.PostExit.MissedTP3,
			ExitVsTP1:         rec.PostExit.ExitVsTP1,
			ExitVsTP2:         rec.PostExit.ExitVsTP2,
			ExitVsTP3:         rec.PostExit.ExitVsTP3,
			TradeSource:       "jsonl",
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ExitTs.Before(rows[j].ExitTs) })
	return rows, nil
}

func loadLegacyTradesCSV(path string) ([]tradeRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	all, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(all) < 2 {
		return nil, nil
	}
	idx := map[string]int{}
	for i, h := range all[0] {
		idx[strings.TrimSpace(h)] = i
	}
	rows := make([]tradeRow, 0, len(all)-1)
	for _, row := range all[1:] {
		rows = append(rows, tradeRow{
			TradeID:          csvCell(row, idx, "trade_id"),
			Symbol:           strings.ToUpper(csvCell(row, idx, "symbol")),
			Side:             strings.ToUpper(csvCell(row, idx, "side")),
			EntryTs:          parseCSVTime(firstNonEmpty(csvCell(row, idx, "entry_ts"), csvCell(row, idx, "entry_day"))),
			ExitTs:           parseCSVTime(firstNonEmpty(csvCell(row, idx, "exit_ts"), csvCell(row, idx, "exit_day"))),
			Strategy:         normalizedLabel(csvCell(row, idx, "strategy")),
			SetupFamily:      normalizedLabel(firstNonEmpty(csvCell(row, idx, "setup_family"), csvCell(row, idx, "setup"))),
			SetupSource:      normalizedLabel(csvCell(row, idx, "setup_source")),
			TradeHorizon:     normalizedLabel(csvCell(row, idx, "trade_horizon")),
			ExecBucket:       normalizedLabel(csvCell(row, idx, "exec_bucket")),
			Session:          normalizedLabel(csvCell(row, idx, "session")),
			EntryStyle:       normalizedLabel(csvCell(row, idx, "entry_style")),
			StrategyFamily:   normalizedLabel(csvCell(row, idx, "strategy_family")),
			EntryTiming:      normalizedLabel(csvCell(row, idx, "entry_timing")),
			EntryPrice:       parseCSVFloat(firstNonEmpty(csvCell(row, idx, "entry"), csvCell(row, idx, "entry_price"))),
			ExitPrice:        parseCSVFloat(firstNonEmpty(csvCell(row, idx, "realized_exit_price"), csvCell(row, idx, "exit_price"), csvCell(row, idx, "exit"))),
			OriginalStop:     parseCSVFloat(csvCell(row, idx, "original_stop")),
			OriginalTP1:      parseCSVFloat(csvCell(row, idx, "original_tp1")),
			OriginalTP2:      parseCSVFloat(csvCell(row, idx, "original_tp2")),
			OriginalTP3:      parseCSVFloat(csvCell(row, idx, "original_tp3")),
			Qty:              parseCSVFloat(csvCell(row, idx, "qty")),
			Leverage:         int(parseCSVFloat(csvCell(row, idx, "lev"))),
			MarginUsed:       parseCSVFloat(csvCell(row, idx, "margin")),
			NetPnL:           parseCSVFloat(firstNonEmpty(csvCell(row, idx, "net_pnl"), csvCell(row, idx, "realized"), csvCell(row, idx, "pnl"))),
			HoldMinutes:      parseCSVFloat(firstNonEmpty(csvCell(row, idx, "hold_min"), csvCell(row, idx, "hold_minutes"))),
			ExitReason:       normalizedLabel(firstNonEmpty(csvCell(row, idx, "normalized_exit_reason"), csvCell(row, idx, "exit_reason"))),
			RawExitReason:    normalizedLabel(firstNonEmpty(csvCell(row, idx, "raw_exit_reason"), csvCell(row, idx, "exit_reason"))),
			CandidateAgeSecs: parseCSVFloat(csvCell(row, idx, "candidate_age_seconds")),
			DistanceToVWAP:   parseCSVFloat(csvCell(row, idx, "distance_to_vwap")),
			ATRExension:      parseCSVFloat(csvCell(row, idx, "atr_extension")),
			ProtectionState:  normalizedLabel(csvCell(row, idx, "protection_state")),
			MaxRSeen:         parseCSVFloat(csvCell(row, idx, "max_r_seen")),
			MinRSeen:         parseCSVFloat(csvCell(row, idx, "min_r_seen")),
			TradeSource:      "csv",
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ExitTs.Before(rows[j].ExitTs) })
	return rows, nil
}

func buildReport(rows []tradeRow, source, input string, topN int) report {
	if topN <= 0 {
		topN = 8
	}
	enriched := make([]tradeRow, 0, len(rows))
	for _, row := range rows {
		enriched = append(enriched, enrichTradeRow(row))
	}
	out := report{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		Input:          input,
		Source:         source,
		Overall:        summarizeBucket("overall", enriched),
		BySetup:        summarizeBy(enriched, func(r tradeRow) string { return r.SetupFamily }),
		BySetupSource:  summarizeBy(enriched, func(r tradeRow) string { return r.SetupSource }),
		ByTradeHorizon: summarizeBy(enriched, func(r tradeRow) string { return r.TradeHorizon }),
		ByStrategy:     summarizeBy(enriched, func(r tradeRow) string { return r.Strategy }),
		BySession:      summarizeBy(enriched, func(r tradeRow) string { return r.Session }),
		ByExitReason:   summarizeBy(enriched, func(r tradeRow) string { return r.ExitReason }),
		ByEntryTiming:  summarizeBy(enriched, func(r tradeRow) string { return r.EntryTiming }),
		ByEntryAction:  summarizeBy(enriched, func(r tradeRow) string { return r.EntryImprovementAction }),
		ByExitAction:   summarizeBy(enriched, func(r tradeRow) string { return r.ExitImprovementAction }),
		FilterImpacts: []filterImpact{
			buildFilterImpact(enriched, "exclude_setup_none", func(r tradeRow) bool { return !isNoneLike(r.SetupFamily) }),
			buildFilterImpact(enriched, "exclude_candidate_age_gt_300", func(r tradeRow) bool { return r.CandidateAgeSecs <= 0 || r.CandidateAgeSecs <= 300 }),
			buildFilterImpact(enriched, "exclude_atr_extension_gt_1_5", func(r tradeRow) bool { return r.ATRExension <= 0 || r.ATRExension <= 1.5 }),
			buildFilterImpact(enriched, "exclude_utc_off_hours", func(r tradeRow) bool { return !strings.EqualFold(r.Session, "UTC_OFF_HOURS") }),
			buildFilterImpact(enriched, "combo_clean_core", func(r tradeRow) bool {
				if isNoneLike(r.SetupFamily) {
					return false
				}
				if r.CandidateAgeSecs > 300 {
					return false
				}
				if r.ATRExension > 1.5 {
					return false
				}
				if strings.EqualFold(r.Session, "UTC_OFF_HOURS") {
					return false
				}
				return true
			}),
		},
	}

	sorted := append([]tradeRow(nil), enriched...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].NetPnL < sorted[j].NetPnL })
	out.WorstTrades = append([]tradeRow(nil), sorted[:minInt(topN, len(sorted))]...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].NetPnL > sorted[j].NetPnL })
	out.BestTrades = append([]tradeRow(nil), sorted[:minInt(topN, len(sorted))]...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PostExitOpportunityPnL60m > sorted[j].PostExitOpportunityPnL60m })
	out.BiggestMissedRuns = append([]tradeRow(nil), sorted[:minInt(topN, len(sorted))]...)
	return out
}

func enrichTradeRow(row tradeRow) tradeRow {
	row.TP1DistancePct = percentDistanceFromEntry(row.EntryPrice, row.ExitVsTP1)
	row.TP2DistancePct = percentDistanceFromEntry(row.EntryPrice, row.ExitVsTP2)
	row.TP3DistancePct = percentDistanceFromEntry(row.EntryPrice, row.ExitVsTP3)
	row.PostExitPeakPrice60m = sideAwarePeakPrice60m(row)
	row.PostExitOpportunityPriceDiff60m = sideAwareOpportunityPriceDiff60m(row)
	row.PostExitOpportunityPct60m = percentDistanceFromEntry(row.ExitPrice, row.PostExitOpportunityPriceDiff60m)
	row.PostExitOpportunityPnL60m = row.PostExitOpportunityPriceDiff60m * row.Qty
	row.PostExitOpportunityR60m = maxFloat(0, row.PostExitBestR60m-row.MaxRSeen)
	row.EntryImprovementAction = classifyEntryImprovement(row)
	row.ExitImprovementAction = classifyExitImprovement(row)
	return row
}

func sideAwarePeakPrice60m(row tradeRow) float64 {
	if strings.EqualFold(row.Side, "SELL") {
		if row.TroughPrice60m > 0 {
			return row.TroughPrice60m
		}
		return row.ExitPrice
	}
	if row.PeakPrice60m > 0 {
		return row.PeakPrice60m
	}
	return row.ExitPrice
}

func sideAwareOpportunityPriceDiff60m(row tradeRow) float64 {
	peak := sideAwarePeakPrice60m(row)
	if peak <= 0 || row.ExitPrice <= 0 {
		return 0
	}
	if strings.EqualFold(row.Side, "SELL") {
		return row.ExitPrice - peak
	}
	return peak - row.ExitPrice
}

func classifyEntryImprovement(row tradeRow) string {
	switch {
	case isNoneLike(row.SetupFamily):
		return "resolve_setup_before_entry"
	case row.CandidateAgeSecs > 300:
		return "enter_earlier_or_expire_candidate"
	case row.ATRExension > 1.5:
		return "wait_for_pullback_after_extension"
	case strings.EqualFold(row.Session, "UTC_OFF_HOURS"):
		return "restrict_off_hours_autonomy"
	case row.DistanceToVWAP > 5:
		return "prefer_closer_to_vwap_or_reclaim"
	default:
		return "entry_ok"
	}
}

func classifyExitImprovement(row tradeRow) string {
	switch {
	case row.PostExitOpportunityR60m >= 1.0 || row.MissedTP3:
		return "let_runner_breathe_after_partial"
	case row.PostExitOpportunityR60m >= 0.5 || row.MissedTP2:
		return "hold_secondary_target_longer"
	case strings.EqualFold(row.ProtectionState, "original") && row.MaxRSeen >= 0.75 && row.NetPnL < 0:
		return "tighten_after_initial_proof"
	case strings.EqualFold(row.ExitReason, "SL") && row.MaxRSeen <= 0.20 && row.NetPnL < 0:
		return "entry_failed_fast_no_exit_issue"
	default:
		return "exit_ok"
	}
}

func summarizeBy(rows []tradeRow, keyFn func(tradeRow) string) []bucketSummary {
	buckets := map[string][]tradeRow{}
	for _, row := range rows {
		key := normalizedLabel(keyFn(row))
		buckets[key] = append(buckets[key], row)
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		li := summarizeBucket(keys[i], buckets[keys[i]])
		lj := summarizeBucket(keys[j], buckets[keys[j]])
		if li.NetPnL == lj.NetPnL {
			return li.Trades > lj.Trades
		}
		return li.NetPnL > lj.NetPnL
	})
	out := make([]bucketSummary, 0, len(keys))
	for _, k := range keys {
		out = append(out, summarizeBucket(k, buckets[k]))
	}
	return out
}

func summarizeBucket(label string, rows []tradeRow) bucketSummary {
	out := bucketSummary{Label: normalizedLabel(label), Trades: len(rows)}
	if len(rows) == 0 {
		return out
	}
	best15 := 0.0
	best60 := 0.0
	exitVsTP1 := 0.0
	hold := 0.0
	for _, row := range rows {
		if row.NetPnL >= 0 {
			out.Wins++
		} else {
			out.Losses++
		}
		out.NetPnL += row.NetPnL
		hold += row.HoldMinutes
		best15 += row.PostExitBestR15m
		best60 += row.PostExitBestR60m
		exitVsTP1 += row.ExitVsTP1
	}
	out.WinRate = float64(out.Wins) / float64(len(rows))
	out.AvgHoldMin = hold / float64(len(rows))
	out.AvgBestR15m = best15 / float64(len(rows))
	out.AvgBestR60m = best60 / float64(len(rows))
	out.AvgExitVsTP1 = exitVsTP1 / float64(len(rows))
	return out
}

func buildFilterImpact(rows []tradeRow, label string, keepFn func(tradeRow) bool) filterImpact {
	kept := make([]tradeRow, 0, len(rows))
	removed := make([]tradeRow, 0, len(rows))
	for _, row := range rows {
		if keepFn(row) {
			kept = append(kept, row)
		} else {
			removed = append(removed, row)
		}
	}
	keepSum := summarizeBucket(label, kept)
	removedSum := summarizeBucket(label, removed)
	return filterImpact{
		Label:        label,
		Removed:      len(removed),
		Remaining:    len(kept),
		NetPnL:       keepSum.NetPnL,
		WinRate:      keepSum.WinRate,
		RemovedNet:   removedSum.NetPnL,
		RemovedWinRt: removedSum.WinRate,
	}
}

func printConsoleReport(rep report) {
	fmt.Printf("paper report\n")
	fmt.Printf("source=%s input=%s generated=%s\n", rep.Source, rep.Input, rep.GeneratedAtUTC)
	fmt.Printf("overall trades=%d wins=%d losses=%d winrate=%.1f%% net=%.4f avg_hold=%.1fm avg_best_r15=%.3f avg_best_r60=%.3f\n",
		rep.Overall.Trades, rep.Overall.Wins, rep.Overall.Losses, rep.Overall.WinRate*100, rep.Overall.NetPnL, rep.Overall.AvgHoldMin, rep.Overall.AvgBestR15m, rep.Overall.AvgBestR60m)
	printSummaryTable("by setup", rep.BySetup, 8)
	printSummaryTable("by strategy", rep.ByStrategy, 8)
	printSummaryTable("by session", rep.BySession, 8)
	printSummaryTable("by exit", rep.ByExitReason, 8)
	printSummaryTable("entry improvements", rep.ByEntryAction, 8)
	printSummaryTable("exit improvements", rep.ByExitAction, 8)
	fmt.Printf("\nfilter impacts\n")
	for _, f := range rep.FilterImpacts {
		fmt.Printf("- %s remaining=%d removed=%d keep_net=%.4f keep_wr=%.1f%% removed_net=%.4f removed_wr=%.1f%%\n",
			f.Label, f.Remaining, f.Removed, f.NetPnL, f.WinRate*100, f.RemovedNet, f.RemovedWinRt*100)
	}
	fmt.Printf("\nworst trades\n")
	for _, row := range rep.WorstTrades {
		fmt.Printf("- %s %s %s setup=%s session=%s pnl=%.4f exit=%s age=%.0fs atr=%.3f\n",
			row.Symbol, row.Side, row.Strategy, row.SetupFamily, row.Session, row.NetPnL, row.ExitReason, row.CandidateAgeSecs, row.ATRExension)
	}
	fmt.Printf("\nbest trades\n")
	for _, row := range rep.BestTrades {
		fmt.Printf("- %s %s %s setup=%s session=%s pnl=%.4f exit=%s age=%.0fs atr=%.3f\n",
			row.Symbol, row.Side, row.Strategy, row.SetupFamily, row.Session, row.NetPnL, row.ExitReason, row.CandidateAgeSecs, row.ATRExension)
	}
	fmt.Printf("\nbiggest missed runs\n")
	for _, row := range rep.BiggestMissedRuns {
		fmt.Printf("- %s %s setup=%s exit=%s post_exit_extra=%.4f extra_r=%.2f exit_action=%s\n",
			row.Symbol, row.Side, row.SetupFamily, row.ExitReason, row.PostExitOpportunityPnL60m, row.PostExitOpportunityR60m, row.ExitImprovementAction)
	}
}

func printSummaryTable(title string, rows []bucketSummary, limit int) {
	fmt.Printf("\n%s\n", title)
	for _, row := range rows[:minInt(limit, len(rows))] {
		fmt.Printf("- %s trades=%d wr=%.1f%% net=%.4f avg_hold=%.1fm avg_best_r15=%.3f\n",
			row.Label, row.Trades, row.WinRate*100, row.NetPnL, row.AvgHoldMin, row.AvgBestR15m)
	}
}

func writeOutputs(outDir string, rep report) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	jsonPath := filepath.Join(outDir, "paper_report.json")
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, b, 0o644); err != nil {
		return err
	}
	mdPath := filepath.Join(outDir, "paper_report.md")
	if err := os.WriteFile(mdPath, []byte(markdownReport(rep)), 0o644); err != nil {
		return err
	}
	return nil
}

func markdownReport(rep report) string {
	var b strings.Builder
	b.WriteString("# Paper Trade Report\n\n")
	b.WriteString(fmt.Sprintf("- Generated UTC: `%s`\n", rep.GeneratedAtUTC))
	b.WriteString(fmt.Sprintf("- Source: `%s`\n", rep.Source))
	b.WriteString(fmt.Sprintf("- Input: `%s`\n", rep.Input))
	b.WriteString(fmt.Sprintf("- Overall: trades=%d wins=%d losses=%d winrate=%.1f%% net=%.4f avg_hold=%.1fm\n\n",
		rep.Overall.Trades, rep.Overall.Wins, rep.Overall.Losses, rep.Overall.WinRate*100, rep.Overall.NetPnL, rep.Overall.AvgHoldMin))
	writeBucketSection(&b, "By Setup", rep.BySetup, 12)
	writeBucketSection(&b, "By Strategy", rep.ByStrategy, 12)
	writeBucketSection(&b, "By Session", rep.BySession, 12)
	writeBucketSection(&b, "By Exit Reason", rep.ByExitReason, 12)
	writeBucketSection(&b, "Entry Improvements", rep.ByEntryAction, 12)
	writeBucketSection(&b, "Exit Improvements", rep.ByExitAction, 12)
	b.WriteString("## Filter Impacts\n\n")
	for _, f := range rep.FilterImpacts {
		b.WriteString(fmt.Sprintf("- `%s`: remaining=%d removed=%d keep_net=%.4f keep_wr=%.1f%% removed_net=%.4f removed_wr=%.1f%%\n",
			f.Label, f.Remaining, f.Removed, f.NetPnL, f.WinRate*100, f.RemovedNet, f.RemovedWinRt*100))
	}
	b.WriteString("\n## Biggest Missed Runs\n\n")
	for _, row := range rep.BiggestMissedRuns {
		b.WriteString(fmt.Sprintf("- `%s %s`: setup=%s exit=%s post_exit_extra=%.4f extra_r=%.2f entry_action=%s exit_action=%s\n",
			row.Symbol, row.Side, row.SetupFamily, row.ExitReason, row.PostExitOpportunityPnL60m, row.PostExitOpportunityR60m, row.EntryImprovementAction, row.ExitImprovementAction))
	}
	return b.String()
}

func writeBucketSection(b *strings.Builder, title string, rows []bucketSummary, limit int) {
	b.WriteString("## " + title + "\n\n")
	for _, row := range rows[:minInt(limit, len(rows))] {
		b.WriteString(fmt.Sprintf("- `%s`: trades=%d wr=%.1f%% net=%.4f avg_hold=%.1fm avg_best_r15=%.3f avg_best_r60=%.3f\n",
			row.Label, row.Trades, row.WinRate*100, row.NetPnL, row.AvgHoldMin, row.AvgBestR15m, row.AvgBestR60m))
	}
	b.WriteString("\n")
}

func csvCell(row []string, idx map[string]int, key string) string {
	i, ok := idx[key]
	if !ok || i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func parseCSVFloat(v string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return f
}

func parseCSVTime(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err == nil {
		return t.UTC()
	}
	t, err = time.Parse("2006-01-02", v)
	if err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func normalizedLabel(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	return v
}

func percentDistanceFromEntry(base, diff float64) float64 {
	if base == 0 {
		return 0
	}
	return (diff / base) * 100
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func isNoneLike(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", "none", "unknown", "no_strategy", "unresolved":
		return true
	default:
		return false
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
