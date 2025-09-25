from __future__ import annotations

import subprocess
from typing import List

import typer

from . import common

app = typer.Typer(help="Rebuild images and restart services.")
@app.callback(invoke_without_command=True)
def deploy_command(
    ctx: typer.Context,
    services: List[str] = typer.Argument(
        None,
        help="Service names or selectors (e.g. op-geth, blockscout) to rebuild/restart.",
    ),
    no_build: bool = typer.Option(False, "--no-build", help="Skip docker image rebuild."),
    no_restart: bool = typer.Option(False, "--no-restart", help="Skip restarting services after build."),
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
) -> None:
    if ctx.invoked_subcommand is not None:
        return
    common.configure_logging(verbose)
    log = common.get_logger(__name__)
    targets = common.resolve_services(
        services,
        default=common.DEFAULT_COMPOSE_TARGETS,
    )

    if not no_build:
        log.info("Building images for %s", ", ".join(targets))
        try:
            common.docker_compose("build", "--parallel", *targets)
        except subprocess.CalledProcessError as exc:
            log.error("docker compose build failed")
            raise typer.Exit(code=exc.returncode)
    else:
        log.info("Skipping build step")

    if no_restart:
        return

    log.info("Restarting services")
    try:
        common.docker_compose("up", "-d", *targets)
    except subprocess.CalledProcessError as exc:
        log.error("docker compose up failed")
        raise typer.Exit(code=exc.returncode)


__all__ = ["app"]
