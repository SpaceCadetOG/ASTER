package main

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/inplay"
)

type liveScannerSnapshot struct {
	At          time.Time
	LongInPlay  []inplay.Entry
	ShortInPlay []inplay.Entry
	MetaBySym   map[string]symbolMeta
}

type liveWatcherSnapshot struct {
	At              time.Time
	LongInPlay      []inplay.Entry
	ShortInPlay     []inplay.Entry
	MetaBySym       map[string]symbolMeta
	FlowBySym       map[string]flowMetrics
	WatchSymbols    []string
	WallSignalsBySy map[string]wallSignal
}

type liveAccountHealthSnapshot struct {
	At      time.Time
	Summary accountHealthSummary
}

type liveRuntimeLoop struct {
	scannerSnap atomic.Pointer[liveScannerSnapshot]
	watcherSnap atomic.Pointer[liveWatcherSnapshot]
	healthSnap  atomic.Pointer[liveAccountHealthSnapshot]
}

func newLiveRuntimeLoop() *liveRuntimeLoop {
	return &liveRuntimeLoop{}
}

func (l *liveRuntimeLoop) publishScanner(s liveScannerSnapshot) {
	cp := &liveScannerSnapshot{
		At:          s.At,
		LongInPlay:  append([]inplay.Entry(nil), s.LongInPlay...),
		ShortInPlay: append([]inplay.Entry(nil), s.ShortInPlay...),
		MetaBySym:   copySymbolMetaMap(s.MetaBySym),
	}
	l.scannerSnap.Store(cp)
}

func (l *liveRuntimeLoop) latestScanner() (liveScannerSnapshot, bool) {
	ptr := l.scannerSnap.Load()
	if ptr == nil {
		return liveScannerSnapshot{}, false
	}
	return liveScannerSnapshot{
		At:          ptr.At,
		LongInPlay:  append([]inplay.Entry(nil), ptr.LongInPlay...),
		ShortInPlay: append([]inplay.Entry(nil), ptr.ShortInPlay...),
		MetaBySym:   copySymbolMetaMap(ptr.MetaBySym),
	}, true
}

func (l *liveRuntimeLoop) publishWatcher(s liveWatcherSnapshot) {
	cp := &liveWatcherSnapshot{
		At:              s.At,
		LongInPlay:      append([]inplay.Entry(nil), s.LongInPlay...),
		ShortInPlay:     append([]inplay.Entry(nil), s.ShortInPlay...),
		MetaBySym:       copySymbolMetaMap(s.MetaBySym),
		FlowBySym:       copyFlowMetricsMap(s.FlowBySym),
		WatchSymbols:    append([]string(nil), s.WatchSymbols...),
		WallSignalsBySy: copyWallSignalsMap(s.WallSignalsBySy),
	}
	l.watcherSnap.Store(cp)
}

func (l *liveRuntimeLoop) latestWatcher() (liveWatcherSnapshot, bool) {
	ptr := l.watcherSnap.Load()
	if ptr == nil {
		return liveWatcherSnapshot{}, false
	}
	return liveWatcherSnapshot{
		At:              ptr.At,
		LongInPlay:      append([]inplay.Entry(nil), ptr.LongInPlay...),
		ShortInPlay:     append([]inplay.Entry(nil), ptr.ShortInPlay...),
		MetaBySym:       copySymbolMetaMap(ptr.MetaBySym),
		FlowBySym:       copyFlowMetricsMap(ptr.FlowBySym),
		WatchSymbols:    append([]string(nil), ptr.WatchSymbols...),
		WallSignalsBySy: copyWallSignalsMap(ptr.WallSignalsBySy),
	}, true
}

func (l *liveRuntimeLoop) publishHealth(s liveAccountHealthSnapshot) {
	cp := &liveAccountHealthSnapshot{At: s.At, Summary: s.Summary}
	l.healthSnap.Store(cp)
}

func (l *liveRuntimeLoop) latestHealth() (liveAccountHealthSnapshot, bool) {
	ptr := l.healthSnap.Load()
	if ptr == nil {
		return liveAccountHealthSnapshot{}, false
	}
	return *ptr, true
}

func (l *liveRuntimeLoop) startScannerWorker(ctx context.Context, every time.Duration, fn func(time.Time) (liveScannerSnapshot, bool)) {
	if every <= 0 {
		every = 5 * time.Second
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		if s, ok := fn(time.Now().UTC()); ok {
			l.publishScanner(s)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case ts := <-t.C:
				if s, ok := fn(ts.UTC()); ok {
					l.publishScanner(s)
				}
			}
		}
	}()
}

func (l *liveRuntimeLoop) startWatcherWorker(ctx context.Context, every time.Duration, fn func(time.Time, liveScannerSnapshot) (liveWatcherSnapshot, bool)) {
	if every <= 0 {
		every = time.Second
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case ts := <-t.C:
				scan, ok := l.latestScanner()
				if !ok {
					continue
				}
				if s, ok := fn(ts.UTC(), scan); ok {
					l.publishWatcher(s)
				}
			}
		}
	}()
}

func (l *liveRuntimeLoop) startAccountHealthWorker(ctx context.Context, every time.Duration, fn func(time.Time) accountHealthSummary) {
	if every <= 0 {
		every = 5 * time.Second
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		l.publishHealth(liveAccountHealthSnapshot{At: time.Now().UTC(), Summary: fn(time.Now().UTC())})
		for {
			select {
			case <-ctx.Done():
				return
			case ts := <-t.C:
				l.publishHealth(liveAccountHealthSnapshot{At: ts.UTC(), Summary: fn(ts.UTC())})
			}
		}
	}()
}

func copyFlowMetricsMap(in map[string]flowMetrics) map[string]flowMetrics {
	if len(in) == 0 {
		return map[string]flowMetrics{}
	}
	out := make(map[string]flowMetrics, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyWallSignalsMap(in map[string]wallSignal) map[string]wallSignal {
	if len(in) == 0 {
		return map[string]wallSignal{}
	}
	out := make(map[string]wallSignal, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func topNSymbolsFromEntries(entries []inplay.Entry, n int) []string {
	if n <= 0 || len(entries) == 0 {
		return nil
	}
	out := make([]string, 0, n)
	seen := map[string]struct{}{}
	for i := 0; i < len(entries) && len(out) < n; i++ {
		raw := strings.ToUpper(strings.TrimSpace(aster.RawSymbol(entries[i].Symbol)))
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}
