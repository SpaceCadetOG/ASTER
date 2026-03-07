"use client";

import type { DashboardTab } from "@/lib/types";

const tabs: { key: DashboardTab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "inplay", label: "In-Play" },
  { key: "long", label: "Long Scanner" },
  { key: "short", label: "Short Scanner" },
  { key: "live", label: "Live-Lite Portal" },
  { key: "asset", label: "Asset Detail" }
];

export function Tabs({
  active,
  onChange
}: {
  active: DashboardTab;
  onChange: (tab: DashboardTab) => void;
}) {
  return (
    <div className="tabs">
      {tabs.map((t) => (
        <button
          key={t.key}
          className={`tab ${active === t.key ? "active" : ""}`}
          onClick={() => onChange(t.key)}
        >
          {t.label}
        </button>
      ))}
    </div>
  );
}
