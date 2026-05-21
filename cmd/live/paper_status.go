package main

import (
	"sort"
	"strings"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/stats"
)

type livePaperSnapshot struct {
	Mode            string                  `json:"mode,omitempty"`
	Summary         string                  `json:"summary,omitempty"`
	Balance         float64                 `json:"balance"`
	Reserve         float64                 `json:"reserve"`
	Equity          float64                 `json:"equity"`
	OpenPnL         float64                 `json:"open_pnl"`
	RealizedToday   float64                 `json:"realized_today"`
	OpenCount       int                     `json:"open_count"`
	RecentClosedN   int                     `json:"recent_closed_count"`
	RecentDecisionN int                     `json:"recent_decision_count"`
	OpenPositions   []livePaperPositionView `json:"open_positions,omitempty"`
	RecentClosed    []livePaperClosedView   `json:"recent_closed,omitempty"`
	RecentDecisions []livePaperDecisionView `json:"recent_decisions,omitempty"`
}

type livePaperPositionView struct {
	Symbol         string    `json:"symbol"`
	Side           string    `json:"side"`
	Source         string    `json:"source,omitempty"`
	Mode           string    `json:"mode,omitempty"`
	Strategy       string    `json:"strategy,omitempty"`
	SetupFamily    string    `json:"setup_family,omitempty"`
	Grade          string    `json:"grade,omitempty"`
	State          string    `json:"state,omitempty"`
	TriggerState   string    `json:"trigger_state,omitempty"`
	ExitProfile    string    `json:"exit_profile,omitempty"`
	EntryPrice     float64   `json:"entry_price"`
	MarkPrice      float64   `json:"mark_price"`
	StopPrice      float64   `json:"stop_price"`
	TP1            float64   `json:"tp1,omitempty"`
	TP2            float64   `json:"tp2,omitempty"`
	TP3            float64   `json:"tp3,omitempty"`
	Qty            float64   `json:"qty"`
	Margin         float64   `json:"margin"`
	Leverage       int       `json:"leverage"`
	UnrealizedPnL  float64   `json:"unrealized_pnl"`
	UnrealizedPct  float64   `json:"unrealized_pct"`
	RealizedPnL    float64   `json:"realized_pnl"`
	MFER           float64   `json:"mfe_r"`
	MAER           float64   `json:"mae_r"`
	OpenedAt       time.Time `json:"opened_at"`
	HoldMinutes    float64   `json:"hold_min"`
	EntryReason    string    `json:"entry_reason,omitempty"`
	DecisionReject string    `json:"entry_decision_reject,omitempty"`
}

type livePaperClosedView struct {
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`
	Source       string    `json:"source,omitempty"`
	Mode         string    `json:"mode,omitempty"`
	Strategy     string    `json:"strategy,omitempty"`
	SetupFamily  string    `json:"setup_family,omitempty"`
	Grade        string    `json:"grade,omitempty"`
	State        string    `json:"state,omitempty"`
	TriggerState string    `json:"trigger_state,omitempty"`
	ExitProfile  string    `json:"exit_profile,omitempty"`
	EntryPrice   float64   `json:"entry_price"`
	ExitPrice    float64   `json:"exit_price"`
	PnLUSD       float64   `json:"pnl_usd"`
	PnLPct       float64   `json:"pnl_pct"`
	Fees         float64   `json:"fees"`
	RiskR        float64   `json:"risk_r"`
	HoldMinutes  float64   `json:"hold_min"`
	MFER         float64   `json:"mfe_r"`
	MAER         float64   `json:"mae_r"`
	CaptureRatio float64   `json:"capture_ratio"`
	MaxGivebackR float64   `json:"max_giveback_r"`
	ExitReason   string    `json:"exit_reason,omitempty"`
	ClosedAt     time.Time `json:"closed_at"`
}

type livePaperDecisionView struct {
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`
	Source       string    `json:"source,omitempty"`
	Mode         string    `json:"mode,omitempty"`
	Strategy     string    `json:"strategy,omitempty"`
	SetupFamily  string    `json:"setup_family,omitempty"`
	Grade        string    `json:"grade,omitempty"`
	State        string    `json:"state,omitempty"`
	TriggerState string    `json:"trigger_state,omitempty"`
	ExitProfile  string    `json:"exit_profile,omitempty"`
	Score        float64   `json:"score"`
	Slope        float64   `json:"slope"`
	Confluence   float64   `json:"confluence_score"`
	EntryPrice   float64   `json:"entry_price"`
	StopDistPct  float64   `json:"stop_distance_pct"`
	Approved     bool      `json:"approved"`
	RejectReason string    `json:"reject_reason,omitempty"`
	GateReasons  []string  `json:"gate_reasons,omitempty"`
	DecidedAt    time.Time `json:"decided_at"`
}

