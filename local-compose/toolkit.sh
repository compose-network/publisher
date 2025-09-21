#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Default Foundry image (override via FOUNDRY_IMAGE if needed)
FOUNDRY_IMAGE="${FOUNDRY_IMAGE:-ghcr.io/foundry-rs/foundry:latest}"

load_env() {
  if [[ -f "${ROOT_DIR}/.env" ]]; then
    set -o allexport
    # shellcheck disable=SC1090
    source "${ROOT_DIR}/.env"
    set +o allexport
  fi
  if [[ -f "${ROOT_DIR}/toolkit.env" ]]; then
    set -o allexport
    # shellcheck disable=SC1090
    source "${ROOT_DIR}/toolkit.env"
    set +o allexport
  fi
}

rpc_request() {
  local url="$1"
  local payload="$2"
  curl -sS --fail --header "Content-Type: application/json" --data "$payload" "$url"
}

wait_for_rpc() {
  local url="$1"
  local label="$2"
  local tries=${3:-40}
  local delay=${4:-3}
  local payload='{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}'
  for ((i=1; i<=tries; i++)); do
    if response=$(rpc_request "$url" "$payload" 2>/dev/null); then
      if [[ "$response" =~ "result" ]]; then
        local block
        block=$(python3 - "$response" <<'PY'
import json, sys
try:
    data=json.loads(sys.argv[1])
    res=data.get("result")
    if isinstance(res,str) and res.startswith("0x"):
        print(int(res,16))
    else:
        print("-1")
except Exception:
    print("-1")
PY
)
        if [[ "$block" != "-1" ]]; then
          printf '[ready] %s at block %s\n' "$label" "$block"
          return 0
        fi
      fi
    fi
    sleep "$delay"
  done
  printf '[error] %s did not respond within timeout (%s attempts)\n' "$label" "$tries" >&2
  return 1
}

deploy_op_geth() {
  load_env
  local rpc_a="${TOOLKIT_ROLLUP_A_RPC:-${ROLLUP_A_RPC_URL:-http://127.0.0.1:18545}}"
  local rpc_b="${TOOLKIT_ROLLUP_B_RPC:-${ROLLUP_B_RPC_URL:-http://127.0.0.1:28545}}"

  echo "[deploy] rebuilding op-geth images"
  docker compose build op-geth-a op-geth-b >/dev/null

  echo "[deploy] ensuring shared publisher is running"
  docker compose up -d rollup-shared-publisher >/dev/null

  echo "[deploy] restarting op-geth services"
  docker compose up -d op-geth-a op-geth-b >/dev/null

  echo "[deploy] restarting rollup control plane"
  docker compose up -d op-node-a op-node-b op-batcher-a op-batcher-b op-proposer-a op-proposer-b >/dev/null

  echo "[deploy] waiting for RPC endpoints"
  wait_for_rpc "$rpc_a" "rollup-a" || exit 1
  wait_for_rpc "$rpc_b" "rollup-b" || exit 1
}

restart_txpools() {
  echo "[nonces] restarting op-geth containers to clear pending txs"
  docker compose restart op-geth-a op-geth-b >/dev/null
  load_env
  local rpc_a="${TOOLKIT_ROLLUP_A_RPC:-${ROLLUP_A_RPC_URL:-http://127.0.0.1:18545}}"
  local rpc_b="${TOOLKIT_ROLLUP_B_RPC:-${ROLLUP_B_RPC_URL:-http://127.0.0.1:28545}}"
  wait_for_rpc "$rpc_a" "rollup-a" || exit 1
  wait_for_rpc "$rpc_b" "rollup-b" || exit 1
  echo "[nonces] txpools cleared"
}

debug_bridge() {
  load_env
  python3 "${ROOT_DIR}/scripts/debug_bridge.py" --mode debug "$@"
}

check_bridge() {
  load_env
  python3 "${ROOT_DIR}/scripts/debug_bridge.py" --mode check "$@"
}

