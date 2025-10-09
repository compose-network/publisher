# Build stage
FROM golang:1.24-alpine AS builder

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
    -ldflags="-w -s -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}" \
    -o rollup-shared-publisher \
    shared-publisher-leader-app/main.go shared-publisher-leader-app/app.go shared-publisher-leader-app/version.go

# Runtime stage
FROM alpine:3.22@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 publisher && \
    adduser -u 1000 -G publisher -D publisher

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/rollup-shared-publisher /app/
COPY --from=builder /build/shared-publisher-leader-app/configs/config.yaml /app/configs/

# Create directory for logs
RUN mkdir -p /app/logs && chown -R publisher:publisher /app

# Switch to non-root user
USER publisher

# Expose ports
EXPOSE 8080 8081

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8081/health || exit 1

ENTRYPOINT ["/app/rollup-shared-publisher"]
CMD ["--config", "/app/configs/config.yaml"]
