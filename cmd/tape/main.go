// cmd/tape/main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go-machine/internal/colorx"
	"go-machine/internal/ws"
)

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

	for {
		conn, err := ws.Dial(context.Background(), wsURL, 10*time.Second)
		if err != nil {
			fmt.Println("dial error:", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for {
			b, err := conn.ReadText(70 * time.Second)
			if err != nil {
				_ = conn.Close()
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

			size := colorx.BySize(usd, fmt.Sprintf("$%.0f", usd))
			sym := strings.TrimSuffix(strings.ToUpper(t.S), "USDT")
			sec := time.UnixMilli(t.T).Format("15:04:05")

			fmt.Printf("%s %s %-8s %s\n", sec, side, sym, size)
		}

		time.Sleep(1500 * time.Millisecond)
	}
}