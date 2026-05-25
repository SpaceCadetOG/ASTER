"use client";

import { useEffect, useMemo, useState } from "react";
import { AssetDetailPanel } from "@/components/AssetDetailPanel";
import { AssetTable } from "@/components/AssetTable";
import { MetricTile } from "@/components/MetricTile";
import { Tabs } from "@/components/Tabs";
import {
  clampText,
  formatCompactUsd,
  formatNumber,
  formatPercent,
  formatSignedUsd,
  formatUsd,
  formatTime
} from "@/lib/format";
import type {
  AssetDetail,
  DashboardData,
  DashboardTab,
  EndpointStatus,
  LiveScanItem,
  ScannerSide,
  ScannerView
} from "@/lib/types";

async function getDashboard(): Promise<DashboardData> {
  const res = await fetch("/api/dashboard", { cache: "no-store" });
  if (!res.ok) {
    throw new Error("dashboard fetch failed");
  }
  return res.json();
}

async function getAssetDetail(symbol: string): Promise<AssetDetail> {
  const res = await fetch(`/api/asset/${encodeURIComponent(symbol)}`, {
    cache: "no-store"
  });
  if (!res.ok) {
    throw new Error("asset detail fetch failed");
  }
  return res.json();
}

function scannerEmptyState(scanner: ScannerView | null, unavailable: string, empty: string) {
  if (!scanner) return unavailable;
  if (scanner.state === "unavailable" || !scanner.connected) return unavailable;
  if (!scanner.rows.length) return empty;
  return "";
}

function statusTone(state: EndpointStatus["state"]) {
  if (state === "live") return "tone-positive";
  if (state === "stale") return "tone-amber";
  if (state === "disconnected") return "tone-negative";
  return "tone-muted";
}

function statusLabel(state: EndpointStatus["state"]) {
  if (state === "live") return "LIVE";
  if (state === "stale") return "STALE";
  if (state === "disconnected") return "DISCONNECTED";
  return "UNAVAILABLE";
}

function gradeTone(value?: string) {
  const key = (value || "N/A").toUpperCase();
  if (key === "A+") return "grade-aplus";
  if (key === "A") return "grade-a";
  if (key === "B") return "grade-b";
  if (key === "C") return "grade-c";
  if (key === "D") return "grade-d";
  return "tone-muted";
}

function numericTone(value: number | null | undefined) {
  if (value === null || value === undefined || Number.isNaN(value)) return "tone-muted";
  if (value > 0) return "tone-positive";
  if (value < 0) return "tone-negative";
  return "tone-muted";
}

function summarizeAccountMode(connected?: boolean, dryRun?: boolean, liveEnabled?: boolean) {
  if (!connected) return "Unavailable";
  return `${dryRun ? "Paper" : "Live"} / ${liveEnabled ? "Enabled" : "Disabled"}`;
}

function paperAccountHero(data: DashboardData | null) {
  const paper = data?.live?.paper;
  if (!paper) return null;
  const netDay = (paper.realizedToday || 0) + (paper.openPnl || 0);
  return (
    <div className="account-hero-list">
      <div className="account-hero-row">
        <span>Balance</span>
        <strong>{formatUsdValue(paper.balance)}</strong>
      </div>
      <div className="account-hero-row">
        <span>Equity</span>
        <strong>{formatUsdValue(paper.equity)}</strong>
      </div>
      <div className="account-hero-row">
        <span>Open PnL</span>
        <strong className={numericTone(paper.openPnl)}>{formatSignedUsd(paper.openPnl)}</strong>
      </div>
      <div className="account-hero-row">
        <span>Net Day</span>
        <strong className={numericTone(netDay)}>{formatSignedUsd(netDay)}</strong>
      </div>
    </div>
  );
}

function runtimeAccountHero(data: DashboardData | null, runtimeAccount: ReturnType<typeof deriveRuntimeAccount>) {
  const latestClosedTrade = data?.live?.paper?.recentClosed?.[0];
  const sourceLabel =
    runtimeAccount.source === "live"
      ? "Exchange Snapshot"
      : runtimeAccount.source === "paper"
        ? "Paper Fallback"
        : "Unavailable";
  return (
    <div className="account-hero-list">
      <div className="account-hero-row">
        <span>Source</span>
        <strong>{sourceLabel}</strong>
      </div>
      <div className="account-hero-row">
        <span>Balance</span>
        <strong>{formatUsdValue(runtimeAccount.balance)}</strong>
      </div>
      <div className="account-hero-row">
        <span>Equity</span>
        <strong>{formatUsdValue(runtimeAccount.equity)}</strong>
      </div>
      <div className="account-hero-row">
        <span>Open PnL</span>
        <strong className={numericTone(runtimeAccount.openPnl)}>{formatSignedUsd(runtimeAccount.openPnl)}</strong>
      </div>
      <div className="account-hero-row">
        <span>Latest Exit</span>
        <strong className={latestClosedTrade ? numericTone(latestClosedTrade.pnlUsd) : "tone-muted"}>
          {latestClosedTrade
            ? `${latestClosedTrade.symbol} ${formatSignedUsd(latestClosedTrade.pnlUsd)}`
            : "No recent exits"}
        </strong>
      </div>
    </div>
  );
}

