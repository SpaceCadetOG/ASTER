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
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
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
