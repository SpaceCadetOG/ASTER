# Systemd Units (Pi)

This folder contains systemd units for:
- `aster-tape.service`
- `aster-live-lite.service`
- `aster-whale.service`
- `aster-liqs.service`
- `aster-oflow.service`

## Install on Raspberry Pi

Build binaries into `/opt/aster/bin`:

```bash
sudo mkdir -p /opt/aster/bin /opt/aster/env
sudo chown -R "$USER:$USER" /opt/aster
go build -o /opt/aster/bin/whale ./cmd/whale
go build -o /opt/aster/bin/liqs  ./cmd/liqs
go build -o /opt/aster/bin/oflow ./cmd/oflow
go build -o /opt/aster/bin/tape  ./cmd/tape
go build -o /opt/aster/bin/live-lite ./cmd/live-lite
chmod +x /opt/aster/bin/whale /opt/aster/bin/liqs /opt/aster/bin/oflow /opt/aster/bin/tape /opt/aster/bin/live-lite
```

Copy env templates (optional), then edit:

```bash
cp systemd/env/whale.env.example /opt/aster/env/whale.env
cp systemd/env/liqs.env.example  /opt/aster/env/liqs.env
cp systemd/env/oflow.env.example /opt/aster/env/oflow.env
cp systemd/env/tape.env.example  /opt/aster/env/tape.env
cp systemd/env/live-lite.env.example /opt/aster/env/live-lite.env
```

Install units:

```bash
sudo cp systemd/aster-whale.service /etc/systemd/system/
sudo cp systemd/aster-liqs.service  /etc/systemd/system/
sudo cp systemd/aster-oflow.service /etc/systemd/system/
sudo cp systemd/aster-tape.service  /etc/systemd/system/
sudo cp systemd/aster-live-lite.service  /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now aster-whale aster-liqs aster-oflow aster-tape aster-live-lite
```

Check status/logs:

```bash
systemctl --no-pager --full status aster-whale aster-liqs aster-oflow aster-tape aster-live-lite
journalctl -u aster-whale -f
journalctl -u aster-liqs -f
journalctl -u aster-oflow -f
journalctl -u aster-tape -f
journalctl -u aster-live-lite -f
```
