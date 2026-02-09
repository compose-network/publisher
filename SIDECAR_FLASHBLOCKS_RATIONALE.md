# Sidecar + Flashblocks Rationale

## Why this is better than modifying op-geth

### 1. Removes timing races from the old push model

Directly injecting cross-chain flow into `op-geth` depends on narrow internal miner timing windows (for example, when a
pending block exists).
The sidecar + flashblocks model uses builder polling (pull), so coordination happens at deterministic flashblock
boundaries rather than racing internal miner states.

### 2. Stronger simulation/execution state alignment

A core requirement is: simulation state must match execution state.
With sidecar + flashblocks, the builder provides in-progress state context (state overrides), and sidecar simulations
run against that same evolving block context. This is much safer than relying on `op-geth`-specific pending-state
behavior.

### 3. Cleaner handling of parallel cross-chain transactions

Parallelism is handled at protocol level:

- Disjoint chain sets can progress in parallel.
- Per chain, execution remains sequential and deterministic.

This keeps Ethereum execution semantics intact while still allowing network-level parallel throughput.

### 4. Better separation of concerns

Modifying `op-geth` mixes cross-chain protocol orchestration into core execution-client logic.
The sidecar design keeps responsibilities separated:

- Builder builds blocks.
- Sidecar coordinates cross-chain simulation/dependencies.
- Publisher remains ordering authority.

This makes reasoning, debugging, and auditing substantially easier.

### 5. Lower long-term maintenance risk

`op-geth` forks are expensive to maintain across upstream upgrades and can drift from core client behavior.
A sidecar-centric integration reduces invasive client changes and keeps most Compose-specific logic in isolated
components.

### 6. Safer failure semantics

With sidecar-driven required transactions, builders can fail fast instead of silently continuing with inconsistent
partial behavior.
This is a better safety profile for atomic cross-chain inclusion.

## Tradeoffs (explicit)

- This approach introduces more moving parts (publisher, sidecar, builder integration).
- Correctness still depends on strict operational settings (for example, no silent fallback path that bypasses sidecar
  decisions).

## Bottom line

For Compose goals (state-correct cross-chain atomicity + practical parallelism), sidecar + flashblocks is a better
engineering path than deep `op-geth` protocol modifications.

---

## Deep Technical Analysis

This section provides a comprehensive technical assessment of the sidecar+flashblocks architecture versus the
old op-geth modification approach, covering parallel transaction support, simulation mechanics, state handling,
and performance characteristics.

### 1. The Fundamental Problem with op-geth (Why It Failed)

Op-geth's block building is a self-contained pipeline with three states: building (running mempool
txs), idle (sealed, waiting), and starting (nanoseconds). There is never a good time to submit an
SCP instance because:

- **Building state**: op-geth is already processing mempool txs. Inserting an XT here invalidates the
  state all subsequent mempool txs were simulated against.
- **Idle state**: Block is sealed. No pending block exists. `StartInstance` returns `NoPendingBlock`.
- **Starting state**: Lasts nanoseconds. Unreliable to hit.

This is not a timing optimization problem. It is a fundamental architectural mismatch. Op-geth was designed
as a self-contained block builder that runs to completion as fast as possible. It was never designed to pause
mid-execution for external consensus rounds. Every workaround — `computependingblock` toggling, TOB ordering,
FCU interception — is fighting the architecture rather than working with it.

The `computependingblock` flag made this worse. When enabled, the pending block state includes mempool tx
effects. XTs simulated against this state would execute in a different position in the final block (before
mempool txs), meaning simulation state != execution state. This violates the core invariant:
whatever the state for any tx during simulation must be the state during sealing.

### 2. How the Sidecar+Flashblocks Architecture Resolves Every Issue

#### 2.1 Timing Races → Eliminated

Op-rbuilder builds blocks in flashblock increments (configurable interval, typically 250ms). At each
flashblock boundary, the builder calls `POST /transactions` to the sidecar. This is a **pull model**:
the builder asks for XTs when it is ready, rather than the publisher pushing XTs when the builder may
not be ready.

The key insight from the codebase (`op-rbuilder/crates/op-rbuilder/src/builders/flashblocks/payload.rs`):
the builder uses cancellation tokens per flashblock. When the sidecar returns `hold: true`, the builder
re-polls after `poll_after_ms` (default 50ms). The builder controls its own timeline — there is no race
against an internal miner state machine.

#### 2.2 State Consistency → Guaranteed by Construction

The state override mechanism is the most critical piece. In `op-rbuilder/crates/op-rbuilder/src/sidecar/overrides.rs`,
`build_state_overrides()` iterates the builder's in-progress `State<DB>.transition_state` and extracts:

- Modified account balances, nonces, and code
- Changed storage slots (only dirty slots, using `value.is_changed()`)
- Destroyed accounts (zeroed out)

These overrides are sent as part of the poll request. The sidecar uses them as the base for
`debug_traceCall` simulation. This means the sidecar simulates against the exact EVM state the
builder has at this flashblock boundary — not `latest`, not a stale canonical state, but the
actual in-progress block state.

