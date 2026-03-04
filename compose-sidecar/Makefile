.PHONY: build run test lint clean tidy

# Binary name
BINARY=sidecar
BUILD_DIR=bin

# Build flags
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.Version=$(VERSION)"

build:
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/sidecar

run: build
	./$(BUILD_DIR)/$(BINARY) --config configs/config.yaml

run-dev: build
	SIDECAR_LOG_LEVEL=debug SIDECAR_LOG_FORMAT=pretty ./$(BUILD_DIR)/$(BINARY) --config configs/config.yaml

test:
	go test -race -v ./...

lint:
	golangci-lint run --timeout=5m

tidy:
	go mod tidy

clean:
	rm -rf $(BUILD_DIR)

# Development helpers
.PHONY: fmt vet

fmt:
	go fmt ./...

vet:
	go vet ./...
