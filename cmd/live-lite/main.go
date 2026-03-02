package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/inplay"
	"go-machine/internal/market"
	"go-machine/internal/ta"
	"go-machine/internal/types"
)

type candidate struct {
	Entry inplay.Entry
	Side  string // BUY/SELL
}

type positionView struct {
	Symbol   string
	Side     string
	SizeAbs  float64
	Entry    float64
	Mark     float64
	Unreal   float64
	Leverage float64
}

type accountSnapshot struct {
	AvailableUSDT float64
	Balances      []aster.Balance
	Positions     []positionView
}

type telegramSink struct {
	enabled  bool
	token    string
	chatID   string
	timeout  time.Duration
	dedupe   time.Duration
	client   *http.Client
	lastSent map[string]time.Time
}

func main() {
	scanEvery := time.Duration(envInt("LIVE_SCAN_SEC", 30)) * time.Second
	dryRun := envBool("LIVE_DRY_RUN", true)
	minGrade := envStr("LIVE_MIN_GRADE", "B")
	reserveUSDT := envFloat("LIVE_RESERVE_USDT", 5)
	tradeMargin := envFloat("LIVE_TRADE_MARGIN_USDT", 10)
	entryBps := envFloat("LIVE_ENTRY_OFFSET_BPS", 2)
	showAccount := envBool("LIVE_SHOW_ACCOUNT", true)
	accountAssets := envCSV("LIVE_ACCOUNT_ASSETS", "USDT")
	if entryBps < 0 {
		entryBps = -entryBps
	}
	gradeTopN := envInt("LIVE_GRADE_TOP_N", 6)
	if gradeTopN <= 0 {
		gradeTopN = 6
	}

	inplayCfg := inplay.Config{
		MinGrade:       envStr("INPLAY_MIN_GRADE", "C"),
		MinVolumeUSD:   envFloat("INPLAY_MIN_VOL_USD", 1_000_000),
		HistoryN:       envInt("INPLAY_HISTORY_N", 5),
		RiseN:          envInt("INPLAY_RISE_N", 3),
		DropGradeScans: envInt("INPLAY_DROP_SCANS", 2),
		FallScans:      envInt("INPLAY_FALL_SCANS", 2),
		TTL:            time.Duration(envInt("INPLAY_TTL_MIN", 30)) * time.Minute,
	}
	longTrk := inplay.NewTracker("long", inplayCfg)
	shortTrk := inplay.NewTracker("short", inplayCfg)

	client := aster.New("")
	rest := buildRESTFromConfig()
	if rest == nil {
		fmt.Println("live-lite: no credentials found, forcing DRY_RUN mode")
		dryRun = true
	}
	tg := newTelegramSink()

	fmt.Printf("live-lite started (scan=%s dry_run=%v min_grade=%s reserve=$%.2f margin=$%.2f)\n",
		scanEvery, dryRun, strings.ToUpper(minGrade), reserveUSDT, tradeMargin)
	tg.Sendf("live-lite started scan=%s dry_run=%v min_grade=%s", scanEvery, dryRun, strings.ToUpper(minGrade))

	tk := time.NewTicker(scanEvery)
	defer tk.Stop()
	lastTopKey := ""
	for {
		now := time.Now().UTC()
		mkts := client.FetchAllMarkets()
		longRows := market.ScoreAndFilter(mkts)
		shortRows := market.ScoreAndFilterShort(mkts)

		longEligible, longConf := buildEligible(client, longRows, "long", gradeTopN)
		shortEligible, shortConf := buildEligible(client, shortRows, "short", gradeTopN)

		longTrk.Update(now, longEligible, longConf)
		shortTrk.Update(now, shortEligible, shortConf)

		longInPlay := longTrk.Entries()
		shortInPlay := shortTrk.Entries()
		printInPlay("LONG", longInPlay)
		printInPlay("SHORT", shortInPlay)

		var acct accountSnapshot
		if rest != nil && showAccount {
			snap, err := fetchAccountSnapshot(rest, accountAssets)
			if err != nil {
				fmt.Println("live-lite: account snapshot error:", err)
			} else {
				acct = snap
				printAccountSnapshot(snap, accountAssets)
			}
		}

		cands := chooseCandidates(longInPlay, shortInPlay, minGrade)
		if len(cands) == 0 {
			fmt.Println("live-lite: no trade candidate")
			<-tk.C
			continue
		}
		best := cands[0]
		fmt.Printf("live-lite: top candidate %s side=%s grade=%s score=%.2f slope=%.3f rank=%.2f\n",
			best.Entry.Symbol, best.Side, best.Entry.CurrentGrade, best.Entry.CurrentScore, best.Entry.ScoreSlope, best.Entry.Rank)
		topKey := fmt.Sprintf("%s|%s|%s", best.Entry.Symbol, best.Side, best.Entry.CurrentGrade)
		if topKey != lastTopKey {
			tg.Sendf("top candidate %s %s grade=%s score=%.2f slope=%.3f rank=%.2f",
				strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, best.Entry.CurrentGrade,
				best.Entry.CurrentScore, best.Entry.ScoreSlope, best.Entry.Rank)
			lastTopKey = topKey
		}

		if rest == nil || dryRun {
			printTradeIntent(best, entryBps, tradeMargin)
			tg.Sendf("DRY_RUN intent %s %s margin=$%.2f grade=%s score=%.2f",
				strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, tradeMargin, best.Entry.CurrentGrade, best.Entry.CurrentScore)
			<-tk.C
			continue
		}

		avail := acct.AvailableUSDT
		if avail <= 0 {
			var err error
			avail, err = availableUSDT(rest)
			if err != nil {
				fmt.Println("live-lite: balance error:", err)
				<-tk.C
				continue
			}
		}
		usable := avail - reserveUSDT
		if usable < tradeMargin {
			fmt.Printf("live-lite: skip (available %.4f, usable %.4f < margin %.4f)\n", avail, usable, tradeMargin)
			tg.Sendf("skip %s %s: usable %.4f < margin %.4f",
				strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, usable, tradeMargin)
			<-tk.C
			continue
		}

		openPos := len(acct.Positions) > 0
		if !showAccount || len(acct.Positions) == 0 {
			var err error
			openPos, err = hasAnyOpenPosition(rest)
			if err != nil {
				fmt.Println("live-lite: position check error:", err)
				<-tk.C
				continue
			}
		}
		if openPos {
			fmt.Println("live-lite: skip (position already open, max one position)")
			tg.Sendf("skip %s %s: existing open position", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side)
			<-tk.C
			continue
		}

		if err := placeEntry(rest, best, entryBps, tradeMargin); err != nil {
			fmt.Println("live-lite: place error:", err)
			tg.Sendf("place error %s %s: %v", strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, err)
		} else {
			tg.Sendf("placed %s %s margin=$%.2f grade=%s",
				strings.ToUpper(aster.RawSymbol(best.Entry.Symbol)), best.Side, tradeMargin, best.Entry.CurrentGrade)
		}

		<-tk.C
	}
}

