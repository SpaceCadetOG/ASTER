package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/notify"
	"go-machine/internal/stats"
)

func normalizeProtectiveStopToTick(side string, stop float64, meta aster.SymbolMeta) float64 {
	if stop <= 0 {
		return 0
	}
	out := stop
	if meta.TickSize > 0 {
		steps := stop / meta.TickSize
		if isLongSide(side) {
			steps = math.Floor(steps + 1e-9)
		} else {
			steps = math.Ceil(steps - 1e-9)
		}
		out = steps * meta.TickSize
	}
	if out <= 0 {
		return 0
	}
	return roundToPrecision(out, meta.PricePrecision)
}

func manualProtectionStillAttaching(p *livePosition) bool {
	if p == nil || !manualManagedTrade(p) || !p.ProtectionPending {
		return false
	}
	state := strings.TrimSpace(p.ManualManageState)
	return state != manualManageStateCritical && state != manualManageStateForceClose
}

func (m *liveExecManager) tryImmediateManualProtection(p *livePosition) {
	if m == nil || p == nil || !manualManagedTrade(p) || m.rest == nil {
		return
	}
	p.ProtectionPending = true
	p.ProtectionRetryAfter = time.Time{}
	p.ForceProtectionNow = true
	_ = m.placeOrReplaceStopWithRetry(p)
}

func (m *liveExecManager) activateManualManagement(req manualManageRequest, now time.Time, reason string) (*livePosition, error) {
	if m == nil {
		return nil, fmt.Errorf("live execution manager unavailable")
	}
	sym := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if sym == "" {
		return nil, fmt.Errorf("invalid symbol")
	}
	if err := m.validateManualManageRequest(req, now); err != nil {
		if m.tg != nil {
			m.tg.Sendf("%s", notify.BuildEventHTML("⚠️", "MANUAL STATE CONFLICT",
				fmt.Sprintf("<b>%s %s</b>", sym, displayPositionSide(req.Side)),
				fmt.Sprintf("<b>Cause:</b> %s", summarizeOneLine(err.Error(), 140)),
				"Bot management paused until the live/manual state matches again.",
			))
		}
		_ = m.save()
		return nil, err
	}
	if existing := m.positions[sym]; m.isActive(existing) {
		if !strings.EqualFold(strings.TrimSpace(existing.Side), strings.TrimSpace(req.Side)) {
			return nil, fmt.Errorf("active opposite-side state already exists for %s", sym)
		}
		if manualPassivePosition(existing) {
			_ = syncImportedRemotePosition(existing, req.Qty, req.Entry, req.Margin, req.Leverage, now)
			p := existing
			currentMark := 0.0
			if m.rest != nil {
				if mark, err := m.currentMark(sym); err == nil && mark > 0 {
					currentMark = mark
				}
			}
			p.EntrySource = manualEntrySourceManaged
			p.EntryReason = manualEntryReasonManaged
			p.ManualManageState = manualManageStatePendingProtection
			p.StarterOnly = false
			p.AddLockedUntilConfirm = false
			if currentMark > 0 {
				p.ManageAnchorPrice = currentMark
			}
			if err := m.initializeBracketLevels(p); err != nil {
				return nil, err
			}
			if currentMark > 0 {
				m.reconstructManualManagedState(now, p, currentMark)
			}
			m.alignManualManagedLeverage(p)
			armManualProtectionAfterReconstruct(now, p)
			m.tryImmediateManualProtection(p)
			m.mu.Lock()
			delete(m.manualRequests, req.Key)
			m.mu.Unlock()
			_ = m.save()
			if !hasLiveProtectiveOrder(p) {
				if manualProtectionStillAttaching(p) {
					return p, nil
				}
				return p, fmt.Errorf("immediate protection attach failed")
			}
			return p, nil
		}
		m.mu.Lock()
		delete(m.manualRequests, req.Key)
		m.mu.Unlock()
		return existing, nil
	}
	p := m.newImportedRemotePosition(sym, req.Side, req.Qty, req.Entry, req.Margin, req.Leverage, now, manualEntrySourceManaged)
	currentMark := 0.0
	if m.rest != nil {
		if mark, err := m.currentMark(sym); err == nil && mark > 0 {
			currentMark = mark
		}
	}
	p.EntryReason = manualEntryReasonManaged
	p.ManualManageState = manualManageStatePendingProtection
	p.StarterOnly = false
	p.AddLockedUntilConfirm = false
	if currentMark > 0 {
		p.ManageAnchorPrice = currentMark
	}
	if err := m.initializeBracketLevels(p); err != nil {
		return nil, err
	}
	if currentMark > 0 {
		m.reconstructManualManagedState(now, p, currentMark)
	}
	m.alignManualManagedLeverage(p)
	armManualProtectionAfterReconstruct(now, p)
	m.tryImmediateManualProtection(p)
	m.positions[sym] = p
	m.mu.Lock()
	delete(m.manualRequests, req.Key)
	m.mu.Unlock()
	_ = m.save()
	if !hasLiveProtectiveOrder(p) {
		if manualProtectionStillAttaching(p) {
			return p, nil
		}
		return p, fmt.Errorf("immediate protection attach failed")
	}
	if m.tg != nil {
		lines := []string{
			fmt.Sprintf("<b>%s %s</b>", p.Symbol, displayPositionSide(p.Side)),
			fmt.Sprintf("<b>Qty:</b> %.6f | <b>Entry:</b> %s", p.RemainingQty, fmtPrice(p.EntryPrice)),
			fmt.Sprintf("<b>Managed As:</b> %s", p.EntryReason),
		}
		if currentMark > 0 {
			lines = append(lines,
				fmt.Sprintf("<b>Mark Now:</b> %s", fmtPrice(currentMark)),
				fmt.Sprintf("<b>Stage:</b> %d | <b>TP Hit:</b> %v/%v/%v", p.ProtectionStage, p.HitTP1, p.HitTP2, p.HitTP3),
				fmt.Sprintf("<b>Would Add If Rechecked:</b> %v", manualWouldAddCapital(p, currentMark, m.ladderCfg.MinAddPnLPct)),
				fmt.Sprintf("<b>Protection:</b> %s", manualProtectionStatus(p)),
			)
		}
		m.tg.Sendf("%s", notify.BuildManagementStatusCard(notify.ManageStateAdopted, p.Symbol, p.Side, lines...))
	}
	_ = m.logFill(now, p, "ENTRY", reason, p.FilledQty, p.EntryPrice, 0, 0)
	m.sendFillReceipt(now, p, "ENTRY", reason, p.FilledQty, p.EntryPrice, 0, 0)
	return p, nil
}