In the sidecar (`compose-sidecar/internal/coordinator/simulation.go`), `chainOverlay` accumulates
state changes from committed XTs within the same block/flashblock. Each subsequent XT simulation
merges the prior XT's post-state into the overlay. This guarantees sequential state correctness
for multiple XTs within the same flashblock.

The old approach had no equivalent. `computependingblock=true` gave a state that included mempool tx
effects in an undefined order. `computependingblock=false` gave the parent block state, missing all
pending XTs. Neither matched the actual sealing state.

#### 2.3 The A/B/x Interleaving Problem → Cannot Happen

The concern was: XT-A simulated, mempool tx `x` runs on post-A state, XT-B arrives and gets inserted
between A and `x`, invalidating `x`'s simulation.

In the sidecar architecture, this interleaving is structurally impossible:

1. The builder polls the sidecar at flashblock start.
2. While polling (and during `hold: true` responses), the builder is waiting — no mempool txs are processed.
3. The sidecar simulates all pending XTs sequentially (chain lock ensures one at a time per chain).
4. After XTs are resolved, the builder receives the committed XT txs and executes them.
5. Only then does the builder proceed to mempool tx execution.

The execution order in `build_next_flashblock()` enforces this:

```
sequencer txs → builder txs → sidecar XTs → pool txs
```

There is no interleaving because the builder pipeline is sequential within each flashblock, and sidecar
XTs are injected at a controlled point before pool txs.

#### 2.4 `computependingblock` → Not Involved

The sidecar architecture bypasses `computependingblock` entirely. Op-rbuilder manages its own EVM state
pipeline using `reth::revm::State<DB>`. State overrides are computed from the builder's actual transition
state, not from any geth-specific pending block abstraction. This eliminates the entire class of bugs
around pending state inconsistency.

#### 2.5 TOB (Top-of-Block) Ordering → Not Required

A key design question was whether XTs must be at the top of the block. TOB had five complications:
synchronized block starts, SP knowing rollup block times, dedicated XT periods, delayed user responses, and
per-block XT allocation windows.

The sidecar approach does not require TOB. XTs are injected at whatever flashblock boundary they resolve at.
They are ordered relative to each other by publisher sequence number, but they do not need to be at position
zero in the block. The `chainOverlay` mechanism ensures each XT simulates on the post-state of all prior
XTs, regardless of how many mempool txs preceded them in the block.

TOB adds complexity without benefit in this architecture.

#### 2.6 Rollup Synchronization → Publisher Periods, Not Block Alignment

The concern was: if XTs are TOB, rollups must start building blocks at the same time. The sidecar approach
decouples this. The publisher broadcasts `StartPeriod` (aligned to Ethereum slots, 12s default). All
sidecars enter the same period regardless of their individual block times.

When a `StartInstance` arrives, participating sidecars are in the active period and can simulate whenever
their builder next polls. The builder polls at flashblock boundaries (sub-second), so the window for
catching an XT is much larger than the nanosecond window in op-geth.

Rollups do not need the same block time. They do not need synchronized block starts. They need to be in
the same period, which is guaranteed by the publisher's `StartPeriod` broadcast.

### 3. Parallel Transaction Support

"Parallel" in Compose has a precise meaning, distinct from Ethereum's execution model.

#### 3.1 Protocol-Level Parallelism (Disjoint Chain Sets)

The SBCP spec enforces: no two SCP instances may overlap on the same chain. But instances on disjoint
chain sets can run concurrently. The publisher tracks active chains and only starts a new instance when
`can_start_instance()` returns true (no overlap with currently active chains).

In practice: if XT-1 involves chains {A, B} and XT-2 involves chains {C, D}, both can run simultaneously.
The publisher assigns sequential sequence numbers, but the sidecars for A/B and C/D process their
respective instances in parallel.

The sidecar's `chainLocks` (per-chain mutexes in `compose-sidecar/internal/coordinator/helpers.go`)
enforce sequential simulation within a single chain while allowing parallel simulation across different
chains. This is the correct granularity — Ethereum execution is sequential per-chain, but independent
chains have no state dependency.

#### 3.2 Sequential Execution Within a Chain

For a single chain, XTs are simulated one at a time in publisher order. The `chainOverlay` mechanism
accumulates state diffs between XTs:

1. XT-A simulates on builder state + prior overlay.
2. XT-A's `StateOverrides` are merged into the overlay.
3. XT-B simulates on builder state + overlay (now including A's effects).
4. And so on.

This guarantees that XT-B sees XT-A's post-state, matching what will happen during actual block execution.

#### 3.3 Multi-Transaction XTs

V2 XTs can contain multiple transactions per chain. Within a single XT, transactions are simulated
sequentially in the order provided. The simulation loop in `processXT()` iterates `txBytesList` in
order, merging state overrides between each tx. This handles cases like: approve token → swap →
bridge, all as a single atomic XT.

#### 3.4 What "Parallel" Does NOT Mean

Parallel does not mean concurrent EVM execution on the same chain. Ethereum's execution model is
inherently sequential per-chain (nonces enforce this). "Parallel" means:

- Multiple XT submissions accepted concurrently (queued by publisher).
- Disjoint-chain instances processed concurrently by their respective sidecars.
- The network-level throughput scales with the number of independent chain groups.

### 4. Simulation Mechanics

#### 4.1 Dual-Tracer Approach

The sidecar uses two EVM tracers per simulation:

- **`prestateTracer` (diff mode)**: Captures account balance, nonce, code, and storage changes.
  These become the `StateOverrides` for subsequent simulations. Implemented in
  `compose-sdk/simulation/simulator.go`.

- **`callTracer`**: Walks the call tree to identify mailbox operations. The parser
  (`compose-sdk/mailbox/parser.go`) detects `read()` and `write()` calls on the mailbox contract.
  Writes trigger CIRC messages to peer sidecars. Reads create dependencies that must be fulfilled
  before the XT can vote true.

Both tracers run against `debug_traceCall` with the builder-provided `state_overrides` merged with
the accumulated `chainOverlay`.

#### 4.2 CIRC (Cross-Rollup Communication) Flow

1. Chain A simulates XT tx, callTracer detects `mailbox.write(chainB, sessionID, label, data)`.
2. Sidecar A sends a `MailboxMessage` to sidecar B (via QUIC or HTTP).
3. Chain B simulates its XT tx, callTracer detects `mailbox.read(chainA, sessionID, label)`.
4. This creates a `CrossRollupDependency`. Sidecar B waits (up to `circTimeout`, default 10s)
   for the matching message.
5. Once received, sidecar B builds a `putInbox` transaction (signed by coordinator key) and
   re-simulates with the mailbox data injected via state overrides.
6. If all txs on all chains succeed, both sidecars vote true.

The dependency matching (`compose-sdk/mailbox/match.go`) checks all six fields: SourceChainID,
DestChainID, Sender, Receiver, SessionID, Label. Zero-value fields act as wildcards.

#### 4.3 State Override Composition

The sidecar builds simulation state from three layers:

1. **Builder overrides**: The in-progress EVM state from op-rbuilder (accounts, storage, code
   modified so far in this block).
2. **Chain overlay**: Accumulated state diffs from prior committed XTs in this block/flashblock.
3. **Mailbox overrides**: Computed storage slots for `putInbox` data
   (`compose-sdk/simulation/state_override.go`).

These are merged using `MergeStateOverrides()` which handles `state` vs `stateDiff` formats and
resolves conflicts by preferring the most recent layer.

### 5. Flashblocks and State Handling

#### 5.1 Flashblock Lifecycle

Each L2 block is divided into flashblocks (sub-blocks) built at regular intervals. The builder
(`op-rbuilder/crates/op-rbuilder/src/builders/flashblocks/payload.rs`) manages this via an async
timer that sends cancellation signals:

1. FCU arrives → block building starts, fallback block is built immediately.
2. Timer fires every `interval` → previous flashblock is finalized, new one starts.
3. At each flashblock boundary: sidecar is polled, XTs injected, then mempool txs fill remaining gas.
4. Flashblocks are broadcast via WebSocket to rollup-boost subscribers.
5. When block deadline is reached (or new FCU arrives), final block is sealed.

#### 5.2 State Continuity Across Flashblocks

The builder maintains a single `State<DB>` object across all flashblocks within a block. Each
flashblock's execution modifies this state. The `transition_state` tracks all dirty accounts and
storage slots since the block began. When the sidecar is polled at flashblock N, the state overrides
reflect all changes from flashblocks 0..N-1.

The sidecar's `chainOverlay` mirrors this: it is keyed by `(BlockNumber, FlashblockIndex)` and
resets when either changes. This ensures the overlay stays synchronized with the builder's state.

#### 5.3 FCU Lock Semantics

When the builder polls the sidecar and receives `hold: true`, it re-polls in a loop. During this
time, no mempool txs are executed. This is the builder-level lock — implemented outside op-geth
rather than inside it.

The builder's cancellation token hierarchy ensures correctness:

- `block_cancel`: Fires when a new FCU arrives, stopping all flashblock building.
- `fb_cancel`: Child token per flashblock, fires when the timer signals next flashblock.

If a new FCU arrives while the sidecar is holding, the block building stops cleanly. No stale
state leaks to the next block.

### 6. Delivery Guarantees and Safety

#### 6.1 Required Transaction Semantics

When the sidecar returns transactions with `required: true`, the builder must either execute them
successfully or fail the entire flashblock. In `execute_sidecar_transactions()`
(`op-rbuilder/crates/op-rbuilder/src/builders/context.rs`), any failure for a required tx — decode
error, signature recovery failure, invalid type, gas limit exceeded, EVM execution error, revert —
returns `Err(PayloadBuilderError)`, aborting the flashblock build.

This is a hard safety guarantee. There is no silent fallback where the builder skips an XT and
continues with an inconsistent block.

#### 6.2 Delivery Tracking

The sidecar tracks delivery state per XT per chain. When committed XT transactions are returned
in a poll response, they are immediately marked as delivered. Delivered XTs are not returned on
subsequent polls.

The pipeline is two-state: deliverable → delivered. All sidecar txs are `required: true`, so the
builder either executes all of them or the flashblock build fails entirely. There is no partial
execution case that would require re-delivery. Crash recovery is handled at the publisher level
via `HandleRollback` / `HandleStartPeriod`.

#### 6.3 Strict Mode (rollup-boost)

Rollup-boost is modified (~30 LOC per the sidecar docs) to disable fallback to op-geth when the
builder fails. Without this, a builder failure could cause rollup-boost to fall back to op-geth,
which would produce a block without the committed XT — breaking atomicity.

### 7. Performance Characteristics

#### 7.1 XT Latency

- **Op-geth approach**: XT latency was effectively unbounded because there was no reliable window
  to submit. The "happy path" failed because block building completed in milliseconds.
- **Sidecar approach**: XT latency is bounded by flashblock interval + 2PC round trip + CIRC
  dependency resolution. With 250ms flashblocks and typical network latency, ~300-500ms is
  achievable for the common case.

#### 7.2 Block Building Impact

- **Op-geth approach**: Required pausing the entire block building pipeline. If using TOB, the
  first portion of every block would be dedicated to XT processing, delaying mempool tx inclusion.
- **Sidecar approach**: Only the target flashblock is affected. Mempool txs flow in other
  flashblocks. If no XTs are pending, the builder gets an empty response immediately and proceeds
  normally — zero overhead.

#### 7.3 Throughput Scaling

- Disjoint chain sets processed in parallel (protocol-level parallelism).
- Per-chain throughput bounded by simulation time + CIRC latency per XT.
- The publisher's queue allows bursts of XT submissions without backpressure on the builder.
- Flashblock intervals can be tuned per deployment (faster intervals = more XT opportunities per
  block, at the cost of more poll overhead).

#### 7.4 Network Overhead

- Each flashblock boundary triggers one HTTP POST to the sidecar (minimal payload).
- State overrides can be large for blocks with many state changes, but this is bounded by the
  number of dirty accounts/slots — typically small for cross-chain operations.
- CIRC messages are small (mailbox message headers + data).

### 8. Assessment of parallel-xts-state.md

The document is technically sound. Key points that hold up against the codebase:

- **Definitions section**: Correct. XT, Instance, Period, Flashblock definitions match the
  specs and implementation.
- **Spec invariants**: Correctly identifies single-active-instance, global ordering, deterministic
  execution, mailbox correctness, and slot lifecycle.
- **v2.1 correctness fixes**: All documented fixes are verified in the codebase — CIRC key
  encoding, dependency matching, period transition abort, rollback handling, delivery tracking,
  CIRC timeout coordination, non-participant detection, cleanup loop, standalone fallback removal,
  block number regression detection.
- **v2.2 design decision rationale**: Correctly documents the resolutions to block building
  timing, `computependingblock` state consistency, TOB vs unordered, rollup synchronization,
  A/B/x interleaving, XT mempool, and the op-rbuilder choice.

One nuance worth noting: the document says "Process all XTs first (A, then B on top of A's state) —
the builder holds the lock throughout." In the actual implementation, the builder does not hold a
single lock for all XTs. It polls the sidecar per flashblock, and the sidecar returns whatever
committed XTs are ready. If XT-A is committed but XT-B is still in 2PC, the builder may execute
A in one flashblock and B in a later one. The sequential state guarantee is maintained by the
sidecar's `chainOverlay`, not by a single builder-side lock across all XTs. The end result is
the same (correct ordering), but the mechanism is more fine-grained than described.

### 9. Assessment of SIDECAR_FLASHBLOCKS_RATIONALE.md (Original Sections)

The six points in the original document are all supported by the codebase analysis. No corrections
needed. The tradeoffs section is honest and accurate.

### 10. Verdict: Sidecar+Flashblocks Is the Correct Architecture

The sidecar+flashblocks approach is not merely "better" than modifying op-geth — it is the only
approach that can satisfy the Compose protocol requirements without fundamental compromises:

1. **State correctness**: Builder-provided state overrides guarantee simulation == execution.
   Op-geth's `computependingblock` cannot provide this guarantee.

2. **Timing reliability**: Pull-based polling eliminates the nanosecond-window problem entirely.
   Push-based injection into op-geth is structurally unreliable.

3. **Spec compliance**: The architecture naturally maps to SBCP's period/instance model. The
   publisher is the sole ordering authority. No standalone fallback contradicts this.

4. **Separation of concerns**: Op-geth/reth handles EVM execution. Op-rbuilder handles block
   construction. The sidecar handles cross-chain coordination. The publisher handles ordering.
   Each component has a single responsibility.

5. **Maintainability**: The op-geth fork required deep modifications to `worker.go`, `miner.go`,
   and block building internals. The sidecar requires ~150 LOC in op-rbuilder (polling + state
   overrides + required tx execution) and ~30 LOC in rollup-boost (strict mode). The rest lives
   in isolated Go services.

6. **Performance**: Sub-second XT latency through flashblocks. Zero overhead when no XTs are
   pending. Parallel processing of disjoint chain sets. None of this was achievable with op-geth
   modifications.

The sidecar architecture is not a theoretical improvement — it was built because the op-geth
approach did not work.

### 11. Mempool Transactions During Parallel XT Simulation

A natural question: what happens when a new mempool transaction arrives while the sidecar is
simulating XTs in parallel across multiple chains?

#### 11.1 The Scenario

Chain A and Chain B builders are both polling the sidecar. The sidecar holds both builders (returning
`hold: true`) while it simulates an XT that spans both chains. Meanwhile, users submit regular
transactions to both chains' mempools. What happens to those transactions?

#### 11.2 Within a Single Flashblock: Strict Ordering

The builder's `build_next_flashblock()` in `payload.rs` (lines 684–952) enforces a fixed execution
order per flashblock:

```
1. Sequencer transactions    (from FCU PayloadAttributes)
2. Builder transactions      (MEV, operational)
3. Sidecar XTs               (from POST /transactions poll)
4. Pool transactions          (from mempool, via best_txs iterator)
```

While the builder is in step 3 (polling the sidecar, potentially in a `hold: true` loop), it has
not reached step 4. Mempool transactions sit in the tx pool untouched. They are not being executed,
not being simulated, and not modifying state. The builder is blocked waiting for the sidecar
response.

Once the sidecar returns (either committed XT txs or an empty response), the builder proceeds to
step 4. It calls `refresh_iterator()` to get a **fresh snapshot** of the mempool (`best_txs.rs`
line 36), then calls `execute_best_transactions()` to process pool txs against the post-XT state.

This means:

- Pool txs always execute **after** sidecar XTs within the same flashblock.
- Pool txs see the post-XT state (including XT state changes committed to `State<DB>`).
- There is no interleaving. The sequence is deterministic.

#### 11.3 Pool Transactions That Conflict with XTs

If a mempool transaction touches the same state as an XT (same contract, same storage slot, same
account balance), the pool tx executes on the post-XT state. Three outcomes:

1. **Pool tx still succeeds**: It runs correctly on the post-XT state. No problem — it just sees
   different storage values than it would have without the XT.

2. **Pool tx reverts**: The builder's `execute_best_transactions()` (`context.rs` lines 547–565)
   handles this gracefully. Reverted pool txs are either included (consuming gas but with no
   effect) or skipped via `mark_invalid()`, depending on whether they are bundle txs with
   revert-exclusion semantics. Either way, the block build continues. Pool tx reverts never fail
   the flashblock.

3. **Pool tx becomes invalid** (nonce too low, insufficient balance after XT drained funds): The
   builder catches `InvalidTxErr` (`context.rs` lines 505–519), skips the tx, optionally
   invalidates the sender's entire pending chain via `mark_invalid()`, and continues with the
   next tx.

This is fundamentally different from required XT semantics. For sidecar XTs with `required: true`,
any failure aborts the flashblock. For pool txs, failures are normal and handled by skipping.

#### 11.4 Across Flashblocks: State Propagation

If flashblock N includes sidecar XTs that modify state, and flashblock N+1 polls the sidecar again,
the state overrides sent to the sidecar in flashblock N+1 reflect **all** changes from flashblocks
0..N — including both XT effects and pool tx effects from earlier flashblocks.

This is because `build_state_overrides()` in `overrides.rs` reads the builder's full
`transition_state`, which accumulates across the entire block. The sidecar for flashblock N+1
simulates against the complete post-state of flashblock N, including any pool txs that ran in N.

Sequence across flashblocks:

```
Flashblock 0:  [sequencer txs] [builder txs] [XT-A]         [pool txs p1, p2, p3]
                                                ↓ state overrides include XT-A + p1,p2,p3 effects
Flashblock 1:  [sequencer txs] [builder txs] [XT-B on post-A+pool state] [pool txs p4, p5]
                                                ↓ state overrides include XT-A + XT-B + all pool effects
Flashblock 2:  [sequencer txs] [builder txs] [no XTs]       [pool txs p6, p7, p8]
```

#### 11.5 Parallel Simulation Across Chains

When the sidecar simulates an XT involving chains A and B, each chain's simulation runs against
that chain's builder-provided state overrides. Chain A's mempool has no effect on chain B's
simulation (they are independent EVM instances with independent state). The sidecar's per-chain
`chainLock` serializes simulation within a single chain, but chains A and B can simulate in
parallel.

Mempool txs on chain A that arrive during simulation do not affect chain A's simulation either,
because the simulation uses the state snapshot from the builder poll (the `state_overrides` in the
`PollRequest`). The builder has not executed any new pool txs since it sent that snapshot — it is
waiting for the sidecar response.

#### 11.6 The chainOverlay and Pool Tx Interaction

The sidecar's `chainOverlay` accumulates state diffs **only from committed XTs**, not from pool
txs. This is correct because:

- Within a flashblock, XTs execute before pool txs. The overlay tracks XT-to-XT state propagation.
- Across flashblocks, pool tx effects are captured in the builder's `state_overrides` (which
  reflect the full `transition_state` including pool tx effects from prior flashblocks).

