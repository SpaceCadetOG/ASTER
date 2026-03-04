#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="${REPO_DIR:-$(pwd)}"
BIN_DIR="${BIN_DIR:-/opt/aster/bin}"
SERVICES=(
  aster-modules-tmux
)
LEGACY_SERVICES=(
  aster-tape
  aster-whale
  aster-live-lite
)

cd "${REPO_DIR}"

echo "[deploy] building binaries into ${BIN_DIR}"
sudo mkdir -p "${BIN_DIR}"
sudo chown -R "$(id -u):$(id -g)" /opt/aster || true

go build -o "${BIN_DIR}/tape" ./cmd/tape
go build -o "${BIN_DIR}/whale" ./cmd/whale
go build -o "${BIN_DIR}/live-lite" ./cmd/live-lite
go build -o "${BIN_DIR}/liqs" ./cmd/liqs
go build -o "${BIN_DIR}/oflow" ./cmd/oflow
go build -o "${BIN_DIR}/long" ./cmd/long
go build -o "${BIN_DIR}/short" ./cmd/short
chmod +x "${BIN_DIR}/tape" "${BIN_DIR}/whale" "${BIN_DIR}/live-lite" "${BIN_DIR}/liqs" "${BIN_DIR}/oflow" "${BIN_DIR}/long" "${BIN_DIR}/short"

echo "[deploy] installing unit files"
sudo cp systemd/aster-tape.service /etc/systemd/system/
sudo cp systemd/aster-whale.service /etc/systemd/system/
sudo cp systemd/aster-live-lite.service /etc/systemd/system/
sudo cp systemd/aster-modules-tmux.service /etc/systemd/system/
sudo cp systemd/aster-autoupdate.service /etc/systemd/system/
sudo cp systemd/aster-autoupdate.timer /etc/systemd/system/
sudo mkdir -p /opt/aster/scripts
sudo cp scripts/tmux_module_runner.sh /opt/aster/scripts/
sudo cp scripts/start_tmux_modules.sh /opt/aster/scripts/
sudo cp scripts/reconcile_on_boot.sh /opt/aster/scripts/
sudo cp scripts/maintenance_midnight.sh /opt/aster/scripts/
sudo cp scripts/maintenance_eod.sh /opt/aster/scripts/
sudo cp scripts/auto_update_aster.sh /opt/aster/scripts/
sudo chmod +x /opt/aster/scripts/tmux_module_runner.sh /opt/aster/scripts/start_tmux_modules.sh /opt/aster/scripts/reconcile_on_boot.sh /opt/aster/scripts/maintenance_midnight.sh /opt/aster/scripts/maintenance_eod.sh /opt/aster/scripts/auto_update_aster.sh
sudo systemctl daemon-reload

echo "[deploy] stopping legacy standalone services"
for svc in "${LEGACY_SERVICES[@]}"; do
  sudo systemctl disable --now "${svc}" || true
done

echo "[deploy] restarting services"
for svc in "${SERVICES[@]}"; do
  sudo systemctl enable --now "${svc}" || true
  sudo systemctl restart "${svc}" || true
done

sudo systemctl enable --now aster-autoupdate.timer || true

echo "[deploy] status"
for svc in "${SERVICES[@]}" "${LEGACY_SERVICES[@]}"; do
  systemctl --no-pager --full status "${svc}" || true
done
