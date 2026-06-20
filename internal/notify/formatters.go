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
	OpenCap   int
	NetDayPct float64
}

type PositionCard struct {
	Symbol           string
	Side             string
	Source           string
	Status           string
	ManageState      string
	Managed          bool
	Protected        bool
	NextAction       string
	Qty              float64
	EntryPrice       float64
	MarkPrice        float64
	LastPrice        float64
	SpreadBps        float64
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
	Symbol    string
	Side      string
	Grade     string
	Score     float64
	Slope     float64
	State     string
	Price     float64
	DayUTC    float64
	UTC4h     float64
	UTC1h     float64
	VolumeUSD float64
}

const (
	ManageStateDetected            = "DETECTED"
	ManageStateAwaitingOperator    = "AWAITING_OPERATOR"
	ManageStateAdopted             = "ADOPTED"
	ManageStateAttachingProtection = "ATTACHING_PROTECTION"
	ManageStateProtected           = "PROTECTED"
	ManageStateDegraded            = "DEGRADED"
	ManageStateForceCloseTriggered = "FORCE_CLOSE_TRIGGERED"
)

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
			"• <b>Realized:</b> %+.2f | <b>Open PnL:</b> %+.2f (%s)",
		title, strings.ToUpper(strings.TrimSpace(p.Session)), strings.TrimSpace(p.TimeLabel),
		p.Balance, p.Equity, dayEmoji, p.NetDay, p.NetDayPct, p.Realized, p.OpenPnL, openPosLabel(p.OpenCount, p.OpenCap),
	))
}

func BuildPositionCard(p PositionCard) string {
	direction := "🟢 LONG"
	if strings.EqualFold(strings.TrimSpace(p.Side), "SELL") || strings.EqualFold(strings.TrimSpace(p.Side), "SHORT") {
		direction = "🔴 SHORT"
	}
	statusBanner := ""
	if p.Managed && !p.Protected {
		statusBanner = "<b>🟥 UNPROTECTED MANAGED TRADE</b>\n"
	} else if p.Managed && p.Protected {
		statusBanner = "<b>🟢 MANAGED + PROTECTED</b>\n"
	}
	setup := strings.TrimSpace(p.Setup)
	if setup == "" {
		setup = "none"
	}
	if p.Leverage <= 0 {
		p.Leverage = 1
	}
	priceLine := fmt.Sprintf("• <b>Price:</b> %.4f → %.4f (<b>%+.2f%%</b>)", p.EntryPrice, p.MarkPrice, p.UnrealizedPnLPct)
	if p.LastPrice > 0 {
		priceLine = fmt.Sprintf("• <b>Price:</b> %.4f → %.4f | <b>Last:</b> %.4f (<b>%+.2f%%</b>)", p.EntryPrice, p.MarkPrice, p.LastPrice, p.UnrealizedPnLPct)
	}
	pnlLine := fmt.Sprintf("• <b>PnL:</b> %s$%.2f | <b>Lev:</b> %dx", pnlEmoji(p.UnrealizedPnL), p.UnrealizedPnL, p.Leverage)
	if p.Qty > 0 {
		pnlLine = fmt.Sprintf("• <b>PnL:</b> %s$%.2f | <b>Qty:</b> %.6f | <b>Lev:</b> %dx", pnlEmoji(p.UnrealizedPnL), p.UnrealizedPnL, p.Qty, p.Leverage)
	}
	setupLine := fmt.Sprintf("• <b>Setup:</b> <code>%s</code> | <b>Age:</b> %dm", setup, p.AgeMin)
	if strings.TrimSpace(p.Source) != "" || strings.TrimSpace(p.Status) != "" || p.SpreadBps > 0 {
		parts := make([]string, 0, 5)
		managed := "NO"
		if p.Managed {
			managed = "YES"
		}
		exchangeStop := "NO"
		if p.Protected {
			exchangeStop = "LIVE"
		}
		if strings.TrimSpace(p.Source) != "" {
			parts = append(parts, fmt.Sprintf("<b>Src:</b> %s", strings.ToUpper(strings.TrimSpace(p.Source))))
		}
		if strings.TrimSpace(p.ManageState) != "" {
			parts = append(parts, fmt.Sprintf("<b>Manage:</b> %s", strings.ToUpper(strings.TrimSpace(p.ManageState))))
		}
		parts = append(parts, fmt.Sprintf("<b>Managed:</b> %s", managed))
		parts = append(parts, fmt.Sprintf("<b>Exchange Stop:</b> %s", exchangeStop))
		if strings.TrimSpace(p.Status) != "" {
			parts = append(parts, fmt.Sprintf("<b>Protection:</b> %s", strings.ToUpper(strings.TrimSpace(p.Status))))
		}
		if p.SpreadBps > 0 {
			parts = append(parts, fmt.Sprintf("<b>Spread:</b> %.1fbps", p.SpreadBps))
		}
		setupLine = setupLine + " | " + strings.Join(parts, " | ")
	}
	if strings.TrimSpace(p.NextAction) != "" {
		setupLine = setupLine + " | " + fmt.Sprintf("<b>Next:</b> %s", p.NextAction)
	}
	return strings.TrimSpace(fmt.Sprintf(
		"%s<b>📦 ACTIVE: %s (%s)</b>\n"+
			"%s\n"+
			"%s\n"+
			"%s\n"+
			"• <b>Safety:</b> SL: %.4f | TP: %.4f",
		statusBanner,
		strings.ToUpper(strings.TrimSpace(p.Symbol)), direction,
		priceLine, pnlLine, setupLine, p.StopLoss, p.TakeProfit,
	))
}

