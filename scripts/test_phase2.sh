#!/bin/bash

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PHASE2_SCRIPT="${SCRIPT_DIR}/phase2_test.py"

# Default values
HOST="localhost"
PORT=8080
DURATION=30
CLIENTS=3

print_usage() {
    echo "Usage: $0 [scenario] [options]"
    echo ""
    echo "Scenarios:"
    echo "  happy-path    - All sequencers vote commit (default)"
    echo "  abort-test    - One sequencer votes abort"
    echo "  random-votes  - Random voting behavior"
    echo "  timeout-test  - Test timeout scenarios"
    echo "  stress-test   - Multiple transactions with many clients"
    echo ""
    echo "Options:"
    echo "  --host HOST           Publisher host (default: localhost)"
    echo "  --port PORT           Publisher port (default: 8080)"
    echo "  --duration SECONDS    Test duration (default: 30)"
    echo "  --clients COUNT       Number of clients (default: 3)"
    echo "  --help               Show this help"
}

run_scenario() {
    local scenario=$1
    shift
    local extra_args="$@"

    echo -e "${BLUE}Running scenario: ${scenario}${NC}"
    echo -e "${YELLOW}Extra args: ${extra_args}${NC}"
    echo "=================================================="

    case $scenario in
        "happy-path")
            echo -e "${GREEN}Happy Path Test - All sequencers vote commit${NC}"
            python3 "$PHASE2_SCRIPT" \
                --host "$HOST" \
                --port "$PORT" \
                --clients "$CLIENTS" \
                --vote-strategy commit \
                --send-tx \
                --tx-count 2 \
                --duration "$DURATION" \
                $extra_args
            ;;

        "abort-test")
            echo -e "${RED}Abort Test - Mixed voting (some abort)${NC}"
            python3 "$PHASE2_SCRIPT" \
                --host "$HOST" \
                --port "$PORT" \
                --clients "$CLIENTS" \
                --vote-strategy abort \
                --send-tx \
                --tx-count 1 \
                --duration "$DURATION" \
                $extra_args
            ;;

        "random-votes")
            echo -e "${YELLOW}Random Votes Test - Unpredictable behavior${NC}"
            python3 "$PHASE2_SCRIPT" \
                --host "$HOST" \
                --port "$PORT" \
                --clients "$CLIENTS" \
                --vote-strategy random \
                --send-tx \
                --tx-count 3 \
                --duration "$DURATION" \
                $extra_args
            ;;

        "timeout-test")
            echo -e "${RED}Timeout Test - Delayed votes to trigger timeout${NC}"
            python3 "$PHASE2_SCRIPT" \
                --host "$HOST" \
                --port "$PORT" \
                --clients "$CLIENTS" \
                --vote-strategy delay \
                --send-tx \
                --tx-count 1 \
                --duration "$DURATION" \
                $extra_args
            ;;

        "stress-test")
            echo -e "${BLUE}Stress Test - Multiple clients and transactions${NC}"
            python3 "$PHASE2_SCRIPT" \
                --host "$HOST" \
                --port "$PORT" \
                --clients 5 \
                --vote-strategy random \
                --send-tx \
                --tx-count 5 \
                --duration "$DURATION" \
                $extra_args
            ;;

        *)
            echo -e "${RED}Unknown scenario: $scenario${NC}"
            print_usage
            exit 1
            ;;
    esac
}

# Parse arguments
scenario="happy-path"
extra_args=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --host)
            HOST="$2"
            shift 2
            ;;
        --port)
            PORT="$2"
            shift 2
            ;;
        --duration)
            DURATION="$2"
            shift 2
            ;;
        --clients)
            CLIENTS="$2"
            shift 2
            ;;
        --help)
            print_usage
            exit 0
            ;;
        --*)
            extra_args="$extra_args $1"
            if [[ $# -gt 1 && ! "$2" =~ ^-- ]]; then
                extra_args="$extra_args $2"
                shift 2
            else
                shift
            fi
            ;;
        *)
            if [[ -z "$scenario_set" ]]; then
                scenario="$1"
                scenario_set=true
            else
                extra_args="$extra_args $1"
            fi
            shift
            ;;
    esac
done

# Check if phase2_test.py exists
if [[ ! -f "$PHASE2_SCRIPT" ]]; then
    echo -e "${RED}Error: phase2_test.py not found at $PHASE2_SCRIPT${NC}"
    exit 1
fi

# Check if Python 3 is available
if ! command -v python3 &> /dev/null; then
    echo -e "${RED}Error: python3 not found${NC}"
    exit 1
fi

echo -e "${GREEN}Phase 2 Test Runner${NC}"
echo "Publisher: $HOST:$PORT"
echo "Clients: $CLIENTS"
echo "Duration: $DURATION seconds"
echo ""

# Run the scenario
run_scenario "$scenario" $extra_args

echo -e "\n${GREEN}Test completed!${NC}"
