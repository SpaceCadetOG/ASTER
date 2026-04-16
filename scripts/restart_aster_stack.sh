#!/usr/bin/env bash
set -euo pipefail

ATTACH_SESSION="${TMUX_ATTACH_SESSION:-aster-live-lite}"
WRAPPER_SERVICE="aster-modules-tmux"
LEGACY_SERVICES=(aster-live-lite aster-tape aster-whale aster-liqs aster-oflow aster-long aster-short)

echo "[aster] stopping legacy services to avoid duplicate runners..."
sudo systemctl disable --now "${LEGACY_SERVICES[@]}" 2>/dev/null || true

echo "[aster] ensuring wrapper service is active..."
sudo systemctl daemon-reload
sudo systemctl enable --now "${WRAPPER_SERVICE}"
sudo systemctl restart "${WRAPPER_SERVICE}"

echo "[aster] tmux sessions:"
tmux ls || true

if tmux has-session -t "${ATTACH_SESSION}" 2>/dev/null; then
  echo "[aster] attaching ${ATTACH_SESSION}"
  exec tmux attach -t "${ATTACH_SESSION}"
fi

echo "[aster] attach session not found: ${ATTACH_SESSION}"
echo "[aster] available sessions:"
tmux ls || true
