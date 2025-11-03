# go-machine/Makefile
.PHONY: run dev devq build test clean deploy fmt backtest

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
backtest:
	mkdir -p bin
	go build -o bin/backtest ./cmd/backtest

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