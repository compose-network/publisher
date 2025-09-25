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

    targets = list(CORE_TARGETS)
    if common.blockscout_enabled():
        for service in common.SERVICE_SELECTORS["blockscout"]:
            if service not in targets:
                targets.append(service)

    should_build = ctx.needs_bootstrap or ctx.changed.intersection({"op_geth", "optimism", "publisher"})
    if should_build:
        log.info("Building Docker images")
        try:
            common.docker_compose("build", "--parallel", *targets)
        except subprocess.CalledProcessError as exc:
            log.error("docker compose build failed")
            raise
    else:
        log.info("Skipping image rebuild (no changes detected)")

    log.info("Starting docker compose services")
    try:
        common.docker_compose("up", "-d", *targets)
    except subprocess.CalledProcessError:
        log.error("docker compose up failed")
        raise
