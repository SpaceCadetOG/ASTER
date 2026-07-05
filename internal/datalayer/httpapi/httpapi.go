package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-machine/internal/datalayer/service"
	dltypes "go-machine/internal/datalayer/types"
)

type Server struct {
	runtime API
}

type API interface {
	Health() dltypes.Health
	Prices() dltypes.PricesResponse
	Price(symbol string) (dltypes.PriceResponse, bool)
	OrderBook(symbol string, limit int) (dltypes.OrderBookResponse, error)
	Candles(ctx context.Context, symbol, tf string, limit int) (dltypes.CandlesResponse, error)
	Account() dltypes.AccountResponse
	Positions() dltypes.PositionsResponse
	Fills(symbol string, limit int) dltypes.FillsResponse
	Liquidations() dltypes.LiquidationsResponse
	Whales() dltypes.WhalesResponse
	OrderFlow(symbol string) (dltypes.OrderFlowResponse, error)
}

func New(rt API) http.Handler {
	s := &Server{runtime: rt}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/prices", s.handlePrices)
	mux.HandleFunc("/api/price/", s.handlePrice)
	mux.HandleFunc("/api/orderbook/", s.handleOrderBook)
	mux.HandleFunc("/api/candles/", s.handleCandles)
	mux.HandleFunc("/api/account", s.handleAccount)
	mux.HandleFunc("/api/account/positions", s.handlePositions)
	mux.HandleFunc("/api/fills", s.handleFills)
	mux.HandleFunc("/api/liquidations", s.handleLiquidations)
	mux.HandleFunc("/api/whales", s.handleWhales)
	mux.HandleFunc("/api/orderflow/", s.handleOrderFlow)
	return withJSON(mux)
}

var _ API = (*service.Runtime)(nil)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime.Health())
}

func (s *Server) handlePrices(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime.Prices())
}

func (s *Server) handlePrice(w http.ResponseWriter, r *http.Request) {
	symbol := strings.TrimPrefix(r.URL.Path, "/api/price/")
	if strings.TrimSpace(symbol) == "" {
		http.Error(w, "missing symbol", http.StatusBadRequest)
		return
	}
	resp, ok := s.runtime.Price(symbol)
	if !ok {
		writeJSON(w, http.StatusNotFound, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleOrderBook(w http.ResponseWriter, r *http.Request) {
	symbol := strings.TrimPrefix(r.URL.Path, "/api/orderbook/")
	if strings.TrimSpace(symbol) == "" {
		http.Error(w, "missing symbol", http.StatusBadRequest)
		return
	}
	limit := intQuery(r, "limit", 20)
	resp, err := s.runtime.OrderBook(symbol, limit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCandles(w http.ResponseWriter, r *http.Request) {
	symbol := strings.TrimPrefix(r.URL.Path, "/api/candles/")
	if strings.TrimSpace(symbol) == "" {
		http.Error(w, "missing symbol", http.StatusBadRequest)
		return
	}
	tf := strings.TrimSpace(r.URL.Query().Get("tf"))
	limit := intQuery(r, "limit", 200)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	resp, err := s.runtime.Candles(ctx, symbol, tf, limit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAccount(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime.Account())
}

func (s *Server) handlePositions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime.Positions())
}

func (s *Server) handleFills(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	limit := intQuery(r, "limit", 50)
	writeJSON(w, http.StatusOK, s.runtime.Fills(symbol, limit))
}

func (s *Server) handleLiquidations(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime.Liquidations())
}

func (s *Server) handleWhales(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.runtime.Whales())
}

func (s *Server) handleOrderFlow(w http.ResponseWriter, r *http.Request) {
	symbol := strings.TrimPrefix(r.URL.Path, "/api/orderflow/")
	if strings.TrimSpace(symbol) == "" {
		http.Error(w, "missing symbol", http.StatusBadRequest)
		return
	}
	resp, err := s.runtime.OrderFlow(symbol)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func intQuery(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}
