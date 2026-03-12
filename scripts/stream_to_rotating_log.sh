#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <prefix>" >&2
  exit 1
fi

PREFIX="$1"
LOG_DIR="${ASTER_LOG_DIR:-logs}"
LOG_TZ="${ASTER_LOG_TZ:-America/Chicago}"
LOG_DATE_MODE="${ASTER_LOG_DATE_MODE:-current_date}"
LOG_CYCLE_HOUR="${ASTER_LOG_CYCLE_HOUR:-16}"
LATEST_LINK="${LOG_DIR}/${PREFIX}-latest.log"

mkdir -p "$LOG_DIR"

trading_day_key() {
  python3 - "$LOG_TZ" "$LOG_CYCLE_HOUR" <<'PY'
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo
import sys

tz = ZoneInfo(sys.argv[1])
cycle_hour = int(sys.argv[2])
now = datetime.now(tz)
if now.hour < cycle_hour:
    now -= timedelta(days=1)
print(now.strftime("%Y-%m-%d"))
PY
}

current_date_key() {
  python3 - "$LOG_TZ" <<'PY'
from datetime import datetime
from zoneinfo import ZoneInfo
import sys

tz = ZoneInfo(sys.argv[1])
print(datetime.now(tz).strftime("%Y-%m-%d"))
PY
}

log_day_key() {
  case "$LOG_DATE_MODE" in
    current_date)
      current_date_key
      ;;
    trading_day)
      trading_day_key
      ;;
    *)
      echo "invalid ASTER_LOG_DATE_MODE: ${LOG_DATE_MODE} (expected current_date or trading_day)" >&2
      exit 1
      ;;
  esac
}

open_log_for_key() {
  local key="$1"
  CURRENT_FILE="${LOG_DIR}/${PREFIX}-${key}.log"
  ln -sfn "$(basename "$CURRENT_FILE")" "$LATEST_LINK"
}

CURRENT_KEY="$(log_day_key)"
CURRENT_FILE=""
open_log_for_key "$CURRENT_KEY"
echo "writing ${PREFIX} output to ${CURRENT_FILE}" >&2

while IFS= read -r line || [[ -n "$line" ]]; do
  next_key="$(log_day_key)"
  if [[ "$next_key" != "$CURRENT_KEY" ]]; then
    CURRENT_KEY="$next_key"
    open_log_for_key "$CURRENT_KEY"
    echo "rotated ${PREFIX} log to ${CURRENT_FILE}" >&2
  fi
  printf '%s\n' "$line" | tee -a "$CURRENT_FILE"
done
