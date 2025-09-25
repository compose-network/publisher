from __future__ import annotations

import subprocess
from typing import List, Optional

import typer

from . import common

app = typer.Typer(help="Stream logs from docker compose services.")


@app.callback(invoke_without_command=True)
def logs_command(
    ctx: typer.Context,
    services: List[str] = typer.Argument(None, help="Service names to filter."),
    follow: bool = typer.Option(False, "-f", "--follow", help="Follow log output."),
    tail: Optional[int] = typer.Option(
        None,
        "--tail",
        min=0,
        help="Number of lines to show from the end of the logs (0 = all).",
    ),
    since: Optional[str] = typer.Option(
        None,
        "--since",
        help="Show logs since timestamp (e.g. 2024-01-02T13:23:37Z) or relative (e.g. 42m).",
    ),
    until: Optional[str] = typer.Option(
        None,
        "--until",
        help="Show logs up to timestamp (e.g. 2024-01-02T13:23:37Z) or relative (e.g. 42m).",
    ),
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
) -> None:
    if ctx.invoked_subcommand is not None:
        return
    common.configure_logging(verbose)
    args = ["docker", "compose", "logs"]
    if follow:
        args.append("--follow")
    if tail is not None:
        args.extend(["--tail", str(tail)])
    if since:
        args.extend(["--since", since])
    if until:
        args.extend(["--until", until])
    if services:
        args.extend(services)
    try:
        subprocess.run(args, cwd=common.ROOT_DIR, check=False)
    except KeyboardInterrupt:  # graceful exit
        raise typer.Exit(code=130) from None


__all__ = ["app"]