So the layering is:

```
Builder state_overrides    =  parent block + all prior flashblock effects (XTs + pool txs)
  + chainOverlay           =  committed XT diffs within current flashblock
  + mailbox overrides      =  putInbox storage slot injections for CIRC dependencies
  ──────────────────────
  = simulation base state  (matches what the builder will execute against)
```

Pool txs from the current flashblock are not in any of these layers because they have not executed
yet at simulation time. Pool txs from prior flashblocks are in the builder's `state_overrides`
because the builder has already committed them to its `State<DB>`.

#### 11.7 Summary: Mempool Is Safe

No special handling is needed for mempool transactions during XT simulation. The architecture
guarantees safety through three properties:

1. **Ordering**: Pool txs always execute after XTs within each flashblock. No interleaving.
2. **Isolation**: The builder is blocked during sidecar polling. No pool txs execute concurrently.
3. **Propagation**: State from prior flashblocks (including pool tx effects) flows through
   builder-provided `state_overrides` on the next poll.

Pool txs that become invalid due to XT state changes are simply skipped by the builder — they do
not break the block or affect XT correctness. This is the standard Ethereum block builder behavior
for conflicting transactions.

---

## Summary

The sidecar+flashblocks architecture resolves every known problem with the op-geth approach:

| Problem (op-geth)                                                   | Resolution (sidecar+flashblocks)                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
|---------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Block builds in milliseconds, no window for SCP                     | **Pull model**: builder initiates by polling sidecar at each flashblock boundary (~250ms). The builder controls when to ask for XTs — no race condition, no timing window to hit. Every flashblock is a valid delivery point.                                                                                                                                                                                                                                                     |
| `computependingblock` state mismatch between simulation and sealing | Builder provides exact in-progress `State<DB>` as `state_overrides` via `build_state_overrides()` extracting dirty accounts, storage, balances, nonces, code from `transition_state.transitions`. Sidecar simulates on the real execution state, not a speculative pending block.                                                                                                                                                                                                 |
| A/B/x interleaving (XT inserted between mempool txs)                | Execution order is hardcoded in `build_next_flashblock()`: sidecar poll → execute sidecar XTs → `refresh_iterator` → `execute_best_transactions`. XTs always execute before pool txs within a flashblock. XTs cannot go after mempool txs within the same flashblock — the code path does not allow it. However, XTs in flashblock N+1 effectively follow pool txs from flashblock N, since each flashblock is independent.                                                       |
| Mempool txs during parallel simulation                              | Pool txs sit in mempool untouched while builder waits; executed after XTs on post-XT state. Pool txs invalidated by XT state changes are skipped via `mark_invalid()` — standard builder behavior.                                                                                                                                                                                                                                                                                |
| TOB requirement (synchronized block starts, XT time windows)        | Not required. XTs land at any flashblock within a period. **`chainOverlay`** — a per-chain state accumulator (`{BlockNumber, FlashblockIndex, Overlay}`) that tracks XT-to-XT state diffs within a single flashblock — provides sequential correctness. It resets on each new flashblock. When multiple XTs touch the same chain in one flashblock, each subsequent XT sees cumulative state changes from prior XTs via the overlay.                                              |
| Rollups must have same block time                                   | Not required. **`StartPeriod` synchronizes periods, not blocks.** A period spans multiple L2 blocks. The publisher broadcasts `StartPeriod` at Ethereum slot boundaries (~12s), and all sidecars align to the same period number. Individual chains can have different block times (250ms, 500ms, 1s) — the period is the coordination unit. Block-level differences are irrelevant because the sidecar tracks state per-flashblock within whatever block cadence the chain uses. |
| `CanIncludeLocalTx` / locking inside op-geth                        | Builder-level hold; sidecar returns `hold: true`, builder re-polls with configurable `max_retries` and `poll_after_ms`; no op-geth modification needed.                                                                                                                                                                                                                                                                                                                           |
| No happy path for testing                                           | **Pull model** guarantees a window exists: the builder always polls at flashblock boundaries, so there is always an opportunity to inject XTs. No timing luck required. Every ~250ms flashblock is an XT opportunity.                                                                                                                                                                                                                                                             |

