import type { AssetDetail, DashboardData } from "@/lib/types";

const now = new Date().toISOString();

export const mockDashboardData: DashboardData = {
  generatedAt: now,
  mode: "degraded",
  longScanner: {
    side: "long",
    exchange: "asterdex (LONGS)",
    generated: now,
    active: ["LONDON", "NEW_YORK", "OVERLAP"],
    state: "ready",
    endpoint: "http://127.0.0.1:8080/api/status",
    connected: true,
    health: "live",
    rows: [
      {
        symbol: "BTC-USD",
        score: 92.4,
        reason: "Bid absorption and momentum continuation",
        volumeUsd: 982000000,
        openInterestUsd: 310000000,
        fundingRatePct: 0.012,
        openPrice: 68408.8,
        lastPrice: 68415.5,
        change24h: 1.8,
        dayUtc24h: 1.8,
        utc4hPct: 0.9,
        utc1hPct: 0.3,
        grade: "A+"
      }
    ],
    inPlay: [
      {
        symbol: "BTC-USD",
        sideBias: "LONG",
        currentGrade: "A+",
        currentScore: 92.4,
        scoreSlope: 0.44,
        state: "heating",
        momentum: true,
        lastSeen: now
      }
    ]
  },
  shortScanner: {
    side: "short",
    exchange: "asterdex (SHORTS)",
    generated: now,
    active: ["LONDON", "NEW_YORK", "OVERLAP"],
    state: "ready",
    endpoint: "http://127.0.0.1:8081/api/status",
    connected: true,
    health: "live",
    rows: [
      {
        symbol: "ETH-USD",
        score: 81.2,
        reason: "Offer absorption and trend fatigue",
        volumeUsd: 412000000,
        openInterestUsd: 188000000,
        fundingRatePct: -0.02,
        openPrice: 3220.2,
        lastPrice: 3181.6,
        change24h: -2.3,
        dayUtc24h: -2.3,
        utc4hPct: -1.1,
        utc1hPct: -0.5,
        grade: "B"
      }
    ],
    inPlay: []
  },
  live: {
    generated: now,
    mode: "paper",
    modeState: "paper_enabled",
    dryRun: true,
    liveEnabled: false,
    endpoint: "http://127.0.0.1:8787/api/status",
    connected: true,
    health: "live",
    topSymbol: "BTC-USD",
    topSide: "LONG",
    topGrade: "A+",
    topScore: 92.4,
    topSlope: 0.44,
    availableUsdt: 1250.42,
    paperSummary: "Paper engine connected. Waiting for future trade ledger endpoints.",
    scannerLongs: [
      {
        symbol: "BTC-USD",
        side: "LONG",
        grade: "A+",
        score: 92.4,
        slope: 0.44,
        state: "heating",
        price: 68415.5,
        dayUtc: 1.8,
        utc4h: 0.9,
        utc1h: 0.3,
        volumeUsd: 982000000
      }
    ],
    scannerShorts: [
      {
        symbol: "ETH-USD",
        side: "SHORT",
        grade: "B",
        score: 81.2,
        slope: -0.28,
        state: "cooling",
        price: 3181.6,
        dayUtc: -2.3,
        utc4h: -1.1,
        utc1h: -0.5,
        volumeUsd: 412000000
      }
    ],
    exec: {
      open: 1,
      pending: 0,
      closed: 12
    }
  },
  modules: [
    {
      id: "oflow",
      label: "OFlow",
      url: "http://127.0.0.1:8090/api/asset?symbol=BTC-USD",
      connected: true,
      capability: "asset-detail",
      symbolMatch: true,
      note: "Order-flow metrics scoped to the selected token.",
      health: "live",
      lastUpdated: now,
      status: {
        symbol: "BTC-USD",
        signal: "buy-imbalance",
        score: 0.72,
        window_sec: 60,
        buy_usd: 820000,
        sell_usd: 510000,
        delta_usd: 310000,
        updated_at: now
      }
    },
    {
      id: "tape",
      label: "Tape",
      url: "http://127.0.0.1:8091/api/status",
      connected: false,
      capability: "module-status-only",
      symbolMatch: false,
      note: "Status-only: asset-scoped endpoint not available.",
      health: "disconnected",
      failedEndpoint: "http://127.0.0.1:8091/api/status -> request failed",
      status: null
    },
    {
      id: "whale",
      label: "Whale",
      url: "http://127.0.0.1:8092/api/asset?symbol=BTC-USD",
      connected: true,
      capability: "asset-detail",
      symbolMatch: true,
      note: "Whale flow window scoped to the selected token.",
      health: "live",
      lastUpdated: now,
      status: {
        symbol: "BTC-USD",
        last_side: "buy",
        total_usd: 2400000,
        count: 4,
        updated_at: now
      }
    },
    {
      id: "liqs",
      label: "Liqs",
      url: "http://127.0.0.1:8093/api/status",
      connected: true,
      capability: "module-status-only",
      symbolMatch: false,
      note: "Status-only: asset-scoped endpoint not available.",
      health: "live",
      lastUpdated: now,
      status: {
        last_symbol: "ETH-USD",
        last_side: "sell",
        events: 12,
        updated_at: now
      }
    }
  ],
  endpoints: [
    {
      id: "long",
      label: "Long Scanner",
      url: "http://127.0.0.1:8080/api/status",
      connected: true,
      scope: "status-only",
      state: "live",
      detail: "Scanner snapshot",
      lastUpdated: now
    },
    {
      id: "short",
      label: "Short Scanner",
      url: "http://127.0.0.1:8081/api/status",
      connected: true,
      scope: "status-only",
      state: "live",
      detail: "Scanner snapshot",
      lastUpdated: now
    },
    {
      id: "live",
      label: "Live",
      url: "http://127.0.0.1:8787/api/status",
      connected: true,
      scope: "status-only",
      state: "live",
      detail: "Runtime status",
      lastUpdated: now
    },
    {
      id: "oflow",
      label: "OFlow",
      url: "http://127.0.0.1:8090/api/asset?symbol=BTC-USD",
      connected: true,
      scope: "asset-scoped",
      state: "live",
      detail: "Asset-scoped",
      lastUpdated: now
    },
    {
      id: "tape",
      label: "Tape",
      url: "http://127.0.0.1:8091/api/status",
      connected: false,
      scope: "status-only",
      state: "disconnected",
      detail: "Status-only",
      failedEndpoint: "http://127.0.0.1:8091/api/status -> request failed"
    },
    {
      id: "whale",
      label: "Whale",
      url: "http://127.0.0.1:8092/api/asset?symbol=BTC-USD",
      connected: true,
      scope: "asset-scoped",
      state: "live",
      detail: "Asset-scoped",
      lastUpdated: now
    },
    {
      id: "liqs",
      label: "Liqs",
      url: "http://127.0.0.1:8093/api/status",
      connected: true,
      scope: "status-only",
      state: "live",
      detail: "Status-only",
      lastUpdated: now
    }
  ]
};

