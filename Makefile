.PHONY: all build clean test coverage lint proto run docker help

# Variables
BINARY_NAME=rollup-shared-publisher
DOCKER_IMAGE=rollup-shared-publisher
VERSION=$(shell git describe --tags --always --dirty)
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}"

# Default target
all: clean lint test build

build: ## Build the application binary
	@echo "Building..."
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) cmd/publisher/main.go

clean: ## Clean up build artifacts
	@echo "Cleaning..."
	rm -rf bin/ coverage.out coverage.html

test: ## Run tests
	@./scripts/test.sh

coverage: ## Run tests with coverage
	@./scripts/test.sh --coverage

lint: ## Run linters
	@echo "Running linters..."
	golangci-lint run --timeout=5m

proto: ## Generate protobuf files
	@./scripts/generate-proto.sh

run: build ## Run the application
	@./bin/$(BINARY_NAME)

docker: ## Build the Docker image
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(VERSION) -f Dockerfile .
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest

docker-run: ## Run the application using docker-compose
	docker-compose -f docker-compose.yml up

help:
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
