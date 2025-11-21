# Rollup Shared Publisher — AGENTS Guide

This document gives developers and agents a precise, implementation‑oriented map of the Rollup Shared Publisher (SP) and its interaction with rollup sequencers to enable synchronous composability between rollups. It focuses on what the system actually does today, how it does it together with sequencers (notably our op‑geth fork), and where to look in code when making changes.


## Purpose

- Provide synchronous composability across rollups: let a user compose transactions that span multiple L2s and either commit on all or abort on all in the same slot.
- Do this by coordinating sequencers with a small, fixed shared actor (the SP) that runs a 2‑Phase Commit (2PC) decision for each cross‑rollup transaction and orchestrates slot timing, sealing, and superblock construction.
- Preserve each rollup’s sovereignty for local execution while checking cross‑rollup consistency (mailboxes) and producing a single L1‑verifiable proof per batch/superblock.

Ground truth specs (read first):
- spec/synchronous_composability_protocol.md — the SCP (2PC) used by SP and sequencers.
- spec/superblock_construction_protocol.md — slot lifecycle, StartSlot/RequestSeal, L2 block submission, rollback.
- spec/settlement_layer.md — proof story: per‑rollup range/aggregation, then a network aggregation to a single on‑chain verification.


## Architecture

Actors
- Shared Publisher (SP): single leader that coordinates slots and runs SCP instances; aggregates L2 blocks and publishes a superblock with ZK proof to L1.
- Sequencer (one per rollup): builds an L2 block per slot, simulates local parts of a cross‑rollup transaction (xT), exchanges CIRC messages with peer sequencers, votes commit/abort, and submits its L2 block to the SP.
- L1: settlement and clock source; contract verifies proofs and tracks superblock chain.

Key protocols/messages (proto/rollup/v1/messages.proto)
- StartSlot, RequestSeal, RollBackAndStartSlot, L2Block (SBCP); StartSC, Vote, Decided, CIRCMessage (SCP + CIRC); XTRequest (user xT bundle).

Time model and states
- SP runs 12s slots (default). Within a slot: Free → Locked (while an xT is decided) → Free … until Seal time → Sealing.
- Sequencer states mirror slot semantics: Waiting → Building‑Free → Building‑Locked → Submission → Waiting.


## End‑to‑End Flow (Happy Path)

1) Slot start
- SP computes the next slot and broadcasts StartSlot with per‑rollup L2BlockRequest (next block number and parent hash).
- Sequencers move to Building‑Free and start drafting their L2 block for that slot.

2) Cross‑rollup xT
- A user (or a sequencer through RPC) submits an XTRequest that includes per‑chain RLP transactions.
- SP enqueues xTs and, while Free, starts the next one via StartSC(slot, sequence, xT, xtID). All sequencers enter Building‑Locked.
- Each sequencer:
  - Filters its local txs for this xT and simulates them with a mailbox tracer.
  - Emits CIRCMessage(s) to peer sequencers for any mailbox writes; blocks and waits for required mailbox reads (ACKs) by consuming CIRC from peers.
  - When its local execution is determinate (success/failure not due to “missing mailbox read”), it votes Vote(xtID, true|false).
- SP gathers votes. If any vote=false or timeout: Decided(xtID, false). If all true before timeout: Decided(xtID, true).
- Sequencers apply decision atomically to their draft: include local xT transactions plus any “putInbox” writes if decision=true; otherwise discard and remove mailbox staging. They return to Building‑Free.

3) Seal and submission
- At seal cutover (default 2/3 of the slot), SP sends RequestSeal(slot, IncludedxTs). Sequencers switch to Submission, finalize their block (include only xTs that Decided=true and appear in IncludedxTs), submit L2Block to SP, then go to Waiting.

4) Superblock + proof
- SP validates received L2Block messages, assembles a superblock (merkle over L2 blocks), collects per‑rollup proofs, runs the network aggregation program, and publishes the superblock with its proof to L1. Rollbacks are handled by RollBackAndStartSlot when needed.

