export function Sparkline({
  values,
  positive
}: {
  values: number[];
  positive: boolean;
}) {
  if (!values.length) {
    return <div className="chart-empty">No candle data available.</div>;
  }
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || 1;
  const points = values
    .map((value, index) => {
      const x = (index / Math.max(values.length - 1, 1)) * 100;
      const y = 100 - ((value - min) / range) * 100;
      return `${x},${y}`;
    })
    .join(" ");

  return (
    <svg
      className="sparkline"
      viewBox="0 0 100 100"
      preserveAspectRatio="none"
      aria-label="price sparkline"
    >
      <polyline
        fill="none"
        stroke={positive ? "var(--accent-strong)" : "var(--danger)"}
        strokeWidth="2.5"
        points={points}
      />
    </svg>
  );
}