# Bridge 1 MTK from A to B, robustly:
# 1) Ensure enough MTK on A (optional mint)
# 2) Trigger xbridge (which causes SP to inject SEND and may execute receive on B)
# 3) Wait for B balance to increase by amount
# 4) Wait for ACK on A's mailbox, then submit Bridge.send on A to burn
# 5) Wait and verify final balances
bridge_once() {
  load_env

  local rpc_a="${TOOLKIT_ROLLUP_A_RPC:-${ROLLUP_A_RPC_URL:-http://127.0.0.1:18545}}"
  local rpc_b="${TOOLKIT_ROLLUP_B_RPC:-${ROLLUP_B_RPC_URL:-http://127.0.0.1:28545}}"
  local addr="${WALLET_ADDRESS:?WALLET_ADDRESS not set}"
  local pk="${WALLET_PRIVATE_KEY:?WALLET_PRIVATE_KEY not set}"
  local amt="1000000000000000000"  # 1e18 default
  local verify_wait=15
  local mint_if_needed=0
  local require_burn=0

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --amount)
        amt="$2"; shift 2 ;;
      --wait)
        verify_wait="$2"; shift 2 ;;
      --mint-if-needed)
        mint_if_needed=1; shift ;;
      --require-burn)
        require_burn=1; shift ;;
      *)
        echo "unknown arg: $1" >&2; return 2 ;;
    esac
  done

  # Contracts and chain IDs
  local token_a token_b bridge_a bridge_b mailbox_a mailbox_b chain_a chain_b
  token_a=$(jq -r .addresses.MyToken "${ROOT_DIR}/networks/rollup-a/contracts.json")
  token_b=$(jq -r .addresses.MyToken "${ROOT_DIR}/networks/rollup-b/contracts.json")
  bridge_a=$(jq -r .addresses.Bridge  "${ROOT_DIR}/networks/rollup-a/contracts.json")
  bridge_b=$(jq -r .addresses.Bridge  "${ROOT_DIR}/networks/rollup-b/contracts.json")
  mailbox_a=$(jq -r .addresses.Mailbox "${ROOT_DIR}/networks/rollup-a/contracts.json")
  mailbox_b=$(jq -r .addresses.Mailbox "${ROOT_DIR}/networks/rollup-b/contracts.json")
  chain_a=$(jq -r .chainInfo.chainId "${ROOT_DIR}/networks/rollup-a/contracts.json")
  chain_b=$(jq -r .chainInfo.chainId "${ROOT_DIR}/networks/rollup-b/contracts.json")

  [[ "$token_a" =~ ^0x && "$bridge_a" =~ ^0x && "$mailbox_a" =~ ^0x ]] || { echo "[error] missing contracts for rollup-a" >&2; return 1; }
  [[ "$token_b" =~ ^0x && "$bridge_b" =~ ^0x && "$mailbox_b" =~ ^0x ]] || { echo "[error] missing contracts for rollup-b" >&2; return 1; }

  wait_for_rpc "$rpc_a" "rollup-a" >/dev/null || return 1
  wait_for_rpc "$rpc_b" "rollup-b" >/dev/null || return 1

  # Helpers
  CAST() { "${ROOT_DIR}/toolkit.sh" cast "$@"; }
  bal_token() { # rpc token addr -> raw uint
    CAST call --rpc-url "$1" "$2" 'balanceOf(address)(uint256)' "$3" | tail -n1 | awk '{print $1}'
  }
  hexlabel_ack="0x41434b2053454e44"  # "ACK SEND"

  # Initial balances
  local a0 b0
  a0=$(bal_token "$rpc_a" "$token_a" "$addr")
  b0=$(bal_token "$rpc_b" "$token_b" "$addr")
  echo "[bridge] initial MTK balances  A=$a0  B=$b0"

  # Ensure funds on A
  if (( a0 < amt )); then
    if (( mint_if_needed )); then
      echo "[bridge] minting on A to reach required amount ($amt)"
      CAST send --rpc-url "$rpc_a" --private-key "$pk" "$token_a" 'mint(address,uint256)' "$addr" "$amt" >/dev/null
      sleep 2
      a0=$(bal_token "$rpc_a" "$token_a" "$addr")
      echo "[bridge] new A balance: $a0"
      if (( a0 < amt )); then echo "[error] mint on A did not reach required amount" >&2; return 1; fi
    else
      echo "[error] not enough MTK on A (have $a0, need $amt). Re-run with --mint-if-needed." >&2
      return 1
    fi
  fi

  # Trigger xbridge (atomic 2PC)
  echo "[bridge] triggering xbridge (A→B, amount=1e18 wei fixed by CLI)"
  (cd "${ROOT_DIR}/services/op-geth" && go run ./cmd/xbridge >/dev/null 2>&1) &

  # Poll for up to ~40s for both sides to reflect the transfer
  local target_b=$(( b0 + amt ))
  local target_a=$(( a0 - amt ))
  local a1 b1 okA=0 okB=0
  for ((i=1;i<=40;i++)); do
    a1=$(bal_token "$rpc_a" "$token_a" "$addr") || a1=0
    b1=$(bal_token "$rpc_b" "$token_b" "$addr") || b1=0
    (( b1 >= target_b )) && okB=1 || okB=0
    (( a1 == target_a )) && okA=1 || okA=0
    if (( (require_burn && okA==1 && okB==1) || (!require_burn && okB==1) )); then
      echo "[bridge] final MTK balances      A=$a1  B=$b1"
      echo "[bridge] SUCCESS"
      return 0
    fi
    sleep 1
  done
  echo "[bridge] final MTK balances      A=${a1:-?}  B=${b1:-?}"
  echo "[bridge] ERROR: balances did not change as expected within 40s." >&2
  return 1
}

