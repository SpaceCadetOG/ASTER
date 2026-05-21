package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type EventLogger struct {
	mu        sync.Mutex
	path      string
	toStdout  bool
	enabled   bool
	simulated bool
	recent    []Event
}

func NewEventLogger(path string, enabled, toStdout, simulated bool) *EventLogger {
	return &EventLogger{path: path, enabled: enabled, toStdout: toStdout, simulated: simulated}
}

func (l *EventLogger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *EventLogger) Emit(e Event) {
	if l == nil || !l.enabled {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if !e.Simulated {
		e.Simulated = l.simulated
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	if l.toStdout {
		fmt.Println(string(b))
	}
	if l.path == "" {
		l.mu.Lock()
		l.appendRecentLocked(e)
		l.mu.Unlock()
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.appendRecentLocked(e)
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func (l *EventLogger) Recent(limit int) []Event {
	if l == nil || limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.recent) == 0 {
		return nil
	}
	if limit > len(l.recent) {
		limit = len(l.recent)
	}
	start := len(l.recent) - limit
	out := make([]Event, limit)
	copy(out, l.recent[start:])
	return out
}

func (l *EventLogger) appendRecentLocked(e Event) {
	const maxRecentEvents = 256
	l.recent = append(l.recent, e)
	if len(l.recent) > maxRecentEvents {
		copy(l.recent, l.recent[len(l.recent)-maxRecentEvents:])
		l.recent = l.recent[:maxRecentEvents]
	}
}
