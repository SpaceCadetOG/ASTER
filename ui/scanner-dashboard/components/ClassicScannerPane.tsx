import type { AssetRow } from "@/lib/types";

function padRight(s: string, n: number): string {
  return s.length >= n ? s.slice(0, n) : s + " ".repeat(n - s.length);
}
function padLeft(s: string, n: number): string {
  return s.length >= n ? s.slice(0, n) : " ".repeat(n - s.length) + s;
}
function fmtNum(v: number, d = 2): string {
  if (!Number.isFinite(v)) return "-";
  return v.toFixed(d);
}
function fmtPrice(v: number): string {
  if (!Number.isFinite(v)) return "-";
  if (v >= 1000) return v.toFixed(2);
  if (v >= 1) return v.toFixed(4);
  return v.toFixed(6);
}

function scannerHeader(exchange: string, active: string[]): string {
  const sessions = active.length ? ` [${active.join("|")}]` : "";
  return `${exchange}${sessions}`;
}

function buildRow(r: AssetRow, side: "long" | "short"): string {
  const score = side === "long" ? r.longScore : r.shortScore;
  const grade = side === "long" ? r.longGrade || "N/A" : r.shortGrade || "N/A";
  const symbol = padRight(r.symbol, 11);
  const scoreS = padLeft(fmtNum(score, 2), 6);
  const d24 = padLeft("-", 8); // exact source not kept in merged row
  const vol = padLeft(fmtNum((r.volume || 0) / 1_000_000, 2) + "M", 8);
  const oi = padLeft(
    r.openInterest && r.openInterest > 0
      ? fmtNum(r.openInterest / 1_000_000, 2) + "M"
      : "-",
    8
  );
  const funding = padLeft(
    Number.isFinite(r.funding || NaN) ? fmtNum(r.funding || 0, 4) : "-",
    10
  );
  const prev = padLeft(fmtPrice(r.index || r.price), 8);
  const last = padLeft(fmtPrice(r.price), 8);
  return `${symbol} | ${scoreS} | ${d24} | ${vol} | ${oi} | ${funding} | ${prev} | ${last} | ${grade}`;
}

export function ClassicScannerPane({
  title,
  exchange,
  generated,
  active,
  rows,
  side
}: {
  title: string;
  exchange: string;
  generated: string;
  active: string[];
  rows: AssetRow[];
  side: "long" | "short";
}) {
  const lines: string[] = [];
  lines.push(scannerHeader(exchange, active));
  lines.push(`generated ${new Date(generated).toLocaleString()}`);
  lines.push("");
  lines.push("Symbol       | Score  | Δ%(24h) | Vol($)   | OI($)   | Funding(%) | Open24h  | Last     | Conf");
  lines.push("-------------+--------+---------+----------+---------+------------+----------+----------+-----");
  rows
    .slice()
    .sort((a, b) =>
      side === "long"
        ? (b.longScore || 0) - (a.longScore || 0)
        : (b.shortScore || 0) - (a.shortScore || 0)
    )
    .slice(0, 40)
    .forEach((r) => lines.push(buildRow(r, side)));

  return (
    <div className="panel">
      <h3>{title}</h3>
      <pre className="terminal-strip" style={{ marginTop: 0 }}>
        {lines.join("\n")}
      </pre>
    </div>
  );
}

