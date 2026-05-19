import { getBackendUrls } from "@/lib/env";
import type {
  AnalyticsBundle,
  DashboardData,
  InPlayRow,
  LiveScanItem,
  LiveView,
  ModuleSummary,
  ScannerRow,
  ScannerSide,
  ScannerView,
  TokenDetailData
} from "@/lib/types";

type RawScannerSnapshot = {
  Exchange?: string;
  Generated?: string;
  Active?: string[];
  Rows?: Array<Record<string, any>>;
  Conf?: Record<string, string>;
  InPlay?: Array<Record<string, any>>;
};

type RawLiveStatus = {
  generated?: string;
  dry_run?: boolean;
  live_enabled?: boolean;
  scanner_bias?: string;
  top_symbol?: string;
  top_side?: string;
  top_grade?: string;
  top_score?: number;
  top_slope?: number;
  top_decision?: string;
  top_decision_why?: string;
  top_reject_reason?: string;
  top_trigger_state?: string;
  long_inplay?: number;
  short_inplay?: number;
  available_usdt?: number;
  paper_summary?: string;
  scanner_longs?: Array<Record<string, any>>;
  scanner_shorts?: Array<Record<string, any>>;
  exec?: Record<string, number>;
};

function normalizeGrade(value?: string): ScannerRow["grade"] {
  if (!value) {
    return "N/A";
  }
  const grade = value.trim().toUpperCase();
  if (grade === "A+" || grade === "A" || grade === "B" || grade === "C" || grade === "D") {
    return grade;
  }
  return "N/A";
}

function normalizeDisplaySymbol(value: string): string {
  const upper = value.toUpperCase().trim();
  if (!upper) {
    return "";
  }
  if (upper.includes("-")) {
    return upper;
  }
  if (upper.endsWith("USDT")) {
    return `${upper.slice(0, -4)}-USD`;
  }
  if (upper.endsWith("USD")) {
    return `${upper.slice(0, -3)}-USD`;
  }
  if (upper.endsWith("BTC") && upper !== "BTC") {
    return `${upper.slice(0, -3)}-BTC`;
  }
  if (upper.endsWith("ETH") && upper !== "ETH") {
    return `${upper.slice(0, -3)}-ETH`;
  }
  return upper;
}

async function fetchJson<T>(url: string): Promise<T | null> {
  try {
    const res = await fetch(url, {
      cache: "no-store"
    });
    if (!res.ok) {
      return null;
    }
    return (await res.json()) as T;
  } catch {
    return null;
  }
}

function normalizeScannerRow(raw: Record<string, any>, grade: string | undefined): ScannerRow {
  return {
    symbol: normalizeDisplaySymbol(String(raw.Symbol || "")),
    score: Number(raw.Score || 0),
    reason: String(raw.Reason || ""),
    volumeUsd: Number(raw.VolumeUSD || 0),
    openInterestUsd:
      raw.OIUSD === undefined || raw.OIUSD === null ? null : Number(raw.OIUSD),
    fundingRatePct:
      raw.FundingRate === undefined || raw.FundingRate === null
        ? null
        : Number(raw.FundingRate) * 100,
    openPrice: Number(raw.OpenPrice || 0),
    lastPrice: Number(raw.LastPrice || 0),
    change24h: Number(raw.Change24h || 0),
    dayUtc24h:
      raw.DayUTC24h === undefined || raw.DayUTC24h === null
        ? null
        : Number(raw.DayUTC24h),
    utc4hPct:
      raw.UTC4hPct === undefined || raw.UTC4hPct === null ? null : Number(raw.UTC4hPct),
    utc1hPct:
      raw.UTC1hPct === undefined || raw.UTC1hPct === null ? null : Number(raw.UTC1hPct),
    grade: normalizeGrade(grade)
  };
}

