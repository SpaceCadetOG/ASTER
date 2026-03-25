package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/notify"
)

type AccountSummary struct {
	Timestamp         time.Time `json:"timestamp"`
	PerpEquity        float64   `json:"perpEquity"`
	PerpWalletBalance float64   `json:"perpWalletBalance"`
	PerpAvailable     float64   `json:"perpAvailable"`
	PerpUnrealizedPnL float64   `json:"perpUnrealizedPnL"`
	PerpRealizedPnL   float64   `json:"perpRealizedPnL"`
	SpotEquity        float64   `json:"spotEquity"`
	TotalEquity       float64   `json:"totalEquity"`
	MarginUsed        float64   `json:"marginUsed"`
	OpenPositions     int       `json:"openPositions"`
	OpenOrders        int       `json:"openOrders"`
	MissingFields     []string  `json:"missingFields,omitempty"`
}

type GrowthWindow struct {
	WindowLabel   string    `json:"windowLabel"`
	StartTime     time.Time `json:"startTime,omitempty"`
	StartEquity   float64   `json:"startEquity,omitempty"`
	CurrentEquity float64   `json:"currentEquity,omitempty"`
	DeltaAbs      float64   `json:"deltaAbs,omitempty"`
	DeltaPct      float64   `json:"deltaPct,omitempty"`
	Available     bool      `json:"available"`
}

type accountFundsSummary struct {
	Enabled            bool    `json:"enabled"`
	PerpTargetUSDT     float64 `json:"perpTargetUsdt"`
	PerpFloorUSDT      float64 `json:"perpFloorUsdt"`
	SweepEnabled       bool    `json:"sweepEnabled"`
	LastTransferStatus string  `json:"lastTransferStatus,omitempty"`
}

type accountSnapshotRecord struct {
	AccountSummary
	Funds accountFundsSummary `json:"funds"`
}

type accountReport struct {
	Generated    time.Time           `json:"generated"`
	Summary      AccountSummary      `json:"summary"`
	PerpGrowth   []GrowthWindow      `json:"perpGrowth,omitempty"`
	TotalGrowth  []GrowthWindow      `json:"totalGrowth,omitempty"`
	Funds        accountFundsSummary `json:"funds"`
	SnapshotPath string              `json:"snapshotPath,omitempty"`
}

type accountReportConfig struct {
	SnapshotEnable bool
	SnapshotEvery  time.Duration
	GrowthEnable   bool
	IncludeSpot    bool
	IncludeTotal   bool
	RefreshEvery   time.Duration
	SnapshotPath   string
}

func loadAccountReportConfig() accountReportConfig {
	cfg := accountReportConfig{
		SnapshotEnable: envBool("LIVE_ACCOUNT_SNAPSHOT_ENABLE", true),
		SnapshotEvery:  time.Duration(envInt("LIVE_ACCOUNT_SNAPSHOT_SEC", 300)) * time.Second,
		GrowthEnable:   envBool("LIVE_ACCOUNT_GROWTH_ENABLE", true),
		IncludeSpot:    envBool("LIVE_ACCOUNT_REPORT_INCLUDE_SPOT", true),
		IncludeTotal:   envBool("LIVE_ACCOUNT_REPORT_INCLUDE_TOTAL", true),
		RefreshEvery:   time.Duration(envInt("LIVE_ACCOUNT_REFRESH_SEC", 2)) * time.Second,
		SnapshotPath:   resolveStatePath("logs/account_snapshots.jsonl"),
	}
	if cfg.SnapshotEvery <= 0 {
		cfg.SnapshotEvery = 5 * time.Minute
	}
	if cfg.RefreshEvery <= 0 {
		cfg.RefreshEvery = 2 * time.Second
	}
	return cfg
}

func floatFromMap(m map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		v, ok := m[key]
		if !ok {
			continue
		}
		return mapFloat(v), true
	}
	return 0, false
}

