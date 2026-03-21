#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LOG_DIR="${ASTER_LOG_DIR:-logs}"
mkdir -p "$LOG_DIR"

pkill -9 -x live-lite 2>/dev/null || true
pkill -9 -f 'cmd/live-lite' 2>/dev/null || true

export LIVE_DRY_RUN="${LIVE_DRY_RUN:-0}"
export LIVE_ENABLE_LIVE_TRADING="${LIVE_ENABLE_LIVE_TRADING:-1}"

# Balanced live profile: still active, but with hard anti-churn guards.
export LIVE_MAX_OPEN_POS="${LIVE_MAX_OPEN_POS:-2}"
export LIVE_MAX_OPEN_PER_SIDE="${LIVE_MAX_OPEN_PER_SIDE:-1}"
export LIVE_TRADE_SLOTS="${LIVE_TRADE_SLOTS:-2}"
export LIVE_MAX_ORDERS_PER_DAY="${LIVE_MAX_ORDERS_PER_DAY:-6}"
export LIVE_MAX_ORDERS_PER_HOUR="${LIVE_MAX_ORDERS_PER_HOUR:-2}"
export LIVE_ORDER_COOLDOWN_SEC="${LIVE_ORDER_COOLDOWN_SEC:-900}"
export LIVE_SYMBOL_COOLDOWN_SEC="${LIVE_SYMBOL_COOLDOWN_SEC:-7200}"

# Keep the new execution safety behavior on by default.
export LIVE_STOP_ENGINE_V2_ENABLE="${LIVE_STOP_ENGINE_V2_ENABLE:-1}"
export LIVE_STOP_TEMPLATE_MODE="${LIVE_STOP_TEMPLATE_MODE:-setup}"
export LIVE_STOP_TRIGGER_REF="${LIVE_STOP_TRIGGER_REF:-mark}"
export LIVE_TP_TRIGGER_REF="${LIVE_TP_TRIGGER_REF:-mark}"
export LIVE_TRIGGER_PRICE_PROTECT="${LIVE_TRIGGER_PRICE_PROTECT:-1}"

# Live profile tuned for flow without giving churn room.
export LIVE_SIMPLE_MODE="${LIVE_SIMPLE_MODE:-true}"
export LIVE_MIN_GRADE="${LIVE_MIN_GRADE:-B}"
export LIVE_B_NEAR_A_ONLY="${LIVE_B_NEAR_A_ONLY:-true}"
export LIVE_MIN_ENTRY_CONF="${LIVE_MIN_ENTRY_CONF:-0.62}"
export LIVE_LEVERAGE_MODE="${LIVE_LEVERAGE_MODE:-fixed}"
export LIVE_LEVERAGE_FIXED="${LIVE_LEVERAGE_FIXED:-2}"
export LIVE_MAX_LEVERAGE="${LIVE_MAX_LEVERAGE:-2}"
export LIVE_TRADE_MARGIN_USDT="${LIVE_TRADE_MARGIN_USDT:-20}"
export LIVE_MIN_AVAILABLE_USDT="${LIVE_MIN_AVAILABLE_USDT:-35}"
export LIVE_MAX_DAILY_LOSS_PCT="${LIVE_MAX_DAILY_LOSS_PCT:-2.0}"

# Churn control stays strict even though trade flow is looser.
export LIVE_SYMBOL_STOPOUT_COUNT="${LIVE_SYMBOL_STOPOUT_COUNT:-1}"
export LIVE_SYMBOL_STOPOUT_LOCK_MIN="${LIVE_SYMBOL_STOPOUT_LOCK_MIN:-180}"
export LIVE_SYMBOL_QUICK_LOSS_LOCK_COUNT="${LIVE_SYMBOL_QUICK_LOSS_LOCK_COUNT:-1}"
export LIVE_SYMBOL_QUICK_LOSS_LOCK_MIN="${LIVE_SYMBOL_QUICK_LOSS_LOCK_MIN:-60}"
export LIVE_SYMBOL_QUICK_LOSS_DAYUTC_PCT="${LIVE_SYMBOL_QUICK_LOSS_DAYUTC_PCT:-25}"

printf 'balanced-live profile: margin=%s lev=%sx max_open=%s max_per_side=%s orders/day=%s orders/hour=%s max_daily_loss=%s%% min_grade=%s min_conf=%s stop_ref=%s\n' \
  "$LIVE_TRADE_MARGIN_USDT" "$LIVE_LEVERAGE_FIXED" "$LIVE_MAX_OPEN_POS" "$LIVE_MAX_OPEN_PER_SIDE" "$LIVE_MAX_ORDERS_PER_DAY" "$LIVE_MAX_ORDERS_PER_HOUR" \
  "$LIVE_MAX_DAILY_LOSS_PCT" "$LIVE_MIN_GRADE" "$LIVE_MIN_ENTRY_CONF" "$LIVE_STOP_TRIGGER_REF" >&2

go run ./cmd/live-lite 2>&1 | bash scripts/stream_to_rotating_log.sh live-lite
