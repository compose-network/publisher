# AGENTS HANDBOOK

## Mission Overview
This repository provisions two **Compose rollups** ("Rollup A" and "Rollup B") that settle to the Hoodi L1 network. Compose extends the OP Stack with cross-rollup message passing, so the stack includes:
- `services/op-geth/` (stage branch of `ssvlabs/op-geth`, the execution client with Compose features),
- `services/rollup-shared-publisher/` (coordinates mailbox gossip and optional proofs), and
- the standard OP Stack services (op-node, op-batcher, op-proposer, op-deployer wrapper) running in Docker.

The goal of this project is to make it trivial for protocol engineers to:
1. Deploy or refresh two Compose rollups on Hoodi with minimal manual steps.
2. Spin them up locally via the `./local` wrapper (which drives Docker Compose under the hood) and test changes to the op-geth fork or shared publisher quickly.
3. Iterate on the surrounding services while remaining self-contained.

## Quickstart — Using the Stack
Refer to [README.md](./README.md) for end-user instructions on prerequisites, the local CLI, and the Docker Compose runtime layout. That document is the authoritative runbook for spinning up the environment.

## Repository Layout & Responsibilities
```
AGENTS.md                # You are here – workflow primer for contributors
README.md                # User-facing setup and usage instructions (.env, setup script, compose)
.env                     # Example environment values (funded Hoodi wallet, RPC URLs, chain IDs)
docker-compose.yml       # Defines the dual-rollup topology (two Compose rollups + shared publisher)

docker/
  op-deployer.Dockerfile # Fetches op-deployer v0.3.3 release binary
  op-node.Dockerfile     # Builds op-node from services/optimism
  op-batcher.Dockerfile  # Builds op-batcher from services/optimism
  op-proposer.Dockerfile # Builds op-proposer from services/optimism

scripts/
  lib.sh                 # Shared shell helpers (env loading, logging)
  setup.sh               # Full deployment + artifact export pipeline

services/
  op-geth/               # Local checkout of ssvlabs/op-geth (cloned automatically when missing)
  optimism/              # Local checkout of ethereum-optimism/optimism
  rollup-shared-publisher/ # Local copy of the shared publisher (synced from ../rollup-shared-publisher by default)

docs/optimism-guide.md   # Upstream tutorial used as inspiration/reference
```

External repositories **not committed** here (ignored via `.gitignore`):
- `services/op-geth/` — cloned automatically from `https://github.com/ssvlabs/op-geth` (stage branch).
- `services/rollup-shared-publisher/` — populated from `ROLLUP_SP_SOURCE` (defaults to `../rollup-shared-publisher`).
- `services/optimism/` — local checkout of the `ethereum-optimism/optimism` monorepo for op-node/op-batcher/op-proposer builds.
`setup.sh` will populate `services/` automatically when these directories are missing.

## Development Workflow
1. **Prepare dependencies**
   - Ensure Docker Engine + Compose v2, Python 3, and Git are available.
   - Clone `optimism` at the desired revision into `services/optimism` (or let `scripts/setup.sh` clone it on first run).
   - Make the private `rollup-shared-publisher` repository available at the path referenced by `ROLLUP_SP_SOURCE` (defaults to `../rollup-shared-publisher`); the setup script mirrors it into `services/rollup-shared-publisher`.
   - Populate `.env` with valid Hoodi RPC endpoints and a funded wallet. The same key/address are reused for mailbox signing and the shared publisher unless you override them explicitly.
   - Keep the Compose helper contracts bundle accessible (default `../old-contracts`). The setup script copies it into `./contracts` so Foundry can build and deploy from a writable path.
   - Optional: set `OP_GETH_PATH` / `ROLLUP_SHARED_PUBLISHER_PATH` in `.env` to point at existing source checkouts; when provided, the setup flow skips syncing and Docker builds use those paths directly.

