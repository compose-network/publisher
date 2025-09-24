from __future__ import annotations

import shutil
import subprocess
from pathlib import Path

import json

from .. import common
from . import BootstrapContext

DEFAULT_OPTIMISM_REPO = "https://github.com/ethereum-optimism/optimism.git"
DEFAULT_OPTIMISM_REF = "op-node/v1.13.4"
DEFAULT_OP_GETH_REPO = "https://github.com/ssvlabs/op-geth.git"
DEFAULT_OP_GETH_BRANCH = "feat/configurable-addresses"


def run(ctx: BootstrapContext) -> None:
    log = common.get_logger(__name__)
    if ctx.fresh:
        common.remove_paths(ctx.networks_dir, ctx.state_dir)

    ctx.services_dir.mkdir(parents=True, exist_ok=True)
    ctx.networks_dir.mkdir(parents=True, exist_ok=True)
    ctx.state_dir.mkdir(parents=True, exist_ok=True)
    ctx.rollup_a_dir.mkdir(parents=True, exist_ok=True)
    ctx.rollup_b_dir.mkdir(parents=True, exist_ok=True)

    optimism_repo = ctx.env.get("OPTIMISM_REPO", DEFAULT_OPTIMISM_REPO)
    optimism_ref = ctx.env.get("OPTIMISM_REF", DEFAULT_OPTIMISM_REF)
    op_geth_repo = ctx.env.get("OP_GETH_REPO", DEFAULT_OP_GETH_REPO)
    op_geth_branch = ctx.env.get("OP_GETH_BRANCH", DEFAULT_OP_GETH_BRANCH)

    if _ensure_repo(op_geth_repo, op_geth_branch, ctx.op_geth_dir):
        ctx.mark_changed("op_geth")
    if _ensure_repo(optimism_repo, optimism_ref, ctx.optimism_dir):
        ctx.mark_changed("optimism")

    _ensure_publisher(ctx)
    _ensure_contract_bundle(ctx)

    _seed_placeholder_contracts(ctx)

    log.debug("Repository step completed")


def _ensure_repo(repo: str, ref: str | None, dest: Path) -> bool:
    log = common.get_logger(__name__)
    if dest.joinpath(".git").exists():
        log.info("Using existing repository at %s", dest)
        return False

    if dest.exists():
        log.info("Removing non-git directory %s", dest)
        shutil.rmtree(dest)

    dest.parent.mkdir(parents=True, exist_ok=True)
    clone_cmd = ["git", "clone"]
    if ref:
        clone_cmd.extend(["--branch", ref, "--single-branch"])
    clone_cmd.extend([repo, str(dest)])
    log.info("Cloning %s into %s", repo, dest)
    try:
        common.run(clone_cmd)
    except subprocess.CalledProcessError:
        if ref:
            log.warning("Shallow clone with ref %s failed; retrying full clone", ref)
            common.run(["git", "clone", repo, str(dest)])
            if ref:
                common.run(["git", "-C", str(dest), "checkout", ref])
        else:
            raise
    return True


def _ensure_publisher(ctx: BootstrapContext) -> None:
    log = common.get_logger(__name__)
    dest = ctx.publisher_dir
    if dest.exists():
        log.info("Using shared publisher at %s", dest)
        return

    source_hint = ctx.env.get("ROLLUP_SP_SOURCE")
    if not source_hint:
        log.warning("Shared publisher directory missing and ROLLUP_SP_SOURCE not set; skipping copy")
        return

    source = Path(source_hint)
    if not source.is_absolute():
        source = (ctx.root_dir / source).resolve()
    if not source.exists():
        log.warning("Shared publisher source %s does not exist", source)
        return

    log.info("Copying shared publisher from %s", source)
    shutil.copytree(source, dest)
    ctx.mark_changed("publisher")


def _ensure_contract_bundle(ctx: BootstrapContext) -> None:
    log = common.get_logger(__name__)
    dest = ctx.contracts_dir
    if dest.exists() and any(dest.iterdir()):
        return

    if ctx.contracts_source and ctx.contracts_source.exists():
        log.info("Syncing contracts bundle from %s", ctx.contracts_source)
        shutil.copytree(ctx.contracts_source, dest, dirs_exist_ok=True)
        return

    log.warning("Contracts directory %s is empty and no CONTRACTS_SOURCE provided", dest)


def _seed_placeholder_contracts(ctx: BootstrapContext) -> None:
    for target, chain_id in ((ctx.rollup_a_dir, ctx.rollup_a_chain_id), (ctx.rollup_b_dir, ctx.rollup_b_chain_id)):
        contracts_json = target / "contracts.json"
        if contracts_json.exists():
            continue
        data = {
            "chainInfo": {"chainId": chain_id},
            "addresses": {
                "Mailbox": "0x" + "0" * 40,
                "PingPong": "0x" + "0" * 40,
                "MyToken": "0x" + "0" * 40,
                "Bridge": "0x" + "0" * 40,
            },
        }
        contracts_json.write_text(json.dumps(data, indent=2) + "\n")
