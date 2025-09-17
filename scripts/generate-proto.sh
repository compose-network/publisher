#!/bin/bash

set -e

PROTO_DIR="api/proto"
OUT_DIR="internal/proto"

# Install protoc-gen-go if not present
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Installing protoc-gen-go..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
fi

# Install protoc-gen-go-grpc if not present
if ! command -v protoc-gen-go-grpc &> /dev/null; then
    echo "Installing protoc-gen-go-grpc..."
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

# Generate Go code from proto files
echo "Generating Go code from proto files..."
protoc \
    --go_out=${OUT_DIR} \
    --go_opt=paths=source_relative \
    --go-grpc_out=${OUT_DIR} \
    --go-grpc_opt=paths=source_relative \
    -I ${PROTO_DIR} \
    ${PROTO_DIR}/*.proto

echo "Proto generation complete!"
