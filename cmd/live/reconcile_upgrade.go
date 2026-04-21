package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go-machine/internal/notify"
)

type accountHealthSummary struct {
	State                 string
	SignedUserDataBackoff bool
	AsOf                  time.Time
}

type VenueOrderState string

const (
	VenueOrderPending  VenueOrderState = "pending"
	VenueOrderAcked    VenueOrderState = "acked"
	VenueOrderUnknown  VenueOrderState = "unknown"
	VenueOrderRejected VenueOrderState = "rejected"
	VenueOrderFilled   VenueOrderState = "filled"
	VenueOrderCanceled VenueOrderState = "canceled"
)

type UnknownExecutionGuard struct {
	Symbol      string
	IntentID    string
	State       VenueOrderState
	FreezeUntil time.Time
}

func buildIntentID(symbol, strategyID string, trigger float64, now time.Time) string {
	return fmt.Sprintf("%s|%s|%.6f|%d",
		strings.ToUpper(strings.TrimSpace(symbol)),
		strings.TrimSpace(strategyID),
		trigger,
		now.UTC().Unix()/5,
	)
}

func (m *liveExecManager) handleUnknownExecution(symbol, intentID string) {
	if m == nil {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(symbol))
	if raw == "" {
		return
	}
	freezeSec := envIntRU("LIVE_EXEC_UNKNOWN_FREEZE_SEC", 8)
	guard := UnknownExecutionGuard{
		Symbol:      raw,
		IntentID:    strings.TrimSpace(intentID),
		State:       VenueOrderUnknown,
		FreezeUntil: time.Now().UTC().Add(time.Duration(freezeSec) * time.Second),
	}
	m.mu.Lock()
	if m.unknownExecGuards == nil {
		m.unknownExecGuards = map[string]UnknownExecutionGuard{}
	}
	m.unknownExecGuards[raw] = guard
	m.mu.Unlock()
	logUnknownExecution(raw, guard.IntentID, "mark_unknown", "execution outcome ambiguous")
	m.emitNotify(notify.Event{
		Key:      "EXECUTION_UNKNOWN",
		Title:    "EXECUTION UNKNOWN",
		Class:    notify.ClassCritical,
		Severity: notify.SeverityCritical,
		Route:    notify.RouteCritical,
		Symbol:   raw,
		Message:  "entry frozen pending reconcile",
		Metadata: map[string]string{
			"intent_id": guard.IntentID,
		},
	})
}

func (m *liveExecManager) isUnknownExecutionFrozen(symbol string) bool {
	if m == nil {
		return false
	}
	raw := strings.ToUpper(strings.TrimSpace(symbol))
	m.mu.RLock()
	guard, ok := m.unknownExecGuards[raw]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Now().UTC().Before(guard.FreezeUntil)
}

func (m *liveExecManager) clearUnknownExecutionGuard(symbol string) {
	if m == nil {
		return
	}
	raw := strings.ToUpper(strings.TrimSpace(symbol))
	m.mu.Lock()
	delete(m.unknownExecGuards, raw)
	m.mu.Unlock()
}

func (m *liveExecManager) reconcileAfterUnknown(symbol, intentID string) string {
	if m == nil || m.rest == nil {
		return "not_ready"
	}
	raw := strings.ToUpper(strings.TrimSpace(symbol))
	orderFound := false
	if orders, err := m.rest.OpenOrders(raw); err == nil {
		orderFound = len(orders) > 0
	} else {
		m.emitNotify(notify.Event{
			Key:      "RECONCILE_FAILED",
			Title:    "RECONCILE FAILED",
			Class:    notify.ClassCritical,
			Severity: notify.SeverityCritical,
			Route:    notify.RouteCritical,
			Symbol:   raw,
			Message:  "remote open orders could not be fetched",
			Metadata: map[string]string{"intent_id": intentID, "error": err.Error()},
		})
	}
	posFound := false
	if rows, err := m.rest.PositionRisk(raw); err == nil {
		view := remotePositionForSide(rows, "")
		posFound = view.QtyAbs > 0
	} else {
		m.emitNotify(notify.Event{
			Key:      "RECONCILE_FAILED",
			Title:    "RECONCILE FAILED",
			Class:    notify.ClassCritical,
			Severity: notify.SeverityCritical,
			Route:    notify.RouteCritical,
			Symbol:   raw,
			Message:  "remote position state could not be fetched",
			Metadata: map[string]string{"intent_id": intentID, "error": err.Error()},
		})
	}
	switch {
	case orderFound:
		logUnknownReconcile(raw, intentID, "order_found")
		return "order_found"
	case posFound:
		logUnknownReconcile(raw, intentID, "position_found")
		return "position_found"
	default:
		m.clearUnknownExecutionGuard(raw)
		logUnknownReconcile(raw, intentID, "cleared")
		return "cleared"
	}
}