export const mockAssetDetail: AssetDetail = {
  generatedAt: now,
  symbol: "BTC-USD",
  requestedSide: "long",
  primaryScanner: mockDashboardData.longScanner,
  scannerRow: mockDashboardData.longScanner?.rows[0] || null,
  longScannerRow: mockDashboardData.longScanner?.rows[0] || null,
  shortScannerRow: null,
  inPlayEntries: mockDashboardData.longScanner?.inPlay || [],
  live: mockDashboardData.live,
  modules: mockDashboardData.modules,
  analytics: {
    requestSymbol: "BTC-USD",
    resolvedSymbol: "BTC-USD",
    longConfluence: {
      score: 92.4,
      trend: "uptrend",
      effort: "steady",
      notes: ["Bid absorption held through overlap.", "Range expansion still orderly."],
      order_block: "5m demand held on retest."
    },
    shortConfluence: {
      score: 28.1,
      trend: "countertrend",
      effort: "weak",
      notes: ["Fade case remains secondary."]
    },
    fusion: { label: "trend continuation", score: 0.91 },
    structure: { higher_highs: true, pullback_depth: "shallow" },
    patterns: { dominant: "flag", continuation: true },
    volstats: { volume_z: 2.4, participation: "high" },
    candles: null
  },
  backendGaps: [
    "Execution preview is read-only. No live order-entry endpoint is exposed.",
    "Detailed paper trade ledger, MFE/MAE, W/L, and exit reasons require future paper endpoints."
  ],
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
