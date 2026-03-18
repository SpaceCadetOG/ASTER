package backtest

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	exitmgr "go-machine/internal/execution"
	"go-machine/internal/features"
	"go-machine/internal/risk"
	"go-machine/internal/stats"
	"go-machine/internal/strategies"
	"go-machine/internal/ta"
)

type Candle = features.Candle

type ScannerPoint struct {
	Ts    time.Time
	Score float64
	Grade string
	Slope float64
	Accel float64
}

type WhalePoint struct {
	Ts    time.Time
	USD   float64
	IsBuy bool
}

type Config struct {
	Symbol           string
	Strategy         string
	TF               string
	StartBal         float64
	FeesBps          float64
	SlipBps          float64
	Leverage         float64
	MarginUSD        float64
	ReserveUSD       float64
	MaxPos           int
	ScoreMin         float64
	GradeMin         string
	WhaleDelta       float64
	EntryMethod      string // next_open|market_close
	StopMode         string
	TargetMode       string
	VPMinTargetPct   float64
	EventLockoutMin  int
	MaxCorrelatedPos int
	FundingRate      float64
	ExpectedHoldHrs  float64
	MinLiqBufferMult float64
	MaxFundingCostR  float64
	MaxSpreadBps     float64
	SpreadProxyFrac  float64
	TP1Frac          float64
	TP2Frac          float64
	TP3Frac          float64
	TrailAfterTP     int
	TrailStopPct     float64
	TrailStopPctTP3  float64
	TrailPctMin      float64
	BELockBps        float64
	MaxHoldBars      int
}

type Trade struct {
	Symbol        string    `json:"symbol"`
	Strategy      string    `json:"strategy"`
	Side          string    `json:"side"`
	EntryTs       time.Time `json:"entry_ts"`
	ExitTs        time.Time `json:"exit_ts"`
	Entry         float64   `json:"entry"`
	Exit          float64   `json:"exit"`
	Stop          float64   `json:"stop"`
	StopReason    string    `json:"stop_reason,omitempty"`
	TP1           float64   `json:"tp1"`
	TP2           float64   `json:"tp2"`
	TP3           float64   `json:"tp3"`
	Qty           float64   `json:"qty"`
	PnL           float64   `json:"pnl"`
	R             float64   `json:"r"`
	MFER          float64   `json:"mfe_r,omitempty"`
	MAER          float64   `json:"mae_r,omitempty"`
	Reason        string    `json:"reason"`
	HoldMins      float64   `json:"hold_mins"`
	Fees          float64   `json:"fees"`
	Slippage      float64   `json:"slippage"`
	Confidence    float64   `json:"confidence"`
	CandidateRaw  float64   `json:"candidate_score"`
	VPSetup       string    `json:"vp_setup,omitempty"`
	VPLevel       float64   `json:"vp_level,omitempty"`
	VPTargetLevel float64   `json:"vp_target_level,omitempty"`
	VPStopMode    string    `json:"vp_stop_mode,omitempty"`
	VPTargetMode  string    `json:"vp_target_mode,omitempty"`
	RejectReason  string    `json:"reject_reason,omitempty"`
	RegimeTag     string    `json:"regime_tag,omitempty"`
	Reasons       string    `json:"reasons,omitempty"`
	SignalSource  string    `json:"signal_source,omitempty"`
	FundingImpact float64   `json:"funding_impact,omitempty"`
	LiqBufferOK   bool      `json:"liq_buffer_ok,omitempty"`
}

type btPosition struct {
	Trade        Trade
	InitialQty   float64
	RemainingQty float64
	HitTP1       bool
	HitTP2       bool
	HitTP3       bool
	TrailOn      bool
	TrailRef     float64
	TrailStop    float64
	BarsHeld     int
}

type Report struct {
	Symbol      string  `json:"symbol"`
	Strategy    string  `json:"strategy"`
	TF          string  `json:"tf"`
	StartBal    float64 `json:"start_balance"`
	FinalBal    float64 `json:"final_balance"`
	TradeCount  int     `json:"trade_count"`
	WinRate     float64 `json:"win_rate"`
	ProfitFact  float64 `json:"profit_factor"`
	MaxDD       float64 `json:"max_drawdown_pct"`
	AvgR        float64 `json:"avg_r"`
	Sharpe      float64 `json:"sharpe"`
	ExposurePct float64 `json:"exposure_pct"`
	AvgHoldMin  float64 `json:"avg_hold_min"`
}

type Result struct {
	Trades    []Trade               `json:"trades"`
	Report    Report                `json:"report"`
	Events    []stats.Event         `json:"events,omitempty"`
	Metrics   stats.Report          `json:"metrics"`
	Readiness stats.ReadinessReport `json:"readiness"`
}