mint_token() {
  load_env

  local chain="rollup-a"
  local amt="1000000000000000000"
  local recipient="${WALLET_ADDRESS:-}"
  local pk="${WALLET_PRIVATE_KEY:-}"
  local rpc_url=""
  local token_addr=""
  local settle_wait=5

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --chain)
        chain=$(printf '%s' "$2" | tr '[:upper:]' '[:lower:]'); shift 2 ;;
      --amount)
        amt="$2"; shift 2 ;;
      --to|--recipient)
        recipient="$2"; shift 2 ;;
      --rpc-url)
        rpc_url="$2"; shift 2 ;;
      --token)
        token_addr="$2"; shift 2 ;;
      --wait)
        settle_wait="$2"; shift 2 ;;
      *)
        echo "unknown arg: $1" >&2; return 2 ;;
    esac
  done

  [[ -n "$recipient" ]] || { echo "[error] recipient address not set (set WALLET_ADDRESS or pass --to)" >&2; return 1; }
  [[ -n "$pk" ]] || { echo "[error] WALLET_PRIVATE_KEY not set" >&2; return 1; }

  local network_dir label default_rpc
  case "$chain" in
    a|rollup-a)
      network_dir="rollup-a"
      label="rollup-a"
      default_rpc="${TOOLKIT_ROLLUP_A_RPC:-${ROLLUP_A_RPC_URL:-http://127.0.0.1:18545}}"
      ;;
    b|rollup-b)
      network_dir="rollup-b"
      label="rollup-b"
      default_rpc="${TOOLKIT_ROLLUP_B_RPC:-${ROLLUP_B_RPC_URL:-http://127.0.0.1:28545}}"
      ;;
    *)
      echo "[error] unsupported chain '$chain' (use rollup-a|rollup-b)" >&2
      return 2
      ;;
  esac

  rpc_url=${rpc_url:-$default_rpc}

  if [[ -z "$token_addr" ]]; then
    if ! token_addr=$(jq -r .addresses.MyToken "${ROOT_DIR}/networks/${network_dir}/contracts.json" 2>/dev/null); then
      echo "[error] could not read token address for $label" >&2
      return 1
    fi
  fi

  if [[ "$token_addr" == "null" || ! "$token_addr" =~ ^0x[0-9a-fA-F]{40}$ ]]; then
    echo "[error] invalid token address '$token_addr'" >&2
    return 1
  fi

  wait_for_rpc "$rpc_url" "$label" >/dev/null || return 1

  CAST() {
    "${ROOT_DIR}/toolkit.sh" cast "$@"
  }
  bal_token() {
    CAST call --rpc-url "$1" "$2" 'balanceOf(address)(uint256)' "$3" | tail -n1 | awk '{print $1}'
  }

  local before after expected
  before=$(bal_token "$rpc_url" "$token_addr" "$recipient") || before=0x0

  echo "[mint] current MTK balance on $label for $recipient: $before"
  echo "[mint] minting $amt wei to $recipient on $label"

  CAST send --rpc-url "$rpc_url" --private-key "$pk" "$token_addr" 'mint(address,uint256)' "$recipient" "$amt" >/dev/null

  sleep "$settle_wait"

  after=$(bal_token "$rpc_url" "$token_addr" "$recipient") || after=0x0
  expected=$(( before + amt ))

  echo "[mint] new MTK balance on $label for $recipient: $after"
  if (( after < expected )); then
    echo "[warn] balance increase ($after) below expected >= $expected" >&2
  else
    echo "[mint] SUCCESS"
  fi
}

