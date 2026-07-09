package notify

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
)

type ScannerRow struct {
	Rank   int
	Symbol string
	Grade  string
	Score  float64
	State  string
	Price  float64
	DayPct float64
	H4Pct  float64
	H1Pct  float64
}

type ScannerView struct {
	Session           string
	Timestamp         string
	MarketBias        string
	LongRows          []ScannerRow
	ShortRows         []ScannerRow
	LiveManualSection string
}

type StatusView struct {
	Mode          string
	EnabledState  string
	TopSymbol     string
	TopSide       string
	MarketBias    string
	OpenPositions int
	AvailableUSDT float64
	PaperPnL      float64
	LivePnL       float64
	IssuesLine    string
}

type EntryView struct {
	Mode     string
	Symbol   string
	Side     string
	Setup    string
	Strategy string
	Grade    string
	Margin   float64
	Leverage int
	Entry    float64
	Stop     float64
	TP1      float64
	TP2      float64
	TP3      float64
}

type PositionView struct {
	Symbol   string
	Side     string
	Reason   string
	PnL      float64
	Entry    float64
	Price    float64
	DayPct   float64
	Stop     float64
	NextTP   float64
	HoldTime string
	Margin   float64
	Leverage int
}

type ExitView struct {
	Mode                  string
	Symbol                string
	Side                  string
	ExitReason            string
	HoldTime              string
	RealizedPnL           float64
	RMultiple             float64
	FillPrice             float64
	Stop                  float64
	Entry                 float64
	ExitPrice             float64
	RemainingPositionLine string
}

type RiskView struct {
	RiskState      string
	SymbolOrScope  string
	RiskMessage    string
	OperatorAction string
}

type ManualView struct {
	Symbol   string
	Side     string
	Quantity float64
	Entry    float64
	Margin   float64
	Leverage int
}

type AccountView struct {
	Mode          string
	Timestamp     string
	AvailableUSDT float64
	Equity        float64
	PaperPnL      float64
	LivePnL       float64
	OpenPositions int
}

type RecapView struct {
	Mode              string
	Date              string
	RealizedPnL       float64
	TradeCount        int
	WinRate           float64
	BestTrade         string
	WorstTrade        string
	RiskNoteOrSummary string
}

func FormatScannerSnapshot(data ScannerView) string {
	var b strings.Builder
	b.WriteString("📡 <b>SCANNER</b>\n")
	fmt.Fprintf(&b, "<b>%s</b> · %s · Bias <b>%s</b>\n",
		htmlEscape(nonEmptyText(data.Session, "Session")),
		htmlEscape(nonEmptyText(data.Timestamp, "--:--")),
		htmlEscape(nonEmptyText(displayStateShort(data.MarketBias), "NEUTRAL")),
	)
	b.WriteString("<pre>")
	b.WriteString(renderScannerPanel(data.LongRows, data.ShortRows))
	b.WriteString("</pre>")
	if strings.TrimSpace(data.LiveManualSection) != "" {
		b.WriteString("\n")
		b.WriteString(htmlEscape(strings.TrimSpace(data.LiveManualSection)))
	}
	return strings.TrimSpace(b.String())
}

func FormatBotStatus(data StatusView) string {
	lines := []string{
		"🧭 <b>BOT STATUS</b>",
		fmt.Sprintf("<b>%s</b> · <b>%s</b> · %s %s",
			htmlEscape(displayMode(data.Mode)),
			htmlEscape(nonEmptyText(strings.ToUpper(strings.TrimSpace(data.EnabledState)), "UNKNOWN")),
			htmlEscape(nonEmptyText(strings.ToUpper(strings.TrimSpace(data.TopSymbol)), "-")),
			htmlEscape(displaySide(data.TopSide)),
		),
		fmt.Sprintf("Open %d · Avail %s · Bias %s",
			data.OpenPositions,
			fmtUSDT(data.AvailableUSDT),
			htmlEscape(nonEmptyText(displayStateShort(data.MarketBias), "NEUTRAL")),
		),
		fmt.Sprintf("Paper %s · Live %s", fmtPnL(data.PaperPnL), fmtPnL(data.LivePnL)),
	}
	if strings.TrimSpace(data.IssuesLine) != "" {
		lines = append(lines, htmlEscape(strings.TrimSpace(data.IssuesLine)))
	}
	return strings.Join(lines, "\n")
}

