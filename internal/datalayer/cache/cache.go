package cache

import (
	"sync"
	"time"
)

type Snapshot[T any] struct {
	Value   T
	Source  string
	Updated time.Time
	Err     error
}

type Latest[T any] struct {
	mu   sync.RWMutex
	snap Snapshot[T]
}

func (l *Latest[T]) Set(value T, source string, updated time.Time, err error) {
	l.mu.Lock()
	l.snap = Snapshot[T]{
		Value:   value,
		Source:  source,
		Updated: updated,
		Err:     err,
	}
	l.mu.Unlock()
}

func (l *Latest[T]) Get() Snapshot[T] {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.snap
}

type TTLMap[T any] struct {
	mu   sync.RWMutex
	data map[string]ttlEntry[T]
}

type ttlEntry[T any] struct {
	value     T
	expiresAt time.Time
	source    string
	updated   time.Time
}

type TTLSnapshot[T any] struct {
	Value   T
	Source  string
	Updated time.Time
	Found   bool
}

func NewTTLMap[T any]() *TTLMap[T] {
	return &TTLMap[T]{data: map[string]ttlEntry[T]{}}
}

func (m *TTLMap[T]) Set(key string, value T, ttl time.Duration, source string, updated time.Time) {
	if ttl <= 0 {
		ttl = time.Second
	}
	m.mu.Lock()
	m.data[key] = ttlEntry[T]{
		value:     value,
		expiresAt: time.Now().UTC().Add(ttl),
		source:    source,
		updated:   updated,
	}
	m.mu.Unlock()
}

func (m *TTLMap[T]) Get(key string) TTLSnapshot[T] {
	m.mu.RLock()
	entry, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return TTLSnapshot[T]{}
	}
	if time.Now().UTC().After(entry.expiresAt) {
		var zero T
		m.mu.Lock()
		delete(m.data, key)
		m.mu.Unlock()
		return TTLSnapshot[T]{Value: zero}
	}
	return TTLSnapshot[T]{
		Value:   entry.value,
		Source:  entry.source,
		Updated: entry.updated,
		Found:   true,
	}
}

type Ring[T any] struct {
	mu    sync.RWMutex
	items []T
	limit int
}

func NewRing[T any](limit int) *Ring[T] {
	if limit <= 0 {
		limit = 50
	}
	return &Ring[T]{limit: limit}
}

func (r *Ring[T]) Add(v T) {
	r.mu.Lock()
	r.items = append(r.items, v)
	if len(r.items) > r.limit {
		r.items = append([]T(nil), r.items[len(r.items)-r.limit:]...)
	}
	r.mu.Unlock()
}

func (r *Ring[T]) Items() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]T(nil), r.items...)
}
