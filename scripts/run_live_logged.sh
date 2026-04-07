#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

ENV_FILE="${ASTER_ENV_FILE:-/opt/aster/env/live.env}"
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

launch_mode="${LIVE_LAUNCH_MODE:-}"
if [[ -z "$launch_mode" && -t 0 ]]; then
  printf "Go live? [y/N]: "
  read -r reply
  case "${reply,,}" in
    y|yes)
      launch_mode="live"
      ;;
    *)
      launch_mode="paper"
      ;;
  esac
fi

if [[ -z "$launch_mode" ]]; then
  launch_mode="paper"
fi

case "${launch_mode,,}" in
  y|yes|live)
    export LIVE_ENABLE_LIVE_TRADING=1
    export LIVE_DRY_RUN=0
    mode_label="LIVE"
    ;;
  n|no|paper|dry|dry-run)
    export LIVE_ENABLE_LIVE_TRADING=0
    export LIVE_DRY_RUN=1
    mode_label="PAPER"
    ;;
  *)
    echo "Invalid LIVE_LAUNCH_MODE='$launch_mode'. Use live or paper." >&2
    exit 1
    ;;
esac

echo "starting ${mode_label} using $(basename "${ENV_FILE}")"
echo "logs: ${LOG_DIR}/live-*.log"

go run ./cmd/live 2>&1 | bash scripts/stream_to_rotating_log.sh live
