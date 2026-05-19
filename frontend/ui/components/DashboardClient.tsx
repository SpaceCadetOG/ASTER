"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { LiveScanList } from "@/components/LiveScanList";
import { MetricCard } from "@/components/MetricCard";
import { ModuleGrid } from "@/components/ModuleGrid";
import { ScannerTable } from "@/components/ScannerTable";
import { formatCompactUsd, formatNumber, formatTime } from "@/lib/format";
import type { DashboardData } from "@/lib/types";

async function getDashboard(): Promise<DashboardData> {
  const res = await fetch("/api/dashboard", { cache: "no-store" });
  if (!res.ok) {
    throw new Error("Dashboard request failed");
  }
  return res.json();
}

export function DashboardClient({ initialData }: { initialData: DashboardData }) {
  const [data, setData] = useState(initialData);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      try {
        const next = await getDashboard();
        if (!cancelled) {
          setData(next);
          setError("");
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to refresh dashboard");
        }
      }
    };
    const timer = setInterval(refresh, 10000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  const longRows = data.longScanner?.rows || [];
  const shortRows = data.shortScanner?.rows || [];
  const topLong = longRows[0];
  const topShort = shortRows[0];
  const liveRows = [
    ...((data.live?.scannerLongs || []).map((row) => ({ ...row, scannerSide: "long" as const }))),
    ...((data.live?.scannerShorts || []).map((row) => ({ ...row, scannerSide: "short" as const })))
  ].sort((a, b) => b.score - a.score);
  const longEmptyMessage = data.longScanner
    ? "No long candidates currently qualify on the scanner."
    : "Long scanner API is unavailable.";
  const shortEmptyMessage = data.shortScanner
    ? "No short candidates currently qualify on the scanner."
    : "Short scanner API is unavailable.";
  const liveEmptyMessage = data.live
    ? "No live scans are active right now."
    : "Live scanner feed is unavailable.";

  return (
    <>
      <section className="hero-panel reveal">
        <div>
          <span className="eyebrow">Scanner Split</span>
          <h1>ASTER momentum scanner, with a public frontend and global flow surface</h1>
          <p>
            Long, short, and live scanner flows stay separated from the Go runtime HTML, while
            the public UI carries token drilldown, trade prep, and market-wide flow visibility.
          </p>
        </div>
        <div className="hero-actions">
          <Link href="/scanners/long" className="cta-primary">
            Open Long Scanner
          </Link>
          <Link href="/scanners/short" className="cta-secondary">
            Open Short Scanner
          </Link>
        </div>
      </section>

      {error ? <div className="banner-error">{error}</div> : null}

      <section className="metric-grid">
        <MetricCard
          label="Long Eligible"
          value={String(longRows.length)}
          hint={topLong ? `Lead ${topLong.symbol} @ ${formatNumber(topLong.score, 2)}` : "No rows"}
        />
        <MetricCard
          label="Short Eligible"
          value={String(shortRows.length)}
          hint={topShort ? `Lead ${topShort.symbol} @ ${formatNumber(topShort.score, 2)}` : "No rows"}
        />
        <MetricCard
          label="Live Mode"
          value={data.live?.dryRun ? "Paper" : data.live?.liveEnabled ? "Live" : "Watch"}
          hint={data.live?.paperSummary || "Live status unavailable"}
        />
        <MetricCard
          label="Available USDT"
          value={formatCompactUsd(data.live?.availableUsdt)}
          hint={data.live?.topDecisionWhy || "No decision narrative"}
        />
      </section>

      <section className="scanner-grid">
        <article className="panel reveal">
          <div className="section-heading">
            <div>
              <span className="eyebrow">Long Scanner</span>
              <h2>{data.longScanner?.exchange || "Unavailable"}</h2>
            </div>
            <span className="timestamp">{formatTime(data.longScanner?.generated)}</span>
          </div>
          <p className="panel-copy">
            Sessions: {(data.longScanner?.active || []).join(" • ") || "No session tags"}
          </p>
          <ScannerTable
            rows={longRows.slice(0, 8)}
            side="long"
            emptyMessage={longEmptyMessage}
          />
        </article>

        <article className="panel reveal">
          <div className="section-heading">
            <div>
              <span className="eyebrow">Short Scanner</span>
              <h2>{data.shortScanner?.exchange || "Unavailable"}</h2>
            </div>
            <span className="timestamp">{formatTime(data.shortScanner?.generated)}</span>
          </div>
          <p className="panel-copy">
            Sessions: {(data.shortScanner?.active || []).join(" • ") || "No session tags"}
          </p>
          <ScannerTable
            rows={shortRows.slice(0, 8)}
            side="short"
            emptyMessage={shortEmptyMessage}
          />
        </article>
      </section>

      <section className="panel reveal">
        <div className="section-heading">
          <div>
            <span className="eyebrow">In-Play List</span>
            <h2>All live scans</h2>
          </div>
          <span className="timestamp">{formatTime(data.live?.generated || data.generatedAt)}</span>
        </div>
        <p className="panel-copy">
          Combined live scanner flow from the current paper/live engine across long and short.
        </p>
        <LiveScanList rows={liveRows} emptyMessage={liveEmptyMessage} />
      </section>

      <section className="panel reveal">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Market Surface</span>
            <h2>Global flow monitor</h2>
          </div>
          <span className="timestamp">{formatTime(data.generatedAt)}</span>
        </div>
        <ModuleGrid modules={data.modules} />
      </section>
    </>
  );
}
