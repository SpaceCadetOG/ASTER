export type DashboardTab =
  | "overview"
  | "inplay"
  | "long"
  | "short"
  | "live"
  | "asset";

export type ModuleStatus = "ok" | "warn" | "down";

export interface ModuleCardData {
  key: string;
  label: string;
  source: string;
  status: ModuleStatus;
  note: string;
}

export interface AssetRow {
  symbol: string;
  price: number;
  mark: number;
  index: number;
  longScore: number;
  shortScore: number;
  inPlayScore: number;
  funding?: number;
  volume?: number;
  openInterest?: number;
  longGrade?: string;
  shortGrade?: string;
  state?: string;
  reason?: string;
  narrative?: string;
}

export interface OverviewMetrics {
  generatedAt: string;
  sessionTags: string[];
  longEligible: number;
  shortEligible: number;
  longInPlay: number;
  shortInPlay: number;
  topSymbol?: string;
  topSide?: string;
  topScore?: number;
}

export interface DashboardData {
  mode: "mock" | "live";
  overview: OverviewMetrics;
  assets: AssetRow[];
  inPlayLong: AssetRow[];
  inPlayShort: AssetRow[];
  modules: ModuleCardData[];
  longScanner?: {
    exchange: string;
    generated: string;
    active: string[];
    rows: AssetRow[];
  };
  shortScanner?: {
    exchange: string;
    generated: string;
    active: string[];
    rows: AssetRow[];
  };
  live?: {
    generated?: string;
    dryRun?: boolean;
    liveEnabled?: boolean;
    topSymbol?: string;
    topSide?: string;
    topGrade?: string;
    topScore?: number;
    topSlope?: number;
    longInPlay?: number;
    shortInPlay?: number;
    availableUSDT?: number;
    paperSummary?: string;
    payoutCycleID?: string;
    payoutNextAt?: string;
    execOpen?: number;
    execPending?: number;
    execPartial1?: number;
    execPartial2?: number;
    execClosed?: number;
  };
}

export interface AssetDetail {
  generatedAt: string;
  asset?: AssetRow;
  longConfluence?: unknown;
  shortConfluence?: unknown;
}
