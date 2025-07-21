# Phase 2 Testing Guide

Phase 2 testing scripts implement a complete Two-Phase Commit (2PC) protocol test suite for the rollup-shared-publisher.
These scripts simulate sequencers that participate in the full 2PC lifecycle.

## Overview

The Phase 2 test suite includes:

- **`phase2_test.py`** - Main test script with full 2PC implementation
- **`test_phase2.sh`** - Bash wrapper for different test scenarios

## Test Architecture

### Complete 2PC Flow

1. **XTRequest** - Sequencer sends cross-chain transaction
2. **Broadcast** - Publisher broadcasts to all sequencers
3. **Vote** - All participants vote (COMMIT/ABORT)
4. **Decided** - Publisher broadcasts final decision
5. **Block** - On COMMIT, sequencers send block confirmations

### Message Types Implemented

- **XTRequest**: Multi-chain transaction requests
- **Vote**: Participant voting (with proper chain_id)
- **Decided**: Final 2PC decision handling
- **Block**: Block submission confirmations

## Available Test Scenarios

### 1. Happy Path Test

```bash
./scripts/test_phase2.sh happy-path --duration 10
```

- All sequencers vote COMMIT
- Complete 2PC flow with block submissions
- Tests normal operation

### 2. Abort Test

```bash
./scripts/test_phase2.sh abort-test --duration 10
```

- All sequencers vote ABORT
- Tests immediate abort on first ABORT vote
- No block submissions

### 3. Random Votes Test

```bash
./scripts/test_phase2.sh random-votes --duration 15
```

- Random voting behavior
- Tests mixed scenarios
- Demonstrates abort-on-first-abort behavior

### 4. Timeout Test

```bash
./scripts/test_phase2.sh timeout-test --duration 20
```

- Delayed voting to trigger timeouts
- Tests timeout handling (3-minute default)
- Requires longer duration

### 5. Stress Test

```bash
./scripts/test_phase2.sh stress-test --duration 30
```

- Multiple clients and transactions
- Tests concurrent 2PC operations
- Higher load testing

## Usage Options

### Basic Usage

```bash
# Default happy path test
./scripts/test_phase2.sh

# Specific scenario
./scripts/test_phase2.sh [scenario] [options]
```

### Advanced Options

```bash
# Custom host/port
./scripts/test_phase2.sh happy-path --host remote-host --port 9090

# More clients
./scripts/test_phase2.sh stress-test --clients 5 --duration 60

# Multiple transactions
./scripts/test_phase2.sh happy-path --tx-count 3
```

### Direct Python Usage

```bash
# Custom configuration
python3 scripts/phase2_test.py \
    --host localhost \
    --port 8080 \
    --clients 3 \
    --vote-strategy commit \
    --send-tx \
    --duration 30
```

## Implementation Details

### Cross-Chain Transaction Format

The tests create proper cross-chain transactions with multiple TransactionRequests:

```python
# XTRequest contains multiple chains
participating_chains = [
    bytes([0x12, 0x34]),  # Chain A
    bytes([0x13, 0x35]),  # Chain B
    bytes([0x14, 0x36]),  # Chain C
]
```

### Vote Strategy Options

- **`commit`** - Always vote COMMIT
- **`abort`** - Always vote ABORT
- **`random`** - Random voting
- **`delay`** - Delayed voting (for timeout tests)

### Proper 2PC Logic

- All participants vote for each transaction
- Coordinator waits for all votes before COMMIT
- Any ABORT vote causes immediate ABORT
- Block submissions only on COMMIT decisions

## Monitoring and Metrics

### Real-time Statistics

```bash
# Publisher stats
curl -s http://localhost:8081/stats | jq .

# Consensus metrics
curl -s http://localhost:8081/metrics | grep publisher_consensus
```

### Key Metrics to Monitor

- `publisher_consensus_transactions_total{state="commit"}` - Successful commits
- `publisher_consensus_transactions_total{state="initiated"}` - Started transactions
- `publisher_consensus_votes_received_total` - Votes by chain and decision
- `publisher_consensus_decisions_broadcast_total` - Decisions sent
- `publisher_consensus_active_transactions` - Current active transactions

## Expected Behavior

### Successful Commit (Happy Path)

```
[sequencer-A] Sent XTRequest
[sequencer-B] Received XTRequest broadcast
[sequencer-C] Received XTRequest broadcast
[sequencer-A] Sent Vote: COMMIT for xt_id=1
[sequencer-B] Sent Vote: COMMIT for xt_id=1
[sequencer-C] Sent Vote: COMMIT for xt_id=1
[sequencer-A] Received Decided: COMMIT for xt_id=1
[sequencer-B] Received Decided: COMMIT for xt_id=1
[sequencer-C] Received Decided: COMMIT for xt_id=1
[sequencer-A] Sent Block with xt_id=1
[sequencer-B] Sent Block with xt_id=1
[sequencer-C] Sent Block with xt_id=1
```

### Abort on First Negative Vote

```
[sequencer-A] Sent XTRequest
[sequencer-B] Received XTRequest broadcast
[sequencer-C] Received XTRequest broadcast
[sequencer-A] Sent Vote: ABORT for xt_id=1
[sequencer-B] Received Decided: ABORT for xt_id=1
[sequencer-C] Received Decided: ABORT for xt_id=1
[sequencer-A] Received Decided: ABORT for xt_id=1
```

## Prerequisites

### Environment Setup

```bash
# Build the publisher
make build

# Start publisher
./bin/rollup-shared-publisher -config configs/config.yaml

# Ensure Python 3 is available
python3 --version
```

### Port Configuration

- **8080** - Publisher TCP port (sequencer connections)
- **8081** - HTTP metrics/stats port

## Troubleshooting

### Common Issues

1. **No messages processed**: Check if publisher is running on correct port
2. **Votes rejected**: Ensure chain IDs match between XTRequest and Vote messages
3. **Transactions stuck**: Check if all participants are voting
4. **Connection refused**: Verify publisher is listening on expected port

### Debug Tools

```bash
# Compare message formats
python3 scripts/debug_message.py

# Check publisher logs
./bin/rollup-shared-publisher -config configs/config.yaml | grep -E "(Vote|Decided|Block)"

# Monitor active connections
curl -s http://localhost:8081/connections | jq .
```