**Parallel XT support** works at three levels:

- **Network level**: Disjoint chain sets process simultaneously (publisher `can_start_instance`).
  Multiple XTs touching different chains run in parallel with no coordination needed between them.
- **Chain level**: Per-chain simulation is sequential via `chainLock`. State diffs accumulate in
  `chainOverlay` — the per-chain overlay struct that merges XT state changes within a flashblock.
  The overlay ensures XT #2 on chain A sees the state diffs from XT #1 on chain A, even though
  both were simulated before either was executed by the builder.
- **Tx level**: Multi-tx XTs simulate sequentially with merged state overrides between txs.

**State correctness** is guaranteed by construction through three layers:

- **Builder `state_overrides`** from `transition_state` = exact in-progress EVM state. Extracted
  by `build_state_overrides()` which iterates all dirty accounts in `State<DB>`, capturing storage
  slot changes, balance updates, nonce increments, and deployed code.
- **`chainOverlay`** = accumulated XT diffs within the current flashblock. A per-chain struct
  (`{BlockNumber, FlashblockIndex, Overlay map[string]any}`) that resets when a new flashblock
  starts. It exists so that when multiple XTs touch the same chain in one flashblock, each
  subsequent simulation sees the cumulative state changes from all prior XTs in that flashblock.
- **`mailbox overrides`** = `putInbox` storage slot injections for CIRC dependency resolution.
- All three layers are merged before simulation. The result matches the state the builder will
  have when it executes the XT transactions.

