from __future__ import annotations

import subprocess
from typing import List

import typer

from . import common

app = typer.Typer(help="Restart docker compose services.")


def _resolve_targets(targets: List[str] | None) -> List[str]:
    if targets:
        return targets
    return list(common.DEFAULT_COMPOSE_TARGETS)


@app.callback(invoke_without_command=True)
def restart_command(
    ctx: typer.Context,
    services: List[str] = typer.Argument(None, help="Services to restart."),
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
) -> None:
    if ctx.invoked_subcommand is not None:
        return
    common.configure_logging(verbose)
    log = common.get_logger(__name__)
    targets = _resolve_targets(services)
    log.info("Restarting %s", ", ".join(targets))
    try:
        common.docker_compose("restart", *targets)
    except subprocess.CalledProcessError as exc:
        log.error("docker compose restart failed")
        raise typer.Exit(code=exc.returncode)


__all__ = ["app"]
