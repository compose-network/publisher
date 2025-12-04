default: run

.PHONY: build
build:
	go build -o cmd/bin/publisher cmd/main.go

.PHONY: test
test:
	go test -v ./...

.PHONY: lint
lint:
	golangci-lint run