**Performance**:

- XT latency: ~300-500ms (flashblock interval + 2PC + CIRC).
- Zero overhead when no XTs pending — builder polls, sidecar has nothing, builder continues.
- Pool txs flow normally in flashblocks without active XTs.
- Throughput scales with number of independent chain groups.
- Pull model eliminates the "no happy path" problem: the builder always has a valid window
  because it creates the window itself by polling.

**Why pull model matters**: In op-geth, the publisher had to push XTs into a block-building
process it did not control. Block building happened in milliseconds, and the push had to arrive
during that narrow window — a race condition with no reliable solution. The sidecar flips this:
the builder polls the sidecar at every flashblock boundary (~250ms). The builder is the initiator,
so there is no timing window to miss. If the sidecar has XTs ready, they are delivered. If not,
the builder continues with pool txs. This is why the sidecar approach has a happy path for
testing and production — the delivery mechanism is deterministic, not probabilistic.

The architecture works because it separates concerns cleanly: the publisher orders, the sidecar
coordinates and simulates, the builder constructs blocks with fine-grained state control, and
pool transactions flow naturally around XT processing without interference. Op-geth was never
designed for external orchestration of cross-chain consensus rounds. The sidecar approach avoids
fighting that architecture entirely.

---

## 12. Cross-Flashblock XT Flow: xt1 → tx1 → xt2 → tx2

