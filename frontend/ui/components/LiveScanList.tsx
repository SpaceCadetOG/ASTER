import Link from "next/link";
import {
  formatCompactUsd,
  formatNumber,
  formatPercent
} from "@/lib/format";
import type { LiveScanItem, ScannerSide } from "@/lib/types";

function sideClass(side: string) {
  return side.toUpperCase() === "LONG" ? "grade-a" : "grade-d";
}

export function LiveScanList({
  rows,
  emptyMessage
}: {
  rows: Array<LiveScanItem & { scannerSide: ScannerSide }>;
  emptyMessage: string;
}) {
  if (!rows.length) {
    return <div className="empty-state">{emptyMessage}</div>;
  }

  return (
    <div className="table-wrap">
      <table className="scanner-table live-scan-table">
        <thead>
          <tr>
            <th>Token</th>
            <th>Side</th>
            <th>Grade</th>
            <th>Score</th>
            <th>State</th>
            <th>Slope</th>
            <th>Day UTC</th>
            <th>4h UTC</th>
            <th>1h UTC</th>
            <th>Vol</th>
            <th>Last</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={`${row.scannerSide}-${row.symbol}-${row.side}`}>
              <td>
                <Link
                  href={`/token/${encodeURIComponent(row.symbol)}?side=${row.scannerSide}`}
                  className="token-link"
                >
                  <span>{row.symbol}</span>
                  <small>{row.state || "Live scan"}</small>
                </Link>
              </td>
              <td>
                <span className={`grade-badge ${sideClass(row.side)}`}>{row.side}</span>
              </td>
              <td>
                <span
                  className={`grade-badge grade-${row.grade
                    .toLowerCase()
                    .replace("+", "plus")
                    .replace(/[^a-z0-9]+/g, "")}`}
                >
                  {row.grade}
                </span>
              </td>
              <td>{formatNumber(row.score, 2)}</td>
              <td>{row.state || "-"}</td>
              <td className={row.slope < 0 ? "down" : "up"}>{formatNumber(row.slope, 3)}</td>
              <td className={row.dayUtc < 0 ? "down" : "up"}>{formatPercent(row.dayUtc, 1)}</td>
              <td className={row.utc4h < 0 ? "down" : "up"}>{formatPercent(row.utc4h, 1)}</td>
              <td className={row.utc1h < 0 ? "down" : "up"}>{formatPercent(row.utc1h, 1)}</td>
              <td>{formatCompactUsd(row.volumeUsd)}</td>
              <td>{formatNumber(row.price, row.price > 100 ? 2 : 4)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
