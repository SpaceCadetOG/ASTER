package notify

import (
	"strings"
	"testing"
)

func TestBuildPositionCard(t *testing.T) {
	msg := BuildPositionCard(PositionCard{
		Symbol:           "BTCUSDT",
		Side:             "BUY",
		Source:           "MANUAL",
		Qty:              0.25,
		EntryPrice:       100,
		MarkPrice:        101,
		LastPrice:        101.2,
		SpreadBps:        3.2,
		UnrealizedPnL:    3.2,
		UnrealizedPnLPct: 1.2,
		Leverage:         3,
		Setup:            "fa",
		Confluence:       0.63,
		AgeMin:           15,
		StopLoss:         98.5,
		TakeProfit:       104,
	})
	if msg == "" || msg[0] != '<' {
		t.Fatalf("expected html-style message, got %q", msg)
	}
	if !strings.Contains(msg, "MANUAL") || !strings.Contains(msg, "Qty") || !strings.Contains(msg, "Last") {
		t.Fatalf("expected richer live fields in card, got %q", msg)
	}
}

func TestBuildScannerSnapshotHTML(t *testing.T) {
	msg := BuildScannerSnapshotHTML([]ScanItem{{
		Symbol:    "BTCUSDT",
		Side:      "LONG",
		Grade:     "A",
		Score:     82,
		Slope:     0.15,
		State:     "heating",
		Price:     68123.45,
		DayUTC:    3.2,
		UTC4h:     1.4,
		UTC1h:     0.5,
		VolumeUSD: 18_500_000,
	}}, nil, "neutral")
	if msg == "" {
		t.Fatal("expected scanner snapshot")
	}
	if !strings.Contains(msg, "score") || !strings.Contains(msg, "vol=") || !strings.Contains(msg, "heating") {
		t.Fatalf("expected richer scanner details, got %q", msg)
	}
}
