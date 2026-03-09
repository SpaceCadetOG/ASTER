package reliability

import (
	"strings"
	"sync"
	"time"
)

// Store exposes a lightweight reliability memory interface for ranking hooks.
// The default in-memory implementation is intentionally small and optional.
type Store interface {
	Adjustment(symbol string) float64
	Record(out Outcome)
	Snapshot(symbol string) Snapshot
}

type Config struct {
	Enabled    bool
	MaxPenalty float64
	MaxBonus   float64
}

type Outcome struct {
	Symbol      string
	Timestamp   time.Time
	Win         bool
	StopOut     bool
	SlippageBps float64
	ExpectancyR float64
}

type Snapshot struct {
	Symbol         string
	N              int
	WinRate        float64
	StopoutDensity float64
	AvgSlippageBps float64
	ExpectancyR    float64
	LastUpdated    time.Time
}

type symbolMem struct {
	snap Snapshot
}

type InMemoryStore struct {
	cfg  Config
	mu   sync.RWMutex
	data map[string]*symbolMem
}

func NewInMemoryStore(cfg Config) *InMemoryStore {
	if cfg.MaxPenalty <= 0 {
		cfg.MaxPenalty = 8
	}
	if cfg.MaxBonus <= 0 {
		cfg.MaxBonus = 4
	}
	return &InMemoryStore{
		cfg:  cfg,
		data: make(map[string]*symbolMem, 256),
	}
}

func (s *InMemoryStore) Adjustment(symbol string) float64 {
	if s == nil || !s.cfg.Enabled {
		return 0
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return 0
	}
	s.mu.RLock()
	mem := s.data[sym]
	s.mu.RUnlock()
	if mem == nil || mem.snap.N < 5 {
		return 0
	}
	winAdj := (mem.snap.WinRate - 0.50) * s.cfg.MaxBonus
	stopAdj := -mem.snap.StopoutDensity * s.cfg.MaxPenalty
	slipAdj := 0.0
	if mem.snap.AvgSlippageBps > 0 {
		slipAdj = -minf(s.cfg.MaxPenalty*0.40, mem.snap.AvgSlippageBps*0.15)
	}
	expAdj := mem.snap.ExpectancyR * 0.75
	return clamp(winAdj+stopAdj+slipAdj+expAdj, -s.cfg.MaxPenalty, s.cfg.MaxBonus)
}

func (s *InMemoryStore) Record(out Outcome) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	sym := strings.ToUpper(strings.TrimSpace(out.Symbol))
	if sym == "" {
		return
	}
	ts := out.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	s.mu.Lock()
	mem := s.data[sym]
	if mem == nil {
		mem = &symbolMem{snap: Snapshot{Symbol: sym}}
		s.data[sym] = mem
	}
	nPrev := mem.snap.N
	n := float64(nPrev + 1)
	win := 0.0
	if out.Win {
		win = 1
	}
	stop := 0.0
	if out.StopOut {
		stop = 1
	}
	mem.snap.WinRate = ((mem.snap.WinRate * float64(nPrev)) + win) / n
	mem.snap.StopoutDensity = ((mem.snap.StopoutDensity * float64(nPrev)) + stop) / n
	mem.snap.AvgSlippageBps = ((mem.snap.AvgSlippageBps * float64(nPrev)) + out.SlippageBps) / n
	mem.snap.ExpectancyR = ((mem.snap.ExpectancyR * float64(nPrev)) + out.ExpectancyR) / n
	mem.snap.N = nPrev + 1
	mem.snap.LastUpdated = ts
	s.mu.Unlock()
}

func (s *InMemoryStore) Snapshot(symbol string) Snapshot {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" || s == nil {
		return Snapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	mem := s.data[sym]
	if mem == nil {
		return Snapshot{Symbol: sym}
	}
	return mem.snap
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
