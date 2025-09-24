from __future__ import annotations

import json
import subprocess
import time
from pathlib import Path

from .. import common
from . import BootstrapContext, ROLLUP_A_RPC_URL, ROLLUP_B_RPC_URL

FORGE_LIBS = (
    "foundry-rs/forge-std",
    "OpenZeppelin/openzeppelin-contracts",
    "OpenZeppelin/openzeppelin-contracts-upgradeable",
)


def run(ctx: BootstrapContext) -> None:
    log = common.get_logger(__name__)
    contracts_dir = ctx.contracts_dir
    if not contracts_dir.exists():
        log.warning("Contracts directory %s not found; skipping helper deployment", contracts_dir)
        return

    log.info("Waiting for rollup RPCs")
    min_block = int(ctx.env.get("CONTRACT_DEPLOY_MIN_BLOCK", "0"))
    for name, url in (("Rollup A", ROLLUP_A_RPC_URL), ("Rollup B", ROLLUP_B_RPC_URL)):
        _wait_for_rpc(url, name)
        if min_block > 0:
            _wait_for_block_height(url, name, min_block)

    settle_delay = int(ctx.env.get("CONTRACT_DEPLOY_DELAY_SECONDS", "20"))
    if settle_delay > 0:
        log.info("Waiting %s seconds for services to settle", settle_delay)
        time.sleep(settle_delay)

    log.info("Ensuring Foundry dependencies")
    _ensure_foundry_dirs(ctx)
    for lib in FORGE_LIBS:
        _ensure_forge_dependency(ctx, lib)

    log.info("Building contracts")
    _run_forge(ctx, "build")

    artifact_dir = contracts_dir / "artifacts"

    log.info("Deploying helper contracts to Rollup A")
    _run_deploy(
        ctx,
        script="script/DeployRollupA.s.sol",
        rpc_url=ROLLUP_A_RPC_URL,
        artifact_path=artifact_dir / "deploy-rollup-a.json",
    )

    log.info("Deploying helper contracts to Rollup B")
    _run_deploy(
        ctx,
        script="script/DeployRollupB.s.sol",
        rpc_url=ROLLUP_B_RPC_URL,
        artifact_path=artifact_dir / "deploy-rollup-b.json",
    )

    artifact_a = contracts_dir / "artifacts" / "deploy-rollup-a.json"
    artifact_b = contracts_dir / "artifacts" / "deploy-rollup-b.json"
    if not artifact_a.exists() or not artifact_b.exists():
        log.warning("Deploy artifacts not found; skipping helper configuration")
        return

    addresses_a = _load_addresses(artifact_a)
    addresses_b = _load_addresses(artifact_b)
    if addresses_a != addresses_b:
        log.warning("Helper contract addresses differ between rollups; not updating config")
        return

    _write_helper_config(ctx, addresses_a)
    _write_contract_json(ctx.rollup_a_dir / "contracts.json", addresses_a, ctx.rollup_a_chain_id)
    _write_contract_json(ctx.rollup_b_dir / "contracts.json", addresses_b, ctx.rollup_b_chain_id)

    log.info("Restarting op-geth/op-node with helper configuration")
    common.docker_compose(
        "up",
        "-d",
        "op-geth-a",
        "op-geth-b",
        "op-node-a",
        "op-node-b",
    )


def _wait_for_rpc(url: str, name: str, attempts: int = 120, delay: float = 1.0) -> None:
    for _ in range(attempts):
        if common.eth_block_number(url) is not None:
            return
        time.sleep(delay)
    raise RuntimeError(f"Timed out waiting for {name} RPC at {url}")


def _wait_for_block_height(url: str, name: str, target: int, attempts: int = 240, delay: float = 1.0) -> None:
    if target <= 0:
        return
    for _ in range(attempts):
        block = common.eth_block_number(url)
        if block is not None and block >= target:
            return
        time.sleep(delay)
    raise RuntimeError(f"Timed out waiting for {name} to reach block {target}")


def _ensure_foundry_dirs(ctx: BootstrapContext) -> None:
    ctx.foundry_home.mkdir(parents=True, exist_ok=True)
    (ctx.foundry_home / ".svm").mkdir(parents=True, exist_ok=True)
    (ctx.foundry_home / ".foundry").mkdir(parents=True, exist_ok=True)
    for subdir in ("artifacts", "broadcast", "cache", "out", "lib"):
        (ctx.contracts_dir / subdir).mkdir(parents=True, exist_ok=True)


def _ensure_forge_dependency(ctx: BootstrapContext, repo: str) -> None:
    lib_dir = ctx.contracts_dir / "lib" / repo.split("/")[-1]
    if repo.endswith("forge-std"):
        lib_dir = ctx.contracts_dir / "lib" / "forge-std"
    if lib_dir.exists():
        return
    _run_forge(ctx, "install", "--no-git", repo)


def _run_forge(ctx: BootstrapContext, *args: str) -> None:
    common.run(_forge_cmd(ctx, *args))