func FormatEntry(data EntryView) string {
	icon := "🟦"
	title := "LIVE ENTRY"
	if strings.EqualFold(strings.TrimSpace(data.Mode), "paper") {
		icon = "🧪"
		title = "PAPER ENTRY"
	}
	lines := []string{
		fmt.Sprintf("%s <b>%s</b>", icon, title),
		fmt.Sprintf("<b>%s %s</b> · %s",
			htmlEscape(strings.ToUpper(strings.TrimSpace(data.Symbol))),
			htmlEscape(displaySide(data.Side)),
			htmlEscape(nonEmptyText(data.Setup, data.Strategy)),
		),
		fmt.Sprintf("%s @ %dx · %s", fmtUSDT(data.Margin), maxIntLocal(data.Leverage, 1), htmlEscape(nonEmptyText(data.Strategy, "setup"))),
		fmt.Sprintf("Entry %s · Stop %s", fmtPriceAdaptive(data.Entry), fmtPriceAdaptive(data.Stop)),
		fmt.Sprintf("TP %s / %s / %s", fmtMaybePrice(data.TP1), fmtMaybePrice(data.TP2), fmtMaybePrice(data.TP3)),
	}
	if strings.TrimSpace(data.Grade) != "" {
		lines = append(lines, fmt.Sprintf("Grade <b>%s</b>", htmlEscape(strings.TrimSpace(data.Grade))))
	}
	return strings.Join(lines, "\n")
}

func FormatPositionUpdate(data PositionView) string {
	lines := []string{
		"📍 <b>POSITION UPDATE</b>",
		fmt.Sprintf("<b>%s %s</b> · PnL <b>%s</b>",
			htmlEscape(strings.ToUpper(strings.TrimSpace(data.Symbol))),
			htmlEscape(displaySide(data.Side)),
			fmtPnL(data.PnL),
		),
		fmt.Sprintf("Entry %s · Mark %s · %s", fmtPriceAdaptive(data.Entry), fmtPriceAdaptive(data.Price), fmtPct2(data.DayPct)),
		fmt.Sprintf("Stop %s · Next TP %s", fmtMaybePrice(data.Stop), fmtMaybePrice(data.NextTP)),
		fmt.Sprintf("Hold %s · %s @ %dx", htmlEscape(nonEmptyText(data.HoldTime, "-")), fmtUSDT(data.Margin), maxIntLocal(data.Leverage, 1)),
	}
	if strings.TrimSpace(data.Reason) != "" {
		lines = append(lines, fmt.Sprintf("Reason %s", htmlEscape(nonEmptyText(data.Reason, "-"))))
	}
	return strings.Join(lines, "\n")
}

func FormatTPHit(data ExitView) string {
	header := "✅ <b>TP HIT</b>"
	if mode := strings.TrimSpace(displayMode(data.Mode)); mode != "" {
		header = fmt.Sprintf("✅ <b>%s TAKE PROFIT</b>", htmlEscape(mode))
	}
	lines := []string{
		header,
		fmt.Sprintf("<b>%s %s</b> · %s",
			htmlEscape(strings.ToUpper(strings.TrimSpace(data.Symbol))),
			htmlEscape(displaySide(data.Side)),
			htmlEscape(humanExitReason(data.ExitReason)),
		),
		fmt.Sprintf("Realized <b>%s</b> · %s", fmtPnL(data.RealizedPnL), fmtRMultiple(data.RMultiple)),
		fmt.Sprintf("Filled %s · Hold %s", fmtPriceAdaptive(data.FillPrice), htmlEscape(nonEmptyText(data.HoldTime, "-"))),
	}
	if strings.TrimSpace(data.RemainingPositionLine) != "" {
		lines = append(lines, htmlEscape(strings.TrimSpace(data.RemainingPositionLine)))
	}
	return strings.Join(lines, "\n")
}

