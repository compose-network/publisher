from __future__ import annotations

import subprocess

import typer

from . import common

app = typer.Typer(help="Stop the Docker Compose stack.")


@app.callback(invoke_without_command=True)
def down_command(
    ctx: typer.Context,
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
) -> None:
    if ctx.invoked_subcommand is not None:
        return
    common.configure_logging(verbose)
    log = common.get_logger(__name__)
    log.info("Stopping Docker Compose services")
    try:
        common.docker_compose("down")
    except subprocess.CalledProcessError as exc:
        log.error("docker compose down failed")
        raise typer.Exit(code=exc.returncode)


__all__ = ["app"]
