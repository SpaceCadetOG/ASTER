import { mockDashboardData } from "@/lib/mock";
import type {
  AssetDetail,
  AssetRow,
  DashboardData,
  ModuleCardData,
  ModuleStatus
} from "@/lib/types";

type ScoredRow = {
  Symbol: string;
  Score: number;
  Reason?: string;
  Change24h?: number;
  VolumeUSD?: number;
  OIUSD?: number | null;
  FundingRate?: number | null;
  OpenPrice?: number;
  LastPrice?: number;
};

type InPlayEntry = {
  symbol?: string;
  Symbol?: string;
  sideBias?: string;
  SideBias?: string;
  currentGrade?: string;
  CurrentGrade?: string;
  currentScore?: number;
  CurrentScore?: number;
  state?: string;
  State?: string;
};

type ScannerSnapshot = {
  Exchange?: string;
  Generated?: string;
  Active?: string[];
  Rows?: ScoredRow[];
  Conf?: Record<string, string>;
  InPlay?: InPlayEntry[];
};

type LiveLiteStatus = {
  generated?: string;
  dry_run?: boolean;
  live_enabled?: boolean;
  top_symbol?: string;
  top_side?: string;
  top_grade?: string;
  top_score?: number;
  top_slope?: number;
  long_inplay?: number;
  short_inplay?: number;
  available_usdt?: number;
  paper_summary?: string;
  payout_cycle_id?: string;
  payout_next_at?: string;
  exec?: {
    open?: number;
    pending?: number;
    partial_tp1?: number;
    partial_tp2?: number;
    closed?: number;
  };
};

function env(name: string, fallback: string): string {
  return process.env[name]?.trim() || fallback;
}

function isMockForced(): boolean {
  return process.env.SCANNER_USE_MOCK === "1" || process.env.SCANNER_USE_MOCK === "true";
}

async function fetchJson<T>(url: string): Promise<T | null> {
  try {
    const res = await fetch(url, {
      cache: "no-store",
      next: { revalidate: 0 }
    });
    if (!res.ok) return null;
    return (await res.json()) as T;
  } catch {
    return null;
  }
}

function moduleCard(key: string, label: string, source: string, ok: boolean, noteOk: string, noteDown: string): ModuleCardData {
  const status: ModuleStatus = ok ? "ok" : "down";
  return {
    key,
    label,
    source,
    status,
    note: ok ? noteOk : noteDown
  };
}

function fromInPlay(e: InPlayEntry): Pick<AssetRow, "symbol" | "inPlayScore" | "state"> {
  const symbol = (e.Symbol || e.symbol || "").toUpperCase();
  const score = e.CurrentScore ?? e.currentScore ?? 0;
  const state = (e.State || e.state || "").toString();
  return {
    symbol,
    inPlayScore: Number(score || 0),
    state
  };
}

function normalizeRows(
  rows: ScoredRow[] = [],
  conf: Record<string, string> = {},
  side: "long" | "short"
): Map<string, Partial<AssetRow>> {
  const map = new Map<string, Partial<AssetRow>>();
  for (const r of rows) {
    const symbol = (r.Symbol || "").toUpperCase();
    if (!symbol) continue;
    map.set(symbol, {
      symbol,
      price: Number(r.LastPrice || 0),
      mark: Number(r.LastPrice || 0),
      index: Number(r.OpenPrice || r.LastPrice || 0),
      volume: Number(r.VolumeUSD || 0),
      openInterest: Number(r.OIUSD || 0),
      funding: Number((r.FundingRate || 0) * 100),
      reason: r.Reason || "",
      narrative: r.Reason || "Scanner-qualified candidate",
      longScore: side === "long" ? Number(r.Score || 0) : 0,
      shortScore: side === "short" ? Number(r.Score || 0) : 0,
      longGrade: side === "long" ? (conf[symbol] || "N/A") : "N/A",
      shortGrade: side === "short" ? (conf[symbol] || "N/A") : "N/A",
      inPlayScore: 0
    });
  }
  return map;
}

