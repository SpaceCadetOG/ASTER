#!/usr/bin/env bash
set -euo pipefail

SESSION="${TMUX_SESSION_NAME:-aster}"
SERVICES=(
  aster-live
  aster-tape
  aster-whale
  aster-liqs
  aster-oflow
  aster-long
  aster-short
)

echo "[aster] stopping services..."
sudo systemctl stop "${SERVICES[@]}" 2>/dev/null || true

echo "[aster] killing tmux session: ${SESSION} (if running)"
tmux kill-session -t "${SESSION}" 2>/dev/null || true

echo "[aster] stopped."
