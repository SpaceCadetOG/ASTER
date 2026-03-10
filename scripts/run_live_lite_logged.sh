#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mkdir -p logs
LOG_FILE="logs/live-lite-$(date +%F).log"

echo "writing live-lite output to ${LOG_FILE}"
go run ./cmd/live-lite 2>&1 | tee -a "$LOG_FILE"
