#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="${REPO_DIR:-$(pwd)}"
BIN_DIR="${BIN_DIR:-/opt/aster/bin}"

cd "${REPO_DIR}"

echo "[deploy] building binaries into ${BIN_DIR}"
sudo mkdir -p "${BIN_DIR}"
sudo chown -R "$(id -u):$(id -g)" /opt/aster || true

go build -o "${BIN_DIR}/tape" ./cmd/tape
go build -o "${BIN_DIR}/whale" ./cmd/whale
go build -o "${BIN_DIR}/live" ./cmd/live
go build -o "${BIN_DIR}/liqs" ./cmd/liqs
go build -o "${BIN_DIR}/oflow" ./cmd/oflow
go build -o "${BIN_DIR}/long" ./cmd/long
go build -o "${BIN_DIR}/short" ./cmd/short
chmod +x "${BIN_DIR}/tape" "${BIN_DIR}/whale" "${BIN_DIR}/live" "${BIN_DIR}/liqs" "${BIN_DIR}/oflow" "${BIN_DIR}/long" "${BIN_DIR}/short"

echo "[deploy] syncing env templates"
sudo mkdir -p /opt/aster/env
sudo cp systemd/env/live.env.example /opt/aster/env/live.env.example
sudo cp systemd/env/long.env.example /opt/aster/env/long.env.example
sudo cp systemd/env/short.env.example /opt/aster/env/short.env.example
sudo cp systemd/env/tape.env.example /opt/aster/env/tape.env.example
sudo cp systemd/env/whale.env.example /opt/aster/env/whale.env.example
sudo cp systemd/env/liqs.env.example /opt/aster/env/liqs.env.example
sudo cp systemd/env/oflow.env.example /opt/aster/env/oflow.env.example

cat <<EOF
[deploy] complete

The legacy Pi tmux/systemd orchestration layer has been removed from this repo.
This helper now only builds binaries and refreshes env examples under:
  ${BIN_DIR}
  /opt/aster/env

Start modules manually with commands such as:
  go run ./cmd/live
  go run ./cmd/long
  go run ./cmd/short
EOF
