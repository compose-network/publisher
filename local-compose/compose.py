#!/usr/bin/env python3
from __future__ import annotations

import typer

from scripts import deploy, down, logs, purge, restart, status, up

app = typer.Typer(help="Compose rollup developer tooling.", add_completion=False)
app.add_typer(up.app, name="up")
app.add_typer(down.app, name="down")
app.add_typer(status.app, name="status")
app.add_typer(logs.app, name="logs")
app.add_typer(restart.app, name="restart")
app.add_typer(deploy.app, name="deploy")
app.add_typer(purge.app, name="purge")


if __name__ == "__main__":
    app()
