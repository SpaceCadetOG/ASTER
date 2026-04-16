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
		Status:           "PENDING_PROTECTION",
		ManageState:      "PENDING_PROTECTION",
		Managed:          true,
		Protected:        false,
		NextAction:       "await stop attach",
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
	if !strings.Contains(msg, "LONG") || strings.Contains(msg, "BUY") {
		t.Fatalf("expected LONG labeling without BUY, got %q", msg)
	}
	if !strings.Contains(msg, "PENDING_PROTECTION") {
		t.Fatalf("expected protection status in card, got %q", msg)
	}
	if !strings.Contains(msg, "UNPROTECTED MANAGED TRADE") {
		t.Fatalf("expected unprotected managed banner, got %q", msg)
	}
	if !strings.Contains(msg, "Managed:</b> YES") || !strings.Contains(msg, "Exchange Stop:</b> NO") || !strings.Contains(msg, "Next:</b> await stop attach") {
		t.Fatalf("expected managed/protected and next-action details, got %q", msg)
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
	if !strings.Contains(msg, "TOP SCANS") || !strings.Contains(msg, "s=<b>82</b>") || !strings.Contains(msg, "heating") {
		t.Fatalf("expected richer scanner details, got %q", msg)
	}
}

func TestBuildManagementStatusCard(t *testing.T) {
	msg := BuildManagementStatusCard(ManageStateDegraded, "BTCUSDT", "LONG",
		"<b>Cause:</b> invalid_after_retry",
		"<b>Retries:</b> 3/4",
	)
	if !strings.Contains(msg, "DEGRADED") {
		t.Fatalf("expected degraded header, got %q", msg)
	}
	if !strings.Contains(msg, "BTCUSDT LONG") {
		t.Fatalf("expected symbol/side context, got %q", msg)
	}
	if !strings.Contains(msg, "invalid_after_retry") || !strings.Contains(msg, "3/4") {
		t.Fatalf("expected lifecycle details, got %q", msg)
	}
}
