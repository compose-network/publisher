from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, Set

import typer
import time
import json

from .. import common

ROLLUP_A_RPC_URL = "http://localhost:18545"
ROLLUP_B_RPC_URL = "http://localhost:28545"
PUBLISHER_HEALTH_URL = "http://localhost:18081/health"


@dataclass
class BootstrapContext:
    env: Dict[str, str]
    root_dir: Path
    services_dir: Path
    contracts_dir: Path
    networks_dir: Path
    state_dir: Path
    rollup_a_dir: Path
    rollup_b_dir: Path
    op_geth_dir: Path
    optimism_dir: Path
    publisher_dir: Path
    op_deployer_image: str
    hoodi_chain_id: int
    rollup_a_chain_id: int
    rollup_b_chain_id: int
    wallet_address: str
    wallet_private_key: str
    sequencer_address: str
    sequencer_private_key: str
    deployment_target: str
    needs_bootstrap: bool
    fresh: bool
    deploy_contracts: bool
    start_services: bool
    contracts_source: Path | None
    rollup_prague_timestamp: int
    rollup_isthmus_timestamp: int
    foundry_image: str
    foundry_home: Path
    genesis_hash_cache_dir: Path
    genesis_account_balance_wei: int
    changed: Set[str] = field(default_factory=set)

    def mark_changed(self, key: str) -> None:
        self.changed.add(key)


from . import contracts, docker, environment, op_deployer, repositories


def bootstrap(*, fresh: bool, skip_contracts: bool, verbose: bool, timeout_seconds: int | None = None) -> BootstrapContext:
    common.configure_logging(verbose)
    log = common.get_logger(__name__)

    start = time.monotonic()

    def check(stage: str) -> None:
        if timeout_seconds is None:
            return
        elapsed = time.monotonic() - start
        if elapsed > timeout_seconds:
            raise TimeoutError(f"Bootstrap exceeded timeout after {elapsed:.1f}s during {stage}")

    env_start = time.monotonic()
    ctx = environment.run(fresh=fresh, skip_contracts=skip_contracts)
    env_elapsed = time.monotonic() - env_start
    log.info("Stage environment: %.1fs", env_elapsed)
    check("environment setup")

    if ctx.needs_bootstrap:
        log.info("Bootstrapping required (fresh=%s)", fresh)
        log.info("Stopping any existing docker compose services and removing volumes")
        try:
            common.docker_compose("down", "-v", check=False)
        except Exception:  # noqa: BLE001
            log.debug("docker compose down -v failed (continuing)")
    else:
        log.info("Bootstrap artifacts already present; use --fresh to rebuild")

    repo_start = time.monotonic()
    repositories.run(ctx)
    repo_elapsed = time.monotonic() - repo_start
    log.info("Stage repositories: %.1fs", repo_elapsed)
    check("repository sync")

    try:
        ctx.networks_dir.mkdir(parents=True, exist_ok=True)
        meta_path = ctx.networks_dir / "bootstrap-meta.json"
        meta = {
            "deployment_target": ctx.deployment_target,
            "start_services": ctx.start_services,
            "timestamp": int(time.time()),
        }
        meta_path.write_text(json.dumps(meta, indent=2) + "\n")
    except Exception as exc:  # noqa: BLE001
        log.debug("Failed to write bootstrap metadata: %s", exc)

    if ctx.needs_bootstrap:
        op_start = time.monotonic()
        op_deployer.run(ctx)
        op_elapsed = time.monotonic() - op_start
        log.info("Stage op-deployer: %.1fs", op_elapsed)
    else:
        log.info("Skipping op-deployer; artifacts already exist")
    check("op-deployer")

    if ctx.start_services:
        docker_start = time.monotonic()
        docker.run(ctx)
        docker_elapsed = time.monotonic() - docker_start
        log.info("Stage docker: %.1fs", docker_elapsed)
        check("docker startup")

        if ctx.deploy_contracts:
            if ctx.needs_bootstrap:
                contracts_start = time.monotonic()
                contracts.run(ctx)
                contracts_elapsed = time.monotonic() - contracts_start
                log.info("Stage contracts: %.1fs", contracts_elapsed)
            else:
                log.info("Contracts already deployed; skipping (use --fresh to redeploy)")
            check("contracts deployment")
        else:
            log.info("Contract deployment skipped")
    else:
        log.info("Deployment target %s skips service startup; docker compose not invoked", ctx.deployment_target)
        if ctx.deploy_contracts:
            log.info("Contract deployment requires running rollup RPCs; skipping because services are disabled")

    total_elapsed = time.monotonic() - start
    log.info("Bootstrap completed in %.1fs", total_elapsed)

    return ctx


app = typer.Typer(help="Bootstrap or start the local Compose stack.")


@app.callback(invoke_without_command=True)
def up_command(
    ctx: typer.Context,
    fresh: bool = typer.Option(
        False,
        "--fresh",
        help="Force regeneration of artifacts even if rollup state exists.",
    ),
    skip_contracts: bool = typer.Option(
        False,
        "--skip-contracts",
        help="Do not deploy helper contracts after bootstrapping.",
    ),
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
    timeout_minutes: float = typer.Option(
        6.0,
        "--timeout-minutes",
        min=0.1,
        help="Abort bootstrap after the given number of minutes (default: 6).",
    ),
) -> None:
    if ctx.invoked_subcommand is not None:
        return

    try:
        timeout_seconds = int(timeout_minutes * 60)
        bootstrap(
            fresh=fresh,
            skip_contracts=skip_contracts,
            verbose=verbose,
            timeout_seconds=timeout_seconds,
        )
    except TimeoutError as exc:
        common.get_logger(__name__).error("Compose bootstrap timed out: %s", exc)
        raise typer.Exit(code=2) from exc
    except Exception as exc:  # noqa: BLE001
        common.get_logger(__name__).error("Compose bootstrap failed: %s", exc)
        raise typer.Exit(code=1) from exc


__all__ = [
    "BootstrapContext",
    "ROLLUP_A_RPC_URL",
    "ROLLUP_B_RPC_URL",
    "PUBLISHER_HEALTH_URL",
    "bootstrap",
    "app",
]
