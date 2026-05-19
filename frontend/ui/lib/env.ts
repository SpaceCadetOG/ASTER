const DEFAULTS = {
  SCANNER_LONG_URL: "http://127.0.0.1:8080",
  SCANNER_SHORT_URL: "http://127.0.0.1:8081",
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
    oflow: pick("OFLOW_URL", "SCANNER_OFLOW_URL") || DEFAULTS.OFLOW_URL,
    tape: pick("TAPE_URL", "SCANNER_TAPE_URL") || DEFAULTS.TAPE_URL,
    whale: pick("WHALE_URL", "SCANNER_WHALE_URL") || DEFAULTS.WHALE_URL,
    liqs: pick("LIQS_URL", "SCANNER_LIQS_URL") || DEFAULTS.LIQS_URL,
    live: pick("LIVE_URL", "SCANNER_LIVE_URL") || DEFAULTS.LIVE_URL
  };
}
