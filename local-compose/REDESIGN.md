# Local Compose Redesign

## 1. Current Flow (as implemented in `scripts/setup.sh` + `./local`)

### 1.1 Prerequisites & Environment
- Load `.env` and `toolkit.env` (if present) to populate Hoodi RPC endpoints, wallet keys, chain IDs, etc.
- Validate required variables (`HOODI_EL_RPC`, `HOODI_CL_RPC`, `WALLET_PRIVATE_KEY`, `WALLET_ADDRESS`).
- Derive defaults for sequencer credential fallbacks, contract bundle location, foundry image, etc.

### 1.2 Workspace Reset & Repo Sync
1. Stop any existing Compose stack via `docker compose down -v`.
2. Delete `state/`, `networks/`, and Foundry subdirectories (`contracts/{artifacts,broadcast,cache,out,lib}`).
3. Ensure `services/` exists.
4. Clone or reuse repositories:
   - `services/optimism` (specific ref `op-node/v1.13.4`).
   - `services/op-geth` (branch `feat/configurable-addresses`).
   - Copy shared publisher checkout from `ROLLUP_SP_SOURCE`.
   - Sync contract bundle into `contracts/`.
5. Seed placeholder contract metadata in `networks/rollup-{a,b}/contracts.json`.

### 1.3 op-deployer Preparation & Apply
1. Build `local/op-deployer:dev` image from release tarball if missing.
2. Initialize op-deployer state once (`state/op-deployer/state.json`).
3. Write `intent.toml` describing both rollups, wallet roles, etc.
4. Run `op-deployer apply` (serial, handles both rollups) to build chain config and deposit plan.
5. Mutate op-deployer outputs to pre-fund L2 allocs and align hard-fork timestamps.
6. Export artifacts (`genesis.json`, `rollup.json`, `addresses.json`, runtime env files) per rollup using `op-deployer inspect` and helper Python scripts.
7. Compute execution genesis hash via standalone Go program executed in Docker.

### 1.4 Docker Image Builds
- `docker compose up --build -d` for core services (rebuilds every image on each setup run):
  - `rollup-shared-publisher` (Python/Go build via Dockerfile).
  - `op-geth` (Go build).
  - `op-node`, `op-batcher`, `op-proposer` (three separate Go builds against `services/optimism`).

*Note:* Builds are triggered only after op-deployer completes, and they run serially because Compose handles each service one after another.

### 1.5 Runtime Bootstrap & Contract Deployment
1. After containers start, wait for RPC readiness on both rollups (poll `eth_blockNumber`).
2. Optional delay (default 10 s) for indexing catch-up.
3. Optionally fund wallet/sequencer via L1 deposits (only if deposit amount > 0).
4. Install Foundry dependencies (`forge-std`, `openzeppelin-contracts`, `openzeppelin-contracts-upgradeable`) from scratch every run.
5. Build and broadcast Foundry scripts for helper contracts on each rollup.
6. Copy artifacts into `networks/rollup-*/contracts.json`, verify shared addresses, and hotpatch op-geth tracer config.
7. Restart op-geth/op-node containers with mailbox settings.

### 1.6 Post-processing & Optional Services
- Generate Blockscout env/frontend configs and nginx proxies.
- (Currently disabled) start Blockscout services.
- Emit final log summary.

### 1.7 Current `./local` Wrapper Capabilities
- `up`: runs `scripts/setup.sh` on first use, otherwise `docker compose up -d`.
- `status`: aggregates `docker compose ps`, RPC calls, and health checks into ASCII panels.
- `logs`, `restart`, `deploy`, `purge`: orchestrate docker compose commands with custom targeting, plus workspace cleanup for `purge`.
- Numerous helpers for formatting output, waiting on RPCs, and removing paths.

## 2. Pain Points & Simplification Opportunities

1. **Monolithic Bash**: ~1,200 lines mixing Docker orchestration, JSON manipulation, and Python snippets. Hard to reason about and extend.
2. **Repeated Go compiles**: separate Dockerfiles for node/batcher/proposer duplicate `go build` work.
3. **Eager workspace wipes**: `reset_workspace` nukes Foundry libs/artifacts even when unnecessary, forcing fresh installs.
4. **Blocking operations**: `docker compose up --build -d` occurs after op-deployer, preventing overlap of the longest tasks.
5. **Inlined Python scripts**: dozens of `python3 - <<'PY'` blocks complicate error handling and readability.
6. **State mutation spread out**: JSON/TOML editing scattered across the script; hard to test isolated pieces.
7. **Limited reuse**: `./local` shell logic isn’t portable; each command reimplements formatting and parameter parsing.
8. **Minimal validation**: errors from background processes or docker builds are easy to miss; logs aren’t structured.

## 3. Goals for the Rewrite

- **Clarity first**: readable, modular code with explicit dependencies and minimal side effects.
- **Task-based structure**: model each setup phase as a composable function with typed inputs/outputs.
- **Selective behaviour**: make expensive steps opt-in/conditional (e.g., skip repo resync if path exists, reuse Foundry cache).
- **Concurrency where safe**: overlap independent phases (repo sync, docker builds) without sacrificing determinism.
- **Extensibility**: easy to add new subcommands or adjust flow without touching a monolithic script.
- **Testability**: ability to dry-run or unit test individual steps (address generation, env file rendering, etc.).
- **Graceful failure**: consistent error messages, exit codes, and cleanup.

