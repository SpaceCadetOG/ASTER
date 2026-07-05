package mltrain

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

var DefaultNumericFeatures = []string{
	"day_utc_24h_pct",
	"utc_4h_pct",
	"utc_1h_pct",
	"volume_ratio",
	"distance_to_vwap_pct",
	"atr_pct",
	"extension_atr",
	"spread_bps",
	"book_imbalance",
	"ofi_z",
	"combined_score",
	"trade_quality",
	"candidate_age_seconds",
}

var DefaultCategoricalFeatures = []string{
	"side",
	"strategy_id",
	"setup_family",
	"session_label",
	"entry_timing",
}

var ForbiddenInputColumns = map[string]bool{
	"realized_r":             true,
	"realized_pnl":           true,
	"win":                    true,
	"tp1_hit":                true,
	"tp2_hit":                true,
	"tp3_hit":                true,
	"max_r_before_exit":      true,
	"min_r_before_exit":      true,
	"normalized_exit_reason": true,
	"raw_exit_reason":        true,
	"hold_seconds":           true,
	"post_exit_peak_r":       true,
	"stop_then_reclaim":      true,
	"reentry_would_win":      true,
}

type Row struct {
	TradeID     string
	EntryTs     time.Time
	Numeric     map[string]float64
	Categorical map[string]string
	Target      float64
}

type Dataset struct {
	Rows                []Row
	NumericFeatures     []string
	CategoricalFeatures []string
	TargetName          string
}

func LoadCSV(path, target string, numericFeatures, categoricalFeatures []string) (Dataset, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dataset{}, 0, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return Dataset{}, 0, err
	}
	if len(records) < 2 {
		return Dataset{}, 0, fmt.Errorf("csv has no data rows")
	}
	header := make(map[string]int, len(records[0]))
	for i, col := range records[0] {
		header[strings.TrimSpace(col)] = i
	}
	for _, name := range numericFeatures {
		if ForbiddenInputColumns[name] {
			return Dataset{}, 0, fmt.Errorf("feature %q is forbidden due to leakage risk", name)
		}
		if _, ok := header[name]; !ok {
			return Dataset{}, 0, fmt.Errorf("missing numeric feature %q", name)
		}
	}
	for _, name := range categoricalFeatures {
		if ForbiddenInputColumns[name] {
			return Dataset{}, 0, fmt.Errorf("feature %q is forbidden due to leakage risk", name)
		}
		if _, ok := header[name]; !ok {
			return Dataset{}, 0, fmt.Errorf("missing categorical feature %q", name)
		}
	}
	targetCol, ok := header[target]
	if !ok {
		return Dataset{}, 0, fmt.Errorf("missing target column %q", target)
	}
	entryTsCol, ok := header["entry_ts"]
	if !ok {
		return Dataset{}, 0, fmt.Errorf("missing required time split column %q", "entry_ts")
	}
	tradeIDCol := -1
	if idx, ok := header["trade_id"]; ok {
		tradeIDCol = idx
	}
	rows := make([]Row, 0, len(records)-1)
	rawRows := 0
	for _, record := range records[1:] {
		rawRows++
		entryTs, err := time.Parse(time.RFC3339, strings.TrimSpace(record[entryTsCol]))
		if err != nil {
			continue
		}
		targetValue, ok := parseTarget(strings.TrimSpace(record[targetCol]), target)
		if !ok {
			continue
		}
		numeric := make(map[string]float64, len(numericFeatures))
		valid := true
		for _, name := range numericFeatures {
			value, err := parseFloat(record[header[name]])
			if err != nil {
				valid = false
				break
			}
			numeric[name] = value
		}
		if !valid {
			continue
		}
		categorical := make(map[string]string, len(categoricalFeatures))
		for _, name := range categoricalFeatures {
			categorical[name] = cleanCategory(record[header[name]])
		}
		tradeID := ""
		if tradeIDCol >= 0 {
			tradeID = strings.TrimSpace(record[tradeIDCol])
		}
		rows = append(rows, Row{
			TradeID:     tradeID,
			EntryTs:     entryTs.UTC(),
			Numeric:     numeric,
			Categorical: categorical,
			Target:      targetValue,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].EntryTs.Before(rows[j].EntryTs) })
	return Dataset{
		Rows:                rows,
		NumericFeatures:     append([]string(nil), numericFeatures...),
		CategoricalFeatures: append([]string(nil), categoricalFeatures...),
		TargetName:          target,
	}, rawRows, nil
}

func parseTarget(raw, target string) (float64, bool) {
	switch target {
	case "win", "stop_then_reclaim":
		if raw == "" {
			return 0, false
		}
		if raw == "1" || strings.EqualFold(raw, "true") {
			return 1, true
		}
		if raw == "0" || strings.EqualFold(raw, "false") {
			return 0, true
		}
		v, err := strconv.ParseFloat(raw, 64)
		return v, err == nil
	default:
		v, err := strconv.ParseFloat(raw, 64)
		return v, err == nil
	}
}

func parseFloat(raw string) (float64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return strconv.ParseFloat(strings.TrimSpace(raw), 64)
}

func cleanCategory(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "unknown"
	}
	return v
}