def _run_deploy(
    ctx: BootstrapContext,
    *,
    script: str,
    rpc_url: str,
    artifact_path: Path,
) -> None:
    base_args = [
        "script",
        script,
        "--broadcast",
        "--force",
        "--rpc-url",
        rpc_url,
        "--private-key",
        ctx.sequencer_private_key or ctx.wallet_private_key,
        "-vvv",
    ]
    max_attempts = int(ctx.env.get("CONTRACT_DEPLOY_MAX_ATTEMPTS", "5"))
    retry_delay = int(ctx.env.get("CONTRACT_DEPLOY_RETRY_DELAY_SECONDS", "5"))
    resume = False

    log = common.get_logger(__name__)

    for attempt in range(1, max_attempts + 1):
        args = list(base_args)
        if resume:
            args.append("--resume")

        proc = subprocess.run(
            _forge_cmd(ctx, *args),
            text=True,
            capture_output=True,
        )

        if proc.returncode == 0:
            if proc.stdout:
                common.console.print(proc.stdout)
            return

        output = f"{proc.stdout}\n{proc.stderr}"
        indexing_delay = "transaction indexing is in progress" in output.lower()

        if indexing_delay and _artifact_has_addresses(artifact_path):
            log.warning(
                "Forge reported transaction indexing delay, but broadcast artifacts exist at %s; assuming success",
                artifact_path,
            )
            if proc.stdout:
                common.console.print(proc.stdout)
            return

        if indexing_delay and attempt < max_attempts:
            resume = True
            log.info(
                "Forge deployment waiting for transaction index (%s/%s)",
                attempt,
                max_attempts,
            )
            time.sleep(retry_delay)
            continue

        common.console.print(proc.stdout)
        common.console.print(proc.stderr, style="bold red")
        raise RuntimeError(f"Contract deployment failed for {script}")

    raise RuntimeError(f"Contract deployment failed for {script} after {max_attempts} attempts")


def _artifact_has_addresses(path: Path) -> bool:
    if not path.exists():
        return False
    try:
        data = json.loads(path.read_text())
    except json.JSONDecodeError:
        return False
    addresses = data.get("addresses")
    return isinstance(addresses, dict) and bool(addresses)


def _forge_cmd(ctx: BootstrapContext, *forge_args: str) -> tuple[str, ...]:
    return (
        "docker",
        "run",
        "--rm",
        "--user",
        _docker_user(),
        "--network",
        "host",
        "-v",
        f"{ctx.contracts_dir}:/contracts",
        "-w",
        "/contracts",
        "-e",
        f"HOME={ctx.foundry_home}",
        "-e",
        f"SVM_HOME={ctx.foundry_home / '.svm'}",
        "-e",
        f"FOUNDRY_DIR={ctx.foundry_home / '.foundry'}",
        "-e",
        f"ROLLUP_A_CHAIN_ID={ctx.rollup_a_chain_id}",
        "-e",
        f"ROLLUP_B_CHAIN_ID={ctx.rollup_b_chain_id}",
        "-e",
        f"ROLLUP_A_RPC_URL={ROLLUP_A_RPC_URL}",
        "-e",
        f"ROLLUP_B_RPC_URL={ROLLUP_B_RPC_URL}",
        "-e",
        f"DEPLOYER_ADDRESS={ctx.sequencer_address or ctx.wallet_address}",
        "-e",
        f"DEPLOYER_PRIVATE_KEY={ctx.sequencer_private_key or ctx.wallet_private_key}",
        "--entrypoint",
        "forge",
        ctx.foundry_image,
        *forge_args,
    )


def _load_addresses(path: Path) -> dict[str, str]:
    data = json.loads(path.read_text())
    addresses = data.get("addresses") or data.get("parent", {}).get("addresses", {})
    return {k: v for k, v in addresses.items() if isinstance(v, str)}


def _write_helper_config(ctx: BootstrapContext, addresses: dict[str, str]) -> None:
    config_path = ctx.op_geth_dir / "config.yml"
    config = f"""
token: {addresses.get('MyToken')}
rollups:
  A:
    rpc: {ROLLUP_A_RPC_URL}
    chain_id: {ctx.rollup_a_chain_id}
    private_key: {ctx.wallet_private_key}
    bridge: {addresses.get('Bridge')}
    contracts:
      bridge: {addresses.get('Bridge')}
      pingpong: {addresses.get('PingPong')}
      mailbox: {addresses.get('Mailbox')}
      token: {addresses.get('MyToken')}
  B:
    rpc: {ROLLUP_B_RPC_URL}
    chain_id: {ctx.rollup_b_chain_id}
    private_key: {ctx.wallet_private_key}
    bridge: {addresses.get('Bridge')}
    contracts:
      bridge: {addresses.get('Bridge')}
      pingpong: {addresses.get('PingPong')}
      mailbox: {addresses.get('Mailbox')}
      token: {addresses.get('MyToken')}
"""
    config_path.write_text(config.strip() + "\n")


def _write_contract_json(path: Path, addresses: dict[str, str], chain_id: int) -> None:
    payload = {
        "chainInfo": {"chainId": chain_id},
        "addresses": addresses,
    }
    path.write_text(json.dumps(payload, indent=2) + "\n")


def _docker_user() -> str:
    import os

    return f"{os.getuid()}:{os.getgid()}"
