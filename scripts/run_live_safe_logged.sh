#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ENV_FILE="${ASTER_ENV_FILE:-/opt/aster/env/live.env}"
while [[ $# -gt 0 ]]; do
  case "$1" in
    -e|--env)
      shift
      if [[ $# -eq 0 ]]; then
        echo "missing value for --env" >&2
        exit 2
      fi
      ENV_FILE="$1"
      shift
      ;;
    *)
      echo "unknown arg: $1" >&2
      echo "usage: $0 [--env /path/to/live.env]" >&2
      exit 2
      ;;
  esac
done
if [[ -f "${ENV_FILE}" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a
fi

LOG_DIR="${ASTER_LOG_DIR:-logs}"
mkdir -p "$LOG_DIR"

LOCK_FILE="${ASTER_LOCK_FILE:-/tmp/aster-live.lock}"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "live already running (lock: $LOCK_FILE)" >&2
  exit 1
fi

pkill -9 -x live 2>/dev/null || true
pkill -9 -f 'cmd/live' 2>/dev/null || true

export LIVE_DRY_RUN="${LIVE_DRY_RUN:-0}"
export LIVE_ENABLE_LIVE_TRADING="${LIVE_ENABLE_LIVE_TRADING:-1}"

# Safe live profile: one idea at a time, small size, hard loss containment.
export LIVE_MAX_OPEN_POS="${LIVE_MAX_OPEN_POS:-1}"
export LIVE_MAX_OPEN_PER_SIDE="${LIVE_MAX_OPEN_PER_SIDE:-1}"
export LIVE_TRADE_SLOTS="${LIVE_TRADE_SLOTS:-1}"
export LIVE_MAX_ORDERS_PER_DAY="${LIVE_MAX_ORDERS_PER_DAY:-3}"
export LIVE_MAX_ORDERS_PER_HOUR="${LIVE_MAX_ORDERS_PER_HOUR:-1}"
export LIVE_ORDER_COOLDOWN_SEC="${LIVE_ORDER_COOLDOWN_SEC:-1800}"
export LIVE_SYMBOL_COOLDOWN_SEC="${LIVE_SYMBOL_COOLDOWN_SEC:-21600}"

# Keep the new execution safety behavior on by default.
export LIVE_STOP_ENGINE_V2_ENABLE="${LIVE_STOP_ENGINE_V2_ENABLE:-1}"
export LIVE_STOP_TEMPLATE_MODE="${LIVE_STOP_TEMPLATE_MODE:-setup}"
export LIVE_STOP_TRIGGER_REF="${LIVE_STOP_TRIGGER_REF:-mark}"
export LIVE_TP_TRIGGER_REF="${LIVE_TP_TRIGGER_REF:-mark}"
export LIVE_TRIGGER_PRICE_PROTECT="${LIVE_TRIGGER_PRICE_PROTECT:-1}"

# Small-account containment.
export LIVE_SIMPLE_MODE="${LIVE_SIMPLE_MODE:-true}"
export LIVE_MIN_GRADE="${LIVE_MIN_GRADE:-A}"
export LIVE_MIN_ENTRY_CONF="${LIVE_MIN_ENTRY_CONF:-0.62}"
export LIVE_LEVERAGE_MODE="${LIVE_LEVERAGE_MODE:-fixed}"
export LIVE_LEVERAGE_FIXED="${LIVE_LEVERAGE_FIXED:-2}"
export LIVE_MAX_LEVERAGE="${LIVE_MAX_LEVERAGE:-2}"
export LIVE_TRADE_MARGIN_USDT="${LIVE_TRADE_MARGIN_USDT:-15}"
export LIVE_MIN_AVAILABLE_USDT="${LIVE_MIN_AVAILABLE_USDT:-30}"
export LIVE_MAX_DAILY_LOSS_PCT="${LIVE_MAX_DAILY_LOSS_PCT:-1.5}"

# Kill repeated same-symbol churn quickly.
export LIVE_SYMBOL_STOPOUT_COUNT="${LIVE_SYMBOL_STOPOUT_COUNT:-1}"
export LIVE_SYMBOL_STOPOUT_LOCK_MIN="${LIVE_SYMBOL_STOPOUT_LOCK_MIN:-180}"
export LIVE_SYMBOL_QUICK_LOSS_LOCK_COUNT="${LIVE_SYMBOL_QUICK_LOSS_LOCK_COUNT:-1}"
export LIVE_SYMBOL_QUICK_LOSS_LOCK_MIN="${LIVE_SYMBOL_QUICK_LOSS_LOCK_MIN:-60}"
export LIVE_SYMBOL_QUICK_LOSS_DAYUTC_PCT="${LIVE_SYMBOL_QUICK_LOSS_DAYUTC_PCT:-25}"

printf 'safe-live profile: margin=%s lev=%sx max_open=%s orders/day=%s orders/hour=%s max_daily_loss=%s%% min_grade=%s min_conf=%s stop_ref=%s\n' \
  "$LIVE_TRADE_MARGIN_USDT" "$LIVE_LEVERAGE_FIXED" "$LIVE_MAX_OPEN_POS" "$LIVE_MAX_ORDERS_PER_DAY" "$LIVE_MAX_ORDERS_PER_HOUR" \
  "$LIVE_MAX_DAILY_LOSS_PCT" "$LIVE_MIN_GRADE" "$LIVE_MIN_ENTRY_CONF" "$LIVE_STOP_TRIGGER_REF" >&2
echo "starting SAFE LIVE using ${ENV_FILE}" >&2
echo "logs: ${LOG_DIR}/live-*.log" >&2
echo "noise logs: ${LOG_DIR}/live-noise-*.log" >&2

# Keep terminal readable while preserving full logs.
export ASTER_TERMINAL_HIDE_NOISE="${ASTER_TERMINAL_HIDE_NOISE:-1}"
export ASTER_NOISE_LOG_ENABLE="${ASTER_NOISE_LOG_ENABLE:-1}"
export ASTER_TERMINAL_NOISE_REGEX="${ASTER_TERMINAL_NOISE_REGEX:-SIMPLE_DECISION|live: perf mode=decision_worker|MISSED_OPP_EXPIRE}"

go run ./cmd/live 2>&1 | bash scripts/stream_to_rotating_log.sh live
