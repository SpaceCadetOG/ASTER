#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mkdir -p logs
STAMP="$(date +%F_%H%M%S)"
LOG_FILE="logs/live-lite-${STAMP}.log"
LATEST_LINK="logs/live-lite-latest.log"

pkill -9 -x live-lite 2>/dev/null || true
pkill -9 -f 'cmd/live-lite' 2>/dev/null || true

echo "writing live-lite output to ${LOG_FILE}"
ln -sfn "$(basename "$LOG_FILE")" "$LATEST_LINK"
go run ./cmd/live-lite 2>&1 | tee "$LOG_FILE"
