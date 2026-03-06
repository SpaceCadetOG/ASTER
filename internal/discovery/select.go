package discovery

import "strings"

// SelectUniverse returns symbols that pass discovery rules.
func SelectUniverse(snaps []Snapshot, cfg Config) []string {
	if !cfg.Enabled {
		return nil
	}
	if cfg.TopN <= 0 {
		cfg.TopN = 10
	}
	out := make([]string, 0, cfg.TopN)
	for _, s := range snaps {
		if cfg.MinVolumeRatio > 0 && s.VolumeRatio > 0 && s.VolumeRatio < cfg.MinVolumeRatio {
			continue
		}
		if cfg.MinVolatility > 0 && s.Volatility < cfg.MinVolatility {
			continue
		}
		if strings.TrimSpace(s.Symbol) == "" {
			continue
		}
		out = append(out, s.Symbol)
		if len(out) >= cfg.TopN {
			break
		}
	}
	return out
}

func normalizeSymbol(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "/", "")
	return s
}
