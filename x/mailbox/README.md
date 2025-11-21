# Mailbox

Mailbox utilities for sequencing cross-rollup transactions.

Contents

- `processor.go`: analyze mailbox calls in traced transactions, drive CIRC send/receive, and build `putInbox` txs.
- `types.go`: config, simulation state, and cross-rollup message/dependency types.
- `abi.go`: mailbox ABI for parsing calls and building calldata.
- `helpers.go`: coordination helpers and deduping helpers.
