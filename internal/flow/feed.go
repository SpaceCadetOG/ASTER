package flow

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

type ExternalSignal struct {
	Symbol     string    `json:"symbol"`
	FlowDelta  float64   `json:"flow_delta"`
	LiqSpike   bool      `json:"liq_spike"`
	WhaleSpike bool      `json:"whale_spike"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type FileFeed struct {
	path string
	ttl  time.Duration
}

func NewFileFeed(path string, ttl time.Duration) *FileFeed {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &FileFeed{path: strings.TrimSpace(path), ttl: ttl}
}

func (f *FileFeed) Snapshot(now time.Time) map[string]ExternalSignal {
	out := map[string]ExternalSignal{}
	if f == nil || f.path == "" {
		return out
	}
	b, err := os.ReadFile(f.path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return out
	}
	var arr []ExternalSignal
	if err := json.Unmarshal(b, &arr); err != nil {
		var wrap struct {
			Signals []ExternalSignal `json:"signals"`
		}
		if err2 := json.Unmarshal(b, &wrap); err2 != nil {
			return out
		}
		arr = wrap.Signals
	}
	for _, s := range arr {
		raw := strings.ToUpper(strings.TrimSpace(s.Symbol))
		if raw == "" {
			continue
		}
		if s.UpdatedAt.IsZero() || now.Sub(s.UpdatedAt) <= f.ttl {
			s.Symbol = raw
			out[raw] = s
		}
	}
	return out
}
