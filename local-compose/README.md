# Compose Dual-Rollup Local Stack

This project spins up two **Compose** rollups ("Rollup A" and "Rollup B") that settle to the Hoodi L1 network. It lets you deploy fresh rollups, run them locally, and iterate on the `ssvlabs/op-geth` fork and the shared publisher without leaving your machine.

## Setup

**Prerequisites**
- Docker 27+ with Compose v2
- Python 3
- Git
- A funded Hoodi L1 wallet and EL & CL RPC endpoints

**Steps**
1. Copy `.env.example` to `.env` and fill in:
   - `HOODI_EL_RPC`
   - `HOODI_CL_RPC`
   - `WALLET_PRIVATE_KEY` / `WALLET_ADDRESS`
2. Run `./local up`.
   - The first run wipes any existing artifacts, clones/updates `optimism` and `op-geth` into `services/`, mirrors the shared publisher from `ROLLUP_SP_SOURCE` (defaults to `../rollup-shared-publisher`) into `services/rollup-shared-publisher`, deploys both rollups to Hoodi, builds the Docker images, deploys the helper contracts, and brings the stack online.
   - Subsequent runs detect the generated artifacts and simply start the containers.
3. Confirm the stack is healthy with `./local status`. You should see advancing block numbers for both rollups, a `200` shared publisher health check, and live Blockscout explorers for each chain.

> Need an offline or CI-friendly run? Set `DEPLOYMENT_TARGET=calldata` in your environment before calling `./local up`; artifacts are produced without sending L1 transactions.

## Iterate on Code Changes

- Rebuild and restart specific components with `./local deploy <op-geth|publisher|blockscout|all>`.
- Set `OP_GETH_PATH` or `ROLLUP_SHARED_PUBLISHER_PATH` in `.env` if you want to build from external checkouts instead of the defaults (`./services/op-geth` and `./services/rollup-shared-publisher`).
- After the stack is running, `./toolkit.sh check-bridge` gives a quick health snapshot (balances, block heights, publisher status).

## Command Cheat Sheet

- `./local status` – show container states, RPC endpoints, and latest L2 blocks.
- `./local down` – stop containers without touching volumes or artifacts.
- `./local deploy blockscout` – restart the explorer + backing services for both rollups.
- `./local deploy all` – rebuild op-geth and the shared publisher images, restart everything, and wait for RPC readiness.
- `./local purge --force` – stop the stack, remove volumes, and delete generated artifacts for a clean redeploy.
- `./toolkit.sh debug-bridge` – inspect recent cross-rollup mailbox activity.

## Customize Later

All additional knobs live in `.env`. Notable toggles:
- `DEPLOY_CONTRACTS=0` to skip helper contract deployment.
- `ROLLUP_ACCOUNT_DEPOSIT_WEI` and friends to control automatic L1 deposits.
- `OP_GETH_PATH` / `ROLLUP_SHARED_PUBLISHER_PATH` / `CONTRACTS_SOURCE` to point at alternate source trees (defaults are `./services/op-geth`, `./services/rollup-shared-publisher`, and `./contracts`).
- `ROLLUP_A_CHAIN_ID` / `ROLLUP_B_CHAIN_ID` if you need different test chain IDs (defaults are 77771/77772).

For deeper operational notes, consult `AGENTS.md` (contributor workflow) and `docs/optimism-guide.md` (background reference).

## Blockscout Explorers

The stack now ships a Blockscout instance per rollup:

- Rollup A explorer: `http://localhost:19000`
- Rollup B explorer: `http://localhost:29000`

Each explorer is pre-configured with the rollup RPC, Hoodi L1 RPC, and the appropriate SystemConfig / helper contracts via `networks/rollup-*/blockscout.env`. The UI is served by an Nginx proxy that also forwards `/api` to the backend, so the REST API stays available at the same base URL. Logs for all explorer components live behind the `blockscout` alias (`./local logs blockscout`).