usage() {
  cat <<'EOF'
Usage: ./toolkit.sh <command> [args]

Commands:
  deploy-op-geth        Rebuild op-geth images, restart services, wait for RPC readiness.
  debug-bridge [args]   Run the bridge diagnostics helper (scripts/debug_bridge.py --mode debug).
  check-bridge [args]   Quick health check (balances, stats, block heights).
  bridge-once [args]    Bridge 1 MTK A→B once; waits and verifies. Args: [--amount WEI] [--mint-if-needed] [--wait SECS]
  mint [args]           Mint MTK on a rollup. Args: [--chain rollup-a|rollup-b] [--amount WEI] [--to ADDRESS] [--wait SECS]
  cast [args]           Run Foundry's `cast` via container with safe entrypoint/networking.
  clear-nonces          Restart op-geth containers to flush pending tx pool/nonces.
  help                  Show this message.
EOF
}

# --- Foundry CAST wrapper ---------------------------------------------------
# Runs `cast` inside the Foundry image, bypassing the default `forge` entrypoint.
# Linux: uses --network host so localhost:<port> works.
# macOS: rewrites --rpc-url localhost/127.0.0.1 to host.docker.internal.
cmd_cast() {
  load_env

  local image="$FOUNDRY_IMAGE"
  local os="$(uname -s)"
  local -a net_args=()

  if [[ "$os" == "Linux" ]]; then
    net_args+=(--network host)
  fi

  if [[ "$os" == "Darwin" ]]; then
    local -a rewritten=()
    local prev=""
    for arg in "$@"; do
      if [[ "$prev" == "--rpc-url" || "$prev" == "-r" ]]; then
        arg="${arg//http:\/\/localhost/http:\/\/host.docker.internal}"
        arg="${arg//http:\/\/127.0.0.1/http:\/\/host.docker.internal}"
      fi
      rewritten+=("$arg")
      prev="$arg"
    done
    set -- "${rewritten[@]}"
    net_args+=(--add-host=host.docker.internal:host-gateway)
  fi

  if [[ ${#net_args[@]:-0} -gt 0 ]]; then
    exec docker run --rm "${net_args[@]}" -e FOUNDRY_DISABLE_NIGHTLY_WARNING=1 --entrypoint cast "$image" "$@"
  else
    exec docker run --rm -e FOUNDRY_DISABLE_NIGHTLY_WARNING=1 --entrypoint cast "$image" "$@"
  fi
}

main() {
  local cmd=${1:-help}
  shift || true
  case "$cmd" in
    deploy-op-geth)
      deploy_op_geth "$@"
      ;;
    debug-bridge)
      debug_bridge "$@"
      ;;
    check-bridge)
      check_bridge "$@"
      ;;
    bridge-once)
      bridge_once "$@"
      ;;
    mint)
      mint_token "$@"
      ;;
    cast)
      cmd_cast "$@"
      ;;
    clear-nonces)
      restart_txpools "$@"
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      echo "Unknown command: $cmd" >&2
      usage >&2
      exit 1
      ;;
  esac
}

main "$@"