2. **Deploy or refresh rollups**
   - Run `./local up`. On the first invocation it wraps `scripts/setup.sh` (stops any existing stack, wipes `state/`, `networks/`, and `contracts/`, deploys both rollups, builds images, hotpatches op-geth, and leaves the services running). Subsequent runs simply start the stack if artifacts already exist.
     - We currently pin the helper contracts to the legacy bundle under `../old-contracts` (see `.env: CONTRACTS_SOURCE`). The op-geth hotpatch, CLI samples, and tracer ABIs assume that layout; keep the folder in sync when editing contracts.
   - To avoid sending L1 transactions (e.g., CI or dry run), export `DEPLOYMENT_TARGET=calldata` before running `./local up`. Artifacts will still be generated using calldata output.
   - The setup pre-funds the `.env` wallet inside the L2 genesis allocs, can top up the account via OptimismPortal deposits, and deploys the Compose helper contracts from `./contracts` once the rollup RPCs are reachable. It wipes `state/`, `networks/`, and `contracts/` automatically each run.
   - `ROLLUP_PRAGUE_TIMESTAMP` / `ROLLUP_ISTHMUS_TIMESTAMP` control when Prague/Isthmus activate; defaults bake them in at genesis so op-node immediately speaks the V4 engine API. The setup also recomputes the execution genesis hash and patches `rollup.json` so the driver and engine always agree on the chain root (Go module/cache persisted under `.cache/genesis-go`, override via `GENESIS_HASH_CACHE_DIR`).
   - After contract deployment, helper addresses (Mailbox, PingPong, MyToken, Bridge) are written back into op-geth’s tracer and sample CLIs. The setup logs `[setup] helper contract hotpatch: updated` when it applies the patch. This is temporary until the upstream repos expose proper configuration hooks.
   - Deposits (if enabled) ride the live Hoodi L1. Expect several minutes for balances to update; adjust `ROLLUP_DEPOSIT_WAIT_*` if you need a longer polling window.

3. **Operate the local stack**
   - Use `./local status` to confirm container health, publisher readiness, and the latest L2 block numbers (1–2 s timeouts so failures return quickly).
   - Endpoints:
     - Shared publisher: TCP `18080`, HTTP `18081`.
     - Rollup A: HTTP `18545`, WS `18546`, auth `18551`, compose mailbox `19898`, op-node RPC `19545`, batcher `18548`, proposer `18560`.
     - Rollup B: mirrored on ports `28545/28546/28551/29898/29545/28548/28560`.
     - Blockscout explorers: Rollup A `19000`, Rollup B `29000`.

4. **Iterate on op-geth or other services**
   - Modify code under `services/op-geth/`, `services/rollup-shared-publisher/`, or `services/optimism/` (or the directories pointed to by `OP_GETH_PATH` / `ROLLUP_SHARED_PUBLISHER_PATH`).
   - Rebuild and restart targeted services with `./local deploy op-geth`, `./local deploy publisher`, `./local deploy blockscout`, or `./local deploy all` (uses short RPC waits to confirm readiness).
   - Need to redeploy contracts after changing them? Update the sources and rerun `./local up` (or invoke `scripts/setup.sh` directly) to broadcast the new bytecode.

5. **Resetting the environment**
   - Run `./local down` to stop containers while keeping volumes.
   - Use `./local purge --force` for a clean slate (`state/`, `networks/`, `contracts/`, `.cache/genesis-go`). Follow with `./local up` to redeploy.
   - The shared publisher keeps no persistent volume; `./local deploy publisher` is enough to pick up source changes.

