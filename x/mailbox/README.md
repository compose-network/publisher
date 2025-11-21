# Mailbox

Mailbox utilities for sequencing cross-rollup transactions.

Contents

- `mailbox.go`: public interfaces, configuration, and constructor wiring with pluggable sender/inbox/tx builder + logger.
- `processor.go`: orchestration entrypoint that uses the injected interfaces for analysis and coordination.
- `analysis.go`: mailbox call parsing, dependency classification, and coordination summaries.
- `inbox.go`: consensus-backed CIRC waiter implementation with configurable polling/timeout.
- `tx_builder.go`: putInbox transaction builder using the configured selector + key.
- `transport.go`: adapter for legacy transport.Client map to the MessageSender interface.
- `types.go`: config, simulation state, and cross-rollup message/dependency types.
- `abi.go`: mailbox ABI for parsing calls and building calldata.
- `helpers.go`: coordination helpers and deduping helpers.
