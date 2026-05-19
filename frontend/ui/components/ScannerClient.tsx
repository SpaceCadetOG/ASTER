"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { MetricCard } from "@/components/MetricCard";
import { ScannerTable } from "@/components/ScannerTable";
import { formatNumber, formatTime } from "@/lib/format";
import type { ScannerSide, ScannerView } from "@/lib/types";

async function getScanner(side: ScannerSide): Promise<ScannerView | null> {
  const res = await fetch(`/api/scanner/${side}`, { cache: "no-store" });
  if (!res.ok) {
    throw new Error("Scanner request failed");
  }
  return res.json();
}

export function ScannerClient({
  side,
  initialData
}: {
  side: ScannerSide;
  initialData: ScannerView | null;
}) {
  const [data, setData] = useState(initialData);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      try {
        const next = await getScanner(side);
        if (!cancelled) {
          setData(next);
          setError("");
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Scanner refresh failed");
        }
      }
    };
    const timer = setInterval(refresh, 10000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [side]);

  const rows = data?.rows || [];
  const top = rows[0];
  const emptyMessage = data
    ? `No ${side} candidates currently qualify on the scanner.`
    : `${side} scanner API is unavailable.`;

  return (
    <>
      <section className="hero-panel reveal">
        <div>
          <span className="eyebrow">{side.toUpperCase()} Scanner</span>
          <h1>{data?.exchange || "Scanner unavailable"}</h1>
          <p>
            Dedicated scanner route that preserves ASTER’s current long/short product behavior
            while moving the UI out of Go binaries.
          </p>
        </div>
        <div className="hero-actions">
          <Link
            href={`/scanners/${side === "long" ? "short" : "long"}`}
            className="cta-secondary"
          >
            Switch to {side === "long" ? "Short" : "Long"}
          </Link>
        </div>
      </section>

      {error ? <div className="banner-error">{error}</div> : null}

      <section className="metric-grid">
        <MetricCard
          label="Generated"
          value={formatTime(data?.generated)}
          hint={(data?.active || []).join(" • ") || "No session tags"}
        />
        <MetricCard
          label="Eligible"
          value={String(rows.length)}
          hint={top ? `${top.symbol} leads at ${formatNumber(top.score, 2)}` : "No rows"}
        />
        <MetricCard
          label="In-Play"
          value={String(data?.inPlay.length || 0)}
          hint={data?.inPlay[0] ? `${data.inPlay[0].symbol} ${data.inPlay[0].state}` : "No in-play rows"}
        />
      </section>

      <section className="panel reveal">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Scanner Table</span>
            <h2>{side === "long" ? "Long opportunities" : "Short opportunities"}</h2>
          </div>
          <span className="timestamp">auto-refresh 10s</span>
        </div>
        <ScannerTable
          rows={rows}
          side={side}
          emptyMessage={emptyMessage}
        />
      </section>
    </>
  );
}