Within a single flashblock, sidecar XTs always execute before pool txs. The flow
`xt1 → tx1 → xt2 → tx2` therefore spans two flashblocks:

### 12.1 Flashblock N

```
Builder polls sidecar (state_overrides from transition_state)
  → sidecar has xt1 committed (2PC decided) → returns xt1 raw txs
  → builder: execute_sidecar_transactions(xt1)
    → evm.transact(&tx) → evm.db_mut().commit(state)   [context.rs:784]
    → xt1 state changes are now in State<DB>.transition_state
  → builder: refresh_iterator → execute_best_transactions
    → tx1 (from pool) executes on post-xt1 state
    → tx1 state changes added to transition_state
  → build_block():
    → clone transition_state                             [payload.rs:1256]
    → merge_transitions into bundle_state                [payload.rs:1258]
    → build header, seal block, create flashblock payload
    → publish flashblock via websocket                   [payload.rs:893]
    → state.take_bundle() (clear bundle_state)           [payload.rs:1467]
    → restore transition_state from clone                [payload.rs:1468]
Result: flashblock N contains [xt1, tx1, ...].
        transition_state preserves ALL accumulated changes.
```

### 12.2 Between Flashblocks (concurrent)

While the builder was executing flashblock N, the sidecar may have been processing xt2
concurrently:

- Publisher sent `StartInstance` for xt2
- Sidecar acquired `chainLock`, simulated xt2 using `chainOverlay` (which includes xt1's
  simulation diffs from `applyCommittedOverrides`)
- Sidecar sent vote → publisher collected votes → 2PC decided commit
- `OnDecision(commit)` → `applyCommittedOverrides(xt2)` → chainOverlay updated with xt2 diffs
- xt2 is now deliverable

The sidecar's simulation of xt2 used the chainOverlay (xt1's simulated diffs), not the builder's
executed state. This is safe because simulation and execution produce the same state changes for
the same input — the sidecar simulated xt1 on the same `state_overrides` the builder executed
xt1 on, so the chainOverlay matches the builder's transition_state.

### 12.3 Flashblock N+1

```
Builder polls sidecar (state_overrides from transition_state — includes xt1 + tx1)
  → sidecar has xt2 committed → returns xt2 raw txs
  → builder: execute_sidecar_transactions(xt2) → transition_state updated
  → builder: refresh_iterator → execute_best_transactions
    → tx2 executes on post-xt2 state
  → build_block() → flashblock N+1 published with [xt2, tx2, ...]
Result: flashblock N+1 contains [xt2, tx2, ...].
```

### 12.4 State Propagation: Two Independent Paths

There are two parallel state propagation mechanisms that stay consistent:

| Path            | Location                     | Mechanism                                            | Purpose                                   |
|-----------------|------------------------------|------------------------------------------------------|-------------------------------------------|
| Sidecar overlay | `chainOverlay`               | `applyCommittedOverrides()` merges simulation diffs  | Subsequent XT *simulations* see prior XTs |
| Builder state   | `State<DB>.transition_state` | `evm.db_mut().commit(state)` after each tx execution | Subsequent XT *executions* see prior XTs  |

Both converge to the same state because xt1's simulation (used for overlay) and xt1's execution
(committed to transition_state) produce identical state changes when given the same input state.

### 12.5 What If xt2 Arrives in the Same Flashblock as xt1?

If both xt1 and xt2 are committed before the builder polls, `collectDeliverable` (delivery.go:24)
returns both in sequence order (`xtLess` sorting by period → sequence → id). The builder executes
them sequentially in `execute_sidecar_transactions`: xt1 commits to State<DB>, then xt2 executes
on post-xt1 state. Pool txs follow after both. The flow becomes `[xt1, xt2, tx1, tx2, ...]`
within a single flashblock.