func FormatSLHit(data ExitView) string {
	header := "🛑 <b>STOP HIT</b>"
	if mode := strings.TrimSpace(displayMode(data.Mode)); mode != "" {
		header = fmt.Sprintf("🛑 <b>%s EXIT</b>", htmlEscape(mode))
	}
	lines := []string{
		header,
		fmt.Sprintf("<b>%s %s</b> · %s",
			htmlEscape(strings.ToUpper(strings.TrimSpace(data.Symbol))),
			htmlEscape(displaySide(data.Side)),
			htmlEscape(humanExitReason(data.ExitReason)),
		),
		fmt.Sprintf("Realized <b>%s</b> · %s", fmtPnL(data.RealizedPnL), fmtRMultiple(data.RMultiple)),
		fmt.Sprintf("Stop %s · Hold %s", fmtMaybePrice(data.Stop), htmlEscape(nonEmptyText(data.HoldTime, "-"))),
	}
	if strings.TrimSpace(data.RemainingPositionLine) != "" {
		lines = append(lines, htmlEscape(strings.TrimSpace(data.RemainingPositionLine)))
	}
	return strings.Join(lines, "\n")
}

func FormatTradeClosed(data ExitView) string {
	header := "📤 <b>TRADE CLOSED</b>"
	if mode := strings.TrimSpace(displayMode(data.Mode)); mode != "" {
		header = fmt.Sprintf("📤 <b>%s EXIT</b>", htmlEscape(mode))
	}
	lines := []string{
		header,
		fmt.Sprintf("<b>%s %s</b> · <b>%s</b>",
			htmlEscape(strings.ToUpper(strings.TrimSpace(data.Symbol))),
			htmlEscape(displaySide(data.Side)),
			fmtPnL(data.RealizedPnL),
		),
		fmt.Sprintf("%s · Hold %s", fmtRMultiple(data.RMultiple), htmlEscape(nonEmptyText(data.HoldTime, "-"))),
		fmt.Sprintf("Entry %s → Exit %s", fmtPriceAdaptive(data.Entry), fmtPriceAdaptive(data.ExitPrice)),
		fmt.Sprintf("Reason %s", htmlEscape(humanExitReason(data.ExitReason))),
	}
	if strings.TrimSpace(data.RemainingPositionLine) != "" {
		lines = append(lines, htmlEscape(strings.TrimSpace(data.RemainingPositionLine)))
	}
	return strings.Join(lines, "\n")
}

func FormatRiskAlert(data RiskView) string {
	return strings.Join([]string{
		"⚠️ <b>RISK ALERT</b>",
		fmt.Sprintf("<b>%s</b> · %s",
			htmlEscape(nonEmptyText(strings.ToUpper(strings.TrimSpace(data.RiskState)), "WATCH")),
			htmlEscape(nonEmptyText(data.SymbolOrScope, "system")),
		),
		htmlEscape(nonEmptyText(data.RiskMessage, "Operator review required")),
		fmt.Sprintf("Action %s", htmlEscape(nonEmptyText(data.OperatorAction, "Review in terminal"))),
	}, "\n")
}

func FormatManualDetected(data ManualView) string {
	symbol := strings.ToUpper(strings.TrimSpace(data.Symbol))
	return strings.Join([]string{
		"👀 <b>MANUAL POSITION</b>",
		fmt.Sprintf("<b>%s %s</b> · Exchange-detected", htmlEscape(symbol), htmlEscape(displaySide(data.Side))),
		fmt.Sprintf("Qty %s · Entry %s · %s @ %dx", fmtQtyAdaptive(data.Quantity), fmtPriceAdaptive(data.Entry), fmtUSDT(data.Margin), maxIntLocal(data.Leverage, 1)),
		"Status <b>Awaiting approval</b>",
		fmt.Sprintf("Reply <code>/manage %s y</code> or <code>/manage %s n</code>", htmlEscape(symbol), htmlEscape(symbol)),
	}, "\n")
}

