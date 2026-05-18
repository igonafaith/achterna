BINARY     := achterna
BUILD_DIR  := ./bin
CMD        := ./cmd/achterna
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all build test lint clean run tidy deploy

all: build

## Build the binary
build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD)
	@echo "Built $(BUILD_DIR)/$(BINARY)"

## Run all tests
test:
	go test ./... -race -cover -timeout 60s

## Run tests with verbose output
test-v:
	go test ./... -race -v -timeout 60s

## Lint using golangci-lint (must be installed)
lint:
	golangci-lint run ./...

## Tidy module dependencies
tidy:
	go mod tidy

## Run locally with default config
run: build
	$(BUILD_DIR)/$(BINARY) -config config.yaml

## Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)

## Install binary to /opt/achterna (requires sudo)
deploy: build
	install -Dm755 $(BUILD_DIR)/$(BINARY) /opt/achterna/$(BINARY)
	install -Dm644 config.yaml /opt/achterna/config.yaml
	install -Dm644 achterna.service /etc/systemd/system/achterna.service
	systemctl daemon-reload
	systemctl enable achterna
	@echo "Deployed. Run: systemctl start achterna"

## Reload config on running instance
reload:
	curl -s -X POST http://localhost:9090/config/reload && echo "Config reloaded"

## Show circuit breaker state
circuit-state:
	curl -s http://localhost:9090/circuit/state | python3 -m json.tool

## Show metrics
metrics:
	curl -s http://localhost:9090/metrics | python3 -m json.tool

## Health check
health:
	curl -s http://localhost:9090/healthz | python3 -m json.tool