func normalizeManualProtectiveStop(symbol, side string, rest *aster.RESTAuth, entry, mark, stopPx, tickSize float64) (float64, float64, error) {
	currentMark := mark
	if currentMark <= 0 && rest != nil {
		bid, ask, err := rest.BookTicker(symbol)
		if err == nil {
			currentMark = chooseProtectiveReference(side, bid, ask)
		}
	}
	if protectiveStopExchangeSafe(side, entry, currentMark, stopPx, tickSize) {
		return stopPx, currentMark, nil
	}
	for _, candidate := range manualStopRetryCandidates(side, entry, currentMark, tickSize) {
		if !protectiveStopExchangeSafe(side, entry, currentMark, candidate, tickSize) {
			continue
		}
		return candidate, currentMark, nil
	}
	return 0, currentMark, fmt.Errorf("no_exchange_safe_manual_stop")
}

func immediateTriggerAPIError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *aster.APIError
	if errors.As(err, &apiErr) {
		body := strings.ToLower(strings.TrimSpace(apiErr.Body))
		return strings.Contains(body, "\"code\":-2021") || strings.Contains(body, "immediately trigger")
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "-2021") || strings.Contains(msg, "immediately trigger")
}

func (m *liveExecManager) logManageFailedSafe(p *livePosition, mark, computedStop, normalizedStop float64, cause string) {
	if p == nil {
		return
	}
	cause = strings.TrimSpace(cause)
	if manageDebugLogging() {
		fmt.Printf("live: manage-failed-safe symbol=%s side=%s mark=%s entry=%s computed_stop=%s normalized_stop=%s cause=%s\n",
			p.Symbol,
			p.Side,
			fmtPrice(mark),
			fmtPrice(p.EntryPrice),
			fmtPrice(computedStop),
			fmtPrice(normalizedStop),
			cause,
		)
	}
	if m != nil && m.tg != nil {
		now := time.Now().UTC()
		state := notify.ManageStateAttachingProtection
		switch strings.TrimSpace(p.ManualManageState) {
		case manualManageStateForceClose:
			state = notify.ManageStateForceCloseTriggered
		case manualManageStateCritical:
			state = notify.ManageStateDegraded
		}
		if !shouldNotifyManageStatus(p, state, cause, now) {
			p.ManageFailSuppressCount++
			return
		}
		suppressed := p.ManageFailSuppressCount
		p.ManageFailSuppressCount = 0
		lines := []string{
			fmt.Sprintf("<b>%s %s</b>", p.Symbol, displayPositionSide(p.Side)),
			fmt.Sprintf("<b>Mark:</b> %s | <b>Entry:</b> %s", fmtPrice(mark), fmtPrice(p.EntryPrice)),
			fmt.Sprintf("<b>Computed Stop:</b> %s | <b>Normalized:</b> %s", fmtPrice(computedStop), fmtPrice(normalizedStop)),
			fmt.Sprintf("<b>Cause:</b> %s", cause),
			fmt.Sprintf("<b>Protection:</b> %s | <b>Retries:</b> %d/%d", manualProtectionStatus(p), p.ProtectionRetryCount, manualProtectionRetryBudget()),
		}
		if suppressed > 0 {
			lines = append(lines, fmt.Sprintf("<b>Suppressed duplicates:</b> %d", suppressed))
		}
		m.tg.Sendf("%s", notify.BuildManagementStatusCard(state, p.Symbol, p.Side, lines...))
	}
}

