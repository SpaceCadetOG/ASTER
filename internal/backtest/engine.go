package backtest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go-machine/internal/features"
	"go-machine/internal/strategies"
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
	TP1           float64   `json:"tp1"`
	TP2           float64   `json:"tp2"`
	Qty           float64   `json:"qty"`
	PnL           float64   `json:"pnl"`
	R             float64   `json:"r"`
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
	Trades []Trade `json:"trades"`
	Report Report  `json:"report"`
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
	var open *Trade
	var pending strategies.Candidate
	entryPending := false
	barsInPos := 0
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
				notional := cfg.MarginUSD * cfg.Leverage
				qty := notional / entryPx
				fee := notional * cfg.FeesBps / 10000.0
				slip := notional * cfg.SlipBps / 10000.0
				balance -= fee + slip
				open = &Trade{
					Symbol:        cfg.Symbol,
					Strategy:      pending.Signal.Name,
					Side:          string(pending.Signal.Side),
					EntryTs:       c.Ts,
					Entry:         entryPx,
					Stop:          pending.Signal.Stop,
					TP1:           pending.Signal.TP1,
					TP2:           pending.Signal.TP2,
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
				}
				barsInPos = 0
			}
			entryPending = false
		}

		if open != nil {
			barsInPos++
			risk := abs(open.Entry-open.Stop) * open.Qty
			slHit, tpHit := false, false
			if open.Side == string(features.SideLong) {
				slHit = c.L <= open.Stop
				tpHit = c.H >= open.TP1
			} else {
				slHit = c.H >= open.Stop
				tpHit = c.L <= open.TP1
			}

			exitPx := 0.0
			reason := ""
			switch {
			case slHit && tpHit:
				exitPx = open.Stop
				reason = "SL+TP same bar (conservative SL)"
			case slHit:
				exitPx = open.Stop
				reason = "SL"
			case tpHit:
				exitPx = open.TP1
				reason = "TP1"
			case barsInPos >= 120:
				exitPx = c.C
				reason = "timeout"
			}

			if exitPx > 0 {
				notional := exitPx * open.Qty
				fee := notional * cfg.FeesBps / 10000.0
				slip := notional * cfg.SlipBps / 10000.0
				pnl := 0.0
				if open.Side == string(features.SideLong) {
					pnl = (exitPx-open.Entry)*open.Qty - fee - slip
				} else {
					pnl = (open.Entry-exitPx)*open.Qty - fee - slip
				}
				balance += pnl
				r := 0.0
				if risk > 1e-9 {
					r = pnl / risk
				}
				open.ExitTs = c.Ts
				open.Exit = exitPx
				open.PnL = pnl
				open.R = r
				open.Reason = reason
				open.HoldMins = c.Ts.Sub(open.EntryTs).Minutes()
				open.Fees += fee
				open.Slippage += slip
				trades = append(trades, *open)
				open = nil
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
					pending = cands[0]
					entryPending = true
				}
			}
		}

		eq := balance
		if open != nil {
			if open.Side == string(features.SideLong) {
				eq += (c.C - open.Entry) * open.Qty
			} else {
				eq += (open.Entry - c.C) * open.Qty
			}
			holdBars++
		}
		equity = append(equity, eq)
	}

	rep := summarize(cfg, trades, equity, balance, holdBars, len(candles))
	return Result{Trades: trades, Report: rep}, nil
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

	f, err := os.Create(csvPath)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"symbol", "strategy", "side", "entry_ts", "exit_ts", "entry", "exit", "stop", "tp1", "tp2", "qty", "pnl", "r", "reason", "hold_mins", "fees", "slippage", "confidence", "candidate_score", "vp_setup", "vp_level", "vp_target_level", "vp_stop_mode", "vp_target_mode", "reject_reason", "regime_tag", "signal_reasons", "signal_source"})
	for _, t := range res.Trades {
		_ = w.Write([]string{t.Symbol, t.Strategy, t.Side, t.EntryTs.Format(time.RFC3339), t.ExitTs.Format(time.RFC3339), f64(t.Entry), f64(t.Exit), f64(t.Stop), f64(t.TP1), f64(t.TP2), f64(t.Qty), f64(t.PnL), f64(t.R), t.Reason, f64(t.HoldMins), f64(t.Fees), f64(t.Slippage), f64(t.Confidence), f64(t.CandidateRaw), t.VPSetup, f64(t.VPLevel), f64(t.VPTargetLevel), t.VPStopMode, t.VPTargetMode, t.RejectReason, t.RegimeTag, t.Reasons, t.SignalSource})
	}
	w.Flush()
	_ = f.Close()

	b, _ := json.MarshalIndent(res.Report, "", "  ")
	return os.WriteFile(jsonPath, b, 0o644)
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
