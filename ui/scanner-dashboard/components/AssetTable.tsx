import { formatCompactUsd, formatNumber, formatPercent } from "@/lib/format";
import type { LiveScanItem, ScannerRow, ScannerSide } from "@/lib/types";

type ScannerTableProps = {
  kind: "scanner";
  side: ScannerSide;
  rows: ScannerRow[];
  emptyMessage: string;
  onSelect: (symbol: string, side: ScannerSide) => void;
};

type LiveTableProps = {
  kind: "live";
  rows: Array<LiveScanItem & { scannerSide: ScannerSide }>;
  emptyMessage: string;
  onSelect: (symbol: string, side: ScannerSide) => void;
};

function gradeTone(value?: string) {
  const key = (value || "N/A").toUpperCase();
  if (key === "A+" || key === "A") return "tone-positive";
  if (key === "B") return "tone-amber";
  if (key === "C") return "tone-orange";
  if (key === "D") return "tone-negative";
  return "tone-muted";
}

function numericTone(value: number | null | undefined) {
  if (value === null || value === undefined || Number.isNaN(value)) return "tone-muted";
  if (value > 0) return "tone-positive";
  if (value < 0) return "tone-negative";
  return "tone-muted";
}

export function AssetTable(props: ScannerTableProps | LiveTableProps) {
  if (!props.rows.length) {
    return <div className="empty-state">{props.emptyMessage}</div>;
  }

  if (props.kind === "scanner") {
    return (
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th>Symbol</th>
              <th>Grade</th>
              <th>Score</th>
              <th>Last</th>
              <th>24h %</th>
              <th>4h %</th>
              <th>1h %</th>
              <th>Funding</th>
              <th>24h Volume</th>
              <th>Open Interest</th>
            </tr>
          </thead>
          <tbody>
            {props.rows.map((row) => (
              <tr key={`${props.side}-${row.symbol}`} className="asset-row">
                <td>
                  <button
                    className="table-link"
                    onClick={() => props.onSelect(row.symbol, props.side)}
                  >
                    <strong>{row.symbol}</strong>
                    <small>{row.reason || `${props.side} scanner row`}</small>
                  </button>
                </td>
                <td>
                  <span className={`badge ${gradeTone(row.grade)}`}>{row.grade}</span>
                </td>
                <td>{formatNumber(row.score, 2)}</td>
                <td>{formatNumber(row.lastPrice, row.lastPrice > 100 ? 2 : 4)}</td>
                <td className={numericTone(row.dayUtc24h)}>{formatPercent(row.dayUtc24h, 1)}</td>
                <td className={numericTone(row.utc4hPct)}>{formatPercent(row.utc4hPct, 1)}</td>
                <td className={numericTone(row.utc1hPct)}>{formatPercent(row.utc1hPct, 1)}</td>
                <td className={numericTone(row.fundingRatePct)}>
                  {formatPercent(row.fundingRatePct, 4)}
                </td>
                <td>{formatCompactUsd(row.volumeUsd)}</td>
                <td>{formatCompactUsd(row.openInterestUsd)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }

  const tickerRows = [...props.rows, ...props.rows];

  return (
    <div className="hotlist-ticker" role="region" aria-label="Live hotlist ticker">
      <div className="hotlist-track">
        {tickerRows.map((row, index) => (
          <button
            key={`${row.scannerSide}-${row.symbol}-${row.side}-${index}`}
            className="hotlist-card"
            onClick={() => props.onSelect(row.symbol, row.scannerSide)}
          >
            <div className="hotlist-topline">
              <strong>{row.symbol}</strong>
              <span
                className={`badge ${
                  row.side.toUpperCase() === "LONG" ? "tone-positive" : "tone-negative"
                }`}
              >
                {row.side}
              </span>
            </div>
            <div className="hotlist-metrics">
              <span className={`badge ${gradeTone(row.grade)}`}>{row.grade}</span>
              <span>Score {formatNumber(row.score, 2)}</span>
              <span className={numericTone(row.slope)}>Slope {formatNumber(row.slope, 3)}</span>
              <span className={numericTone(row.dayUtc)}>{formatPercent(row.dayUtc, 1)}</span>
              <span>{formatCompactUsd(row.volumeUsd)}</span>
              <span>{formatNumber(row.price, row.price > 100 ? 2 : 4)}</span>
            </div>
            <small>{row.state || "Live hotlist"}</small>
          </button>
        ))}
      </div>
    </div>
  );
}
