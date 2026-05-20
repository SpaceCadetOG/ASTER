# Phase 08: Persistent Services On GCP VMs

This runbook replaces manually started ASTER processes on the current GCP VMs with supervised `systemd` services.

Scope:

- management VM services:
  - `aster-long`
  - `aster-short`
  - `aster-tape`
  - `aster-whale`
  - `aster-liqs`
  - `aster-oflow`
- execution VM service:
  - `aster-live`

The units assume:

- repo checkout at `/opt/aster/repo`
- built binaries in `/opt/aster/bin`
- env files in `/opt/aster/env`
- logs/state/out directories already exist under `/opt/aster`

## Design Notes

- The units run the built binaries directly from `/opt/aster/bin`.
- `systemd` owns process supervision and restart behavior.
- service stdout/stderr goes to journald
- ASTER runtime-owned files still use `/opt/aster/logs`, `/opt/aster/state`, and `/opt/aster/out` through env

`aster-live.service` intentionally runs `/opt/aster/bin/live` directly instead of `scripts/run_live_logged.sh`.

Why:

- the logged launcher is useful for manual host sessions
- it also adds `pkill`, file locking, and a logging pipe that are better handled by `systemd`
- the paper-safe behavior is already controlled directly by `/opt/aster/env/live.env`

## Common Preflight

Run these checks on each VM before enabling services:

```bash
cd /opt/aster/repo
ls systemd/gcp
ls systemd/env
ls /opt/aster/bin
```

Expected users:

- current GCP host user: `ogbebortrading`

If a future host uses a different runtime user, update the `User=` line in the unit files before copying them into `/etc/systemd/system/`.

## Management VM Steps

Host:

- `aster-mgmt-vm`
- `10.10.10.2`

### 1. Stop manually launched processes

If the services were launched by hand with `nohup`, shell backgrounds, or interactive sessions, stop them before enabling `systemd`:

```bash
pkill -f '/opt/aster/bin/long' || true
pkill -f '/opt/aster/bin/short' || true
pkill -f '/opt/aster/bin/tape' || true
pkill -f '/opt/aster/bin/whale' || true
pkill -f '/opt/aster/bin/liqs' || true
pkill -f '/opt/aster/bin/oflow' || true
pkill -f 'go run ./cmd/long' || true
pkill -f 'go run ./cmd/short' || true
pkill -f 'go run ./cmd/tape' || true
pkill -f 'go run ./cmd/whale' || true
pkill -f 'go run ./cmd/liqs' || true
pkill -f 'go run ./cmd/oflow' || true
```

### 2. Create env files from examples

```bash
mkdir -p /opt/aster/env
cp /opt/aster/repo/systemd/env/long.env.example  /opt/aster/env/long.env
cp /opt/aster/repo/systemd/env/short.env.example /opt/aster/env/short.env
cp /opt/aster/repo/systemd/env/tape.env.example  /opt/aster/env/tape.env
cp /opt/aster/repo/systemd/env/whale.env.example /opt/aster/env/whale.env
cp /opt/aster/repo/systemd/env/liqs.env.example  /opt/aster/env/liqs.env
cp /opt/aster/repo/systemd/env/oflow.env.example /opt/aster/env/oflow.env
```

Current validated management values:

- `long.env`
  - `PORT=8080`
- `short.env`
  - `PORT=8081`
- `tape.env`
  - `TAPE_SYMBOLS=btcusdt,ethusdt,solusdt`
  - `TAPE_MIN_USD=100`
- `whale.env`
  - `WHALE_SYMBOLS=btcusdt,ethusdt,solusdt`
  - `WHALE_MIN_USD=5000`
  - `WHALE_TIER1_USD=10000`
  - `WHALE_TIER2_USD=50000`
  - `WHALE_TIER3_USD=150000`
  - `WHALE_WINDOW_SEC=30`
  - `WHALE_BURST_COUNT=5`
  - `WHALE_IMBALANCE_PCT=65`
- `liqs.env`
  - `LIQ_SYMBOLS=`
  - `LIQ_MIN_USD=0`
  - `LIQ_WINDOW_SEC=60`
  - `LIQ_TIER1_USD=20000`
  - `LIQ_TIER2_USD=100000`
  - `LIQ_TIER3_USD=500000`
  - `LIQ_PRINT_RAW=0`
- `oflow.env`
  - `OFLOW_SYMBOLS=btcusdt,ethusdt,solusdt`
  - `OFLOW_WINDOW_SEC=20`
  - `OFLOW_LARGE_USD=5000`
  - `OFLOW_PRINT_EVERY_MS=1000`
  - `OFLOW_TOP_N=5`

### 3. Install unit files

```bash
sudo cp /opt/aster/repo/systemd/gcp/aster-long.service  /etc/systemd/system/
sudo cp /opt/aster/repo/systemd/gcp/aster-short.service /etc/systemd/system/
sudo cp /opt/aster/repo/systemd/gcp/aster-tape.service  /etc/systemd/system/
sudo cp /opt/aster/repo/systemd/gcp/aster-whale.service /etc/systemd/system/
sudo cp /opt/aster/repo/systemd/gcp/aster-liqs.service  /etc/systemd/system/
sudo cp /opt/aster/repo/systemd/gcp/aster-oflow.service /etc/systemd/system/
```