func Run(cfg Config, candles []Candle, scans []ScannerPoint, whales []WhalePoint) (Result, error) {
	if len(candles) < 5 {
		return Result{}, fmt.Errorf("not enough candles")
	}
	if cfg.MaxPos <= 0 {
		cfg.MaxPos = 1
	}
	if cfg.MarginUSD <= 0 {
		cfg.MarginUSD = 10
	}
	if cfg.ReserveUSD <= 0 {
		cfg.ReserveUSD = 5
	}
	if cfg.StartBal <= 0 {
		cfg.StartBal = 1000
	}
	if cfg.Leverage <= 0 {
		cfg.Leverage = 3
	}
	if cfg.EntryMethod == "" {
		cfg.EntryMethod = "next_open"
	}
	if cfg.Strategy == "" {
		cfg.Strategy = "router"
	}
	if cfg.ExpectedHoldHrs <= 0 {
		cfg.ExpectedHoldHrs = 8
	}
	if cfg.MinLiqBufferMult <= 0 {
		cfg.MinLiqBufferMult = 2.5
	}
	if cfg.MaxFundingCostR <= 0 {
		cfg.MaxFundingCostR = 0.25
	}
	if cfg.MaxSpreadBps <= 0 {
		cfg.MaxSpreadBps = 20
	}
	if cfg.SpreadProxyFrac <= 0 {
		cfg.SpreadProxyFrac = 0.05
	}
	if cfg.TP1Frac < 0 {
		cfg.TP1Frac = 0
	}
	if cfg.TP2Frac < 0 {
		cfg.TP2Frac = 0
	}
	if cfg.TP3Frac < 0 {
		cfg.TP3Frac = 0
	}
	sumFrac := cfg.TP1Frac + cfg.TP2Frac + cfg.TP3Frac
	if sumFrac <= 0 || sumFrac > 1 {
		cfg.TP1Frac, cfg.TP2Frac, cfg.TP3Frac = 0.35, 0.25, 0.20
	}
	if cfg.TrailAfterTP < 1 {
		cfg.TrailAfterTP = 3
	}
	if cfg.TrailAfterTP > 3 {
		cfg.TrailAfterTP = 3
	}
	if cfg.TrailStopPct <= 0 {
		cfg.TrailStopPct = 1.50
	}
	if cfg.TrailStopPctTP3 <= 0 {
		cfg.TrailStopPctTP3 = 3.25
	}
	if cfg.TrailPctMin <= 0 {
		cfg.TrailPctMin = cfg.TrailStopPct
	}
	if cfg.BELockBps < 0 {
		cfg.BELockBps = 0
	}
	if cfg.MaxHoldBars <= 0 {
		cfg.MaxHoldBars = 120
	}

	fe := features.NewEngine(features.Config{})
	router := strategies.NewRouter(strategies.RouterConfig{
		MinGrade:                  cfg.GradeMin,
		MinScore:                  cfg.ScoreMin,
		MinWhaleDelta:             cfg.WhaleDelta,
		AllowWarmup:               true,
		WarmupSlopeMin:            0.01,
		MaxOne:                    true,
		EnableVPSetups:            true,
		MinVPConfidence:           0.55,
		UseVPReversal:             true,
		EnableInstitutionalPA:     true,
		UseSessionRegimeRisk:      true,
		AllowDeadZoneOnlyAPlus:    true,
		MinConfluenceScore:        0.58,
		RejectIfTargetTooClosePct: cfg.VPMinTargetPct,
		RiskPolicy: strategies.RiskPolicyConfig{
			StopMode:             strategies.StopMode(strings.ToLower(strings.TrimSpace(cfg.StopMode))),
			TargetMode:           strategies.TargetMode(strings.ToLower(strings.TrimSpace(cfg.TargetMode))),
			MinTargetDistancePct: cfg.VPMinTargetPct,
		},
	})

	balance := cfg.StartBal
	equity := make([]float64, 0, len(candles))
	trades := []Trade{}
	events := []stats.Event{}
	emit := func(e stats.Event) {
		e.Simulated = true
		if e.Timestamp.IsZero() {
			e.Timestamp = time.Now().UTC()
		}
		events = append(events, e)
	}
	var open *btPosition
	var pending strategies.Candidate
	var pendingRisk risk.Decision
	entryPending := false
	holdBars := 0

	var scan ScannerPoint
	scanIdx := 0
	whaleIdx := 0

	for i := 0; i < len(candles); i++ {
		c := candles[i]

		for scanIdx < len(scans) && !scans[scanIdx].Ts.After(c.Ts) {
			scan = scans[scanIdx]
			scanIdx++
		}

		newWhales := make([]features.WhaleEvent, 0, 8)
		for whaleIdx < len(whales) && !whales[whaleIdx].Ts.After(c.Ts) {
			w := whales[whaleIdx]
			newWhales = append(newWhales, features.WhaleEvent{Ts: w.Ts, USD: w.USD, IsBuy: w.IsBuy})
			whaleIdx++
		}
		fe.AddWhales(newWhales, c.Ts)
		snap := fe.Eval(candles[:i+1])

		if entryPending && open == nil {
			entryPx := c.O
			if cfg.EntryMethod == "market_close" {
				entryPx = c.C
			}
			if entryPx > 0 {
				stopPx := nonZero(pending.Signal.Stop, deriveBacktestStop(pending.Signal, entryPx))
				stopReason := ""
				if strings.EqualFold(cfg.StopMode, "hybrid") {
					micro := ta.SnapshotFromFeatureCandles(candles[:i+1], 14, 3, 15, 20)
					structLow, structHigh := recentStructureBand(candles[:i+1], 8)
					hcfg := exitmgr.DefaultHybridStopConfig()
					hcfg.Enabled = true
					res := exitmgr.ComputeHybridStop(hcfg, exitmgr.HybridStopInput{
						Side:          signalSideToOrder(pending.Signal.Side),
						Entry:         entryPx,
						SignalStop:    pending.Signal.Stop,
						StructureLow:  structLow,
						StructureHigh: structHigh,
						SessionVWAP:   micro.SessionVWAP,
						EMA9:          micro.EMA9,
						ATR:           micro.ATR,
						TargetPrice:   nonZero(pending.Signal.TP1, pending.Signal.TP2),
						Template:      hybridTemplateForSignal(pending.Signal.Name),
					})
					if res.Rejected {
						f := false
						emit(stats.Event{
							Timestamp:   c.Ts,
							Type:        "GATE_DECISION",
							Symbol:      cfg.Symbol,
							Side:        signalSideToOrder(pending.Signal.Side),
							Strategy:    pending.Signal.Name,
							Score:       scan.Score,
							Slope:       scan.Slope,
							GateAllow:   &f,
							GateReasons: []string{res.RejectReason},
							Reason:      "hybrid_stop_rejected",
						})
						entryPending = false
						eq := balance
						equity = append(equity, eq)
						continue
					}
					if res.StopPrice > 0 {
						stopPx = res.StopPrice
						stopReason = res.StopReason
					}
				}
				notional := cfg.MarginUSD * cfg.Leverage
				qty := notional / entryPx
				fee := notional * cfg.FeesBps / 10000.0
				slip := notional * cfg.SlipBps / 10000.0
				balance -= fee + slip
				trade := Trade{
					Symbol:        cfg.Symbol,
					Strategy:      pending.Signal.Name,
					Side:          string(pending.Signal.Side),
					EntryTs:       c.Ts,
					Entry:         entryPx,
					Stop:          stopPx,
					StopReason:    stopReason,
					TP1:           pending.Signal.TP1,
					TP2:           pending.Signal.TP2,
					TP3:           nonZero(pending.Signal.TP3, pending.Signal.TP2),
					Qty:           qty,
					Fees:          fee,
					Slippage:      slip,
					Confidence:    pending.Signal.Confidence,
					CandidateRaw:  pending.Score,
					VPSetup:       pending.Signal.VPSetup,
					VPLevel:       pending.Signal.VPLevel,
					VPTargetLevel: pending.Signal.VPTargetLevel,
					VPStopMode:    pending.Signal.StopMode,
					VPTargetMode:  pending.Signal.TargetMode,
					RejectReason:  pending.Signal.RejectReason,
					RegimeTag:     pending.Signal.RegimeTag,
					Reasons:       strings.Join(pending.Signal.Reasons, "|"),
					SignalSource:  strings.Join(pending.Signal.SignalSource, "|"),
					LiqBufferOK:   pendingRisk.LiqBufferOK,
				}
				open = &btPosition{
					Trade:        trade,
					InitialQty:   qty,
					RemainingQty: qty,
				}
				emit(stats.Event{
					Timestamp:   c.Ts,
					Type:        "POSITION_OPEN",
					Symbol:      cfg.Symbol,
					Side:        open.Trade.Side,
					Strategy:    open.Trade.Strategy,
					Score:       scan.Score,
					Slope:       scan.Slope,
					EntryPx:     open.Trade.Entry,
					Discovery:   open.Trade.CandidateRaw,
					TriggerRef:  cfg.EntryMethod,
					Reason:      "backtest_entry_open",
					VolumeRatio: snap.Flow.VolumeZ,
				})
			}
			entryPending = false
		}

		if open != nil {
			open.BarsHeld++
			updateBacktestExcursions(open, c)
			stopPx := backtestProtectedStop(open)
			if backtestBarHitsLevel(open.Trade.Side, c, stopPx) {
				reason := "SL"
				if open.TrailOn && open.TrailStop > 0 {
					reason = "TRAIL_STOP"
				}
				exitPx := stopPx
				if open != nil {
					prevPnL := open.Trade.PnL
					if closeTrade, closed := realizeBacktestExit(open, cfg, c.Ts, exitPx, open.RemainingQty, reason, true); closed {
						balance += closeTrade.PnL - prevPnL
						emitBacktestCloseEvent(emit, c.Ts, closeTrade, scan, snap.Flow.VolumeZ)
						trades = append(trades, closeTrade)
						open = nil
					}
				}
			} else if open != nil {
				if !open.HitTP1 && backtestBarHitsLevel(open.Trade.Side, c, open.Trade.TP1) {
					qty := backtestTargetQty(open.InitialQty, cfg.TP1Frac, open.RemainingQty)
					open.HitTP1 = true
					if qty > 0 {
						emit(stats.Event{
							Timestamp:  c.Ts,
							Type:       "ORDER_FILL",
							Symbol:     open.Trade.Symbol,
							Side:       open.Trade.Side,
							Strategy:   open.Trade.Strategy,
							Reason:     "TP1",
							EntryPx:    open.Trade.Entry,
							ExitPx:     open.Trade.TP1,
							HoldMin:    c.Ts.Sub(open.Trade.EntryTs).Minutes(),
							Discovery:  open.Trade.CandidateRaw,
							TriggerRef: open.Trade.StopReason,
						})
						prevPnL := open.Trade.PnL
						if closeTrade, closed := realizeBacktestExit(open, cfg, c.Ts, open.Trade.TP1, qty, "TP1", false); closed {
							balance += closeTrade.PnL - prevPnL
							emitBacktestCloseEvent(emit, c.Ts, closeTrade, scan, snap.Flow.VolumeZ)
							trades = append(trades, closeTrade)
							open = nil
						} else {
							balance += open.Trade.PnL - prevPnL
						}
					}
					if open != nil && cfg.BELockBps > 0 {
						be := backtestBELockPrice(open.Trade.Side, open.Trade.Entry, cfg.BELockBps)
						if improvesStop(open.Trade.Side, be, open.Trade.Stop) {
							open.Trade.Stop = be
						}
					}
				}
				if open != nil && !open.HitTP2 && backtestBarHitsLevel(open.Trade.Side, c, open.Trade.TP2) {
					qty := backtestTargetQty(open.InitialQty, cfg.TP2Frac, open.RemainingQty)
					open.HitTP2 = true
					if qty > 0 {
						emit(stats.Event{
							Timestamp:  c.Ts,
							Type:       "ORDER_FILL",
							Symbol:     open.Trade.Symbol,
							Side:       open.Trade.Side,
							Strategy:   open.Trade.Strategy,
							Reason:     "TP2",
							EntryPx:    open.Trade.Entry,
							ExitPx:     open.Trade.TP2,
							HoldMin:    c.Ts.Sub(open.Trade.EntryTs).Minutes(),
							Discovery:  open.Trade.CandidateRaw,
							TriggerRef: open.Trade.StopReason,
						})
						prevPnL := open.Trade.PnL
						if closeTrade, closed := realizeBacktestExit(open, cfg, c.Ts, open.Trade.TP2, qty, "TP2", false); closed {
							balance += closeTrade.PnL - prevPnL
							emitBacktestCloseEvent(emit, c.Ts, closeTrade, scan, snap.Flow.VolumeZ)
							trades = append(trades, closeTrade)
							open = nil
						} else {
							balance += open.Trade.PnL - prevPnL
						}
					}
					if open != nil && cfg.TrailAfterTP <= 2 {
						open.TrailOn = true
						open.TrailRef = open.Trade.TP2
						open.TrailStop = backtestCalcTrailStop(cfg, open, open.Trade.TP2, false)
					}
				}
				if open != nil && !open.HitTP3 && backtestBarHitsLevel(open.Trade.Side, c, open.Trade.TP3) {
					qty := backtestTargetQty(open.InitialQty, cfg.TP3Frac, open.RemainingQty)
					open.HitTP3 = true
					if qty > 0 {
						emit(stats.Event{
							Timestamp:  c.Ts,
							Type:       "ORDER_FILL",
							Symbol:     open.Trade.Symbol,
							Side:       open.Trade.Side,
							Strategy:   open.Trade.Strategy,
							Reason:     "TP3",
							EntryPx:    open.Trade.Entry,
							ExitPx:     open.Trade.TP3,
							HoldMin:    c.Ts.Sub(open.Trade.EntryTs).Minutes(),
							Discovery:  open.Trade.CandidateRaw,
							TriggerRef: open.Trade.StopReason,
						})
						prevPnL := open.Trade.PnL
						if closeTrade, closed := realizeBacktestExit(open, cfg, c.Ts, open.Trade.TP3, qty, "TP3", false); closed {
							balance += closeTrade.PnL - prevPnL
							emitBacktestCloseEvent(emit, c.Ts, closeTrade, scan, snap.Flow.VolumeZ)
							trades = append(trades, closeTrade)
							open = nil
						} else {
							balance += open.Trade.PnL - prevPnL
						}
					}
					if open != nil && cfg.TrailAfterTP <= 3 {
						open.TrailOn = true
						open.TrailRef = open.Trade.TP3
						open.TrailStop = backtestCalcTrailStop(cfg, open, open.Trade.TP3, true)
					}
				}
				if open != nil && open.TrailOn {
					favorableRef := backtestFavorableRef(open.Trade.Side, c)
					if advancesTrail(open.Trade.Side, favorableRef, open.TrailRef) {
						open.TrailRef = favorableRef
						open.TrailStop = backtestCalcTrailStop(cfg, open, favorableRef, open.HitTP3)
					}
				}
				if open != nil && open.BarsHeld >= cfg.MaxHoldBars {
					prevPnL := open.Trade.PnL
					if closeTrade, closed := realizeBacktestExit(open, cfg, c.Ts, c.C, open.RemainingQty, "timeout", true); closed {
						balance += closeTrade.PnL - prevPnL
						emitBacktestCloseEvent(emit, c.Ts, closeTrade, scan, snap.Flow.VolumeZ)
						trades = append(trades, closeTrade)
						open = nil
					}
				}
			}
		}

		if open == nil && !entryPending && i+1 < len(candles) {
			if inEventLockout(c.Ts, cfg.EventLockoutMin) {
				eq := balance
				equity = append(equity, eq)
				continue
			}
			usable := balance - cfg.ReserveUSD
			if usable >= cfg.MarginUSD {
				ctx := strategies.Context{
					Symbol:       cfg.Symbol,
					TF:           cfg.TF,
					ScannerScore: scan.Score,
					ScannerGrade: scan.Grade,
					ScoreSlope:   scan.Slope,
					ScanAccel:    scan.Accel,
					Snapshot:     snap,
					Candles:      candles[:i+1],
				}
				cands := evalCandidates(cfg.Strategy, router, ctx)
				if len(cands) > 0 {
					emit(stats.Event{
						Timestamp:   c.Ts,
						Type:        "SIGNAL",
						Symbol:      cfg.Symbol,
						Side:        signalSideToOrder(cands[0].Signal.Side),
						Strategy:    cands[0].Signal.Name,
						Score:       scan.Score,
						Slope:       scan.Slope,
						EntryPx:     nonZero(cands[0].Signal.Entry, c.C),
						Discovery:   cands[0].Score,
						TriggerRef:  cfg.EntryMethod,
						Reason:      strings.Join(cands[0].Signal.Reasons, "|"),
						VolumeRatio: snap.Flow.VolumeZ,
					})
					dec := risk.Approve(risk.Config{
						Enabled:              true,
						MinLiqBufferMult:     cfg.MinLiqBufferMult,
						MaxFundingCostR:      cfg.MaxFundingCostR,
						MaxSpreadBps:         cfg.MaxSpreadBps,
						MinBookImbalance:     1.0,
						MaxRecentSlippageBps: math.Max(cfg.SlipBps*3, 20),
					}, risk.Input{
						Side:              signalSideToOrder(cands[0].Signal.Side),
						Entry:             nonZero(cands[0].Signal.Entry, c.C),
						Stop:              deriveBacktestStop(cands[0].Signal, c.C),
						Leverage:          cfg.Leverage,
						NotionalUSD:       cfg.MarginUSD * cfg.Leverage,
						FundingRate:       cfg.FundingRate,
						HoldHours:         cfg.ExpectedHoldHrs,
						SpreadBps:         spreadProxyBps(c, cfg.SpreadProxyFrac),
						BookImbalance:     1.1,
						RecentSlippageBps: cfg.SlipBps,
						VenueHealthy:      true,
					})
					if dec.Approved {
						t := true
						emit(stats.Event{
							Timestamp:  c.Ts,
							Type:       "GATE_DECISION",
							Symbol:     cfg.Symbol,
							Side:       signalSideToOrder(cands[0].Signal.Side),
							Strategy:   cands[0].Signal.Name,
							Score:      scan.Score,
							Slope:      scan.Slope,
							Discovery:  cands[0].Score,
							GateAllow:  &t,
							TriggerRef: cfg.StopMode,
							Reason:     "risk_approved",
						})
						pending = cands[0]
						pendingRisk = dec
						entryPending = true
					} else {
						f := false
						reasons := []string{firstNonEmpty(dec.RejectReason, "risk_reject")}
						emit(stats.Event{
							Timestamp:   c.Ts,
							Type:        "GATE_DECISION",
							Symbol:      cfg.Symbol,
							Side:        signalSideToOrder(cands[0].Signal.Side),
							Strategy:    cands[0].Signal.Name,
							Score:       scan.Score,
							Slope:       scan.Slope,
							Discovery:   cands[0].Score,
							GateAllow:   &f,
							GateReasons: reasons,
							Reason:      "risk_rejected",
						})
					}
				}
			}
		}

		eq := balance
		if open != nil {
			if open.Trade.Side == string(features.SideLong) {
				eq += (c.C - open.Trade.Entry) * open.RemainingQty
			} else {
				eq += (open.Trade.Entry - c.C) * open.RemainingQty
			}
			holdBars++
		}
		equity = append(equity, eq)
	}

	rep := summarize(cfg, trades, equity, balance, holdBars, len(candles))
	metrics := stats.Aggregate(events)
	readiness := stats.EvaluateReadiness(metrics, stats.DefaultReadinessConfig())
	return Result{Trades: trades, Report: rep, Events: events, Metrics: metrics, Readiness: readiness}, nil
}