Safety/Liveness
- Safety mirrors 2PC: if any sequencer votes 0, only abort is possible; if all vote 1 with no failures, only commit is possible. Agreement holds among correct parties.
- Liveness: crash‑fault tolerant for sequencers; SP is a single liveness point (no termination if SP is down). See fault tables in the specs for exact behaviors.


## How Sequencers Actually “Do The Right Thing” (op‑geth fork)

Integration points in our op‑geth fork bind the execution layer to SCP/SBCP and the SP transport:

- User entrypoint: eth_sendXTransaction
  - internal/ethapi/api.go:1768 — TransactionAPI.SendXTransaction decodes a hex‑encoded proto Message containing XTRequest and forwards it into the SP path via HandleSPMessage(ctx, msg) with a forward hint in context.

- SP/coordinator wiring and miner hooks
  - eth/api_backend.go:1364 — SetSequencerCoordinator wires the SBCP coordinator and TCP SP client into the backend; registers consensus callbacks and miner notifications.
  - eth/api_backend.go:1406 — NotifySlotStart(startSlot) clears pending state for the new slot.
  - eth/api_backend.go:1426 — NotifyRequestSeal(ctx, requestSeal) stores the included xtIDs and triggers sending of any stored per‑slot L2 blocks to the SP.
  - eth/api_backend.go:1481 — sendStoredL2Block() encodes the Geth block to L2Block (proto) and sends it to SP, marking included xTs as committed in the local consensus layer.

- Synchronous composability via mailbox tracing
  - eth/api_backend.go:1620 — simulateXTRequestForSBCP is called by the sequencer coordinator when StartSC arrives:
    - Simulate each local tx with a mailbox tracer (eth/ssv_mailbox_processor.go) to discover outgoing messages and blocking reads.
    - Send CIRC messages to peers for writes; wait and stage “putInbox” transactions for reads; re‑simulate to detect ACKs; pool successful txs once dependencies are satisfied.
    - Return vote=true iff all local xT transactions are successful after mailbox dependencies are satisfied; else vote=false.
  - eth/ssv_mailbox_processor.go — MailboxProcessor parses mailbox ABI calls, classifies reads/writes, sends/awaits CIRC, and builds signed putInbox txs with the local sequencer key.

- P2P CIRC transport
  - node/node.go bootstraps a per‑rollup P2P server and peer clients; eth/api_backend wires handlers so inbound CIRC is routed to the SBCP consensus layer.

The net effect: the execution client determines a correct local vote by simulating with mailbox semantics, exchanging CIRC over P2P, and staging putInbox writes. Only after Decided(true) and RequestSeal do we submit the final L2 block to the SP.


## Shared Publisher — Internal Structure

The SP composes a generic publisher + SBCP adapter + slot/coordinator + transport/auth + optional proving pipeline.

Sub‑systems
- Transport/auth (x/transport, x/auth): TCP server with optional ECDSA handshake auth, proto framing, and metrics.
- Generic publisher (x/publisher): routes XTRequest/Vote/Block, runs a pluggable 2PC coordinator, tracks active xTs.
- Consensus engine (x/consensus): 2PC (leader/follower roles), state manager, callbacks for Start/Vote/Decision/Block.
- SBCP layer (x/superblock): slot state machine, message handlers, queue, L2/L1 integration, proofs pipeline.

Slot orchestration (SP)
- Starting → Free: decide who participates and broadcast StartSlot with per‑chain L2BlockRequest.
- Free: dequeue the next XTRequest and start StartSC; remain Free until lock is needed.
- Locked: one xT in flight using SCP; on Decided, unlock to Free.
- Sealing: at seal time, broadcast RequestSeal(IncludedxTs), accept L2Block messages, build superblock, trigger proofing/publish, then start next slot.

