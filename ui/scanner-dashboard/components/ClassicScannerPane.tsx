import { formatCompactUsd, formatNumber, formatPercent, formatTime } from "@/lib/format";
import type { ScannerSide, ScannerView } from "@/lib/types";

export function ClassicScannerPane({
  title,
  scanner,
  side
}: {
  title: string;
  scanner: ScannerView | null;
  side: ScannerSide;
}) {
  if (!scanner) {
    return (
      <div className="panel">
        <h3>{title}</h3>
        <div className="empty-state">Scanner unavailable.</div>
      </div>
    );
  }

  return (
    <div className="panel">
      <h3>{title}</h3>
      <div className="subtle" style={{ marginBottom: 12 }}>
        {scanner.exchange} · updated {formatTime(scanner.generated)}
      </div>
      <pre className="terminal-strip" style={{ marginTop: 0 }}>
        {[
          `sessions=${scanner.active.join("|") || "N/A"}`,
          "",
          ...scanner.rows.slice(0, 20).map((row) =>
            [
              row.symbol.padEnd(12, " "),
              formatNumber(row.score, 2).padStart(6, " "),
              formatPercent(row.dayUtc24h, 1).padStart(8, " "),
              formatCompactUsd(row.volumeUsd).padStart(8, " "),
              formatCompactUsd(row.openInterestUsd).padStart(8, " "),
              formatPercent(row.fundingRatePct, 4).padStart(9, " "),
              formatNumber(row.lastPrice, row.lastPrice > 100 ? 2 : 4).padStart(10, " "),
              (side === "long" ? row.grade : row.grade).padStart(4, " ")
            ].join(" | ")
          )
        ].join("\n")}
      </pre>
    </div>
  );
}
