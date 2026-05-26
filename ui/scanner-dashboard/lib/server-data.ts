import { getBackendUrls } from "@/lib/env";
import type {
  AnalyticsBundle,
  AssetDetail,
  ConnectivityState,
  DashboardData,
  EndpointStatus,
  InPlayRow,
  LiveScanItem,
  LiveView,
  ModuleSummary,
  ScannerRow,
  ScannerSide,
  ScannerView
} from "@/lib/types";

type RawScannerSnapshot = {
  Exchange?: string;
  Generated?: string;
  Active?: string[];
  Rows?: Array<Record<string, unknown>>;
  Conf?: Record<string, string>;
  InPlay?: Array<Record<string, unknown>>;
};

type RawLiveStatus = {
  generated?: string;
  mode?: string;
  mode_state?: string;
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
  live?: {
    generated?: string;
    health?: string;
    health_detail?: string;
    balance?: number;
    margin_balance?: number;
    available_usdt?: number;
    equity?: number;
    realized_day?: number;
    open_pnl?: number;
    open_count?: number;
    bot_count?: number;
    manual_count?: number;
    positions?: Array<Record<string, unknown>>;
  };
  paper_summary?: string;
  paper?: {
    mode?: string;
    summary?: string;
    balance?: number;
    reserve?: number;
    equity?: number;
    open_pnl?: number;
    realized_today?: number;
    open_count?: number;
    recent_closed_count?: number;
    recent_decision_count?: number;
    open_positions?: Array<Record<string, unknown>>;
    recent_closed?: Array<Record<string, unknown>>;
    recent_decisions?: Array<Record<string, unknown>>;
  };
  scanner_longs?: Array<Record<string, unknown>>;
  scanner_shorts?: Array<Record<string, unknown>>;
  exec?: Record<string, number>;
};

function normalizeLiveAccount(raw: RawLiveStatus["live"] | undefined) {
  if (!raw) {
    return undefined;
  }
  return {
    generated: typeof raw.generated === "string" ? raw.generated : undefined,
    health: typeof raw.health === "string" ? raw.health : undefined,
    healthDetail: typeof raw.health_detail === "string" ? raw.health_detail : undefined,
    balance: Number(raw.balance || raw.margin_balance || raw.equity || raw.available_usdt || 0),
    marginBalance: Number(raw.margin_balance || raw.balance || raw.equity || raw.available_usdt || 0),
    availableUsdt: Number(raw.available_usdt || 0),
    equity: Number(raw.equity || 0),
    realizedDay: Number(raw.realized_day || 0),
    openPnl: Number(raw.open_pnl || 0),
    openCount: Number(raw.open_count || 0),
    botCount: Number(raw.bot_count || 0),
    manualCount: Number(raw.manual_count || 0),
    positions: (raw.positions || []).map((row) => ({
      symbol: normalizeDisplaySymbol(String(row.symbol || "")),
      side: String(row.side || ""),
      source: typeof row.source === "string" ? row.source : undefined,
      manageState: typeof row.manage_state === "string" ? row.manage_state : undefined,
      protectionState: typeof row.protection_state === "string" ? row.protection_state : undefined,
      managed: Boolean(row.managed),
      protected: Boolean(row.protected),
      qty: Number(row.qty || 0),
      entryPrice: Number(row.entry_price || 0),
      markPrice: Number(row.mark_price || 0),
      lastPrice: Number(row.last_price || 0),
      spreadBps: Number(row.spread_bps || 0),
      unrealizedPnl: Number(row.unrealized_pnl || 0),
      unrealizedPnlPct: Number(row.unrealized_pnl_pct || 0),
      realizedPnl: Number(row.realized_pnl || 0),
      exchangeUnreal: Number(row.exchange_unreal || 0),
      leverage: Number(row.leverage || 0),
      margin: Number(row.margin || 0),
      stopPrice: Number(row.stop_price || 0),
      holdMin: Number(row.hold_min || 0),
      entryReason: typeof row.entry_reason === "string" ? row.entry_reason : undefined
    }))
  };
}

