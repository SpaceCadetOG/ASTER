import type { ReactNode } from "react";

export function MetricTile({
  label,
  value,
  valueClassName = ""
}: {
  label: string;
  value: ReactNode;
  valueClassName?: string;
}) {
  return (
    <div className="tile">
      <div className="tile-label">{label}</div>
      <div className={`tile-value ${valueClassName}`.trim()}>{value}</div>
    </div>
  );
}
