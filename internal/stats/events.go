package stats

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

type Event struct {
	Timestamp    time.Time `json:"timestamp"`
	Type         string    `json:"type"`
	Simulated    bool      `json:"simulated,omitempty"`
	Symbol       string    `json:"symbol,omitempty"`
	Side         string    `json:"side,omitempty"`
	TF           string    `json:"tf,omitempty"`
	Strategy     string    `json:"strategy,omitempty"`
	TriggerState string    `json:"trigger_state,omitempty"`
	ExitProfile  string    `json:"exit_profile,omitempty"`
	Score        float64   `json:"score,omitempty"`
	Slope        float64   `json:"slope,omitempty"`
	VolumeRatio  float64   `json:"volume_ratio,omitempty"`
	EntryPx      float64   `json:"entry_px,omitempty"`
	ExitPx       float64   `json:"exit_px,omitempty"`
	MarkPx       float64   `json:"mark_px,omitempty"`
	LastPx       float64   `json:"last_px,omitempty"`
	TriggerRef   string    `json:"trigger_ref,omitempty"`
	RiskR        float64   `json:"risk_r,omitempty"`
	HoldMin      float64   `json:"hold_min,omitempty"`
	MFER         float64   `json:"mfe_r,omitempty"`
	MAER         float64   `json:"mae_r,omitempty"`
	PnLUSD       float64   `json:"pnl_usd,omitempty"`
	PnLPct       float64   `json:"pnl_pct,omitempty"`
	Fees         float64   `json:"fees,omitempty"`
	Slippage     float64   `json:"slippage,omitempty"`
	Discovery    float64   `json:"discovery_score,omitempty"`
	Trigger      float64   `json:"trigger_score,omitempty"`
	Execution    float64   `json:"execution_score,omitempty"`
	Combined     float64   `json:"combined_score,omitempty"`
	StopDistPct  float64   `json:"stop_distance_pct,omitempty"`
	MissCategory string    `json:"miss_category,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	GateAllow    *bool     `json:"gate_allow,omitempty"`
	GateReasons  []string  `json:"gate_reasons,omitempty"`
}

func LoadEvents(path string, from, to *time.Time) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make([]Event, 0, 1024)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if from != nil && e.Timestamp.Before(*from) {
			continue
		}
		if to != nil && e.Timestamp.After(*to) {
			continue
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