function mergeAssetMaps(
  longMap: Map<string, Partial<AssetRow>>,
  shortMap: Map<string, Partial<AssetRow>>,
  longInPlay: InPlayEntry[],
  shortInPlay: InPlayEntry[]
): AssetRow[] {
  const all = new Map<string, AssetRow>();
  const upsert = (symbol: string, patch: Partial<AssetRow>) => {
    const cur = all.get(symbol) || {
      symbol,
      price: 0,
      mark: 0,
      index: 0,
      longScore: 0,
      shortScore: 0,
      inPlayScore: 0
    };
    all.set(symbol, { ...cur, ...patch, symbol });
  };

  for (const [symbol, row] of longMap) upsert(symbol, row);
  for (const [symbol, row] of shortMap) upsert(symbol, row);

  for (const entry of longInPlay) {
    const p = fromInPlay(entry);
    if (!p.symbol) continue;
    upsert(p.symbol, { inPlayScore: Math.max(p.inPlayScore || 0, (all.get(p.symbol)?.inPlayScore || 0)), state: p.state || "in-play" });
  }
  for (const entry of shortInPlay) {
    const p = fromInPlay(entry);
    if (!p.symbol) continue;
    upsert(p.symbol, { inPlayScore: Math.max(p.inPlayScore || 0, (all.get(p.symbol)?.inPlayScore || 0)), state: p.state || "in-play" });
  }

  return [...all.values()].sort((a, b) => {
    const aMax = Math.max(a.longScore || 0, a.shortScore || 0, a.inPlayScore || 0);
    const bMax = Math.max(b.longScore || 0, b.shortScore || 0, b.inPlayScore || 0);
    return bMax - aMax;
  });
}

