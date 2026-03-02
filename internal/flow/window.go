package flow

import (
	"sync"
	"time"
)

type Event struct {
	Ts    time.Time
	USD   float64
	IsBuy bool
}

type Stats struct {
	Count      int
	TotalUSD   float64
	BuyUSD     float64
	SellUSD    float64
	DeltaUSD   float64
	LargeCount int
	BuyPct     float64
	SellPct    float64
}

type Window struct {
	mu             sync.Mutex
	window         time.Duration
	largeThreshold float64
	evs            []Event
	head           int
	count          int
	totalUSD       float64
	buyUSD         float64
	sellUSD        float64
	largeCount     int
}

func NewWindow(window time.Duration, largeThreshold float64) *Window {
	if window <= 0 {
		window = 30 * time.Second
	}
	if largeThreshold < 0 {
		largeThreshold = 0
	}
	return &Window{
		window:         window,
		largeThreshold: largeThreshold,
		evs:            make([]Event, 0, 256),
	}
}

func (w *Window) Add(e Event) {
	if e.Ts.IsZero() {
		e.Ts = time.Now()
	}
	if e.USD < 0 {
		e.USD = 0
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.evs = append(w.evs, e)
	w.count++
	w.totalUSD += e.USD
	if e.IsBuy {
		w.buyUSD += e.USD
	} else {
		w.sellUSD += e.USD
	}
	if e.USD >= w.largeThreshold && w.largeThreshold > 0 {
		w.largeCount++
	}
	w.expireLocked(e.Ts)
}

func (w *Window) Snapshot() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.expireLocked(time.Now())
	return w.statsLocked()
}

// SnapshotAt returns stats using a caller-provided clock (useful for replay/backtest).
func (w *Window) SnapshotAt(now time.Time) Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.expireLocked(now)
	return w.statsLocked()
}

func (w *Window) Events() []Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.expireLocked(time.Now())
	if w.head >= len(w.evs) {
		return nil
	}
	out := make([]Event, len(w.evs)-w.head)
	copy(out, w.evs[w.head:])
	return out
}

func (w *Window) statsLocked() Stats {
	s := Stats{
		Count:      w.count,
		TotalUSD:   w.totalUSD,
		BuyUSD:     w.buyUSD,
		SellUSD:    w.sellUSD,
		DeltaUSD:   w.buyUSD - w.sellUSD,
		LargeCount: w.largeCount,
	}
	if s.TotalUSD > 0 {
		s.BuyPct = 100 * s.BuyUSD / s.TotalUSD
		s.SellPct = 100 * s.SellUSD / s.TotalUSD
	}
	return s
}

func (w *Window) expireLocked(now time.Time) {
	cutoff := now.Add(-w.window)
	for w.head < len(w.evs) {
		e := w.evs[w.head]
		if !e.Ts.Before(cutoff) {
			break
		}
		w.count--
		w.totalUSD -= e.USD
		if e.IsBuy {
			w.buyUSD -= e.USD
		} else {
			w.sellUSD -= e.USD
		}
		if e.USD >= w.largeThreshold && w.largeThreshold > 0 {
			w.largeCount--
		}
		w.head++
	}

	if w.count < 0 {
		w.count = 0
	}
	if w.totalUSD < 0 {
		w.totalUSD = 0
	}
	if w.buyUSD < 0 {
		w.buyUSD = 0
	}
	if w.sellUSD < 0 {
		w.sellUSD = 0
	}
	if w.largeCount < 0 {
		w.largeCount = 0
	}

	// Compact storage when expired prefix is large.
	if w.head > 1024 && w.head*2 >= len(w.evs) {
		w.evs = append([]Event(nil), w.evs[w.head:]...)
		w.head = 0
	}
}
