export type ScannerSide = "long" | "short";

export type Grade = "A+" | "A" | "B" | "C" | "D" | "N/A";

export interface ScannerRow {
  symbol: string;
  score: number;
  reason: string;
  volumeUsd: number;
  openInterestUsd: number | null;
  fundingRatePct: number | null;
  openPrice: number;
  lastPrice: number;
  change24h: number;
  dayUtc24h: number | null;
  utc4hPct: number | null;
  utc1hPct: number | null;
  grade: Grade;
}

export interface InPlayRow {
  symbol: string;
  sideBias: string;
  currentGrade: string;
  currentScore: number;
  scoreSlope: number;
  state: string;
  momentum: boolean;
  lastSeen?: string;
}

export interface ScannerView {
  side: ScannerSide;
  exchange: string;
  generated: string;
  active: string[];
  rows: ScannerRow[];
  inPlay: InPlayRow[];
}

export interface LiveScanItem {
  symbol: string;
  side: string;
  grade: string;
  score: number;
  slope: number;
  state: string;
  price: number;
  dayUtc: number;
  utc4h: number;
  utc1h: number;
  volumeUsd: number;
}

export interface LiveView {
  generated?: string;
  dryRun?: boolean;
  liveEnabled?: boolean;
  scannerBias?: string;
  topSymbol?: string;
  topSide?: string;
  topGrade?: string;
  topScore?: number;
  topSlope?: number;
  topDecision?: string;
  topDecisionWhy?: string;
  topRejectReason?: string;
  topTriggerState?: string;
  longInPlay?: number;
  shortInPlay?: number;
  availableUsdt?: number;
  paperSummary?: string;
  scannerLongs?: LiveScanItem[];
  scannerShorts?: LiveScanItem[];
  exec?: Record<string, number>;
}

export interface ModuleSummary {
  id: "oflow" | "tape" | "whale" | "liqs";
  label: string;
  url: string;
  connected: boolean;
  capability: "module-status-only" | "asset-detail";
  status: Record<string, unknown> | null;
  symbolMatch: boolean;
  note: string;
}

export interface DashboardData {
  generatedAt: string;
  longScanner: ScannerView | null;
  shortScanner: ScannerView | null;
  live: LiveView | null;
  modules: ModuleSummary[];
}

export interface AnalyticsBundle {
  requestSymbol: string;
  resolvedSymbol?: string;
  longConfluence: Record<string, unknown> | null;
  shortConfluence: Record<string, unknown> | null;
  fusion: Record<string, unknown> | null;
  structure: Record<string, unknown> | null;
  patterns: Record<string, unknown> | null;
  volstats: Record<string, unknown> | null;
  candles: {
    symbol?: string;
    tf?: string;
    data?: Array<{
      t: string;
      O: number;
      H: number;
      L: number;
      C: number;
      V: number;
    }>;
  } | null;
}

export interface TokenDetailData {
  generatedAt: string;
  symbol: string;
  requestedSide: ScannerSide;
  primaryScanner: ScannerView | null;
  scannerRow: ScannerRow | null;
  longScannerRow: ScannerRow | null;
  shortScannerRow: ScannerRow | null;
  inPlayEntries: InPlayRow[];
  live: LiveView | null;
  modules: ModuleSummary[];
  analytics: AnalyticsBundle;
  backendGaps: string[];
  tradePanel: {
    executionAvailable: false;
    reason: string;
    expectedContract: {
      method: "POST";
      path: "/api/trades";
      fields: string[];
    };
  };
}