func envIntRU(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func entriesBlockedByAccountHealth(summary accountHealthSummary) (string, bool) {
	if summary.State == "failed" {
		return "account_health_failed", true
	}
	return "", false
}

func fillEpsilon(q float64) float64 {
	return maxFloat(1e-9, mathAbs(q)*0.0005)
}

func mathAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func (m *liveExecManager) transitionPendingToOpen(now time.Time, p *livePosition, qty, avgPx float64, reason string) (bool, error) {
	if p == nil {
		return false, nil
	}
	if qty <= 0 {
		qty = p.Qty
	}
	if avgPx > 0 {
		p.EntryPrice = avgPx
	}
	p.FilledQty = qty
	p.RemainingQty = qty
	p.State = execOpen
	p.UpdatedAt = now
	updateManagePhase(p, false)
	refreshRunnerReservation(p, m.ladderCfg.StarterUSDT)
	if strings.TrimSpace(p.EntrySource) == "" {
		p.EntrySource = "BOT"
	}
	if err := m.placeInitialBrackets(p); err != nil {
		return true, err
	}
	fmt.Printf("live: entry live %s reason=%s qty=%.6f avg=%.6f\n", p.Symbol, reason, p.FilledQty, p.EntryPrice)
	if m.tg != nil {
		title := "ENTRY FILLED"
		if !strings.EqualFold(reason, "ENTRY_FILLED") {
			title = "ENTRY RECOVERED"
		}
		m.tg.Sendf("%s", notify.BuildEventHTML("✅", title,
			fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
			fmt.Sprintf("<b>Qty:</b> %.6f | <b>Avg:</b> %s", p.FilledQty, fmtPrice(p.EntryPrice)),
			fmt.Sprintf("<b>Reason:</b> %s", strings.ToUpper(strings.TrimSpace(reason))),
		))
	}
	_ = m.logFill(now, p, "ENTRY", reason, p.FilledQty, p.EntryPrice, 0, 0)
	m.sendFillReceipt(now, p, "ENTRY", reason, p.FilledQty, p.EntryPrice, 0, 0)
	return true, nil
}

func (m *liveExecManager) syncPendingEntryFromRemote(p *livePosition) (float64, float64, bool) {
	if m == nil || m.rest == nil || p == nil {
		return 0, 0, false
	}
	rows, err := m.rest.PositionRisk(p.Symbol)
	if err != nil {
		return 0, 0, false
	}
	for _, row := range rows {
		amt := mapFloat(row["positionAmt"])
		if mathAbs(amt) <= 1e-10 {
			continue
		}
		side := "BUY"
		if amt < 0 {
			side = "SELL"
		}
		if !strings.EqualFold(normalizePositionSide(side), normalizePositionSide(p.Side)) {
			continue
		}
		entry := mapFloat(row["entryPrice"])
		mark := mapFloat(row["markPrice"])
		if entry <= 0 {
			entry = mark
		}
		return mathAbs(amt), entry, true
	}
	return 0, 0, false
}

func (m *liveExecManager) closePendingWithoutFill(now time.Time, p *livePosition, reason string) (bool, error) {
	if p == nil {
		return false, nil
	}
	markLivePositionClosed(p, now, reason)
	_ = m.logFill(now, p, "ENTRY", reason, 0, 0, 0, 0)
	m.sendFillReceipt(now, p, "ENTRY", reason, 0, 0, 0, 0)
	return true, nil
}

func (m *liveExecManager) stageRemainingQty(target, filled, rem float64) float64 {
	left := maxFloat(0, target-filled)
	if rem > 0 && left > rem {
		left = rem
	}
	return left
}

type remotePositionView struct {
	QtyAbs     float64
	EntryPrice float64
	MarkPrice  float64
	Margin     float64
}

func remotePositionForSide(rows []map[string]any, side string) remotePositionView {
	view := remotePositionView{}
	side = normalizePositionSide(side)
	for _, row := range rows {
		amt := mapFloat(row["positionAmt"])
		if mathAbs(amt) <= 1e-10 {
			continue
		}
		rowSide := "BUY"
		if amt < 0 {
			rowSide = "SELL"
		}
		if side != "" && !strings.EqualFold(normalizePositionSide(rowSide), side) {
			continue
		}
		view.QtyAbs = mathAbs(amt)
		view.EntryPrice = mapFloat(row["entryPrice"])
		view.MarkPrice = mapFloat(row["markPrice"])
		view.Margin = maxFloat(mapFloat(row["isolatedWallet"]), mapFloat(row["positionInitialMargin"]))
		return view
	}
	return view
}

func derivedAddFillPrice(currentEntry, currentQty float64, view remotePositionView) float64 {
	if view.EntryPrice <= 0 || view.QtyAbs <= currentQty {
		return view.EntryPrice
	}
	deltaQty := view.QtyAbs - currentQty
	if deltaQty <= 0 {
		return view.EntryPrice
	}
	totalCost := view.EntryPrice * view.QtyAbs
	oldCost := currentEntry * currentQty
	fillPx := (totalCost - oldCost) / deltaQty
	if fillPx <= 0 {
		return view.EntryPrice
	}
	return fillPx
}

func (m *liveExecManager) closeFromRemoteSnapshot(now time.Time, p *livePosition, fillPx float64, reason string) (bool, error) {
	if p == nil {
		return false, nil
	}
	_ = m.cancelRemainingExits(p)
	p.CloseReason = reason
	if m.tg != nil {
		m.tg.Sendf("%s", notify.BuildEventHTML("📪", "POSITION CLOSED",
			fmt.Sprintf("<b>%s</b>", p.Symbol),
			fmt.Sprintf("<b>Reason:</b> %s", reason),
		))
	}
	if fillPx <= 0 {
		fillPx = p.LastMark
	}
	if fillPx <= 0 {
		fillPx = p.EntryPrice
	}
	pnl, pct := realizedFromFill(p.Side, p.EntryPrice, fillPx, p.RemainingQty)
	p.RealizedPnL += pnl
	_ = m.addDayRealized(now, pnl)
	_ = m.logFill(now, p, "CLOSE", reason, p.RemainingQty, fillPx, pnl, pct)
	m.sendFillReceipt(now, p, "CLOSE", reason, p.RemainingQty, fillPx, pnl, pct)
	markLivePositionClosed(p, now, reason)
	p.RemainingQty = 0
	m.maybeSweepTradeProfit(now, p)
	return true, nil
}

func (m *liveExecManager) syncOpenFromRemote(now time.Time, p *livePosition, rows []map[string]any) (bool, bool, error) {
	if m == nil || p == nil {
		return false, false, nil
	}
	view := remotePositionForSide(rows, p.Side)
	if view.MarkPrice > 0 {
		p.LastMark = view.MarkPrice
	}
	if view.EntryPrice > 0 && p.EntryPrice <= 0 {
		p.EntryPrice = view.EntryPrice
	}
	if view.QtyAbs <= 1e-10 {
		if p.PendingExitOrderID > 0 {
			reason := firstNonEmpty(p.PendingExitReason, "LIMIT_EXIT_FILLED")
			m.clearPendingExit(p)
			changed, err := m.closeFromRemoteSnapshot(now, p, view.MarkPrice, reason)
			return changed, true, err
		}
		changed, err := m.closeFromRemoteSnapshot(now, p, view.MarkPrice, "POSITION_FLAT_REMOTE")
		return changed, true, err
	}
	eps := fillEpsilon(maxFloat(p.RemainingQty, view.QtyAbs))
	if p.RemainingQty <= 0 {
		p.RemainingQty = view.QtyAbs
		p.FilledQty = maxFloat(p.FilledQty, view.QtyAbs)
		p.UpdatedAt = now
		p.UnknownExitChecks = 0
		return true, false, m.ensureExitOrders(p)
	}
	if view.QtyAbs > p.RemainingQty+eps {
		if p.PendingAddOrderID > 0 {
			deltaQty := view.QtyAbs - p.RemainingQty
			deltaMargin := 0.0
			if view.Margin > p.Margin {
				deltaMargin = view.Margin - p.Margin
			}
			reason := "ADD_RECOVERED_REMOTE"
			if strings.EqualFold(strings.TrimSpace(p.PendingAddEntryReason), "continuation_fast") {
				reason = "CONFIRMED_ADD"
			}
			if err := m.applyAddFill(now, p, deltaQty, derivedAddFillPrice(p.EntryPrice, p.RemainingQty, view), deltaMargin, reason); err != nil {
				return true, false, err
			}
			p.PendingAddFilledQty = p.PendingAddQty
			m.clearPendingAdd(p)
			p.UnknownExitChecks = 0
			return true, false, nil
		}
		p.RemainingQty = view.QtyAbs
		p.FilledQty = maxFloat(p.FilledQty, view.QtyAbs)
		p.UpdatedAt = now
		p.UnknownExitChecks = 0
		if err := m.ensureExitOrders(p); err != nil {
			return true, false, err
		}
		return true, false, nil
	}
	if view.QtyAbs < p.RemainingQty-eps {
		delta := p.RemainingQty - view.QtyAbs
		fillPx := view.MarkPrice
		if fillPx <= 0 {
			fillPx = p.LastMark
		}
		if fillPx <= 0 {
			fillPx = p.EntryPrice
		}
		if p.PendingExitOrderID > 0 {
			if err := m.applyPendingExitProgress(now, p, delta, fillPx, false); err != nil {
				return true, false, err
			}
			p.UnknownExitChecks = 0
			if p.RemainingQty <= fillEpsilon(p.Qty) {
				reason := firstNonEmpty(p.PendingExitReason, "LIMIT_EXIT_FILLED")
				m.clearPendingExit(p)
				changed, err := m.closeFromRemoteSnapshot(now, p, fillPx, reason)
				return changed, true, err
			}
			return true, false, nil
		}
		pnl, pct := realizedFromFill(p.Side, p.EntryPrice, fillPx, delta)
		p.RealizedPnL += pnl
		_ = m.addDayRealized(now, pnl)
		p.RemainingQty = view.QtyAbs
		p.UpdatedAt = now
		p.UnknownExitChecks = 0
		_ = m.cancelRemainingExits(p)
		_ = m.logFill(now, p, "SYNC", "REMOTE_PARTIAL_SYNC", delta, fillPx, pnl, pct)
		m.sendFillReceipt(now, p, "SYNC", "REMOTE_PARTIAL_SYNC", delta, fillPx, pnl, pct)
		if p.RemainingQty <= fillEpsilon(p.Qty) {
			changed, err := m.closeFromRemoteSnapshot(now, p, fillPx, "POSITION_FLAT_REMOTE")
			return changed, true, err
		}
		if err := m.ensureExitOrders(p); err != nil {
			return true, false, err
		}
		return true, false, nil
	}
	p.UnknownExitChecks = 0
	return false, false, nil
}

func (m *liveExecManager) applyPendingExitProgress(now time.Time, p *livePosition, deltaQty, fillPx float64, final bool) error {
	if p == nil || deltaQty <= 0 {
		return nil
	}
	p.PendingExitFilledQty += deltaQty
	p.RemainingQty = maxFloat(0, p.RemainingQty-deltaQty)
	p.UpdatedAt = now
	pnl, pct := realizedFromFill(p.Side, p.EntryPrice, fillPx, deltaQty)
	p.RealizedPnL += pnl
	dayRealized := m.addDayRealized(now, pnl)
	reason := firstNonEmpty(p.PendingExitReason, "LIMIT_EXIT")
	action := firstNonEmpty(p.PendingExitAction, "CLOSE")
	title := "LIMIT EXIT PARTIAL"
	fillAction := "CLOSE"
	if strings.EqualFold(action, "FORCE_CLOSE") {
		title = "FORCE CLOSE PARTIAL"
		fillAction = "FORCE_CLOSE"
	}
	if final {
		title = "LIMIT EXIT FILLED"
		if strings.EqualFold(action, "FORCE_CLOSE") {
			title = "FORCE CLOSE FILLED"
		}
	}
	if m.tg != nil {
		m.tg.Sendf("%s", notify.BuildEventHTML("✅", title,
			fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
			fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", deltaQty, fmtPrice(fillPx)),
			fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%) | <b>Day:</b> %+.2f", pnl, pct, dayRealized),
		))
	}
	_ = m.logFill(now, p, fillAction, reason, deltaQty, fillPx, pnl, pct)
	m.sendFillReceipt(now, p, fillAction, reason, deltaQty, fillPx, pnl, pct)
	if p.RemainingQty <= fillEpsilon(p.Qty) {
		m.clearPendingExit(p)
		markLivePositionClosed(p, now, reason)
		p.RemainingQty = 0
		m.maybeSweepTradeProfit(now, p)
		return nil
	}
	if final {
		m.clearPendingExit(p)
		if err := m.ensureExitOrders(p); err != nil {
			return err
		}
	}
	return nil
}

