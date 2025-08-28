.PHONY: all build clean test coverage lint proto run docker help

# Variables
BINARY_NAME=rollup-shared-publisher
DOCKER_IMAGE=rollup-shared-publisher
VERSION=$(shell git describe --tags --always --dirty)
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(shell git rev-parse HEAD)
LDFLAGS=-ldflags "-X main.Version=${VERSION} \
                 -X main.BuildTime=${BUILD_TIME} \
                 -X main.GitCommit=${GIT_COMMIT}"

# Default target
all: clean lint test build

build: ## Build the application binary
	@echo "Building..."
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) shared-publisher-leader-app/main.go shared-publisher-leader-app/app.go shared-publisher-leader-app/version.go

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
	@echo "Generating protobuf files..."
	cd proto && buf generate
	@echo "Protobuf files generated at proto/rollup/v1/"

proto-clean: ## Clean generated protobuf files
	@echo "Cleaning generated protobuf files..."
	find proto -name "*.pb.go" -delete

proto-lint: ## Lint protobuf files
	@echo "Linting protobuf files..."
	cd proto && make proto-lint

run: build ## Run the application
	@echo "Running shared publisher..."
	./bin/$(BINARY_NAME) --config shared-publisher-leader-app/configs/config.yaml

run-dev: build ## Run in development mode
	@echo "Running in development mode..."
	./bin/$(BINARY_NAME) --config shared-publisher-leader-app/configs/config.yaml --log-pretty --log-level debug

docker: ## Build the Docker image
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(VERSION) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		-f Dockerfile .
	docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest

docker-compose: ## Run with docker-compose
	@echo "Running with docker-compose..."
	docker-compose up --build

docker-compose-down: ## Stop docker-compose
	@echo "Stopping docker-compose..."
	docker-compose down

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
