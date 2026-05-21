"use client";

import { useEffect, useMemo, useState } from "react";
import { AssetDetailPanel } from "@/components/AssetDetailPanel";
import { AssetTable } from "@/components/AssetTable";
import { MetricTile } from "@/components/MetricTile";
import { Tabs } from "@/components/Tabs";
import {
  formatCompactUsd,
  formatNumber,
  formatPercent,
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

export function DashboardShell() {
  const [tab, setTab] = useState<DashboardTab>("overview");
  const [scannerTab, setScannerTab] = useState<"long" | "short" | "live">("long");
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

  const selectedScanner =
    scannerTab === "short"
      ? data?.shortScanner || null
      : scannerTab === "live"
        ? null
        : data?.longScanner || null;

  const runtimeGenerated = data?.live?.generated || data?.generatedAt;

  const handleSelect = (symbol: string, side: ScannerSide) => {
    setSelectedSymbol(symbol);
    setSelectedSide(side);
    setTab("asset");
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
        <MetricTile label="Top Grade" value={data.live?.topGrade || "N/A"} />
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
                  <span>Paper Summary</span>
                  <strong>{data.live?.paperSummary || "Unavailable"}</strong>
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
          <MetricTile label="Top Grade" value={data.live?.topGrade || "N/A"} />
          <MetricTile label="Top Score" value={formatNumber(data.live?.topScore, 2)} />
          <MetricTile label="Top Slope" value={formatNumber(data.live?.topSlope, 3)} />
          <MetricTile label="Open Exec" value={String(data.live?.exec?.open || 0)} />
          <MetricTile label="Pending Exec" value={String(data.live?.exec?.pending || 0)} />
          <MetricTile label="Closed Exec" value={String(data.live?.exec?.closed || 0)} />
          <MetricTile label="Available USDT" value={formatCompactUsd(data.live?.availableUsdt)} />
          <MetricTile label="Generated" value={formatTime(runtimeGenerated)} />
          <MetricTile label="Paper Summary" value={data.live?.paperSummary || "Unavailable"} />
        </section>
      ) : null}

      {tab === "paper" ? (
        <section className="panel-stack">
          <div className="panel runtime-grid">
            <MetricTile label="Paper Summary" value={data.live?.paper?.summary || data.live?.paperSummary || "Unavailable"} />
            <MetricTile label="Open Paper" value={String(data.live?.paper?.openCount || 0)} />
            <MetricTile label="Recent Closed" value={String(data.live?.paper?.recentClosedCount || 0)} />
            <MetricTile label="Recent Decisions" value={String(data.live?.paper?.recentDecisionCount || 0)} />
            <MetricTile label="Generated" value={formatTime(runtimeGenerated)} />
          </div>
          <div className="panel runtime-grid">
            <MetricTile
              label="Mode"
              value={data.live?.paper?.mode || (data.live?.dryRun ? "paper" : "unavailable")}
            />
            <MetricTile label="Dry Run" value={data.live?.dryRun ? "true" : "false"} />
            <MetricTile label="Live Enabled" value={data.live?.liveEnabled ? "true" : "false"} />
            <MetricTile label="Paper Equity" value={formatCompactUsd(data.live?.paper?.equity)} />
            <MetricTile label="Paper Balance" value={formatCompactUsd(data.live?.paper?.balance)} />
            <MetricTile label="Open PnL" value={formatCompactUsd(data.live?.paper?.openPnl)} />
            <MetricTile label="Realized Today" value={formatCompactUsd(data.live?.paper?.realizedToday)} />
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
                        <td>{pos.symbol}</td>
                        <td>{pos.side}</td>
                        <td>
                          <strong>{pos.strategy || "N/A"}</strong>
                          <br />
                          <small>{pos.mode || pos.source || "paper"}</small>
                        </td>
                        <td>{pos.grade || "N/A"}</td>
                        <td>{pos.state || "N/A"}</td>
                        <td>{formatNumber(pos.entryPrice, 4)}</td>
                        <td>{formatNumber(pos.markPrice, 4)}</td>
                        <td>{formatNumber(pos.stopPrice, 4)}</td>
                        <td>
                          {formatNumber(pos.tp1, 4)}
                          {pos.tp2 ? ` / ${formatNumber(pos.tp2, 4)}` : ""}
                        </td>
                        <td>
                          <strong>{formatCompactUsd(pos.unrealizedPnl)}</strong>
                          <br />
                          <small>{formatPercent(pos.unrealizedPct, 2)}</small>
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
                        <td>{trade.symbol}</td>
                        <td>{trade.side}</td>
                        <td>
                          <strong>{trade.strategy || "N/A"}</strong>
                          <br />
                          <small>{trade.mode || trade.source || "paper"}</small>
                        </td>
                        <td>{formatNumber(trade.entryPrice, 4)}</td>
                        <td>{formatNumber(trade.exitPrice, 4)}</td>
                        <td>
                          <strong>{formatCompactUsd(trade.pnlUsd)}</strong>
                          <br />
                          <small>{formatPercent(trade.pnlPct, 2)}</small>
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
                        <td>{decision.symbol}</td>
                        <td>{decision.side}</td>
                        <td>
                          <strong>{decision.strategy || "N/A"}</strong>
                          <br />
                          <small>{decision.setupFamily || decision.mode || "paper_auto"}</small>
                        </td>
                        <td>{decision.grade || "N/A"}</td>
                        <td>
                          {formatNumber(decision.score, 2)}
                          <br />
                          <small>slope {formatNumber(decision.slope, 3)}</small>
                        </td>
                        <td>{decision.approved ? "APPROVED" : "REJECTED"}</td>
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

      {tab === "asset" ? (
        <AssetDetailPanel detail={detail} />
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
        <span>Top day move: {formatPercent(data.live?.scannerLongs?.[0]?.dayUtc, 1)}</span>
      </footer>
    </main>
  );
}
