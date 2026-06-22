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

function formatUsdValue(value: number | null | undefined) {
  return formatUsd(value);
}

function displayRuntimeMode(data: DashboardData | null) {
  if (!data?.live?.connected) return "UNAVAILABLE";
  const mode = data.live.mode?.trim();
  if (mode) return mode.toUpperCase();
  return data.live.dryRun ? "PAPER" : "LIVE";
}

function displayAvailableUsdt(data: DashboardData | null) {
  const liveAccount = data?.live?.liveAccount;
  if (hasUsableLiveAccount(data) && liveAccount) return liveAccount.availableUsdt;
  return data?.live?.paper?.availableUsdt ?? data?.live?.availableUsdt;
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
      balance: liveAccount.marginBalance || liveAccount.balance || liveAccount.equity,
      availableUsdt: liveAccount.availableUsdt,
      marginUsed: liveAccount.marginUsed,
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
      availableUsdt: paper.availableUsdt,
      marginUsed: paper.marginUsed,
      equity: paper.equity,
      openPnl: paper.openPnl,
      realizedDay: paper.realizedToday,
      openCount: paper.openCount,
      botCount: paper?.openCount || 0,
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
    marginUsed: 0,
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

function hasUsableLiveAccount(data: DashboardData | null) {
  const liveAccount = data?.live?.liveAccount;
  if (!liveAccount) return false;
  const generated = liveAccount.generated || "";
  return (
    (!!generated && !generated.startsWith("0001-")) ||
    !!liveAccount.health ||
    Math.abs(liveAccount.availableUsdt || 0) > 0.000001 ||
    Math.abs(liveAccount.equity || 0) > 0.000001 ||
    Math.abs(liveAccount.openPnl || 0) > 0.000001 ||
    Math.abs(liveAccount.realizedDay || 0) > 0.000001 ||
    (liveAccount.openCount || 0) > 0 ||
    (liveAccount.positions?.length || 0) > 0
  );
}

function deriveTradeAccount(data: DashboardData | null, accountTab: "paper" | "live") {
  const paper = data?.live?.paper;
  const liveAccount = data?.live?.liveAccount;
  const liveReady = hasUsableLiveAccount(data);

  if (accountTab === "paper") {
    const openPnl = paper?.openPnl || 0;
    const realizedDay = paper?.realizedToday || 0;
    return {
      source: "paper" as const,
      title: "Paper Account",
      summaryLabel: "Paper Ledger",
      summary:
        "Paper ledger snapshot from the live runtime. Click any paper asset row below to open Asset Detail.",
      balance: paper?.balance,
      availableUsdt: paper?.availableUsdt,
      marginUsed: paper?.marginUsed || 0,
      equity: paper?.equity,
      openPnl,
      realizedDay,
      netDay: realizedDay + openPnl,
      openCount: paper?.openCount || 0,
      botCount: paper?.openCount || 0,
      manualCount: 0,
      health: paper ? "PAPER" : "UNAVAILABLE",
      generated: runtimeGeneratedValue(data)
    };
  }

  const openPnl = liveReady ? liveAccount?.openPnl || 0 : undefined;
  const realizedDay = liveReady ? liveAccount?.realizedDay || 0 : undefined;
  const balance = liveReady ? liveAccount?.marginBalance || liveAccount?.balance || liveAccount?.equity : undefined;
  const equity = liveReady ? liveAccount?.equity || balance : undefined;
  return {
    source: liveReady ? "live" as const : "none" as const,
    title: "Live Account",
    summaryLabel: liveReady ? "Exchange Snapshot" : "Exchange Snapshot Unavailable",
    summary: liveReady
      ? "Read-only exchange account snapshot from Aster. This shows real user funds; execution remains controlled by runtime safety flags."
      : "Live account snapshot is unavailable. Paper balances are not used in this Live view.",
    balance,
    availableUsdt: liveReady ? liveAccount?.availableUsdt : undefined,
    marginUsed: liveReady ? liveAccount?.marginUsed || Math.max(0, (balance || 0) - (liveAccount?.availableUsdt || 0)) : undefined,
    equity,
    openPnl,
    realizedDay,
    netDay:
      openPnl === undefined || realizedDay === undefined
        ? undefined
        : realizedDay + openPnl,
    openCount: liveReady ? liveAccount?.openCount || 0 : 0,
    botCount: liveReady ? liveAccount?.botCount || 0 : 0,
    manualCount: liveReady ? liveAccount?.manualCount || 0 : 0,
    health: liveReady ? liveAccount?.health || "LIVE" : "UNAVAILABLE",
    generated: liveReady ? liveAccount?.generated || runtimeGeneratedValue(data) : runtimeGeneratedValue(data)
  };
}

function runtimeGeneratedValue(data: DashboardData | null) {
  return data?.live?.generated || data?.generatedAt;
}

export function DashboardShell() {
  const [tab, setTab] = useState<DashboardTab>("overview");
  const [scannerTab, setScannerTab] = useState<"long" | "short" | "live">("long");
  const [accountTab, setAccountTab] = useState<"paper" | "live">("paper");
  const [data, setData] = useState<DashboardData | null>(null);
  const [lastLiveAccount, setLastLiveAccount] = useState<
    NonNullable<NonNullable<DashboardData["live"]>["liveAccount"]> | undefined
  >(undefined);
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
        if (hasUsableLiveAccount(payload)) {
          setLastLiveAccount(payload.live?.liveAccount);
        }
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

  const accountData = useMemo<DashboardData | null>(() => {
    if (!data || hasUsableLiveAccount(data) || !lastLiveAccount || !data.live) {
      return data;
    }
    return {
      ...data,
      live: {
        ...data.live,
        liveAccount: lastLiveAccount
      }
    };
  }, [data, lastLiveAccount]);

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

  const runtimeGenerated = runtimeGeneratedValue(accountData);
  const runtimeAccount = deriveRuntimeAccount(accountData);
  const tradeAccount = deriveTradeAccount(accountData, accountTab);
  const runtimeAccountSummary =
    accountTab === "paper"
      ? tradeAccount.summary
      : runtimeAccount.healthDetail ||
        tradeAccount.summary;
  const paperOpenPositions = accountData?.live?.paper?.openPositions || [];
  const liveOpenPositions = accountData?.live?.liveAccount?.positions || [];
  const tradeOpenPositions = accountTab === "paper" ? paperOpenPositions : liveOpenPositions;
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
          value={displayRuntimeMode(data)}
        />
        <MetricTile label="Available USDT" value={formatCompactUsd(displayAvailableUsdt(accountData))} />
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
            value={displayRuntimeMode(data)}
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
            <div className="account-summary-grid">
              <div className="tile tile-summary">
                <div className="tile-label">{tradeAccount.title}</div>
                <div className="tile-value tile-value-large">
                  <div className="account-hero-list">
                    <div className="account-hero-row">
                      <span>Source</span>
                      <strong>{tradeAccount.summaryLabel}</strong>
                    </div>
                    <div className="account-hero-row">
                      <span>Balance</span>
                      <strong>{formatUsdValue(tradeAccount.balance)}</strong>
                    </div>
                    <div className="account-hero-row">
                      <span>Equity</span>
                      <strong>{formatUsdValue(tradeAccount.equity)}</strong>
                    </div>
                    <div className="account-hero-row">
                      <span>Open PnL</span>
                      <strong className={numericTone(tradeAccount.openPnl)}>{formatSignedUsd(tradeAccount.openPnl)}</strong>
                    </div>
                    <div className="account-hero-row">
                      <span>Net Day</span>
                      <strong className={numericTone(tradeAccount.netDay)}>{formatSignedUsd(tradeAccount.netDay)}</strong>
                    </div>
                  </div>
                </div>
                <div className="tile-subcopy">
                  {clampText(runtimeAccountSummary, 180) || "Runtime account summary unavailable."}
                </div>
              </div>
              <MetricTile label="Balance" value={formatUsdValue(tradeAccount.balance)} />
              <MetricTile label="Equity" value={formatUsdValue(tradeAccount.equity)} />
              <MetricTile label="Available USDT" value={formatUsdValue(tradeAccount.availableUsdt)} />
              <MetricTile label="Margin Used" value={formatUsdValue(tradeAccount.marginUsed)} />
              <MetricTile
                label="Open PnL"
                value={formatSignedUsd(tradeAccount.openPnl)}
                valueClassName={numericTone(tradeAccount.openPnl)}
              />
              <MetricTile
                label="Realized Today"
                value={formatSignedUsd(tradeAccount.realizedDay)}
                valueClassName={numericTone(tradeAccount.realizedDay)}
              />
              <MetricTile
                label="Net Day"
                value={formatSignedUsd(tradeAccount.netDay)}
                valueClassName={numericTone(tradeAccount.netDay)}
              />
              <MetricTile
                label="Runtime Mode"
                value={displayRuntimeMode(data)}
              />
              <MetricTile
                label="Trading Status"
                value={!data?.live?.connected ? "Unavailable" : data.live?.liveEnabled ? "Enabled" : data.live?.dryRun ? "Paper Safe" : "Disabled"}
              />
              <MetricTile label="Health" value={tradeAccount.health} />
              <MetricTile label="Open Positions" value={String(tradeAccount.openCount || 0)} />
              <MetricTile label="Bot Positions" value={String(tradeAccount.botCount || 0)} />
              <MetricTile label="Manual Positions" value={String(tradeAccount.manualCount || 0)} />
              <MetricTile label="Generated" value={formatTime(tradeAccount.generated || runtimeGenerated)} />
            </div>
          </div>

          <div className="panel">
            <h3>{accountTab === "paper" ? "Open Paper Positions" : "Open Live Positions"}</h3>
            {!tradeOpenPositions.length ? (
              <div className="placeholder-card">
                {accountTab === "paper"
                  ? "No active paper positions exposed by the runtime."
                  : "No active live exchange positions exposed by the runtime."}
              </div>
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
                    {accountTab === "paper"
                      ? paperOpenPositions.map((pos) => (
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
                      ))
                      : liveOpenPositions.map((pos) => (
                        <tr key={`${pos.symbol}-${pos.side}-${pos.entryPrice}`}>
                          <td>
                            <button
                              className="table-link paper-asset-link"
                              onClick={() => handleAssetJump(pos.symbol, pos.side?.toLowerCase() === "short" ? "short" : "long")}
                            >
                              <strong className="table-symbol">{pos.symbol}</strong>
                              <small>{pos.entryReason || pos.source || "exchange position"}</small>
                            </button>
                          </td>
                          <td>{pos.side}</td>
                          <td className="paper-strategy-cell">
                            <strong>{pos.source || "EXCHANGE"}</strong>
                            <br />
                            <small>{pos.manageState || pos.protectionState || "live"}</small>
                          </td>
                          <td>
                            <span className="grade-text tone-muted">N/A</span>
                          </td>
                          <td>{pos.protected ? "PROTECTED" : pos.protectionState || "N/A"}</td>
                          <td>{formatNumber(pos.entryPrice, 4)}</td>
                          <td>{formatNumber(pos.markPrice || pos.lastPrice, 4)}</td>
                          <td>{formatNumber(pos.stopPrice, 4)}</td>
                          <td>N/A</td>
                          <td className={numericTone(pos.unrealizedPnl)}>
                            <strong>{formatSignedUsd(pos.unrealizedPnl)}</strong>
                            <br />
                            <small className="pnl-subvalue">{formatPercent(pos.unrealizedPnlPct, 2)}</small>
                          </td>
                          <td>N/A</td>
                          <td>{formatNumber(pos.holdMin, 1)}m</td>
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