func (m *liveExecManager) applyTPProgress(now time.Time, p *livePosition, stage int, deltaQty, fillPx float64, final bool) error {
	if p == nil || deltaQty <= 0 {
		return nil
	}
	p.RemainingQty = maxFloat(0, p.RemainingQty-deltaQty)
	p.UpdatedAt = now
	pnl, pct := realizedFromFill(p.Side, p.EntryPrice, fillPx, deltaQty)
	p.RealizedPnL += pnl
	dayRealized := m.addDayRealized(now, pnl)
	reason := fmt.Sprintf("TP%d_PARTIAL", stage)
	title := fmt.Sprintf("TP%d PARTIAL", stage)
	switch stage {
	case 1:
		p.TP1FilledQty += deltaQty
		if final || p.TP1FilledQty >= p.TP1Qty-fillEpsilon(p.TP1Qty) {
			p.HitTP1 = true
			p.TP1OrderID = 0
			reason = "TP1_HIT"
			title = "TP1 HIT"
			p.State = execPartialTP1
			if m.beLockBps > 0 {
				p.StopPrice = beLockPrice(p.Side, p.EntryPrice, m.beLockBps)
			}
			m.maybeEnableTrail(p, 1)
		}
	case 2:
		p.TP2FilledQty += deltaQty
		if final || p.TP2FilledQty >= p.TP2Qty-fillEpsilon(p.TP2Qty) {
			p.HitTP2 = true
			p.TP2OrderID = 0
			reason = "TP2_HIT"
			title = "TP2 HIT"
			p.State = execPartialTP2
			m.maybeEnableTrail(p, 2)
		}
	case 3:
		p.TP3FilledQty += deltaQty
		if final || p.TP3FilledQty >= p.TP3Qty-fillEpsilon(p.TP3Qty) {
			p.HitTP3 = true
			p.TP3OrderID = 0
			reason = "TP3_HIT"
			title = "TP3 HIT"
			m.maybeEnableTrail(p, 3)
		}
	}
	if m.tg != nil {
		m.tg.Sendf("%s", notify.BuildEventHTML("✅", title,
			fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
			fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", deltaQty, fmtPrice(fillPx)),
			fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%) | <b>Day:</b> %+.2f", pnl, pct, dayRealized),
		))
	}
	_ = m.logFill(now, p, "TP", reason, deltaQty, fillPx, pnl, pct)
	m.sendFillReceipt(now, p, "TP", reason, deltaQty, fillPx, pnl, pct)
	// Keep reduce-only stop quantity synchronized with remaining position after any TP fill.
	if err := m.placeOrReplaceStop(p); err != nil {
		return err
	}
	return nil
}

