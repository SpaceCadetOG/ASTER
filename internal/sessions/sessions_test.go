package sessions

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return ts
}

func TestActiveSessionLabels(t *testing.T) {
	ts := mustParse(t, "2026-03-04T08:20:00Z") // 02:20 CT
	labels := ActiveSessionLabels(ts)
	if len(labels) == 0 {
		t.Fatal("expected labels")
	}
	foundOverlap := false
	for _, l := range labels {
		if l == "ASIA_EU_OVERLAP" {
			foundOverlap = true
			break
		}
	}
	if !foundOverlap {
		t.Fatalf("expected ASIA_EU_OVERLAP in labels: %v", labels)
	}
}

func TestScannerScoreMultiplier(t *testing.T) {
	ts := mustParse(t, "2026-03-04T08:20:00Z")
	m := ScannerScoreMultiplier(ts)
	if m < 0.99 || m > 1.01 {
		t.Fatalf("expected neutral multiplier near 1.0 got %.4f", m)
	}
	ts2 := mustParse(t, "2026-03-04T23:30:00Z") // 17:30 CT dead
	m2 := ScannerScoreMultiplier(ts2)
	if m2 < 0.99 || m2 > 1.01 {
		t.Fatalf("expected neutral multiplier near 1.0 got %.4f", m2)
	}
}