function formatUsdValue(value: number | null | undefined) {
  return formatUsd(value);
}

function normalizeTradeSide(value: string | undefined): ScannerSide {
  return value?.toUpperCase() === "SELL" || value?.toUpperCase() === "SHORT" ? "short" : "long";
}

function deriveRuntimeAccount(data: DashboardData | null) {
  const liveAccount = data?.live?.liveAccount;
  const paper = data?.live?.paper;
  const hasLiveSnapshot =
    !!liveAccount &&
    (
      Math.abs(liveAccount.availableUsdt || 0) > 0.000001 ||
      Math.abs(liveAccount.equity || 0) > 0.000001 ||
      Math.abs(liveAccount.openPnl || 0) > 0.000001 ||
      Math.abs(liveAccount.realizedDay || 0) > 0.000001 ||
      (liveAccount.openCount || 0) > 0 ||
      (liveAccount.botCount || 0) > 0 ||
      (liveAccount.manualCount || 0) > 0
    );

  if (hasLiveSnapshot && liveAccount) {
    return {
      source: "live" as const,
      balance: liveAccount.availableUsdt,
      availableUsdt: liveAccount.availableUsdt,
      equity: liveAccount.equity,
      openPnl: liveAccount.openPnl,
      realizedDay: liveAccount.realizedDay,
      openCount: liveAccount.openCount,
      botCount: liveAccount.botCount,
      manualCount: liveAccount.manualCount,
      health: liveAccount.health || "LIVE",
      healthDetail: liveAccount.healthDetail
    };
  }

  if (paper) {
    return {
      source: "paper" as const,
      balance: paper.balance,
      availableUsdt: Math.max(0, paper.balance - paper.reserve),
      equity: paper.equity,
      openPnl: paper.openPnl,
      realizedDay: paper.realizedToday,
      openCount: paper.openCount,
      botCount: 0,
      manualCount: 0,
      health: data?.live?.connected ? "PAPER_FALLBACK" : "DISCONNECTED",
      healthDetail:
        "Live account snapshot unavailable, showing paper/runtime balances while auto trading is disabled."
    };
  }

  return {
    source: "none" as const,
    balance: 0,
    availableUsdt: data?.live?.availableUsdt || 0,
    equity: 0,
    openPnl: 0,
    realizedDay: 0,
    openCount: 0,
    botCount: 0,
    manualCount: 0,
    health: data?.live?.connected ? "UNKNOWN" : "DISCONNECTED",
    healthDetail: undefined
  };
}

