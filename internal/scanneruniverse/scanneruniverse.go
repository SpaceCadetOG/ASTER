package scanneruniverse

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type statusSnapshot struct {
	Rows          []map[string]any `json:"Rows"`
	InPlay        []map[string]any `json:"InPlay"`
	ScannerLongs  []map[string]any `json:"scanner_longs"`
	ScannerShorts []map[string]any `json:"scanner_shorts"`
}

func ResolveCSVOrScanner(envName string, fallback []string, statusURLs []string) []string {
	if syms := parseCSVEnv(envName); len(syms) > 0 {
		return syms
	}
	if syms := fetchScannerSymbols(statusURLs); len(syms) > 0 {
		return syms
	}
	return normalizeSymbols(fallback)
}

func parseCSVEnv(envName string) []string {
	raw := strings.TrimSpace(os.Getenv(envName))
	if raw == "" {
		return nil
	}
	return normalizeSymbols(strings.Split(raw, ","))
}

func fetchScannerSymbols(statusURLs []string) []string {
	client := &http.Client{Timeout: 4 * time.Second}
	seen := map[string]struct{}{}
	for _, rawURL := range statusURLs {
		u := strings.TrimSpace(rawURL)
		if u == "" {
			continue
		}
		resp, err := client.Get(u)
		if err != nil {
			continue
		}
		var snap statusSnapshot
		if err := json.NewDecoder(resp.Body).Decode(&snap); err == nil {
			appendSymbols(seen, snap.Rows)
			appendSymbols(seen, snap.InPlay)
			appendSymbols(seen, snap.ScannerLongs)
			appendSymbols(seen, snap.ScannerShorts)
		}
		resp.Body.Close()
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for sym := range seen {
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}

func appendSymbols(dst map[string]struct{}, rows []map[string]any) {
	for _, row := range rows {
		raw, _ := row["Symbol"].(string)
		if raw == "" {
			raw, _ = row["symbol"].(string)
		}
		sym := normalizeSymbol(raw)
		if sym != "" {
			dst[sym] = struct{}{}
		}
	}
}

func normalizeSymbols(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		sym := normalizeSymbol(raw)
		if sym == "" {
			continue
		}
		if _, ok := seen[sym]; ok {
			continue
		}
		seen[sym] = struct{}{}
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}

func normalizeSymbol(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "-", "")
	switch {
	case strings.HasSuffix(s, "USDT"):
		return strings.ToLower(s)
	case strings.HasSuffix(s, "USD"):
		return strings.ToLower(strings.TrimSuffix(s, "USD") + "USDT")
	default:
		return strings.ToLower(s)
	}
}
