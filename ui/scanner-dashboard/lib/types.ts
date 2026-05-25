export type DashboardTab =
  | "overview"
  | "scanners"
  | "hotlist"
  | "runtime"
  | "paper"
  | "health";

export type ScannerSide = "long" | "short";
export type Grade = "A+" | "A" | "B" | "C" | "D" | "N/A";
export type LoadState = "unavailable" | "empty" | "ready";
export type ConnectivityState = "live" | "stale" | "disconnected" | "unavailable";

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
  state: LoadState;
  endpoint: string;
  connected: boolean;
  health: ConnectivityState;
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
  mode?: string;
  modeState?: string;
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
  liveAccount?: LiveAccountView;
  paperSummary?: string;
  paper?: PaperView;
  scannerLongs?: LiveScanItem[];
  scannerShorts?: LiveScanItem[];
  exec?: Record<string, number>;
  endpoint: string;
  connected: boolean;
  health: ConnectivityState;
}

export interface LiveAccountView {
  generated?: string;
  health?: string;
  healthDetail?: string;
  availableUsdt: number;
  equity: number;
  realizedDay: number;
  openPnl: number;
  openCount: number;
  botCount: number;
  manualCount: number;
  positions?: LiveAccountPositionView[];
}

export interface LiveAccountPositionView {
  symbol: string;
  side: string;
  source?: string;
  manageState?: string;
  protectionState?: string;
  managed: boolean;
  protected: boolean;
  qty: number;
  entryPrice: number;
  markPrice: number;
  lastPrice: number;
  spreadBps: number;
  unrealizedPnl: number;
  unrealizedPnlPct: number;
  realizedPnl: number;
  exchangeUnreal: number;
  leverage: number;
  margin: number;
  stopPrice: number;
  holdMin: number;
  entryReason?: string;
}

export interface PaperPositionView {
  symbol: string;
  side: string;
  source?: string;
  mode?: string;
  strategy?: string;
  setupFamily?: string;
  grade?: string;
  state?: string;
  triggerState?: string;
  exitProfile?: string;
  entryPrice: number;
  markPrice: number;
  stopPrice: number;
  tp1?: number;
  tp2?: number;
  tp3?: number;
  qty: number;
  margin: number;
  leverage: number;
  unrealizedPnl: number;
  unrealizedPct: number;
  realizedPnl: number;
  mfeR: number;
  maeR: number;
  openedAt?: string;
  holdMin: number;
  entryReason?: string;
  entryDecisionReject?: string;
}

export interface PaperClosedTradeView {
  symbol: string;
  side: string;
  source?: string;
  mode?: string;
  strategy?: string;
  setupFamily?: string;
  grade?: string;
  state?: string;
  triggerState?: string;
  exitProfile?: string;
  entryPrice: number;
  exitPrice: number;
  pnlUsd: number;
  pnlPct: number;
  fees: number;
  riskR: number;
  holdMin: number;
  mfeR: number;
  maeR: number;
  captureRatio: number;
  maxGivebackR: number;
  exitReason?: string;
  closedAt?: string;
}

export interface PaperDecisionView {
  symbol: string;
  side: string;
  source?: string;
  mode?: string;
  strategy?: string;
  setupFamily?: string;
  grade?: string;
  state?: string;
  triggerState?: string;
  exitProfile?: string;
  score: number;
  slope: number;
  confluenceScore: number;
  entryPrice: number;
  stopDistancePct: number;
  approved: boolean;
  rejectReason?: string;
  gateReasons?: string[];
  decidedAt?: string;
}

export interface PaperView {
  mode?: string;
  summary?: string;
  balance: number;
  reserve: number;
  equity: number;
  openPnl: number;
  realizedToday: number;
  openCount: number;
  recentClosedCount: number;
  recentDecisionCount: number;
  openPositions: PaperPositionView[];
  recentClosed: PaperClosedTradeView[];
  recentDecisions: PaperDecisionView[];
}

export interface EndpointStatus {
  id: "long" | "short" | "live" | "oflow" | "tape" | "whale" | "liqs";
  label: string;
  url: string;
  connected: boolean;
  scope: "status-only" | "asset-scoped";
  state: ConnectivityState;
  detail: string;
  failedEndpoint?: string;
  lastUpdated?: string;
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
  health: ConnectivityState;
  lastUpdated?: string;
  failedEndpoint?: string;
}

export interface DashboardData {
  generatedAt: string;
  mode: "live" | "degraded" | "unavailable";
  longScanner: ScannerView | null;
  shortScanner: ScannerView | null;
  live: LiveView | null;
  modules: ModuleSummary[];
  endpoints: EndpointStatus[];
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

export interface AssetDetail {
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
  executionPreview: {
    available: false;
    message: string;
    contract: {
      method: "POST";
      path: "/api/trades";
      fields: string[];
    };
  };
}
