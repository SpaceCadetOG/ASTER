"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { JsonCard } from "@/components/JsonCard";
import { ModuleGrid } from "@/components/ModuleGrid";
import { Sparkline } from "@/components/Sparkline";
import { TradePanel } from "@/components/TradePanel";
import {
  formatCompactUsd,
  formatNumber,
  formatPercent,
  formatTime
} from "@/lib/format";
import type { ModuleSummary, ScannerSide, TokenDetailData } from "@/lib/types";

async function getToken(symbol: string, side: string): Promise<TokenDetailData> {
  const res = await fetch(`/api/token/${encodeURIComponent(symbol)}?side=${side}`, {
    cache: "no-store"
  });
  if (!res.ok) {
    throw new Error("Token detail request failed");
  }
  return res.json();
}

function extractNotes(data: Record<string, unknown> | null) {
  if (!data || !Array.isArray(data.notes)) {
    return [];
  }
  return data.notes.map(String);
}

function extractScore(data: Record<string, unknown> | null) {
  return typeof data?.score === "number" ? data.score : 0;
}

function extractLabel(data: Record<string, unknown> | null) {
  return typeof data?.label === "string" ? data.label : "N/A";
}

function gradeClass(grade?: string) {
  const key = (grade || "N/A").toUpperCase();
  if (key === "A+") {
    return "grade-aplus";
  }
  if (key === "A") {
    return "grade-a";
  }
  if (key === "B") {
    return "grade-b";
  }
  if (key === "C") {
    return "grade-c";
  }
  return "grade-d";
}

function sessionLabel(active: string[]) {
  if (!active.length) {
    return "No active session";
  }
  return active
    .map((item) =>
      item
        .toLowerCase()
        .split("_")
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(" ")
    )
    .join(" • ");
}

function scannerTone(side: ScannerSide) {
  return side === "long" ? "tone-long" : "tone-short";
}

const moduleTabs: Array<{
  id: ModuleSummary["id"];
  label: string;
}> = [
  { id: "whale", label: "Whale" },
  { id: "tape", label: "Tape" },
  { id: "oflow", label: "Oflow" },
  { id: "liqs", label: "Liqs" }
];

