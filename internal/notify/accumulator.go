package notify

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Snapshot struct {
	Mode              string
	Status            string
	RealizedToday     float64
	UnrealizedNow     float64
	FeesFundingToday  float64
	OpenPositionLines []string
	WatchlistLines    []string
}

type WindowStats struct {
	Entries            int
	Exits              int
	Adds               int
	OrderErrors        int
	UnknownExecs       int
	ReconcileFailures  int
	ProtectionFailures int
	RejectReasons      map[string]int
}

type Accumulator struct {
	mu     sync.Mutex
	hourly WindowStats
	daily  WindowStats
}

func NewAccumulator() *Accumulator {
	return &Accumulator{
		hourly: WindowStats{RejectReasons: map[string]int{}},
		daily:  WindowStats{RejectReasons: map[string]int{}},
	}
}

func (a *Accumulator) Add(event Event) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	apply := func(w *WindowStats) {
		switch event.Key {
		case "ORDER_PLACED":
			w.Entries++
		case "POSITION_CLOSED":
			w.Exits++
		case "ADD_SUBMITTED":
			w.Adds++
		case "ORDER_ERROR":
			w.OrderErrors++
		case "EXECUTION_UNKNOWN":
			w.UnknownExecs++
		case "RECONCILE_FAILED":
			w.ReconcileFailures++
		case "PROTECTION_ATTACH_FAILED":
			w.ProtectionFailures++
		}
		if rr := strings.TrimSpace(event.Metadata["reject_reason"]); rr != "" {
			w.RejectReasons[rr]++
		}
	}
	apply(&a.hourly)
	apply(&a.daily)
}

func (a *Accumulator) RenderHourlyReport(now time.Time, snap Snapshot) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	lines := []string{
		fmt.Sprintf("<b>Mode:</b> %s | <b>Session:</b> %s", snap.Mode, snap.Status),
		fmt.Sprintf("<b>PnL:</b> day %+.2f | open %+.2f | fees %.2f", snap.RealizedToday, snap.UnrealizedNow, snap.FeesFundingToday),
		fmt.Sprintf("<b>Activity:</b> entries %d | exits %d | adds %d", a.hourly.Entries, a.hourly.Exits, a.hourly.Adds),
	}
	if a.hourly.OrderErrors+a.hourly.UnknownExecs+a.hourly.ReconcileFailures+a.hourly.ProtectionFailures > 0 {
		lines = append(lines, fmt.Sprintf("<b>Health:</b> err %d | unk %d | rec %d | prot %d",
			a.hourly.OrderErrors, a.hourly.UnknownExecs, a.hourly.ReconcileFailures, a.hourly.ProtectionFailures))
	}
	if rejects := topRejectLines(a.hourly.RejectReasons, 2); len(rejects) > 0 {
		lines = append(lines, "<b>Blocked:</b> "+strings.Join(rejects, " | "))
	}
	if len(snap.OpenPositionLines) > 0 {
		maxPos := len(snap.OpenPositionLines)
		if maxPos > 3 {
			maxPos = 3
		}
		lines = append(lines, "<b>Open:</b> "+strings.Join(snap.OpenPositionLines[:maxPos], " | "))
	}
	if len(snap.WatchlistLines) > 0 {
		maxWatch := len(snap.WatchlistLines)
		if maxWatch > 4 {
			maxWatch = 4
		}
		lines = append(lines, "<b>Watch:</b> "+strings.Join(snap.WatchlistLines[:maxWatch], " | "))
	}
	a.hourly = WindowStats{RejectReasons: map[string]int{}}
	return BuildEventHTML("🕐", "HOURLY DIGEST", lines...)
}

func (a *Accumulator) RenderOvernightReport(now time.Time, snap Snapshot) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	lines := []string{
		fmt.Sprintf("<b>Mode:</b> %s | <b>Session:</b> %s", snap.Mode, snap.Status),
		fmt.Sprintf("<b>Overnight:</b> realized %+.2f | open %+.2f", snap.RealizedToday, snap.UnrealizedNow),
	}
	incidents := a.daily.OrderErrors + a.daily.UnknownExecs + a.daily.ReconcileFailures + a.daily.ProtectionFailures
	if incidents > 0 {
		lines = append(lines, fmt.Sprintf("<b>Incidents:</b> %d (err %d | unk %d | rec %d | prot %d)",
			incidents, a.daily.OrderErrors, a.daily.UnknownExecs, a.daily.ReconcileFailures, a.daily.ProtectionFailures))
	}
	if len(snap.OpenPositionLines) > 0 {
		maxPos := len(snap.OpenPositionLines)
		if maxPos > 3 {
			maxPos = 3
		}
		lines = append(lines, "<b>Open:</b> "+strings.Join(snap.OpenPositionLines[:maxPos], " | "))
	}
	return BuildEventHTML("🌅", "OVERNIGHT", lines...)
}

func (a *Accumulator) RenderDailyReport(now time.Time, snap Snapshot) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	lines := []string{
		fmt.Sprintf("<b>Mode:</b> %s | <b>Session:</b> %s", snap.Mode, snap.Status),
		fmt.Sprintf("<b>Daily:</b> realized %+.2f | carry %+.2f | fees %.2f", snap.RealizedToday, snap.UnrealizedNow, snap.FeesFundingToday),
		fmt.Sprintf("<b>Activity:</b> entries %d | exits %d | adds %d", a.daily.Entries, a.daily.Exits, a.daily.Adds),
	}
	issues := a.daily.OrderErrors + a.daily.UnknownExecs + a.daily.ReconcileFailures + a.daily.ProtectionFailures
	if issues > 0 {
		lines = append(lines, fmt.Sprintf("<b>Issues:</b> err %d | unk %d | rec %d | prot %d",
			a.daily.OrderErrors, a.daily.UnknownExecs, a.daily.ReconcileFailures, a.daily.ProtectionFailures))
	}
	if rejects := topRejectLines(a.daily.RejectReasons, 3); len(rejects) > 0 {
		lines = append(lines, "<b>Top blocked:</b> "+strings.Join(rejects, " | "))
	}
	if len(snap.OpenPositionLines) > 0 {
		maxPos := len(snap.OpenPositionLines)
		if maxPos > 3 {
			maxPos = 3
		}
		lines = append(lines, "<b>Open:</b> "+strings.Join(snap.OpenPositionLines[:maxPos], " | "))
	}
	a.daily = WindowStats{RejectReasons: map[string]int{}}
	return BuildEventHTML("🧾", "DAILY", lines...)
}

func topRejectLines(m map[string]int, limit int) []string {
	type kv struct {
		K string
		V int
	}
	items := make([]kv, 0, len(m))
	for k, v := range m {
		items = append(items, kv{K: k, V: v})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].V > items[j].V })
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprintf("%s: %d", it.K, it.V))
	}
	return out
}
