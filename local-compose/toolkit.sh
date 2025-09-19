#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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

usage() {
  cat <<'EOF'
Usage: ./toolkit.sh <command> [args]

Commands:
  deploy-op-geth        Rebuild op-geth images, restart services, wait for RPC readiness.
  debug-bridge [args]   Run the bridge diagnostics helper (scripts/debug_bridge.py --mode debug).
  check-bridge [args]   Quick health check (balances, stats, block heights).
  clear-nonces          Restart op-geth containers to flush pending tx pool/nonces.
  help                  Show this message.
EOF
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
