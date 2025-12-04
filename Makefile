CMD_MAIN=cmd/main.go

default: run

.PHONY: build
build:
	go build -o cmd/bin/publisher $(CMD_MAIN)

.PHONY: test
test:
	go test -v $(CMD_MAIN)

.PHONY: lint
lint:
	golangci-lint run $(CMD_MAIN)