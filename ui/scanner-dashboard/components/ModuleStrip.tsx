import type { ModuleSummary } from "@/lib/types";

export function ModuleStrip({ modules }: { modules: ModuleSummary[] }) {
  return (
    <div className="module-grid">
      {modules.map((module) => (
        <div key={module.id} className="module-card">
          <div className="module-top">
            <div>
              <strong>{module.label}</strong>
              <div className="subtle" style={{ marginTop: 4 }}>
                {module.capability === "asset-detail"
                  ? "Asset-scoped"
                  : "Status-only: asset-scoped endpoint not available."}
              </div>
            </div>
            <span className={`badge ${module.connected ? "tone-positive" : "tone-negative"}`}>
              {module.connected ? "Connected" : "Disconnected"}
            </span>
          </div>
          <div className="subtle module-source">{module.url}</div>
          <div className="subtle module-note" style={{ marginTop: 6 }}>
            {module.note}
          </div>
        </div>
      ))}
    </div>
  );
}
