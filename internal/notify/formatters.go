package notify

import (
	"fmt"
	"strings"
)

type PulseSnapshot struct {
	Title     string
	TimeLabel string
	Session   string
	Balance   float64
	Equity    float64
	Realized  float64
	OpenPnL   float64
	NetDay    float64
	OpenCount int
	NetDayPct float64
}

type PositionCard struct {
	Symbol           string
	Side             string
	EntryPrice       float64
	MarkPrice        float64
	UnrealizedPnL    float64
	UnrealizedPnLPct float64
	Leverage         int
	Setup            string
	Confluence       float64
	AgeMin           int
	StopLoss         float64
	TakeProfit       float64
}

type ScanItem struct {
	Symbol string
	Grade  string
	Score  float64
}

func BuildSessionPulseHTML(p PulseSnapshot) string {
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = "SESSION"
	}
	dayEmoji := "🟢"
	if p.NetDay < 0 {
		dayEmoji = "🔴"
	}
	return strings.TrimSpace(fmt.Sprintf(
		"<b>☀️ %s: %s (%s)</b>\n"+
			"<b>💰 ACCOUNT OVERVIEW</b>\n"+
			"• <b>Balance:</b> $%.2f | <b>Equity:</b> $%.2f\n"+
			"• <b>Net Day:</b> %s %+.2f (%.2f%%)\n"+
			"• <b>Realized:</b> %+.2f | <b>Open PnL:</b> %+.2f (%d pos)",
		title, strings.ToUpper(strings.TrimSpace(p.Session)), strings.TrimSpace(p.TimeLabel),
		p.Balance, p.Equity, dayEmoji, p.NetDay, p.NetDayPct, p.Realized, p.OpenPnL, p.OpenCount,
	))
}

func BuildPositionCard(p PositionCard) string {
	direction := "🟢 BUY"
	if strings.EqualFold(strings.TrimSpace(p.Side), "SELL") {
		direction = "🔴 SELL"
	}
	setup := strings.TrimSpace(p.Setup)
	if setup == "" {
		setup = "none"
	}
	if p.Leverage <= 0 {
		p.Leverage = 1
	}
	return strings.TrimSpace(fmt.Sprintf(
		"<b>📦 ACTIVE: %s (%s)</b>\n"+
			"• <b>Price:</b> %.4f → %.4f (<b>%+.2f%%</b>)\n"+
			"• <b>PnL:</b> %s$%.2f | <b>Lev:</b> %dx\n"+
			"• <b>Setup:</b> <code>%s</code> (Conf: %.0f%%) | <b>Age:</b> %dm\n"+
			"• <b>Safety:</b> SL: %.4f | TP: %.4f",
		strings.ToUpper(strings.TrimSpace(p.Symbol)), direction,
		p.EntryPrice, p.MarkPrice, p.UnrealizedPnLPct,
		pnlEmoji(p.UnrealizedPnL), p.UnrealizedPnL, p.Leverage,
		setup, p.Confluence*100.0, p.AgeMin, p.StopLoss, p.TakeProfit,
	))
}

func BuildScannerSnapshotHTML(longs, shorts []ScanItem, bias string) string {
	return strings.TrimSpace(fmt.Sprintf(
		"<b>📡 TOP SCANS</b>\n"+
			"• <b>LONG:</b> %s\n"+
			"• <b>SHORT:</b> %s\n"+
			"⚡ <b>Bias:</b> %s",
		renderScanLine(longs),
		renderScanLine(shorts),
		strings.ToUpper(strings.TrimSpace(bias)),
	))
}

func pnlEmoji(v float64) string {
	if v < 0 {
		return "🔴 "
	}
	return "🟢 "
}

func renderScanLine(items []ScanItem) string {
	if len(items) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s (<b>%s</b>/%.0f)", strings.ToUpper(strings.TrimSpace(it.Symbol)), strings.TrimSpace(it.Grade), it.Score))
	}
	return strings.Join(parts, " | ")
}

func BuildEventHTML(icon, title string, lines ...string) string {
	icon = strings.TrimSpace(icon)
	if icon == "" {
		icon = "ℹ️"
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "UPDATE"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s <b>%s</b>", icon, title)
	for _, l := range lines {
		s := strings.TrimSpace(l)
		if s == "" {
			continue
		}
		fmt.Fprintf(&b, "\n• %s", s)
	}
	return strings.TrimSpace(b.String())
}
