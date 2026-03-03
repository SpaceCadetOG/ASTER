#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${ASTER_WORKDIR:-/opt/aster}"
cd "${ROOT_DIR}"

echo "[maint-eod] $(date -Is) start"
git fetch --all --prune || true
git pull --ff-only || true
scripts/reconcile_on_boot.sh || true
echo "[maint-eod] $(date -Is) done"
