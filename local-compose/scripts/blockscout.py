from __future__ import annotations

import json
import subprocess
import time
from pathlib import Path
from typing import Iterable, Mapping

import typer

from . import common

app = typer.Typer(help="Install Blockscout explorers and publish helper contracts.")

CONTRACTS_DIR = common.ROOT_DIR / "contracts"
NETWORKS_DIR = common.ROOT_DIR / "networks"

CONFIG_PATHS = (
    NETWORKS_DIR / "rollup-a" / "blockscout.env",
    NETWORKS_DIR / "rollup-a" / "blockscout-frontend.env",
    NETWORKS_DIR / "rollup-a" / "blockscout-nginx.conf",
    NETWORKS_DIR / "rollup-b" / "blockscout.env",
    NETWORKS_DIR / "rollup-b" / "blockscout-frontend.env",
    NETWORKS_DIR / "rollup-b" / "blockscout-nginx.conf",
)

ROLLUPS: dict[str, Mapping[str, object]] = {
    "rollup-a": {
        "label": "Rollup A",
        "base_url": "http://localhost:19000",
        "contracts_path": NETWORKS_DIR / "rollup-a" / "contracts.json",
        "broadcast_script": "DeployRollupA.s.sol",
    },
    "rollup-b": {
        "label": "Rollup B",
        "base_url": "http://localhost:29000",
        "contracts_path": NETWORKS_DIR / "rollup-b" / "contracts.json",
        "broadcast_script": "DeployRollupB.s.sol",
    },
}

CONTRACT_SPECS = {
    "Mailbox": {
        "artifact": Path("out/Mailbox.sol/Mailbox.json"),
        "contract_name": "src/Mailbox.sol:Mailbox",
        "constructor_types": ["address"],
    },
    "Bridge": {
        "artifact": Path("out/Bridge.sol/Bridge.json"),
        "contract_name": "src/Bridge.sol:Bridge",
        "constructor_types": ["address"],
    },
    "PingPong": {
        "artifact": Path("out/PingPong.sol/PingPong.json"),
        "contract_name": "src/PingPong.sol:PingPong",
        "constructor_types": ["address"],
    },
    "MyToken": {
        "artifact": Path("out/Token.sol/MyToken.json"),
        "contract_name": "src/Token.sol:MyToken",
        "constructor_types": [],
    },
}


@app.callback(invoke_without_command=True)
def blockscout_command(
    ctx: typer.Context,
    skip_verification: bool = typer.Option(
        False,
        "--skip-verification",
        help="Build and start Blockscout but skip contract verification.",
    ),
    verbose: bool = typer.Option(False, "--verbose", help="Enable debug logging."),
) -> None:
    if ctx.invoked_subcommand is not None:
        return

    common.configure_logging(verbose)
    log = common.get_logger(__name__)

    if not common.bootstrap_completed():
        log.error("Compose stack has not been bootstrapped yet; run './compose up' first.")
        raise typer.Exit(code=1)

    missing = [path for path in CONFIG_PATHS if not path.exists()]
    if missing:
        joined = ", ".join(str(path.relative_to(common.ROOT_DIR)) for path in missing)
        log.error("Missing Blockscout configuration files (%s); re-run './compose up --fresh'.", joined)
        raise typer.Exit(code=1)

    targets = common.SERVICE_SELECTORS["blockscout"]

    log.info("Building Blockscout images")
    try:
        common.docker_compose("build", "--parallel", *targets)
    except subprocess.CalledProcessError as exc:
        log.error("docker compose build failed for Blockscout")
        raise typer.Exit(code=exc.returncode)

    log.info("Starting Blockscout services")
    try:
        common.docker_compose("up", "-d", *targets)
    except subprocess.CalledProcessError as exc:
        log.error("docker compose up failed for Blockscout")
        raise typer.Exit(code=exc.returncode)

    for rollup_key, meta in ROLLUPS.items():
        label = meta["label"]
        health_url = f"{meta['base_url']}/api/health"
    log.info("Waiting for %s explorer to become healthy", label)
    _wait_for_health(health_url)

    if skip_verification:
        log.info("Skipping contract verification (--skip-verification)")
    else:
        log.info("Submitting helper contracts for verification")
        env = common.load_env(required=False)
        foundry_image = env.get("FOUNDRY_IMAGE", "ghcr.io/foundry-rs/foundry:latest")
        foundry_home = Path(env.get("FOUNDRY_HOME_DIR", "/tmp/foundry"))
        if not foundry_home.is_absolute():
            foundry_home = (common.ROOT_DIR / foundry_home).resolve()

        for rollup_key, meta in ROLLUPS.items():
            try:
                _verify_rollup(meta, foundry_image, foundry_home)
            except Exception as exc:  # noqa: BLE001
                log.warning("Verification for %s failed: %s", meta["label"], exc)

    try:
        common.blockscout_marker_path().touch(exist_ok=True)
    except Exception as exc:  # noqa: BLE001
        log.debug("Failed to record Blockscout enablement: %s", exc)

    log.info("Blockscout is enabled; future './compose up' runs will include the explorers.")


def _wait_for_health(url: str, attempts: int = 30, delay: float = 2.0) -> None:
    for _ in range(attempts):
        status = common.http_status(url, timeout=5.0)
        if status == 200:
            return
        time.sleep(delay)
    raise RuntimeError(f"Service at {url} did not become healthy")


