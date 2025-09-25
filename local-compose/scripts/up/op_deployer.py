from __future__ import annotations

import json
import subprocess
from pathlib import Path

from .. import common
from . import BootstrapContext, ROLLUP_A_RPC_URL, ROLLUP_B_RPC_URL


def run(ctx: BootstrapContext) -> None:
    log = common.get_logger(__name__)
    _ensure_image(ctx)
    _ensure_state(ctx)
    _write_intent(ctx)

    log.info("Running op-deployer apply (%s)", ctx.deployment_target)
    common.run(
        (
            "docker",
            "run",
            "--rm",
            "--user",
            _docker_user(),
            "-v",
            f"{ctx.state_dir}:/work",
            "-w",
            "/work",
            "-e",
            "HOME=/work",
            "-e",
            "DEPLOYER_CACHE_DIR=/work/.cache",
            "-e",
            f"L1_RPC_URL={ctx.env['HOODI_EL_RPC']}",
            "-e",
            f"DEPLOYER_PRIVATE_KEY={ctx.wallet_private_key}",
            ctx.op_deployer_image,
            "apply",
            f"--deployment-target={ctx.deployment_target}",
        )
    )

    _export_artifacts(ctx)
    addresses = _export_addresses(ctx)
    _write_blockscout_configs(ctx, addresses)


