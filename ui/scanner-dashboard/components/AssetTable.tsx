import type { AssetRow } from "@/lib/types";

function num(v: number | undefined, d = 2): string {
  if (v === undefined || Number.isNaN(v)) return "-";
  return v.toFixed(d);
}

function money(v: number | undefined): string {
  if (!v) return "-";
  if (Math.abs(v) >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(2)}B`;
  if (Math.abs(v) >= 1_000_000) return `${(v / 1_000_000).toFixed(2)}M`;
  if (Math.abs(v) >= 1_000) return `${(v / 1_000).toFixed(2)}K`;
  return v.toFixed(2);
}

export function AssetTable({
  rows,
  onSelect
}: {
  rows: AssetRow[];
  onSelect: (symbol: string) => void;
}) {
  return (
    <table className="table">
      <thead>
        <tr>
          <th>Symbol</th>
          <th>Price</th>
          <th>Long</th>
          <th>Short</th>
          <th>In-Play</th>
          <th>Funding %</th>
          <th>Volume</th>
          <th>OI</th>
          <th>State</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr
            key={r.symbol}
            className="asset-row"
            onClick={() => onSelect(r.symbol)}
          >
            <td>
              <strong>{r.symbol}</strong>
            </td>
            <td>{num(r.price, r.price > 1000 ? 2 : 6)}</td>
            <td>{num(r.longScore)}</td>
            <td>{num(r.shortScore)}</td>
            <td>{num(r.inPlayScore)}</td>
            <td className={(r.funding || 0) >= 0 ? "good" : "bad"}>
              {num(r.funding, 4)}
            </td>
            <td>{money(r.volume)}</td>
            <td>{money(r.openInterest)}</td>
            <td>
              <span className="pill">{r.state || "-"}</span>
            </td>
          </tr>
        ))}
        {rows.length === 0 ? (
          <tr>
            <td colSpan={9} className="muted">
              (none)
            </td>
          </tr>
        ) : null}
      </tbody>
    </table>
  );
}
