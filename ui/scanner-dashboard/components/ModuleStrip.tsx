import type { ModuleCardData } from "@/lib/types";

export function ModuleStrip({ modules }: { modules: ModuleCardData[] }) {
  return (
    <div className="module-grid">
      {modules.map((m) => (
        <div key={m.key} className="module-card">
          <div className="module-top">
            <strong>{m.label}</strong>
            <span
              className={`status-dot ${
                m.status === "ok"
                  ? "status-ok"
                  : m.status === "warn"
                    ? "status-warn"
                    : "status-down"
              }`}
            />
          </div>
          <div className="subtle module-source">{m.source}</div>
          <div className="subtle module-note" style={{ marginTop: 6 }}>
            {m.note}
          </div>
        </div>
      ))}
    </div>
  );
}
