import type { DashboardData } from "@/lib/types";

export const mockDashboardData: DashboardData = {
  mode: "mock",
  overview: {
    generatedAt: new Date().toISOString(),
    sessionTags: ["ASIA"],
    longEligible: 8,
    shortEligible: 5,
    longInPlay: 4,
    shortInPlay: 2,
    topSymbol: "HUSDT",
    topSide: "BUY",
    topScore: 95.2
  },
  assets: [
    {
      symbol: "HUSDT",
      price: 0.183,
      mark: 0.1831,
      index: 0.1829,
      longScore: 95.2,
      shortScore: 22.1,
      inPlayScore: 92.6,
      funding: 0.012,
      volume: 25300000,
      openInterest: 12200000,
      longGrade: "A+",
      shortGrade: "D",
      state: "heating",
      reason: "fa",
      narrative: "Momentum persistent with supportive in-play slope."
    },
    {
      symbol: "BTCUSDT",
      price: 68411,
      mark: 68415.5,
      index: 68408.8,
      longScore: 78.1,
      shortScore: 76.2,
      inPlayScore: 80.7,
      funding: -0.003,
      volume: 982000000,
      openInterest: 310000000,
      longGrade: "B",
      shortGrade: "B",
      state: "in-play",
      reason: "vwap_confluence",
      narrative: "Balanced tape; scanner shows bid/ask pressure crossover."
    }
  ],
  inPlayLong: [
    {
      symbol: "HUSDT",
      price: 0.183,
      mark: 0.1831,
      index: 0.1829,
      longScore: 95.2,
      shortScore: 22.1,
      inPlayScore: 92.6,
      funding: 0.012,
      volume: 25300000,
      openInterest: 12200000,
      longGrade: "A+",
      shortGrade: "D",
      state: "heating",
      reason: "fa",
      narrative: "Momentum persistent with supportive in-play slope."
    }
  ],
  inPlayShort: [
    {
      symbol: "BTCUSDT",
      price: 68411,
      mark: 68415.5,
      index: 68408.8,
      longScore: 78.1,
      shortScore: 76.2,
      inPlayScore: 80.7,
      funding: -0.003,
      volume: 982000000,
      openInterest: 310000000,
      longGrade: "B",
      shortGrade: "B",
      state: "cooling",
      reason: "vwap_confluence",
      narrative: "Balanced tape; scanner shows bid/ask pressure crossover."
    }
  ],
  modules: [
    {
      key: "live-lite",
      label: "live-lite",
      source: "http://127.0.0.1:8787/api/status",
      status: "ok",
      note: "Execution status reachable"
    },
    {
      key: "long",
      label: "long scanner",
      source: "http://127.0.0.1:8080/api/status",
      status: "ok",
      note: "Long scanner feed reachable"
    },
    {
      key: "short",
      label: "short scanner",
      source: "http://127.0.0.1:8081/api/status",
      status: "ok",
      note: "Short scanner feed reachable"
    },
    {
      key: "oflow",
      label: "oflow",
      source: "stream module",
      status: "warn",
      note: "Stream-only module (no HTTP endpoint yet)"
    },
    {
      key: "whale",
      label: "whale",
      source: "stream module",
      status: "warn",
      note: "Stream-only module (no HTTP endpoint yet)"
    },
    {
      key: "liqs",
      label: "liqs",
      source: "stream module",
      status: "warn",
      note: "Stream-only module (no HTTP endpoint yet)"
    },
    {
      key: "tape",
      label: "tape",
      source: "stream module",
      status: "warn",
      note: "Stream-only module (no HTTP endpoint yet)"
    }
  ],
  longScanner: {
    exchange: "asterdex (LONGS)",
    generated: new Date().toISOString(),
    active: ["ASIA"],
    rows: []
  },
  shortScanner: {
    exchange: "asterdex (SHORTS)",
    generated: new Date().toISOString(),
    active: ["ASIA"],
    rows: []
  }
};
