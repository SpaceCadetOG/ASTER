package notify

import (
	"strings"
	"testing"
)

func TestHTMLEscape(t *testing.T) {
	got := htmlEscape(`BTC<&>"`)
	if got != "BTC&lt;&amp;&gt;&#34;" {
		t.Fatalf("unexpected escape: %q", got)
	}
}

func TestFmtPnL(t *testing.T) {
	if got := fmtPnL(12.345); got != "+12.35" {
		t.Fatalf("expected rounded pnl, got %q", got)
	}
	if got := fmtPnL(-2); got != "-2.00" {
		t.Fatalf("expected signed pnl, got %q", got)
	}
}

func TestFmtPriceAdaptive(t *testing.T) {
	cases := map[string]string{
		"big":   fmtPriceAdaptive(1234.567),
		"mid":   fmtPriceAdaptive(12.345678),
		"small": fmtPriceAdaptive(0.123456789),
		"tiny":  fmtPriceAdaptive(0.00123456789),
	}
	if cases["big"] != "1234.57" {
		t.Fatalf("expected 2 decimals for big price, got %q", cases["big"])
	}
	if cases["mid"] != "12.3457" {
		t.Fatalf("expected 4 decimals for mid price, got %q", cases["mid"])
	}
	if cases["small"] != "0.123457" {
		t.Fatalf("expected 6 decimals for small price, got %q", cases["small"])
	}
	if cases["tiny"] != "0.00123457" {
		t.Fatalf("expected 8 decimals for tiny price, got %q", cases["tiny"])
	}
}

func TestScannerRowAlignment(t *testing.T) {
	msg := FormatScannerSnapshot(ScannerView{
		Session:    "NY AM",
		Timestamp:  "09:41 CT",
		MarketBias: "long",
		LongRows: []ScannerRow{{
			Rank:   1,
			Symbol: "BTC",
			Grade:  "A",
			Score:  88,
			State:  "armed",
			Price:  104220.5,
			DayPct: 2.1,
			H4Pct:  1.4,
			H1Pct:  0.6,
		}},
		ShortRows: []ScannerRow{{
			Rank:   1,
			Symbol: "ETH",
			Grade:  "B",
			Score:  77,
			State:  "fade",
			Price:  5832.2,
			DayPct: -1.1,
			H4Pct:  -0.7,
			H1Pct:  -0.4,
		}},
	})
	if !strings.Contains(msg, "<pre>L  SYM") {
		t.Fatalf("expected scanner pre block, got %q", msg)
	}
	if !strings.Contains(msg, "1  BTC") || !strings.Contains(msg, "1  ETH") {
		t.Fatalf("expected aligned scanner rows, got %q", msg)
	}
}

func TestFormatBotStatus(t *testing.T) {
	msg := FormatBotStatus(StatusView{
		Mode:          "live",
		EnabledState:  "enabled",
		TopSymbol:     "BTC",
		TopSide:       "long",
		MarketBias:    "long",
		OpenPositions: 2,
		AvailableUSDT: 842.5,
		PaperPnL:      124.3,
		LivePnL:       51.3,
		IssuesLine:    "Pending: 1 manual approval",
	})
	if !strings.Contains(msg, "<b>LIVE</b> · <b>ENABLED</b> · BTC LONG") {
		t.Fatalf("expected premium status header, got %q", msg)
	}
	if !strings.Contains(msg, "Open 2 · Avail 842.50 USDT · Bias LONG") {
		t.Fatalf("expected compact status metrics, got %q", msg)
	}
}

func TestFormatEntry(t *testing.T) {
	msg := FormatEntry(EntryView{
		Mode:     "paper",
		Symbol:   "SOL",
		Side:     "buy",
		Setup:    "Breakout",
		Strategy: "vp_trend",
		Grade:    "A",
		Margin:   50,
		Leverage: 5,
		Entry:    182.44,
		Stop:     179.10,
		TP1:      185.20,
		TP2:      187.50,
		TP3:      190.00,
	})
	if !strings.Contains(msg, "🧪 <b>PAPER ENTRY</b>") {
		t.Fatalf("expected paper entry header, got %q", msg)
	}
	if !strings.Contains(msg, "<b>SOL LONG</b> · Breakout") {
		t.Fatalf("expected symbol and setup, got %q", msg)
	}
}

func TestFormatTradeClosed(t *testing.T) {
	msg := FormatTradeClosed(ExitView{
		Symbol:      "BTC",
		Side:        "short",
		RealizedPnL: 42.67,
		RMultiple:   1.84,
		HoldTime:    "38m",
		Entry:       104220.5,
		ExitPrice:   103850.0,
		ExitReason:  "momentum_exit",
	})
	if !strings.Contains(msg, "📤 <b>TRADE CLOSED</b>") {
		t.Fatalf("expected trade closed header, got %q", msg)
	}
	if !strings.Contains(msg, "Reason Momentum Exit") {
		t.Fatalf("expected human exit reason, got %q", msg)
	}
}

func TestHumanExitReasonMapping(t *testing.T) {
	if got := humanExitReason("tp2"); got != "Take Profit 2" {
		t.Fatalf("expected TP mapping, got %q", got)
	}
	if got := humanExitReason("funding_aware_pre_exit"); got != "Funding Exit" {
		t.Fatalf("expected funding mapping, got %q", got)
	}
}
