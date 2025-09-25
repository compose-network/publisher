from __future__ import annotations

import json
import logging
import os
import shutil
import subprocess
import urllib.error
import urllib.request
from pathlib import Path
from typing import Dict, Iterable, Mapping, Optional, Sequence

from dotenv import dotenv_values
ROOT_DIR = Path(__file__).resolve().parent.parent
DEFAULT_COMPOSE_TARGETS = (
    "rollup-shared-publisher",
    "op-geth-a",
    "op-geth-b",
    "op-node-a",
    "op-node-b",
    "op-batcher-a",
    "op-batcher-b",
    "op-proposer-a",
    "op-proposer-b",
)

BLOCKSCOUT_MARKER = ROOT_DIR / "networks" / "blockscout.enabled"
BOOTSTRAP_MARKER = ROOT_DIR / "networks" / "bootstrap-complete"

SERVICE_SELECTORS = {
    "op-geth": (
        "op-geth-a",
        "op-geth-b",
    ),
    "blockscout": (
        "blockscout-a-db",
        "blockscout-a-redis",
        "blockscout-a",
        "blockscout-a-frontend",
        "blockscout-a-proxy",
        "blockscout-b-db",
        "blockscout-b-redis",
        "blockscout-b",
        "blockscout-b-frontend",
        "blockscout-b-proxy",
    ),
    "publisher": (
        "rollup-shared-publisher",
    ),
}

class _PlainConsole:
    def print(self, *objects: object, **kwargs: object) -> None:
        style = kwargs.pop("style", None)
        end = kwargs.pop("end", "\n")
        sep = kwargs.pop("sep", " ")
        if kwargs:
            unsupported = ", ".join(sorted(kwargs))
            raise TypeError(f"Unsupported console.print kwargs: {unsupported}")
        if style:
            # Styles are ignored in plain console mode.
            pass
        output = sep.join(str(obj) for obj in objects)
        print(output, end=end)


console = _PlainConsole()
_logger_configured = False


def configure_logging(verbose: bool = False) -> None:
    global _logger_configured
    if _logger_configured:
        return
    level = logging.DEBUG if verbose else logging.INFO
    logging.basicConfig(level=level)
    _logger_configured = True


def get_logger(name: str | None = None) -> logging.Logger:
    configure_logging()
    return logging.getLogger(name or "compose")


def ensure_executable(binary: str) -> None:
    if shutil.which(binary) is None:
        raise RuntimeError(f"Required executable '{binary}' not found in PATH")


def resolve_services(
    requested: Iterable[str] | None,
    *,
    default: Iterable[str] | None,
) -> list[str]:
    """Expand selector aliases to concrete docker-compose service names."""

    if requested:
        queue: Iterable[str] = requested
    elif default is not None:
        queue = default
    else:
        return []

    resolved: list[str] = []
    seen: set[str] = set()

    for name in queue:
        expanded = SERVICE_SELECTORS.get(name, (name,))
        for target in expanded:
            if target not in seen:
                resolved.append(target)
                seen.add(target)

    return resolved


def default_compose_targets() -> list[str]:
    targets = list(DEFAULT_COMPOSE_TARGETS)
    if blockscout_enabled():
        for service in SERVICE_SELECTORS["blockscout"]:
            if service not in targets:
                targets.append(service)
    return targets


def blockscout_marker_path() -> Path:
    return BLOCKSCOUT_MARKER


def blockscout_enabled() -> bool:
    return BLOCKSCOUT_MARKER.exists()


def bootstrap_marker_path() -> Path:
    return BOOTSTRAP_MARKER


def bootstrap_completed() -> bool:
    return BOOTSTRAP_MARKER.exists()


def run(
    args: Sequence[str],
    *,
    env: Optional[Mapping[str, str]] = None,
    cwd: Optional[Path] = None,
    check: bool = True,
) -> subprocess.CompletedProcess[str]:
    get_logger().debug("running %s", " ".join(args))
    return subprocess.run(
        list(args),
        cwd=cwd or ROOT_DIR,
        env=_merged_env(env),
        check=check,
        text=True,
    )


def capture(
    args: Sequence[str],
    *,
    env: Optional[Mapping[str, str]] = None,
    cwd: Optional[Path] = None,
    check: bool = True,
) -> str:
    get_logger().debug("capturing %s", " ".join(args))
    result = subprocess.run(
        list(args),
        cwd=cwd or ROOT_DIR,
        env=_merged_env(env),
        check=check,
        text=True,
        capture_output=True,
    )
    return result.stdout


