#!/usr/bin/env bash
set -euo pipefail

SESSION="${TMUX_SESSION_NAME:-aster}"
WORKDIR="${ASTER_WORKDIR:-/opt/aster}"
ENVFILE="${ASTER_ENV_FILE:-/opt/aster/env/live-lite.env}"
LOG_TARGET="${ASTER_LOG_TARGET:-journalctl -u aster-live-lite -f}"

pane_cmd() {
  local cmd="$1"
  if [[ -f "${ENVFILE}" ]]; then
    printf 'cd %q && set -a && source %q && set +a && %s' "${WORKDIR}" "${ENVFILE}" "${cmd}"
  else
    printf 'cd %q && %s' "${WORKDIR}" "${cmd}"
  fi
}

run_cmd() {
  local bin="$1"
  local fallback="$2"
  if [[ -x "${bin}" ]]; then
    printf '%q' "${bin}"
  else
    printf '%s' "${fallback}"
  fi
}

if tmux has-session -t "${SESSION}" 2>/dev/null; then
  exec tmux attach -t "${SESSION}"
fi

tmux new-session -d -s "${SESSION}" -n monitor
tmux send-keys -t "${SESSION}:monitor.0" "$(pane_cmd "$(run_cmd /opt/aster/bin/live-lite 'go run ./cmd/live-lite')")" C-m

tmux split-window -h -t "${SESSION}:monitor"
tmux send-keys -t "${SESSION}:monitor.1" "$(pane_cmd "$(run_cmd /opt/aster/bin/tape 'go run ./cmd/tape')")" C-m

tmux split-window -v -t "${SESSION}:monitor.0"
tmux send-keys -t "${SESSION}:monitor.2" "$(pane_cmd "$(run_cmd /opt/aster/bin/whale 'go run ./cmd/whale')")" C-m

tmux split-window -v -t "${SESSION}:monitor.1"
tmux send-keys -t "${SESSION}:monitor.3" "${LOG_TARGET}" C-m

tmux select-layout -t "${SESSION}:monitor" tiled >/dev/null
exec tmux attach -t "${SESSION}"