function normalizeInPlay(raw: Record<string, any>): InPlayRow {
  return {
    symbol: normalizeDisplaySymbol(String(raw.symbol || raw.Symbol || "")),
    sideBias: String(raw.sideBias || raw.SideBias || ""),
    currentGrade: String(raw.currentGrade || raw.CurrentGrade || "N/A"),
    currentScore: Number(raw.currentScore || raw.CurrentScore || 0),
    scoreSlope: Number(raw.scoreSlope || raw.ScoreSlope || 0),
    state: String(raw.state || raw.State || ""),
    momentum: Boolean(raw.momentum || raw.Momentum),
    lastSeen: raw.lastSeen || raw.LastSeen
  };
}

function normalizeScanner(side: ScannerSide, raw: RawScannerSnapshot | null): ScannerView | null {
  if (!raw) {
    return null;
  }
  const conf = raw.Conf || {};
  const rows = (raw.Rows || [])
    .map((row) => normalizeScannerRow(row, conf[String(row.Symbol || "").toUpperCase()]))
    .filter((row) => row.symbol)
    .sort((a, b) => b.score - a.score);
  const inPlay = (raw.InPlay || [])
    .map(normalizeInPlay)
    .filter((row) => row.symbol);
  return {
    side,
    exchange:
      raw.Exchange || (side === "long" ? "asterdex (LONGS)" : "asterdex (SHORTS)"),
    generated: raw.Generated || new Date().toISOString(),
    active: raw.Active || [],
    rows,
    inPlay
  };
}

function normalizeLiveScanItem(raw: Record<string, any>): LiveScanItem {
  return {
    symbol: normalizeDisplaySymbol(String(raw.Symbol || "")),
    side: String(raw.Side || ""),
    grade: String(raw.Grade || "N/A"),
    score: Number(raw.Score || 0),
    slope: Number(raw.Slope || 0),
    state: String(raw.State || ""),
    price: Number(raw.Price || 0),
    dayUtc: Number(raw.DayUTC || 0),
    utc4h: Number(raw.UTC4h || 0),
    utc1h: Number(raw.UTC1h || 0),
    volumeUsd: Number(raw.VolumeUSD || 0)
  };
}

function normalizeLive(raw: RawLiveStatus | null): LiveView | null {
  if (!raw) {
    return null;
  }
  return {
    generated: raw.generated,
    dryRun: raw.dry_run,
    liveEnabled: raw.live_enabled,
    scannerBias: raw.scanner_bias,
    topSymbol: raw.top_symbol,
    topSide: raw.top_side,
    topGrade: raw.top_grade,
    topScore: raw.top_score,
    topSlope: raw.top_slope,
    topDecision: raw.top_decision,
    topDecisionWhy: raw.top_decision_why,
    topRejectReason: raw.top_reject_reason,
    topTriggerState: raw.top_trigger_state,
    longInPlay: raw.long_inplay,
    shortInPlay: raw.short_inplay,
    availableUsdt: raw.available_usdt,
    paperSummary: raw.paper_summary,
    scannerLongs: (raw.scanner_longs || []).map(normalizeLiveScanItem),
    scannerShorts: (raw.scanner_shorts || []).map(normalizeLiveScanItem),
    exec: raw.exec
  };
}

function buildSymbolCandidates(symbol: string): string[] {
  const upper = symbol.toUpperCase().trim();
  const compact = upper.replace(/[^A-Z0-9]/g, "");
  const out = new Set<string>([upper, compact]);
  if (compact.endsWith("USD")) {
    out.add(`${compact}T`);
  }
  if (!compact.endsWith("USDT") && compact.endsWith("USD")) {
    out.add(compact.replace(/USD$/, "USDT"));
  }
  return [...out].filter(Boolean);
}

async function fetchFirst<T>(urls: string[]): Promise<{ data: T | null; url?: string }> {
  for (const url of urls) {
    const data = await fetchJson<T>(url);
    if (data) {
      return { data, url };
    }
  }
  return { data: null };
}

function moduleNote(id: ModuleSummary["id"]): string {
  switch (id) {
    case "oflow":
      return "Global order-flow pulse across the live scanner universe.";
    case "tape":
      return "Global tape pulse across the live scanner universe.";
    case "whale":
      return "Global whale flow across the live scanner universe.";
    case "liqs":
      return "Global liquidation pulse across the live scanner universe.";
  }
}