func sliceFromMap(m map[string]any, keys ...string) ([]any, bool) {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch arr := v.(type) {
		case []any:
			return arr, true
		case []map[string]any:
			out := make([]any, 0, len(arr))
			for _, row := range arr {
				out = append(out, row)
			}
			return out, true
		}
	}
	return nil, false
}

func mapFromAny(v any) map[string]any {
	row, _ := v.(map[string]any)
	return row
}

func fetchNormalizedAccountSummary(rest *aster.RESTAuth, transferManager TransferManager) (AccountSummary, error) {
	now := time.Now().UTC()
	out := AccountSummary{Timestamp: now}
	missing := map[string]struct{}{}
	acct, acctErr := rest.GetAccountSummary()
	if acctErr != nil {
		missing["perp_account_summary"] = struct{}{}
	}

	perpEquity, havePerpEquity := floatFromMap(acct, "totalMarginBalance", "marginBalance")
	perpWallet, havePerpWallet := floatFromMap(acct, "totalWalletBalance", "walletBalance")
	perpAvailable, havePerpAvail := floatFromMap(acct, "availableBalance", "maxWithdrawAmount")
	perpUnreal, havePerpUnreal := floatFromMap(acct, "totalUnrealizedProfit", "totalCrossUnPnl", "unrealizedProfit")
	perpRealized, havePerpRealized := floatFromMap(acct, "totalRealizedProfit", "totalRealizedPnL", "realizedProfit")
	marginUsed, haveMarginUsed := floatFromMap(acct, "totalInitialMargin", "totalPositionInitialMargin")
	if openOrderMargin, ok := floatFromMap(acct, "totalOpenOrderInitialMargin", "openOrderInitialMargin"); ok {
		marginUsed += openOrderMargin
		haveMarginUsed = true
	}
	totalEquity, haveTotalEquity := floatFromMap(acct, "totalEquity")
	haveOpenPositions := false
	if positionsArr, ok := sliceFromMap(acct, "positions"); ok {
		haveOpenPositions = true
		for _, raw := range positionsArr {
			row := mapFromAny(raw)
			if abs(mapFloat(row["positionAmt"])) <= 1e-10 {
				continue
			}
			out.OpenPositions++
		}
	}

	var bals []aster.Balance
	var balErr error
	if !havePerpWallet || !havePerpAvail || !havePerpUnreal {
		bals, balErr = rest.GetBalance()
		if balErr != nil {
			missing["perp_balance"] = struct{}{}
		}
	}

	if balErr == nil {
		for _, b := range bals {
			if !strings.EqualFold(strings.TrimSpace(b.Asset), "USDT") {
				continue
			}
			if !havePerpWallet {
				perpWallet = b.Balance
				havePerpWallet = true
			}
			if !havePerpAvail {
				perpAvailable = b.AvailableBalance
				havePerpAvail = true
			}
			if !havePerpUnreal {
				perpUnreal = b.CrossUnPnl
				havePerpUnreal = true
			}
			break
		}
	}

	var rows []map[string]any
	var posErr error
	if !haveOpenPositions || !havePerpUnreal || !haveMarginUsed {
		rows, posErr = rest.PositionRisk("")
		if posErr != nil {
			missing["position_risk"] = struct{}{}
		}
	}

	if posErr == nil {
		unrealFromPositions := 0.0
		marginFromPositions := 0.0
		openPositions := 0
		for _, row := range rows {
			amt := mapFloat(row["positionAmt"])
			if abs(amt) <= 1e-10 {
				continue
			}
			openPositions++
			unrealFromPositions += mapFloat(row["unRealizedProfit"])
			margin := mapFloat(row["isolatedWallet"])
			if margin <= 0 {
				margin = mapFloat(row["positionInitialMargin"])
			}
			marginFromPositions += maxFloat(margin, 0)
		}
		if !haveOpenPositions {
			out.OpenPositions = openPositions
		}
		if !havePerpUnreal {
			perpUnreal = unrealFromPositions
			havePerpUnreal = true
		}
		if !haveMarginUsed && marginFromPositions > 0 {
			marginUsed = marginFromPositions
			haveMarginUsed = true
		}
	}

	orders, ordersErr := rest.OpenOrders("")
	if ordersErr != nil {
		missing["open_orders"] = struct{}{}
	}
	if acctErr != nil && balErr != nil && posErr != nil && ordersErr != nil {
		return out, fmt.Errorf("account summary unavailable: acct=%v balance=%v positions=%v orders=%v", acctErr, balErr, posErr, ordersErr)
	}

	if ordersErr == nil {
		out.OpenOrders = len(orders)
	}

	spotEquity := 0.0
	haveSpot := false
	if transferManager != nil && transferManager.Supported() {
		if spotAvail, err := transferManager.SpotAvailableUSDT(); err == nil {
			spotEquity = maxFloat(spotAvail, 0)
			haveSpot = true
		} else {
			missing["spot_equity"] = struct{}{}
		}
	} else {
		missing["spot_equity"] = struct{}{}
	}

	if !havePerpEquity && havePerpWallet {
		perpEquity = perpWallet + perpUnreal
		havePerpEquity = true
	}
	if !haveTotalEquity && havePerpEquity && haveSpot {
		totalEquity = perpEquity + spotEquity
		haveTotalEquity = true
	}

	out.PerpEquity = perpEquity
	out.PerpWalletBalance = perpWallet
	out.PerpAvailable = perpAvailable
	out.PerpUnrealizedPnL = perpUnreal
	out.PerpRealizedPnL = perpRealized
	out.SpotEquity = spotEquity
	out.TotalEquity = totalEquity
	out.MarginUsed = marginUsed

	if !havePerpEquity {
		missing["perp_equity"] = struct{}{}
	}
	if !havePerpWallet {
		missing["perp_wallet_balance"] = struct{}{}
	}
	if !havePerpAvail {
		missing["perp_available"] = struct{}{}
	}
	if !havePerpUnreal {
		missing["perp_unrealized_pnl"] = struct{}{}
	}
	if !havePerpRealized {
		missing["perp_realized_pnl"] = struct{}{}
	}
	if !haveMarginUsed {
		missing["margin_used"] = struct{}{}
	}
	if !haveTotalEquity {
		missing["total_equity"] = struct{}{}
	}
	if out.OpenOrders == 0 && ordersErr != nil {
		missing["open_orders"] = struct{}{}
	}
	out.MissingFields = missingFieldsList(missing)
	return out, nil
}