Settlement/proofs (optional but wired)
- Each sequencer proves its batch (range → aggregation) producing AggregationOutputs; SP verifies all and runs a network aggregation to a single SNARK, then posts state + proof to L1 (x/superblock/l1, x/superblock/proofs/*).
- Contracts in this repo are examples/demo; the on‑chain interface used by the L1 publisher is the dispute game factory binding (x/superblock/l1/contracts/*).


## Message Transport and Authentication

- SP listens via x/transport/tcp Server on a configured port; heartbeat and backoff are handled internally.
- One‑time ECDSA handshake authenticates the client connection (compressed 33‑byte pubkeys). The SP maintains a trusted allowlist by id → pubkey.
- CIRC messages between sequencers go over a separate P2P TCP mesh (also x/transport/tcp), not via the SP.

Operational footguns
- Chain ID representation must match across all paths (proto uses raw bytes). Use helpers from x/consensus (e.g., ChainKeyBytes/ChainKeyUint64) to avoid mismatches.
- Only one Vote and one Decided are allowed per xT; handlers must check and ignore duplicates/timeouts accordingly (see specs).
- StartSC sequence is per‑slot monotonically increasing; sequencers discard out‑of‑order StartSC unless the previous instance was decided.


## Code Map (where to read/change)

Core protocol types
- proto/rollup/v1/messages.proto — all wire messages.

Consensus (2PC)
- x/consensus/coordinator.go — 2PC lifecycle, state management, callbacks.
- x/consensus/protocol_handler.go — maps proto messages to coordinator methods.

Publisher (generic)
- x/publisher/publisher.go — transport + consensus glue; broadcasts Decided.
- x/publisher/handler.go — XTRequest/Vote/Block handlers.
- x/publisher/router.go — per‑message router registry.

SBCP — Shared Publisher side
- x/superblock/coordinator.go — slot loop, StartSlot/RequestSeal/L2Block handling, superblock build, L1 publish, rollback.
- x/superblock/slot/state_machine.go — SP slot FSM and per‑slot tracking of SCP instances and received L2 blocks.
- x/superblock/handlers/*.go — SBCP message handling, XT queueing.
- x/superblock/adapter/publisher_wrapper.go — wraps the generic publisher with SBCP; intercepts XTRequest and consensus callbacks to keep slot state.
- x/superblock/l1/* — L1 publisher (EIP‑1559 tx build/sign/send), event watcher, receipt polling.
- x/superblock/proofs_pipeline.go and x/superblock/proofs/* — proof collection and prover integration + HTTP API.

SBCP — Sequencer side (SDK used in op‑geth and test‑app)
- x/superblock/sequencer/coordinator.go — sequencer FSM and message router; votes via callbacks into the host.
- x/superblock/sequencer/scp_integration.go — tracks current xT context; adds decided txs to block builder.
- x/superblock/sequencer/state_machine.go — Waiting/Building‑Free/Locked/Submission.
- x/superblock/sequencer/bootstrap/bootstrap.go — helper to wire coordinator + SP client + P2P server/clients.

Transport/auth
- x/transport/tcp/server.go and client.go — TCP server/client; auth handshake is in x/transport/tcp/connection.go using x/auth.

Leader application
- shared-publisher-leader-app/app.go — composes everything, runs SP TCP + HTTP servers and proofs API.
- shared-publisher-leader-app/configs/config.yaml — example runtime config.

op‑geth fork (essential for understanding how sequencers integrate)
- internal/ethapi/api.go:1768 — eth_sendXTransaction.
- eth/api_backend.go — coordinator wiring, miner hooks, simulateXTRequestForSBCP, L2Block send.
- eth/ssv_mailbox_processor.go — mailbox tracer and CIRC orchestration.
- node/node.go — bootstraps SBCP runtime (SP client, P2P transport, coordinator lifecycle).


## References and Quick Pointers

- SCP spec: spec/synchronous_composability_protocol.md
- SBCP spec: spec/superblock_construction_protocol.md
- Settlement/proofs: spec/settlement_layer.md
- Publisher code entry: x/publisher/publisher.go
- SP coordinator: x/superblock/coordinator.go
- Sequencer coordinator: x/superblock/sequencer/coordinator.go
- op‑geth RPC entry: ~/Code/op-geth/internal/ethapi/api.go:1768
- Mailbox processor: ~/Code/op-geth/eth/ssv_mailbox_processor.go
- XTRequest example client: ~/Code/op-geth/cmd/xclient (and xbridge)
