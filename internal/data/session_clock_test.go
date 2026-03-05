package data

import (
	"testing"
	"time"
)

func mustParseRFC3339(t *testing.T, v string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, v)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return ts
}

func TestDayKeyNY17CTBoundaryAndDST(t *testing.T) {
	// Before DST switch (CST)
	ts := mustParseRFC3339(t, "2026-03-03T21:59:00Z") // 15:59 CT
	if got := DayKeyNY17CT(ts); got != "2026-03-02" {
		t.Fatalf("before 16:00 CT expected previous day, got %s", got)
	}
	ts = mustParseRFC3339(t, "2026-03-03T22:00:00Z") // 16:00 CT
	if got := DayKeyNY17CT(ts); got != "2026-03-03" {
		t.Fatalf("at 16:00 CT expected same day, got %s", got)
	}

	// After DST switch (CDT)
	ts = mustParseRFC3339(t, "2026-03-09T20:59:00Z") // 15:59 CT
	if got := DayKeyNY17CT(ts); got != "2026-03-08" {
		t.Fatalf("after DST before 16:00 CT expected previous day, got %s", got)
	}
	ts = mustParseRFC3339(t, "2026-03-09T21:00:00Z") // 16:00 CT
	if got := DayKeyNY17CT(ts); got != "2026-03-09" {
		t.Fatalf("after DST at 16:00 CT expected same day, got %s", got)
	}
}

func TestUSMorningKeyCT(t *testing.T) {
	ts := mustParseRFC3339(t, "2026-03-04T12:59:00Z") // 06:59 CT
	if got := USMorningKeyCT(ts); got != "2026-03-03" {
		t.Fatalf("before 07:00 expected previous day, got %s", got)
	}
	ts = mustParseRFC3339(t, "2026-03-04T13:00:00Z") // 07:00 CT
	if got := USMorningKeyCT(ts); got != "2026-03-04" {
		t.Fatalf("at 07:00 expected current day, got %s", got)
	}
}

func TestCurrentRegimeCT(t *testing.T) {
	cases := []struct {
		ts   string
		want Regime
	}{
		{"2026-03-04T08:30:00Z", OverlapAE},   // 02:30 CST
		{"2026-03-04T08:15:00Z", OverlapAE},   // 02:15 CST
		{"2026-03-04T14:30:00Z", OverlapEUUS}, // 08:30 CST
		{"2026-03-04T17:30:00Z", RegimeUS},    // 11:30 CST
		{"2026-03-04T23:30:00Z", RegimeDead},  // 17:30 CST
		{"2026-03-05T02:30:00Z", RegimeAsia},  // 20:30 CST
	}
	for _, tc := range cases {
		got := CurrentRegimeCT(mustParseRFC3339(t, tc.ts))
		if got != tc.want {
			t.Fatalf("ts=%s want=%s got=%s", tc.ts, tc.want, got)
		}
	}
}

func TestIsMajorOverlapCT(t *testing.T) {
	ts := mustParseRFC3339(t, "2026-03-04T14:59:00Z") // 08:59 CST
	if !IsMajorOverlapCT(ts) {
		t.Fatal("expected overlap true")
	}
	ts = mustParseRFC3339(t, "2026-03-04T23:10:00Z") // 17:10 CST
	if IsMajorOverlapCT(ts) {
		t.Fatal("expected overlap false")
	}
}
