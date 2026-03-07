package inplay

import (
	"testing"
	"time"

	"go-machine/internal/market"
)

func TestTrackerStatesPumpingAndDumpingLong(t *testing.T) {
	trk := NewTracker("long", Config{MinGrade: "C", MinVolumeUSD: 1000, HistoryN: 5, RiseN: 3, DropGradeScans: 10, FallScans: 10, TTL: time.Hour})
	now := time.Now().UTC()
	grades := map[string]string{"BTCUSDT": "A+"}

	rows := []market.Scored{{Market: market.Market{Symbol: "BTCUSDT", VolumeUSD: 2000, LastPrice: 100, Change24h: 8}, Score: 80, Eligible: true}}
	trk.Update(now, rows, grades)
	rows[0].LastPrice = 101
	rows[0].Score = 86
	rows[0].VolumeUSD = 2200
	rows[0].Change24h = 9
	trk.Update(now.Add(1*time.Minute), rows, grades)
	rows[0].LastPrice = 103
	rows[0].Score = 92
	rows[0].VolumeUSD = 2500
	rows[0].Change24h = 10
	trk.Update(now.Add(2*time.Minute), rows, grades)

	es := trk.Entries()
	if len(es) != 1 {
		t.Fatalf("expected 1 entry")
	}
	if es[0].State != StatePumping && es[0].State != StateInPlay {
		t.Fatalf("expected pumping/in-play, got %s", es[0].State)
	}

	rows[0].LastPrice = 99
	rows[0].Score = 70
	rows[0].VolumeUSD = 1700
	rows[0].Change24h = -1
	trk.Update(now.Add(3*time.Minute), rows, grades)
	es = trk.Entries()
	if len(es) != 1 {
		t.Fatalf("expected 1 entry after dump")
	}
	if es[0].State != StateDumping && es[0].State != StateCooling && es[0].State != StateExhausted {
		t.Fatalf("expected dumping/cooling/exhausted, got %s", es[0].State)
	}
}

func TestTrackerExhaustedTransition(t *testing.T) {
	trk := NewTracker("long", Config{MinGrade: "C", MinVolumeUSD: 1000, HistoryN: 6, RiseN: 3, DropGradeScans: 10, FallScans: 10, TTL: time.Hour})
	now := time.Now().UTC()
	grades := map[string]string{"BARDUSDT": "A+"}
	rows := []market.Scored{{Market: market.Market{Symbol: "BARDUSDT", VolumeUSD: 3000, LastPrice: 1.0, Change24h: 20}, Score: 95, Eligible: true}}
	trk.Update(now, rows, grades)
	rows[0].LastPrice, rows[0].Score, rows[0].VolumeUSD = 1.08, 103, 3200
	trk.Update(now.Add(1*time.Minute), rows, grades)
	rows[0].LastPrice, rows[0].Score, rows[0].VolumeUSD = 1.12, 110, 3300
	trk.Update(now.Add(2*time.Minute), rows, grades)
	rows[0].LastPrice, rows[0].Score, rows[0].VolumeUSD = 1.03, 86, 1800
	rows[0].Change24h = 4
	trk.Update(now.Add(3*time.Minute), rows, grades)
	es := trk.Entries()
	if len(es) != 1 {
		t.Fatalf("expected 1 entry")
	}
	if es[0].State != StateExhausted && es[0].State != StateDumping {
		t.Fatalf("expected exhausted/dumping, got %s", es[0].State)
	}
}

func TestTrackerStatesPumpingAndDumpingShort(t *testing.T) {
	trk := NewTracker("short", Config{MinGrade: "C", MinVolumeUSD: 1000, HistoryN: 5, RiseN: 3, DropGradeScans: 10, FallScans: 10, TTL: time.Hour})
	now := time.Now().UTC()
	grades := map[string]string{"ETHUSDT": "A+"}

	rows := []market.Scored{{Market: market.Market{Symbol: "ETHUSDT", VolumeUSD: 2000, LastPrice: 100, Change24h: -8}, Score: 80, Eligible: true}}
	trk.Update(now, rows, grades)
	rows[0].LastPrice = 99
	rows[0].Score = 85
	rows[0].VolumeUSD = 2300
	rows[0].Change24h = -9
	trk.Update(now.Add(1*time.Minute), rows, grades)
	rows[0].LastPrice = 97
	rows[0].Score = 91
	rows[0].VolumeUSD = 2600
	rows[0].Change24h = -10
	trk.Update(now.Add(2*time.Minute), rows, grades)

	es := trk.Entries()
	if len(es) != 1 {
		t.Fatalf("expected 1 entry")
	}
	if es[0].State != StatePumping && es[0].State != StateInPlay {
		t.Fatalf("expected pumping/in-play, got %s", es[0].State)
	}

	rows[0].LastPrice = 101
	rows[0].Score = 69
	rows[0].VolumeUSD = 1700
	rows[0].Change24h = 1
	trk.Update(now.Add(3*time.Minute), rows, grades)
	es = trk.Entries()
	if len(es) != 1 {
		t.Fatalf("expected 1 entry after reverse")
	}
	if es[0].State != StateDumping && es[0].State != StateCooling && es[0].State != StateExhausted {
		t.Fatalf("expected dumping/cooling/exhausted, got %s", es[0].State)
	}
}
