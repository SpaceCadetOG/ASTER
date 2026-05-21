import {
  formatClockTime,
  formatCompactUsd,
  formatKeyLabel,
  formatNumber,
  formatPercent,
  formatTime,
  formatUsd
} from "@/lib/format";
import type { AssetDetail, ModuleSummary } from "@/lib/types";

type CandlePoint = {
  t: string;
  C: number;
};

type FeedEntry = {
  id: string;
  time: string;
  side?: string;
  accent?: "positive" | "negative" | "amber" | "muted";
  primary: string;
  secondary?: string;
};

function gradeTone(value?: string) {
  const key = (value || "N/A").toUpperCase();
  if (key === "A+" || key === "A") return "tone-positive";
  if (key === "B") return "tone-amber";
  if (key === "C") return "tone-orange";
  if (key === "D") return "tone-negative";
  return "tone-muted";
}

function numericTone(value: number | null | undefined) {
  if (value === null || value === undefined || Number.isNaN(value)) return "tone-muted";
  if (value > 0) return "tone-positive";
  if (value < 0) return "tone-negative";
  return "tone-muted";
}

function setupSummary(data: Record<string, unknown> | null) {
  if (!data) {
    return {
      score: "N/A",
      trend: "Unavailable",
      effort: "Unavailable",
      note: "No confluence payload returned."
    };
  }
  const score = data.score;
  const trend =
    data.trend || data.bias || data.direction || data.label || data.regime || "Unavailable";
  const effort = data.effort || data.participation || data.intensity || "Unavailable";
  const note = [
    data.order_block,
    data.orderBlock,
    data.bid_ask_absorption,
    data.absorption,
    Array.isArray(data.notes) ? data.notes.join(" ") : null
  ]
    .filter(Boolean)
    .join(" ")
    .trim();
  return {
    score: typeof score === "number" ? formatNumber(score, 2) : String(score || "N/A"),
    trend: String(trend),
    effort: String(effort),
    note: note || "No order block or absorption notes returned."
  };
}

function extractModuleFields(module: ModuleSummary) {
  const keys = [
    "symbol",
    "signal",
    "score",
    "window_sec",
    "min_usd",
    "large_usd",
    "count",
    "events",
    "buy_usd",
    "sell_usd",
    "delta_usd",
    "total_usd",
    "last_symbol",
    "last_side"
  ];
  const status = module.status || {};
  return keys
    .filter((key) => key in status)
    .map((key) => ({ key, value: String(status[key]) }));
}

