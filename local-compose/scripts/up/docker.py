from __future__ import annotations

import subprocess

from .. import common
from . import BootstrapContext


CORE_TARGETS = (
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


def run(ctx: BootstrapContext) -> None:
    log = common.get_logger(__name__)

    should_build = ctx.needs_bootstrap or ctx.changed.intersection({"op_geth", "optimism", "publisher"})
    if should_build:
        log.info("Building Docker images")
        try:
            common.docker_compose("build", "--parallel", *CORE_TARGETS)
        except subprocess.CalledProcessError as exc:
            log.error("docker compose build failed")
            raise
    else:
        log.info("Skipping image rebuild (no changes detected)")

    log.info("Starting docker compose services")
    try:
        common.docker_compose("up", "-d", *CORE_TARGETS)
    except subprocess.CalledProcessError:
        log.error("docker compose up failed")
        raise
