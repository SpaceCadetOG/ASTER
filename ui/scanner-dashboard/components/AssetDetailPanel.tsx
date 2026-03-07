import type { AssetDetail } from "@/lib/types";

function line(label: string, value: string) {
  return (
    <div style={{ display: "flex", justifyContent: "space-between", gap: 12 }}>
      <span className="muted">{label}</span>
      <span>{value}</span>
    </div>
  );
}

function n(v: number | undefined, d = 4): string {
  if (v === undefined || Number.isNaN(v)) return "-";
  return v.toFixed(d);
}

export function AssetDetailPanel({ detail }: { detail?: AssetDetail }) {
  if (!detail?.asset) {
    return (
      <div className="panel">
        <h3>Asset Detail</h3>
        <div className="subtle">Select an asset row to inspect details.</div>
      </div>
    );
  }

  const a = detail.asset;
  return (
    <div className="panel">
      <h3>Asset Detail · {a.symbol}</h3>
      <div className="row" style={{ gap: 6 }}>
        {line("Price / Mark / Index", `${n(a.price, 6)} / ${n(a.mark, 6)} / ${n(a.index, 6)}`)}
        {line("Long / Short / In-Play", `${n(a.longScore, 2)} / ${n(a.shortScore, 2)} / ${n(a.inPlayScore, 2)}`)}
        {line("Funding %", n(a.funding, 4))}
        {line("Volume USD", n(a.volume, 2))}
        {line("Open Interest USD", n(a.openInterest, 2))}
        {line("Grades", `${a.longGrade || "N/A"} / ${a.shortGrade || "N/A"}`)}
      </div>
      <div className="narrative">
        <strong>Narrative:</strong> {a.narrative || a.reason || "No narrative available."}
      </div>
      <div className="row" style={{ marginTop: 10, gridTemplateColumns: "1fr 1fr" }}>
        <div className="panel" style={{ padding: 8 }}>
          <h3 style={{ marginBottom: 4 }}>Long Confluence (raw)</h3>
          <pre className="subtle" style={{ margin: 0, whiteSpace: "pre-wrap" }}>
            {JSON.stringify(detail.longConfluence || {}, null, 2)}
          </pre>
        </div>
        <div className="panel" style={{ padding: 8 }}>
          <h3 style={{ marginBottom: 4 }}>Short Confluence (raw)</h3>
          <pre className="subtle" style={{ margin: 0, whiteSpace: "pre-wrap" }}>
            {JSON.stringify(detail.shortConfluence || {}, null, 2)}
          </pre>
        </div>
      </div>
    </div>
  );
}
