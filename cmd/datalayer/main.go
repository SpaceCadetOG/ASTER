package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go-machine/adapters/aster"
	"go-machine/internal/datalayer/httpapi"
	"go-machine/internal/datalayer/service"
)

func main() {
	cfg := loadConfig()
	logger := log.New(os.Stdout, "datalayer: ", log.LstdFlags|log.LUTC)

	client := aster.New("")
	rest := buildRESTFromEnv(logger)
	rt := service.NewRuntime(cfg, client, rest, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt.Start(ctx)

	srv := &http.Server{
		Addr:              envStr("DATALAYER_ADDR", ":8095"),
		Handler:           httpapi.New(rt),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Printf("listening on %s symbols=%d auth=%t", srv.Addr, len(cfg.Symbols), rest != nil)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("server error: %v", err)
	}
}

func loadConfig() service.Config {
	symbols := parseSymbols(envStr("DATALAYER_SYMBOLS", "BTCUSDT,ETHUSDT,SOLUSDT"))
	return service.Config{
		Symbols:            symbols,
		PriceRefresh:       envDurationSec("DATALAYER_PRICE_REFRESH_SEC", 15),
		CandleTTL:          envDurationSec("DATALAYER_CANDLE_TTL_SEC", 15),
		AccountRefresh:     envDurationSec("DATALAYER_ACCOUNT_REFRESH_SEC", 15),
		UserDataMaxStale:   envDurationSec("DATALAYER_USERDATA_MAX_STALE_SEC", 120),
		MarketLevels:       envInt("DATALAYER_MARKET_LEVELS", 20),
		MarketSpeed:        envStr("DATALAYER_MARKET_SPEED", "100ms"),
		MarketTrades:       envInt("DATALAYER_MARKET_TRADES", 50),
		EventBuffer:        envInt("DATALAYER_EVENT_BUFFER", 50),
		WhaleMinUSD:        envFloat("DATALAYER_WHALE_MIN_USD", 500),
		LiquidationMinUSD:  envFloat("DATALAYER_LIQ_MIN_USD", 500),
		OrderFlowLargeUSD:  envFloat("DATALAYER_OFLOW_LARGE_USD", 50),
		WhaleWindow:        envDurationSec("DATALAYER_WHALE_WINDOW_SEC", 30),
		LiquidationWindow:  envDurationSec("DATALAYER_LIQ_WINDOW_SEC", 60),
		OrderFlowWindow:    envDurationSec("DATALAYER_OFLOW_WINDOW_SEC", 20),
		OrderBookFallback:  envInt("DATALAYER_ORDERBOOK_LIMIT", 20),
		DefaultCandleLimit: envInt("DATALAYER_CANDLE_LIMIT", 200),
	}
}

func buildRESTFromEnv(logger *log.Logger) *aster.RESTAuth {
	cfg, ok := restAuthConfigFromEnv()
	if !ok {
		logger.Printf("starting without account auth")
		return nil
	}
	rest := aster.NewRESTAuthWithConfig(cfg)
	if err := rest.ConfigError(); err != nil {
		logger.Printf("auth config error: %v", err)
		return nil
	}
	_ = rest.SyncTime()
	return rest
}

func restAuthConfigFromEnv() (aster.RESTAuthConfig, bool) {
	key := strings.TrimSpace(os.Getenv("ASTER_API_KEY"))
	secret := strings.TrimSpace(os.Getenv("ASTER_API_SECRET"))
	user := strings.TrimSpace(os.Getenv("ASTER_USER"))
	signer := strings.TrimSpace(os.Getenv("ASTER_SIGNER"))
	priv := strings.TrimSpace(os.Getenv("ASTER_PRIVATE_KEY"))
	authMode := envStr("ASTER_AUTH_MODE", "auto")
	rawChainID := strings.TrimSpace(os.Getenv("ASTER_CHAIN_ID"))
	chainIDSet := rawChainID != ""
	chainID := int64(0)
	if rawChainID != "" {
		parsed, _ := strconv.ParseInt(rawChainID, 10, 64)
		chainID = parsed
	}
	hasHMAC := key != "" && secret != ""
	hasAgent := user != "" && signer != "" && priv != ""
	if !hasHMAC && !hasAgent {
		return aster.RESTAuthConfig{}, false
	}
	return aster.RESTAuthConfig{
		APIKey:     key,
		APISecret:  secret,
		User:       user,
		Signer:     signer,
		PrivateKey: priv,
		AuthMode:   authMode,
		ChainID:    chainID,
		ChainIDSet: chainIDSet,
		BaseURL:    strings.TrimSpace(os.Getenv("ASTER_BASE_URL")),
	}, true
}

func parseSymbols(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		sym := strings.ToUpper(strings.TrimSpace(part))
		if sym == "" {
			continue
		}
		if _, ok := seen[sym]; ok {
			continue
		}
		seen[sym] = struct{}{}
		out = append(out, sym)
	}
	return out
}

func envStr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func envFloat(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return n
}

func envDurationSec(key string, fallback int) time.Duration {
	sec := envInt(key, fallback)
	if sec <= 0 {
		sec = fallback
	}
	return time.Duration(sec) * time.Second
}
