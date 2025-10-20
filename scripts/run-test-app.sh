#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Parse arguments
MODE="${1:-auth}"  # Default to auth mode
if [[ "$MODE" != "auth" && "$MODE" != "no-auth" ]]; then
    echo "Usage: $0 [auth|no-auth]"
    echo "  auth    - Run with authentication (default)"
    echo "  no-auth - Run without authentication"
    exit 1
fi

# Process tracking
SP_PID=""
SEQ_PIDS=()

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[runner]${NC} $1"
}

error() {
    echo -e "${RED}[runner]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[runner]${NC} $1"
}

cleanup() {
    log "Cleaning up processes..."

    # Kill sequencers first
    for pid in "${SEQ_PIDS[@]:-}"; do
        if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
            log "Stopping sequencer (pid $pid)"
            kill -TERM "$pid" 2>/dev/null || true

            # Wait up to 2 seconds for graceful shutdown
            for i in {1..20}; do
                if ! kill -0 "$pid" 2>/dev/null; then
                    break
                fi
                sleep 0.1
            done

            # Force kill if still running
            if kill -0 "$pid" 2>/dev/null; then
                warn "Force killing sequencer (pid $pid)"
                kill -9 "$pid" 2>/dev/null || true
            fi
        fi
    done

    # Kill SP
    if [[ -n "${SP_PID:-}" ]] && kill -0 "$SP_PID" 2>/dev/null; then
        log "Stopping shared publisher (pid $SP_PID)"
        kill -TERM "$SP_PID" 2>/dev/null || true

        # Wait for graceful shutdown
        for i in {1..20}; do
            if ! kill -0 "$SP_PID" 2>/dev/null; then
                break
            fi
            sleep 0.1
        done

        # Force kill if needed
        if kill -0 "$SP_PID" 2>/dev/null; then
            warn "Force killing SP (pid $SP_PID)"
            kill -9 "$SP_PID" 2>/dev/null || true
        fi
    fi

    # Clean up any lingering processes on our ports
    for port in 8080 8081 9000 9001; do
        if command -v lsof >/dev/null 2>&1; then
            local pids
            pids=$(lsof -ti tcp:$port 2>/dev/null || true)
            if [[ -n "$pids" ]]; then
                warn "Cleaning up processes on port $port: $pids"
                echo "$pids" | xargs -r kill -9 2>/dev/null || true
            fi
        fi
    done

    log "Cleanup complete"
}

# Set up cleanup on exit
trap cleanup EXIT INT TERM

# Wait for health endpoint
wait_health() {
    local max_attempts=50
    for i in $(seq 1 $max_attempts); do
        if curl -sf http://localhost:8081/health >/dev/null 2>&1; then
            return 0
        fi
        sleep 0.2
    done
    error "Health check failed after $max_attempts attempts"
    return 1
}

make_no_auth_config() {
    local main_config="$ROOT_DIR/publisher-leader-app/configs/config.yaml"
    local no_auth_config="/tmp/sp-no-auth.yaml"

    if [[ ! -f "$main_config" ]]; then
        error "Main config file not found: $main_config"
        exit 1
    fi

    sed '/^auth:/,/^[a-z]/ {
        /^auth:/ {
            r /dev/stdin
            d
        }
        /^[a-z]/ !d
    }' "$main_config" > "$no_auth_config" << 'EOF'
auth:
  enabled: false

EOF

    echo "$no_auth_config"
}

# Start SP based on mode
start_sp() {
    if [[ "$MODE" == "auth" ]]; then
        log "Starting SP with authentication..."
        ( cd "$ROOT_DIR" && \
          go run ./publisher-leader-app \
            --config publisher-leader-app/configs/config.yaml \
            --log-pretty \
            --metrics ) &
        SP_PID=$!
    else
        log "Starting SP without authentication..."
        local config
        config=$(make_no_auth_config)
        ( cd "$ROOT_DIR" && \
          go run ./publisher-leader-app \
            --config "$config" \
            --log-pretty \
            --metrics ) &
        SP_PID=$!
    fi

    log "SP started with PID $SP_PID"
}

# Start sequencers based on mode
start_sequencers() {
    if [[ "$MODE" == "auth" ]]; then
        log "Starting sequencers with authentication and CIRC..."

        # Sequencer 1
        ( cd "$ROOT_DIR" && \
          go run ./test-app \
            --chain-id 1 \
            --initiate \
            --log-pretty \
            --log-level info \
            --private-key 3afc6aa26dcfb78f93b2df978e41b2d89449e7951670763717265ab0a552aae0 \
            --sp-pub 034f2a8d175528ed60f64b7c3a5d5e72cf2aa3acda444b33e16fdfb3e3e4326ce5 ) &
        SEQ_PIDS+=("$!")

        # Sequencer 2
        ( cd "$ROOT_DIR" && \
          go run ./test-app \
            --chain-id 2 \
            --log-pretty \
            --log-level info \
            --private-key 1e33f16449a0b646f672b0a5415bed21310d388effb7d3b95816d1c12c492f74 \
            --sp-pub 034f2a8d175528ed60f64b7c3a5d5e72cf2aa3acda444b33e16fdfb3e3e4326ce5 ) &
        SEQ_PIDS+=("$!")
    else
        log "Starting sequencers without authentication (with CIRC)..."

        # Sequencer 1
        ( cd "$ROOT_DIR" && \
          go run ./test-app \
            --chain-id 1 \
            --initiate \
            --log-pretty \
            --log-level info \
            --no-auth ) &
        SEQ_PIDS+=("$!")

        # Sequencer 2
        ( cd "$ROOT_DIR" && \
          go run ./test-app \
            --chain-id 2 \
            --log-pretty \
            --log-level info \
            --no-auth ) &
        SEQ_PIDS+=("$!")
    fi

    log "Sequencers started with PIDs: ${SEQ_PIDS[*]}"
}

# Main execution
main() {
    log "Starting test with mode: $MODE"
    log "Root directory: $ROOT_DIR"

    # Clean up any existing processes
    cleanup

    # Start SP
    start_sp

    # Wait for SP to be healthy
    log "Waiting for SP health check..."
    if ! wait_health; then
        error "SP failed to become healthy"
        exit 1
    fi
    log "SP is healthy"

    # Small delay for SP to fully initialize
    sleep 0.5

    # Start sequencers
    start_sequencers

    # Wait for sequencers to complete
    log "Waiting for sequencers to complete..."
    for pid in "${SEQ_PIDS[@]}"; do
        wait "$pid" 2>/dev/null || true
    done

    log "Test completed successfully"

    # Give time for final messages
    sleep 1
}

main
