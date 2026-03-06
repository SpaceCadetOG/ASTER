package throttle

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Dedupe struct {
	mu     sync.Mutex
	window time.Duration
	last   map[string]time.Time
}

func NewDedupe(window time.Duration) *Dedupe {
	return &Dedupe{window: window, last: map[string]time.Time{}}
}

func (d *Dedupe) Allow(symbol, side string, now time.Time) bool {
	if d == nil || d.window <= 0 {
		return true
	}
	key := fmt.Sprintf("%s|%s", strings.ToUpper(strings.TrimSpace(symbol)), strings.ToUpper(strings.TrimSpace(side)))
	d.mu.Lock()
	defer d.mu.Unlock()
	last := d.last[key]
	if !last.IsZero() && now.Sub(last) < d.window {
		return false
	}
	d.last[key] = now
	return true
}