export function TokenClient({ initialData }: { initialData: TokenDetailData }) {
  const [data, setData] = useState(initialData);
  const [error, setError] = useState("");
  const [flowTab, setFlowTab] = useState<ModuleSummary["id"]>("whale");
  const [setupTab, setSetupTab] = useState<ScannerSide>(initialData.requestedSide);

  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      try {
        const next = await getToken(initialData.symbol, initialData.requestedSide);
        if (!cancelled) {
          setData(next);
          setError("");
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to refresh token detail");
        }
      }
    };
    const timer = setInterval(refresh, 12000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [initialData.requestedSide, initialData.symbol]);

  const longScore = data.longScannerRow?.score || 0;
  const shortScore = data.shortScannerRow?.score || 0;
  const candles = data.analytics.candles?.data || [];
  const closes = candles.map((candle) => candle.C);
  const positive = (closes.at(-1) || 0) >= (closes[0] || 0);
  const activeConfluence =
    setupTab === "long" ? data.analytics.longConfluence : data.analytics.shortConfluence;
  const activeRow = setupTab === "long" ? data.longScannerRow : data.shortScannerRow;
  const activeModule = data.modules.find((module) => module.id === flowTab) || data.modules[0];

  const scannerNarrative = useMemo(() => {
    const parts = [];
    if (data.scannerRow) {
      parts.push(`${formatNumber(data.scannerRow.score, 2)} score`);
      parts.push(`${data.scannerRow.grade} grade`);
    }
    if (data.live?.topDecision) {
      parts.push(`live decision ${data.live.topDecision}`);
    }
    return parts.join(" • ");
  }, [data.live?.topDecision, data.scannerRow]);

  return (
    <>
      <section className="token-hero reveal">
        <div className="token-hero-head">
          <div>
            <div className="eyebrow">ASTER Momentum Scanner</div>
            <h1>{data.symbol}</h1>
          </div>
          <div className="token-hero-actions">
            <Link href={`/scanners/${data.requestedSide}`} className="cta-secondary">
              Back to {data.requestedSide === "long" ? "Long" : "Short"}
            </Link>
          </div>
        </div>

        <div className="score-strip">
          <div className={`score-panel ${scannerTone("long")}`}>
            <span className="eyebrow">Long Score</span>
            <div className="score-line">
              <strong>{formatNumber(longScore, 2)}</strong>
              <span className={`grade-badge ${gradeClass(data.longScannerRow?.grade)}`}>
                {data.longScannerRow?.grade || "N/A"}
              </span>
            </div>
          </div>
          <div className={`score-panel ${scannerTone("short")}`}>
            <span className="eyebrow">Short Score</span>
            <div className="score-line">
              <strong>{formatNumber(shortScore, 2)}</strong>
              <span className={`grade-badge ${gradeClass(data.shortScannerRow?.grade)}`}>
                {data.shortScannerRow?.grade || "N/A"}
              </span>
            </div>
          </div>
          <div className="score-panel">
            <span className="eyebrow">Time</span>
            <div className="score-line score-line-compact">
              <strong>{formatTime(data.generatedAt)}</strong>
            </div>
          </div>
        </div>
      </section>

      {error ? <div className="banner-error">{error}</div> : null}

      <section className="detail-grid">
        <article className="panel reveal">
          <div className="section-heading">
            <div>
              <span className="eyebrow">Scanner Context</span>
              <h2>Current read</h2>
            </div>
          </div>

          <div className="context-grid">
            <div className="context-card">
              <span className="eyebrow">Price Path</span>
              <Sparkline values={closes} positive={positive} />
            </div>
            <div className="context-card">
              <div className="scanner-context-list">
                <div className="scanner-context-row">
                  <span>Last Price:</span>
                  <strong>{formatNumber(data.scannerRow?.lastPrice, 4)}</strong>
                </div>
                <div className="scanner-context-row">
                  <span>Volume:</span>
                  <strong className={Number(data.scannerRow?.volumeUsd || 0) > 0 ? "up" : ""}>
                    {formatCompactUsd(data.scannerRow?.volumeUsd)}
                  </strong>
                </div>
                <div className="scanner-context-row">
                  <span>OI:</span>
                  <strong
                    className={Number(data.scannerRow?.openInterestUsd || 0) > 0 ? "up" : ""}
                  >
                    {formatCompactUsd(data.scannerRow?.openInterestUsd)}
                  </strong>
                </div>
                <div className="scanner-context-row">
                  <span>Funding:</span>
                  <strong
                    className={
                      (data.scannerRow?.fundingRatePct || 0) >= 0 ? "up" : "down"
                    }
                  >
                    {formatPercent(data.scannerRow?.fundingRatePct, 3)}
                  </strong>
                </div>
                <div className="scanner-context-row">
                  <span>Session:</span>
                  <strong>{sessionLabel(data.primaryScanner?.active || [])}</strong>
                </div>
                <div className="scanner-context-row">
                  <span>Setup:</span>
                  <strong>{scannerNarrative || "No scanner narrative yet"}</strong>
                </div>
              </div>
            </div>
          </div>

          <div className="session-chip-row">
            {(data.primaryScanner?.active || []).map((item) => (
              <span key={item} className="session-chip">
                {item.replaceAll("_", " ")}
              </span>
            ))}
          </div>

          {data.inPlayEntries.length ? (
            <div className="chip-row">
              {data.inPlayEntries.map((entry) => (
                <span key={`${entry.symbol}-${entry.sideBias}`} className="chip">
                  {entry.sideBias.toUpperCase()} • {entry.state} • {entry.currentGrade}
                </span>
              ))}
            </div>
          ) : null}
        </article>

        <article className="panel reveal">
          <div className="section-heading">
            <div>
              <span className="eyebrow">Confluence</span>
              <h2>Setup read</h2>
            </div>
          </div>

          <div className="tab-row">
            <button
              className={`tab-button ${setupTab === "long" ? "tab-active" : ""}`}
              onClick={() => setSetupTab("long")}
              type="button"
            >
              Long
            </button>
            <button
              className={`tab-button ${setupTab === "short" ? "tab-active" : ""}`}
              onClick={() => setSetupTab("short")}
              type="button"
            >
              Short
            </button>
          </div>

          <div className="analysis-card analysis-card-strong">
            <div className="analysis-head">
              <span className={`grade-badge ${gradeClass(extractLabel(activeConfluence))}`}>
                {extractLabel(activeConfluence)}
              </span>
              <strong>{formatNumber(extractScore(activeConfluence), 2)}</strong>
            </div>
            <div className="scanner-context-list">
              <div className="scanner-context-row">
                <span>Scanner Grade:</span>
                <strong>{activeRow?.grade || "N/A"}</strong>
              </div>
              <div className="scanner-context-row">
                <span>Scanner Score:</span>
                <strong>{formatNumber(activeRow?.score, 2)}</strong>
              </div>
            </div>
            <ul className="notes-list">
              {extractNotes(activeConfluence).map((note) => (
                <li key={`${setupTab}-${note}`}>{note}</li>
              ))}
            </ul>
          </div>
        </article>
      </section>

      <section className="panel reveal">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Flow Tabs</span>
          </div>
        </div>

        <div className="tab-row">
          {moduleTabs.map((tab) => (
            <button
              key={tab.id}
              className={`tab-button ${flowTab === tab.id ? "tab-active" : ""}`}
              onClick={() => setFlowTab(tab.id)}
              type="button"
            >
              {tab.label}
            </button>
          ))}
        </div>

        {activeModule ? <ModuleGrid modules={[activeModule]} /> : null}
      </section>

      <TradePanel detail={data} />

      <section className="panel reveal">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Backend Gaps</span>
            <h2>Still not wired</h2>
          </div>
        </div>
        <ul className="notes-list">
          {data.backendGaps.map((gap) => (
            <li key={gap}>{gap}</li>
          ))}
        </ul>
      </section>

      <section className="json-grid">
        <JsonCard title="Fusion response" data={data.analytics.fusion} />
        <JsonCard title="Structure response" data={data.analytics.structure} />
        <JsonCard title="Patterns response" data={data.analytics.patterns} />
        <JsonCard title="Volstats response" data={data.analytics.volstats} />
      </section>
    </>
  );
}