def _ensure_image(ctx: BootstrapContext) -> None:
    log = common.get_logger(__name__)
    inspect = subprocess.run(
        ["docker", "image", "inspect", ctx.op_deployer_image],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    if inspect.returncode == 0:
        return

    log.info("Building op-deployer image %s", ctx.op_deployer_image)
    common.run(
        (
            "docker",
            "build",
            "-t",
            ctx.op_deployer_image,
            "-f",
            str(ctx.root_dir / "docker" / "op-deployer.Dockerfile"),
            str(ctx.root_dir),
        )
    )


def _ensure_state(ctx: BootstrapContext) -> None:
    state_path = ctx.state_dir / "state.json"
    cache_dir = ctx.state_dir / ".cache"
    cache_dir.mkdir(parents=True, exist_ok=True)
    if state_path.exists() and not ctx.fresh:
        return

    if state_path.exists():
        state_path.unlink()

    common.run(
        (
            "docker",
            "run",
            "--rm",
            "--user",
            _docker_user(),
            "-v",
            f"{ctx.state_dir}:/work",
            "-w",
            "/work",
            "-e",
            "HOME=/work",
            "-e",
            "DEPLOYER_CACHE_DIR=/work/.cache",
            ctx.op_deployer_image,
            "init",
            "--intent-type",
            "custom",
            "--l1-chain-id",
            str(ctx.hoodi_chain_id),
            "--l2-chain-ids",
            f"{ctx.rollup_a_chain_id},{ctx.rollup_b_chain_id}",
        )
    )


def _write_intent(ctx: BootstrapContext) -> None:
    intent_path = ctx.state_dir / "intent.toml"
    wallet = ctx.wallet_address.lower()
    sequencer = ctx.sequencer_address.lower()

    template = f"""
configType = "custom"
l1ChainID = {ctx.hoodi_chain_id}
fundDevAccounts = false
l1ContractsLocator = "tag://op-contracts/v3.0.0"
l2ContractsLocator = "tag://op-contracts/v3.0.0"

[superchainRoles]
  proxyAdminOwner = "{wallet}"
  protocolVersionsOwner = "{wallet}"
  guardian = "{wallet}"

[[chains]]
  id = "{_chain_id_hex(ctx.rollup_a_chain_id)}"
  baseFeeVaultRecipient = "{wallet}"
  l1FeeVaultRecipient = "{wallet}"
  sequencerFeeVaultRecipient = "{sequencer}"
  eip1559DenominatorCanyon = 250
  eip1559Denominator = 50
  eip1559Elasticity = 6
  gasLimit = 60000000
  operatorFeeScalar = 0
  operatorFeeConstant = 0
  minBaseFee = 0
  [chains.roles]
    l1ProxyAdminOwner = "{wallet}"
    l2ProxyAdminOwner = "{wallet}"
    systemConfigOwner = "{wallet}"
    unsafeBlockSigner = "{wallet}"
    batcher = "{wallet}"
    proposer = "{wallet}"
    challenger = "{wallet}"

[[chains]]
  id = "{_chain_id_hex(ctx.rollup_b_chain_id)}"
  baseFeeVaultRecipient = "{wallet}"
  l1FeeVaultRecipient = "{wallet}"
  sequencerFeeVaultRecipient = "{sequencer}"
  eip1559DenominatorCanyon = 250
  eip1559Denominator = 50
  eip1559Elasticity = 6
  gasLimit = 60000000
  operatorFeeScalar = 0
  operatorFeeConstant = 0
  minBaseFee = 0
  [chains.roles]
    l1ProxyAdminOwner = "{wallet}"
    l2ProxyAdminOwner = "{wallet}"
    systemConfigOwner = "{wallet}"
    unsafeBlockSigner = "{wallet}"
    batcher = "{wallet}"
    proposer = "{wallet}"
    challenger = "{wallet}"
"""
    intent_path.write_text(template.strip() + "\n")


def _export_artifacts(ctx: BootstrapContext) -> dict[int, dict[str, str]]:
    log = common.get_logger(__name__)
    hashes: dict[int, dict[str, str]] = {}
    for chain_id, target in (
        (ctx.rollup_a_chain_id, ctx.rollup_a_dir),
        (ctx.rollup_b_chain_id, ctx.rollup_b_dir),
    ):
        target.mkdir(parents=True, exist_ok=True)
        genesis_hash = _export_genesis(ctx, chain_id, target / "genesis.json")
        _export_rollup(ctx, chain_id, target / "rollup.json", genesis_hash)
        _ensure_jwt(target / "jwt.txt")
        _ensure_password(target / "password.txt")
        hashes[chain_id] = {"genesis_hash": genesis_hash}

    log.info("op-deployer artifacts exported")
    return hashes


def _export_genesis(ctx: BootstrapContext, chain_id: int, path: Path) -> str:
    raw = common.capture(
        (
            "docker",
            "run",
            "--rm",
            "--user",
            _docker_user(),
            "-v",
            f"{ctx.state_dir}:/work",
            "-w",
            "/work",
            ctx.op_deployer_image,
            "inspect",
            "genesis",
            str(chain_id),
        )
    )
    path.write_text(raw)

    data = json.loads(path.read_text())
    data.setdefault("alloc", {})
    balance_hex = hex(ctx.genesis_account_balance_wei)
    for address in {ctx.wallet_address.lower(), ctx.sequencer_address.lower()}:
        if address:
            data["alloc"].setdefault(address, {})["balance"] = balance_hex
    config = data.setdefault("config", {})
    config["pragueTime"] = ctx.rollup_prague_timestamp
    config["isthmusTime"] = ctx.rollup_isthmus_timestamp
    path.write_text(json.dumps(data, indent=2) + "\n")

    return _compute_genesis_hash(ctx, path)


def _export_rollup(ctx: BootstrapContext, chain_id: int, path: Path, genesis_hash: str) -> None:
    raw = common.capture(
        (
            "docker",
            "run",
            "--rm",
            "--user",
            _docker_user(),
            "-v",
            f"{ctx.state_dir}:/work",
            "-w",
            "/work",
            ctx.op_deployer_image,
            "inspect",
            "rollup",
            str(chain_id),
        )
    )
    data = json.loads(raw)
    data["isthmus_time"] = ctx.rollup_isthmus_timestamp
    data.setdefault("genesis", {}).setdefault("l2", {})["hash"] = genesis_hash
    path.write_text(json.dumps(data, indent=2) + "\n")


def _compute_genesis_hash(ctx: BootstrapContext, path: Path) -> str:
    rel = path.relative_to(ctx.root_dir)
    mod_cache = ctx.genesis_hash_cache_dir / "mod"
    build_cache = ctx.genesis_hash_cache_dir / "build"
    mod_cache.mkdir(parents=True, exist_ok=True)
    build_cache.mkdir(parents=True, exist_ok=True)

    cmd = (
        "docker",
        "run",
        "--rm",
        "-v",
        f"{ctx.root_dir}:/workspace",
        "-v",
        f"{ctx.op_geth_dir}:/op-geth",
        "-v",
        f"{mod_cache}:/go/pkg/mod",
        "-v",
        f"{build_cache}:/root/.cache/go-build",
        "-w",
        "/workspace/scripts/genesis_hash",
        "-e",
        "HOME=/tmp/home",
        "-e",
        "GOMODCACHE=/go/pkg/mod",
        "-e",
        "GOCACHE=/root/.cache/go-build",
        "golang:1.24-alpine",
        "sh",
        "-c",
        f"set -e; apk add --no-cache git >/dev/null; mkdir -p /tmp/home; go run . /workspace/{rel.as_posix()}"
    )
    hash_hex = common.capture(cmd).strip()
    return hash_hex


def _export_addresses(ctx: BootstrapContext) -> dict[int, dict[str, str]]:
    state_path = ctx.state_dir / "state.json"
    if not state_path.exists():
        return {}

    state = json.loads(state_path.read_text())
    deployments = {
        entry["id"].lower(): entry
        for entry in state.get("opChainDeployments", [])
        if isinstance(entry, dict) and "id" in entry
    }

    mapping = {
        ctx.rollup_a_chain_id: ctx.rollup_a_dir,
        ctx.rollup_b_chain_id: ctx.rollup_b_dir,
    }

    label_map = {
        "optimismPortalProxyAddress": "OPTIMISM_PORTAL",
        "l1StandardBridgeProxyAddress": "L1_STANDARD_BRIDGE",
        "systemConfigProxyAddress": "SYSTEM_CONFIG",
        "L2OutputOracleProxyAddress": "L2_OUTPUT_ORACLE",
        "disputeGameFactoryProxyAddress": "DISPUTE_GAME_FACTORY",
    }

    result: dict[int, dict[str, str]] = {}
    for chain_id, target in mapping.items():
        chain_hex = _chain_id_hex(chain_id).lower()
        deployment = deployments.get(chain_hex)
        if not deployment:
            continue
        addresses = {
            label: deployment[key]
            for key, label in label_map.items()
            if deployment.get(key)
        }
        (target / "addresses.json").write_text(json.dumps(addresses, indent=2) + "\n")
        env_lines = []
        if addresses.get("L2_OUTPUT_ORACLE"):
            env_lines.append(f"L2OO_ADDRESS={addresses['L2_OUTPUT_ORACLE']}")
            env_lines.append(f"OP_PROPOSER_L2OO_ADDRESS={addresses['L2_OUTPUT_ORACLE']}")
        if addresses.get("DISPUTE_GAME_FACTORY"):
            env_lines.append(f"DISPUTE_GAME_FACTORY_ADDRESS={addresses['DISPUTE_GAME_FACTORY']}")
            env_lines.append(
                f"OP_PROPOSER_GAME_FACTORY_ADDRESS={addresses['DISPUTE_GAME_FACTORY']}"
            )
        (target / "runtime.env").write_text("\n".join(env_lines) + "\n")
        result[chain_id] = addresses
    return result


def _write_blockscout_configs(
    ctx: BootstrapContext,
    addresses: dict[int, dict[str, str]],
) -> None:
    config = (
        (ctx.rollup_a_dir, ctx.rollup_a_chain_id, 19000, "a"),
        (ctx.rollup_b_dir, ctx.rollup_b_chain_id, 29000, "b"),
    )

    for base_dir, chain_id, port, suffix in config:
        blockscout_env = base_dir / "blockscout.env"
        frontend_env = base_dir / "blockscout-frontend.env"
        nginx_conf = base_dir / "blockscout-nginx.conf"

        http_rpc_host = "op-geth-a" if suffix == "a" else "op-geth-b"
        http_url = f"http://{http_rpc_host}:8545"
        ws_url = f"ws://{http_rpc_host}:8546"
        redis_url = f"redis://blockscout-{suffix}-redis:6379/0"
        db_url = f"postgresql://blockscout:blockscout@blockscout-{suffix}-db:5432/blockscout"
        blockscout_env.write_text(
            "\n".join(
                [
                    "# Autogenerated",
                    f"CHAIN_ID={chain_id}",
                    f"CHAIN_NAME=Rollup {'A' if suffix == 'a' else 'B'} Compose",
                    f"NETWORK=Rollup {'A' if suffix == 'a' else 'B'}",
                    "SUBNETWORK=Compose Rollups",
                    "CHAIN_TYPE=optimism",
                    "ETHEREUM_JSONRPC_VARIANT=geth",
                    "ETHEREUM_JSONRPC_TRANSPORT=http",
                    f"ETHEREUM_JSONRPC_HTTP_URL={http_url}",
                    f"ETHEREUM_JSONRPC_TRACE_URL={http_url}",
                    f"ETHEREUM_JSONRPC_WS_URL={ws_url}",
                    f"DATABASE_URL={db_url}",
                    "DATABASE_SSL=false",
                    "ECTO_USE_SSL=false",
                    f"REDIS_URL={redis_url}",
                    "SECRET_KEY_BASE=development",
                    "PORT=4000",
                    "POOL_SIZE=40",
                    "API_RATE_LIMIT_DISABLED=true",
                ]
            )
            + "\n"
        )

        frontend_env.write_text(
            "\n".join(
                [
                    "# Autogenerated",
                    "NEXT_PUBLIC_API_PROTOCOL=http",
                    f"NEXT_PUBLIC_API_HOST=localhost:{port}",
                    "NEXT_PUBLIC_API_BASE_PATH=",
                    "NEXT_PUBLIC_API_WEBSOCKET_PROTOCOL=ws",
                    f"NEXT_PUBLIC_NETWORK_NAME=Rollup {'A' if suffix == 'a' else 'B'} Compose",
                    f"NEXT_PUBLIC_NETWORK_SHORT_NAME=Rollup {'A' if suffix == 'a' else 'B'}",
                    f"NEXT_PUBLIC_NETWORK_ID={chain_id}",
                    "NEXT_PUBLIC_NETWORK_CURRENCY_NAME=Ether",
                    "NEXT_PUBLIC_NETWORK_CURRENCY_SYMBOL=ETH",
                    "NEXT_PUBLIC_NETWORK_CURRENCY_DECIMALS=18",
                    "NEXT_PUBLIC_APP_PROTOCOL=http",
                    f"NEXT_PUBLIC_APP_HOST=localhost:{port}",
                    "NEXT_PUBLIC_IS_TESTNET=true",
                ]
            )
            + "\n"
        )

        nginx_conf.write_text(
            f"""
map $http_upgrade $connection_upgrade {{
  default upgrade;
  ''      close;
}}

server {{
  listen 80;
  listen [::]:80;
  server_name _;

  location /api {{
    proxy_pass http://blockscout-{suffix}:4000;
  }}

  location / {{
    proxy_pass http://blockscout-{suffix}-frontend:3000;
  }}
}}
""".strip()
            + "\n"
        )


def _ensure_jwt(path: Path) -> None:
    if path.exists():
        return
    import secrets

    path.write_text(secrets.token_hex(32) + "\n")


def _ensure_password(path: Path) -> None:
    if path.exists():
        return
    path.write_text("\n")


def _chain_id_hex(chain_id: int) -> str:
    return f"0x{chain_id:064x}"


def _docker_user() -> str:
    import os

    return f"{os.getuid()}:{os.getgid()}"
