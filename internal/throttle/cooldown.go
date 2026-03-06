package throttle

import (
	"strings"
	"sync"
	"time"
)

type Cooldown struct {
	mu       sync.Mutex
	window   time.Duration
	lastSeen map[string]time.Time
}

func NewCooldown(window time.Duration) *Cooldown {
	return &Cooldown{window: window, lastSeen: map[string]time.Time{}}
}

func (c *Cooldown) Allow(symbol string, now time.Time) bool {
	if c == nil || c.window <= 0 {
		return true
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	c.mu.Lock()
	defer c.mu.Unlock()
	last := c.lastSeen[sym]
	if !last.IsZero() && now.Sub(last) < c.window {
		return false
	}
	c.lastSeen[sym] = now
	return true
}
