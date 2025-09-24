from __future__ import annotations

import json
import os

import typer

from . import common
from .up import PUBLISHER_HEALTH_URL, ROLLUP_A_RPC_URL, ROLLUP_B_RPC_URL

app = typer.Typer(help="Show service status and key health checks.")


@app.callback(invoke_without_command=True)
def status_command(
    ctx: typer.Context,
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
    rpc_checks: bool = typer.Option(True, "--rpc", help="Include RPC block height checks."),
) -> None:
    if ctx.invoked_subcommand is not None:
        return
    common.configure_logging(verbose)
    log = common.get_logger(__name__)

    env = common.load_env(required=False)
    def _truthy(name: str) -> bool:
        raw = os.environ.get(name)
        if raw is None:
            raw = env.get(name, "")
        return str(raw).lower() in {"1", "true", "yes"}

    deployment_target = os.environ.get("DEPLOYMENT_TARGET", env.get("DEPLOYMENT_TARGET", "live")).lower()
    skip_services = _truthy("COMPOSE_SKIP_SERVICES")
    force_services = _truthy("COMPOSE_FORCE_SERVICES")
    services_disabled = (deployment_target == "calldata" and not force_services) or skip_services

    meta_path = common.ROOT_DIR / "networks" / "bootstrap-meta.json"
    if meta_path.exists():
        try:
            meta = json.loads(meta_path.read_text())
            meta_target = str(meta.get("deployment_target", "")).lower()
            meta_start = bool(meta.get("start_services", True))
            if meta_target:
                deployment_target = meta_target
            if not meta_start and not force_services:
                services_disabled = True
        except json.JSONDecodeError:
            pass

    try:
        services_raw = common.capture(
            ("docker", "compose", "ps", "--all", "--format", "{{.Service}}|{{.State}}|{{.Status}}"),
            check=False,
        )
    except Exception as exc:  # noqa: BLE001
        log.error("Failed to query docker compose status: %s", exc)
        raise typer.Exit(code=1)

    rows = []
    for line in services_raw.splitlines():
        parts = [part.strip() for part in line.split("|")]
        if len(parts) == 3:
            rows.append(parts)
    if rows:
        table = common.summary_table("docker-compose", rows)
        common.console.print(table, end="")
    else:
        if services_disabled:
            common.console.print("Services are disabled for calldata target; no compose containers running.")
        else:
            common.console.print("No compose services found.")

    if not rpc_checks:
        return

    if services_disabled:
        common.console.print("RPC checks skipped (calldata target with services disabled).")
        return

    rollup_a = ROLLUP_A_RPC_URL
    rollup_b = ROLLUP_B_RPC_URL
    publisher = PUBLISHER_HEALTH_URL

    status_rows = []
    block_a = common.eth_block_number(rollup_a)
    block_b = common.eth_block_number(rollup_b)
    status_rows.append(("Rollup A", rollup_a, _block_label(block_a)))
    status_rows.append(("Rollup B", rollup_b, _block_label(block_b)))
    status_rows.append(("Publisher", publisher, _http_label(common.http_status(publisher))))

    status_table = common.summary_table("health", status_rows)
    common.console.print(status_table, end="")


def _block_label(height: int | None) -> str:
    if height is None:
        return "unreachable"
    return f"block {height}"


def _http_label(status: int | None) -> str:
    if status is None:
        return "no response"
    return f"HTTP {status}"


__all__ = ["app"]
