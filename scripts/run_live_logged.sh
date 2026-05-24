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

DEFAULT_LOG_DIR="logs"
if [[ -d /opt/aster || "${ENV_FILE}" == /opt/aster/* ]]; then
  DEFAULT_LOG_DIR="/opt/aster/logs"
fi
LOG_DIR="${ASTER_LOG_DIR:-$DEFAULT_LOG_DIR}"
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
    export LIVE_RUNTIME_MODE=live
    mode_label="LIVE"
    ;;
  n|no|paper|dry|dry-run)
    export LIVE_ENABLE_LIVE_TRADING=0
    export LIVE_DRY_RUN=1
    export LIVE_RUNTIME_MODE=paper
    mode_label="PAPER"
    ;;
  *)
    echo "Invalid LIVE_LAUNCH_MODE='$launch_mode'. Use live or paper." >&2
    exit 1
    ;;
esac

echo "starting ${mode_label} using ${ENV_FILE}"
echo "logs: ${LOG_DIR}/live-*.log"

# Keep terminal readable while preserving full logs.
export ASTER_TERMINAL_HIDE_NOISE="${ASTER_TERMINAL_HIDE_NOISE:-1}"
export ASTER_NOISE_LOG_ENABLE="${ASTER_NOISE_LOG_ENABLE:-1}"
export ASTER_TERMINAL_NOISE_REGEX="${ASTER_TERMINAL_NOISE_REGEX:-SIMPLE_DECISION|live: perf mode=decision_worker|MISSED_OPP_EXPIRE}"
echo "noise logs: ${LOG_DIR}/live-noise-*.log"

go run ./cmd/live 2>&1 | bash scripts/stream_to_rotating_log.sh live
