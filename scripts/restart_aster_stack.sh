#!/usr/bin/env bash
set -euo pipefail

SESSION="${TMUX_SESSION_NAME:-aster}"
CORE_SERVICES=(
  aster-live-lite
  aster-tape
  aster-whale
)
EXTRA_SERVICES=(
  aster-liqs
  aster-oflow
  aster-long
  aster-short
)

LOG_LIVE="${LOG_LIVE:-journalctl -u aster-live-lite -f}"
LOG_TAPE="${LOG_TAPE:-journalctl -u aster-tape -f}"
LOG_WHALE="${LOG_WHALE:-journalctl -u aster-whale -f}"
LOG_EXTRA="${LOG_EXTRA:-journalctl -u aster-liqs -u aster-oflow -f}"

echo "[aster] stopping old services..."
sudo systemctl stop "${CORE_SERVICES[@]}" "${EXTRA_SERVICES[@]}" 2>/dev/null || true

echo "[aster] stopping old tmux session (if any)..."
tmux kill-session -t "${SESSION}" 2>/dev/null || true

echo "[aster] starting core services..."
sudo systemctl daemon-reload
sudo systemctl enable --now "${CORE_SERVICES[@]}"

if [[ "${START_EXTRA_SERVICES:-0}" == "1" ]]; then
  echo "[aster] starting extra services..."
  sudo systemctl enable --now "${EXTRA_SERVICES[@]}"
fi

echo "[aster] creating tmux layout..."
tmux new-session -d -s "${SESSION}" -n logs
tmux send-keys -t "${SESSION}:logs.0" "${LOG_LIVE}" C-m

tmux split-window -h -t "${SESSION}:logs"
tmux send-keys -t "${SESSION}:logs.1" "${LOG_TAPE}" C-m

tmux split-window -v -t "${SESSION}:logs.0"
tmux send-keys -t "${SESSION}:logs.2" "${LOG_WHALE}" C-m

tmux split-window -v -t "${SESSION}:logs.1"
tmux send-keys -t "${SESSION}:logs.3" "watch -n 2 'systemctl --no-pager --full status ${CORE_SERVICES[*]} | sed -n \"1,80p\"'" C-m

tmux select-layout -t "${SESSION}:logs" tiled >/dev/null

tmux new-window -t "${SESSION}" -n extra
tmux send-keys -t "${SESSION}:extra.0" "${LOG_EXTRA}" C-m

tmux new-window -t "${SESSION}" -n shell
tmux send-keys -t "${SESSION}:shell.0" "echo 'aster stack ready'" C-m

echo "[aster] stack restarted. attaching tmux session: ${SESSION}"
exec tmux attach -t "${SESSION}"
