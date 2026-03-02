# Systemd Units (Pi)

This folder contains systemd units for:
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
chmod +x /opt/aster/bin/whale /opt/aster/bin/liqs /opt/aster/bin/oflow
```

Copy env templates (optional), then edit:

```bash
cp systemd/env/whale.env.example /opt/aster/env/whale.env
cp systemd/env/liqs.env.example  /opt/aster/env/liqs.env
cp systemd/env/oflow.env.example /opt/aster/env/oflow.env
```

Install units:

```bash
sudo cp systemd/aster-whale.service /etc/systemd/system/
sudo cp systemd/aster-liqs.service  /etc/systemd/system/
sudo cp systemd/aster-oflow.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now aster-whale aster-liqs aster-oflow
```

Check status/logs:

```bash
systemctl --no-pager --full status aster-whale aster-liqs aster-oflow
journalctl -u aster-whale -f
journalctl -u aster-liqs -f
journalctl -u aster-oflow -f
```
