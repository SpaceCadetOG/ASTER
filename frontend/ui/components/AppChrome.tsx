import Link from "next/link";

export function AppChrome() {
  return (
    <header className="app-header">
      <div>
        <Link href="/" className="brand-mark">
          ASTER Momentum Scanner
        </Link>
        <p className="brand-copy">
          Independent frontend for scanner flows, token drilldown, and trade prep.
        </p>
      </div>
      <nav className="top-nav">
        <Link href="/scanners/long">Long Scanner</Link>
        <Link href="/scanners/short">Short Scanner</Link>
      </nav>
    </header>
  );
}