function assetModuleNote(id: ModuleSummary["id"]): string {
  switch (id) {
    case "oflow":
      return "Order-flow metrics scoped to the selected token. If this asset is not subscribed by the module, the card will show that explicitly.";
    case "tape":
      return "Recent tape prints and tape stats for the selected token. If this asset is not subscribed by the module, the card will show that explicitly.";
    case "whale":
      return "Whale flow window scoped to the selected token. If this asset is not subscribed by the module, the card will show that explicitly.";
    case "liqs":
      return "Recent liquidation flow scoped to the selected token.";
  }
}

function normalizeModuleSymbol(raw: string): string {
  return raw.toUpperCase().replace(/[^A-Z0-9]/g, "").replace(/USDT$/, "").replace(/USD$/, "");
}

function normalizeModule(
  id: ModuleSummary["id"],
  url: string,
  status: Record<string, unknown> | null,
  symbol: string
): ModuleSummary {
  const lastSymbol = String(status?.last_symbol || status?.LastSymbol || "").toUpperCase();
  const clean = normalizeModuleSymbol(symbol);
  const assetSymbol = normalizeModuleSymbol(String(status?.symbol || ""));
  const lastSymbolClean = normalizeModuleSymbol(lastSymbol);
  const hasAssetScope = Boolean(symbol) && Boolean(status) && Boolean(assetSymbol);
  return {
    id,
    label: id.toUpperCase(),
    url: hasAssetScope
      ? `${url}/api/asset?symbol=${encodeURIComponent(symbol)}`
      : `${url}/api/status`,
    connected: Boolean(status),
    capability: hasAssetScope ? "asset-detail" : "module-status-only",
    status,
    symbolMatch:
      (Boolean(assetSymbol) && assetSymbol === clean) ||
      (Boolean(lastSymbolClean) && lastSymbolClean === clean),
    note: hasAssetScope ? assetModuleNote(id) : moduleNote(id)
  };
}

async function fetchModules(symbol: string) {
  const urls = getBackendUrls();
  const suffix = symbol ? `/api/asset?symbol=${encodeURIComponent(symbol)}` : "/api/status";
  const [oflow, tape, whale, liqs] = await Promise.all([
    fetchJson<Record<string, unknown>>(`${urls.oflow}${suffix}`),
    fetchJson<Record<string, unknown>>(`${urls.tape}${suffix}`),
    fetchJson<Record<string, unknown>>(`${urls.whale}${suffix}`),
    fetchJson<Record<string, unknown>>(`${urls.liqs}${suffix}`)
  ]);
  return [
    normalizeModule("oflow", urls.oflow, oflow, symbol),
    normalizeModule("tape", urls.tape, tape, symbol),
    normalizeModule("whale", urls.whale, whale, symbol),
    normalizeModule("liqs", urls.liqs, liqs, symbol)
  ];
}

export async function buildDashboardData(): Promise<DashboardData> {
  const urls = getBackendUrls();
  const [longRaw, shortRaw, liveRaw] = await Promise.all([
    fetchJson<RawScannerSnapshot>(`${urls.long}/api/status`),
    fetchJson<RawScannerSnapshot>(`${urls.short}/api/status`),
    fetchJson<RawLiveStatus>(`${urls.live}/api/status`)
  ]);

  return {
    generatedAt: new Date().toISOString(),
    longScanner: normalizeScanner("long", longRaw),
    shortScanner: normalizeScanner("short", shortRaw),
    live: normalizeLive(liveRaw),
    modules: await fetchModules("")
  };
}

export async function buildScannerData(side: ScannerSide): Promise<ScannerView | null> {
  const urls = getBackendUrls();
  const raw = await fetchJson<RawScannerSnapshot>(
    `${side === "long" ? urls.long : urls.short}/api/status`
  );
  return normalizeScanner(side, raw);
}

