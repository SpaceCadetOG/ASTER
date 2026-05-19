import Link from "next/link";
import { formatCompactUsd, formatNumber, formatPercent } from "@/lib/format";
import type { ScannerRow, ScannerSide } from "@/lib/types";

export function ScannerTable({
  rows,
  side,
  emptyMessage
}: {
  rows: ScannerRow[];
  side: ScannerSide;
  emptyMessage: string;
}) {
  if (!rows.length) {
    return <div className="empty-state">{emptyMessage}</div>;
  }

  return (
    <div className="table-wrap">
      <table className="scanner-table">
        <thead>
          <tr>
            <th>Token</th>
            <th>Grade</th>
            <th>Score</th>
            <th>Day UTC</th>
            <th>4h UTC</th>
            <th>1h UTC</th>
            <th>24h</th>
            <th>Vol</th>
            <th>OI</th>
            <th>Funding</th>
            <th>Last</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={`${side}-${row.symbol}`}>
              <td>
                <Link
                  href={`/token/${encodeURIComponent(row.symbol)}?side=${side}`}
                  className="token-link"
                >
                  <span>{row.symbol}</span>
                  <small>{row.reason || "Scanner-qualified candidate"}</small>
                </Link>
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
              <td className={row.dayUtc24h && row.dayUtc24h < 0 ? "down" : "up"}>
                {formatPercent(row.dayUtc24h, 1)}
              </td>
              <td className={row.utc4hPct && row.utc4hPct < 0 ? "down" : "up"}>
                {formatPercent(row.utc4hPct, 1)}
              </td>
              <td className={row.utc1hPct && row.utc1hPct < 0 ? "down" : "up"}>
                {formatPercent(row.utc1hPct, 1)}
              </td>
              <td className={row.change24h < 0 ? "down" : "up"}>
                {formatPercent(row.change24h, 1)}
              </td>
              <td>{formatCompactUsd(row.volumeUsd)}</td>
              <td>{formatCompactUsd(row.openInterestUsd)}</td>
              <td>{formatPercent(row.fundingRatePct, 3)}</td>
              <td>{formatNumber(row.lastPrice, row.lastPrice > 100 ? 2 : 4)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
