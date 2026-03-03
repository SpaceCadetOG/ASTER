#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <session> <command...>" >&2
  exit 1
fi

SESSION="$1"
shift
CMD="$*"
WORKDIR="${ASTER_WORKDIR:-/opt/aster}"
ENVFILE="${ASTER_ENV_FILE:-}"

if tmux has-session -t "${SESSION}" 2>/dev/null; then
  exit 0
fi

if [[ -n "${ENVFILE}" && -f "${ENVFILE}" ]]; then
  RUN_CMD="cd '${WORKDIR}' && set -a && source '${ENVFILE}' && set +a && while true; do ${CMD}; rc=$?; echo \"[${SESSION}] exited rc=${rc} at $(date)\"; sleep 2; done"
else
  RUN_CMD="cd '${WORKDIR}' && while true; do ${CMD}; rc=$?; echo \"[${SESSION}] exited rc=${rc} at $(date)\"; sleep 2; done"
fi

tmux new-session -d -s "${SESSION}" "bash -lc \"${RUN_CMD}\""
