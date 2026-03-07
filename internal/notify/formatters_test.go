package notify

import "testing"

func TestBuildPositionCard(t *testing.T) {
	msg := BuildPositionCard(PositionCard{
		Symbol:           "BTCUSDT",
		Side:             "BUY",
		EntryPrice:       100,
		MarkPrice:        101,
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
}

func TestBuildScannerSnapshotHTML(t *testing.T) {
	msg := BuildScannerSnapshotHTML([]ScanItem{{Symbol: "BTCUSDT", Grade: "A", Score: 82}}, nil, "neutral")
	if msg == "" {
		t.Fatal("expected scanner snapshot")
	}
}