export function DashboardShell() {
  const [tab, setTab] = useState<DashboardTab>("overview");
  const [scannerTab, setScannerTab] = useState<"long" | "short" | "live">("long");
  const [accountTab, setAccountTab] = useState<"paper" | "live">("paper");
  const [data, setData] = useState<DashboardData | null>(null);
  const [selectedSymbol, setSelectedSymbol] = useState<string>("");
  const [selectedSide, setSelectedSide] = useState<ScannerSide>("long");
  const [detail, setDetail] = useState<AssetDetail | undefined>(undefined);
  const [error, setError] = useState("");
  const [hasInitializedSelection, setHasInitializedSelection] = useState(false);

  useEffect(() => {
    let cancelled = false;

    const run = async () => {
      try {
        const payload = await getDashboard();
        if (cancelled) return;
        setData(payload);
        setError("");

        if (!hasInitializedSelection) {
          const initial =
            payload.live?.topSymbol ||
            payload.longScanner?.rows[0]?.symbol ||
            payload.shortScanner?.rows[0]?.symbol ||
            "";
          if (initial) {
            setSelectedSymbol(initial);
            setHasInitializedSelection(true);
          }
        }
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to load dashboard");
      }
    };

    run();
    const timer = setInterval(run, 12_000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [hasInitializedSelection]);

  useEffect(() => {
    if (!selectedSymbol) return;
    let cancelled = false;
    const run = () => {
      getAssetDetail(selectedSymbol)
        .then((payload) => {
          if (!cancelled) {
            setDetail(payload);
          }
        })
        .catch(() => undefined);
    };
    run();
    const timer = setInterval(run, 4000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [selectedSymbol]);

  const liveRows = useMemo<Array<LiveScanItem & { scannerSide: ScannerSide }>>(() => {
    if (!data?.live) return [];
    return [
      ...(data.live.scannerLongs || []).map((row) => ({ ...row, scannerSide: "long" as const })),
      ...(data.live.scannerShorts || []).map((row) => ({ ...row, scannerSide: "short" as const }))
    ].sort((a, b) => b.score - a.score);
  }, [data]);

  const scannerAssetOptions = useMemo<Array<{ symbol: string; side: ScannerSide; source: string; score: number }>>(() => {
    const seen = new Set<string>();
    const options: Array<{ symbol: string; side: ScannerSide; source: string; score: number }> = [];
    const add = (symbol: string, side: ScannerSide, source: string, score: number) => {
      const clean = symbol.trim();
      if (!clean || seen.has(clean)) return;
      seen.add(clean);
      options.push({ symbol: clean, side, source, score });
    };
    (data?.longScanner?.rows || []).forEach((row) => add(row.symbol, "long", "Long", row.score));
    (data?.shortScanner?.rows || []).forEach((row) => add(row.symbol, "short", "Short", row.score));
    liveRows.forEach((row) => add(row.symbol, row.scannerSide, "Live", row.score));
    return options.sort((a, b) => b.score - a.score || a.symbol.localeCompare(b.symbol));
  }, [data, liveRows]);

  const selectedScanner =
    scannerTab === "short"
      ? data?.shortScanner || null
      : scannerTab === "live"
        ? null
        : data?.longScanner || null;

  const runtimeGenerated = data?.live?.generated || data?.generatedAt;
  const paperHero = paperAccountHero(data);
  const runtimeAccount = deriveRuntimeAccount(data);
  const liveHero = runtimeAccountHero(data, runtimeAccount);
  const latestClosedTrade = data?.live?.paper?.recentClosed?.[0];
  const runtimeAccountSummary =
    accountTab === "paper"
      ? "Paper ledger snapshot from the live runtime. Click any paper asset row below to open Asset Detail."
      : runtimeAccount.healthDetail ||
        "Live account snapshot from the runtime. Funds, equity, open PnL, and position counts remain read-only.";
  const selectedSummaryRow =
    data?.longScanner?.rows.find((row) => row.symbol === selectedSymbol) ||
    data?.shortScanner?.rows.find((row) => row.symbol === selectedSymbol) ||
    liveRows.find((row) => row.symbol === selectedSymbol);

  const handleSelect = (symbol: string, side: ScannerSide) => {
    setSelectedSymbol(symbol);
    setSelectedSide(side);
    setTab("scanners");
  };

  const handleScannerAssetSelect = (value: string) => {
    const selected = scannerAssetOptions.find((option) => option.symbol === value);
    if (!selected) return;
    setSelectedSymbol(selected.symbol);
    setSelectedSide(selected.side);
  };

  const handleAssetJump = (symbol: string, fallbackSide?: ScannerSide | string) => {
    const inferredSide: ScannerSide =
      data?.longScanner?.rows.some((row) => row.symbol === symbol)
        ? "long"
        : data?.shortScanner?.rows.some((row) => row.symbol === symbol)
          ? "short"
          : liveRows.find((row) => row.symbol === symbol)?.scannerSide ||
            (typeof fallbackSide === "string" ? normalizeTradeSide(fallbackSide) : fallbackSide) ||
            selectedSide;
    handleSelect(symbol, inferredSide);
  };

  if (!data) {
    return (
      <main className="page">
        <div className="page-header">
          <div>
            <div className="eyebrow">ASTER</div>
            <h1 className="title">ASTER Unified Operator Portal</h1>
          </div>
          <span className="subtle">Loading dashboard…</span>
        </div>
      </main>
    );
  }

  const longEmpty = scannerEmptyState(
    data.longScanner,
    "Scanner unavailable.",
    "Scanner connected but no rows."
  );
  const shortEmpty = scannerEmptyState(
    data.shortScanner,
    "Scanner unavailable.",
    "Scanner connected but no rows."
  );
  const hotlistEmpty = !data.live
    ? "Live hotlist unavailable."
    : !data.live.connected
      ? "Live hotlist unavailable."
      : liveRows.length
      ? ""
      : "Live hotlist connected but no rows.";
  const failedEndpoints = data.endpoints.filter((endpoint) => endpoint.state !== "live");
  const tickerMessage = failedEndpoints
    .map((endpoint) => {
      const failed = endpoint.failedEndpoint ? ` · failed ${endpoint.failedEndpoint}` : "";
      const updated = endpoint.lastUpdated ? ` · last ${formatTime(endpoint.lastUpdated)}` : "";
      return `${endpoint.label}: ${statusLabel(endpoint.state)}${updated}${failed}`;
    })
    .join("   |   ");
  const modeTone =
    data.mode === "live" ? "tone-positive" : data.mode === "degraded" ? "tone-amber" : "tone-negative";

  return (
    <main className="page">
      <div className="page-header">
        <div>
          <div className="eyebrow">ASTER Operator Shell</div>
          <h1 className="title">ASTER Unified Operator Portal</h1>
        </div>
        <div className="page-header-meta">
          <span className="subtle">Updated {formatTime(data.generatedAt)}</span>
          <span className={`badge ${modeTone}`}>
            {data.mode.toUpperCase()}
          </span>
        </div>
      </div>

      {error ? <div className="banner warning">API warning: {error}</div> : null}
      {failedEndpoints.length ? (
        <div className="ticker-banner" role="status" aria-label="Backend status ticker">
          <div className="ticker-track">
            <span className="ticker-copy">{tickerMessage}</span>
            <span className="ticker-copy" aria-hidden="true">
              {tickerMessage}
            </span>
          </div>
        </div>
      ) : null}

      <section className="hero-grid">
        <MetricTile
          label="Top Candidate"
          value={`${data.live?.topSymbol || "N/A"} ${data.live?.topSide || ""}`.trim()}
        />
        <MetricTile
          label="Top Grade"
          value={data.live?.topGrade || "N/A"}
          valueClassName={`grade-text ${gradeTone(data.live?.topGrade)}`}
        />
        <MetricTile label="Top Score" value={formatNumber(data.live?.topScore, 2)} />
        <MetricTile label="Top Slope" value={formatNumber(data.live?.topSlope, 3)} />
        <MetricTile
          label="Runtime Mode"
          value={
            !data.live?.connected
              ? "UNAVAILABLE"
              : data.live?.dryRun
                ? "DRY_RUN"
                : "LIVE"
          }
        />
        <MetricTile label="Available USDT" value={formatCompactUsd(data.live?.availableUsdt)} />
      </section>

      <Tabs active={tab} onChange={setTab} />

      {tab === "overview" ? (
        <section className="panel-stack">
          <div className="panel overview-grid">
            <div>
              <h3>Scanner Coverage</h3>
              <div className="metric-grid-2">
                <MetricTile
                  label="Long Scanner"
                  value={data.longScanner ? String(data.longScanner.rows.length) : "Unavailable"}
                />
                <MetricTile
                  label="Short Scanner"
                  value={data.shortScanner ? String(data.shortScanner.rows.length) : "Unavailable"}
                />
                <MetricTile
                  label="Live Longs"
                  value={String(data.live?.scannerLongs?.length || 0)}
                />
                <MetricTile
                  label="Live Shorts"
                  value={String(data.live?.scannerShorts?.length || 0)}
                />
              </div>
            </div>
            <div>
              <h3>Runtime Snapshot</h3>
              <div className="kv-list">
                <div className="kv-row">
                  <span>Paper / Live</span>
                  <strong>
                    {!data.live?.connected
                      ? "Unavailable"
                      : `${data.live?.dryRun ? "Paper Mode" : "Live Mode"} / ${
                          data.live?.liveEnabled ? "Enabled" : "Disabled"
                        }`}
                  </strong>
                </div>
                <div className="kv-row">
                  <span>Generated</span>
                  <strong>{formatTime(runtimeGenerated)}</strong>
                </div>
                <div className="kv-row">
                  <span>Account Summary</span>
                  <strong>{clampText(runtimeAccountSummary, 110) || "Unavailable"}</strong>
                </div>
              </div>
            </div>
          </div>

          <div className="panel">
            <h3>Live Hotlist / In-Play</h3>
            <AssetTable
              kind="live"
              rows={liveRows.slice(0, 12)}
              emptyMessage={hotlistEmpty}
              variant="ticker"
              onSelect={handleSelect}
            />
          </div>

          <div className="overview-columns">
            <div className="panel">
              <h3>Long Scanner</h3>
              <AssetTable
                kind="scanner"
                side="long"
                rows={(data.longScanner?.rows || []).slice(0, 10)}
                emptyMessage={longEmpty}
                onSelect={handleSelect}
              />
            </div>
            <div className="panel">
              <h3>Short Scanner</h3>
              <AssetTable
                kind="scanner"
                side="short"
                rows={(data.shortScanner?.rows || []).slice(0, 10)}
                emptyMessage={shortEmpty}
                onSelect={handleSelect}
              />
            </div>
          </div>
        </section>
      ) : null}

      {tab === "scanners" ? (
        <section className="panel-stack">
          <div className="scanner-toolbar">
            <div className="subtabs">
              <button
                className={`tab ${scannerTab === "long" ? "active" : ""}`}
                onClick={() => setScannerTab("long")}
              >
                Long Scanner
              </button>
              <button
                className={`tab ${scannerTab === "short" ? "active" : ""}`}
                onClick={() => setScannerTab("short")}
              >
                Short Scanner
              </button>
              <button
                className={`tab ${scannerTab === "live" ? "active" : ""}`}
                onClick={() => setScannerTab("live")}
              >
                Live Hotlist
              </button>
            </div>
            <label className="asset-picker">
              <span>Asset Detail</span>
              <select value={selectedSymbol} onChange={(event) => handleScannerAssetSelect(event.target.value)}>
                <option value="">Select asset</option>
                {scannerAssetOptions.map((option) => (
                  <option key={`${option.source}-${option.symbol}`} value={option.symbol}>
                    {option.symbol} · {option.source} · {formatNumber(option.score, 1)}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {scannerTab === "live" ? (
            <div className="panel">
              <h3>Live Hotlist</h3>
              <AssetTable
                kind="live"
                rows={liveRows}
                emptyMessage={hotlistEmpty}
                variant="table"
                onSelect={handleSelect}
              />
            </div>
          ) : (
            <div className="panel">
              <h3>{scannerTab === "long" ? "Long Scanner" : "Short Scanner"}</h3>
              <div className="panel-copy">
                {selectedScanner?.exchange || "Scanner unavailable"} · active sessions{" "}
                {selectedScanner?.active.join(" / ") || "N/A"} · updated{" "}
                {formatTime(selectedScanner?.generated)} · {statusLabel(selectedScanner?.health || "unavailable")}
              </div>
              <AssetTable
                kind="scanner"
                side={scannerTab}
                rows={selectedScanner?.rows || []}
                emptyMessage={scannerTab === "long" ? longEmpty : shortEmpty}
                onSelect={handleSelect}
              />
            </div>
          )}
          {selectedSymbol ? <AssetDetailPanel detail={detail} /> : null}
        </section>
      ) : null}

      {tab === "hotlist" ? (
        <section className="panel">
          <h3>Live Hotlist / In-Play</h3>
          <AssetTable
            kind="live"
            rows={liveRows}
            emptyMessage={hotlistEmpty}
            variant="table"
            onSelect={handleSelect}
          />
        </section>
      ) : null}

      {tab === "runtime" ? (
        <section className="panel runtime-grid">
          <MetricTile
            label="Runtime Mode"
            value={
              !data.live?.connected
                ? "UNAVAILABLE"
                : data.live?.dryRun
                  ? "DRY_RUN"
                  : "LIVE"
            }
          />
          <MetricTile
            label="Trading Status"
            value={
              !data.live?.connected
                ? "Unavailable"
                : data.live.liveEnabled
                  ? "Enabled"
                  : "Disabled"
            }
          />
          <MetricTile
            label="Top Candidate"
            value={`${data.live?.topSymbol || "N/A"} ${data.live?.topSide || ""}`.trim()}
          />
          <MetricTile
            label="Top Grade"
            value={data.live?.topGrade || "N/A"}
            valueClassName={`grade-text ${gradeTone(data.live?.topGrade)}`}
          />
          <MetricTile label="Top Score" value={formatNumber(data.live?.topScore, 2)} />
          <MetricTile label="Top Slope" value={formatNumber(data.live?.topSlope, 3)} />
          <MetricTile label="Open Exec" value={String(data.live?.exec?.open || 0)} />
          <MetricTile label="Pending Exec" value={String(data.live?.exec?.pending || 0)} />
          <MetricTile label="Closed Exec" value={String(data.live?.exec?.closed || 0)} />
          <MetricTile label="Available USDT" value={formatCompactUsd(data.live?.availableUsdt)} />
          <MetricTile label="Generated" value={formatTime(runtimeGenerated)} />
          <MetricTile
            label="Account Summary"
            value={clampText(runtimeAccountSummary, 64) || "Unavailable"}
          />
        </section>
      ) : null}

      {tab === "paper" ? (
        <section className="panel-stack">
          <div className="panel">
            <div className="section-head">
              <div>
                <h3>Account Summary</h3>
                <div className="panel-copy">Paper ledger and runtime account posture.</div>
              </div>
              <div className="subtabs">
                <button
                  className={`tab ${accountTab === "paper" ? "active" : ""}`}
                  onClick={() => setAccountTab("paper")}
                >
                  Paper
                </button>
                <button
                  className={`tab ${accountTab === "live" ? "active" : ""}`}
                  onClick={() => setAccountTab("live")}
                >
                  Live
                </button>
              </div>
            </div>
            {accountTab === "paper" ? (
              <div className="account-summary-grid">
                <div className="tile tile-summary">
                  <div className="tile-label">Paper Account</div>
                  <div className="tile-value tile-value-large">
                    {paperHero || "Paper account unavailable."}
                  </div>
                  <div className="tile-subcopy">
                    {clampText(runtimeAccountSummary, 160) || "Structured paper account view."}
                  </div>
                </div>
                <MetricTile label="Balance" value={formatUsdValue(data.live?.paper?.balance)} />
                <MetricTile label="Equity" value={formatUsdValue(data.live?.paper?.equity)} />
                <MetricTile label="Reserve" value={formatUsdValue(data.live?.paper?.reserve)} />
                <MetricTile
                  label="Open PnL"
                  value={formatSignedUsd(data.live?.paper?.openPnl)}
                  valueClassName={numericTone(data.live?.paper?.openPnl)}
                />
                <MetricTile
                  label="Realized Today"
                  value={formatSignedUsd(data.live?.paper?.realizedToday)}
                  valueClassName={numericTone(data.live?.paper?.realizedToday)}
                />
                <MetricTile
                  label="Net Day"
                  value={formatSignedUsd((data.live?.paper?.realizedToday || 0) + (data.live?.paper?.openPnl || 0))}
                  valueClassName={numericTone((data.live?.paper?.realizedToday || 0) + (data.live?.paper?.openPnl || 0))}
                />
                <MetricTile label="Open Positions" value={String(data.live?.paper?.openCount || 0)} />
                <MetricTile label="Recent Closed" value={String(data.live?.paper?.recentClosedCount || 0)} />
                <MetricTile label="Recent Decisions" value={String(data.live?.paper?.recentDecisionCount || 0)} />
                <MetricTile label="Generated" value={formatTime(runtimeGenerated)} />
              </div>
            ) : (
              <div className="account-summary-grid">
                <div className="tile tile-summary">
                  <div className="tile-label">
                    {runtimeAccount.source === "live" ? "Exchange Account View" : "Paper Fallback View"}
                  </div>
                  <div className="tile-value tile-value-large">
                    {liveHero}
                  </div>
                  <div className="tile-subcopy">
                    {clampText(runtimeAccountSummary, 180) || "Runtime account summary unavailable."}
                  </div>
                </div>
                <MetricTile
                  label="Runtime Mode"
                  value={!data.live?.connected ? "UNAVAILABLE" : data.live?.mode?.toUpperCase() || (data.live?.dryRun ? "DRY_RUN" : "LIVE")}
                />
                <MetricTile
                  label="Trading Status"
                  value={!data.live?.connected ? "Unavailable" : data.live?.liveEnabled ? "Enabled" : data.live?.dryRun ? "Paper Safe" : "Disabled"}
                />
                <MetricTile
                  label="Account Balance"
                  value={formatUsdValue(runtimeAccount.balance)}
                />
                <MetricTile
                  label="Available USDT"
                  value={formatUsdValue(runtimeAccount.availableUsdt)}
                />
                <MetricTile
                  label="Account Equity"
                  value={formatUsdValue(runtimeAccount.equity)}
                />
                <MetricTile
                  label="Open PnL"
                  value={formatSignedUsd(runtimeAccount.openPnl)}
                  valueClassName={numericTone(runtimeAccount.openPnl)}
                />
                <MetricTile
                  label="Realized Day"
                  value={formatSignedUsd(runtimeAccount.realizedDay)}
                  valueClassName={numericTone(runtimeAccount.realizedDay)}
                />
                <MetricTile
                  label="Account Mode"
                  value={runtimeAccount.source === "paper"
                    ? "Paper / Disabled"
                    : summarizeAccountMode(data.live?.connected, data.live?.dryRun, data.live?.liveEnabled)}
                />
                <MetricTile
                  label="Live Health"
                  value={runtimeAccount.health}
                />
                <MetricTile label="Open Positions" value={String(runtimeAccount.openCount || 0)} />
                <MetricTile label="Bot Positions" value={String(runtimeAccount.botCount || 0)} />
                <MetricTile label="Manual Positions" value={String(runtimeAccount.manualCount || 0)} />
                <MetricTile label="Open Exec" value={String(data.live?.exec?.open || 0)} />
                <MetricTile label="Pending Exec" value={String(data.live?.exec?.pending || 0)} />
                <MetricTile label="Closed Exec" value={String(data.live?.exec?.closed || 0)} />
                <MetricTile label="Generated" value={formatTime(runtimeGenerated)} />
              </div>
            )}
          </div>

          <div className="panel">
            <h3>Open Paper Positions</h3>
            {!data.live?.paper?.openPositions?.length ? (
              <div className="placeholder-card">No active paper positions exposed by the runtime.</div>
            ) : (
              <div className="table-wrap">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Symbol</th>
                      <th>Side</th>
                      <th>Strategy</th>
                      <th>Grade</th>
                      <th>State</th>
                      <th>Entry</th>
                      <th>Mark</th>
                      <th>Stop</th>
                      <th>TP</th>
                      <th>PnL</th>
                      <th>MFE/MAE</th>
                      <th>Opened</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.live.paper.openPositions.map((pos) => (
                      <tr key={`${pos.symbol}-${pos.side}-${pos.openedAt || pos.entryPrice}`}>
                        <td>
                          <button
                            className="table-link paper-asset-link"
                            onClick={() => handleAssetJump(pos.symbol, pos.side)}
                          >
                            <strong className="table-symbol">{pos.symbol}</strong>
                            <small>{pos.setupFamily || pos.triggerState || "open position"}</small>
                          </button>
                        </td>
                        <td>{pos.side}</td>
                        <td className="paper-strategy-cell">
                          <strong>{pos.strategy || "N/A"}</strong>
                          <br />
                          <small>{pos.mode || pos.source || "paper"}</small>
                        </td>
                        <td>
                          <span className={`grade-text ${gradeTone(pos.grade)}`}>{pos.grade || "N/A"}</span>
                        </td>
                        <td>{pos.state || "N/A"}</td>
                        <td>{formatNumber(pos.entryPrice, 4)}</td>
                        <td>{formatNumber(pos.markPrice, 4)}</td>
                        <td>{formatNumber(pos.stopPrice, 4)}</td>
                        <td>
                          {formatNumber(pos.tp1, 4)}
                          {pos.tp2 ? ` / ${formatNumber(pos.tp2, 4)}` : ""}
                        </td>
                        <td className={numericTone(pos.unrealizedPnl)}>
                          <strong>{formatSignedUsd(pos.unrealizedPnl)}</strong>
                          <br />
                          <small className="pnl-subvalue">{formatPercent(pos.unrealizedPct, 2)}</small>
                        </td>
                        <td>
                          {formatNumber(pos.mfeR, 2)} / {formatNumber(pos.maeR, 2)}
                        </td>
                        <td>{formatTime(pos.openedAt)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div className="panel">
            <h3>Recent Closed Paper Trades</h3>
            <div className="panel-copy">Trade history from the runtime event log.</div>
            {!data.live?.paper?.recentClosed?.length ? (
              <div className="placeholder-card">No recent closed paper trades exposed by the runtime.</div>
            ) : (
              <div className="table-wrap">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Closed</th>
                      <th>Symbol</th>
                      <th>Side</th>
                      <th>Strategy</th>
                      <th>Grade</th>
                      <th>Entry</th>
                      <th>Exit</th>
                      <th>PnL</th>
                      <th>R</th>
                      <th>MFE/MAE</th>
                      <th>Reason</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.live.paper.recentClosed.map((trade) => (
                      <tr key={`${trade.symbol}-${trade.closedAt || trade.exitPrice}`}>
                        <td>{formatTime(trade.closedAt)}</td>
                        <td>
                          <button
                            className="table-link paper-asset-link"
                            onClick={() => handleAssetJump(trade.symbol, trade.side)}
                          >
                            <strong className="table-symbol">{trade.symbol}</strong>
                            <small>{trade.setupFamily || trade.triggerState || "closed trade"}</small>
                          </button>
                        </td>
                        <td>{trade.side}</td>
                        <td className="paper-strategy-cell">
                          <strong>{trade.strategy || "N/A"}</strong>
                          <br />
                          <small>{trade.mode || trade.source || "paper"}</small>
                        </td>
                        <td>
                          <span className={`grade-text ${gradeTone(trade.grade)}`}>{trade.grade || "N/A"}</span>
                        </td>
                        <td>{formatNumber(trade.entryPrice, 4)}</td>
                        <td>{formatNumber(trade.exitPrice, 4)}</td>
                        <td className={numericTone(trade.pnlUsd)}>
                          <strong>{formatSignedUsd(trade.pnlUsd)}</strong>
                          <br />
                          <small className="pnl-subvalue">{formatPercent(trade.pnlPct, 2)}</small>
                        </td>
                        <td>{formatNumber(trade.riskR, 2)}</td>
                        <td>
                          {formatNumber(trade.mfeR, 2)} / {formatNumber(trade.maeR, 2)}
                        </td>
                        <td>{trade.exitReason || "N/A"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div className="panel">
            <h3>Recent Paper Decisions</h3>
            {!data.live?.paper?.recentDecisions?.length ? (
              <div className="placeholder-card">No recent paper decisions exposed by the runtime.</div>
            ) : (
              <div className="table-wrap">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Time</th>
                      <th>Symbol</th>
                      <th>Side</th>
                      <th>Strategy</th>
                      <th>Grade</th>
                      <th>Score</th>
                      <th>Decision</th>
                      <th>Reason</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.live.paper.recentDecisions.map((decision) => (
                      <tr key={`${decision.symbol}-${decision.decidedAt || decision.entryPrice}`}>
                        <td>{formatTime(decision.decidedAt)}</td>
                        <td>
                          <button
                            className="table-link paper-asset-link"
                            onClick={() => handleAssetJump(decision.symbol, decision.side)}
                          >
                            <strong className="table-symbol">{decision.symbol}</strong>
                            <small>{decision.setupFamily || decision.triggerState || "decision"}</small>
                          </button>
                        </td>
                        <td>{decision.side}</td>
                        <td className="paper-strategy-cell">
                          <strong>{decision.strategy || "N/A"}</strong>
                          <br />
                          <small>{decision.setupFamily || decision.mode || "paper"}</small>
                        </td>
                        <td>
                          <span className={`grade-text ${gradeTone(decision.grade)}`}>{decision.grade || "N/A"}</span>
                        </td>
                        <td>
                          {formatNumber(decision.score, 2)}
                          <br />
                          <small>slope {formatNumber(decision.slope, 3)}</small>
                        </td>
                        <td className={decision.approved ? "tone-positive" : "tone-negative"}>
                          {decision.approved ? "APPROVED" : "REJECTED"}
                        </td>
                        <td>{decision.rejectReason || decision.gateReasons?.[0] || "approved"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </section>
      ) : null}

      {tab === "health" ? (
        <section className="panel-stack">
          <div className="panel">
            <h3>Backend Endpoint Map</h3>
            <div className="health-grid">
              {data.endpoints.map((endpoint) => (
                <div key={endpoint.id} className="health-card">
                  <div className="module-heading">
                    <div>
                      <strong>{endpoint.label}</strong>
                      <div className="subtle">{endpoint.scope}</div>
                    </div>
                    <span className={`badge ${statusTone(endpoint.state)}`}>
                      {statusLabel(endpoint.state)}
                    </span>
                  </div>
                  <code className="endpoint-code">{endpoint.url}</code>
                  <div className="panel-copy">{endpoint.detail}</div>
                  {endpoint.lastUpdated ? (
                    <div className="panel-copy">Last known timestamp: {formatTime(endpoint.lastUpdated)}</div>
                  ) : null}
                  {endpoint.failedEndpoint ? (
                    <div className="panel-copy">Failed endpoint: {endpoint.failedEndpoint}</div>
                  ) : null}
                </div>
              ))}
            </div>
          </div>
          <div className="panel">
            <h3>Health Notes</h3>
            <div className="gap-list">
              <div className="gap-item">
                Browser calls stay behind Next API routes; no private VM URLs are called directly
                from the client.
              </div>
              <div className="gap-item">
                When upstream endpoints are unavailable, the UI shows explicit unavailable or stale
                states instead of silently inventing data.
              </div>
            </div>
          </div>
        </section>
      ) : null}

      <footer className="footer-strip">
        <span>Selected symbol: {selectedSymbol || "None"}</span>
        <span>Selected side: {selectedSide.toUpperCase()}</span>
        <span>
          Selected move: {selectedSummaryRow ? formatPercent("dayUtc24h" in selectedSummaryRow ? selectedSummaryRow.dayUtc24h : selectedSummaryRow.dayUtc, 1) : "N/A"}
        </span>
        <span>
          Selected score: {selectedSummaryRow ? formatNumber(selectedSummaryRow.score, 2) : "N/A"}
        </span>
      </footer>
    </main>
  );
}
