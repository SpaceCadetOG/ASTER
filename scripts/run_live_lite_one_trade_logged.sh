#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LOG_DIR="${ASTER_LOG_DIR:-logs}"
mkdir -p "$LOG_DIR"

LOCK_FILE="${ASTER_LOCK_FILE:-/tmp/aster-live-lite.lock}"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "live-lite already running (lock: $LOCK_FILE)" >&2
  exit 1
fi

pkill -9 -x live-lite 2>/dev/null || true
pkill -9 -f 'cmd/live-lite' 2>/dev/null || true

export LIVE_DRY_RUN="${LIVE_DRY_RUN:-0}"
export LIVE_ENABLE_LIVE_TRADING="${LIVE_ENABLE_LIVE_TRADING:-1}"

# True micro-live validation: at most one long and one short, low leverage.
export LIVE_MAX_OPEN_POS="${LIVE_MAX_OPEN_POS:-2}"
export LIVE_MAX_OPEN_PER_SIDE="${LIVE_MAX_OPEN_PER_SIDE:-1}"
export LIVE_TRADE_SLOTS="${LIVE_TRADE_SLOTS:-2}"
export LIVE_MAX_ORDERS_PER_DAY="${LIVE_MAX_ORDERS_PER_DAY:-0}"
export LIVE_MAX_ORDERS_PER_HOUR="${LIVE_MAX_ORDERS_PER_HOUR:-1}"
export LIVE_ORDER_COOLDOWN_SEC="${LIVE_ORDER_COOLDOWN_SEC:-900}"
export LIVE_SYMBOL_COOLDOWN_SEC="${LIVE_SYMBOL_COOLDOWN_SEC:-14400}"

# Conservative entry profile for behavior validation.
export LIVE_SIMPLE_MODE="${LIVE_SIMPLE_MODE:-true}"
export LIVE_MIN_GRADE="${LIVE_MIN_GRADE:-A}"
export LIVE_MIN_ENTRY_CONF="${LIVE_MIN_ENTRY_CONF:-0.60}"
export LIVE_LEVERAGE_MODE="${LIVE_LEVERAGE_MODE:-fixed}"
export LIVE_LEVERAGE_FIXED="${LIVE_LEVERAGE_FIXED:-2}"
export LIVE_MAX_LEVERAGE="${LIVE_MAX_LEVERAGE:-2}"
export LIVE_TRADE_MARGIN_USDT="${LIVE_TRADE_MARGIN_USDT:-25}"
export LIVE_MIN_AVAILABLE_USDT="${LIVE_MIN_AVAILABLE_USDT:-35}"
export LIVE_MAX_DAILY_LOSS_PCT="${LIVE_MAX_DAILY_LOSS_PCT:-2.0}"
export LIVE_SYMBOL_STOPOUT_COUNT="${LIVE_SYMBOL_STOPOUT_COUNT:-1}"
export LIVE_SYMBOL_STOPOUT_LOCK_MIN="${LIVE_SYMBOL_STOPOUT_LOCK_MIN:-180}"

printf 'one-long-one-short live profile: margin=%s lev=%sx max_open=%s max_per_side=%s orders/day=%s min_grade=%s min_conf=%s\n' \
  "$LIVE_TRADE_MARGIN_USDT" "$LIVE_LEVERAGE_FIXED" "$LIVE_MAX_OPEN_POS" "$LIVE_MAX_OPEN_PER_SIDE" "$LIVE_MAX_ORDERS_PER_DAY" "$LIVE_MIN_GRADE" "$LIVE_MIN_ENTRY_CONF" >&2

go run ./cmd/live-lite 2>&1 | bash scripts/stream_to_rotating_log.sh live-lite
