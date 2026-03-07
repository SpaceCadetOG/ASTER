// cmd/tape/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-machine/internal/colorx"
	"go-machine/internal/ws"
)

type tapeStatus struct {
	mu         sync.RWMutex
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Connected  bool      `json:"connected"`
	MinUSD     float64   `json:"min_usd"`
	LastSymbol string    `json:"last_symbol,omitempty"`
	LastUSD    float64   `json:"last_usd,omitempty"`
	LastSide   string    `json:"last_side,omitempty"`
	Events     int64     `json:"events"`
}

func (s *tapeStatus) snapshot() tapeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s
}

func startStatusServer(addr string, st *tapeStatus) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st.snapshot())
	})
	go func() {
		fmt.Println("tape status server:", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Println("tape status server error:", err)
		}
	}()
}

func main() {
	syms := strings.Split(strings.TrimSpace(os.Getenv("TAPE_SYMBOLS")), ",")
	if len(syms) == 0 || strings.TrimSpace(syms[0]) == "" {
		syms = []string{
			"btcusdt", "ethusdt", "usdtusdt", "bnbusdt", "xrpusdt", "solusdt",
			"adausdt", "dogeusdt", "maticusdt", "avaxusdt", "linkusdt", "ltcusdt",
			"atomusdt", "nearusdt", "etcusdt", "suiusdt", "hypeusdt", "pepeusdt",
			"shibusdt", "wldusdt", "usdcusdt", "fttusdt", "aptusdt", "ltusdt", "asterusdt",
		}
	}

	minUSD := 100.0
	if v := strings.TrimSpace(os.Getenv("TAPE_MIN_USD")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			minUSD = f
		}
	}

	streams := make([]string, 0, len(syms))
	for _, s := range syms {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			streams = append(streams, s+"@aggTrade")
		}
	}

	wsURL := "wss://fstream.asterdex.com/stream?streams=" + strings.Join(streams, "/")
	fmt.Printf("ASTER tape connected (min $%.0f)\n", minUSD)
	fmt.Println(wsURL)
	st := &tapeStatus{
		StartedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		MinUSD:    minUSD,
	}
	startStatusServer(envStr("TAPE_HTTP_ADDR", ":8091"), st)

	for {
		conn, err := ws.Dial(context.Background(), wsURL, 10*time.Second)
		if err != nil {
			st.mu.Lock()
			st.Connected = false
			st.UpdatedAt = time.Now().UTC()
			st.mu.Unlock()
			fmt.Println("dial error:", err)
			time.Sleep(2 * time.Second)
			continue
		}
		st.mu.Lock()
		st.Connected = true
		st.UpdatedAt = time.Now().UTC()
		st.mu.Unlock()

		for {
			b, err := conn.ReadText(70 * time.Second)
			if err != nil {
				_ = conn.Close()
				st.mu.Lock()
				st.Connected = false
				st.UpdatedAt = time.Now().UTC()
				st.mu.Unlock()
				break
			}

			var msg struct {
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(b, &msg); err != nil || len(msg.Data) == 0 {
				continue
			}

			var t struct {
				P string `json:"p"`
				Q string `json:"q"`
				T int64  `json:"T"`
				M bool   `json:"m"`
				S string `json:"s"`
			}
			if err := json.Unmarshal(msg.Data, &t); err != nil {
				continue
			}

			p, errP := strconv.ParseFloat(t.P, 64)
			q, errQ := strconv.ParseFloat(t.Q, 64)
			if errP != nil || errQ != nil || p <= 0 || q <= 0 {
				continue
			}

			usd := p * q
			if usd < minUSD {
				continue
			}

			side := "BUY"
			if t.M {
				side = "SELL"
			}

			if side == "BUY" {
				side = colorx.Buy(side)
			} else {
				side = colorx.Sell(side)
			}
			plainSide := "SELL"
			if !t.M {
				plainSide = "BUY"
			}

			size := colorx.BySize(usd, fmt.Sprintf("$%.0f", usd))
			sym := strings.TrimSuffix(strings.ToUpper(t.S), "USDT")
			st.mu.Lock()
			st.UpdatedAt = time.Now().UTC()
			st.Events++
			st.LastSymbol = sym
			st.LastUSD = usd
			st.LastSide = plainSide
			st.mu.Unlock()
			sec := time.UnixMilli(t.T).Format("15:04:05")

			fmt.Printf("%s %s %-8s %s\n", sec, side, sym, size)
		}

		time.Sleep(1500 * time.Millisecond)
	}
}

func envStr(k, def string) string {
	s := strings.TrimSpace(os.Getenv(k))
	if s == "" {
		return def
	}
	return s
}
