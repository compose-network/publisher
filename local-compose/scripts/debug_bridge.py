#!/usr/bin/env python3
"""Compose bridge debugging helper.

Collects recent mailbox activity, shared publisher status, and targeted op-geth log snippets.
Configuration is sourced from `toolkit.env` (if present) overriding `.env`.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple

REPO_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_CONFIG_FILES = [REPO_ROOT / "toolkit.env", REPO_ROOT / ".env"]

# Known mailbox selectors across legacy and current contracts
SELECTOR_MAP = {
    "308508ff": "write(uint256,address,uint256,bytes,bytes)",
    "9b8b9a26": "write(uint256,uint256,address,address,uint256,bytes)",
    "222a7194": "putInbox(uint256,address,address,uint256,bytes,bytes)",
    "45f303fe": "putInbox(uint256,uint256,address,address,uint256,bytes)",
    "fa67378b": "read(uint256,address,address,uint256,bytes)",
    "a19ad3c7": "write(uint256,uint256,address,uint256,bytes,bytes)",
    "8c29401f": "putInbox(uint256,uint256,address,uint256,bytes,bytes)",
    "bd8b74e8": "read(uint256,uint256,address,uint256,bytes)",
    "52efea6e": "clear()",
}

LOG_KEYWORDS = ["mailbox", "SSV", "Send CIRC", "putInbox", "Tracer captured"]


def load_env(files: Iterable[Path]) -> Dict[str, str]:
    env: Dict[str, str] = {}
    for file in files:
        if not file.exists():
            continue
        for raw_line in file.read_text().splitlines():
            line = raw_line.strip()
            if not line or line.startswith("#"):
                continue
            if "=" not in line:
                continue
            key, value = line.split("=", 1)
            env.setdefault(key.strip(), value.strip())
    return env


def read_json(path: Path) -> Dict[str, Any]:
    try:
        return json.loads(path.read_text())
    except FileNotFoundError:
        raise SystemExit(f"missing required file: {path}")


@dataclass
class Rollup:
    name: str
    rpc_url: str
    container: str
    chain_id: int
    mailbox: str
    bridge: str
    token: str
    wallet_address: str


def rpc_call(url: str, method: str, params: Optional[list] = None, *, timeout: float = 15.0) -> Any:
    payload = json.dumps({
        "jsonrpc": "2.0",
        "id": int(time.time() * 1000) % 1_000_000,
        "method": method,
        "params": params or [],
    }).encode()
    req = urllib.request.Request(url, data=payload, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = json.loads(resp.read())
    except urllib.error.URLError as exc:
        raise RuntimeError(f"RPC call {method} failed against {url}: {exc}")
    if "error" in data:
        err = data["error"]
        raise RuntimeError(f"RPC error {method}: {err}")
    return data.get("result")


def hex_to_int(value: Optional[str]) -> Optional[int]:
    if value is None:
        return None
    return int(value, 16)


def int_to_hex(value: int) -> str:
    return hex(value)


def chunk32(data: bytes) -> List[bytes]:
    return [data[i:i + 32] for i in range(0, len(data), 32)]


def decode_bytes(data: bytes, offset: int) -> bytes:
    if offset < 0 or offset + 32 > len(data):
        return b""
    length = int.from_bytes(data[offset:offset + 32], "big")
    start = offset + 32
    end = start + length
    if end > len(data):
        return b""
    return data[start:end]


def format_bytes(value: bytes) -> str:
    if not value:
        return "0x"
    try:
        decoded = value.decode("utf-8")
    except UnicodeDecodeError:
        return "0x" + value.hex()
    printable = all(32 <= ord(ch) <= 126 for ch in decoded)
    return decoded if printable else "0x" + value.hex()


def decode_mailbox_call(input_data: str) -> Dict[str, Any]:
    if not input_data or input_data == "0x":
        return {}
    payload = bytes.fromhex(input_data[2:])
    if len(payload) < 4:
        return {"raw": input_data}
    selector = payload[:4].hex()
    body = payload[4:]
    words = chunk32(body)
    info: Dict[str, Any] = {"selector": selector, "fn": SELECTOR_MAP.get(selector, "unknown")}

    try:
        if selector == "308508ff":  # write(uint256,address,uint256,bytes,bytes)
            if len(words) < 5:
                return info
            chain_dest = int.from_bytes(words[0], "big")
            receiver = "0x" + words[1][-20:].hex()
            session_id = int.from_bytes(words[2], "big")
            label_offset = int.from_bytes(words[3], "big")
            data_offset = int.from_bytes(words[4], "big")
            label = decode_bytes(body, label_offset)
            data_field = decode_bytes(body, data_offset)
            info.update({
                "type": "write",
                "chain_dest": chain_dest,
                "receiver": receiver,
                "session_id": session_id,
                "label": format_bytes(label),
                "data": format_bytes(data_field),
            })
        elif selector == "9b8b9a26":  # write(uint256,uint256,address,address,uint256,bytes)
            if len(words) < 6:
                return info
            chain_src = int.from_bytes(words[0], "big")
            chain_dest = int.from_bytes(words[1], "big")
            sender = "0x" + words[2][-20:].hex()
            receiver = "0x" + words[3][-20:].hex()
            session_id = int.from_bytes(words[4], "big")
            data_offset = int.from_bytes(words[5], "big")
            data_field = decode_bytes(body, data_offset)
            info.update({
                "type": "write",
                "chain_src": chain_src,
                "chain_dest": chain_dest,
                "sender": sender,
                "receiver": receiver,
                "session_id": session_id,
                "label": "LEGACY",
                "data": format_bytes(data_field),
            })
        elif selector == "a19ad3c7":  # write(uint256,uint256,address,uint256,bytes,bytes)
            if len(words) < 6:
                return info
            chain_src = int.from_bytes(words[0], "big")
            chain_dest = int.from_bytes(words[1], "big")
            receiver = "0x" + words[2][-20:].hex()
            session_id = int.from_bytes(words[3], "big")
            data_offset = int.from_bytes(words[4], "big")
            label_offset = int.from_bytes(words[5], "big")
            data_field = decode_bytes(body, data_offset)
            label = decode_bytes(body, label_offset)
            info.update({
                "type": "write",
                "chain_src": chain_src,
                "chain_dest": chain_dest,
                "receiver": receiver,
                "session_id": session_id,
                "data": format_bytes(data_field),
                "label": format_bytes(label),
            })
        elif selector in {"222a7194", "45f303fe"}:  # putInbox variants
            if len(words) < 6:
                return info
            chain_src = int.from_bytes(words[0], "big")
            receiver = "0x" + words[2][-20:].hex()
            session_id = int.from_bytes(words[3], "big")
            label_offset = int.from_bytes(words[4], "big")
            data_offset = int.from_bytes(words[5], "big")
            label = decode_bytes(body, label_offset)
            data_field = decode_bytes(body, data_offset)
            info.update({
                "type": "putInbox",
                "chain_src": chain_src,
                "receiver": receiver,
                "session_id": session_id,
                "label": format_bytes(label),
                "data": format_bytes(data_field),
            })
        elif selector == "8c29401f":  # putInbox(uint256,uint256,address,uint256,bytes,bytes)
            if len(words) < 6:
                return info
            chain_src = int.from_bytes(words[0], "big")
            chain_dest = int.from_bytes(words[1], "big")
            receiver = "0x" + words[2][-20:].hex()
            session_id = int.from_bytes(words[3], "big")
            data_offset = int.from_bytes(words[4], "big")
            label_offset = int.from_bytes(words[5], "big")
            data_field = decode_bytes(body, data_offset)
            label = decode_bytes(body, label_offset)
            info.update({
                "type": "putInbox",
                "chain_src": chain_src,
                "chain_dest": chain_dest,
                "receiver": receiver,
                "session_id": session_id,
                "data": format_bytes(data_field),
                "label": format_bytes(label),
            })
        elif selector == "fa67378b":
            if len(words) < 5:
                return info
            chain_src = int.from_bytes(words[0], "big")
            sender = "0x" + words[1][-20:].hex()
            receiver = "0x" + words[2][-20:].hex()
            session_id = int.from_bytes(words[3], "big")
            label_offset = int.from_bytes(words[4], "big")
            label = decode_bytes(body, label_offset)
            info.update({
                "type": "read",
                "chain_src": chain_src,
                "sender": sender,
                "receiver": receiver,
                "session_id": session_id,
                "label": format_bytes(label),
            })
        elif selector == "bd8b74e8":  # read(uint256,uint256,address,uint256,bytes)
            if len(words) < 5:
                return info
            chain_src = int.from_bytes(words[0], "big")
            chain_dest = int.from_bytes(words[1], "big")
            receiver = "0x" + words[2][-20:].hex()
            session_id = int.from_bytes(words[3], "big")
            label_offset = int.from_bytes(words[4], "big")
            label = decode_bytes(body, label_offset)
            info.update({
                "type": "read",
                "chain_src": chain_src,
                "chain_dest": chain_dest,
                "receiver": receiver,
                "session_id": session_id,
                "label": format_bytes(label),
            })
        elif selector == "52efea6e":  # clear()
            info.update({
                "type": "clear",
            })
    except Exception as exc:  # best effort decode
        info.setdefault("error", str(exc))
        info.setdefault("raw", input_data)
    return info


def traverse_calls(call: Dict[str, Any], mailbox: str, current: List[Dict[str, Any]], *, block: int, tx_hash: str, origin: str) -> None:
    to_addr = call.get("to")
    if to_addr and to_addr.lower() == mailbox:
        details = decode_mailbox_call(call.get("input", "0x"))
        details.update({
            "block": block,
            "tx": tx_hash,
            "from": call.get("from"),
            "value": call.get("value"),
        })
        current.append(details)
    for child in call.get("calls", []) or []:
        traverse_calls(child, mailbox, current, block=block, tx_hash=tx_hash, origin=origin)


def collect_mailbox_activity(rollup: Rollup, block_window: int, session_filter: Optional[int]) -> List[Dict[str, Any]]:
    latest_hex = rpc_call(rollup.rpc_url, "eth_blockNumber")
    latest = int(latest_hex, 16)
    first = max(0, latest - block_window + 1)
    results: List[Dict[str, Any]] = []
    for number in range(latest, first - 1, -1):
        block_data = rpc_call(rollup.rpc_url, "eth_getBlockByNumber", [hex(number), True])
        if not block_data:
            continue
        txs: List[Dict[str, Any]] = block_data.get("transactions", [])
        for tx in txs:
            tx_hash = tx.get("hash")
            if not tx_hash:
                continue
            try:
                trace = rpc_call(rollup.rpc_url, "debug_traceTransaction", [tx_hash, {"tracer": "callTracer"}])
            except RuntimeError:
                continue
            traverse_calls(trace, rollup.mailbox.lower(), results, block=number, tx_hash=tx_hash, origin=tx.get("from", ""))
    if session_filter is not None:
        results = [entry for entry in results if entry.get("session_id") == session_filter]
    results.sort(key=lambda item: (item.get("block", 0), item.get("tx", "")))
    return results


def fetch_shared_publisher_stats(url: str) -> Dict[str, Any]:
    try:
        with urllib.request.urlopen(url, timeout=10) as resp:
            return json.loads(resp.read())
    except urllib.error.URLError as exc:
        raise RuntimeError(f"failed to reach shared publisher stats at {url}: {exc}")


def capture_logs(container: str, since: str = "120s") -> List[str]:
    cmd = ["docker", "logs", container, f"--since={since}"]
    try:
        raw = subprocess.check_output(cmd, stderr=subprocess.STDOUT, text=True)
    except subprocess.CalledProcessError as exc:
        return [f"(failed to fetch logs for {container}: {exc.output.strip()})"]
    interesting: List[str] = []
    pattern = re.compile("|".join(re.escape(k) for k in LOG_KEYWORDS), re.IGNORECASE)
    for line in raw.splitlines():
        if pattern.search(line):
            interesting.append(line)
    return interesting[-20:]


def format_activity(entries: List[Dict[str, Any]]) -> List[str]:
    formatted: List[str] = []
    for entry in entries:
        label = entry.get("label", "?")
        session = entry.get("session_id")
        fn = entry.get("fn")
        block = entry.get("block")
        tx = entry.get("tx")
        data = entry.get("data", "")
        formatted.append(
            f"block {block} tx {tx[:10]}… fn={fn} session={session} label={label} data={data}"
        )
    return formatted


def eth_get_balance(rpc_url: str, address: str) -> Optional[int]:
    try:
        result = rpc_call(rpc_url, "eth_getBalance", [address, "latest"])
        return int(result, 16)
    except RuntimeError:
        return None


def summarize_eth_balances(rollups: List[Rollup]) -> List[str]:
    rows: List[str] = []
    for rollup in rollups:
        balance_wei = eth_get_balance(rollup.rpc_url, rollup.wallet_address)
        if balance_wei is None:
            rows.append(f"{rollup.name}: balance query failed")
        else:
            eth_value = balance_wei / 10**18
            rows.append(f"{rollup.name}: balance {eth_value:.4f} ETH ({balance_wei} wei)")
    return rows


def erc20_balance(rpc_url: str, token_address: str, account: str) -> Optional[int]:
    if not token_address:
        return None
    account_hex = account.lower()
    if account_hex.startswith("0x"):
        account_hex = account_hex[2:]
    data = "0x70a08231" + account_hex.rjust(64, "0")
    params = [{"to": token_address, "data": data}, "latest"]
    try:
        result = rpc_call(rpc_url, "eth_call", params)
    except RuntimeError:
        return None
    if not isinstance(result, str):
        return None
    try:
        return int(result, 16)
    except (TypeError, ValueError):
        return None


def summarize_token_balances(rollups: List[Rollup]) -> List[str]:
    rows: List[str] = []
    for rollup in rollups:
        token_address = rollup.token
        if not token_address:
            rows.append(f"{rollup.name}: token address unavailable")
            continue
        balance_raw = erc20_balance(rollup.rpc_url, token_address, rollup.wallet_address)
        if balance_raw is None:
            rows.append(f"{rollup.name}: token balance query failed ({token_address})")
            continue
        token_value = balance_raw / 10**18
        rows.append(
            f"{rollup.name}: token balance {token_value:.4f} ({balance_raw} raw) [{token_address}]"
        )
    return rows


def run(mode: str, session: Optional[int], block_window: int, since_logs: str) -> int:
    env = load_env(DEFAULT_CONFIG_FILES)
    wallet = env.get("WALLET_ADDRESS")
    if not wallet:
        raise SystemExit("WALLET_ADDRESS not set in .env or toolkit.env")

    configs: List[Rollup] = []
    for name, container, env_rpc_key, default_rpc, contracts_path in [
        ("rollup-a", "op-geth-a", "TOOLKIT_ROLLUP_A_RPC", "http://127.0.0.1:18545", REPO_ROOT / "networks/rollup-a/contracts.json"),
        ("rollup-b", "op-geth-b", "TOOLKIT_ROLLUP_B_RPC", "http://127.0.0.1:28545", REPO_ROOT / "networks/rollup-b/contracts.json"),
    ]:
        rpc = env.get(env_rpc_key) or env.get(f"{env_rpc_key[9:]}" if env_rpc_key.startswith("TOOLKIT_") else env_rpc_key) or env.get(f"{name.upper().replace('-', '_')}_RPC_URL") or default_rpc
        data = read_json(contracts_path)
        addresses = {k.lower(): v for k, v in data.get("addresses", {}).items()}
        mailbox = addresses.get("mailbox")
        bridge = addresses.get("bridge")
        token = addresses.get("mytoken")
        if not mailbox:
            raise SystemExit(f"Missing Mailbox address in {contracts_path}")
        chain_id = data.get("chainInfo", {}).get("chainId")
        configs.append(Rollup(
            name=name,
            rpc_url=rpc,
            container=container,
            chain_id=chain_id,
            mailbox=mailbox.lower(),
            bridge=bridge,
            token=token,
            wallet_address=wallet,
        ))

    stats_url = env.get("TOOLKIT_SP_STATS_URL") or "http://127.0.0.1:18081/stats"

    if mode == "check":
        print("Balances:")
        for line in summarize_eth_balances(configs):
            print("  ", line)
        token_lines = summarize_token_balances(configs)
        if token_lines:
            print("Token balances:")
            for line in token_lines:
                print("  ", line)
        try:
            stats = fetch_shared_publisher_stats(stats_url)
            active = stats.get("active_2pc_transactions")
            slot = stats.get("current_slot")
            state = stats.get("current_state")
            print("Shared publisher:")
            print(f"  stats URL: {stats_url}")
            print(f"  current_slot={slot} active_2pc={active} state={state}")
        except RuntimeError as exc:
            print(f"Shared publisher stats unavailable: {exc}")
        for rollup in configs:
            try:
                block_hex = rpc_call(rollup.rpc_url, "eth_blockNumber")
                print(f"  {rollup.name} latest block: {int(block_hex, 16)}")
            except RuntimeError as exc:
                print(f"  {rollup.name} block query failed: {exc}")
        return 0

    # debug mode
    print(f"Collecting mailbox activity (last {block_window} blocks)…")
    for rollup in configs:
        try:
            activity = collect_mailbox_activity(rollup, block_window, session)
        except RuntimeError as exc:
            print(f"[{rollup.name}] failed to collect activity: {exc}")
            continue
        if not activity:
            print(f"[{rollup.name}] No mailbox activity detected in last {block_window} blocks")
            continue
        print(f"[{rollup.name}] Mailbox activity:")
        for line in format_activity(activity):
            print("  ", line)

    try:
        stats = fetch_shared_publisher_stats(stats_url)
        print("Shared publisher stats:")
        print(json.dumps(stats, indent=2)[:2000])
    except RuntimeError as exc:
        print(f"Shared publisher stats unavailable: {exc}")

    print("Recent balances:")
    for line in summarize_eth_balances(configs):
        print("  ", line)
    token_lines = summarize_token_balances(configs)
    if token_lines:
        print("Token balances:")
        for line in token_lines:
            print("  ", line)

    print("Log snippets:")
    for rollup in configs:
        lines = capture_logs(rollup.container, since=since_logs)
        print(f"[{rollup.container}]")
        if lines:
            for line in lines:
                print("  ", line)
        else:
            print("  (no matching log lines in window)")

    return 0


def main() -> None:
    parser = argparse.ArgumentParser(description="Compose bridge diagnostics helper")
    parser.add_argument("--mode", choices=["debug", "check"], default="debug")
    parser.add_argument("--session", type=int, help="filter by session id", default=None)
    parser.add_argument("--blocks", type=int, default=12, help="number of recent blocks to scan")
    parser.add_argument("--since", default="120s", help="docker logs --since window (e.g. 5m)")
    args = parser.parse_args()

    try:
        rc = run(args.mode, args.session, args.blocks, args.since)
    except Exception as exc:  # ensure non-zero exit
        print(f"Error: {exc}", file=sys.stderr)
        rc = 1
    sys.exit(rc)


if __name__ == "__main__":
    main()
