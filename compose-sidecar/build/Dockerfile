FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git make build-base

WORKDIR /workspace

COPY go.mod go.sum ./publisher/
COPY x ./publisher/x
COPY proto ./publisher/proto
COPY specs/compose/ ./specs/compose/
COPY compose-sdk/go.mod compose-sdk/go.sum ./compose-sdk/
COPY compose-sdk/ ./compose-sdk/

WORKDIR /workspace/sidecar
COPY compose-sidecar/go.mod compose-sidecar/go.sum ./

RUN sed -i 's|replace github.com/compose-network/publisher => ../|replace github.com/compose-network/publisher => /workspace/publisher|' go.mod && \
    sed -i 's|replace github.com/compose-network/specs/compose => ../specs/compose|replace github.com/compose-network/specs/compose => /workspace/specs/compose|' go.mod && \
    sed -i 's|replace github.com/compose-network/compose-sdk => ../compose-sdk|replace github.com/compose-network/compose-sdk => /workspace/compose-sdk|' go.mod

RUN go mod download

COPY compose-sidecar/ .
RUN CGO_ENABLED=0 GOOS=linux go build -o /workspace/sidecar/bin/sidecar ./cmd/sidecar

FROM alpine:3.19
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /workspace/sidecar/bin/sidecar /usr/local/bin/sidecar

RUN adduser -D -u 1000 sidecar
USER sidecar

WORKDIR /config
EXPOSE 8090

ENTRYPOINT ["/usr/local/bin/sidecar"]
