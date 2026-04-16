#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

WORKDIR="${ASTER_WORKDIR:-/opt/aster}"
SCRIPTDIR="${ASTER_SCRIPT_DIR:-/opt/aster/scripts}"
BINDIR="${ASTER_BIN_DIR:-/opt/aster/bin}"

start_one() {
  local session="$1"
  local envfile="$2"
  local bin="$3"
  local fallback="$4"
  local cmd="${fallback}"
  if [[ -x "${bin}" ]]; then
    cmd="${bin}"
  fi
  ASTER_WORKDIR="${WORKDIR}" ASTER_ENV_FILE="${envfile}" "${SCRIPTDIR}/tmux_module_runner.sh" "${session}" "${cmd}"
}

start_one "aster-live-lite" "/opt/aster/env/live-lite.env" "${BINDIR}/live-lite" "go run ./cmd/live-lite"
start_one "aster-long" "/opt/aster/env/long.env" "${BINDIR}/long" "go run ./cmd/long"
start_one "aster-short" "/opt/aster/env/short.env" "${BINDIR}/short" "go run ./cmd/short"
start_one "aster-tape" "/opt/aster/env/tape.env" "${BINDIR}/tape" "go run ./cmd/tape"
start_one "aster-whale" "/opt/aster/env/whale.env" "${BINDIR}/whale" "go run ./cmd/whale"
start_one "aster-liqs" "/opt/aster/env/liqs.env" "${BINDIR}/liqs" "go run ./cmd/liqs"
start_one "aster-oflow" "/opt/aster/env/oflow.env" "${BINDIR}/oflow" "go run ./cmd/oflow"

echo "tmux modules ready: aster-live-lite, aster-long, aster-short, aster-tape, aster-whale, aster-liqs, aster-oflow"