func (m *liveExecManager) applyStopProgress(now time.Time, p *livePosition, deltaQty, fillPx float64, final bool) error {
	if p == nil || deltaQty <= 0 {
		return nil
	}
	p.StopFilledQty += deltaQty
	p.RemainingQty = maxFloat(0, p.RemainingQty-deltaQty)
	p.UpdatedAt = now
	pnl, pct := realizedFromFill(p.Side, p.EntryPrice, fillPx, deltaQty)
	p.RealizedPnL += pnl
	dayRealized := m.addDayRealized(now, pnl)
	reason := "STOP_PARTIAL"
	title := "STOP PARTIAL"
	if final || p.RemainingQty <= fillEpsilon(p.Qty) {
		p.StopOrderID = 0
		reason = "STOP_HIT"
		title = "STOP HIT"
		markLivePositionClosed(p, now, "STOP_HIT")
	}
	if m.tg != nil {
		m.tg.Sendf("%s", notify.BuildEventHTML("🛑", title,
			fmt.Sprintf("<b>%s %s</b>", p.Symbol, p.Side),
			fmt.Sprintf("<b>Qty:</b> %.6f | <b>Px:</b> %s", deltaQty, fmtPrice(fillPx)),
			fmt.Sprintf("<b>PnL:</b> %+.2f (%+.2f%%) | <b>Day:</b> %+.2f", pnl, pct, dayRealized),
		))
	}
	_ = m.logFill(now, p, "STOP", reason, deltaQty, fillPx, pnl, pct)
	m.sendFillReceipt(now, p, "STOP", reason, deltaQty, fillPx, pnl, pct)
	if p.State != execClosed {
		return m.placeOrReplaceStop(p)
	}
	m.maybeSweepTradeProfit(now, p)
	return nil
}