func BuildScannerSnapshotHTML(longs, shorts []ScanItem, bias string) string {
	return strings.TrimSpace(fmt.Sprintf(
		"<b>📡 TOP SCANS</b>\n"+
			"%s\n"+
			"%s\n"+
			"⚡ <b>Bias:</b> %s",
		renderScanSectionCompact("LONG", longs, 3),
		renderScanSectionCompact("SHORT", shorts, 3),
		biasLabel(bias),
	))
}

func BuildManagementStatusCard(state, symbol, side string, lines ...string) string {
	icon, title := managementStateHeader(state)
	headline := strings.TrimSpace(fmt.Sprintf("%s <b>%s</b>", icon, title))
	var b strings.Builder
	b.WriteString(headline)
	if strings.TrimSpace(symbol) != "" {
		s := strings.ToUpper(strings.TrimSpace(symbol))
		dir := strings.ToUpper(strings.TrimSpace(side))
		if dir != "" {
			fmt.Fprintf(&b, "\n• <b>Trade:</b> %s %s", s, dir)
		} else {
			fmt.Fprintf(&b, "\n• <b>Trade:</b> %s", s)
		}
	}
	for _, l := range lines {
		s := strings.TrimSpace(l)
		if s == "" {
			continue
		}
		fmt.Fprintf(&b, "\n• %s", s)
	}
	return strings.TrimSpace(b.String())
}

func pnlEmoji(v float64) string {
	if v < 0 {
		return "🔴 "
	}
	return "🟢 "
}

func renderScanSection(items []ScanItem) string {
	if len(items) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(items))
	for i, it := range items {
		side := strings.ToUpper(strings.TrimSpace(it.Side))
		if side == "" {
			side = "?"
		}
		state := strings.ToLower(strings.TrimSpace(it.State))
		if state == "" {
			state = "n/a"
		}
		price := "n/a"
		if it.Price > 0 {
			price = formatScanPrice(it.Price)
		}
		dayUTC := "n/a"
		if it.DayUTC != 0 {
			dayUTC = fmt.Sprintf("%+.1f%%", it.DayUTC)
		}
		utc4h := "n/a"
		if it.UTC4h != 0 {
			utc4h = fmt.Sprintf("%+.1f%%", it.UTC4h)
		}
		utc1h := "n/a"
		if it.UTC1h != 0 {
			utc1h = fmt.Sprintf("%+.1f%%", it.UTC1h)
		}
		vol := "n/a"
		if it.VolumeUSD > 0 {
			vol = humanUSD(it.VolumeUSD)
		}
		parts = append(parts, fmt.Sprintf(
			"%d) <b>%s</b> [%s] g=<b>%s</b> score=<b>%.0f</b> slope=%+.3f state=%s px=%s day=%s 4h=%s 1h=%s vol=%s",
			i+1,
			shortSymbol(it.Symbol),
			side,
			strings.TrimSpace(it.Grade),
			it.Score,
			it.Slope,
			state,
			price,
			dayUTC,
			utc4h,
			utc1h,
			vol,
		))
	}
	return strings.Join(parts, "\n")
}