function toNumber(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim()) {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

function toString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function toTone(side: string) {
  if (/BUY/i.test(side)) return "positive" as const;
  if (/SELL/i.test(side)) return "negative" as const;
  return "muted" as const;
}

function toneClass(tone?: FeedEntry["accent"]) {
  if (tone === "positive") return "tone-positive";
  if (tone === "negative") return "tone-negative";
  if (tone === "amber") return "tone-amber";
  return "tone-muted";
}

function moduleFeedEntries(module: ModuleSummary): FeedEntry[] {
  const recent = Array.isArray(module.status?.recent) ? module.status?.recent : [];
  return recent
    .map((item, index) => {
      const row = item as Record<string, unknown>;
      const side = toString(row.side);
      const usd = toNumber(row.usd);
      const ts = toString(row.ts);

      if (module.id === "tape") {
        return {
          id: `${module.id}-${index}-${ts}`,
          time: formatClockTime(ts),
          side,
          accent: toTone(side),
          primary: `${side || "TRADE"} ${formatUsd(usd, 0)}`,
          secondary: `price ${formatNumber(toNumber(row.price), 4)} · qty ${formatNumber(toNumber(row.qty), 4)}`
        };
      }

      if (module.id === "whale") {
        const burst = row.burst ? "burst YES" : "burst NO";
        const dominant = toString(row.dominant_side) || "NEUTRAL";
        return {
          id: `${module.id}-${index}-${ts}`,
          time: formatClockTime(ts),
          side,
          accent: usd !== null && usd >= 50_000 ? "amber" : toTone(side),
          primary: `${side || "FLOW"} ${formatUsd(usd, 0)} ${toString(row.tier)}`.trim(),
          secondary: `window ${formatNumber(toNumber(row.window_count), 0)} · buy% ${formatNumber(toNumber(row.buy_pct), 0)} · ${burst} · dominant ${dominant}`
        };
      }

      if (module.id === "oflow") {
        const signal = toString(row.signal) || "NEUTRAL";
        const score = formatNumber(toNumber(row.score), 0);
        return {
          id: `${module.id}-${index}-${ts}`,
          time: formatClockTime(ts),
          side,
          accent: signal === "BULL" ? "positive" : signal === "BEAR" ? "negative" : "muted",
          primary: `${side || "FLOW"} ${formatUsd(usd, 0)} · ${signal} ${score}`,
          secondary: `delta ${formatUsd(toNumber(row.delta_usd), 0)} · large ${formatNumber(
            toNumber(row.large_count),
            0
          )} · window ${formatNumber(toNumber(row.window_sec), 0)}s`
        };
      }

      if (module.id === "liqs") {
        return {
          id: `${module.id}-${index}-${ts}`,
          time: formatClockTime(ts),
          side,
          accent: toTone(side),
          primary: `LIQ ${side || "FLOW"} ${formatUsd(usd, 0)}`,
          secondary: `price ${formatNumber(toNumber(row.price), 4)} · qty ${formatNumber(toNumber(row.qty), 4)}`
        };
      }

      return null;
    })
    .filter(Boolean)
    .reverse() as FeedEntry[];
}

function buildChartPath(points: CandlePoint[]) {
  if (!points.length) {
    return { path: "", latest: null as number | null, change: null as number | null };
  }
  const closes = points.map((point) => point.C);
  const min = Math.min(...closes);
  const max = Math.max(...closes);
  const span = Math.max(max - min, 0.000001);
  const coords = points.map((point, index) => {
    const x = (index / Math.max(points.length - 1, 1)) * 100;
    const y = 100 - ((point.C - min) / span) * 100;
    return `${x},${y}`;
  });
  const latest = closes[closes.length - 1];
  const first = closes[0];
  const change = first ? ((latest - first) / first) * 100 : 0;
  return { path: coords.join(" "), latest, change };
}

function JsonAccordion({
  title,
  payload
}: {
  title: string;
  payload: Record<string, unknown> | null;
}) {
  return (
    <details className="accordion">
      <summary>{title}</summary>
      <pre>{JSON.stringify(payload || { unavailable: true }, null, 2)}</pre>
    </details>
  );
}

function CurrentStatusChart({ candles }: { candles: CandlePoint[] }) {
  const { path, latest, change } = buildChartPath(candles);
  return (
    <div className="status-chart-card">
      <div className="status-chart-head">
        <div>
          <div className="tile-label">Price Path</div>
          <strong>{formatNumber(latest, latest && latest > 100 ? 2 : 4)}</strong>
        </div>
        <span className={numericTone(change)}>{formatPercent(change, 2)}</span>
      </div>
      {path ? (
        <svg viewBox="0 0 100 100" className="status-chart" preserveAspectRatio="none">
          <polyline
            fill="none"
            stroke="currentColor"
            strokeWidth="2.6"
            points={path}
            className={numericTone(change)}
          />
        </svg>
      ) : (
        <div className="empty-state">Price path unavailable.</div>
      )}
    </div>
  );
}

export function AssetDetailPanel({ detail }: { detail?: AssetDetail }) {
  if (!detail) {
    return (
      <section className="panel">
        <h3>Asset Detail</h3>
        <div className="empty-state">Select a symbol from Scanners or Live Hotlist.</div>
      </section>
    );
  }

  const row = detail.scannerRow;
  const longSummary = setupSummary(detail.analytics.longConfluence);
  const shortSummary = setupSummary(detail.analytics.shortConfluence);
  const sessionTags = detail.primaryScanner?.active || [];
  const candles: CandlePoint[] = (detail.analytics.candles?.data || [])
    .filter((point) => typeof point?.C === "number" && typeof point?.t === "string")
    .map((point) => ({ t: point.t, C: point.C }));

  return (
    <section className="asset-detail-stack">
      <div className="hero-card">
        <div>
          <div className="hero-overline">ASTER Unified Operator Portal</div>
          <h2>{detail.symbol}</h2>
          <div className="hero-meta">
            <span>Updated {formatTime(detail.generatedAt)}</span>
            <span>Primary {detail.requestedSide.toUpperCase()}</span>
          </div>
        </div>
        <div className="score-strip">
          <div className="score-card">
            <span className="tile-label">Long Score</span>
            <strong>{formatNumber(detail.longScannerRow?.score, 2)}</strong>
            <span className={`badge ${gradeTone(detail.longScannerRow?.grade)}`}>
              {detail.longScannerRow?.grade || "N/A"}
            </span>
          </div>
          <div className="score-card">
            <span className="tile-label">Short Score</span>
            <strong>{formatNumber(detail.shortScannerRow?.score, 2)}</strong>
            <span className={`badge ${gradeTone(detail.shortScannerRow?.grade)}`}>
              {detail.shortScannerRow?.grade || "N/A"}
            </span>
          </div>
        </div>
      </div>

      <div className="detail-grid">
        <article className="panel">
          <div className="section-head">
            <div>
              <div className="tile-label">Scanner Context</div>
              <h3>Current Status</h3>
            </div>
            <span className="subtle">{formatTime(detail.generatedAt)}</span>
          </div>
          <div className="status-context-layout">
            <CurrentStatusChart candles={candles} />
            <div>
              <div className="context-grid">
                <div className="context-item">
                  <span>Last Price</span>
                  <strong>{formatNumber(row?.lastPrice, row && row.lastPrice > 100 ? 2 : 4)}</strong>
                </div>
                <div className="context-item">
                  <span>24h Volume</span>
                  <strong>{formatCompactUsd(row?.volumeUsd)}</strong>
                </div>
                <div className="context-item">
                  <span>Open Interest</span>
                  <strong>{formatCompactUsd(row?.openInterestUsd)}</strong>
                </div>
                <div className="context-item">
                  <span>Funding</span>
                  <strong className={numericTone(row?.fundingRatePct)}>
                    {formatPercent(row?.fundingRatePct, 4)}
                  </strong>
                </div>
              </div>
              <div className="session-pills">
                {sessionTags.length ? (
                  sessionTags.map((session) => (
                    <span
                      key={session}
                      className={`pill ${
                        /OVERLAP|NEW_YORK|LONDON/i.test(session) ? "pill-active" : ""
                      }`}
                    >
                      {formatKeyLabel(session)}
                    </span>
                  ))
                ) : (
                  <span className="pill">No active session</span>
                )}
              </div>
            </div>
          </div>
        </article>

        <article className="panel">
          <h3>Execution Preview</h3>
          <div className="placeholder-copy">{detail.executionPreview.message}</div>
          <div className="kv-list">
            <div className="kv-row">
              <span>Method</span>
              <strong>{detail.executionPreview.contract.method}</strong>
            </div>
            <div className="kv-row">
              <span>Path</span>
              <strong>{detail.executionPreview.contract.path}</strong>
            </div>
            <div className="kv-row">
              <span>Expected Fields</span>
              <strong>{detail.executionPreview.contract.fields.join(", ")}</strong>
            </div>
          </div>
        </article>
      </div>

      <div className="detail-grid">
        <article className="panel">
          <h3>Long Setup Read</h3>
          <div className="kv-list">
            <div className="kv-row">
              <span>Score</span>
              <strong>{longSummary.score}</strong>
            </div>
            <div className="kv-row">
              <span>Trend</span>
              <strong>{longSummary.trend}</strong>
            </div>
            <div className="kv-row">
              <span>Effort</span>
              <strong>{longSummary.effort}</strong>
            </div>
          </div>
          <p className="panel-copy">{longSummary.note}</p>
          <JsonAccordion title="Long Confluence Raw" payload={detail.analytics.longConfluence} />
        </article>

        <article className="panel">
          <h3>Short Setup Read</h3>
          <div className="kv-list">
            <div className="kv-row">
              <span>Score</span>
              <strong>{shortSummary.score}</strong>
            </div>
            <div className="kv-row">
              <span>Trend</span>
              <strong>{shortSummary.trend}</strong>
            </div>
            <div className="kv-row">
              <span>Effort</span>
              <strong>{shortSummary.effort}</strong>
            </div>
          </div>
          <p className="panel-copy">{shortSummary.note}</p>
          <JsonAccordion title="Short Confluence Raw" payload={detail.analytics.shortConfluence} />
        </article>
      </div>

      <article className="panel">
        <h3>Module Flow / Linkage</h3>
        <div className="module-grid">
          {detail.modules.map((module) => (
            <div key={module.id} className="module-card">
              <div className="module-heading">
                <div>
                  <strong>{module.label}</strong>
                  <div className="subtle">
                    {module.capability === "asset-detail"
                      ? `Asset-scoped for ${detail.symbol}`
                      : "Status-only: asset-scoped endpoint not available."}
                  </div>
                </div>
                <span className={`badge ${module.connected ? "tone-positive" : "tone-negative"}`}>
                  {module.connected ? "Connected" : "Disconnected"}
                </span>
              </div>
              <code className="endpoint-code">{module.url}</code>
              <div className="kv-list">
                {extractModuleFields(module).map((field) => (
                  <div key={`${module.id}-${field.key}`} className="kv-row">
                    <span>{formatKeyLabel(field.key)}</span>
                    <strong>{field.value}</strong>
                  </div>
                ))}
                {"updated_at" in (module.status || {}) ? (
                  <div className="kv-row">
                    <span>Updated</span>
                    <strong>{formatTime(String(module.status?.updated_at || ""))}</strong>
                  </div>
                ) : null}
              </div>
              <div className="panel-copy">{module.note}</div>
            </div>
          ))}
        </div>
      </article>

      <article className="panel">
        <h3>Live Module Prints</h3>
        <div className="module-grid">
          {detail.modules.map((module) => {
            const entries = moduleFeedEntries(module);
            return (
              <div key={`${module.id}-feed`} className="module-card">
                <div className="module-heading">
                  <div>
                    <strong>{module.label} Feed</strong>
                    <div className="subtle">Streaming the clicked asset: {detail.symbol}</div>
                  </div>
                  <span className={`badge ${module.connected ? "tone-positive" : "tone-muted"}`}>
                    {module.connected ? "Live Poll" : "Unavailable"}
                  </span>
                </div>
                {entries.length ? (
                  <div className="feed-list">
                    {entries.map((entry) => (
                      <div key={entry.id} className="feed-row">
                        <span className="feed-time">{entry.time}</span>
                        <strong className={toneClass(entry.accent)}>{entry.primary}</strong>
                        {entry.secondary ? <span className="feed-meta">{entry.secondary}</span> : null}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="empty-state">
                    No recent {module.label.toLowerCase()} prints yet for {detail.symbol}.
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </article>

      <article className="panel">
        <h3>Raw Responses</h3>
        <div className="accordion-grid">
          <JsonAccordion title="Fusion Response" payload={detail.analytics.fusion} />
          <JsonAccordion title="Structure Response" payload={detail.analytics.structure} />
          <JsonAccordion title="Patterns Response" payload={detail.analytics.patterns} />
          <JsonAccordion title="Volstats Response" payload={detail.analytics.volstats} />
        </div>
      </article>

      <article className="panel">
        <h3>Backend Gaps</h3>
        <div className="gap-list">
          {detail.backendGaps.map((gap) => (
            <div key={gap} className="gap-item">
              {gap}
            </div>
          ))}
        </div>
      </article>
    </section>
  );
}
