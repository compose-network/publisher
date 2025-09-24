from __future__ import annotations

from pathlib import Path

from .. import common
from . import BootstrapContext

DEFAULT_SEQUENCER_PRIVATE_KEY = "0x1111111111111111111111111111111111111111111111111111111111111111"
DEFAULT_SEQUENCER_ADDRESS = "0x19e7e376e7c213b7e7e7e46cc70a5dd086daff2a"
DEFAULT_HOODI_CHAIN_ID = 560048
DEFAULT_ROLLUP_A_CHAIN_ID = 77771
DEFAULT_ROLLUP_B_CHAIN_ID = 77772
DEFAULT_OP_DEPLOYER_IMAGE = "local/op-deployer:dev"
DEFAULT_FOUNDRY_IMAGE = "ghcr.io/foundry-rs/foundry:latest"


def run(*, fresh: bool, skip_contracts: bool) -> BootstrapContext:
    log = common.get_logger(__name__)
    env = common.load_env(required=True)

    required = ["HOODI_EL_RPC", "HOODI_CL_RPC", "WALLET_PRIVATE_KEY", "WALLET_ADDRESS"]
    missing = [name for name in required if not env.get(name)]
    if missing:
        raise RuntimeError(f"Missing required environment variables: {', '.join(missing)}")

    root_dir = common.ROOT_DIR
    services_dir = root_dir / "services"
    contracts_dir = root_dir / "contracts"
    networks_dir = root_dir / "networks"
    state_dir = root_dir / "state" / "op-deployer"
    rollup_a_dir = networks_dir / "rollup-a"
    rollup_b_dir = networks_dir / "rollup-b"

    op_geth_dir = _resolve_path(env.get("OP_GETH_PATH"), services_dir / "op-geth", root_dir)
    optimism_dir = _resolve_path(env.get("OPTIMISM_PATH"), services_dir / "optimism", root_dir)
    publisher_dir = _resolve_path(
        env.get("ROLLUP_SHARED_PUBLISHER_PATH"),
        services_dir / "rollup-shared-publisher",
        root_dir,
    )
    contracts_source_raw = env.get("CONTRACTS_SOURCE")
    contracts_source = (
        _resolve_path(contracts_source_raw, None, root_dir) if contracts_source_raw else None
    )

    hoodi_chain_id = int(str(env.get("HOODI_CHAIN_ID", DEFAULT_HOODI_CHAIN_ID)), 0)
    rollup_a_chain_id = int(str(env.get("ROLLUP_A_CHAIN_ID", DEFAULT_ROLLUP_A_CHAIN_ID)), 0)
    rollup_b_chain_id = int(str(env.get("ROLLUP_B_CHAIN_ID", DEFAULT_ROLLUP_B_CHAIN_ID)), 0)

    sequencer_private_key = _normalize_hex(env.get("SEQUENCER_PRIVATE_KEY", DEFAULT_SEQUENCER_PRIVATE_KEY), ensure_prefix=True)
    sequencer_address = _normalize_hex(env.get("SEQUENCER_ADDRESS", DEFAULT_SEQUENCER_ADDRESS), ensure_prefix=True)

    import os

    deployment_target = os.environ.get("DEPLOYMENT_TARGET", env.get("DEPLOYMENT_TARGET", "live"))
    op_deployer_image = env.get("OP_DEPLOYER_IMAGE", DEFAULT_OP_DEPLOYER_IMAGE)
    foundry_image = env.get("FOUNDRY_IMAGE", DEFAULT_FOUNDRY_IMAGE)
    foundry_home = Path(env.get("FOUNDRY_HOME_DIR", "/tmp/foundry"))
    if not foundry_home.is_absolute():
        foundry_home = (root_dir / foundry_home).resolve()
    genesis_hash_cache_dir = Path(env.get("GENESIS_HASH_CACHE_DIR", root_dir / ".cache" / "genesis-go"))
    if not genesis_hash_cache_dir.is_absolute():
        genesis_hash_cache_dir = (root_dir / genesis_hash_cache_dir).resolve()
    rollup_prague_timestamp = int(env.get("ROLLUP_PRAGUE_TIMESTAMP", "0"), 0)
    rollup_isthmus_timestamp = int(env.get("ROLLUP_ISTHMUS_TIMESTAMP", str(rollup_prague_timestamp)), 0)
    genesis_balance = int(
        str(env.get("ROLLUP_ACCOUNT_GENESIS_BALANCE_WEI", "100000000000000000")),
        0,
    )

    needs_bootstrap = fresh or not (
        rollup_a_dir.joinpath("rollup.json").exists() and rollup_b_dir.joinpath("rollup.json").exists()
    )

    def _truthy(name: str) -> bool:
        raw = os.environ.get(name)
        if raw is None:
            raw = env.get(name, "")
        return str(raw).lower() in {"1", "true", "yes"}

    start_services = True
    disable_services = _truthy("COMPOSE_SKIP_SERVICES")
    force_services = _truthy("COMPOSE_FORCE_SERVICES")
    if disable_services:
        start_services = False
    elif deployment_target.lower() == "calldata" and not force_services:
        start_services = False

    ctx = BootstrapContext(
        env=env,
        root_dir=root_dir,
        services_dir=services_dir,
        contracts_dir=contracts_dir,
        networks_dir=networks_dir,
        state_dir=state_dir,
        rollup_a_dir=rollup_a_dir,
        rollup_b_dir=rollup_b_dir,
        op_geth_dir=op_geth_dir,
        optimism_dir=optimism_dir,
        publisher_dir=publisher_dir,
        op_deployer_image=op_deployer_image,
        hoodi_chain_id=hoodi_chain_id,
        rollup_a_chain_id=rollup_a_chain_id,
        rollup_b_chain_id=rollup_b_chain_id,
        wallet_address=_normalize_hex(env["WALLET_ADDRESS"], ensure_prefix=True),
        wallet_private_key=_normalize_hex(env["WALLET_PRIVATE_KEY"], ensure_prefix=True),
        sequencer_address=sequencer_address,
        sequencer_private_key=sequencer_private_key,
        deployment_target=deployment_target,
        needs_bootstrap=needs_bootstrap,
        fresh=fresh,
        deploy_contracts=not skip_contracts,
        start_services=start_services,
        contracts_source=contracts_source,
        rollup_prague_timestamp=rollup_prague_timestamp,
        rollup_isthmus_timestamp=rollup_isthmus_timestamp,
        foundry_image=foundry_image,
        foundry_home=foundry_home,
        genesis_hash_cache_dir=genesis_hash_cache_dir,
        genesis_account_balance_wei=genesis_balance,
    )

    log.debug(
        "Context initialised (bootstrap=%s, contracts=%s)",
        ctx.needs_bootstrap,
        ctx.deploy_contracts,
    )

    return ctx


def _resolve_path(value: str | Path | None, default: Path | None, root: Path) -> Path:
    if value is None:
        if default is None:
            raise ValueError("Path resolution requires a default when value is None")
        path = Path(default)
    else:
        path = Path(value)
    if not path.is_absolute():
        path = (root / path).resolve()
    return path


def _normalize_hex(value: str | None, *, ensure_prefix: bool = False) -> str:
    if value is None:
        return ""
    value = value.strip().lower()
    if ensure_prefix and not value.startswith("0x"):
        value = "0x" + value
    return value
