package notify

import (
	"crypto/sha1"
	"fmt"
	"sync"
	"time"
)

type dedupeEntry struct {
	LastSent  time.Time
	LastHash  string
	LastState string
}

type DedupeStore struct {
	mu    sync.Mutex
	items map[string]dedupeEntry
}

func NewDedupeStore() *DedupeStore {
	return &DedupeStore{items: make(map[string]dedupeEntry)}
}

func (d *DedupeStore) ShouldSend(event Event, rendered string, policy Policy) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := dedupeKey(event)
	hash := renderHash(rendered)
	now := event.OccurredAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	prev, ok := d.items[key]
	if !ok {
		d.items[key] = dedupeEntry{
			LastSent:  now,
			LastHash:  hash,
			LastState: event.Metadata["state"],
		}
		return true
	}
	if policy.StateTransitionOnly {
		newState := event.Metadata["state"]
		if newState != "" && newState == prev.LastState {
			return false
		}
	}
	window := time.Duration(policy.DedupeWindowSec) * time.Second
	if window > 0 && now.Sub(prev.LastSent) < window && prev.LastHash == hash {
		return false
	}
	d.items[key] = dedupeEntry{
		LastSent:  now,
		LastHash:  hash,
		LastState: event.Metadata["state"],
	}
	return true
}

func dedupeKey(event Event) string {
	return fmt.Sprintf("%s|%s|%s", event.Key, event.Symbol, event.PositionID)
}

func renderHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", sum[:])
}