func evalCandidates(name string, router *strategies.Router, ctx strategies.Context) []strategies.Candidate {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "router" {
		return router.Eval(ctx)
	}
	var strat strategies.Strategy
	switch name {
	case "lsr":
		strat = strategies.LSR{}
	case "bos_pb":
		strat = strategies.BOSPB{}
	case "ob_r":
		strat = strategies.OBR{}
	case "fvg_c":
		strat = strategies.FVGC{}
	case "fa":
		strat = strategies.FailedAuction{}
	case "od":
		strat = strategies.OpenDrive{}
	case "vp_accumulation":
		strat = strategies.VPAccumulation{}
	case "vp_trend":
		strat = strategies.VPTrendRetest{}
	case "vp_rejection":
		strat = strategies.VPRejection{}
	case "vp_reversal":
		strat = strategies.VPReversal{}
	case "daily_open_sr":
		strat = strategies.DailyOpenSR{}
	case "pd_levels_retest":
		strat = strategies.PDLevelsRetest{}
	case "failed_auction_magnet":
		strat = strategies.FailedAuctionMagnetStrategy{}
	case "vwap_confluence":
		strat = strategies.VWAPConfluenceStrategy{}
	default:
		return nil
	}
	sig := strat.Eval(ctx)
	if !sig.Active {
		return nil
	}
	sig = strategies.ApplyRiskPolicy(sig, ctx.Snapshot, strategies.DefaultRiskPolicy())
	scoreNorm := ctx.ScannerScore / 100.0
	if scoreNorm < 0 {
		scoreNorm = 0
	}
	if scoreNorm > 1.5 {
		scoreNorm = 1.5
	}
	whaleBoost := 1.0
	if ctx.Snapshot.Flow.WhaleDelta1m > 0 && sig.Side == features.SideLong {
		whaleBoost = 1.1
	}
	if ctx.Snapshot.Flow.WhaleDelta1m < 0 && sig.Side == features.SideShort {
		whaleBoost = 1.1
	}
	return []strategies.Candidate{{Signal: sig, Score: sig.Confidence * scoreNorm * whaleBoost}}
}

