#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="${REPO_DIR:-$(pwd)}"
BIN_DIR="${BIN_DIR:-/opt/aster/bin}"
SERVICES=(
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
chmod +x "${BIN_DIR}/tape" "${BIN_DIR}/whale" "${BIN_DIR}/live-lite"

echo "[deploy] installing unit files"
sudo cp systemd/aster-tape.service /etc/systemd/system/
sudo cp systemd/aster-whale.service /etc/systemd/system/
sudo cp systemd/aster-live-lite.service /etc/systemd/system/
sudo systemctl daemon-reload

echo "[deploy] restarting services"
for svc in "${SERVICES[@]}"; do
  sudo systemctl enable --now "${svc}" || true
  sudo systemctl restart "${svc}" || true
done

echo "[deploy] status"
for svc in "${SERVICES[@]}"; do
  systemctl --no-pager --full status "${svc}" || true
done
