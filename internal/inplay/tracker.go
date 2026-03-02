package inplay

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"go-machine/internal/market"
)

type State string

const (
	StateWarming State = "warming"
	StateInPlay  State = "in-play"
	StateCooling State = "cooling"
)

type Entry struct {
	Symbol       string    `json:"symbol"`
	SideBias     string    `json:"sideBias"`
	CurrentGrade string    `json:"currentGrade"`
	CurrentScore float64   `json:"currentScore"`
	ScoreSlope   float64   `json:"scoreSlope"`
	FirstSeen    time.Time `json:"firstSeen"`
	LastSeen     time.Time `json:"lastSeen"`
	State        State     `json:"state"`
	Momentum     bool      `json:"momentum"`
	Rank         float64   `json:"rank"`
}

type Config struct {
	MinGrade       string
	MinVolumeUSD   float64
	HistoryN       int
	RiseN          int
	DropGradeScans int
	FallScans      int
	TTL            time.Duration
}

type scorePoint struct {
	ts    time.Time
	score float64
	grade string
	vol   float64
}

type symbolState struct {
	firstSeen  time.Time
	lastSeen   time.Time
	history    []scorePoint
	dStreak    int
	fallStreak int
	state      State
}

type Tracker struct {
	mu   sync.RWMutex
	side string
	cfg  Config
	data map[string]*symbolState
}

func NewTracker(side string, cfg Config) *Tracker {
	if cfg.MinGrade == "" {
		cfg.MinGrade = "C"
	}
	if cfg.HistoryN <= 0 {
		cfg.HistoryN = 5
	}
	if cfg.RiseN <= 0 {
		cfg.RiseN = 3
	}
	if cfg.DropGradeScans <= 0 {
		cfg.DropGradeScans = 2
	}
	if cfg.FallScans <= 0 {
		cfg.FallScans = 2
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Minute
	}
	return &Tracker{
		side: strings.ToLower(strings.TrimSpace(side)),
		cfg:  cfg,
		data: map[string]*symbolState{},
	}
}

func (t *Tracker) Update(now time.Time, rows []market.Scored, grades map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	seen := make(map[string]struct{}, len(rows))
	minGradeVal := gradeValue(t.cfg.MinGrade)

	for _, r := range rows {
		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" {
			continue
		}
		seen[sym] = struct{}{}
		g := strings.TrimSpace(grades[sym])
		if g == "" {
			g = market.FallbackGradeDirectional(r.Score, r.Change24h, t.side)
		}

		ss := t.data[sym]
		if ss == nil {
			ss = &symbolState{firstSeen: now, state: StateWarming}
			t.data[sym] = ss
		}
		ss.lastSeen = now
		ss.history = append(ss.history, scorePoint{
			ts:    now,
			score: r.Score,
			grade: g,
			vol:   r.VolumeUSD,
		})
		if len(ss.history) > t.cfg.HistoryN {
			ss.history = append([]scorePoint(nil), ss.history[len(ss.history)-t.cfg.HistoryN:]...)
		}

		if gradeValue(g) <= gradeValue("D") {
			ss.dStreak++
		} else {
			ss.dStreak = 0
		}

		slope := calcSlope(ss.history)
		if slope < 0 {
			ss.fallStreak++
		} else {
			ss.fallStreak = 0
		}

		rising := isRising(ss.history, t.cfg.RiseN)
		gradeOK := gradeValue(g) >= minGradeVal
		volOK := r.VolumeUSD >= t.cfg.MinVolumeUSD

		switch {
		case rising && gradeOK && volOK:
			ss.state = StateInPlay
		case rising && volOK:
			ss.state = StateWarming
		default:
			ss.state = StateCooling
		}

		if ss.dStreak >= t.cfg.DropGradeScans || ss.fallStreak >= t.cfg.FallScans {
			delete(t.data, sym)
		}
	}

	// Expire inactive symbols.
	for sym, ss := range t.data {
		if now.Sub(ss.lastSeen) > t.cfg.TTL {
			delete(t.data, sym)
		}
	}
}

func (t *Tracker) Entries() []Entry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]Entry, 0, len(t.data))
	for sym, ss := range t.data {
		if len(ss.history) == 0 {
			continue
		}
		last := ss.history[len(ss.history)-1]
		slope := calcSlope(ss.history)
		e := Entry{
			Symbol:       sym,
			SideBias:     t.side,
			CurrentGrade: last.grade,
			CurrentScore: last.score,
			ScoreSlope:   slope,
			FirstSeen:    ss.firstSeen,
			LastSeen:     ss.lastSeen,
			State:        ss.state,
			Momentum:     slope > 0 && isRising(ss.history, t.cfg.RiseN),
		}
		e.Rank = rankFor(e)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rank > out[j].Rank })
	return out
}

func rankFor(e Entry) float64 {
	stateBoost := 0.0
	switch e.State {
	case StateInPlay:
		stateBoost = 25
	case StateWarming:
		stateBoost = 10
	}
	momentum := 0.0
	if e.Momentum {
		momentum = 5
	}
	return 20*float64(gradeValue(e.CurrentGrade)) + 0.4*e.CurrentScore + 8*e.ScoreSlope + stateBoost + momentum
}

func calcSlope(points []scorePoint) float64 {
	n := len(points)
	if n < 2 {
		return 0
	}
	first := points[0].score
	last := points[n-1].score
	return (last - first) / math.Max(1, float64(n-1))
}

func isRising(points []scorePoint, riseN int) bool {
	if len(points) < 2 {
		return false
	}
	if riseN <= 1 {
		riseN = 2
	}
	if len(points) < riseN {
		riseN = len(points)
	}
	start := len(points) - riseN
	rises := 0
	for i := start + 1; i < len(points); i++ {
		if points[i].score > points[i-1].score {
			rises++
		}
	}
	return rises >= riseN-1
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