function normalizePaper(raw: RawLiveStatus["paper"] | undefined) {
  if (!raw) {
    return undefined;
  }
  const normalizePaperLabel = (value: unknown) =>
    typeof value === "string" ? value : undefined;
  return {
    mode: normalizePaperLabel(raw.mode),
    summary: typeof raw.summary === "string" ? raw.summary : undefined,
    balance: Number(raw.balance || 0),
    reserve: Number(raw.reserve || 0),
    equity: Number(raw.equity || 0),
    openPnl: Number(raw.open_pnl || 0),
    realizedToday: Number(raw.realized_today || 0),
    openCount: Number(raw.open_count || 0),
    recentClosedCount: Number(raw.recent_closed_count || 0),
    recentDecisionCount: Number(raw.recent_decision_count || 0),
    openPositions: (raw.open_positions || []).map((row) => ({
      symbol: normalizeDisplaySymbol(String(row.symbol || "")),
      side: String(row.side || ""),
      source: normalizePaperLabel(row.source),
      mode: normalizePaperLabel(row.mode),
      strategy: typeof row.strategy === "string" ? row.strategy : undefined,
      setupFamily: typeof row.setup_family === "string" ? row.setup_family : undefined,
      grade: typeof row.grade === "string" ? row.grade : undefined,
      state: typeof row.state === "string" ? row.state : undefined,
      triggerState: typeof row.trigger_state === "string" ? row.trigger_state : undefined,
      exitProfile: typeof row.exit_profile === "string" ? row.exit_profile : undefined,
      entryPrice: Number(row.entry_price || 0),
      markPrice: Number(row.mark_price || 0),
      stopPrice: Number(row.stop_price || 0),
      tp1: Number(row.tp1 || 0),
      tp2: Number(row.tp2 || 0),
      tp3: Number(row.tp3 || 0),
      qty: Number(row.qty || 0),
      margin: Number(row.margin || 0),
      leverage: Number(row.leverage || 0),
      unrealizedPnl: Number(row.unrealized_pnl || 0),
      unrealizedPct: Number(row.unrealized_pct || 0),
      realizedPnl: Number(row.realized_pnl || 0),
      mfeR: Number(row.mfe_r || 0),
      maeR: Number(row.mae_r || 0),
      openedAt: typeof row.opened_at === "string" ? row.opened_at : undefined,
      holdMin: Number(row.hold_min || 0),
      entryReason: typeof row.entry_reason === "string" ? row.entry_reason : undefined,
      entryDecisionReject:
        typeof row.entry_decision_reject === "string" ? row.entry_decision_reject : undefined
    })),
    recentClosed: (raw.recent_closed || []).map((row) => ({
      symbol: normalizeDisplaySymbol(String(row.symbol || "")),
      side: String(row.side || ""),
      source: normalizePaperLabel(row.source),
      mode: normalizePaperLabel(row.mode),
      strategy: typeof row.strategy === "string" ? row.strategy : undefined,
      setupFamily: typeof row.setup_family === "string" ? row.setup_family : undefined,
      grade: typeof row.grade === "string" ? row.grade : undefined,
      state: typeof row.state === "string" ? row.state : undefined,
      triggerState: typeof row.trigger_state === "string" ? row.trigger_state : undefined,
      exitProfile: typeof row.exit_profile === "string" ? row.exit_profile : undefined,
      entryPrice: Number(row.entry_price || 0),
      exitPrice: Number(row.exit_price || 0),
      pnlUsd: Number(row.pnl_usd || 0),
      pnlPct: Number(row.pnl_pct || 0),
      fees: Number(row.fees || 0),
      riskR: Number(row.risk_r || 0),
      holdMin: Number(row.hold_min || 0),
      mfeR: Number(row.mfe_r || 0),
      maeR: Number(row.mae_r || 0),
      captureRatio: Number(row.capture_ratio || 0),
      maxGivebackR: Number(row.max_giveback_r || 0),
      exitReason: typeof row.exit_reason === "string" ? row.exit_reason : undefined,
      closedAt: typeof row.closed_at === "string" ? row.closed_at : undefined
    })),
    recentDecisions: (raw.recent_decisions || []).map((row) => ({
      symbol: normalizeDisplaySymbol(String(row.symbol || "")),
      side: String(row.side || ""),
      source: normalizePaperLabel(row.source),
      mode: normalizePaperLabel(row.mode),
      strategy: typeof row.strategy === "string" ? row.strategy : undefined,
      setupFamily: typeof row.setup_family === "string" ? row.setup_family : undefined,
      grade: typeof row.grade === "string" ? row.grade : undefined,
      state: typeof row.state === "string" ? row.state : undefined,
      triggerState: typeof row.trigger_state === "string" ? row.trigger_state : undefined,
      exitProfile: typeof row.exit_profile === "string" ? row.exit_profile : undefined,
      score: Number(row.score || 0),
      slope: Number(row.slope || 0),
      confluenceScore: Number(row.confluence_score || 0),
      entryPrice: Number(row.entry_price || 0),
      stopDistancePct: Number(row.stop_distance_pct || 0),
      approved: Boolean(row.approved),
      rejectReason: typeof row.reject_reason === "string" ? row.reject_reason : undefined,
      gateReasons: Array.isArray(row.gate_reasons)
        ? row.gate_reasons.filter((v): v is string => typeof v === "string")
        : [],
      decidedAt: typeof row.decided_at === "string" ? row.decided_at : undefined
    }))
  };
}