export async function buildDashboardData(): Promise<DashboardData> {
  if (isMockForced()) {
    return {
      ...mockDashboardData,
      overview: { ...mockDashboardData.overview, generatedAt: new Date().toISOString() }
    };
  }

  const longBase = env("SCANNER_LONG_URL", "http://127.0.0.1:8080");
  const shortBase = env("SCANNER_SHORT_URL", "http://127.0.0.1:8081");
  const liveBase = env("SCANNER_LIVE_URL", "http://127.0.0.1:8787");
  const oflowBase = env("SCANNER_OFLOW_URL", "http://127.0.0.1:8090");
  const tapeBase = env("SCANNER_TAPE_URL", "http://127.0.0.1:8091");
  const whaleBase = env("SCANNER_WHALE_URL", "http://127.0.0.1:8092");
  const liqsBase = env("SCANNER_LIQS_URL", "http://127.0.0.1:8093");

  const [longSnap, shortSnap, liveStatus, oflowStatus, tapeStatus, whaleStatus, liqsStatus] = await Promise.all([
    fetchJson<ScannerSnapshot>(`${longBase}/api/status`),
    fetchJson<ScannerSnapshot>(`${shortBase}/api/status`),
    fetchJson<LiveLiteStatus>(`${liveBase}/api/status`),
    fetchJson<Record<string, unknown>>(`${oflowBase}/api/status`),
    fetchJson<Record<string, unknown>>(`${tapeBase}/api/status`),
    fetchJson<Record<string, unknown>>(`${whaleBase}/api/status`),
    fetchJson<Record<string, unknown>>(`${liqsBase}/api/status`)
  ]);

  if (!longSnap && !shortSnap && !liveStatus) {
    return {
      ...mockDashboardData,
      mode: "mock",
      overview: {
        ...mockDashboardData.overview,
        generatedAt: new Date().toISOString(),
        sessionTags: ["MOCK_FALLBACK"]
      }
    };
  }

  const longRows = longSnap?.Rows || [];
  const shortRows = shortSnap?.Rows || [];
  const longInPlay = longSnap?.InPlay || [];
  const shortInPlay = shortSnap?.InPlay || [];
  const longConf = longSnap?.Conf || {};
  const shortConf = shortSnap?.Conf || {};

  const longMap = normalizeRows(longRows, longConf, "long");
  const shortMap = normalizeRows(shortRows, shortConf, "short");
  const assets = mergeAssetMaps(longMap, shortMap, longInPlay, shortInPlay);
  const sessionTags = [...new Set([...(longSnap?.Active || []), ...(shortSnap?.Active || [])])];

  const modules: ModuleCardData[] = [
    moduleCard("live", "live", `${liveBase}/api/status`, Boolean(liveStatus), "Status API connected", "Status API unavailable"),
    moduleCard("long", "long scanner", `${longBase}/api/status`, Boolean(longSnap), "Long scanner connected", "Long scanner unavailable"),
    moduleCard("short", "short scanner", `${shortBase}/api/status`, Boolean(shortSnap), "Short scanner connected", "Short scanner unavailable"),
    moduleCard("oflow", "oflow", `${oflowBase}/api/status`, Boolean(oflowStatus), "Flow status connected", "Flow status unavailable"),
    moduleCard("tape", "tape", `${tapeBase}/api/status`, Boolean(tapeStatus), "Tape status connected", "Tape status unavailable"),
    moduleCard("whale", "whale", `${whaleBase}/api/status`, Boolean(whaleStatus), "Whale status connected", "Whale status unavailable"),
    moduleCard("liqs", "liqs", `${liqsBase}/api/status`, Boolean(liqsStatus), "Liqs status connected", "Liqs status unavailable")
  ];

  return {
    mode: "live",
    overview: {
      generatedAt: new Date().toISOString(),
      sessionTags,
      longEligible: longRows.length,
      shortEligible: shortRows.length,
      longInPlay: longInPlay.length || Number(liveStatus?.long_inplay || 0),
      shortInPlay: shortInPlay.length || Number(liveStatus?.short_inplay || 0),
      topSymbol: liveStatus?.top_symbol || assets[0]?.symbol,
      topSide: liveStatus?.top_side || (assets[0]?.longScore || 0) >= (assets[0]?.shortScore || 0) ? "BUY" : "SELL",
      topScore: liveStatus?.top_score || Math.max(assets[0]?.longScore || 0, assets[0]?.shortScore || 0)
    },
    assets,
    inPlayLong: assets.filter((a) => (a.inPlayScore || 0) > 0 && (a.longScore || 0) >= (a.shortScore || 0)),
    inPlayShort: assets.filter((a) => (a.inPlayScore || 0) > 0 && (a.shortScore || 0) > (a.longScore || 0)),
    modules,
    longScanner: {
      exchange: longSnap?.Exchange || "asterdex (LONGS)",
      generated: longSnap?.Generated || new Date().toISOString(),
      active: longSnap?.Active || [],
      rows: [...longMap.values()].map((r) => ({
        symbol: r.symbol || "",
        price: Number(r.price || 0),
        mark: Number(r.mark || 0),
        index: Number(r.index || 0),
        longScore: Number(r.longScore || 0),
        shortScore: Number(r.shortScore || 0),
        inPlayScore: Number(r.inPlayScore || 0),
        funding: Number(r.funding || 0),
        volume: Number(r.volume || 0),
        openInterest: Number(r.openInterest || 0),
        longGrade: r.longGrade,
        shortGrade: r.shortGrade,
        state: r.state,
        reason: r.reason,
        narrative: r.narrative
      }))
    },
    shortScanner: {
      exchange: shortSnap?.Exchange || "asterdex (SHORTS)",
      generated: shortSnap?.Generated || new Date().toISOString(),
      active: shortSnap?.Active || [],
      rows: [...shortMap.values()].map((r) => ({
        symbol: r.symbol || "",
        price: Number(r.price || 0),
        mark: Number(r.mark || 0),
        index: Number(r.index || 0),
        longScore: Number(r.longScore || 0),
        shortScore: Number(r.shortScore || 0),
        inPlayScore: Number(r.inPlayScore || 0),
        funding: Number(r.funding || 0),
        volume: Number(r.volume || 0),
        openInterest: Number(r.openInterest || 0),
        longGrade: r.longGrade,
        shortGrade: r.shortGrade,
        state: r.state,
        reason: r.reason,
        narrative: r.narrative
      }))
    },
    live: liveStatus
      ? {
          generated: liveStatus.generated,
          dryRun: liveStatus.dry_run,
          liveEnabled: liveStatus.live_enabled,
          topSymbol: liveStatus.top_symbol,
          topSide: liveStatus.top_side,
          topGrade: liveStatus.top_grade,
          topScore: liveStatus.top_score,
          topSlope: liveStatus.top_slope,
          longInPlay: liveStatus.long_inplay,
          shortInPlay: liveStatus.short_inplay,
          availableUSDT: liveStatus.available_usdt,
          paperSummary: liveStatus.paper_summary,
          payoutCycleID: liveStatus.payout_cycle_id,
          payoutNextAt: liveStatus.payout_next_at,
          execOpen: liveStatus.exec?.open,
          execPending: liveStatus.exec?.pending,
          execPartial1: liveStatus.exec?.partial_tp1,
          execPartial2: liveStatus.exec?.partial_tp2,
          execClosed: liveStatus.exec?.closed
        }
      : undefined
  };
}

export async function buildAssetDetail(symbol: string): Promise<AssetDetail> {
  const dashboard = await buildDashboardData();
  const upper = symbol.toUpperCase();
  const asset = dashboard.assets.find((a) => a.symbol === upper);

  const longBase = env("SCANNER_LONG_URL", "http://127.0.0.1:8080");
  const shortBase = env("SCANNER_SHORT_URL", "http://127.0.0.1:8081");
  const query = `${encodeURIComponent(upper)}&tf=5m&n=200&win=20&zmin=2.0&vmin=500000&levels=50`;
  const [longConfluence, shortConfluence] = await Promise.all([
    fetchJson<unknown>(`${longBase}/api/confluence?symbol=${query}&side=long`),
    fetchJson<unknown>(`${shortBase}/api/confluence?symbol=${query}&side=short`)
  ]);

  return {
    generatedAt: new Date().toISOString(),
    asset,
    longConfluence,
    shortConfluence
  };
}