## Testing & Verification Tips
- **Rollup liveness**: query `eth_blockNumber` on ports `18545` and `28545`. Block numbers should advance every few seconds; `docker logs op-node-{a,b}` also shows rollup progress.
- **Compose mailbox**: tail `docker logs op-geth-{a,b}` for messages referencing `compose` or mailbox connections. You should see successful dials to `rollup-shared-publisher:8080` and cross-chain gossip over port `9898`.
- **Bridge sanity**: run `go run ./cmd/mint` once per redeploy to seed L2 funds, then `go run ./cmd/xbridge`. A healthy run mints 100 `MyToken` on Rollup B, burns the same amount on Rollup A, and `./toolkit.sh debug-bridge` shows matching `SEND`/`ACK SEND` mailbox entries.
- **Shared publisher health**: `curl http://localhost:18081/health` should return `200`; readiness requires at least one connected sequencer.
- **Blockscout health**: `curl http://localhost:19000/api/health` and `http://localhost:29000/api/health` should return `200` once indexing catches up.
- **Accounts**: the sequencer key is pre-imported during container bootstrap. `eth_accounts` on either rollup should return the wallet address from `.env`.
- **Funding**: the Hoodi wallet must remain funded; script deploys multiple contracts and the batcher/proposer continue to submit transactions. Monitor balance via the L1 RPC to avoid failures.
- **op-deployer cache**: stored in `state/op-deployer/.cache`. Safe to delete between runs if you need to refresh artifacts.

## Extending the Repository
- **Scripts**: add shared shell helpers to `scripts/lib.sh`; hook into `setup.sh` for deployment changes.
- **Docker images**: update the Dockerfiles under `docker/` when you need different build flags or upstream revisions. They consume the local `services/optimism` checkout, so align branches accordingly.
- **Compose topology**: modify `docker-compose.yml` to introduce additional services (explorers, RPC proxies, etc.). Keep port assignments documented.
- **Shared publisher**: tweak runtime behaviour by adjusting the `rollup-shared-publisher` service env vars. For deeper changes, edit the copied sources under `services/rollup-shared-publisher` and rebuild the image.
- **L1 placeholders**: the shared publisher accepts dummy L1 contract values for local testing. Expect warnings about missing notifications until a real contract is deployed.
- **Documentation**: update `README.md` for user-facing changes and expand this `AGENTS.md` when process evolves.

## Support Checklist for New Agents
1. Confirm `.env` has valid Hoodi endpoints and a funded private key.
2. Ensure the service checkouts exist (`services/optimism`, `services/op-geth`, `services/rollup-shared-publisher`); `scripts/setup.sh` will populate them automatically when they are missing.
3. Run `./local up` (set `DEPLOYMENT_TARGET=calldata` in the environment if L1 access is unavailable).
4. Confirm the stack is healthy via `./local status` (should report advancing blocks and a `200` publisher health code).
5. Inspect `networks/rollup-*/contracts.json` for the deployed helper addresses. Both rollups receive the exact same Mailbox, PingPong, MyToken, and Bridge addresses.
6. Iterate on code and rebuild targeted services with `./local deploy <target>`.
7. Share updates by amending README/AGENTS with any notable changes.

## Workflow Toolkit
- `toolkit.sh` lives at the repo root and wraps the common maintenance loops. Run `./toolkit.sh help` to see the command list. The helpers delegate to the python diagnostics in `scripts/debug_bridge.py` and avoid bespoke one-liners.
- Commands:
  - `deploy-op-geth` – rebuild both op-geth images, restart the execution stacks (including op-node/batcher/proposer), and wait for both RPCs to answer `eth_blockNumber`.
- `debug-bridge [--blocks N --session ID --since 2m]` – scan recent blocks for Mailbox writes/ACKs, fetch shared publisher stats, and tail compose-specific log lines.
- `check-bridge` – fast health check with balances, block heights, and shared publisher status (suitable after restarts).
- `clear-nonces` – restarts the op-geth containers to flush their txpools when CLI experiments get stuck on nonce reuse.
- `toolkit.env` can override RPC endpoints or stats URLs; keep an up-to-date helper bundle in `./contracts` (refreshed via `CONTRACTS_SOURCE`) when iterating on helper contracts.
- Configuration: the scripts read `.env` first and then (optionally) `toolkit.env` for overrides. Drop custom RPC endpoints or stats URLs in `toolkit.env` so they survive `.env` refreshes from upstream.
- Prerequisites: Python 3 is already required for `setup.sh`. The toolkit only depends on the standard library plus Docker/Compose and Foundry’s `cast` container (pulled automatically when invoked).

Welcome aboard—this handbook should give you enough context to resume work instantly. Dive into `README.md` next for end-user execution details, and happy hacking!
