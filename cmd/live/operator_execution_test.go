package main

import (
	"strings"
	"testing"
)

func TestParseOperatorOrderCommandLongLimitWithBracket(t *testing.T) {
	req, err := parseOperatorOrderCommand("/long OUSDT usd=10 limit=123 sl=120 tp=129 lev=5")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if req.Symbol != "OUSDT" {
		t.Fatalf("expected symbol OUSDT, got %s", req.Symbol)
	}
	if req.Side != "BUY" {
		t.Fatalf("expected BUY, got %s", req.Side)
	}
	if req.USD != 10 {
		t.Fatalf("expected usd 10, got %.2f", req.USD)
	}
	if !req.HasLimit || req.LimitPrice != 123 {
		t.Fatalf("expected limit 123, got %+v", req)
	}
	if !req.HasStopLoss || req.StopLoss != 120 {
		t.Fatalf("expected stop 120, got %+v", req)
	}
	if !req.HasTakeProfit || req.TakeProfit != 129 {
		t.Fatalf("expected tp 129, got %+v", req)
	}
	if req.Leverage != 5 {
		t.Fatalf("expected leverage 5, got %d", req.Leverage)
	}
}

func TestParseOperatorOrderCommandShortMarket(t *testing.T) {
	req, err := parseOperatorOrderCommand("/short OUSDT usd=25 market")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if req.Symbol != "OUSDT" {
		t.Fatalf("expected symbol OUSDT, got %s", req.Symbol)
	}
	if req.Side != "SELL" {
		t.Fatalf("expected SELL, got %s", req.Side)
	}
	if req.USD != 25 {
		t.Fatalf("expected usd 25, got %.2f", req.USD)
	}
	if req.HasLimit {
		t.Fatalf("expected market order, got %+v", req)
	}
}

func TestParseOperatorOrderCommandRequiresUSD(t *testing.T) {
	if _, err := parseOperatorOrderCommand("/long OUSDT limit=123"); err == nil {
		t.Fatalf("expected usd requirement error")
	}
}

func TestGroundZeroCloseCommandNotBlocked(t *testing.T) {
	t.Setenv("LIVE_OPERATOR_EXECUTION_ENABLE", "0")
	ctx := &telegramCommandCtx{}
	out := ctx.handleCommand("", "/close SNDKUSDT")
	if strings.Contains(out, "TRADING DISABLED") {
		t.Fatalf("expected close command to bypass ground-zero trading block, got %q", out)
	}
	if !strings.Contains(out, "POSITION NOT FOUND") {
		t.Fatalf("expected position-not-found response, got %q", out)
	}
}

func TestGroundZeroManageCommandNotBlocked(t *testing.T) {
	t.Setenv("LIVE_OPERATOR_EXECUTION_ENABLE", "0")
	ctx := &telegramCommandCtx{execMgr: &liveExecManager{}}
	out := ctx.handleCommand("", "/manage SNDKUSDT y")
	if strings.Contains(out, "TRADING DISABLED") {
		t.Fatalf("expected manage command to bypass ground-zero trading block, got %q", out)
	}
	if !strings.Contains(out, "no pending manual request") {
		t.Fatalf("expected pending-request response, got %q", out)
	}
}
