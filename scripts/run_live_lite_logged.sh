#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

LOG_DIR="${ASTER_LOG_DIR:-logs}"
mkdir -p "$LOG_DIR"

pkill -9 -x live-lite 2>/dev/null || true
pkill -9 -f 'cmd/live-lite' 2>/dev/null || true

go run ./cmd/live-lite 2>&1 | bash scripts/stream_to_rotating_log.sh live-lite
