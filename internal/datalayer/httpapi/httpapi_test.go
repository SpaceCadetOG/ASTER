package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dltypes "go-machine/internal/datalayer/types"
)

type stubAPI struct{}

func (stubAPI) Health() dltypes.Health {
	return dltypes.Health{Status: "ok", Ts: time.Now().UTC()}
}

func (stubAPI) Prices() dltypes.PricesResponse {
	return dltypes.PricesResponse{
		Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC()},
		Prices: []dltypes.Price{
			{Symbol: "BTC-USD", Mid: 100000},
		},
	}
}

func (stubAPI) Price(symbol string) (dltypes.PriceResponse, bool) {
	if strings.EqualFold(symbol, "BTCUSDT") || strings.EqualFold(symbol, "BTC-USD") {
		return dltypes.PriceResponse{
			Meta:  dltypes.Meta{Source: "test", Ts: time.Now().UTC()},
			Price: dltypes.Price{Symbol: "BTC-USD", Mid: 100000},
		}, true
	}
	return dltypes.PriceResponse{
		Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC(), Error: "missing"},
	}, false
}

func (stubAPI) OrderBook(symbol string, limit int) (dltypes.OrderBookResponse, error) {
	return dltypes.OrderBookResponse{
		Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC()},
		OrderBook: dltypes.OrderBook{
			Symbol: symbol,
			Bids:   []dltypes.BookLevel{{Price: 1, Qty: 2}},
			Asks:   []dltypes.BookLevel{{Price: 1.1, Qty: 3}},
		},
	}, nil
}

func (stubAPI) Candles(ctx context.Context, symbol, tf string, limit int) (dltypes.CandlesResponse, error) {
	return dltypes.CandlesResponse{
		Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC()},
		Candles: dltypes.Candles{
			Symbol:    "BTC-USD",
			Timeframe: "1m",
			Candles:   []dltypes.Candle{{Ts: time.Now().UTC(), Close: 100000}},
		},
	}, nil
}

func (stubAPI) Account() dltypes.AccountResponse {
	return dltypes.AccountResponse{Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC()}}
}

func (stubAPI) Positions() dltypes.PositionsResponse {
	return dltypes.PositionsResponse{Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC()}}
}

func (stubAPI) Fills(symbol string, limit int) dltypes.FillsResponse {
	return dltypes.FillsResponse{Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC()}}
}

func (stubAPI) Liquidations() dltypes.LiquidationsResponse {
	return dltypes.LiquidationsResponse{Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC()}}
}

func (stubAPI) Whales() dltypes.WhalesResponse {
	return dltypes.WhalesResponse{Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC()}}
}

func (stubAPI) OrderFlow(symbol string) (dltypes.OrderFlowResponse, error) {
	return dltypes.OrderFlowResponse{
		Meta: dltypes.Meta{Source: "test", Ts: time.Now().UTC()},
		OrderFlow: dltypes.OrderFlow{
			Symbol: symbol,
			Signal: "BULL",
		},
	}, nil
}

func TestPriceRouteReturnsNotFoundForUnknownSymbol(t *testing.T) {
	handler := New(stubAPI{})
	req := httptest.NewRequest(http.MethodGet, "/api/price/UNKNOWN", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCandlesRouteUsesPathSymbol(t *testing.T) {
	handler := New(stubAPI{})
	req := httptest.NewRequest(http.MethodGet, "/api/candles/BTCUSDT?tf=1m&limit=5", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"timeframe\":\"1m\"") {
		t.Fatalf("expected candle payload, got %s", rec.Body.String())
	}
}
