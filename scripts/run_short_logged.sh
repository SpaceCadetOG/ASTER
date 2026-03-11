#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mkdir -p logs
STAMP="$(date +%F_%H%M%S)"
LOG_FILE="logs/short-${STAMP}.log"
LATEST_LINK="logs/short-latest.log"

echo "writing short scanner output to ${LOG_FILE}"
ln -sfn "$(basename "$LOG_FILE")" "$LATEST_LINK"
go run ./cmd/short 2>&1 | tee "$LOG_FILE"