func buildEligible(c *aster.Client, rows []market.Scored, side string, gradeTopN int) ([]market.Scored, map[string]string) {
	out := make([]market.Scored, 0, len(rows))
	conf := make(map[string]string, len(rows))
	eligibleByScore := make([]market.Scored, 0, len(rows))
	for _, r := range rows {
		if !r.Eligible {
			continue
		}
		out = append(out, r)
		eligibleByScore = append(eligibleByScore, r)
		conf[r.Symbol] = market.FallbackGradeDirectional(r.Score, r.Change24h, side)
	}
	sort.Slice(eligibleByScore, func(i, j int) bool { return eligibleByScore[i].Score > eligibleByScore[j].Score })
	for i := 0; i < len(eligibleByScore) && i < gradeTopN; i++ {
		sym := eligibleByScore[i].Symbol
		lbl := confluenceLabel(c, sym, side)
		if lbl == "" || lbl == "_" || lbl == "C" {
			lbl = market.FallbackGradeDirectional(eligibleByScore[i].Score, eligibleByScore[i].Change24h, side)
		}
		conf[sym] = lbl
	}
	return out, conf
}

func chooseCandidates(longInPlay, shortInPlay []inplay.Entry, minGrade string) []candidate {
	minVal := gradeValue(minGrade)
	out := make([]candidate, 0, len(longInPlay)+len(shortInPlay))
	for _, e := range longInPlay {
		if e.State == inplay.StateInPlay && gradeValue(e.CurrentGrade) >= minVal && e.ScoreSlope > 0 {
			out = append(out, candidate{Entry: e, Side: "BUY"})
		}
	}
	for _, e := range shortInPlay {
		if e.State == inplay.StateInPlay && gradeValue(e.CurrentGrade) >= minVal && e.ScoreSlope > 0 {
			out = append(out, candidate{Entry: e, Side: "SELL"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entry.Rank > out[j].Entry.Rank })
	return out
}

func printInPlay(tag string, entries []inplay.Entry) {
	fmt.Printf("🔥 IN-PLAY (%s)\n", tag)
	for i := 0; i < len(entries) && i < 5; i++ {
		e := entries[i]
		fmt.Printf("%d) %-12s grade=%-2s score=%6.2f slope=%6.3f state=%-8s\n",
			i+1, e.Symbol, e.CurrentGrade, e.CurrentScore, e.ScoreSlope, e.State)
	}
	if len(entries) == 0 {
		fmt.Println("(none)")
	}
}

func printTradeIntent(c candidate, entryBps, margin float64) {
	sym := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	lev := 3
	if strings.EqualFold(c.Entry.CurrentGrade, "A+") {
		lev = 5
	}
	fmt.Printf("DRY_RUN intent: symbol=%s side=%s margin=$%.2f leverage=%dx entry=LIMIT(mid %+0.2fbps in direction)\n",
		sym, c.Side, margin, lev, entryBps)
}

func fetchAccountSnapshot(rest *aster.RESTAuth, assets []string) (accountSnapshot, error) {
	snap := accountSnapshot{}
	bals, err := rest.GetBalance()
	if err != nil {
		return snap, err
	}
	snap.Balances = filterBalances(bals, assets)
	for _, b := range bals {
		if strings.EqualFold(strings.TrimSpace(b.Asset), "USDT") {
			snap.AvailableUSDT = b.AvailableBalance
			break
		}
	}
	rows, err := rest.PositionRisk("")
	if err != nil {
		return snap, err
	}
	for _, r := range rows {
		amt := mapFloat(r["positionAmt"])
		if amt == 0 {
			continue
		}
		side := "LONG"
		if amt < 0 {
			side = "SHORT"
		}
		snap.Positions = append(snap.Positions, positionView{
			Symbol:   strings.ToUpper(strings.TrimSpace(fmt.Sprint(r["symbol"]))),
			Side:     side,
			SizeAbs:  abs(amt),
			Entry:    mapFloat(r["entryPrice"]),
			Mark:     mapFloat(r["markPrice"]),
			Unreal:   mapFloat(r["unRealizedProfit"]),
			Leverage: mapFloat(r["leverage"]),
		})
	}
	sort.Slice(snap.Positions, func(i, j int) bool {
		return snap.Positions[i].Unreal > snap.Positions[j].Unreal
	})
	return snap, nil
}

func printAccountSnapshot(snap accountSnapshot, assets []string) {
	fmt.Printf("💼 ACCOUNT availableUSDT=%.4f\n", snap.AvailableUSDT)
	if len(snap.Balances) == 0 {
		fmt.Println("balances: (none matching filter)")
	} else {
		fmt.Println("balances:")
		for _, b := range snap.Balances {
			fmt.Printf("- %-6s bal=%-12.6f avail=%-12.6f upnl=%-10.6f\n",
				strings.ToUpper(b.Asset), b.Balance, b.AvailableBalance, b.CrossUnPnl)
		}
	}
	fmt.Println("active positions:")
	if len(snap.Positions) == 0 {
		fmt.Println("(none)")
		return
	}
	for _, p := range snap.Positions {
		fmt.Printf("- %-10s %-5s qty=%-10.4f entry=%-10.4f mark=%-10.4f upnl=%-10.4f lev=%g\n",
			p.Symbol, p.Side, p.SizeAbs, p.Entry, p.Mark, p.Unreal, p.Leverage)
	}
}

func filterBalances(rows []aster.Balance, assets []string) []aster.Balance {
	if len(assets) == 0 {
		out := make([]aster.Balance, 0, len(rows))
		for _, b := range rows {
			if b.Balance != 0 || b.AvailableBalance != 0 || b.CrossUnPnl != 0 {
				out = append(out, b)
			}
		}
		return out
	}
	want := make(map[string]struct{}, len(assets))
	for _, a := range assets {
		if s := strings.ToUpper(strings.TrimSpace(a)); s != "" {
			want[s] = struct{}{}
		}
	}
	out := make([]aster.Balance, 0, len(rows))
	for _, b := range rows {
		if _, ok := want[strings.ToUpper(strings.TrimSpace(b.Asset))]; ok {
			out = append(out, b)
		}
	}
	return out
}

func availableUSDT(rest *aster.RESTAuth) (float64, error) {
	bals, err := rest.GetBalance()
	if err != nil {
		return 0, err
	}
	for _, b := range bals {
		if strings.EqualFold(strings.TrimSpace(b.Asset), "USDT") {
			return b.AvailableBalance, nil
		}
	}
	return 0, nil
}

func hasAnyOpenPosition(rest *aster.RESTAuth) (bool, error) {
	rows, err := rest.PositionRisk("")
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		amt := mapFloat(r["positionAmt"])
		if amt != 0 {
			return true, nil
		}
	}
	return false, nil
}

func placeEntry(rest *aster.RESTAuth, c candidate, entryBps, margin float64) error {
	rawSym := strings.ToUpper(aster.RawSymbol(c.Entry.Symbol))
	bid, ask, err := rest.BookTicker(rawSym)
	if err != nil {
		return err
	}
	mid := (bid + ask) / 2
	if mid <= 0 {
		return fmt.Errorf("invalid mid price")
	}
	price := mid
	if c.Side == "BUY" {
		price = mid * (1 - entryBps/10000.0)
	} else {
		price = mid * (1 + entryBps/10000.0)
	}
	meta, err := rest.SymbolMeta(rawSym, false)
	if err != nil {
		return err
	}
	price, _, err = rest.RoundPrice(rawSym, price)
	if err != nil {
		return err
	}
	qty := margin / price
	qty, _, err = rest.RoundQty(rawSym, qty)
	if err != nil {
		return err
	}
	if qty <= 0 {
		return fmt.Errorf("qty <= 0 after rounding")
	}

	lev := 3
	if strings.EqualFold(c.Entry.CurrentGrade, "A+") {
		lev = 5
	}
	_, _ = rest.ChangeLeverage(rawSym, lev)

	vals := url.Values{}
	vals.Set("symbol", rawSym)
	vals.Set("side", c.Side)
	vals.Set("type", "LIMIT")
	vals.Set("timeInForce", "GTC")
	vals.Set("positionSide", "BOTH")
	vals.Set("reduceOnly", "false")
	vals.Set("quantity", formatFloat(qty, meta.QtyPrecision))
	vals.Set("price", formatFloat(price, meta.PricePrecision))

	out, err := rest.PlaceOrder(vals)
	if err != nil {
		return err
	}
	fmt.Printf("live-lite: placed entry %s %s qty=%s price=%s -> %v\n",
		rawSym, c.Side, vals.Get("quantity"), vals.Get("price"), out)
	return nil
}

func buildRESTFromConfig() *aster.RESTAuth {
	fileKV := getConfigKV()

	key := cfgGet(fileKV, "ASTER_API_KEY", "aster_api_key", "api_key", "key")
	sec := cfgGet(fileKV, "ASTER_API_SECRET", "aster_api_secret", "api_secret", "secret")
	if key == "" || sec == "" {
		return nil
	}
	baseURL := cfgGet(fileKV, "EXEC_BASE_URL", "aster_base_url", "base_url")
	rest := aster.NewRESTAuthWithConfig(aster.RESTAuthConfig{
		APIKey:    key,
		APISecret: sec,
		AuthMode:  "hmac",
		BaseURL:   baseURL,
	})
	_ = rest.SyncTime()
	return rest
}

func loadSimpleYAMLKV(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(parts[0]))
		v := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		if k != "" && v != "" {
			out[k] = v
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func getConfigKV() map[string]string {
	cfgPath := strings.TrimSpace(os.Getenv("ASTER_CONFIG"))
	if cfgPath == "" {
		if _, err := os.Stat(".aster.yaml"); err == nil {
			cfgPath = ".aster.yaml"
		} else if home, err := os.UserHomeDir(); err == nil && home != "" {
			cfgPath = filepath.Join(home, ".aster.yaml")
		}
	}
	if cfgPath == "" {
		return map[string]string{}
	}
	kv, err := loadSimpleYAMLKV(cfgPath)
	if err != nil {
		return map[string]string{}
	}
	return kv
}

func cfgGet(fileKV map[string]string, envName string, keys ...string) string {
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		return v
	}
	for _, k := range keys {
		if v := strings.TrimSpace(fileKV[strings.ToLower(strings.TrimSpace(k))]); v != "" {
			return v
		}
	}
	return ""
}

func mapFloat(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	default:
		f, _ := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f
	}
}

