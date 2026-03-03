package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type klineResp [][]json.RawMessage

func main() {
	base := envStr("BT_FETCH_BASE_URL", "https://fapi.asterdex.com")
	tf := envStr("BT_FETCH_TF", "1m")
	limit := envInt("BT_FETCH_LIMIT", 1500)
	symbols := splitSymbols(envStr("BT_FETCH_SYMBOLS", "BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,ASTERUSDT,HYPEUSDT"))

	if len(symbols) == 0 {
		fail("no symbols")
	}
	if limit < 10 {
		limit = 10
	}
	if limit > 1500 {
		limit = 1500
	}

	if err := os.MkdirAll("data", 0o755); err != nil {
		fail("mkdir data: %v", err)
	}

	for _, sym := range symbols {
		rows, err := fetchKlines(base, sym, tf, limit)
		if err != nil {
			fmt.Printf("skip %s: %v\n", sym, err)
			continue
		}
		out := filepath.Join("data", fmt.Sprintf("%s_%s.csv", sym, tf))
		if err := writeCSV(out, rows); err != nil {
			fmt.Printf("skip %s: write error: %v\n", sym, err)
			continue
		}
		fmt.Printf("ok %s -> %s (%d candles)\n", sym, out, len(rows))
	}
}

func fetchKlines(base, symbol, interval string, limit int) (klineResp, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/fapi/v1/klines")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("symbol", symbol)
	q.Set("interval", interval)
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rows klineResp
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty klines")
	}
	return rows, nil
}

func writeCSV(path string, rows klineResp) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"timestamp", "open", "high", "low", "close", "volume"}); err != nil {
		return err
	}
	for _, row := range rows {
		if len(row) < 6 {
			continue
		}
		ts, _ := unquote(row[0])
		o, _ := unquote(row[1])
		h, _ := unquote(row[2])
		l, _ := unquote(row[3])
		c, _ := unquote(row[4])
		v, _ := unquote(row[5])
		if err := w.Write([]string{ts, o, h, l, c, v}); err != nil {
			return err
		}
	}
	return w.Error()
}

func unquote(b json.RawMessage) (string, error) {
	var s string
	if len(b) == 0 {
		return "", fmt.Errorf("empty")
	}
	if b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	return string(b), nil
}

func splitSymbols(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.ToUpper(strings.TrimSpace(p))
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func envStr(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return d
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return d
	}
	return n
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