def docker_compose(
    *compose_args: str,
    env: Optional[Mapping[str, str]] = None,
    check: bool = True,
) -> subprocess.CompletedProcess[str]:
    ensure_executable("docker")
    ensure_executable("docker-compose") if shutil.which("docker-compose") else None
    return run(("docker", "compose", *compose_args), env=env, check=check)


def load_env(required: bool = True) -> Dict[str, str]:
    env_path = ROOT_DIR / ".env"
    toolkit_path = ROOT_DIR / "toolkit.env"

    if not env_path.exists():
        if required:
            raise FileNotFoundError("Missing .env file")
        return {}

    env: Dict[str, str] = {
        key: value
        for key, value in dotenv_values(env_path).items()
        if value is not None
    }

    # Allow environment variables to override .env entries for ad-hoc runs.
    for key, value in os.environ.items():
        if key in env:
            env[key] = value

    if toolkit_path.exists():
        env.update({
            key: value
            for key, value in dotenv_values(toolkit_path).items()
            if value is not None
        })

    return env


def _merged_env(extra: Optional[Mapping[str, str]]) -> Dict[str, str]:
    merged = dict(os.environ)
    if extra:
        merged.update(extra)
    return merged


def is_bootstrapped() -> bool:
    rollup_dir = ROOT_DIR / "networks"
    return (
        rollup_dir.joinpath("rollup-a", "rollup.json").exists()
        and rollup_dir.joinpath("rollup-b", "rollup.json").exists()
    )


def existing_services() -> Iterable[str]:
    result = capture(("docker", "compose", "ps", "--all", "--services"), check=False)
    return tuple(line.strip() for line in result.splitlines() if line.strip())


def running_services() -> Iterable[str]:
    result = capture(("docker", "compose", "ps", "--status", "running", "--services"), check=False)
    return tuple(line.strip() for line in result.splitlines() if line.strip())


def remove_paths(*paths: Path) -> None:
    for path in paths:
        if path.exists():
            get_logger().debug("removing %s", path)
            try:
                if path.is_dir():
                    shutil.rmtree(path)
                else:
                    path.unlink()
            except PermissionError:
                _remove_with_docker(path)
            except OSError as exc:
                get_logger().warning("Failed to remove %s: %s", path, exc)


def _remove_with_docker(path: Path) -> None:
    get_logger().debug("Attempting docker-assisted removal of %s", path)
    parent = path.parent
    target = path.name
    if not parent.exists():
        return
    cmd = [
        "docker",
        "run",
        "--rm",
        "-v",
        f"{parent}:/workspace",
        "alpine:3",
        "sh",
        "-c",
        f"rm -rf /workspace/{target}",
    ]
    try:
        subprocess.run(cmd, check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    except Exception as exc:  # noqa: BLE001
        get_logger().warning("Docker-assisted removal failed for %s: %s", path, exc)



def eth_block_number(url: str, timeout: float = 2.0) -> Optional[int]:
    payload = json.dumps(
        {"jsonrpc": "2.0", "id": 1, "method": "eth_blockNumber", "params": []}
    ).encode()
    request = urllib.request.Request(
        url,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            data = json.load(response)
    except (OSError, ValueError, urllib.error.URLError):
        return None
    result = data.get("result")
    if isinstance(result, str) and result.startswith("0x"):
        try:
            return int(result, 16)
        except ValueError:
            return None
    return None


def http_status(url: str, timeout: float = 2.0) -> Optional[int]:
    request = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status
    except urllib.error.HTTPError as exc:
        return exc.code
    except (OSError, urllib.error.URLError):
        return None


__all__ = [
    "ROOT_DIR",
    "DEFAULT_COMPOSE_TARGETS",
    "default_compose_targets",
    "blockscout_marker_path",
    "blockscout_enabled",
    "bootstrap_marker_path",
    "bootstrap_completed",
    "console",
    "configure_logging",
    "get_logger",
    "ensure_executable",
    "run",
    "capture",
    "docker_compose",
    "load_env",
    "is_bootstrapped",
    "existing_services",
    "running_services",
    "remove_paths",
    "eth_block_number",
    "http_status",
]
