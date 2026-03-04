#!/usr/bin/env bash
set -euo pipefail

# One-step starter for Raspberry Pi:
# 1) Build/install latest binaries + units
# 2) Restart services cleanly
# 3) Launch tmux dashboard

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if [[ "${SKIP_DEPLOY:-0}" != "1" ]]; then
  scripts/deploy_pi.sh
fi

exec scripts/restart_aster_stack.sh
