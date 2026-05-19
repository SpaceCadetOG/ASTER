export function JsonCard({
  title,
  data
}: {
  title: string;
  data: unknown;
}) {
  return (
    <details className="json-card">
      <summary>{title}</summary>
      <pre>{JSON.stringify(data, null, 2)}</pre>
    </details>
  );
}
