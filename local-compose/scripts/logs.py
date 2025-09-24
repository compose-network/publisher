from __future__ import annotations

import subprocess
from typing import List

import typer

from . import common

app = typer.Typer(help="Stream logs from docker compose services.")


@app.callback(invoke_without_command=True)
def logs_command(
    ctx: typer.Context,
    services: List[str] = typer.Argument(None, help="Service names to filter."),
    follow: bool = typer.Option(False, "-f", "--follow", help="Follow log output."),
    tail: int = typer.Option(
        100,
        "--tail",
        min=0,
        help="Number of lines to show from the end of the logs (0 = all).",
    ),
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
) -> None:
    if ctx.invoked_subcommand is not None:
        return
    common.configure_logging(verbose)
    args = ["docker", "compose", "logs"]
    if follow:
        args.append("--follow")
    if tail:
        args.extend(["--tail", str(tail)])
    if services:
        args.extend(services)
    try:
        subprocess.run(args, cwd=common.ROOT_DIR, check=False)
    except KeyboardInterrupt:  # graceful exit
        raise typer.Exit(code=130) from None


__all__ = ["app"]