---

## 13. State Finalization Timeline

State finalization is not a single atomic event. It happens in stages, each providing a
different guarantee:

### 13.1 Stage 1: Consensus Finalization (2PC Commit)

**Where**: `OnDecision()` (handlers.go:364) or `tryMakeDecision()` (helpers.go:156)
**What happens**:

- `xt.Decision = &decision` — XT marked as committed
- `applyCommittedOverrides(instanceID)` — merges XT's simulated state diffs into `chainOverlay`
- `signalWaiters()` — unblocks any builder polls waiting on this XT

**Guarantee**: All participating sidecars agree this XT should be included. The XT is now
deliverable to builders. The chainOverlay is updated so subsequent simulations see this XT's
state changes.

**State is NOT in the builder yet.** The builder hasn't executed anything — it only knows
the XT is ready when it next polls the sidecar.

### 13.2 Stage 2: Delivery (Sidecar → Builder)

**Where**: `HandleBuilderPoll()` → `collectDeliverable()` → `buildCommittedTransactions()`
**What happens**:

- `collectDeliverable` (delivery.go:24) walks entries in order, skipping:
    - Already delivered XTs
    - Aborted XTs — marked as delivered and skipped
    - Stops at first undecided XT — returns it as blocking
- Committed XTs with raw txs are collected as deliverable
- `buildCommittedTransactions` prepends putInbox txs for CIRC dependencies
- `markDelivered` flags XTs as delivered for this chain
- Response sent to builder with `hold: false` and tx list

**Guarantee**: The XT's raw transactions are delivered to the builder and marked as delivered
to prevent re-delivery.

**The sidecar only delivers after 2PC commit.** The builder never receives undecided XTs.
This is the key safety property — the builder cannot execute an XT that might later be aborted.

### 13.3 Stage 3: Execution Finalization (Builder)

**Where**: `execute_sidecar_transactions()` (context.rs:633)
**What happens**:

- For each XT tx: `evm.transact(&tx)` → `ResultAndState { result, state }`
- If required tx fails (decode, signature, limits, execution, revert) → flashblock build fails
- If successful: `evm.db_mut().commit(state)` (context.rs:784) — state changes committed to
  `State<DB>.transition_state`
- Receipt built and added to `info.receipts`
- Tx added to `info.executed_transactions`

**Guarantee**: The XT's state changes are in the builder's in-memory EVM state. All subsequent
transactions (pool txs and future flashblock XTs) execute on this updated state.

**`transition_state` persists across flashblocks.** The `build_block` function clones it
before merging into bundle_state, then restores it afterward (payload.rs:1256, 1468). This
means the next flashblock's `build_state_overrides()` includes this XT's effects.

### 13.4 Stage 4: Flashblock Publication

**Where**: `build_block()` → `ws_pub.publish(&fb_payload)` (payload.rs:893)
**What happens**:

- `merge_transitions(BundleRetention::Reverts)` — transitions become bundle_state
- Block header constructed with receipts_root, logs_bloom, state_root (may be zero if disabled)
- Block sealed, flashblock payload created
- Published via WebSocket to rollup-boost/subscribers
- `state.take_bundle()` — bundle_state cleared
- `state.transition_state = untouched_transition_state` — restored for next flashblock

**Guarantee**: Users and downstream services can see the XT in a flashblock. The flashblock
includes the XT's receipt and effects. However, the block is not yet final — the builder may
produce more flashblocks in this block.

### 13.5 Stage 5: Block Seal

**Where**: Final flashblock or `get_payload` from op-node
**What happens**: The full block (all flashblocks combined) is finalized with a computed state
root and submitted to the execution layer.

**Guarantee**: The block is valid from the execution layer's perspective. The state root
commits all XT effects.

### 13.6 Stage 6: L1 Settlement

**Where**: Superblock published to L1
**What happens**: The superblock (containing L2 blocks from multiple rollups) is posted to L1
with proof of cross-chain consistency.

**Guarantee**: Settlement finality. The XT effects are anchored on L1 and cannot be reverted
without an L1 reorg.

### 13.7 Summary: When Is State "Final"?

| Stage              | When                           | What is guaranteed                                        |
|--------------------|--------------------------------|-----------------------------------------------------------|
| 2PC Commit         | After all sidecars vote yes    | Protocol agrees XT will be included; chainOverlay updated |
| Delivery           | Next builder poll              | Builder receives raw XT txs; sidecar marks delivered      |
| Builder execution  | During flashblock construction | EVM state updated in transition_state; receipts generated |
| Flashblock publish | After build_block()            | Users see XT in flashblock; state not yet root-committed  |
| Block seal         | get_payload / final flashblock | Full block with state root; execution layer finality      |
| L1 settlement      | Superblock on L1               | Settlement finality; anchored to Ethereum                 |

The gap between "2PC commit" and "builder execution" is the delivery window. During this window,
the sidecar knows the XT is committed (chainOverlay updated), but the builder hasn't executed it
yet. The builder will execute it on the next poll because `collectDeliverable` returns committed
XTs and the txs are marked `required: true`.
