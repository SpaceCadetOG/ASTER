const DEFAULTS = {
  SCANNER_LONG_URL: "http://127.0.0.1:8080",
  SCANNER_SHORT_URL: "http://127.0.0.1:8081",
  SCANNER_LIVE_URL: "http://127.0.0.1:8787",
  SCANNER_OFLOW_URL: "http://127.0.0.1:8090",
  SCANNER_TAPE_URL: "http://127.0.0.1:8091",
  SCANNER_WHALE_URL: "http://127.0.0.1:8092",
  SCANNER_LIQS_URL: "http://127.0.0.1:8093",
  OFLOW_URL: "http://127.0.0.1:8090",
  TAPE_URL: "http://127.0.0.1:8091",
  WHALE_URL: "http://127.0.0.1:8092",
  LIQS_URL: "http://127.0.0.1:8093",
  LIVE_URL: "http://127.0.0.1:8787"
} as const;

function pick(...names: string[]): string | undefined {
  for (const name of names) {
    const value = process.env[name]?.trim();
    if (value) {
      return value.replace(/\/+$/, "");
    }
  }
  return undefined;
}

export function getBackendUrls() {
  return {
    long: pick("SCANNER_LONG_URL") || DEFAULTS.SCANNER_LONG_URL,
    short: pick("SCANNER_SHORT_URL") || DEFAULTS.SCANNER_SHORT_URL,
    live: pick("SCANNER_LIVE_URL", "LIVE_URL") || DEFAULTS.SCANNER_LIVE_URL,
    oflow: pick("SCANNER_OFLOW_URL", "OFLOW_URL") || DEFAULTS.SCANNER_OFLOW_URL,
    tape: pick("SCANNER_TAPE_URL", "TAPE_URL") || DEFAULTS.SCANNER_TAPE_URL,
    whale: pick("SCANNER_WHALE_URL", "WHALE_URL") || DEFAULTS.SCANNER_WHALE_URL,
    liqs: pick("SCANNER_LIQS_URL", "LIQS_URL") || DEFAULTS.SCANNER_LIQS_URL
  };
}
