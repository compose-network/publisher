# Compose Dual-Rollup Local Stack

This repository spins up two **Compose** rollups ("Rollup A" and "Rollup B") that settle to the Hoodi L1 network. Compose extends the OP Stack with cross-rollup communication via the `rollup-shared-publisher`, and this environment is tuned for iterating on the [`ssvlabs/op-geth`](https://github.com/ssvlabs/op-geth) fork and the shared publisher with minimal fuss.

## Prerequisites

- Docker 27+
- Docker Compose v2 (bundled with Docker Desktop / Engine)
- Python 3 (used by the helper script)
- Git (the setup flow clones the op-geth fork)
- A funded Hoodi L1 account (private key is read from `.env`)
- Access to the private `rollup-shared-publisher` repository (mirrored locally at `../rollup-shared-publisher` for now)

All configuration lives in [`.env`](./.env). The defaults expect:

- `HOODI_CHAIN_ID`, `HOODI_EL_RPC`, `HOODI_CL_RPC` pointing at the Hoodi L1 RPC endpoints
- `WALLET_PRIVATE_KEY` / `WALLET_ADDRESS` for the funded deployment wallet (re-used for batcher, proposer, sequencer)
- `ROLLUP_A_CHAIN_ID` / `ROLLUP_B_CHAIN_ID` for the two L2s
- `SP_L1_SUPERBLOCK_CONTRACT`, `SP_L1_SHARED_PUBLISHER_PK_HEX`, `SP_L1_FROM_ADDRESS` to tune the shared publisher's optional L1 integration (safe to leave as placeholders for local-only testing). The shared publisher reuses `HOODI_EL_RPC` for its L1 RPC by default.

## First-Time Setup

1. Copy `.env.example` to `.env`, then fill in the required Hoodi RPC endpoints plus your funded L1 wallet (`WALLET_PRIVATE_KEY`/`WALLET_ADDRESS`). Leave the optional knobs alone for the first pass.
2. Run `./local.sh up`. On the first invocation it wraps `scripts/setup.sh`: the setup stops any existing Compose stack, wipes `state/`, `networks/`, and `contracts/`, clones/syncs the required repositories, deploys both rollups to Hoodi, builds the images, deploys helper contracts, applies the op-geth hotpatch, and leaves the services running on ports `18545/28545`.
   - Ensure the Compose helper bundle exists at `CONTRACTS_SOURCE` (defaults to `../old-contracts`). The setup copies it into `./contracts` automatically. Set `DEPLOY_CONTRACTS=0` if you want to skip helper deployment entirely.
   - The shared publisher reuses the funded Hoodi wallet by default (`SP_L1_SHARED_PUBLISHER_PK_HEX`/`SP_L1_FROM_ADDRESS`). Update those fields alongside `WALLET_PRIVATE_KEY`/`WALLET_ADDRESS` if you swap accounts.
3. Run `./local.sh status` to verify the shared publisher is healthy and that both rollup RPCs return advancing block numbers.

When the script finishes you have two live rollups plus the shared publisher. Optional health checks (`./toolkit.sh check-bridge`, `curl http://localhost:18081/health`, etc.) are great follow-ups once both rollups are producing blocks.

## Local CLI

`./local.sh` is the operator-facing entry point once the environment is bootstrapped:

- `./local.sh up` — bootstrap on first run (delegates to `scripts/setup.sh`) or start the stack if artifacts already exist.
- `./local.sh status` — print container state, exposed RPC endpoints, shared publisher health, and the latest L2 block numbers with snappy 2 s timeouts.
- `./local.sh restart <op-geth|publisher|all>` — restart targeted services without rebuilding images and wait for RPC readiness (4-minute default timeout, override with `RPC_RETRIES`/`RPC_DELAY`).
- `./local.sh deploy <op-geth|publisher|all>` — rebuild the relevant images before reusing the restart flow (handy after editing `op-geth` or the shared publisher).
- `./local.sh down` — stop containers while leaving volumes intact.
- `./local.sh purge [--force]` — tear everything down, remove volumes, and delete generated artifacts (`state/`, `networks/`, `contracts/`, `.cache/genesis-go`). Follow with `./local.sh up` for a clean redeploy.

Legacy `scripts/setup.sh` remains available for advanced automation, but day-to-day iteration should flow through `./local.sh`.

## One-Time Deployment & Genesis

The initial deployment (contract deployment on Hoodi + generating rollup artifacts) is handled by [`scripts/setup.sh`](./scripts/setup.sh). The script now also prepares the local sources required to build the Compose stack. It will:

1. Clone `optimism/` at `OPTIMISM_REF` (defaults to `op-node/v1.13.4`) if the directory is missing. Existing checkouts are left untouched so you can manage branches manually.
2. Clone `op-geth/` from `https://github.com/ssvlabs/op-geth` on the `stage` branch when absent. If you already have a checkout, the script will log that it is reusing it.
3. Copy `../rollup-shared-publisher` into `./rollup-shared-publisher/` the first time it runs (unless `ROLLUP_SP_SKIP_SYNC=1`). This is a temporary workaround until the repository can be cloned directly.
4. Build a lightweight `op-deployer` image from the local [`optimism`](./optimism) repo (or reuses `OP_DEPLOYER_IMAGE` if set).
4. Run `op-deployer` with a custom intent to deploy two chains that use the wallet from `.env` for all operational roles.
5. Write per-rollup artifacts to `networks/rollup-a` and `networks/rollup-b`:
   - `genesis.json` for `op-geth`
   - `rollup.json` for `op-node`
   - `jwt.txt` shared between the engine / node for auth
   - `password.txt` used to import the sequencer key into `op-geth`
   - `addresses.json` and `runtime.env` with deployed contract addresses (SystemConfig, dispute game factory, bridges)
   - Align `pragueTime` / `isthmusTime` between configs using `ROLLUP_PRAGUE_TIMESTAMP` / `ROLLUP_ISTHMUS_TIMESTAMP` (both default to activating at genesis).
   - Patch the exported `rollup.json` with the execution-layer genesis hash computed from the final `genesis.json` so op-node and op-geth agree on the chain root. Module downloads for this helper are cached under `.cache/genesis-go` (override with `GENESIS_HASH_CACHE_DIR`).
   - Hotpatch op-geth’s tracer/CLI samples (ping-pong, bridge, mint) with the freshly deployed Compose helper contract addresses so local tooling targets the correct contracts. The script prints a `[setup] helper contract hotpatch` line when it updates the sources. Once upstream supports CLI/ENV injection this step will be removed.
6. Pre-fund the `.env` wallet inside the L2 genesis allocs (see `ROLLUP_ACCOUNT_GENESIS_BALANCE_WEI`).
7. (Optional) Top up the deployed rollups by submitting deposits through each OptimismPortal when the balance drops below `ROLLUP_ACCOUNT_MIN_BALANCE_WEI`.
8. (Optional) Deploy the Compose helper contracts synced into `./contracts` to both rollups using Foundry. Successful deployments store the resulting addresses alongside the other artifacts and persist them to `networks/rollup-*/contracts.json`.
9. Build and start the Docker Compose stack, wait for the rollup RPCs to answer `eth_blockNumber`, and hotpatch op-geth’s helper addresses.

Run it once after updating `.env`:

```bash
./scripts/setup.sh
```

> ⚠️ The script sends real transactions to Hoodi via `op-deployer apply`. Make sure the wallet in `.env` is funded.

> ℹ️ The setup script builds and starts the Compose stack automatically. If the rollup RPCs fail to come up before the timeout, the script exits with an error—inspect `docker compose logs` and rerun once the issue is resolved (or set `DEPLOY_CONTRACTS=0` to skip helper deployment).

> ℹ️ The script preloads the `.env` wallet into the L2 genesis allocs. Deposits are only used when you configure a non-zero `ROLLUP_ACCOUNT_DEPOSIT_WEI` to top up an existing network. If you enable deposits, remember they ride the live Hoodi L1—depending on network conditions the script may need several minutes while it waits for the L2 balance to reflect the new funds. Tweak `ROLLUP_DEPOSIT_WAIT_ATTEMPTS` / `ROLLUP_DEPOSIT_WAIT_DELAY` if you expect longer finality windows.

To do a dry-run that only produces calldata and artifacts (without publishing to L1), set `DEPLOYMENT_TARGET=calldata` when invoking the script. Combine it with `DEPLOY_CONTRACTS=0` if you do not want to broadcast the Compose helper contracts.

Each invocation wipes the previous deployment and brings up a fresh stack. Rerun it after changing `.env` or if you need to regenerate artifacts; take backups if you want to preserve the prior state.

## Running the Dual Rollup Stack

The setup script leaves the stack running. If you stop it later (`docker compose down -v`), bring it back with:

```bash
docker compose up --build -d
```

To stop the stack without losing state, run `./local.sh down`. For a full reset (containers, volumes, and generated artifacts) use `./local.sh purge`.

Services per rollup:

| Service                  | Ports (host → container) | Description |
|--------------------------|--------------------------|-------------|
| `rollup-shared-publisher`| 18080 → 8080 (TCP), 18081 → 8081 (HTTP) | Compose coordination layer. Runs from the local copy of `rollup-shared-publisher` with proofs disabled for fast local iteration. |
| `op-geth-a/b`            | 18545/28545 → 8545 (HTTP), 18546/28546 → 8546 (WS), 18551/28551 → 8551 (auth), 19898/29898 → 9898 (Compose mailbox) | Execution clients built from the `ssvlabs/op-geth` fork. They connect to the shared publisher via `--sp.addr`, expose mailbox listeners on port 9898, and include the Compose-specific APIs. |
| `op-node-a/b`            | 19545/29545 → 9545       | Consensus client compiled from the local `optimism` repo (sequencer mode enabled, P2P disabled). |
| `op-batcher-a/b`         | 18548/28548 → 8548       | Local build of `op-batcher`, posting batches to Hoodi with the shared wallet. |
| `op-proposer-a/b`        | 18560/28560 → 8560       | Local build of `op-proposer`, submitting to the dispute game factory. |

The setup script already deploys the Compose helper contracts and stores their addresses under `networks/rollup-*/contracts.json`. Rerun the script whenever you need a fresh environment—it will wipe the previous deployment and complete the full pipeline again.

Because the `op-geth` and `rollup-shared-publisher` images are built from local sources, any code changes you make under `./op-geth` or `./rollup-shared-publisher` are picked up on the next `./local.sh deploy op-geth` or `./local.sh deploy publisher` (or `all`). Set `OP_GETH_PATH` / `ROLLUP_SHARED_PUBLISHER_PATH` in `.env` if you want to build from an external checkout instead of the copies in this repository.

Compose-specific knobs worth knowing:

- The shared mailbox ports (9898) let the two rollups gossip cross-chain messages. Adjust `--sequencer.addrs` in `docker-compose.yml` if you add more rollups.
- `--sequencer.key` is wired to the funded wallet key. Override `docker-compose.yml` manually if you need a dedicated mailbox signer.
- The shared publisher runs with proofs disabled and no L1 connection. Set `PROOFS_ENABLED=true` and populate the `L1_*` variables if you want to exercise the proving pipeline locally.

### Workflow Toolkit

- `toolkit.sh` wraps the high-signal operational loops. Run `./toolkit.sh help` for the full command list.
- Primary commands:
  - `deploy-op-geth` – rebuild both execution images, restart op-geth/op-node/op-batcher/op-proposer, and wait until the rollup RPCs answer `eth_blockNumber`.
  - `debug-bridge [--blocks N --session ID --since 2m]` – invokes `scripts/debug_bridge.py` to grab mailbox activity, shared publisher stats, and recent Compose log snippets.
  - `check-bridge` – quick health check that reports balances, block heights, and publisher status.
  - `clear-nonces` – restarts the op-geth containers to flush their txpools when CLI experiments trip over reused nonces.
- Configuration layering: `.env` provides defaults, while an optional `toolkit.env` (see `toolkit.env.example`) can override RPC URLs or the stats endpoint without conflicting with upstream `.env` updates.
- Requirements: Python 3 (standard library only), Docker/Compose, and internet access the first time Foundry’s `cast` container is pulled. All other dependencies ship with the repo.

### Resetting / Redeploying

- Stop everything but keep volumes: `./local.sh down`.
- Start fresh: `./local.sh purge` followed by `./local.sh up` (the first command stops/remove containers and deletes `state/`, `networks/`, `contracts/`, and `.cache/genesis-go`).
- Need a quick bounce after editing code? Use `./local.sh restart <op-geth|publisher|all>`.
- Need to rebuild images after code changes? Run `./local.sh deploy <op-geth|publisher|all>`; it rebuilds the relevant images and waits for RPC readiness.
- The shared publisher keeps no persistent state; a targeted `./local.sh deploy publisher` is enough to pick up configuration or code changes.

## Customization Notes

- Override the `op-deployer` image by exporting `OP_DEPLOYER_IMAGE` before running the setup script.
- `docker-compose.yml` builds `op-node`, `op-batcher`, and `op-proposer` from the checked-out `optimism` repo so you stay compatible with the deployed contracts.
- Each rollup uses the same wallet/key for sequencer, batcher, proposer, mailbox, and admin roles (matching the project requirements). Update the intent generation inside `scripts/setup.sh` if you want unique keys per role.
- To inspect the deployed contract addresses, check `networks/rollup-*/addresses.json` after the setup script runs.
- The Compose helper contracts are currently sourced from the legacy bundle in `../old-contracts` (see `.env`). Replace the contents of `./contracts` or override `CONTRACTS_SOURCE` if you want to deploy a different suite, and tweak `ROLLUP_A_RPC_URL` / `ROLLUP_B_RPC_URL` if your rollups are exposed elsewhere. Toggle the behaviour via `DEPLOY_CONTRACTS=0/1`, override the Foundry image with `FOUNDRY_IMAGE`, or isolate caches with `FOUNDRY_HOME_DIR`. Deposits can be tuned with `ROLLUP_ACCOUNT_MIN_BALANCE_WEI`, `ROLLUP_ACCOUNT_DEPOSIT_WEI`, `ROLLUP_DEPOSIT_GAS_LIMIT`, and the wait parameters documented in `.env`.
- Tweak the shared publisher runtime by editing its environment variables under the `rollup-shared-publisher` service in `docker-compose.yml`.
- For purely local testing you can point `SP_L1_SUPERBLOCK_CONTRACT` at a dummy address. The shared publisher still boots and coordinates mailbox traffic, but L1 event watching will log warnings until a real contract exists.

## Repository Layout

```
.
├── docker-compose.yml         # runtime topology for both rollups + shared publisher
├── docker/
│   ├── op-deployer.Dockerfile # builder image for op-deployer
│   ├── op-node.Dockerfile     # builds op-node from ./optimism
│   ├── op-batcher.Dockerfile  # builds op-batcher from ./optimism
│   └── op-proposer.Dockerfile # builds op-proposer from ./optimism
├── scripts/
│   ├── lib.sh                 # shared shell helpers
│   └── setup.sh               # automated deploy + artifact generation
├── optimism/                  # pinned optimism monorepo clone
├── op-geth/                   # clone of https://github.com/ssvlabs/op-geth (stage branch)
├── rollup-shared-publisher/   # local copy of the shared publisher until cloning is supported
└── docs/optimism-guide.md     # reference material
```

Happy hacking!
