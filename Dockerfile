# Build stage
FROM golang:1.25-alpine@sha256:b6ed3fd0452c0e9bcdef5597f29cc1418f61672e9d3a2f55bf02e7222c014abd AS builder

# Install dependencies
RUN apk add --no-cache git make gcc musl-dev

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with version info
ARG VERSION=unknown
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}" \
    -o publisher \
    publisher-leader-app/main.go publisher-leader-app/app.go publisher-leader-app/version.go

# Runtime stage
FROM alpine:3.22@sha256:4bcff63911fcb4448bd4fdacec207030997caf25e9bea4045fa6c8c44de311d1

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 publisher && \
    adduser -u 1000 -G publisher -D publisher

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/publisher /app/
COPY --from=builder /build/publisher-leader-app/configs/config.yaml /app/configs/

# Create directory for logs
RUN mkdir -p /app/logs && chown -R publisher:publisher /app

# Switch to non-root user
USER publisher

# Expose ports
EXPOSE 8080 8081

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8081/health || exit 1

ENTRYPOINT ["/app/publisher"]
CMD ["--config", "/app/configs/config.yaml"]