## 4. Proposed Python CLI (`compose.py`)

### 4.1 Technology Choices
- **Typer** for CLI scaffolding (fast, declarative commands, type hints).
- **Rich** (optional) or simple logging for colored/status output.
- **pathlib**, **subprocess**, **dataclasses** for clean filesystem & process handling.
- **pydantic / dataclasses-json** (optional) if we need structured config parsing.
- Use a shared `scripts/common.py` for utilities (env loading, docker helpers, logging wrappers).

### 4.2 Command Layout
- `compose.py` registers Typer app and attaches subcommands imported from `scripts/*.py`.
- Individual command modules export a Typer `Typer` instance or command function, plus any helper functions unique to that command.

| Command | Responsibilities (initial minimal scope) |
|---------|------------------------------------------|
| `up` | Detect first run, invoke end-to-end bootstrap (new orchestrator replacing `setup.sh`), or start existing stack. |
| `down` | `docker compose down` without volume removal. |
| `status` | Report service health, RPC block heights, publisher status. Start minimal (plain text). |
| `logs` | Tail docker compose logs with service filters. |
| `restart` | Restart targeted services. |
| `deploy` | Trigger rebuild + restart for targets. |
| `purge` | Stop stack, remove volumes/artifacts, optionally confirm. |

*Future additions*: targeted `build`, `check`, or diagnostics once the foundation is stable.

### 4.3 New Bootstrap Flow (managed by `scripts/up.py`)

`scripts/up.py` is now a thin coordinator that orchestrates a handful of focused step modules stored under `scripts/up/`. Each module exposes a single `run(context)` entry point that consumes a shared `BootstrapContext` (dataclass capturing paths, env vars, and step results). Steps are executed sequentially by default, but each can opt into internal parallelism if needed. The proposed pipeline:

1. **Environment (`scripts/up/environment.py`)** – load `.env`/`toolkit.env`, validate required keys, derive defaults (chain IDs, repo paths, Docker image tags), and create the context.
2. **Repositories (`scripts/up/repositories.py`)** – ensure required checkouts exist (clone if missing, otherwise log reuse), sync the contract bundle when empty, and note whether source trees changed (to drive later build decisions).
3. **Op-deployer (`scripts/up/op_deployer.py`)** – build the local op-deployer image if absent, initialise state, render `intent.toml`, run `op-deployer apply`, and export rollup artifacts (`genesis.json`, `rollup.json`, `addresses.json`, JWT/password). Genesis hash calculation reuses the existing Go helper via `go run` inside a Docker container for reproducibility.
4. **Docker runtime (`scripts/up/docker.py`)** – decide whether to rebuild images based on the repository step output, run `docker compose build --parallel` for the changed targets, then start the stack with `docker compose up -d`.
5. **Contracts (`scripts/up/contracts.py`)** – optional helper deployment: wait for rollup RPCs, reuse Foundry caches, build & broadcast helper scripts, update `config.yml`, and restart services if mailbox addresses changed. This step is gated behind `--deploy-contracts/--skip-contracts` flags so first-time bring-up can remain minimal when desired.

Each step logs start/finish markers with elapsed time, and the runner produces a concise summary at the end.

### 4.4 Files & Modules
- `compose.py` – Typer entrypoint wiring subcommands.
- `scripts/common.py` – shared utilities (env loading, logging, subprocess helpers, simple health checks).
- `scripts/up.py` – orchestrator that chains together step modules and exposes CLI flags.
- `scripts/up/environment.py` – load/validate env vars, build `BootstrapContext`.
- `scripts/up/repositories.py` – clone/sync repos and contract bundle; detect changes.
- `scripts/up/op_deployer.py` – manage op-deployer lifecycle and export artifacts.
- `scripts/up/docker.py` – build/start docker services based on context.
- `scripts/up/contracts.py` – optional helper contract deployment and config patching.
- `scripts/{down,status,logs,restart,deploy,purge}.py` – thin wrappers for remaining CLI commands.

### 4.5 Simplifications / Behaviour Changes to Validate
- Re-evaluate unconditional workspace wipe; default to incremental refresh, but offer `--fresh` flag.
- Keep Foundry libs unless explicit `--reset-contracts`.
- Defer Blockscout configuration until explicitly requested.
- Collapse repeated JSON patching via Python functions; store derived values in structured objects.
- Provide explicit logging for each major step with timers, so we can profile future optimizations.

### 4.6 Migration Plan
1. Implement new CLI alongside existing scripts (no deletion yet).
2. Document differences and usage in `README.md` once stable.
3. Gradually port existing `./local` commands to Python equivalents.
4. When parity is achieved, retire Bash scripts (in a later change).

## 5. Next Steps
- Finish Python-based bootstrap pipeline (step modules + orchestrator).
- Ensure `compose up` no longer shells out to `scripts/setup.sh`; retire Bash path after validation.
- Extend tests/dry-runs for repo sync, op-deployer, and optional contract deployment paths.
- Update documentation and gather runtime metrics to guide future optimizations.
