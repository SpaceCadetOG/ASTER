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
	var b strings.Builder
	fmt.Fprintf(&b, "Hourly Digest (%s)\n", now.Format("15:04"))
	fmt.Fprintf(&b, "Mode: %s\nStatus: %s\n\n", snap.Mode, snap.Status)
	fmt.Fprintf(&b, "PnL\n- Realized today: %.2f\n- Unrealized: %.2f\n- Fees/Funding today: %.2f\n\n",
		snap.RealizedToday, snap.UnrealizedNow, snap.FeesFundingToday)
	fmt.Fprintf(&b, "Activity (last hour)\n- Entries: %d\n- Exits: %d\n- Adds: %d\n\n",
		a.hourly.Entries, a.hourly.Exits, a.hourly.Adds)
	fmt.Fprintf(&b, "Health\n- Order errors: %d\n- Unknown executions: %d\n- Reconcile failures: %d\n- Protection failures: %d\n\n",
		a.hourly.OrderErrors, a.hourly.UnknownExecs, a.hourly.ReconcileFailures, a.hourly.ProtectionFailures)
	fmt.Fprintf(&b, "Blocked / Missed\n")
	for _, line := range topRejectLines(a.hourly.RejectReasons, 4) {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	if len(a.hourly.RejectReasons) == 0 {
		fmt.Fprintf(&b, "- none\n")
	}
	fmt.Fprintf(&b, "\nOpen Positions\n")
	if len(snap.OpenPositionLines) == 0 {
		fmt.Fprintf(&b, "- none\n")
	} else {
		for _, line := range snap.OpenPositionLines {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	fmt.Fprintf(&b, "\nWatchlist\n")
	if len(snap.WatchlistLines) == 0 {
		fmt.Fprintf(&b, "- none\n")
	} else {
		for _, line := range snap.WatchlistLines {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	a.hourly = WindowStats{RejectReasons: map[string]int{}}
	return b.String()
}

func (a *Accumulator) RenderOvernightReport(now time.Time, snap Snapshot) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "0700 Overnight (%s)\n", now.Format("2006-01-02"))
	fmt.Fprintf(&b, "Mode: %s\nStatus: %s\n\n", snap.Mode, snap.Status)
	fmt.Fprintf(&b, "Overnight Summary\n- Realized: %.2f\n- Unrealized: %.2f\n\n", snap.RealizedToday, snap.UnrealizedNow)
	fmt.Fprintf(&b, "Incidents\n- Order errors: %d\n- Unknown executions: %d\n- Reconcile failures: %d\n- Protection failures: %d\n\n",
		a.daily.OrderErrors, a.daily.UnknownExecs, a.daily.ReconcileFailures, a.daily.ProtectionFailures)
	fmt.Fprintf(&b, "Open Risk\n")
	if len(snap.OpenPositionLines) == 0 {
		fmt.Fprintf(&b, "- none\n\n")
	} else {
		for _, line := range snap.OpenPositionLines {
			fmt.Fprintf(&b, "- %s\n", line)
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "Action Needed\n")
	if a.daily.OrderErrors+a.daily.UnknownExecs+a.daily.ReconcileFailures+a.daily.ProtectionFailures == 0 {
		fmt.Fprintf(&b, "- none\n")
	} else {
		fmt.Fprintf(&b, "- review unresolved incidents\n")
	}
	return b.String()
}

func (a *Accumulator) RenderDailyReport(now time.Time, snap Snapshot) string {
	if a == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "1900 Daily (%s)\n", now.Format("2006-01-02"))
	fmt.Fprintf(&b, "Mode: %s\nStatus: %s\n\n", snap.Mode, snap.Status)
	fmt.Fprintf(&b, "Daily Performance\n- Realized: %.2f\n- Unrealized carry: %.2f\n- Fees/Funding: %.2f\n\n",
		snap.RealizedToday, snap.UnrealizedNow, snap.FeesFundingToday)
	fmt.Fprintf(&b, "Activity\n- Entries: %d\n- Exits: %d\n- Adds: %d\n\n",
		a.daily.Entries, a.daily.Exits, a.daily.Adds)
	fmt.Fprintf(&b, "Operational Issues\n- Order errors: %d\n- Unknown executions: %d\n- Reconcile failures: %d\n- Protection failures: %d\n\n",
		a.daily.OrderErrors, a.daily.UnknownExecs, a.daily.ReconcileFailures, a.daily.ProtectionFailures)
	fmt.Fprintf(&b, "Top Reject Reasons\n")
	for _, line := range topRejectLines(a.daily.RejectReasons, 5) {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	if len(a.daily.RejectReasons) == 0 {
		fmt.Fprintf(&b, "- none\n")
	}
	fmt.Fprintf(&b, "\nCarry\n")
	if len(snap.OpenPositionLines) == 0 {
		fmt.Fprintf(&b, "- none\n")
	} else {
		for _, line := range snap.OpenPositionLines {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}
	a.daily = WindowStats{RejectReasons: map[string]int{}}
	return b.String()
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