func buildLivePaperSnapshot(mode runtimeOperatingMode, p *paperTrader, meta map[string]symbolMeta, eventLog *stats.EventLogger, limit int) *livePaperSnapshot {
	if p == nil || !p.enabled {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	if meta == nil {
		meta = map[string]symbolMeta{}
	}

	now := time.Now().UTC()
	dayKey := now.In(p.reportLoc).Format("2006-01-02")
	realizedToday := 0.0
	if ds := p.dayStats[dayKey]; ds != nil {
		realizedToday = ds.Net
	}

	openRows := make([]livePaperPositionView, 0, len(p.positions))
	openPnL := 0.0
	for raw, pos := range p.positions {
		if pos == nil {
			continue
		}
		mark := meta[raw].LastPrice
		if mark <= 0 {
			mark = pos.LastMark
		}
		if mark <= 0 {
			mark = pos.Entry
		}
		upnl, upct := realizedFromFill(pos.Side, pos.Entry, mark, pos.Qty)
		openPnL += upnl
		openRows = append(openRows, livePaperPositionView{
			Symbol:         strings.ToUpper(strings.TrimSpace(aster.RawSymbol(pos.Symbol))),
			Side:           strings.ToUpper(strings.TrimSpace(pos.Side)),
			Source:         firstNonEmpty(strings.TrimSpace(pos.EntryMode), "paper"),
			Mode:           firstNonEmpty(strings.TrimSpace(pos.EntryMode), "paper"),
			Strategy:       firstNonEmpty(strings.TrimSpace(pos.EntryStrategyID), strings.TrimSpace(pos.EntryReason)),
			SetupFamily:    pos.EntrySetupFamily,
			Grade:          pos.EntryGrade,
			State:          string(pos.EntryState),
			TriggerState:   pos.EntryTrigger,
			ExitProfile:    pos.ExitProfile,
			EntryPrice:     pos.Entry,
			MarkPrice:      mark,
			StopPrice:      pos.Stop,
			TP1:            pos.TP1,
			TP2:            pos.TP2,
			TP3:            pos.TP3,
			Qty:            pos.Qty,
			Margin:         pos.Margin,
			Leverage:       pos.Leverage,
			UnrealizedPnL:  upnl,
			UnrealizedPct:  upct,
			RealizedPnL:    pos.Realized,
			MFER:           pos.MaxFavorableR,
			MAER:           pos.MaxAdverseR,
			OpenedAt:       pos.OpenedAt,
			HoldMinutes:    now.Sub(pos.OpenedAt).Minutes(),
			EntryReason:    pos.EntryReason,
			DecisionReject: pos.EntryDecisionReject,
		})
	}
	sort.Slice(openRows, func(i, j int) bool {
		return openRows[i].OpenedAt.After(openRows[j].OpenedAt)
	})

	recentClosed, recentDecisions := loadRecentPaperViews(eventLog, limit)
	return &livePaperSnapshot{
		Mode:            string(mode),
		Summary:         p.Summary(meta),
		Balance:         p.balance,
		Reserve:         p.reserve,
		Equity:          p.Equity(meta),
		OpenPnL:         openPnL,
		RealizedToday:   realizedToday,
		OpenCount:       len(openRows),
		RecentClosedN:   len(recentClosed),
		RecentDecisionN: len(recentDecisions),
		OpenPositions:   openRows,
		RecentClosed:    recentClosed,
		RecentDecisions: recentDecisions,
	}
}

func loadRecentPaperViews(eventLog *stats.EventLogger, limit int) ([]livePaperClosedView, []livePaperDecisionView) {
	if eventLog == nil || limit <= 0 {
		return []livePaperClosedView{}, []livePaperDecisionView{}
	}
	events := eventLog.Recent(limit * 8)
	if len(events) == 0 {
		return []livePaperClosedView{}, []livePaperDecisionView{}
	}
	closed := make([]livePaperClosedView, 0, limit)
	decisions := make([]livePaperDecisionView, 0, limit)
	for i := len(events) - 1; i >= 0 && (len(closed) < limit || len(decisions) < limit); i-- {
		ev := events[i]
		if !strings.EqualFold(strings.TrimSpace(ev.Source), "paper_auto") && !strings.EqualFold(strings.TrimSpace(ev.Mode), "paper_auto") {
			continue
		}
		switch ev.Type {
		case "POSITION_CLOSE":
			if len(closed) >= limit {
				continue
			}
			closed = append(closed, livePaperClosedView{
				Symbol:       strings.ToUpper(strings.TrimSpace(ev.Symbol)),
				Side:         strings.ToUpper(strings.TrimSpace(ev.Side)),
				Source:       ev.Source,
				Mode:         ev.Mode,
				Strategy:     ev.Strategy,
				SetupFamily:  ev.SetupFamily,
				Grade:        ev.Grade,
				State:        ev.State,
				TriggerState: ev.TriggerState,
				ExitProfile:  ev.ExitProfile,
				EntryPrice:   ev.EntryPx,
				ExitPrice:    ev.ExitPx,
				PnLUSD:       ev.PnLUSD,
				PnLPct:       ev.PnLPct,
				Fees:         ev.Fees,
				RiskR:        ev.RiskR,
				HoldMinutes:  ev.HoldMin,
				MFER:         ev.MFER,
				MAER:         ev.MAER,
				CaptureRatio: ev.CaptureRatio,
				MaxGivebackR: ev.MaxGivebackR,
				ExitReason:   ev.Reason,
				ClosedAt:     ev.Timestamp,
			})
		case "ENTRY_DECISION":
			if len(decisions) >= limit {
				continue
			}
			approved := false
			if ev.GateAllow != nil {
				approved = *ev.GateAllow
			}
			decisions = append(decisions, livePaperDecisionView{
				Symbol:       strings.ToUpper(strings.TrimSpace(ev.Symbol)),
				Side:         strings.ToUpper(strings.TrimSpace(ev.Side)),
				Source:       ev.Source,
				Mode:         ev.Mode,
				Strategy:     ev.Strategy,
				SetupFamily:  ev.SetupFamily,
				Grade:        ev.Grade,
				State:        ev.State,
				TriggerState: ev.TriggerState,
				ExitProfile:  ev.ExitProfile,
				Score:        ev.Score,
				Slope:        ev.Slope,
				Confluence:   ev.ConfluenceScore,
				EntryPrice:   ev.EntryPx,
				StopDistPct:  ev.StopDistPct,
				Approved:     approved,
				RejectReason: ev.Reason,
				GateReasons:  append([]string(nil), ev.GateReasons...),
				DecidedAt:    ev.Timestamp,
			})
		}
	}
	return closed, decisions
}

func cloneLivePaperSnapshot(src *livePaperSnapshot) *livePaperSnapshot {
	if src == nil {
		return nil
	}
	dst := *src
	dst.OpenPositions = append(make([]livePaperPositionView, 0, len(src.OpenPositions)), src.OpenPositions...)
	dst.RecentClosed = append(make([]livePaperClosedView, 0, len(src.RecentClosed)), src.RecentClosed...)
	dst.RecentDecisions = make([]livePaperDecisionView, len(src.RecentDecisions))
	for i := range src.RecentDecisions {
		dst.RecentDecisions[i] = src.RecentDecisions[i]
		dst.RecentDecisions[i].GateReasons = append([]string(nil), src.RecentDecisions[i].GateReasons...)
	}
	return &dst
}
