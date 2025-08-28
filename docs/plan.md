## Superblock Construction Protocol (SBCP) Implementation Plan

### Executive Summary
Implement a slotted protocol for coordinating L2 block construction and cross-chain transaction execution across multiple rollups, extending the existing 2PC system to support full superblock assembly.

### Current State
- ✅ 2PC consensus protocol for cross-chain transactions
- ✅ TCP transport with authentication
- ✅ Message signing and verification
- ❌ Slot-based execution
- ❌ L2 block management
- ❌ Superblock assembly and publishing

### Target Architecture

```
┌─────────────────────────────────────┐
│         Slot Manager (12s)          │
├─────────────────────────────────────┤
│    State: Starting→Free→Locked→Sealing│
├─────────────────────────────────────┤
│         Sequencer FSM               │
│  Waiting→Building-Free⟷Locked→Submit│
├─────────────────────────────────────┤
│      L2 Block Management            │
│   Validation, Storage, Recovery     │
├─────────────────────────────────────┤
│     Superblock Builder              │
│   Assembly, Merkle Root, L1 Publish │
└─────────────────────────────────────┘
```

---

## Implementation Tasks

### Phase 1: Protocol Definition & Infrastructure
**Duration: 3 days**

#### Task 1.1: Define Protocol Messages
- Add SBCP-specific protobuf messages to `proto/rollup/v1/`
- Messages needed: `StartSlot`, `RequestSeal`, `L2Block`, `L2BlockRequest`, `RollBackAndStartSlot`
- Extend existing `Message` wrapper with new payload types
- Generate Go bindings and update type helpers

#### Task 1.2: Create Module Structure
- Create `x/superblock/` directory
- Define core interfaces in `interfaces.go`
- Set up module configuration structures
- Create metrics registry for SBCP-specific metrics

#### Task 1.3: Design State Storage
- Define persistent state requirements
- Design schema for L2 block storage
- Design schema for superblock storage
- Plan WAL integration for crash recovery

---

### Phase 2: Slot Management System
**Duration: 4 days**

#### Task 2.1: Implement Slot Timer
- Create slot manager that aligns with Ethereum's 12-second slots
- Calculate slot numbers from genesis time
- Handle slot transitions and timing events
- Implement 2/3 cutover mechanism for sealing phase

#### Task 2.2: Build Slot State Machine
- Implement SP states: `StartingSlot`, `Free`, `Locked`, `Sealing`
- Handle state transitions based on slot progress
- Integrate with existing consensus coordinator
- Add slot-aware message routing

#### Task 2.3: Active Rollup Tracking
- Implement L1 registry monitoring
- Track active rollups per slot
- Handle dynamic rollup addition/removal
- Maintain rollup metadata (endpoints, public keys)

---

### Phase 3: Sequencer State Machine
**Duration: 5 days**

#### Task 3.1: Implement Core FSM
- Build state machine: `Waiting`, `Building-Free`, `Building-Locked`, `Submission`
- Handle `StartSlot` message processing
- Implement draft block management
- Add top-of-block transaction injection

#### Task 3.2: Integrate SCP Protocol
- Connect with existing consensus module
- Handle `StartSC` message to lock building
- Process `Decided` messages for transaction inclusion
- Manage SCP instance lifecycle per slot

#### Task 3.3: Block Building Logic
- Implement local transaction queuing
- Handle transaction ordering and inclusion
- Add CIRC message processing
- Implement `RequestSeal` handling

#### Task 3.4: Block Submission
- Build L2 block encoding
- Implement block submission to SP
- Add retry logic for failed submissions
- Handle submission confirmations

---

### Phase 4: Cross-Chain Transaction Queue
**Duration: 3 days**

#### Task 4.1: Priority Queue Implementation
- Build persistent priority queue for xT requests
- Implement request expiration
- Handle request resubmission on failure
- Add deduplication logic

#### Task 4.2: Queue Management
- Process incoming xT requests from users/sequencers
- Trigger SCP instances from queue
- Handle queue persistence across slots
- Implement request prioritization algorithm

---

### Phase 5: L2 Block Management
**Duration: 4 days**

#### Task 5.1: Block Validation
- Implement header validation
- Add CIRC message consistency checks
- Validate parent hash chains
- Check cross-chain transaction inclusion

#### Task 5.2: Block Storage
- Implement block store interface
- Add block retrieval by chain/number
- Handle block pruning after finalization
- Implement efficient block queries

#### Task 5.3: Recovery Protocol
- Build block recovery mechanism for crashed sequencers
- Implement `L2BlockRequest` handling
- Add block resync protocol
- Handle partial slot recovery

---

### Phase 6: Superblock Assembly
**Duration: 4 days**

#### Task 6.1: Block Collection
- Track received L2 blocks per slot
- Validate block set completeness
- Handle missing block scenarios
- Implement timeout handling

#### Task 6.2: Superblock Construction
- Build merkle tree of L2 block hashes
- Create superblock structure
- Calculate superblock hash
- Handle partial superblock scenarios

#### Task 6.3: L1 Publishing
- Integrate with Ethereum client
- Implement superblock submission transaction
- Handle gas estimation and pricing
- Add submission retry logic

---

### Phase 7: Rollback & Recovery
**Duration: 3 days**

#### Task 7.1: Rollback Mechanism
- Detect invalid superblocks
- Implement `RollBackAndStartSlot` message
- Handle state reversion to valid checkpoint
- Coordinate rollback across all sequencers

#### Task 7.2: Crash Recovery
- Implement WAL for critical operations
- Handle SP crash and restart
- Handle sequencer crash scenarios
- Implement state reconstruction from storage

#### Task 7.3: Reorg Handling
- Monitor L1 for reorgs
- Handle superblock resubmission
- Update local state on reorg
- Notify sequencers of reorg events

---

### Phase 8: Performance Optimizations
**Duration: 3 days**

#### Task 8.1: Parallel Transaction Processing
- Implement transaction dependency analysis
- Build dependency graph for smart contracts
- Enable parallel local transaction processing
- Add fork management for speculative execution

#### Task 8.2: Message Optimization
- Implement message batching
- Add compression for large blocks
- Optimize protobuf encoding
- Reduce network round trips

---

### Phase 9: Monitoring & Observability
**Duration: 2 days**

#### Task 9.1: Metrics Implementation
- Add slot timing metrics
- Track state transition latencies
- Monitor block submission success rates
- Add xT queue depth metrics

#### Task 9.2: Health Checks
- Implement slot progress monitoring
- Add sequencer liveness checks
- Monitor L1 submission status
- Create alerting rules