func FormatAccountSummary(data AccountView) string {
	var b strings.Builder
	b.WriteString("💼 <b>ACCOUNT</b>\n")
	fmt.Fprintf(&b, "<b>%s</b> · %s\n", htmlEscape(displayMode(data.Mode)), htmlEscape(nonEmptyText(data.Timestamp, "--:--")))
	b.WriteString("<pre>")
	fmt.Fprintf(&b, "Avail   %s\n", fmtUSDT(data.AvailableUSDT))
	fmt.Fprintf(&b, "Equity  %s\n", fmtUSDT(data.Equity))
	fmt.Fprintf(&b, "Paper   %s\n", fmtPnL(data.PaperPnL))
	fmt.Fprintf(&b, "Live    %s", fmtPnL(data.LivePnL))
	b.WriteString("</pre>\n")
	fmt.Fprintf(&b, "Open %d", data.OpenPositions)
	return strings.TrimSpace(b.String())
}

func FormatDailyRecap(data RecapView) string {
	lines := []string{
		"🧾 <b>DAILY RECAP</b>",
		fmt.Sprintf("<b>%s</b> · %s", htmlEscape(displayMode(data.Mode)), htmlEscape(nonEmptyText(data.Date, "-"))),
		fmt.Sprintf("PnL <b>%s</b> · Trades %d · Win %s", fmtPnL(data.RealizedPnL), data.TradeCount, fmtPct1(data.WinRate)),
		fmt.Sprintf("Best %s · Worst %s", htmlEscape(nonEmptyText(data.BestTrade, "-")), htmlEscape(nonEmptyText(data.WorstTrade, "-"))),
	}
	if strings.TrimSpace(data.RiskNoteOrSummary) != "" {
		lines = append(lines, htmlEscape(strings.TrimSpace(data.RiskNoteOrSummary)))
	}
	return strings.Join(lines, "\n")
}

func fmtPriceAdaptive(v float64) string {
	switch {
	case v == 0:
		return "-"
	case math.Abs(v) >= 1000:
		return strconv.FormatFloat(v, 'f', 2, 64)
	case math.Abs(v) >= 1:
		return strconv.FormatFloat(v, 'f', 4, 64)
	case math.Abs(v) < 0.01:
		return strconv.FormatFloat(v, 'f', 8, 64)
	default:
		return strconv.FormatFloat(v, 'f', 6, 64)
	}
}

func fmtMaybePrice(v float64) string {
	if v <= 0 {
		return "-"
	}
	return fmtPriceAdaptive(v)
}

func fmtPnL(v float64) string {
	return fmt.Sprintf("%+.2f", v)
}

func fmtPct1(v float64) string {
	return fmt.Sprintf("%+.1f%%", v)
}

func fmtPct2(v float64) string {
	return fmt.Sprintf("%+.2f%%", v)
}

func fmtScore(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

func fmtQtyAdaptive(v float64) string {
	av := math.Abs(v)
	switch {
	case av >= 100:
		return fmt.Sprintf("%.2f", v)
	case av >= 1:
		return fmt.Sprintf("%.3f", v)
	default:
		return fmt.Sprintf("%.4f", v)
	}
}

func fmtUSDT(v float64) string {
	return fmt.Sprintf("%.2f USDT", v)
}

func fmtRMultiple(v float64) string {
	if v == 0 {
		return "R -"
	}
	return fmt.Sprintf("R %.2f", v)
}

func humanExitReason(reason string) string {
	r := strings.ToUpper(strings.TrimSpace(reason))
	switch r {
	case "", "UNKNOWN":
		return "Exit"
	case "TP", "TP1", "TAKE_PROFIT_1":
		return "Take Profit 1"
	case "TP2", "TAKE_PROFIT_2":
		return "Take Profit 2"
	case "TP3", "TAKE_PROFIT_3":
		return "Take Profit 3"
	case "SL", "STOP", "STOP_LOSS":
		return "Stop Loss"
	case "TRAIL", "TRAIL_EXIT", "TRAILING_STOP":
		return "Trail Exit"
	case "MOMENTUM_EXIT", "MOMENTUM":
		return "Momentum Exit"
	case "FORCE_CLOSE":
		return "Forced Close"
	case "MANUAL", "MANUAL_CLOSE":
		return "Manual Close"
	case "PRE_EOD_EXIT":
		return "Pre-EOD Exit"
	case "FUNDING_AWARE_PRE_EXIT":
		return "Funding Exit"
	default:
		return strings.Title(strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(r, "_", " "), "-", " ")))
	}
}

