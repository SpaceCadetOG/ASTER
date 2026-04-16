#!/usr/bin/env bash
set -euo pipefail

SESSION="${TMUX_SESSION_NAME:-aster}"
WORKDIR="${ASTER_WORKDIR:-/opt/aster}"
SCRIPTDIR="${ASTER_SCRIPT_DIR:-/opt/aster/scripts}"
BINDIR="${ASTER_BIN_DIR:-/opt/aster/bin}"

pane_cmd() {
  local envfile="$1"
  local cmd="$2"
  if [[ -f "${envfile}" ]]; then
    printf 'cd %q && set -a && source %q && set +a && %s' "${WORKDIR}" "${envfile}" "${cmd}"
  else
    printf 'cd %q && %s' "${WORKDIR}" "${cmd}"
  fi
}

bin_or_fallback() {
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

# Tab 1: git action
tmux new-session -d -s "${SESSION}" -n gitaction
tmux send-keys -t "${SESSION}:gitaction.0" "cd ${WORKDIR} && git status -sb && echo \"git actions tab ready\" && exec bash" C-m

# Tab 2: live-lite
tmux new-window -t "${SESSION}" -n live-lite
tmux send-keys -t "${SESSION}:live-lite.0" "$(pane_cmd "/opt/aster/env/live-lite.env" "$(bin_or_fallback "${SCRIPTDIR}/run_live_lite_logged.sh" "bash scripts/run_live_lite_logged.sh")")" C-m

# Tab 3: long/short split
tmux new-window -t "${SESSION}" -n scanners
tmux send-keys -t "${SESSION}:scanners.0" "$(pane_cmd "/opt/aster/env/long.env" "$(bin_or_fallback "${SCRIPTDIR}/run_long_logged.sh" "bash scripts/run_long_logged.sh")")" C-m
tmux split-window -h -t "${SESSION}:scanners.0"
tmux send-keys -t "${SESSION}:scanners.1" "$(pane_cmd "/opt/aster/env/short.env" "$(bin_or_fallback "${SCRIPTDIR}/run_short_logged.sh" "bash scripts/run_short_logged.sh")")" C-m
tmux select-layout -t "${SESSION}:scanners" even-horizontal >/dev/null

# Tab 4: cross split tape/whale/liqs/oflow
tmux new-window -t "${SESSION}" -n flow
tmux send-keys -t "${SESSION}:flow.0" "$(pane_cmd "/opt/aster/env/tape.env" "$(bin_or_fallback "${BINDIR}/tape" "go run ./cmd/tape")")" C-m
tmux split-window -h -t "${SESSION}:flow.0"
tmux send-keys -t "${SESSION}:flow.1" "$(pane_cmd "/opt/aster/env/whale.env" "$(bin_or_fallback "${BINDIR}/whale" "go run ./cmd/whale")")" C-m
tmux split-window -v -t "${SESSION}:flow.0"
tmux send-keys -t "${SESSION}:flow.2" "$(pane_cmd "/opt/aster/env/liqs.env" "$(bin_or_fallback "${BINDIR}/liqs" "go run ./cmd/liqs")")" C-m
tmux split-window -v -t "${SESSION}:flow.1"
tmux send-keys -t "${SESSION}:flow.3" "$(pane_cmd "/opt/aster/env/oflow.env" "$(bin_or_fallback "${BINDIR}/oflow" "go run ./cmd/oflow")")" C-m
tmux select-layout -t "${SESSION}:flow" tiled >/dev/null

tmux select-window -t "${SESSION}:live-lite"
exec tmux attach -t "${SESSION}"