func renderScanSectionCompact(label string, items []ScanItem, topN int) string {
	if topN <= 0 {
		topN = 3
	}
	if len(items) == 0 {
		return fmt.Sprintf("• <b>%s:</b> (none)", strings.ToUpper(strings.TrimSpace(label)))
	}
	n := len(items)
	if n > topN {
		n = topN
	}
	parts := make([]string, 0, n+1)
	parts = append(parts, fmt.Sprintf("• <b>%s:</b>", strings.ToUpper(strings.TrimSpace(label))))
	for i := 0; i < n; i++ {
		it := items[i]
		price := "n/a"
		if it.Price > 0 {
			price = formatScanPrice(it.Price)
		}
		dayUTC := "n/a"
		if it.DayUTC != 0 {
			dayUTC = fmt.Sprintf("%+.1f%%", it.DayUTC)
		}
		utc4h := "n/a"
		if it.UTC4h != 0 {
			utc4h = fmt.Sprintf("%+.1f%%", it.UTC4h)
		}
		utc1h := "n/a"
		if it.UTC1h != 0 {
			utc1h = fmt.Sprintf("%+.1f%%", it.UTC1h)
		}
		parts = append(parts, fmt.Sprintf("  %d) <b>%s</b> g=<b>%s</b> s=<b>%.0f</b> st=%s px=%s day=%s 4h=%s 1h=%s",
			i+1,
			shortSymbol(it.Symbol),
			strings.TrimSpace(it.Grade),
			it.Score,
			strings.ToLower(strings.TrimSpace(it.State)),
			price,
			dayUTC,
			utc4h,
			utc1h,
		))
	}
	return strings.Join(parts, "\n")
}

func managementStateHeader(state string) (string, string) {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case ManageStateDetected:
		return "🔎", "DETECTED"
	case ManageStateAwaitingOperator:
		return "🟡", "AWAITING OPERATOR"
	case ManageStateAdopted:
		return "🤝", "ADOPTED"
	case ManageStateAttachingProtection:
		return "🛠️", "ATTACHING PROTECTION"
	case ManageStateProtected:
		return "🟢", "PROTECTED"
	case ManageStateDegraded:
		return "🟥", "DEGRADED (UNPROTECTED)"
	case ManageStateForceCloseTriggered:
		return "🛑", "FORCE CLOSE TRIGGERED"
	default:
		return "ℹ️", strings.ToUpper(strings.TrimSpace(state))
	}
}

func openPosLabel(openCount, openCap int) string {
	if openCap > 0 {
		return fmt.Sprintf("%d/%d pos", openCount, openCap)
	}
	return fmt.Sprintf("%d pos", openCount)
}

func shortSymbol(sym string) string {
	s := strings.ToUpper(strings.TrimSpace(sym))
	s = strings.TrimSuffix(s, "USDT")
	s = strings.TrimSuffix(s, "-USD")
	if s == "" {
		return strings.ToUpper(strings.TrimSpace(sym))
	}
	return s
}

func biasLabel(b string) string {
	switch strings.ToUpper(strings.TrimSpace(b)) {
	case "LONG":
		return "🟢 LONG"
	case "SHORT":
		return "🔴 SHORT"
	default:
		return "🟡 NEUTRAL"
	}
}

func formatScanPrice(v float64) string {
	switch {
	case v >= 1000:
		return fmt.Sprintf("%.2f", v)
	case v >= 1:
		return fmt.Sprintf("%.4f", v)
	default:
		return fmt.Sprintf("%.6f", v)
	}
}

func humanUSD(v float64) string {
	av := v
	if av < 0 {
		av = -av
	}
	switch {
	case av >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", v/1_000_000_000)
	case av >= 1_000_000:
		return fmt.Sprintf("%.2fM", v/1_000_000)
	case av >= 1_000:
		return fmt.Sprintf("%.2fK", v/1_000)
	default:
		return fmt.Sprintf("%.0f", v)
	}
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
