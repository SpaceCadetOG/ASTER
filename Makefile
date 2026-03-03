# go-machine/Makefile
.PHONY: run dev devq build test clean deploy fmt backtest \
	exec-balance exec-account exec-open-orders exec-position exec-place exec-cancel exec-cancel-all exec-status

ASTER_CONFIG ?= $(CURDIR)/.aster.yaml
EXEC_BASE_URL ?= https://fapi.asterdex.com
EXEC_SYMBOL ?= BTCUSDT

smoke:
	./scripts/smoke.sh

# ---- Local runs ----
run:
	go run ./cmd/long/main.go
	go run ./cmd/short/main.go

# Auto-reload dev server (restarts on file changes, shows child logs)
dev:
	go run ./internal/dev/watch.go -- go run ./cmd/long/main.go
	go run ./internal/dev/watch.go -- go run ./cmd/short/main.go

# Quieter auto-reload (only restart notices; suppress child stdout)
devq:
	go run ./internal/dev/watch.go -- -q go run ./cmd/long/main.go
	go run ./internal/dev/watch.go -- -q go run ./cmd/short/main.go

# ---- Build/test ----
build:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
	go build -trimpath -ldflags "-s -w" -o go-machine-long ./cmd/long/main.go
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
	go build -trimpath -ldflags "-s -w" -o go-machine-short ./cmd/short/main.go

test:
	go test ./... -v

fmt:
	go fmt ./...

clean:
	rm -f go-machine-long go-machine-short
	rm -rf bin

# ---- Backtester ----
BT_SYMBOLS ?= BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,ASTERUSDT,HYPEUSDT
BT_STRATEGY ?= router
BT_TF ?= 1m
backtest:
	BT_SYMBOLS=$(BT_SYMBOLS) \
	BT_STRATEGY=$(BT_STRATEGY) \
	BT_TF=$(BT_TF) \
	go run ./cmd/backtest

# ---- Manual deploy to VM (optional; CI/CD will do this) ----
# Requires: export SSH_USER=ubuntu ; export SSH_HOST=YOUR_VM_IP
deploy: build
	scp go-machine-long $(SSH_USER)@$(SSH_HOST):/opt/go-machine/go-machine-long
	scp go-machine-short $(SSH_USER)@$(SSH_HOST):/opt/go-machine/go-machine-short
	ssh $(SSH_USER)@$(SSH_HOST) "\
		sudo systemctl restart traderbot && \
		sudo systemctl restart traderbot-short && \
		systemctl --no-pager --full status traderbot && \
		systemctl --no-pager --full status traderbot-short \
	"

# ---- Exec shortcuts ----
exec-balance:
	ASTER_CONFIG=$(ASTER_CONFIG) EXEC_BASE_URL=$(EXEC_BASE_URL) EXEC_ACTION=balance go run ./cmd/exec

exec-account:
	ASTER_CONFIG=$(ASTER_CONFIG) EXEC_BASE_URL=$(EXEC_BASE_URL) EXEC_ACTION=account EXEC_SYMBOL=$(EXEC_SYMBOL) go run ./cmd/exec

exec-open-orders:
	ASTER_CONFIG=$(ASTER_CONFIG) EXEC_BASE_URL=$(EXEC_BASE_URL) EXEC_ACTION=open_orders EXEC_SYMBOL=$(EXEC_SYMBOL) go run ./cmd/exec

exec-position:
	ASTER_CONFIG=$(ASTER_CONFIG) EXEC_BASE_URL=$(EXEC_BASE_URL) EXEC_ACTION=position EXEC_SYMBOL=$(EXEC_SYMBOL) go run ./cmd/exec

# Optional vars: SIDE, KIND, USD, DRY_RUN, EXEC_AT, EXEC_OFFSET_BPS, EXEC_OFFSET_PCT, EXEC_PRICE, EXEC_QTY, EXEC_DEBUG
SIDE ?= BUY
KIND ?= LIMIT
USD ?= 50
DRY_RUN ?= 1
EXEC_DEBUG ?= 1
exec-place:
	ASTER_CONFIG=$(ASTER_CONFIG) EXEC_BASE_URL=$(EXEC_BASE_URL) EXEC_ACTION=place EXEC_SYMBOL=$(EXEC_SYMBOL) EXEC_SIDE=$(SIDE) EXEC_KIND=$(KIND) EXEC_USD=$(USD) DRY_RUN=$(DRY_RUN) EXEC_DEBUG=$(EXEC_DEBUG) EXEC_AT=$(EXEC_AT) EXEC_OFFSET_BPS=$(EXEC_OFFSET_BPS) EXEC_OFFSET_PCT=$(EXEC_OFFSET_PCT) EXEC_PRICE=$(EXEC_PRICE) EXEC_QTY=$(EXEC_QTY) go run ./cmd/exec

# Required var: ORDER_ID
ORDER_ID ?=
exec-cancel:
	@test -n "$(ORDER_ID)" || (echo "ORDER_ID is required. Example: make exec-cancel EXEC_SYMBOL=ETHUSDT ORDER_ID=123"; exit 2)
	ASTER_CONFIG=$(ASTER_CONFIG) EXEC_BASE_URL=$(EXEC_BASE_URL) EXEC_ACTION=cancel EXEC_SYMBOL=$(EXEC_SYMBOL) EXEC_ORDER_ID=$(ORDER_ID) DRY_RUN=0 go run ./cmd/exec

exec-cancel-all:
	ASTER_CONFIG=$(ASTER_CONFIG) EXEC_BASE_URL=$(EXEC_BASE_URL) EXEC_ACTION=cancel_all EXEC_SYMBOL=$(EXEC_SYMBOL) DRY_RUN=0 go run ./cmd/exec

exec-status:
	@test -n "$(ORDER_ID)" || (echo "ORDER_ID is required. Example: make exec-status EXEC_SYMBOL=ETHUSDT ORDER_ID=123"; exit 2)
	ASTER_CONFIG=$(ASTER_CONFIG) EXEC_BASE_URL=$(EXEC_BASE_URL) EXEC_ACTION=status EXEC_SYMBOL=$(EXEC_SYMBOL) EXEC_ORDER_ID=$(ORDER_ID) go run ./cmd/exec
