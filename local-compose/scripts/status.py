from __future__ import annotations

import json
import os
import re
from pathlib import Path
from typing import Dict, Iterable, Mapping, Optional

import typer
from prettytable import HRuleStyle, PrettyTable, VRuleStyle

from . import common
from .up import PUBLISHER_HEALTH_URL, ROLLUP_A_RPC_URL, ROLLUP_B_RPC_URL


PUBLISHER_HTTP_ENDPOINT = "http://localhost:18080"

app = typer.Typer(help="Show service status and key health checks.")


@app.callback(invoke_without_command=True)
def status_command(
    ctx: typer.Context,
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
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

    compose_status = _compose_service_status()
    if compose_status is None:
        log.error("Failed to query docker compose status")
        raise typer.Exit(code=1)

    sections = []

    publisher_table = _render_publisher_section(
        compose_status,
        services_disabled=services_disabled,
    )
    if publisher_table:
        sections.append(("publisher", publisher_table))

    services_table = _render_services_section(
        compose_status,
        services_disabled=services_disabled,
    )
    if services_table:
        sections.append(("services", services_table))

    contracts_table = _render_contracts_section()
    if contracts_table:
        sections.append(("contracts", contracts_table))

    if not sections:
        if services_disabled:
            common.console.print("Services are disabled (calldata target); nothing to report.")
        else:
            common.console.print("No compose services found.")
        return

    for index, (title, table) in enumerate(sections):
        if index != 0:
            common.console.print()
        common.console.print(title)
        common.console.print(table)


def _compose_service_status() -> Optional[Dict[str, Dict[str, str]]]:
    try:
        services_raw = common.capture(
            ("docker", "compose", "ps", "--all", "--format", "{{.Service}}|{{.State}}|{{.Status}}"),
            check=False,
        )
    except Exception:  # noqa: BLE001
        return None

    status: Dict[str, Dict[str, str]] = {}
    for line in services_raw.splitlines():
        parts = [part.strip() for part in line.split("|")]
        if len(parts) != 3 or not parts[0]:
            continue
        service, state, detail = parts
        status[service] = {"state": state, "status": detail}
    return status


def _render_publisher_section(
    compose_status: Mapping[str, Mapping[str, str]],
    *,
    services_disabled: bool,
) -> str:
    info = compose_status.get("rollup-shared-publisher")
    status_text = _format_status(info, disabled_default="disabled" if services_disabled else "missing")

    if info and _is_running(info) and "healthy" not in status_text:
        health_code = common.http_status(PUBLISHER_HEALTH_URL)
        if health_code == 200:
            status_text = f"{status_text} (healthy)"

    table = _new_table(["label", "value"])
    table.add_row(["status", status_text])
    table.add_row(["endpoint", PUBLISHER_HTTP_ENDPOINT])
    return table.get_string(header=False)


def _render_services_section(
    compose_status: Mapping[str, Mapping[str, str]],
    *,
    services_disabled: bool,
) -> str:
    rollup_labels = {"a": "rollup-a", "b": "rollup-b"}

    if services_disabled:
        block_heights = {"a": None, "b": None}
    else:
        block_heights = {
            "a": common.eth_block_number(ROLLUP_A_RPC_URL),
            "b": common.eth_block_number(ROLLUP_B_RPC_URL),
        }

    table = _new_table(["service", "rollup", "status", "detail", "endpoint"])

    for service_name, entries in _service_layout(
        block_heights,
        compose_status,
        services_disabled=services_disabled,
    ).items():
        first = True
        for rollup_key in ("a", "b"):
            entry = entries[rollup_key]
            info = compose_status.get(entry.compose)
            status_default = "disabled" if services_disabled else "missing"
            status_text = _format_status(info, disabled_default=status_default)
            detail = entry.detail
            endpoint = entry.endpoint
            row = [service_name if first else "", rollup_labels[rollup_key], status_text, detail, endpoint]
            divider = rollup_key == "b"
            table.add_row(row, divider=divider)
            first = False

    return table.get_string(header=False)


def _render_contracts_section() -> Optional[str]:
    contracts_path_candidates = (
        Path("networks/rollup-a/contracts.json"),
        Path("networks/rollup-b/contracts.json"),
    )
    addresses: Dict[str, str] | None = None
    for path in contracts_path_candidates:
        if path.exists():
            try:
                data = json.loads(path.read_text())
            except json.JSONDecodeError:
                continue
            addresses = data.get("addresses")
            if isinstance(addresses, dict):
                break
            addresses = None

    table = _new_table(["name", "value"])
    if not addresses:
        table.add_row(["contracts", "metadata unavailable"])
        return table.get_string(header=False)

    order = ["Mailbox", "Bridge", "PingPong", "MyToken", "Coordinator"]
    for key in order:
        value = addresses.get(key)
        if value:
            table.add_row([key, value])
    for key, value in sorted(addresses.items()):
        if key in order:
            continue
        table.add_row([key, value])
    return table.get_string(header=False)


def _format_status(
    info: Optional[Mapping[str, str]],
    *,
    disabled_default: str,
) -> str:
    if info is None:
        return disabled_default

    state = (info.get("state") or "").strip().lower()
    detail = (info.get("status") or "").strip()
    normalized = _normalize_status_detail(detail, state)
    if normalized:
        return normalized
    if state:
        return state
    return "unknown"


class _ServiceEntry:
    __slots__ = ("compose", "detail", "endpoint")

    def __init__(self, compose: str, detail: str, endpoint: str) -> None:
        self.compose = compose
        self.detail = detail
        self.endpoint = endpoint


def _service_layout(
    block_heights: Mapping[str, Optional[int]],
    compose_status: Mapping[str, Mapping[str, str]],
    *,
    services_disabled: bool,
) -> Dict[str, Dict[str, _ServiceEntry]]:
    return {
        "op-geth": {
            "a": _ServiceEntry(
                "op-geth-a",
                _block_detail(block_heights.get("a"), services_disabled),
                ROLLUP_A_RPC_URL,
            ),
            "b": _ServiceEntry(
                "op-geth-b",
                _block_detail(block_heights.get("b"), services_disabled),
                ROLLUP_B_RPC_URL,
            ),
        },
        "op-node": {
            "a": _ServiceEntry("op-node-a", "rpc", "http://localhost:19545"),
            "b": _ServiceEntry("op-node-b", "rpc", "http://localhost:29545"),
        },
        "batcher": {
            "a": _ServiceEntry("op-batcher-a", "port 18548", ""),
            "b": _ServiceEntry("op-batcher-b", "port 28548", ""),
        },
        "proposer": {
            "a": _ServiceEntry("op-proposer-a", "port 18560", ""),
            "b": _ServiceEntry("op-proposer-b", "port 28560", ""),
        },
        "blockscout": {
            "a": _ServiceEntry(
                "blockscout-a",
                _blockscout_detail(
                    compose_status.get("blockscout-a"),
                    "http://localhost:19000/api/health",
                    services_disabled,
                ),
                "http://localhost:19000",
            ),
            "b": _ServiceEntry(
                "blockscout-b",
                _blockscout_detail(
                    compose_status.get("blockscout-b"),
                    "http://localhost:29000/api/health",
                    services_disabled,
                ),
                "http://localhost:29000",
            ),
        },
    }


def _block_detail(height: Optional[int], services_disabled: bool) -> str:
    if services_disabled:
        return "disabled"
    if height is None:
        return "unreachable"
    return f"block {height}"


def _blockscout_detail(
    info: Optional[Mapping[str, str]],
    url: str,
    services_disabled: bool,
) -> str:
    if services_disabled:
        return "disabled"
    if info is None or not _is_running(info):
        return "unreachable"
    status = common.http_status(url)
    if status == 200:
        return "healthy"
    if status is None:
        return "unreachable"
    return f"HTTP {status}"


def _new_table(field_names: Iterable[str]) -> PrettyTable:
    table = PrettyTable()
    table.field_names = list(field_names)
    table.align = "l"
    table.left_padding_width = 1
    table.right_padding_width = 1
    table.horizontal_char = "─"
    table.vertical_char = "│"
    table.junction_char = "┼"
    table.top_left_junction_char = "┌"
    table.top_right_junction_char = "┐"
    table.top_junction_char = "┬"
    table.bottom_left_junction_char = "└"
    table.bottom_right_junction_char = "┘"
    table.bottom_junction_char = "┴"
    table.left_junction_char = "├"
    table.right_junction_char = "┤"
    table.hrules = HRuleStyle.FRAME
    table.vrules = VRuleStyle.ALL
    return table


def _is_running(info: Mapping[str, str]) -> bool:
    return (info.get("state") or "").strip().lower() == "running"


def _normalize_status_detail(detail: str, state: str) -> Optional[str]:
    lowered = detail.lower()
    if not lowered:
        return None
    if lowered.startswith("up"):
        return _shorten_up_detail(lowered)
    if lowered.startswith("running"):
        return "running"
    if lowered.startswith("health: "):
        return lowered
    if lowered.startswith("restarting"):
        return "restarting"
    if lowered.startswith("created"):
        return "created"
    if lowered.startswith("exited") or state in {"exited", "dead"}:
        return "down"
    if lowered.startswith("paused"):
        return "paused"
    return lowered


def _shorten_up_detail(detail: str) -> str:
    # detail already lowercase and starts with "up"
    remainder = detail[2:].strip()
    if not remainder:
        return "up"

    suffix = ""
    if " (" in remainder:
        base, rest = remainder.split(" (", 1)
        remainder = base.strip()
        suffix = " (" + rest

    normalized = remainder
    normalized = normalized.replace("about ", "")
    normalized = normalized.replace("approximately ", "")
    normalized = normalized.replace("around ", "")

    if normalized.startswith("an "):
        normalized = "1 " + normalized[3:]
    elif normalized.startswith("a "):
        normalized = "1 " + normalized[2:]

    if normalized.startswith("less than a minute"):
        return f"up <1m{suffix}"
    if normalized.startswith("less than a second"):
        return f"up <1s{suffix}"

    runtime_match = re.match(r"(?P<num>\d+)\s+(?P<unit>second|seconds|minute|minutes|hour|hours|day|days|week|weeks)", normalized)
    if runtime_match:
        num = runtime_match.group("num")
        unit = runtime_match.group("unit")
        suffix_map = {
            "second": "s",
            "seconds": "s",
            "minute": "m",
            "minutes": "m",
            "hour": "h",
            "hours": "h",
            "day": "d",
            "days": "d",
            "week": "w",
            "weeks": "w",
        }
        unit_short = suffix_map.get(unit, unit[:1])
        return f"up {num}{unit_short}{suffix}"

    return f"up {normalized}{suffix}"


__all__ = ["app"]
