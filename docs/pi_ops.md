# Pi Runtime Ops

## 1) Build Audit (compile only)

```bash
GOCACHE=/tmp/go-build go test ./... -run TestDoesNotExist
```

## 2) Manual module checks (Mac or Pi)

```bash
go run ./cmd/tape
go run ./cmd/whale
go run ./cmd/long
go run ./cmd/short
go run ./cmd/backtest
```

Exec auth + account checks:

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com EXEC_ACTION=auth_check go run ./cmd/exec
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com EXEC_ACTION=balance go run ./cmd/exec
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com EXEC_ACTION=open_orders EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com EXEC_ACTION=position EXEC_SYMBOL=BTCUSDT go run ./cmd/exec
```

Live-lite dry run:

```bash
ASTER_CONFIG=~/.aster.yaml EXEC_BASE_URL=https://fapi.asterdex.com LIVE_DRY_RUN=1 LIVE_SHOW_ACCOUNT=1 LIVE_ACCOUNT_ASSETS=USDT,BTC,USDF go run ./cmd/live-lite
```

Expected behavior:
- IN-PLAY long/short sections print each loop.
- If nothing is eligible: `live-lite: no trade candidate`.
- Account snapshot shows `availableUSDT`, balances list, and active positions list.

## 3) Install systemd units on Pi

```bash
sudo cp systemd/aster-*.service /etc/systemd/system/
sudo cp systemd/env/*.env.example /opt/aster/env/ || true
sudo systemctl daemon-reload
sudo systemctl enable --now aster-tape aster-whale aster-live-lite
```

## 4) Deploy binaries + restart

```bash
scripts/deploy_pi.sh
```

## 5) tmux dashboard

```bash
scripts/tmux_aster.sh
```

## 6) One-step bot start (recommended)

```bash
scripts/start_bot.sh
```

Options:
- skip rebuild/redeploy and only restart + attach tmux:

```bash
SKIP_DEPLOY=1 scripts/start_bot.sh
```
- include extra services (`liqs/oflow/long/short`) in restart script:

```bash
START_EXTRA_SERVICES=1 scripts/start_bot.sh
```