func (m *liveExecManager) placeOrReplaceStop(p *livePosition) error {
	if p.RemainingQty <= 0 {
		return nil
	}
	if manualPassivePosition(p) {
		return nil
	}
	now := time.Now().UTC()
	if manualManagedTrade(p) {
		forceNow := p.ForceProtectionNow
		switch strings.TrimSpace(p.ManualManageState) {
		case manualManageStateForceClose, manualManageStateCritical:
			if m.shouldEmergencyForceCloseManagedPosition(p, firstNonEmpty(p.LastManageFailCause, "managed_unprotected")) {
				return m.emergencyForceCloseManagedPosition(p, firstNonEmpty(p.LastManageFailCause, "managed_unprotected"), now)
			}
			p.ManualManageState = manualManageStatePendingProtection
		}
		if p.ProtectionPending && !forceNow && !p.ProtectionRetryAfter.IsZero() && now.Before(p.ProtectionRetryAfter) {
			return nil
		}
		if envBool("LIVE_MANUAL_PROTECTION_DEFER_UNTIL_CONVICTION", false) && !forceNow && !manualProtectionConvictionReady(p) {
			if manualProtectionConvictionTimedOut(p, now) {
				recordManualProtectionFailure(p, now, "awaiting_conviction_timeout")
				m.handleManualProtectionFailure(p, "awaiting_conviction_timeout", now)
				return nil
			}
			p.ManualManageState = manualManageStatePendingProtection
			markProtectionPending(p, now, "awaiting_conviction")
			return nil
		}
		p.ForceProtectionNow = false
	}
	prevStop := p.ProtectedStop
	if prevStop <= 0 {
		prevStop = p.StopPrice
	}
	oldStopOrderID := p.StopOrderID
	meta, err := m.rest.SymbolMeta(p.Symbol, true)
	if err != nil {
		return err
	}
	qty, _, err := m.rest.RoundQty(p.Symbol, p.RemainingQty)
	if err != nil {
		return err
	}
	computedStop := p.StopPrice
	legalityAdjusted := false
	protectiveEntry := manageAnchorPrice(p)
	protectiveMark := 0.0
	if protectiveEntry <= 0 {
		protectiveEntry = p.EntryPrice
	}
	stopPx, _, err := m.rest.RoundPrice(p.Symbol, computedStop)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT") {
		mark, err := m.currentProtectiveReference(p.Symbol, p.Side)
		if err != nil || mark <= 0 {
			recordManualProtectionFailure(p, now, "mark_unavailable")
			m.logManageFailedSafe(p, 0, computedStop, stopPx, "mark_unavailable")
			m.handleManualProtectionFailure(p, "mark_unavailable", now)
			if manualManagedTrade(p) {
				return nil
			}
			return fmt.Errorf("manage-failed-safe: mark unavailable for %s %s", p.Symbol, p.Side)
		}
		protectiveMark = mark
		computedStop = chooseManagedProtectiveStop(p.Side, protectiveEntry, mark, computedStop, p.ProtectedStop)
		stopPx, _, err = m.rest.RoundPrice(p.Symbol, computedStop)
		if err != nil {
			return err
		}
		normalizedStop, normalizedMark, normErr := normalizeManualProtectiveStop(p.Symbol, p.Side, m.rest, protectiveEntry, mark, stopPx, meta.TickSize)
		if normErr != nil || normalizedStop <= 0 {
			recordManualProtectionFailure(p, now, "invalid_after_retry")
			m.logManageFailedSafe(p, normalizedMark, computedStop, stopPx, "invalid_after_retry")
			m.handleManualProtectionFailure(p, "invalid_after_retry", now)
			if manualManagedTrade(p) {
				return nil
			}
			return fmt.Errorf("manage-failed-safe: invalid protective stop symbol=%s side=%s mark=%s entry=%s computed_stop=%s normalized_stop=%s",
				p.Symbol, p.Side, fmtPrice(normalizedMark), fmtPrice(p.EntryPrice), fmtPrice(computedStop), fmtPrice(stopPx))
		}
		protectiveMark = normalizedMark
		stopPx = normalizedStop
		legalityAdjusted = math.Abs(stopPx-computedStop) > 1e-9
	}
	stopPx = normalizeProtectiveStopToTick(p.Side, stopPx, meta)
	if !legalityAdjusted {
		legalityAdjusted = math.Abs(stopPx-computedStop) > 1e-9
	}
	if qty <= 0 || stopPx <= 0 {
		return fmt.Errorf("invalid stop qty/price")
	}
	legalQty, legalStop, legalityReason := validateOrderLegality(meta, qty, stopPx)
	if legalityReason != "" {
		if manualManagedTrade(p) && legalityReason == orderIllegalTickSizeReason {
			repairMark := protectiveMark
			if repairMark <= 0 {
				repairMark = maxFloat(p.LastMark, protectiveEntry)
			}
			for _, candidate := range manualStopRetryCandidates(p.Side, protectiveEntry, repairMark, meta.TickSize) {
				retryStop := normalizeProtectiveStopToTick(p.Side, candidate, meta)
				if !protectiveStopExchangeSafe(p.Side, protectiveEntry, repairMark, retryStop, meta.TickSize) {
					continue
				}
				if q2, s2, r2 := validateOrderLegality(meta, qty, retryStop); r2 == "" {
					legalQty = q2
					legalStop = s2
					legalityReason = ""
					break
				}
			}
		}
		if legalityReason != "" {
			m.recordOrderLegalityFailure(p.Symbol, legalityReason, now)
			if manualManagedTrade(p) {
				recordManualProtectionFailure(p, now, legalityReason)
				m.handleManualProtectionFailure(p, legalityReason, now)
				return nil
			}
			return fmt.Errorf("%s", legalityReason)
		}
	}
	qty = legalQty
	stopPx = legalStop
	stopPx = normalizeProtectiveStopToTick(p.Side, stopPx, meta)
	if !legalityAdjusted {
		legalityAdjusted = math.Abs(stopPx-computedStop) > 1e-9
	}
	p.StopPrice = stopPx
	closeSide := "SELL"
	if strings.EqualFold(p.Side, "SELL") {
		closeSide = "BUY"
	}
	out, err := m.rest.ReplaceStopOrder(
		p.Symbol,
		closeSide,
		oldStopOrderID,
		qty,
		stopPx,
		meta.QtyPrecision,
		meta.PricePrecision,
	)
	if err != nil {
		if !strings.EqualFold(strings.TrimSpace(p.EntrySource), "BOT") && immediateTriggerAPIError(err) {
			lastRetryStop := stopPx
			retryPlaced := false
			lastRetryMark := 0.0
			retryOldStopID := int64(0)
			for attempt := 0; attempt < 3 && !retryPlaced; attempt++ {
				mark, markErr := m.currentProtectiveReference(p.Symbol, p.Side)
				if markErr != nil || mark <= 0 {
					recordManualProtectionFailure(p, now, "exchange_immediate_trigger_mark_unavailable")
					m.logManageFailedSafe(p, 0, computedStop, stopPx, "exchange_immediate_trigger_mark_unavailable")
					m.handleManualProtectionFailure(p, "exchange_immediate_trigger_mark_unavailable", now)
					if manualManagedTrade(p) {
						return nil
					}
					return fmt.Errorf("manage-failed-safe: mark unavailable for %s %s", p.Symbol, p.Side)
				}
				lastRetryMark = mark
				for _, candidate := range manualStopRetryCandidates(p.Side, protectiveEntry, mark, meta.TickSize) {
					retryStop := normalizeProtectiveStopToTick(p.Side, candidate, meta)
					if !protectiveStopExchangeSafe(p.Side, protectiveEntry, mark, retryStop, meta.TickSize) {
						lastRetryStop = retryStop
						continue
					}
					if legalQty, legalStop, legalityReason := validateOrderLegality(meta, qty, retryStop); legalityReason != "" {
						m.recordOrderLegalityFailure(p.Symbol, legalityReason, now)
						lastRetryStop = retryStop
						qty = legalQty
						continue
					} else {
						retryStop = legalStop
						qty = legalQty
					}
					lastRetryStop = retryStop
					out, err = m.rest.ReplaceStopOrder(
						p.Symbol,
						closeSide,
						retryOldStopID,
						qty,
						retryStop,
						meta.QtyPrecision,
						meta.PricePrecision,
					)
					if err == nil {
						stopPx = retryStop
						retryPlaced = true
						break
					}
					if !immediateTriggerAPIError(err) {
						break
					}
				}
			}
			if !retryPlaced {
				recordManualProtectionFailure(p, now, "exchange_immediate_trigger_retry_failed")
				m.logManageFailedSafe(p, lastRetryMark, computedStop, lastRetryStop, "exchange_immediate_trigger_retry_failed")
				m.handleManualProtectionFailure(p, "exchange_immediate_trigger_retry_failed", now)
				if manualManagedTrade(p) {
					return nil
				}
				return fmt.Errorf("manage-failed-safe: stop placement retry failed symbol=%s side=%s mark=%s entry=%s computed_stop=%s normalized_stop=%s err=%v",
					p.Symbol, p.Side, fmtPrice(lastRetryMark), fmtPrice(p.EntryPrice), fmtPrice(computedStop), fmtPrice(lastRetryStop), err)
			}
		} else {
			return err
		}
	}
	p.StopOrderID = mapInt64(out["orderId"])
	p.ProtectedStop = stopPx
	wasProtectionPending := p.ProtectionPending
	clearProtectionPending(p)
	if wasProtectionPending && m.tg != nil && manualManagedTrade(p) {
		m.tg.Sendf("%s", notify.BuildManagementStatusCard(notify.ManageStateProtected, p.Symbol, p.Side,
			fmt.Sprintf("<b>Exchange stop:</b> %s", fmtPrice(stopPx)),
			fmt.Sprintf("<b>Order ID:</b> %d", p.StopOrderID),
			"<b>Fresh entries remain blocked</b> until normal gates re-evaluate.",
		))
	}
	stopChanged := prevStop <= 0 || math.Abs(prevStop-stopPx) > 1e-9
	if stopChanged && manageDebugLogging() {
		fmt.Printf("live: stop update %s %s old=%s new=%s trigger_ref=%s reason=%s source=%s\n",
			p.Symbol,
			p.Side,
			fmtPrice(prevStop),
			fmtPrice(stopPx),
			strings.ToUpper(strings.TrimSpace(m.stopTriggerRef)),
			nonEmpty(strings.ToUpper(strings.TrimSpace(p.StopReason)), "PROTECT"),
			nonEmpty(strings.ToUpper(strings.TrimSpace(p.EntrySource)), "BOT"),
		)
	}
	if legalityAdjusted {
		fmt.Printf("PROTECTION_ADJUSTED symbol=%s side=%s computed_stop=%s submitted_stop=%s accepted_stop=%s trigger_ref=%s legality_adjustment_applied=%t\n",
			p.Symbol,
			p.Side,
			fmtPrice(computedStop),
			fmtPrice(stopPx),
			fmtPrice(stopPx),
			strings.ToUpper(strings.TrimSpace(m.stopTriggerRef)),
			true,
		)
	}
	if stopChanged && m.eventLog != nil {
		m.eventLog.Emit(stats.Event{
			Timestamp:  time.Now().UTC(),
			Type:       "STOP_UPDATE",
			Symbol:     p.Symbol,
			Side:       p.Side,
			Source:     nonEmpty(strings.ToUpper(strings.TrimSpace(p.EntrySource)), "BOT"),
			EntryPx:    p.EntryPrice,
			ExitPx:     stopPx,
			TriggerRef: strings.ToUpper(strings.TrimSpace(m.stopTriggerRef)),
			Reason:     nonEmpty(strings.ToUpper(strings.TrimSpace(p.StopReason)), "PROTECT"),
		})
	}
	fmt.Printf("STOP_CHAIN symbol=%s side=%s computed_stop=%s submitted_stop=%s accepted_stop=%s trigger_ref=%s legality_adjustment_applied=%t\n",
		p.Symbol,
		p.Side,
		fmtPrice(computedStop),
		fmtPrice(stopPx),
		fmtPrice(stopPx),
		strings.ToLower(strings.TrimSpace(m.stopTriggerRef)),
		legalityAdjusted,
	)
	return nil
}