func missingFieldsList(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for field := range m {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

func accountFundsState(m *liveExecManager) accountFundsSummary {
	if m == nil {
		return accountFundsSummary{}
	}
	return accountFundsSummary{
		Enabled:            m.fundsCfg.Enable,
		PerpTargetUSDT:     m.fundsCfg.PerpTargetUSDT,
		PerpFloorUSDT:      m.fundsCfg.PerpFloorUSDT,
		SweepEnabled:       m.fundsCfg.SweepProfitEnable,
		LastTransferStatus: strings.TrimSpace(m.lastTransferStatus),
	}
}

func appendAccountSnapshot(path string, rec accountSnapshotRecord) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func loadAccountSnapshots(path string) ([]accountSnapshotRecord, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	out := make([]accountSnapshotRecord, 0, 256)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec accountSnapshotRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Timestamp.IsZero() {
			continue
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out, nil
}

func nearestEarlierSnapshot(records []accountSnapshotRecord, target time.Time, selector func(accountSnapshotRecord) float64) (accountSnapshotRecord, bool) {
	var best accountSnapshotRecord
	found := false
	for _, rec := range records {
		if rec.Timestamp.After(target) {
			break
		}
		if selector(rec) <= 0 {
			continue
		}
		best = rec
		found = true
	}
	return best, found
}

func computeGrowthWindows(now time.Time, records []accountSnapshotRecord, current accountSnapshotRecord, selector func(accountSnapshotRecord) float64) []GrowthWindow {
	specs := []struct {
		label string
		dur   time.Duration
	}{
		{label: "24h", dur: 24 * time.Hour},
		{label: "7d", dur: 7 * 24 * time.Hour},
		{label: "30d", dur: 30 * 24 * time.Hour},
	}
	out := make([]GrowthWindow, 0, len(specs))
	currentEquity := selector(current)
	for _, spec := range specs {
		win := GrowthWindow{
			WindowLabel:   spec.label,
			CurrentEquity: currentEquity,
		}
		start, ok := nearestEarlierSnapshot(records, now.Add(-spec.dur), selector)
		if ok && currentEquity > 0 {
			win.Available = true
			win.StartTime = start.Timestamp
			win.StartEquity = selector(start)
			win.DeltaAbs = currentEquity - win.StartEquity
			if win.StartEquity > 0 {
				win.DeltaPct = (win.DeltaAbs / win.StartEquity) * 100.0
			}
		}
		out = append(out, win)
	}
	return out
}

func (m *liveExecManager) recordTransferStatus(status string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastTransferStatus = strings.TrimSpace(status)
}

func (m *liveExecManager) AccountReportSnapshot() accountReport {
	if m == nil {
		return accountReport{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.accountReport
}

func (m *liveExecManager) ensureAccountReportFresh(now time.Time, maxAge time.Duration) accountReport {
	if m == nil {
		return accountReport{}
	}
	report := m.AccountReportSnapshot()
	if !report.Generated.IsZero() && maxAge > 0 && now.Sub(report.Generated) <= maxAge {
		return report
	}
	m.refreshAccountReport(now, false)
	return m.AccountReportSnapshot()
}

func (m *liveExecManager) runAccountReportingLoop() {
	if m == nil || m.rest == nil {
		return
	}
	m.refreshAccountReport(time.Now().UTC(), m.accountReportCfg.SnapshotEnable)
	ticker := time.NewTicker(m.accountReportCfg.SnapshotEvery)
	defer ticker.Stop()
	for now := range ticker.C {
		m.refreshAccountReport(now.UTC(), m.accountReportCfg.SnapshotEnable)
	}
}

func (m *liveExecManager) refreshAccountReport(now time.Time, persist bool) {
	if m == nil || m.rest == nil {
		return
	}
	summary, err := fetchNormalizedAccountSummary(m.rest, m.transferManager)
	if err != nil {
		fmt.Printf("live-lite: account report refresh error: %v\n", err)
		return
	}
	summary.Timestamp = now.UTC()
	rec := accountSnapshotRecord{
		AccountSummary: summary,
		Funds:          accountFundsState(m),
	}
	if persist && m.accountReportCfg.SnapshotEnable {
		if err := appendAccountSnapshot(m.accountReportCfg.SnapshotPath, rec); err != nil {
			fmt.Printf("live-lite: account snapshot write error: %v\n", err)
		}
	}
	records, err := loadAccountSnapshots(m.accountReportCfg.SnapshotPath)
	if err != nil {
		fmt.Printf("live-lite: account snapshot load error: %v\n", err)
	}
	if len(records) == 0 || records[len(records)-1].Timestamp.Before(rec.Timestamp) {
		records = append(records, rec)
	}
	report := accountReport{
		Generated:    now.UTC(),
		Summary:      summary,
		Funds:        rec.Funds,
		SnapshotPath: m.accountReportCfg.SnapshotPath,
	}
	if m.accountReportCfg.GrowthEnable {
		report.PerpGrowth = computeGrowthWindows(now.UTC(), records, rec, func(r accountSnapshotRecord) float64 {
			return r.PerpEquity
		})
		if m.accountReportCfg.IncludeTotal {
			report.TotalGrowth = computeGrowthWindows(now.UTC(), records, rec, func(r accountSnapshotRecord) float64 {
				return r.TotalEquity
			})
		}
	}
	m.mu.Lock()
	m.accountReport = report
	m.mu.Unlock()
	if len(summary.MissingFields) > 0 {
		fmt.Printf("live-lite: account summary missing=%s\n", strings.Join(summary.MissingFields, ","))
	}
}

func formatAccountValue(v float64, missing bool) string {
	if missing {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f", v)
}

func hasMissingField(summary AccountSummary, field string) bool {
	for _, missing := range summary.MissingFields {
		if missing == field {
			return true
		}
	}
	return false
}

func growthLine(window GrowthWindow) string {
	if !window.Available {
		return fmt.Sprintf("%s: insufficient history", window.WindowLabel)
	}
	return fmt.Sprintf("%s: %+.2f / %+.2f%%", window.WindowLabel, window.DeltaAbs, window.DeltaPct)
}

func buildAccountSummaryText(report accountReport, includeGrowth bool) string {
	if report.Generated.IsZero() {
		return ""
	}
	lines := []string{
		"ACCOUNT SUMMARY",
		fmt.Sprintf("- Perp Equity: %s", formatAccountValue(report.Summary.PerpEquity, hasMissingField(report.Summary, "perp_equity"))),
		fmt.Sprintf("- Perp Available: %s", formatAccountValue(report.Summary.PerpAvailable, hasMissingField(report.Summary, "perp_available"))),
		fmt.Sprintf("- Perp Wallet: %s", formatAccountValue(report.Summary.PerpWalletBalance, hasMissingField(report.Summary, "perp_wallet_balance"))),
		fmt.Sprintf("- UPNL: %+.2f", report.Summary.PerpUnrealizedPnL),
		fmt.Sprintf("- Margin Used: %s", formatAccountValue(report.Summary.MarginUsed, hasMissingField(report.Summary, "margin_used"))),
		fmt.Sprintf("- Open Positions: %d", report.Summary.OpenPositions),
		fmt.Sprintf("- Open Orders: %d", report.Summary.OpenOrders),
	}
	if report.Funds.Enabled {
		lines = append(lines,
			fmt.Sprintf("- Perp Target/Floor: %.2f / %.2f", report.Funds.PerpTargetUSDT, report.Funds.PerpFloorUSDT),
			fmt.Sprintf("- Profit Sweep: %v", report.Funds.SweepEnabled),
		)
		if strings.TrimSpace(report.Funds.LastTransferStatus) != "" {
			lines = append(lines, fmt.Sprintf("- Last Transfer: %s", report.Funds.LastTransferStatus))
		}
	}
	if envBool("LIVE_ACCOUNT_REPORT_INCLUDE_SPOT", true) && !hasMissingField(report.Summary, "spot_equity") {
		lines = append(lines, fmt.Sprintf("- Spot Equity: %s", formatAccountValue(report.Summary.SpotEquity, false)))
	}
	if envBool("LIVE_ACCOUNT_REPORT_INCLUDE_TOTAL", true) && !hasMissingField(report.Summary, "total_equity") {
		lines = append(lines, fmt.Sprintf("- Total Equity: %s", formatAccountValue(report.Summary.TotalEquity, false)))
	}
	if includeGrowth && len(report.PerpGrowth) > 0 {
		lines = append(lines, "", "GROWTH")
		for _, window := range report.PerpGrowth {
			lines = append(lines, "- "+growthLine(window))
		}
	}
	return strings.Join(lines, "\n")
}

func buildAccountHTML(report accountReport, growthOnly bool, includeMissed []string) string {
	if report.Generated.IsZero() {
		title := "ACCOUNT SUMMARY"
		icon := "💼"
		if growthOnly {
			title = "ACCOUNT GROWTH"
			icon = "📈"
		}
		return notify.BuildEventHTML(icon, title, "Account summary unavailable")
	}
	lines := []string{}
	if !growthOnly {
		lines = append(lines,
			fmt.Sprintf("<b>Perp Equity:</b> %s", formatAccountValue(report.Summary.PerpEquity, hasMissingField(report.Summary, "perp_equity"))),
			fmt.Sprintf("<b>Perp Wallet:</b> %s", formatAccountValue(report.Summary.PerpWalletBalance, hasMissingField(report.Summary, "perp_wallet_balance"))),
			fmt.Sprintf("<b>Perp Available:</b> %s", formatAccountValue(report.Summary.PerpAvailable, hasMissingField(report.Summary, "perp_available"))),
			fmt.Sprintf("<b>UPNL:</b> %+.2f", report.Summary.PerpUnrealizedPnL),
			fmt.Sprintf("<b>Margin Used:</b> %s", formatAccountValue(report.Summary.MarginUsed, hasMissingField(report.Summary, "margin_used"))),
			fmt.Sprintf("<b>Open Positions:</b> %d", report.Summary.OpenPositions),
			fmt.Sprintf("<b>Open Orders:</b> %d", report.Summary.OpenOrders),
		)
		if envBool("LIVE_ACCOUNT_REPORT_INCLUDE_SPOT", true) && !hasMissingField(report.Summary, "spot_equity") {
			lines = append(lines, fmt.Sprintf("<b>Spot Equity:</b> %s", formatAccountValue(report.Summary.SpotEquity, false)))
		}
		if envBool("LIVE_ACCOUNT_REPORT_INCLUDE_TOTAL", true) && !hasMissingField(report.Summary, "total_equity") {
			lines = append(lines, fmt.Sprintf("<b>Total Equity:</b> %s", formatAccountValue(report.Summary.TotalEquity, false)))
		}
		if report.Funds.Enabled {
			lines = append(lines,
				fmt.Sprintf("<b>Perp Target/Floor:</b> %.2f / %.2f", report.Funds.PerpTargetUSDT, report.Funds.PerpFloorUSDT),
				fmt.Sprintf("<b>Profit Sweep:</b> %v", report.Funds.SweepEnabled),
			)
			if strings.TrimSpace(report.Funds.LastTransferStatus) != "" {
				lines = append(lines, fmt.Sprintf("<b>Last Transfer:</b> %s", report.Funds.LastTransferStatus))
			}
		}
	}
	lines = append(lines, "<b>Growth:</b>")
	for _, window := range report.PerpGrowth {
		lines = append(lines, growthLine(window))
	}
	if len(report.TotalGrowth) > 0 {
		lines = append(lines, "<b>Total Growth:</b>")
		for _, window := range report.TotalGrowth {
			lines = append(lines, growthLine(window))
		}
	}
	for _, line := range includeMissed {
		lines = append(lines, line)
	}
	title := "ACCOUNT SUMMARY"
	icon := "💼"
	if growthOnly {
		title = "ACCOUNT GROWTH"
		icon = "📈"
	}
	return notify.BuildEventHTML(icon, title, lines...)
}

func compactAccountSummaryLine(report accountReport) string {
	if report.Generated.IsZero() {
		return "Account: unavailable"
	}
	fields := []string{}
	if !hasMissingField(report.Summary, "perp_equity") {
		fields = append(fields, fmt.Sprintf("Perp Eq %.2f", report.Summary.PerpEquity))
	}
	if !hasMissingField(report.Summary, "perp_available") {
		fields = append(fields, fmt.Sprintf("Avail %.2f", report.Summary.PerpAvailable))
	}
	if !hasMissingField(report.Summary, "perp_unrealized_pnl") {
		fields = append(fields, fmt.Sprintf("UPNL %+.2f", report.Summary.PerpUnrealizedPnL))
	}
	fields = append(fields, fmt.Sprintf("Pos %d", report.Summary.OpenPositions))
	fields = append(fields, fmt.Sprintf("Ord %d", report.Summary.OpenOrders))
	if len(fields) == 0 {
		return "Account: unavailable"
	}
	return "Account: " + strings.Join(fields, " | ")
}

func (m *liveExecManager) AccountDigestSection() string {
	report := m.AccountReportSnapshot()
	if report.Generated.IsZero() {
		return ""
	}
	return buildAccountSummaryText(report, true)
}