func updateBacktestExcursions(pos *btPosition, c Candle) {
	if pos == nil {
		return
	}
	risk := math.Abs(pos.Trade.Entry-pos.Trade.Stop) * pos.InitialQty
	if risk <= 1e-9 {
		return
	}
	if strings.EqualFold(pos.Trade.Side, string(features.SideLong)) || strings.EqualFold(pos.Trade.Side, "BUY") {
		pos.Trade.MFER = math.Max(pos.Trade.MFER, (c.H-pos.Trade.Entry)*pos.InitialQty/risk)
		pos.Trade.MAER = math.Max(pos.Trade.MAER, (pos.Trade.Entry-c.L)*pos.InitialQty/risk)
		return
	}
	pos.Trade.MFER = math.Max(pos.Trade.MFER, (pos.Trade.Entry-c.L)*pos.InitialQty/risk)
	pos.Trade.MAER = math.Max(pos.Trade.MAER, (c.H-pos.Trade.Entry)*pos.InitialQty/risk)
}

func backtestProtectedStop(pos *btPosition) float64 {
	if pos == nil {
		return 0
	}
	stop := pos.Trade.Stop
	if pos.TrailOn && pos.TrailStop > 0 {
		if strings.EqualFold(pos.Trade.Side, string(features.SideLong)) || strings.EqualFold(pos.Trade.Side, "BUY") {
			if pos.TrailStop > stop {
				stop = pos.TrailStop
			}
		} else if stop <= 0 || pos.TrailStop < stop {
			stop = pos.TrailStop
		}
	}
	return stop
}