type FetchResult<T> = {
  data: T | null;
  ok: boolean;
  failedEndpoint?: string;
};

const STALE_AFTER_MS = 2 * 60 * 1000;

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

async function fetchJson<T>(url: string): Promise<FetchResult<T>> {
  try {
    const res = await fetch(url, {
      cache: "no-store",
      next: { revalidate: 0 }
    });
    if (!res.ok) {
      return {
        data: null,
        ok: false,
        failedEndpoint: `${url} -> HTTP ${res.status}`
      };
    }
    return {
      data: (await res.json()) as T,
      ok: true
    };
  } catch {
    return {
      data: null,
      ok: false,
      failedEndpoint: `${url} -> request failed`
    };
  }
}

function computeHealth(timestamp?: string, connected = false): ConnectivityState {
  if (!connected) {
    return "disconnected";
  }
  if (!timestamp) {
    return "live";
  }
  const parsed = new Date(timestamp);
  if (Number.isNaN(parsed.getTime())) {
    return "live";
  }
  return Date.now() - parsed.getTime() > STALE_AFTER_MS ? "stale" : "live";
}

function normalizeScannerRow(raw: Record<string, unknown>, grade: string | undefined): ScannerRow {
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

function normalizeInPlay(raw: Record<string, unknown>): InPlayRow {
  return {
    symbol: normalizeDisplaySymbol(String(raw.symbol || raw.Symbol || "")),
    sideBias: String(raw.sideBias || raw.SideBias || ""),
    currentGrade: String(raw.currentGrade || raw.CurrentGrade || "N/A"),
    currentScore: Number(raw.currentScore || raw.CurrentScore || 0),
    scoreSlope: Number(raw.scoreSlope || raw.ScoreSlope || 0),
    state: String(raw.state || raw.State || ""),
    momentum: Boolean(raw.momentum || raw.Momentum),
    lastSeen: raw.lastSeen ? String(raw.lastSeen) : raw.LastSeen ? String(raw.LastSeen) : undefined
  };
}

function normalizeScanner(
  side: ScannerSide,
  endpoint: string,
  raw: RawScannerSnapshot | null,
  connected: boolean
): ScannerView | null {
  if (!raw) {
    return {
      side,
      exchange: side === "long" ? "asterdex (LONGS)" : "asterdex (SHORTS)",
      generated: "",
      active: [],
      rows: [],
      inPlay: [],
      state: "unavailable",
      endpoint,
      connected,
      health: connected ? "unavailable" : "disconnected"
    };
  }
  const conf = raw.Conf || {};
  const rows = (raw.Rows || [])
    .map((row) => normalizeScannerRow(row, conf[String(row.Symbol || "").toUpperCase()]))
    .filter((row) => row.symbol)
    .sort((a, b) => b.score - a.score);
  const inPlay = (raw.InPlay || []).map(normalizeInPlay).filter((row) => row.symbol);
  return {
    side,
    exchange:
      raw.Exchange || (side === "long" ? "asterdex (LONGS)" : "asterdex (SHORTS)"),
    generated: raw.Generated || new Date().toISOString(),
    active: raw.Active || [],
    rows,
    inPlay,
    state: rows.length ? "ready" : "empty",
    endpoint,
    connected,
    health: computeHealth(raw.Generated, connected)
  };
}

function normalizeLiveScanItem(raw: Record<string, unknown>): LiveScanItem {
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

function normalizeLive(endpoint: string, raw: RawLiveStatus | null, connected: boolean): LiveView | null {
  if (!raw) {
    return {
      scannerLongs: [],
      scannerShorts: [],
      endpoint,
      connected,
      health: connected ? "unavailable" : "disconnected"
    };
  }
  return {
    generated: raw.generated,
    mode: raw.mode,
    modeState: raw.mode_state,
    dryRun: raw.dry_run,
    liveEnabled: raw.live_enabled,
    scannerBias: raw.scanner_bias,
    topSymbol: normalizeDisplaySymbol(String(raw.top_symbol || "")) || undefined,
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
    liveAccount: normalizeLiveAccount(raw.live),
    paperSummary: raw.paper_summary,
    paper: normalizePaper(raw.paper),
    scannerLongs: (raw.scanner_longs || []).map(normalizeLiveScanItem),
    scannerShorts: (raw.scanner_shorts || []).map(normalizeLiveScanItem),
    exec: raw.exec,
    endpoint,
    connected,
    health: computeHealth(raw.generated, connected)
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
    const result = await fetchJson<T>(url);
    if (result.data) {
      return { data: result.data, url };
    }
  }
  return { data: null };
}

function moduleNote(id: ModuleSummary["id"]): string {
  switch (id) {
    case "oflow":
      return "Status-only: asset-scoped endpoint not available.";
    case "tape":
      return "Status-only: asset-scoped endpoint not available.";
    case "whale":
      return "Status-only: asset-scoped endpoint not available.";
    case "liqs":
      return "Status-only: asset-scoped endpoint not available.";
  }
}

function assetModuleNote(id: ModuleSummary["id"]): string {
  switch (id) {
    case "oflow":
      return "Order-flow metrics scoped to the selected token.";
    case "tape":
      return "Recent tape prints and tape stats for the selected token.";
    case "whale":
      return "Whale flow window scoped to the selected token.";
    case "liqs":
      return "Recent liquidation flow scoped to the selected token.";
  }
}

function normalizeModuleSymbol(raw: string): string {
  return raw.toUpperCase().replace(/[^A-Z0-9]/g, "").replace(/USDT$/, "").replace(/USD$/, "");
}

function redactEndpointDisplay(url: string): string {
  try {
    const parsed = new URL(url);
    return parsed.pathname + parsed.search;
  } catch {
    return url.replace(/^https?:\/\/[^/]+/i, "") || "private backend";
  }
}

function normalizeModule(
  id: ModuleSummary["id"],
  baseUrl: string,
  status: Record<string, unknown> | null,
  symbol: string,
  assetScoped: boolean,
  failedEndpoint?: string
): ModuleSummary {
  const lastSymbol = String(status?.last_symbol || status?.LastSymbol || "").toUpperCase();
  const clean = normalizeModuleSymbol(symbol);
  const assetSymbol = normalizeModuleSymbol(String(status?.symbol || ""));
  const lastSymbolClean = normalizeModuleSymbol(lastSymbol);
  const lastUpdated = typeof status?.updated_at === "string" ? status.updated_at : undefined;
  return {
    id,
    label: id === "oflow" ? "OFlow" : id.charAt(0).toUpperCase() + id.slice(1),
    url: redactEndpointDisplay(
      assetScoped
      ? `${baseUrl}/api/asset?symbol=${encodeURIComponent(symbol)}`
      : `${baseUrl}/api/status`
    ),
    connected: Boolean(status),
    capability: assetScoped ? "asset-detail" : "module-status-only",
    status,
    symbolMatch:
      (Boolean(assetSymbol) && assetSymbol === clean) ||
      (Boolean(lastSymbolClean) && lastSymbolClean === clean),
    note: assetScoped ? assetModuleNote(id) : moduleNote(id),
    health: computeHealth(lastUpdated, Boolean(status)),
    lastUpdated,
    failedEndpoint
  };
}

async function fetchModule(
  id: ModuleSummary["id"],
  baseUrl: string,
  symbol: string
): Promise<ModuleSummary> {
  if (!symbol) {
    const result = await fetchJson<Record<string, unknown>>(`${baseUrl}/api/status`);
    return normalizeModule(id, baseUrl, result.data, symbol, false, result.failedEndpoint);
  }

  const assetUrl = `${baseUrl}/api/asset?symbol=${encodeURIComponent(symbol)}`;
  const assetResult = await fetchJson<Record<string, unknown>>(assetUrl);
  if (assetResult.data) {
    return normalizeModule(id, baseUrl, assetResult.data, symbol, true);
  }

  const statusResult = await fetchJson<Record<string, unknown>>(`${baseUrl}/api/status`);
  return normalizeModule(
    id,
    baseUrl,
    statusResult.data,
    symbol,
    false,
    assetResult.failedEndpoint || statusResult.failedEndpoint
  );
}

async function fetchModules(symbol: string) {
  const urls = getBackendUrls();
  return Promise.all([
    fetchModule("oflow", urls.oflow, symbol),
    fetchModule("tape", urls.tape, symbol),
    fetchModule("whale", urls.whale, symbol),
    fetchModule("liqs", urls.liqs, symbol)
  ]);
}

function buildEndpointStatus(
  id: EndpointStatus["id"],
  label: string,
  url: string,
  connected: boolean,
  scope: EndpointStatus["scope"],
  state: ConnectivityState,
  detail: string,
  failedEndpoint?: string,
  lastUpdated?: string
): EndpointStatus {
  return {
    id,
    label,
    url: redactEndpointDisplay(url),
    connected,
    scope,
    state,
    detail,
    failedEndpoint: failedEndpoint ? redactEndpointDisplay(failedEndpoint) : undefined,
    lastUpdated
  };
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

export async function buildDashboardData(): Promise<DashboardData> {
  const urls = getBackendUrls();
  const [longResult, shortResult, liveResult] = await Promise.all([
    fetchJson<RawScannerSnapshot>(`${urls.long}/api/status`),
    fetchJson<RawScannerSnapshot>(`${urls.short}/api/status`),
    fetchJson<RawLiveStatus>(`${urls.live}/api/status`)
  ]);

  const longScanner = normalizeScanner("long", `${urls.long}/api/status`, longResult.data, longResult.ok);
  const shortScanner = normalizeScanner("short", `${urls.short}/api/status`, shortResult.data, shortResult.ok);
  const live = normalizeLive(`${urls.live}/api/status`, liveResult.data, liveResult.ok);
  const probeSymbol =
    live?.topSymbol ||
    longScanner?.rows[0]?.symbol ||
    shortScanner?.rows[0]?.symbol ||
    "";
  const modules = await fetchModules(probeSymbol);
  const healthyCount = [longScanner, shortScanner, live]
    .filter(Boolean)
    .filter((item) => item?.health === "live" || item?.health === "stale").length;
  const mode: DashboardData["mode"] =
    healthyCount === 3 ? "live" : healthyCount > 0 ? "degraded" : "unavailable";

  return {
    generatedAt: new Date().toISOString(),
    mode,
    longScanner,
    shortScanner,
    live,
    modules,
    endpoints: [
      buildEndpointStatus(
        "long",
        "Long Scanner",
        `${urls.long}/api/status`,
        longResult.ok,
        "status-only",
        longScanner?.health || "unavailable",
        longResult.ok
          ? longScanner?.state === "empty"
            ? "Connected but no rows"
            : "Scanner connected"
          : "Scanner unavailable",
        longResult.failedEndpoint,
        longScanner?.generated || undefined
      ),
      buildEndpointStatus(
        "short",
        "Short Scanner",
        `${urls.short}/api/status`,
        shortResult.ok,
        "status-only",
        shortScanner?.health || "unavailable",
        shortResult.ok
          ? shortScanner?.state === "empty"
            ? "Connected but no rows"
            : "Scanner connected"
          : "Scanner unavailable",
        shortResult.failedEndpoint,
        shortScanner?.generated || undefined
      ),
      buildEndpointStatus(
        "live",
        "Live",
        `${urls.live}/api/status`,
        liveResult.ok,
        "status-only",
        live?.health || "unavailable",
        liveResult.ok ? "Runtime connected" : "Runtime unavailable",
        liveResult.failedEndpoint,
        live?.generated
      ),
      ...modules.map((module) =>
        buildEndpointStatus(
          module.id,
          module.label,
          module.url,
          module.connected,
          module.capability === "asset-detail" ? "asset-scoped" : "status-only",
          module.health,
          module.capability === "asset-detail" ? "Asset-scoped available" : "Status-only",
          module.failedEndpoint,
          module.lastUpdated
        )
      )
    ]
  };
}

export async function buildAssetDetail(symbol: string): Promise<AssetDetail> {
  const dashboard = await buildDashboardData();
  const urls = getBackendUrls();
  const upper = normalizeDisplaySymbol(symbol) || symbol.toUpperCase();
  const modules = await fetchModules(upper);
  const longScanner = dashboard.longScanner;
  const shortScanner = dashboard.shortScanner;
  const live = dashboard.live;

  const longScannerRow = longScanner?.rows.find((row) => row.symbol === upper) || null;
  const shortScannerRow = shortScanner?.rows.find((row) => row.symbol === upper) || null;
  const requestedSide: ScannerSide =
    (longScannerRow?.score || 0) >= (shortScannerRow?.score || 0) ? "long" : "short";
  const primaryScanner =
    requestedSide === "long" ? longScanner || shortScanner : shortScanner || longScanner;
  const scannerRow =
    requestedSide === "long"
      ? longScannerRow || shortScannerRow
      : shortScannerRow || longScannerRow;
  const inPlayEntries = [
    ...(longScanner?.inPlay || []),
    ...(shortScanner?.inPlay || [])
  ].filter((entry) => entry.symbol === upper);
  const analytics = await buildAnalytics(upper, requestedSide, urls.long, urls.short);

  const backendGaps = [
    !live
      ? "Live runtime status is unavailable, so execution context may be stale."
      : null,
    !analytics.fusion ? "Fusion response unavailable for this asset." : null,
    !analytics.structure ? "Structure response unavailable for this asset." : null,
    !analytics.patterns ? "Patterns response unavailable for this asset." : null,
    !analytics.volstats ? "Volstats response unavailable for this asset." : null,
    ...modules
      .filter((module) => module.capability === "module-status-only")
      .map((module) => `${module.label}: Status-only: asset-scoped endpoint not available.`),
    "Execution preview is read-only. No live order-entry endpoint is exposed.",
    "Detailed paper trade ledger, MFE/MAE, W/L, and exit reasons require future paper endpoints."
  ].filter(Boolean) as string[];

  return {
    generatedAt: new Date().toISOString(),
    symbol: upper,
    requestedSide,
    primaryScanner,
    scannerRow,
    longScannerRow,
    shortScannerRow,
    inPlayEntries,
    live,
    modules,
    analytics,
    backendGaps,
    executionPreview: {
      available: false,
      message: "Read-only preview only. Live controls are intentionally not exposed.",
      contract: {
        method: "POST",
        path: "/api/trades",
        fields: ["symbol", "side", "marginUsd", "orderType", "limitPrice"]
      }
    }
  };
}