async function buildAnalytics(
  symbol: string,
  side: ScannerSide,
  longBase: string,
  shortBase: string
): Promise<AnalyticsBundle> {
  const candidates = buildSymbolCandidates(symbol);

  const confluenceQuery = (candidate: string, targetSide: ScannerSide, baseUrl: string) =>
    `${baseUrl}/api/confluence?symbol=${encodeURIComponent(candidate)}&tf=5m&n=200&win=20&zmin=2.0&vmin=500000&levels=50&side=${targetSide}`;

  const sharedQuery = (candidate: string, path: string, baseUrl: string) =>
    `${baseUrl}${path}?symbol=${encodeURIComponent(candidate)}&tf=5m&n=200&left=3&right=3&win=20&zmin=2.0&vmin=500000&levels=50`;

  const primaryBase = side === "long" ? longBase : shortBase;

  const [longConfluence, shortConfluence, fusion, structure, patterns, volstats, candles] =
    await Promise.all([
      fetchFirst<Record<string, unknown>>(
        candidates.map((candidate) => confluenceQuery(candidate, "long", longBase))
      ),
      fetchFirst<Record<string, unknown>>(
        candidates.map((candidate) => confluenceQuery(candidate, "short", shortBase))
      ),
      fetchFirst<Record<string, unknown>>(
        candidates.map((candidate) => sharedQuery(candidate, "/api/fusion", primaryBase))
      ),
      fetchFirst<Record<string, unknown>>(
        candidates.map((candidate) => sharedQuery(candidate, "/api/structure", primaryBase))
      ),
      fetchFirst<Record<string, unknown>>(
        candidates.map((candidate) => sharedQuery(candidate, "/api/patterns", primaryBase))
      ),
      fetchFirst<Record<string, unknown>>(
        candidates.map((candidate) => sharedQuery(candidate, "/api/volstats", primaryBase))
      ),
      fetchFirst<AnalyticsBundle["candles"]>(
        candidates.map(
          (candidate) =>
            `${primaryBase}/api/candles?symbol=${encodeURIComponent(candidate)}&tf=5m&n=80`
        )
      )
    ]);

  return {
    requestSymbol: symbol,
    resolvedSymbol:
      String(
        longConfluence.data?.symbol ||
          shortConfluence.data?.symbol ||
          fusion.data?.symbol ||
          candles.data?.symbol ||
          ""
      ) || undefined,
    longConfluence: longConfluence.data,
    shortConfluence: shortConfluence.data,
    fusion: fusion.data,
    structure: structure.data,
    patterns: patterns.data,
    volstats: volstats.data,
    candles: candles.data
  };
}

export async function buildTokenDetailData(
  symbol: string,
  requestedSide: ScannerSide
): Promise<TokenDetailData> {
  const urls = getBackendUrls();
  const [dashboard, modules] = await Promise.all([buildDashboardData(), fetchModules(symbol)]);
  const longScanner = dashboard.longScanner;
  const shortScanner = dashboard.shortScanner;
  const upper = symbol.toUpperCase();

  const longScannerRow = longScanner?.rows.find((row) => row.symbol === upper) || null;
  const shortScannerRow = shortScanner?.rows.find((row) => row.symbol === upper) || null;

  const primaryScanner =
    requestedSide === "long"
      ? longScanner || shortScanner
      : shortScanner || longScanner;

  const scannerRow =
    requestedSide === "long"
      ? longScannerRow || shortScannerRow
      : shortScannerRow || longScannerRow;

  const inPlayEntries = [
    ...(longScanner?.inPlay || []),
    ...(shortScanner?.inPlay || [])
  ].filter((entry) => entry.symbol === upper);

  return {
    generatedAt: new Date().toISOString(),
    symbol: upper,
    requestedSide,
    primaryScanner,
    scannerRow,
    longScannerRow,
    shortScannerRow,
    inPlayEntries,
    live: dashboard.live,
    modules,
    analytics: await buildAnalytics(upper, requestedSide, urls.long, urls.short),
    backendGaps: [
      "cmd/live exposes status only. No HTTP trade execution endpoint is available to wire the trade panel."
    ],
    tradePanel: {
      executionAvailable: false,
      reason:
        "Execution is intentionally not wired because ASTER does not currently expose a real HTTP order-entry API.",
      expectedContract: {
        method: "POST",
        path: "/api/trades",
        fields: ["symbol", "side", "marginUsd", "orderType", "limitPrice"]
      }
    }
  };
}