func backtestBarHitsLevel(side string, c Candle, level float64) bool {
	if level <= 0 {
		return false
	}
	if strings.EqualFold(side, string(features.SideLong)) || strings.EqualFold(side, "BUY") {
		return c.H >= level
	}
	return c.L <= level
}

func backtestTargetQty(initialQty, frac, remaining float64) float64 {
	if initialQty <= 0 || remaining <= 0 {
		return 0
	}
	if frac <= 0 {
		return remaining
	}
	q := initialQty * frac
	if q > remaining {
		q = remaining
	}
	if q <= 0 {
		return remaining
	}
	return q
}

func improvesStop(side string, candidate, current float64) bool {
	if candidate <= 0 {
		return false
	}
	if strings.EqualFold(side, string(features.SideLong)) || strings.EqualFold(side, "BUY") {
		return candidate > current
	}
	return current <= 0 || candidate < current
}

func backtestBELockPrice(side string, entry, beLockBps float64) float64 {
	if entry <= 0 {
		return 0
	}
	d := beLockBps / 10000.0
	if strings.EqualFold(side, string(features.SideLong)) || strings.EqualFold(side, "BUY") {
		return entry * (1 + d)
	}
	return entry * (1 - d)
}

func backtestFavorableRef(side string, c Candle) float64 {
	if strings.EqualFold(side, string(features.SideLong)) || strings.EqualFold(side, "BUY") {
		return c.H
	}
	return c.L
}

