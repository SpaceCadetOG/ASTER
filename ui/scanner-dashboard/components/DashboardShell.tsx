"use client";

import { useEffect, useMemo, useState } from "react";
import { Tabs } from "@/components/Tabs";
import { MetricTile } from "@/components/MetricTile";
import { AssetTable } from "@/components/AssetTable";
import { ModuleStrip } from "@/components/ModuleStrip";
import { AssetDetailPanel } from "@/components/AssetDetailPanel";
import type { AssetDetail, DashboardData, DashboardTab } from "@/lib/types";

async function getDashboard(): Promise<DashboardData> {
  const res = await fetch("/api/dashboard", { cache: "no-store" });
  if (!res.ok) throw new Error("dashboard fetch failed");
  return res.json();
}

async function getAssetDetail(symbol: string): Promise<AssetDetail> {
  const res = await fetch(`/api/asset/${encodeURIComponent(symbol)}`, {
    cache: "no-store"
  });
  if (!res.ok) throw new Error("asset detail fetch failed");
  return res.json();
}

export function DashboardShell() {
  const [tab, setTab] = useState<DashboardTab>("overview");
  const [scannerView, setScannerView] = useState<"long" | "short">("long");
  const [data, setData] = useState<DashboardData | null>(null);
  const [selected, setSelected] = useState<string>("");
  const [detail, setDetail] = useState<AssetDetail | undefined>(undefined);
  const [err, setErr] = useState<string>("");

  useEffect(() => {
    let cancelled = false;
    const run = async () => {
      try {
        const payload = await getDashboard();
        if (cancelled) return;
        setData(payload);
        setErr("");
        if (!selected && payload.assets[0]?.symbol) {
          setSelected(payload.assets[0].symbol);
        }
      } catch (e) {
        if (cancelled) return;
        setErr(e instanceof Error ? e.message : "failed to load");
      }
    };
    run();
    const t = setInterval(run, 10_000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, [selected]);

  useEffect(() => {
    if (!selected) return;
    let cancelled = false;
    getAssetDetail(selected)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [selected]);

  const rows = useMemo(() => {
    if (!data) return [];
    if (tab === "inplay") {
      const inplayRows = [...data.inPlayLong, ...data.inPlayShort].sort(
        (a, b) => (b.inPlayScore || 0) - (a.inPlayScore || 0)
      );
      if (inplayRows.length > 0) return inplayRows;
      return [...data.assets]
        .filter((a) => (a.inPlayScore || 0) > 0)
        .sort((a, b) => (b.inPlayScore || 0) - (a.inPlayScore || 0));
    }
    if (tab === "long") {
      return [...data.assets].sort((a, b) => (b.longScore || 0) - (a.longScore || 0));
    }
    if (tab === "short") {
      return [...data.assets].sort(
        (a, b) => (b.shortScore || 0) - (a.shortScore || 0)
      );
    }
    return data.assets;
  }, [data, tab]);

  if (!data) {
    return (
      <main className="page">
        <div className="header">
          <h1 className="title">ASTER Scanner Dashboard</h1>
          <span className="subtle">Loading…</span>
        </div>
      </main>
    );
  }

  return (
    <main className="page">
      <div className="header">
        <h1 className="title">ASTER Unified Scanner Portal</h1>
        <span className="subtle">
          mode={data.mode} · updated {new Date(data.overview.generatedAt).toLocaleTimeString()}
        </span>
      </div>
      {err ? <div className="subtle bad">API warning: {err}</div> : null}
      <div className="terminal-strip">
        {`/status  /balance  /positions  /pause  /resume  /close SYMBOL  /closeall\nsession=${data.overview.sessionTags.join("|") || "N/A"} long_inplay=${data.overview.longInPlay} short_inplay=${data.overview.shortInPlay} top=${data.overview.topSymbol || "-"}`}
      </div>

      <div className="row metrics-grid">
        <MetricTile label="Session" value={data.overview.sessionTags.join(", ") || "-"} />
        <MetricTile label="Long Eligible" value={String(data.overview.longEligible)} />
        <MetricTile label="Short Eligible" value={String(data.overview.shortEligible)} />
        <MetricTile label="Long In-Play" value={String(data.overview.longInPlay)} />
        <MetricTile label="Short In-Play" value={String(data.overview.shortInPlay)} />
        <MetricTile
          label="Top Candidate"
          value={`${data.overview.topSymbol || "-"} ${data.overview.topSide || ""} ${(
            data.overview.topScore || 0
          ).toFixed(2)}`}
        />
      </div>

      <Tabs
        active={tab}
        onChange={(next) => {
          setTab(next);
          if (next === "asset" && selected) {
            setSelected(selected);
          }
          if (next === "long") setScannerView("long");
          if (next === "short") setScannerView("short");
        }}
      />

      <div className="layout">
        <div className="row" style={{ gap: 12 }}>
          {tab === "overview" ? (
            <div className="panel">
              <h3>Scanner Portal</h3>
              <div className="scanner-tabs">
                <button
                  className={`scanner-tab ${scannerView === "long" ? "active" : ""}`}
                  onClick={() => setScannerView("long")}
                >
                  Long
                </button>
                <button
                  className={`scanner-tab ${scannerView === "short" ? "active" : ""}`}
                  onClick={() => setScannerView("short")}
                >
                  Short
                </button>
              </div>
              <div className="subtle" style={{ marginBottom: 8 }}>
                {scannerView === "long"
                  ? `${data.longScanner?.exchange || "asterdex (LONGS)"} · ${(
                      data.longScanner?.active || []
                    ).join("|") || "N/A"}`
                  : `${data.shortScanner?.exchange || "asterdex (SHORTS)"} · ${(
                      data.shortScanner?.active || []
                    ).join("|") || "N/A"}`}
              </div>
              <AssetTable
                rows={
                  scannerView === "long"
                    ? [...(data.longScanner?.rows || [])].sort(
                        (a, b) => (b.longScore || 0) - (a.longScore || 0)
                      )
                    : [...(data.shortScanner?.rows || [])].sort(
                        (a, b) => (b.shortScore || 0) - (a.shortScore || 0)
                      )
                }
                onSelect={(symbol) => {
                  setSelected(symbol);
                  setTab("asset");
                }}
              />
            </div>
          ) : null}

          {tab === "long" ? (
            <div className="panel">
              <h3>Long Scanner</h3>
              <AssetTable
                rows={[...(data.longScanner?.rows || [])].sort(
                  (a, b) => (b.longScore || 0) - (a.longScore || 0)
                )}
                onSelect={(symbol) => {
                  setSelected(symbol);
                  setTab("asset");
                }}
              />
            </div>
          ) : null}

          {tab === "short" ? (
            <div className="panel">
              <h3>Short Scanner</h3>
              <AssetTable
                rows={[...(data.shortScanner?.rows || [])].sort(
                  (a, b) => (b.shortScore || 0) - (a.shortScore || 0)
                )}
                onSelect={(symbol) => {
                  setSelected(symbol);
                  setTab("asset");
                }}
              />
            </div>
          ) : null}

          {tab === "live" ? (
            <div className="panel">
              <h3>Live Portal</h3>
              <div className="row" style={{ gridTemplateColumns: "repeat(3,minmax(0,1fr))", gap: 8 }}>
                <MetricTile label="Mode" value={data.live?.dryRun ? "DRY_RUN" : "LIVE"} />
                <MetricTile label="Available USDT" value={`${(data.live?.availableUSDT || 0).toFixed(4)}`} />
                <MetricTile
                  label="Top"
                  value={`${data.live?.topSymbol || "-"} ${data.live?.topSide || ""} ${(data.live?.topScore || 0).toFixed(2)}`}
                />
                <MetricTile label="Exec Open" value={String(data.live?.execOpen || 0)} />
                <MetricTile label="Exec Pending" value={String(data.live?.execPending || 0)} />
                <MetricTile label="Exec Closed" value={String(data.live?.execClosed || 0)} />
              </div>
              <div className="terminal-strip" style={{ marginTop: 10 }}>
                {`paper=${data.live?.paperSummary || "n/a"}\npayout_cycle=${data.live?.payoutCycleID || "n/a"} next=${data.live?.payoutNextAt || "n/a"}\ngenerated=${data.live?.generated || "n/a"}`}
              </div>
            </div>
          ) : null}

          {tab === "inplay" || tab === "asset" ? (
            <div className="panel">
              <h3>{tab === "inplay" ? "In-Play" : "Asset Selection"}</h3>
              <AssetTable
                rows={rows}
                onSelect={(symbol) => {
                  setSelected(symbol);
                  setTab("asset");
                }}
              />
            </div>
          ) : null}
        </div>

        <div className="row" style={{ gap: 12 }}>
          {tab === "asset" || tab === "inplay" || tab === "overview" ? (
            <AssetDetailPanel detail={detail} />
          ) : null}
          <div className="panel">
            <h3>Scanners + Cmd Modules</h3>
            <ModuleStrip modules={data.modules} />
          </div>
        </div>
      </div>
    </main>
  );
}