def _verify_rollup(meta: Mapping[str, object], foundry_image: str, foundry_home: Path) -> None:
    base_url = str(meta["base_url"])
    contracts_path = Path(meta["contracts_path"])
    broadcast_script = str(meta["broadcast_script"])

    contracts_data = _load_contracts_json(contracts_path)
    if contracts_data is None:
        raise RuntimeError(f"Missing contract address data at {contracts_path}")

    chain_id = str(contracts_data.get("chainInfo", {}).get("chainId"))
    if not chain_id:
        raise RuntimeError("contracts.json missing chainInfo.chainId")

    broadcast_path = (
        CONTRACTS_DIR
        / "broadcast"
        / broadcast_script
        / chain_id
        / "run-latest.json"
    )
    if not broadcast_path.exists():
        raise RuntimeError(f"Broadcast log not found at {broadcast_path}")

    with broadcast_path.open() as handle:
        broadcast = json.load(handle)

    transactions = broadcast.get("transactions")
    if not isinstance(transactions, list):
        raise RuntimeError(f"Unexpected broadcast format in {broadcast_path}")

    addresses = contracts_data.get("addresses", {})
    if not isinstance(addresses, dict):
        raise RuntimeError(f"Unexpected address map in {contracts_path}")

    for name, spec in CONTRACT_SPECS.items():
        address = addresses.get(name)
        if not isinstance(address, str):
            continue
        tx = _match_transaction(transactions, name, address)
        if tx is None:
            continue
        artifact_path = CONTRACTS_DIR / spec["artifact"]
        if not artifact_path.exists():
            raise RuntimeError(f"Missing artifact for {name} at {artifact_path}")
        with artifact_path.open() as handle:
            artifact = json.load(handle)

        metadata = artifact.get("metadata")
        if not isinstance(metadata, dict):
            raise RuntimeError(f"Artifact for {name} missing metadata")

        constructor_types = spec["constructor_types"]
        arguments = tx.get("arguments") if isinstance(tx, dict) else []
        if arguments is None:
            arguments = []
        constructor_args = _encode_constructor_args(constructor_types, arguments)
        compiler_version = _compiler_version(metadata)

        _run_forge_verify(
            foundry_image=foundry_image,
            foundry_home=foundry_home,
            address=address,
            contract_name=spec["contract_name"],
            compiler_version=compiler_version,
            constructor_args=constructor_args,
            verifier_url=f"{base_url}/api/",
            chain_id=chain_id,
        )


def _load_contracts_json(path: Path) -> Mapping[str, object] | None:
    if not path.exists():
        return None
    with path.open() as handle:
        return json.load(handle)


def _match_transaction(transactions: Iterable[object], name: str, address: str) -> Mapping[str, object] | None:
    address_norm = address.lower()
    for tx in transactions:
        if not isinstance(tx, dict):
            continue
        tx_name = str(tx.get("contractName", ""))
        tx_address = str(tx.get("contractAddress", "")).lower()
        if tx_name == name and tx_address == address_norm:
            return tx
    return None


def _encode_constructor_args(types: Iterable[str], args: Iterable[object]) -> str:
    type_list = list(types)
    arg_list = list(args)
    if not type_list:
        return ""
    if len(type_list) != len(arg_list):
        raise RuntimeError("Constructor argument length mismatch")

    encoded_parts = []
    for typ, raw in zip(type_list, arg_list):
        if typ == "address":
            if not isinstance(raw, str):
                raise RuntimeError("Constructor argument for address must be a string")
            hex_value = raw.lower()
            if not hex_value.startswith("0x"):
                raise RuntimeError("Address argument missing 0x prefix")
            payload = bytes.fromhex(hex_value[2:].rjust(40, "0"))
        elif typ.startswith("uint"):
            value = int(raw, 0) if isinstance(raw, str) else int(raw)
            payload = value.to_bytes(32, byteorder="big", signed=False)
        else:
            raise RuntimeError(f"Unsupported constructor type '{typ}'")
        encoded_parts.append(payload.rjust(32, b"\x00"))
    return "0x" + b"".join(encoded_parts).hex()


def _compiler_version(metadata: Mapping[str, object]) -> str | None:
    compiler_version = str(metadata.get("compiler", {}).get("version", "")).strip()
    if not compiler_version:
        return None
    if not compiler_version.startswith("v"):
        compiler_version = f"v{compiler_version}"
    return compiler_version


def _run_forge_verify(
    *,
    foundry_image: str,
    foundry_home: Path,
    address: str,
    contract_name: str,
    compiler_version: str | None,
    constructor_args: str,
    verifier_url: str,
    chain_id: str,
) -> None:
    args = [
        "verify-contract",
        address,
        contract_name,
        "--verifier",
        "blockscout",
        "--verifier-url",
        verifier_url,
        "--chain-id",
        str(chain_id),
        "--watch",
    ]

    if compiler_version:
        args.extend(["--compiler-version", compiler_version])
    if constructor_args:
        args.extend(["--constructor-args", constructor_args])

    cmd = _forge_cmd(foundry_image, foundry_home, *args)
    result = subprocess.run(cmd, text=True, capture_output=True)
    log = common.get_logger(__name__)
    if result.returncode != 0:
        log.debug("forge stdout:\n%s", result.stdout)
        log.debug("forge stderr:\n%s", result.stderr)
        raise RuntimeError(f"forge verify-contract failed for {contract_name}")
    if result.stdout:
        log.info(result.stdout.strip())


def _forge_cmd(foundry_image: str, foundry_home: Path, *args: str) -> tuple[str, ...]:
    return (
        "docker",
        "run",
        "--rm",
        "--network",
        "host",
        "-v",
        f"{CONTRACTS_DIR.resolve()}:/contracts",
        "-w",
        "/contracts",
        "-e",
        f"HOME={foundry_home}",
        "-e",
        f"SVM_HOME={foundry_home / '.svm'}",
        "-e",
        f"FOUNDRY_DIR={foundry_home / '.foundry'}",
        "--entrypoint",
        "forge",
        foundry_image,
        *args,
    )


__all__ = ["app"]
