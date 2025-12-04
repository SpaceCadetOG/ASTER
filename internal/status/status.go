package status

import (
	"fmt"
	"html"
	"net/http"
	"sort"
	"sync"
	"time"

	"go-machine/internal/market"
)

type Snapshot struct {
	Generated time.Time
	Exchange  string
	Active    []string
	Rows      []market.Scored   // includes Change24h etc.
	Conf      map[string]string // symbol -> "A+","A","B","C","D" (or "N/A")
}

type Store struct {
	mu  sync.RWMutex
	cur Snapshot
}

func NewStore() *Store { return &Store{} }

func (s *Store) SetSnap(sn Snapshot) {
	s.mu.Lock()
	s.cur = sn
	s.mu.Unlock()
}

func (s *Store) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		snap := s.cur
		s.mu.RUnlock()

		// infer direction from exchange name
		direction := "long"
		if snap.Exchange != "" && (snap.Exchange == "asterdex (SHORTS)" || snap.Exchange == "shorts") {
			direction = "short"
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><meta charset="utf-8">
<title>Scanner Status</title>
<style>
:root{
  --bg:#111; --fg:#eee; --muted:#aaa; --row:#151515; --grid:#2a2a2a;
  --up:#19c37d; --down:#ff6464; --blue:#9cf;
}
*{box-sizing:border-box}
body{background:var(--bg);color:var(--fg);font-family:ui-monospace,Menlo,Consolas,monospace;margin:16px}
h3{margin:0 0 10px}
small{color:var(--muted)}
table{border-collapse:collapse;width:100%}
th,td{padding:8px 10px;border-bottom:1px solid var(--grid);vertical-align:middle;white-space:nowrap}
th{color:var(--blue);text-align:left;font-weight:600}
tr:hover{background:var(--row)}
.badge{display:inline-block;padding:2px 8px;border-radius:10px;font-weight:700}
.num{font-variant-numeric:tabular-nums}
.up{color:var(--up)}
.down{color:var(--down)}
.mono{white-space:pre; font-family:inherit}
.sep{opacity:.25}
</style></head><body>`)

		fmt.Fprintf(w, `<h3>%s <small>&nbsp;generated %s</small></h3>`,
			html.EscapeString(snap.Exchange),
			html.EscapeString(snap.Generated.Format(time.RFC3339)),
		)

		if len(snap.Active) > 0 {
			fmt.Fprint(w, `<div style="margin:6px 0 12px 0;"><small>Sessions:&nbsp;`)
			for i, a := range snap.Active {
				if i > 0 {
					fmt.Fprint(w, ` <span class="sep">•</span> `)
				}
				fmt.Fprint(w, html.EscapeString(a))
			}
			fmt.Fprint(w, `</small></div>`)
		}

		// keep CLI banner
		fmt.Fprintf(w, `<div class="mono" style="margin-bottom:8px;">%s</div>`, html.EscapeString(market.FormatHeader(snap.Exchange, snap.Active)))

		fmt.Fprint(w, `<table>
<thead>
<tr>
  <th>Symbol</th>
  <th class="num">Score</th>
  <th class="num">Δ24h%</th>
  <th class="num">Vol($)</th>
  <th class="num">OI($)</th>
  <th class="num">Funding(%)</th>
  <th class="num">Prev24h</th>
  <th class="num">Last</th>
  <th>Conf</th>
</tr>
</thead><tbody>`)

		// stable order (highest score first)
		rows := append([]market.Scored(nil), snap.Rows...)
		sort.Slice(rows, func(i, j int) bool { return rows[i].Score > rows[j].Score })

		for _, row := range rows {
			// grade
			grade := "N/A"
			if snap.Conf != nil && snap.Conf[row.Symbol] != "" {
				grade = snap.Conf[row.Symbol]
			} else {
				grade = market.FallbackGradeDirectional(row.Score, row.Change24h, direction)
			}
			textHex, bgHex := market.GradePalette(grade)

			// Δ24h
			p24, c24, a24 := pctCell(row.Change24h)

			// nil-safe OI & Funding strings
			oi := "-"
			if row.OIUSD != nil && *row.OIUSD > 0 {
				oi = fmt.Sprintf("%.2fM", *row.OIUSD/1e6)
			}
			funding := "-"
			if row.FundingRate != nil {
				funding = fmt.Sprintf("%.4f", *row.FundingRate*100.0)
			}

			prev := row.OpenPrice
			if prev == 0 {
				prev = row.LastPrice
			}

			fmt.Fprintf(w, `<tr>
<td>%s</td>
<td class="num">%0.2f</td>
<td class="num %s">%s%0.1f%%</td>
<td class="num">%0.2fM</td>
<td class="num">%s</td>
<td class="num">%s</td>
<td class="num %s">%0.4f</td>
<td class="num %s">%0.4f</td>
<td><span class="badge" style="color:%s;background:%s;border:1px solid %s;">%s</span></td>
</tr>`,
				html.EscapeString(row.Symbol),
				row.Score,
				// 24h Δ
				c24, a24, p24,
				// Vol($)
				row.VolumeUSD/1e6,
				// OI($)
				oi,
				// Funding(%)
				funding,
				// Prev24h vs Last
				priceColor(prev, row.LastPrice), prev,
				priceColor(row.LastPrice, prev), row.LastPrice,
				// Badge
				html.EscapeString(textHex), html.EscapeString(bgHex), html.EscapeString(textHex),
				html.EscapeString(grade),
			)
		}

		fmt.Fprint(w, `</tbody></table></body></html>`)
	})
}

// pctCell returns (val, class, arrow)
func pctCell(v float64) (float64, string, string) {
	switch {
	case v > 0:
		return v, "up", "▲"
	case v < 0:
		return v, "down", "▼"
	default:
		return 0, "", ""
	}
}

// priceColor returns "up" if a>b, "down" if a<b, else ""
func priceColor(a, b float64) string {
	switch {
	case a > b:
		return "up"
	case a < b:
		return "down"
	default:
		return ""
	}
}