### 4. Reload and enable

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now aster-long aster-short aster-tape aster-whale aster-liqs aster-oflow
```

### 5. Status checks

```bash
sudo systemctl status aster-long --no-pager
sudo systemctl status aster-short --no-pager
sudo systemctl status aster-tape --no-pager
sudo systemctl status aster-whale --no-pager
sudo systemctl status aster-liqs --no-pager
sudo systemctl status aster-oflow --no-pager
```

### 6. Journald logs

```bash
journalctl -u aster-long -n 100 --no-pager
journalctl -u aster-short -n 100 --no-pager
journalctl -u aster-tape -n 100 --no-pager
journalctl -u aster-whale -n 100 --no-pager
journalctl -u aster-liqs -n 100 --no-pager
journalctl -u aster-oflow -n 100 --no-pager
```

Follow live logs:

```bash
journalctl -u aster-long -f
journalctl -u aster-short -f
```

### 7. Restart and stop commands

```bash
sudo systemctl restart aster-long aster-short aster-tape aster-whale aster-liqs aster-oflow
sudo systemctl stop aster-long aster-short aster-tape aster-whale aster-liqs aster-oflow
sudo systemctl start aster-long aster-short aster-tape aster-whale aster-liqs aster-oflow
```

### 8. Verify exactly one process per service

```bash
pgrep -af '/opt/aster/bin/long'
pgrep -af '/opt/aster/bin/short'
pgrep -af '/opt/aster/bin/tape'
pgrep -af '/opt/aster/bin/whale'
pgrep -af '/opt/aster/bin/liqs'
pgrep -af '/opt/aster/bin/oflow'
```

Each command should show exactly one running process owned by `systemd`.

### 9. Verify health endpoints

```bash
curl -sS http://127.0.0.1:8080/health
curl -sS http://127.0.0.1:8081/health
curl -sS http://127.0.0.1:8091/healthz
curl -sS http://127.0.0.1:8092/healthz
curl -sS http://127.0.0.1:8093/healthz
curl -sS http://127.0.0.1:8090/healthz
```

## Execution VM Steps

Host:

- `aster-exec-vm`
- `10.20.10.2`

### 1. Stop manually launched live processes

```bash
pkill -f '/opt/aster/bin/live' || true
pkill -f 'go run ./cmd/live' || true
pkill -f 'bash scripts/run_live_logged.sh' || true
```

### 2. Create or refresh the live env file

```bash
mkdir -p /opt/aster/env
cp /opt/aster/repo/systemd/env/live.env.example /opt/aster/env/live.env
```

Current validated paper-mode values:

- `LIVE_LAUNCH_MODE=paper`
- `LIVE_DRY_RUN=1`
- `LIVE_ENABLE_LIVE_TRADING=0`
- `ASTER_LOG_DIR=/opt/aster/logs`
- `LIVE_STATE_DIR=/opt/aster/state`
- `LIVE_SHOW_ACCOUNT=0`
- `LIVE_USERDATA_STREAM_ENABLE=0`
- Telegram commands/reports disabled for this smoke/runtime phase

### 3. Install the live unit

```bash
sudo cp /opt/aster/repo/systemd/gcp/aster-live.service /etc/systemd/system/
```

### 4. Reload and enable

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now aster-live
```

### 5. Status checks

```bash
sudo systemctl status aster-live --no-pager
```

### 6. Journald logs

```bash
journalctl -u aster-live -n 200 --no-pager
journalctl -u aster-live -f
```

### 7. Restart and stop commands

```bash
sudo systemctl restart aster-live
sudo systemctl stop aster-live
sudo systemctl start aster-live
```

### 8. Verify exactly one live process

```bash
pgrep -af '/opt/aster/bin/live'
```

### 9. Verify health and status

```bash
curl -sS http://127.0.0.1:8787/healthz
curl -sS http://127.0.0.1:8787/api/status
```

Expected smoke indicators:

- `dry_run=true`
- `live_enabled=false`
- paper summary present
- scanner fields populated

## Reboot Verification Checklist

Run on both VMs after a reboot:

```bash
sudo reboot
```

After reconnect:

```bash
systemctl is-active aster-long aster-short aster-tape aster-whale aster-liqs aster-oflow
systemctl is-active aster-live
```

Then verify:

```bash
curl -sS http://127.0.0.1:8080/health
curl -sS http://127.0.0.1:8081/health
curl -sS http://127.0.0.1:8787/healthz
```

And confirm single processes:

```bash
pgrep -af '/opt/aster/bin/long'
pgrep -af '/opt/aster/bin/short'
pgrep -af '/opt/aster/bin/live'
```

If any service fails:

```bash
sudo systemctl status <unit> --no-pager
journalctl -u <unit> -n 200 --no-pager
```

## Notes

- The current units are intentionally simple and host-oriented.
- They do not restore the removed tmux/autoupdate Pi architecture.
- They are suitable for current GCP dev VMs and can be reused later on production VMs with different env contents.
