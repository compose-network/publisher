#!/bin/bash

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Default values
COVERAGE=false
VERBOSE=false
RACE=true
TIMEOUT="30s"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -c|--coverage)
            COVERAGE=true
            shift
            ;;
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        --no-race)
            RACE=false
            shift
            ;;
        -t|--timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

echo -e "${YELLOW}Running tests...${NC}"

# Build test flags
TEST_FLAGS="-timeout ${TIMEOUT}"
if [ "$VERBOSE" = true ]; then
    TEST_FLAGS="${TEST_FLAGS} -v"
fi
if [ "$RACE" = true ]; then
    TEST_FLAGS="${TEST_FLAGS} -race"
fi
if [ "$COVERAGE" = true ]; then
    TEST_FLAGS="${TEST_FLAGS} -coverprofile=coverage.out -covermode=atomic"
fi

# Run tests
echo "Test flags: ${TEST_FLAGS}"
go test ${TEST_FLAGS} ./...

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ All tests passed!${NC}"

    if [ "$COVERAGE" = true ]; then
        echo -e "${YELLOW}Generating coverage report...${NC}"
        go tool cover -html=coverage.out -o coverage.html
        echo -e "${GREEN}✓ Coverage report generated: coverage.html${NC}"

        # Show coverage summary
        echo -e "\n${YELLOW}Coverage summary:${NC}"
        go tool cover -func=coverage.out | grep total | awk '{print $3}'
    fi
else
    echo -e "${RED}✗ Tests failed!${NC}"
    exit 1
fi
