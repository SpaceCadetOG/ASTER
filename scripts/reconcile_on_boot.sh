#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

STATE_FILE="${LIVE_STATE_FILE:-out/live_exec_state.json}"
echo "[reconcile] state file: ${STATE_FILE}"
if [[ -f "${STATE_FILE}" ]]; then
  echo "[reconcile] found local live state"
else
  echo "[reconcile] no local state file"
fi

echo "[reconcile] tmux sessions:"
tmux ls 2>/dev/null || true
