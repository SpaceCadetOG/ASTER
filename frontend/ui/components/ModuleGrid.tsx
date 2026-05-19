import { formatTime } from "@/lib/format";
import type { ModuleSummary } from "@/lib/types";

function extractLabelValue(status: Record<string, unknown> | null) {
  if (!status) {
    return [];
  }
  const keys = [
    "symbol",
    "subscribed",
    "signal",
    "score",
    "dominant_side",
    "window_sec",
    "min_usd",
    "large_usd",
    "events",
    "count",
    "buy_usd",
    "sell_usd",
    "delta_usd",
    "total_usd",
    "large_count",
    "last_symbol",
    "last_side",
    "last_usd",
    "last_price",
    "last_qty",
    "burst"
  ];
  return keys
    .filter((key) => key in status)
    .map((key) => ({
      key,
      value: String(status[key])
    }));
}

export function ModuleGrid({ modules }: { modules: ModuleSummary[] }) {
  return (
    <div className="module-grid">
      {modules.map((module) => (
        <article className="module-card reveal" key={module.id}>
          <div className="module-heading">
            <div>
              <span className="eyebrow">{module.label}</span>
            </div>
            <span className={`status-pill ${module.connected ? "status-ok" : "status-down"}`}>
              {module.status?.subscribed === false
                ? "not tracked"
                : module.symbolMatch
                  ? "tracking asset"
                  : "module status"}
            </span>
          </div>
          <p>{module.note}</p>
          <div className="kv-list">
            {extractLabelValue(module.status).map((item) => (
              <div key={`${module.id}-${item.key}`} className="kv-row">
                <span>{item.key}</span>
                <span>{item.value}</span>
              </div>
            ))}
            {"updated_at" in (module.status || {}) ? (
              <div className="kv-row">
                <span>updated</span>
                <span>{formatTime(String(module.status?.updated_at || ""))}</span>
              </div>
            ) : null}
          </div>
          {Array.isArray(module.status?.recent) && module.status.recent.length > 0 ? (
            <div className="recent-list">
              {module.status.recent.slice(0, 5).map((item, index) => {
                const row = item as Record<string, unknown>;
                return (
                  <div className="recent-row" key={`${module.id}-${index}`}>
                    <span>{formatTime(String(row.ts || row.Ts || ""))}</span>
                    <span>{String(row.side || row.last_side || "-")}</span>
                    <span>{String(row.usd || row.last_usd || "-")}</span>
                  </div>
                );
              })}
            </div>
          ) : null}
          <code>{module.url}</code>
        </article>
      ))}
    </div>
  );
}