func (m *liveExecManager) ensureExitOrders(p *livePosition) error {
	if m == nil || p == nil || p.State == execClosed || p.RemainingQty <= 0 {
		return nil
	}
	if m.tpRatchetOnly {
		if p.TP1OrderID > 0 {
			_, _ = m.rest.CancelOrder(p.Symbol, p.TP1OrderID)
			p.TP1OrderID = 0
		}
		if p.TP2OrderID > 0 {
			_, _ = m.rest.CancelOrder(p.Symbol, p.TP2OrderID)
			p.TP2OrderID = 0
		}
		if p.TP3OrderID > 0 {
			_, _ = m.rest.CancelOrder(p.Symbol, p.TP3OrderID)
			p.TP3OrderID = 0
		}
		if p.StopOrderID == 0 {
			return m.placeOrReplaceStop(p)
		}
		return nil
	}
	if p.StopOrderID == 0 {
		if err := m.placeOrReplaceStop(p); err != nil {
			return err
		}
	}
	if !p.HitTP1 {
		if rem := m.stageRemainingQty(p.TP1Qty, p.TP1FilledQty, p.RemainingQty); rem > fillEpsilon(p.TP1Qty) && p.TP1OrderID == 0 {
			id, err := m.placeReduceLimit(p, rem, p.TP1Price)
			if err != nil {
				return err
			}
			p.TP1OrderID = id
		}
	}
	if !p.HitTP2 {
		if rem := m.stageRemainingQty(p.TP2Qty, p.TP2FilledQty, p.RemainingQty); rem > fillEpsilon(p.TP2Qty) && p.TP2OrderID == 0 {
			id, err := m.placeReduceLimit(p, rem, p.TP2Price)
			if err != nil {
				return err
			}
			p.TP2OrderID = id
		}
	}
	if !p.HitTP3 {
		if rem := m.stageRemainingQty(p.TP3Qty, p.TP3FilledQty, p.RemainingQty); rem > fillEpsilon(p.TP3Qty) && p.TP3OrderID == 0 {
			id, err := m.placeReduceLimit(p, rem, p.TP3Price)
			if err != nil {
				return err
			}
			p.TP3OrderID = id
		}
	}
	return nil
}
