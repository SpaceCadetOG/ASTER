"use client";

import type { DashboardTab } from "@/lib/types";

const tabs: Array<{ key: DashboardTab; label: string }> = [
  { key: "overview", label: "Overview" },
  { key: "scanners", label: "Scanners" },
  { key: "hotlist", label: "Live Hotlist / In-Play" },
  { key: "runtime", label: "Runtime" },
  { key: "paper", label: "Paper" },
  { key: "asset", label: "Asset Detail" },
  { key: "health", label: "Health" }
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
      {tabs.map((tab) => (
        <button
          key={tab.key}
          className={`tab ${active === tab.key ? "active" : ""}`}
          onClick={() => onChange(tab.key)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}