func formatFloat(v float64, prec int) string {
	if prec < 0 {
		prec = 0
	}
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

func envStr(k, def string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	return v
}

func envFloat(k string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func envInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envBool(k string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	v = strings.ToLower(v)
	return !(v == "0" || v == "false" || v == "no" || v == "off")
}

func envCSV(k, def string) []string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		v = def
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func newTelegramSink() *telegramSink {
	fileKV := getConfigKV()
	token := cfgGet(fileKV, "LIVE_TG_BOT_TOKEN", "live_tg_bot_token")
	if token == "" {
		token = cfgGet(fileKV, "TELEGRAM_BOT_TOKEN", "telegram_bot_token", "tg_bot_token")
	}
	chatID := cfgGet(fileKV, "LIVE_TG_CHAT_ID", "live_tg_chat_id")
	if chatID == "" {
		chatID = cfgGet(fileKV, "TELEGRAM_CHAT_ID", "telegram_chat_id", "tg_chat_id")
	}
	timeoutSec := envInt("LIVE_TG_TIMEOUT_SEC", 5)
	if timeoutSec <= 0 {
		timeoutSec = 5
	}
	dedupeSec := envInt("LIVE_TG_DEDUPE_SEC", 30)
	if dedupeSec < 0 {
		dedupeSec = 0
	}
	return &telegramSink{
		enabled:  token != "" && chatID != "",
		token:    token,
		chatID:   chatID,
		timeout:  time.Duration(timeoutSec) * time.Second,
		dedupe:   time.Duration(dedupeSec) * time.Second,
		client:   &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
		lastSent: map[string]time.Time{},
	}
}

func (t *telegramSink) Sendf(format string, args ...any) {
	if t == nil || !t.enabled {
		return
	}
	msg := strings.TrimSpace(fmt.Sprintf(format, args...))
	if msg == "" {
		return
	}
	now := time.Now()
	if t.dedupe > 0 {
		if at, ok := t.lastSent[msg]; ok && now.Sub(at) < t.dedupe {
			return
		}
		t.lastSent[msg] = now
	}
	_ = t.send(msg)
}

func (t *telegramSink) send(msg string) error {
	form := url.Values{}
	form.Set("chat_id", t.chatID)
	form.Set("text", msg)
	form.Set("disable_web_page_preview", "true")
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram http %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func gradeValue(g string) int {
	switch strings.ToUpper(strings.TrimSpace(g)) {
	case "A+":
		return 5
	case "A":
		return 4
	case "B":
		return 3
	case "C":
		return 2
	case "D":
		return 1
	default:
		return 0
	}
}

func calcVWAP(bars []types.Candle) float64 {
	var pv, v float64
	for _, b := range bars {
		tp := (b.H + b.L + b.C) / 3.0
		pv += tp * b.V
		v += b.V
	}
	if v == 0 {
		return 0
	}
	return pv / v
}

func symbolCandidates(ui string) []string {
	base := strings.ReplaceAll(ui, "-", "")
	cands := []string{}
	if strings.HasSuffix(base, "USD") {
		cands = append(cands, base+"T")
		cands = append(cands, base)
	}
	cands = append(cands, ui)
	return cands
}

func confluenceLabel(c *aster.Client, symbol, side string) string {
	const (
		tf     = types.TF5m
		n      = 200
		win    = 20
		zmin   = 2.0
		vmin   = 1_000_000.0
		levels = 50
	)

	var useSym string
	var bars []types.Candle
	for _, cand := range symbolCandidates(symbol) {
		b, err := c.LoadCandles(cand, tf, n)
		if err == nil && len(b) > 0 {
			useSym, bars = cand, b
			break
		}
	}
	if len(bars) == 0 {
		return "C"
	}

	vwap := calcVWAP(bars)
	tr := ta.TrendMetrics(useSym, tf, bars, vwap)
	ef := ta.ComputeEffort(useSym, tf, bars, win, zmin, vmin)
	obRaw, err := c.FetchOrderBook(useSym, levels)
	if err != nil {
		return "C"
	}
	ob := ta.OrderBookContext(useSym, obRaw.Bids, obRaw.Asks, levels)
	conf := ta.ComputeConfluence(tr, ef, ob, side)
	return conf.Label
}