func advancesTrail(side string, candidate, current float64) bool {
	if candidate <= 0 {
		return false
	}
	if current <= 0 {
		return true
	}
	if strings.EqualFold(side, string(features.SideLong)) || strings.EqualFold(side, "BUY") {
		return candidate > current
	}
	return candidate < current
}

func backtestCalcTrailStop(cfg Config, pos *btPosition, ref float64, postTP3 bool) float64 {
	if ref <= 0 {
		return 0
	}
	pct := cfg.TrailStopPct / 100.0
	if postTP3 && cfg.TrailStopPctTP3 > 0 {
		pct = cfg.TrailStopPctTP3 / 100.0
	}
	if pct <= 0 {
		pct = 0.015
	}
	dist := ref * pct
	if floor := ref * (cfg.TrailPctMin / 100.0); floor > dist {
		dist = floor
	}
	if strings.EqualFold(pos.Trade.Side, string(features.SideLong)) || strings.EqualFold(pos.Trade.Side, "BUY") {
		return ref - dist
	}
	return ref + dist
}

func realizeBacktestExit(pos *btPosition, cfg Config, ts time.Time, exitPx, qty float64, reason string, final bool) (Trade, bool) {
	if pos == nil || qty <= 0 || pos.RemainingQty <= 0 {
		return Trade{}, false
	}
	if qty > pos.RemainingQty {
		qty = pos.RemainingQty
	}
	notional := exitPx * qty
	fee := notional * cfg.FeesBps / 10000.0
	slip := notional * cfg.SlipBps / 10000.0
	pnl := 0.0
	if strings.EqualFold(pos.Trade.Side, string(features.SideLong)) || strings.EqualFold(pos.Trade.Side, "BUY") {
		pnl = (exitPx-pos.Trade.Entry)*qty - fee - slip
	} else {
		pnl = (pos.Trade.Entry-exitPx)*qty - fee - slip
	}
	funding := fundingImpact(pos.Trade.Side, cfg.FundingRate, pos.Trade.Entry*qty, ts.Sub(pos.Trade.EntryTs).Hours())
	pnl += funding
	pos.Trade.PnL += pnl
	pos.Trade.Fees += fee
	pos.Trade.Slippage += slip
	pos.Trade.FundingImpact += funding
	pos.RemainingQty = math.Max(0, pos.RemainingQty-qty)
	if pos.RemainingQty <= 1e-9 {
		final = true
	}
	if !final {
		return Trade{}, false
	}
	risk := math.Abs(pos.Trade.Entry-pos.Trade.Stop) * pos.InitialQty
	r := 0.0
	if risk > 1e-9 {
		r = pos.Trade.PnL / risk
	}
	pos.Trade.ExitTs = ts
	pos.Trade.Exit = exitPx
	pos.Trade.R = r
	pos.Trade.Reason = reason
	pos.Trade.HoldMins = ts.Sub(pos.Trade.EntryTs).Minutes()
	pos.Trade.Qty = pos.InitialQty
	return pos.Trade, true
}

