from __future__ import annotations

import subprocess
from pathlib import Path
from typing import Iterable

import typer

from . import common

CONTRACT_SUBDIRS = (
    "artifacts",
    "broadcast",
    "cache",
    "out",
    "lib",
)

app = typer.Typer(help="Remove generated artifacts and stop the stack.")


@app.callback(invoke_without_command=True)
def purge_command(
    ctx: typer.Context,
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
    force: bool = typer.Option(False, "--force", "-f", help="Skip confirmation prompt."),
    keep_contract_libs: bool = typer.Option(
        False,
        "--keep-contract-libs",
        help="Preserve contracts/lib when purging artifacts.",
    ),
) -> None:
    if ctx.invoked_subcommand is not None:
        return
    common.configure_logging(verbose)
    log = common.get_logger(__name__)

    if not force:
        confirm = typer.confirm(
            "This will stop containers and remove generated state. Continue?", default=False
        )
        if not confirm:
            log.info("Purge aborted")
            raise typer.Exit(code=0)

    log.info("Stopping docker compose stack and removing volumes")
    try:
        common.docker_compose("down", "-v", check=False)
    except subprocess.CalledProcessError:
        pass

    targets = [
        common.ROOT_DIR / "state",
        common.ROOT_DIR / "networks",
        common.ROOT_DIR / ".cache" / "genesis-go",
    ]

    contracts_dir = common.ROOT_DIR / "contracts"
    for sub in CONTRACT_SUBDIRS:
        if keep_contract_libs and sub == "lib":
            continue
        targets.append(contracts_dir / sub)

    for path in targets:
        common.remove_paths(path)

    log.info("Purge complete")


__all__ = ["app"]
