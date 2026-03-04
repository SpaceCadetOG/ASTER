#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="${REPO_DIR:-/opt/aster}"
BRANCH="${BRANCH:-main}"
REMOTE="${REMOTE:-origin}"
BIN_DIR="${BIN_DIR:-/opt/aster/bin}"
LOG_DIR="${LOG_DIR:-/opt/aster/out}"
LOCK_FILE="${LOCK_FILE:-/tmp/aster-autoupdate.lock}"

mkdir -p "${LOG_DIR}"
LOG_FILE="${LOG_DIR}/autoupdate.log"

exec 9>"${LOCK_FILE}"
if ! flock -n 9; then
  exit 0
fi

ts() { date +"%Y-%m-%d %H:%M:%S %Z"; }
log() { echo "[$(ts)] $*" >> "${LOG_FILE}"; }

cd "${REPO_DIR}"

if [[ -n "$(git status --porcelain)" ]]; then
  log "skip: working tree dirty"
  exit 0
fi

git fetch --quiet "${REMOTE}" "${BRANCH}"
local_sha="$(git rev-parse HEAD)"
remote_sha="$(git rev-parse "${REMOTE}/${BRANCH}")"

if [[ "${local_sha}" == "${remote_sha}" ]]; then
  exit 0
fi

log "update: ${local_sha:0:7} -> ${remote_sha:0:7}"
git pull --ff-only --quiet "${REMOTE}" "${BRANCH}"

mkdir -p "${BIN_DIR}"
go build -o "${BIN_DIR}/live-lite" ./cmd/live-lite
go build -o "${BIN_DIR}/tape" ./cmd/tape
go build -o "${BIN_DIR}/whale" ./cmd/whale
chmod +x "${BIN_DIR}/live-lite" "${BIN_DIR}/tape" "${BIN_DIR}/whale"

sudo systemctl restart aster-modules-tmux
log "restart: aster-modules-tmux complete"