func emitBacktestCloseEvent(emit func(stats.Event), ts time.Time, trade Trade, scan ScannerPoint, volumeRatio float64) {
	if emit == nil {
		return
	}
	emit(stats.Event{
		Timestamp:   ts,
		Type:        "POSITION_CLOSE",
		Symbol:      trade.Symbol,
		Side:        trade.Side,
		Strategy:    trade.Strategy,
		Reason:      trade.Reason,
		Score:       scan.Score,
		Slope:       scan.Slope,
		EntryPx:     trade.Entry,
		ExitPx:      trade.Exit,
		RiskR:       trade.R,
		HoldMin:     trade.HoldMins,
		MFER:        trade.MFER,
		MAER:        trade.MAER,
		PnLUSD:      trade.PnL,
		Fees:        trade.Fees,
		Slippage:    trade.Slippage,
		Discovery:   trade.CandidateRaw,
		TriggerRef:  trade.StopReason,
		VolumeRatio: volumeRatio,
	})
}

func summarize(cfg Config, trades []Trade, equity []float64, finalBal float64, holdBars, totalBars int) Report {
	wins := 0
	gp, gl, sumR, hold := 0.0, 0.0, 0.0, 0.0
	rets := make([]float64, 0, len(trades))
	for _, t := range trades {
		if t.PnL >= 0 {
			wins++
			gp += t.PnL
		} else {
			gl += -t.PnL
		}
		sumR += t.R
		hold += t.HoldMins
		rets = append(rets, t.R)
	}
	winRate := 0.0
	if len(trades) > 0 {
		winRate = 100 * float64(wins) / float64(len(trades))
	}
	pf := 0.0
	if gl > 0 {
		pf = gp / gl
	}
	avgR := 0.0
	avgHold := 0.0
	if len(trades) > 0 {
		avgR = sumR / float64(len(trades))
		avgHold = hold / float64(len(trades))
	}
	exposure := 0.0
	if totalBars > 0 {
		exposure = 100 * float64(holdBars) / float64(totalBars)
	}
	return Report{
		Symbol:      cfg.Symbol,
		Strategy:    cfg.Strategy,
		TF:          cfg.TF,
		StartBal:    cfg.StartBal,
		FinalBal:    finalBal,
		TradeCount:  len(trades),
		WinRate:     winRate,
		ProfitFact:  pf,
		MaxDD:       maxDD(equity),
		AvgR:        avgR,
		Sharpe:      sharpe(rets),
		ExposurePct: exposure,
		AvgHoldMin:  avgHold,
	}
}

func maxDD(eq []float64) float64 {
	if len(eq) == 0 {
		return 0
	}
	peak, dd := eq[0], 0.0
	for _, e := range eq {
		if e > peak {
			peak = e
		}
		if peak > 0 {
			x := 100 * (peak - e) / peak
			if x > dd {
				dd = x
			}
		}
	}
	return dd
}

