package mlschema

import (
	"testing"
	"time"

	"go-machine/internal/paperreport"
)

func TestBuildFeatureRow(t *testing.T) {
	rec := paperreport.ClosedTradeRecord{
		TradeID: "t1",
		Symbol:  "btcusdt",
		Side:    "buy",
		Identity: paperreport.ClosedTradeIdentity{
			Strategy:         "vp_trend",
			SetupFamily:      "cont",
			Session:          "london",
			EntryTiming:      "fresh",
			ConfluenceScore:  0.72,
			CandidateAgeSecs: 90,
			DistanceToVWAP:   1.5,
			ATRExension:      0.8,
		},
		Entry: paperreport.ClosedTradeEntry{
			EntryTs:    time.Unix(10, 0).UTC(),
			EntryPrice: 100,
			Qty:        2,
		},
		Plan: paperreport.ClosedTradePlan{
			OriginalStop:  98,
			Pct24hAtEntry: 4,
			Pct4hAtEntry:  2,
			Pct1hAtEntry:  1,
		},
		Exit: paperreport.ClosedTradeExit{
			NetPnL:      4,
			HoldMinutes: 5,
			MaxRSeen:    1.8,
			ExitReason:  "profit_lock",
			HitTP1:      true,
			HitTP2:      false,
			HitTP3:      false,
		},
		PostExit: paperreport.ClosedTradePostExit{
			PostExitPeakR:      2.0,
			StoppedThenReclaim: true,
			ReentryWouldWork:   true,
		},
	}

	row := BuildFeatureRow(rec)
	if row.Symbol != "BTCUSDT" {
		t.Fatalf("expected normalized symbol, got %q", row.Symbol)
	}
	if !row.Win {
		t.Fatalf("expected positive realized R to be win")
	}
	if row.RealizedR != 1 {
		t.Fatalf("expected realized R 1.0, got %.4f", row.RealizedR)
	}
	if !row.TP1Hit || row.TP2Hit || row.TP3Hit {
		t.Fatalf("unexpected TP flags: %+v", row)
	}
}
