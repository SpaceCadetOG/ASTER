#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LOG_DIR="${ASTER_LOG_DIR:-logs}"
mkdir -p "$LOG_DIR"

pkill -9 -x live-lite 2>/dev/null || true
pkill -9 -f 'cmd/live-lite' 2>/dev/null || true

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

echo "Starting live-lite in ${mode_label} mode"
echo "Manual trades opened on the exchange will be imported and managed by the bot."

go run ./cmd/live-lite 2>&1 | bash scripts/stream_to_rotating_log.sh live-lite