func displayStateShort(state string) string {
	s := strings.ToUpper(strings.TrimSpace(state))
	switch s {
	case "LONG":
		return "LONG"
	case "SHORT":
		return "SHORT"
	case "NEUTRAL":
		return "NEUTRAL"
	case "HEATING":
		return "READY"
	case "PENDING_PROTECTION":
		return "PROTECT"
	case "FORCE_CLOSE_TRIGGERED":
		return "FORCE"
	}
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	if len(s) > 7 {
		return s[:7]
	}
	if s == "" {
		return "-"
	}
	return s
}

func displayMode(mode string) string {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "PAPER", "DRY_RUN":
		return "PAPER"
	case "LIVE":
		return "LIVE"
	default:
		return strings.ToUpper(strings.TrimSpace(mode))
	}
}

func displaySide(side string) string {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "BUY", "LONG":
		return "LONG"
	case "SELL", "SHORT":
		return "SHORT"
	default:
		return strings.ToUpper(strings.TrimSpace(side))
	}
}

func htmlEscape(s string) string {
	return html.EscapeString(strings.TrimSpace(s))
}

func renderScannerPanel(longs, shorts []ScannerRow) string {
	lines := []string{
		"L  SYM    G   S   STATE   PRICE      D%    4H%   1H%",
	}
	if len(longs) == 0 {
		lines = append(lines, "1  -      -   -   -       -          -     -     -")
	} else {
		for i, row := range longs {
			if i >= 3 {
				break
			}
			lines = append(lines, scannerRowLine(row))
		}
	}
	lines = append(lines, "", "S  SYM    G   S   STATE   PRICE      D%    4H%   1H%")
	if len(shorts) == 0 {
		lines = append(lines, "1  -      -   -   -       -          -     -     -")
	} else {
		for i, row := range shorts {
			if i >= 3 {
				break
			}
			lines = append(lines, scannerRowLine(row))
		}
	}
	return htmlEscape(strings.Join(lines, "\n"))
}

func scannerRowLine(row ScannerRow) string {
	rank := row.Rank
	if rank <= 0 {
		rank = 1
	}
	return fmt.Sprintf("%-2d %-6s %-3s %-3s %-7s %-10s %-5s %-5s %-5s",
		rank,
		clampText(strings.ToUpper(strings.TrimSpace(row.Symbol)), 6),
		clampText(strings.ToUpper(strings.TrimSpace(row.Grade)), 3),
		clampText(fmtScore(row.Score), 3),
		clampText(displayStateShort(row.State), 7),
		clampText(fmtPriceAdaptive(row.Price), 10),
		clampText(strings.TrimSuffix(fmtPct1(row.DayPct), "%"), 5),
		clampText(strings.TrimSuffix(fmtPct1(row.H4Pct), "%"), 5),
		clampText(strings.TrimSuffix(fmtPct1(row.H1Pct), "%"), 5),
	)
}

func clampText(s string, width int) string {
	s = strings.TrimSpace(s)
	if len(s) > width {
		return s[:width]
	}
	return s
}

func nonEmptyText(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func maxIntLocal(a, b int) int {
	if a > b {
		return a
	}
	return b
}
