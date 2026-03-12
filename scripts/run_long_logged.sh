#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mkdir -p logs
STAMP="$(date +%F_%H%M%S)"
LOG_FILE="logs/long-${STAMP}.log"
LATEST_LINK="logs/long-latest.log"

pkill -9 -x long 2>/dev/null || true
pkill -9 -f 'cmd/long' 2>/dev/null || true

echo "writing long scanner output to ${LOG_FILE}"
ln -sfn "$(basename "$LOG_FILE")" "$LATEST_LINK"
go run ./cmd/long 2>&1 | tee "$LOG_FILE"
