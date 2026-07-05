package service

import (
	"testing"
	"time"

	"go-machine/internal/datalayer/cache"
	dltypes "go-machine/internal/datalayer/types"
)

func TestFillsFiltersBySymbolAndLimit(t *testing.T) {
	rt := NewRuntime(Config{AccountRefresh: 15 * time.Second}, nil, nil, nil)
	now := time.Now().UTC()
	rt.fillsCache.Set([]dltypes.Fill{
		{Symbol: "BTC-USD", Ts: now},
		{Symbol: "ETH-USD", Ts: now.Add(-time.Second)},
		{Symbol: "BTC-USD", Ts: now.Add(-2 * time.Second)},
	}, "test", now, nil)

	resp := rt.Fills("BTCUSDT", 1)
	if len(resp.Fills) != 1 {
		t.Fatalf("expected 1 fill, got %d", len(resp.Fills))
	}
	if resp.Fills[0].Symbol != "BTC-USD" {
		t.Fatalf("expected BTC-USD, got %s", resp.Fills[0].Symbol)
	}
}

func TestOrderFlowBuildsSignalFromCollector(t *testing.T) {
	rt := NewRuntime(Config{OrderFlowWindow: 30 * time.Second, EventBuffer: 10, OrderFlowLargeUSD: 50}, nil, nil, nil)
	now := time.Now().UTC()
	rt.oflow.add(dltypes.Event{Symbol: "BTC-USD", Side: "BUY", USD: 200, Ts: now})
	rt.oflow.add(dltypes.Event{Symbol: "BTC-USD", Side: "BUY", USD: 100, Ts: now.Add(time.Second)})
	rt.oflow.add(dltypes.Event{Symbol: "BTC-USD", Side: "SELL", USD: 20, Ts: now.Add(2 * time.Second)})

	resp, err := rt.OrderFlow("BTCUSDT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OrderFlow.Symbol != "BTC-USD" {
		t.Fatalf("unexpected symbol: %s", resp.OrderFlow.Symbol)
	}
	if resp.OrderFlow.Signal != "BULL" {
		t.Fatalf("expected BULL signal, got %s", resp.OrderFlow.Signal)
	}
	if resp.OrderFlow.Summary.Count != 3 {
		t.Fatalf("expected 3 events, got %d", resp.OrderFlow.Summary.Count)
	}
}

func TestMetaFromSnapshotMarksStaleAndPartial(t *testing.T) {
	now := time.Now().UTC()
	meta := metaFromSnapshot(cache.Snapshot[dltypes.Account]{
		Value:   dltypes.Account{},
		Source:  "rest",
		Updated: now.Add(-time.Minute),
		Err:     errString("upstream"),
	}, now, 5*time.Second)
	if !meta.Stale {
		t.Fatalf("expected stale meta")
	}
	if !meta.Partial {
		t.Fatalf("expected partial meta")
	}
	if meta.Error == "" {
		t.Fatalf("expected error metadata")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
