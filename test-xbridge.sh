#!/bin/bash

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_DIR="$SCRIPT_DIR"

NUM_TXS=${1:-20}
DELAY_MS=${2:-100}

echo "Starting xbridge test..."
echo "  Transactions: $NUM_TXS"
echo "  Delay: ${DELAY_MS}ms"
echo "  Log directory: $LOG_DIR"
echo ""

echo "Clearing old logs..."
rm -f "$LOG_DIR/rollupNativeA.txt" "$LOG_DIR/rollupNativeB.txt" "$LOG_DIR/sharedPublisher.txt"

echo "Starting log capture from both rollups and shared publisher (2s before + test duration + 10s after)"
kubectl -n optimism logs optimism-stack-geth-0 --since=2s -f > "$LOG_DIR/rollupNativeA.txt" 2>&1 &
PID_A=$!

kubectl -n optimism logs optimism-stack-2-geth-0 --since=2s -f > "$LOG_DIR/rollupNativeB.txt" 2>&1 &
PID_B=$!

kubectl -n rollup logs rollup-shared-publisher-7f7bcf6567-qsnzt --since=2s -f > "$LOG_DIR/sharedPublisher.txt" 2>&1 &
PID_SP=$!

trap "kill $PID_A $PID_B $PID_SP 2>/dev/null; wait $PID_A $PID_B $PID_SP 2>/dev/null" EXIT

sleep 2

echo "Running xbridge..."
cd "$SCRIPT_DIR/op-geth"
go run ./cmd/xbridge -batch -num "$NUM_TXS" -delay "$DELAY_MS"

echo ""
echo "Waiting for logs to settle..."
sleep 10

kill $PID_A $PID_B $PID_SP 2>/dev/null || true
wait $PID_A $PID_B $PID_SP 2>/dev/null || true

echo ""
echo "Test complete!"
echo "Logs saved:"
echo "  - $LOG_DIR/rollupNativeA.txt ($(wc -l < "$LOG_DIR/rollupNativeA.txt") lines)"
echo "  - $LOG_DIR/rollupNativeB.txt ($(wc -l < "$LOG_DIR/rollupNativeB.txt") lines)"
echo "  - $LOG_DIR/sharedPublisher.txt ($(wc -l < "$LOG_DIR/sharedPublisher.txt") lines)"