func sharpe(rs []float64) float64 {
	if len(rs) < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range rs {
		mean += r
	}
	mean /= float64(len(rs))
	var v float64
	for _, r := range rs {
		d := r - mean
		v += d * d
	}
	v /= float64(len(rs) - 1)
	if v <= 1e-9 {
		return 0
	}
	return mean / sqrt(v)
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 6; i++ {
		z = 0.5 * (z + x/z)
	}
	return z
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func WriteOutputs(res Result, outDir string) error {
	if outDir == "" {
		outDir = "out"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	csvPath := filepath.Join(outDir, "trades.csv")
	jsonPath := filepath.Join(outDir, "report.json")
	metricsPath := filepath.Join(outDir, "metrics.json")
	readinessPath := filepath.Join(outDir, "readiness.json")
	eventsPath := filepath.Join(outDir, "events.jsonl")

	f, err := os.Create(csvPath)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"symbol", "strategy", "side", "entry_ts", "exit_ts", "entry", "exit", "stop", "stop_reason", "tp1", "tp2", "tp3", "qty", "pnl", "r", "mfe_r", "mae_r", "reason", "hold_mins", "fees", "slippage", "funding_impact", "liq_buffer_ok", "confidence", "candidate_score", "vp_setup", "vp_level", "vp_target_level", "vp_stop_mode", "vp_target_mode", "reject_reason", "regime_tag", "signal_reasons", "signal_source"})
	for _, t := range res.Trades {
		_ = w.Write([]string{t.Symbol, t.Strategy, t.Side, t.EntryTs.Format(time.RFC3339), t.ExitTs.Format(time.RFC3339), f64(t.Entry), f64(t.Exit), f64(t.Stop), t.StopReason, f64(t.TP1), f64(t.TP2), f64(t.TP3), f64(t.Qty), f64(t.PnL), f64(t.R), f64(t.MFER), f64(t.MAER), t.Reason, f64(t.HoldMins), f64(t.Fees), f64(t.Slippage), f64(t.FundingImpact), fmt.Sprintf("%t", t.LiqBufferOK), f64(t.Confidence), f64(t.CandidateRaw), t.VPSetup, f64(t.VPLevel), f64(t.VPTargetLevel), t.VPStopMode, t.VPTargetMode, t.RejectReason, t.RegimeTag, t.Reasons, t.SignalSource})
	}
	w.Flush()
	_ = f.Close()

	b, _ := json.MarshalIndent(res.Report, "", "  ")
	if err := os.WriteFile(jsonPath, b, 0o644); err != nil {
		return err
	}
	mb, _ := json.MarshalIndent(res.Metrics, "", "  ")
	if err := os.WriteFile(metricsPath, mb, 0o644); err != nil {
		return err
	}
	rb, _ := json.MarshalIndent(res.Readiness, "", "  ")
	if err := os.WriteFile(readinessPath, rb, 0o644); err != nil {
		return err
	}
	ef, err := os.Create(eventsPath)
	if err != nil {
		return err
	}
	defer ef.Close()
	bw := bufio.NewWriter(ef)
	enc := json.NewEncoder(bw)
	for _, evt := range res.Events {
		if err := enc.Encode(evt); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return nil
}

func WriteBatchSummary(path string, reports []Report) error {
	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Symbol == reports[j].Symbol {
			return reports[i].Strategy < reports[j].Strategy
		}
		return reports[i].Symbol < reports[j].Symbol
	})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(reports, "", "  ")
	return os.WriteFile(path, b, 0o644)
}

func f64(x float64) string {
	s := fmt.Sprintf("%.10f", x)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

func inEventLockout(ts time.Time, lockMin int) bool {
	if lockMin <= 0 {
		return false
	}
	m := ts.UTC().Minute()
	return m < lockMin || m >= (60-lockMin)
}

func deriveBacktestStop(sig strategies.Signal, mark float64) float64 {
	if sig.Stop > 0 {
		return sig.Stop
	}
	if sig.Side == features.SideShort {
		return mark * 1.006
	}
	return mark * 0.994
}

func nonZero(v, fallback float64) float64 {
	if v > 0 {
		return v
	}
	return fallback
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func spreadProxyBps(c Candle, frac float64) float64 {
	if c.C <= 0 || c.H <= 0 || c.L <= 0 {
		return 0
	}
	if frac <= 0 {
		frac = 0.05
	}
	return math.Abs(c.H-c.L) / c.C * 10000.0 * frac
}

func fundingImpact(side string, fr, notional, holdH float64) float64 {
	if fr == 0 || notional <= 0 {
		return 0
	}
	if holdH <= 0 {
		holdH = 8
	}
	f := math.Abs(fr) * notional * (holdH / 8.0)
	if strings.EqualFold(side, string(features.SideLong)) || strings.EqualFold(side, "BUY") {
		if fr > 0 {
			return -f
		}
		return f
	}
	if fr < 0 {
		return -f
	}
	return f
}

func signalSideToOrder(s features.Side) string {
	if s == features.SideShort {
		return "SELL"
	}
	return "BUY"
}

func hybridTemplateForSignal(name string) exitmgr.StopTemplate {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "continuation_fast", "momentum_ignite_long", "momentum_ignite_short":
		return exitmgr.StopTemplateContinuationImpulse
	case "fa", "failed_auction_magnet", "vwap_confluence", "bos_pb", "open_drive":
		return exitmgr.StopTemplateReclaimPullback
	case "mom_reversal", "mom_reversal_short", "exhaustion_flip_long", "exhaustion_flip_short":
		return exitmgr.StopTemplateReversalExhaustion
	default:
		return exitmgr.StopTemplateMeanRevertRotation
	}
}

func recentStructureBand(candles []Candle, lookback int) (float64, float64) {
	if len(candles) == 0 {
		return 0, 0
	}
	if lookback <= 0 || lookback > len(candles) {
		lookback = len(candles)
	}
	start := len(candles) - lookback
	low, high := 0.0, 0.0
	for i := start; i < len(candles); i++ {
		c := candles[i]
		if c.L > 0 && (low == 0 || c.L < low) {
			low = c.L
		}
		if c.H > 0 && c.H > high {
			high = c.H
		}
	}
	return low, high
}
