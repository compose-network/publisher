#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
source "$ROOT_DIR/scripts/lib.sh"

load_env

if [[ -n ${LOCAL_OP_GETH_PATH:-} && -z ${OP_GETH_PATH:-} ]]; then
  log "[WARN] LOCAL_OP_GETH_PATH is deprecated; use OP_GETH_PATH"
  OP_GETH_PATH="${LOCAL_OP_GETH_PATH}"
fi

if [[ -n ${LOCAL_ROLLUP_SHARED_PUBLISHER_PATH:-} && -z ${ROLLUP_SHARED_PUBLISHER_PATH:-} ]]; then
  log "[WARN] LOCAL_ROLLUP_SHARED_PUBLISHER_PATH is deprecated; use ROLLUP_SHARED_PUBLISHER_PATH"
  ROLLUP_SHARED_PUBLISHER_PATH="${LOCAL_ROLLUP_SHARED_PUBLISHER_PATH}"
fi

require_env HOODI_EL_RPC
require_env HOODI_CL_RPC
require_env WALLET_PRIVATE_KEY
require_env WALLET_ADDRESS

DEFAULT_HOODI_CHAIN_ID=560048
HOODI_CHAIN_ID=${HOODI_CHAIN_ID:-$DEFAULT_HOODI_CHAIN_ID}
DEFAULT_ROLLUP_A_CHAIN_ID=77771
DEFAULT_ROLLUP_B_CHAIN_ID=77772
ROLLUP_A_CHAIN_ID=${ROLLUP_A_CHAIN_ID:-$DEFAULT_ROLLUP_A_CHAIN_ID}
ROLLUP_B_CHAIN_ID=${ROLLUP_B_CHAIN_ID:-$DEFAULT_ROLLUP_B_CHAIN_ID}
export ROLLUP_A_CHAIN_ID ROLLUP_B_CHAIN_ID

SERVICES_DIR=${SERVICES_DIR:-$ROOT_DIR/services}

OP_DEPLOYER_IMAGE=${OP_DEPLOYER_IMAGE:-local/op-deployer:dev}
L1_CONTRACTS_TAG=${L1_CONTRACTS_TAG:-tag://op-contracts/v3.0.0}
L2_CONTRACTS_TAG=${L2_CONTRACTS_TAG:-tag://op-contracts/v3.0.0}
DEPLOYMENT_TARGET=${DEPLOYMENT_TARGET:-live}
HOST_UID=$(id -u)
HOST_GID=$(id -g)
DOCKER_USER_FLAG="--user ${HOST_UID}:${HOST_GID}"
STATE_DIR="$ROOT_DIR/state/op-deployer"
ROLLUP_A_DIR="$ROOT_DIR/networks/rollup-a"
ROLLUP_B_DIR="$ROOT_DIR/networks/rollup-b"
OPTIMISM_DIR="${OPTIMISM_PATH:-$SERVICES_DIR/optimism}"
OPTIMISM_REPO=${OPTIMISM_REPO:-https://github.com/ethereum-optimism/optimism.git}
OPTIMISM_REF=${OPTIMISM_REF:-op-node/v1.13.4}
OP_GETH_DIR="${OP_GETH_PATH:-$SERVICES_DIR/op-geth}"
OP_GETH_REPO=${OP_GETH_REPO:-https://github.com/ssvlabs/op-geth.git}
OP_GETH_BRANCH=${OP_GETH_BRANCH:-feat/configurable-addresses}
ROLLUP_SP_SOURCE=${ROLLUP_SP_SOURCE:-$ROOT_DIR/..}
ROLLUP_SP_DIR="${ROLLUP_SHARED_PUBLISHER_PATH:-$SERVICES_DIR/rollup-shared-publisher}"
CONTRACTS_SOURCE=${CONTRACTS_SOURCE:-$ROOT_DIR/contracts}
CONTRACTS_DIR=${CONTRACTS_DIR:-$ROOT_DIR/contracts}
ROLLUP_A_RPC_URL=${ROLLUP_A_RPC_URL:-http://localhost:18545}
ROLLUP_B_RPC_URL=${ROLLUP_B_RPC_URL:-http://localhost:28545}
ROLLUP_A_RPC_URL_CONTAINER=${ROLLUP_A_RPC_URL_CONTAINER:-http://op-geth-a:8545}
ROLLUP_B_RPC_URL_CONTAINER=${ROLLUP_B_RPC_URL_CONTAINER:-http://op-geth-b:8545}
DEPLOY_CONTRACTS=${DEPLOY_CONTRACTS:-1}
FOUNDRY_IMAGE=${FOUNDRY_IMAGE:-ghcr.io/foundry-rs/foundry:latest}
FOUNDRY_HOME_DIR=${FOUNDRY_HOME_DIR:-/tmp/foundry}
FOUNDRY_NETWORK=${FOUNDRY_NETWORK:-$(basename "$ROOT_DIR")_default}
ROLLUP_ACCOUNT_GENESIS_BALANCE_WEI=${ROLLUP_ACCOUNT_GENESIS_BALANCE_WEI:-100000000000000000}
export ROLLUP_ACCOUNT_GENESIS_BALANCE_WEI
ROLLUP_PRAGUE_TIMESTAMP=${ROLLUP_PRAGUE_TIMESTAMP:-0}
ROLLUP_ISTHMUS_TIMESTAMP=${ROLLUP_ISTHMUS_TIMESTAMP:-$ROLLUP_PRAGUE_TIMESTAMP}
export ROLLUP_PRAGUE_TIMESTAMP
export ROLLUP_ISTHMUS_TIMESTAMP
GENESIS_HASH_CACHE_DIR=${GENESIS_HASH_CACHE_DIR:-$ROOT_DIR/.cache/genesis-go}
export GENESIS_HASH_CACHE_DIR

if [[ -n ${OP_GETH_PATH:-} && -z ${OP_GETH_SKIP_SYNC:-} ]]; then
  OP_GETH_SKIP_SYNC=1
fi

if [[ -n ${ROLLUP_SHARED_PUBLISHER_PATH:-} && -z ${ROLLUP_SP_SKIP_SYNC:-} ]]; then
  ROLLUP_SP_SKIP_SYNC=1
fi

if ! command -v docker >/dev/null 2>&1; then
  log "[ERROR] docker is required to run setup"
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  log "[ERROR] docker compose v2 is required"
  exit 1
fi

mkdir -p "$SERVICES_DIR"

clone_repo_if_missing() {
  local repo=$1
  local dest=$2
  local ref=${3:-}

  if [[ -d "$dest/.git" ]]; then
    log "Using existing $(basename "$dest") checkout at $dest"
    return
  fi

  if [[ -d "$dest" ]]; then
    log "Removing existing non-git directory $dest"
    rm -rf "$dest"
  fi

  if [[ -n "$ref" ]]; then
    log "Cloning $repo ($ref) into $dest"
    if git clone --branch "$ref" --single-branch "$repo" "$dest" >/dev/null 2>&1; then
      return
    fi
    log "[WARN] git clone with --branch $ref failed; falling back to full clone"
  else
    log "Cloning $repo into $dest"
  fi

  git clone "$repo" "$dest"

  if [[ -n "$ref" ]]; then
    if git -C "$dest" checkout "$ref" >/dev/null 2>&1; then
      return
    fi
    log "[WARN] Unable to checkout $ref in $dest; leaving default branch"
  fi
}

copy_rollup_shared_publisher() {
  if [[ ${ROLLUP_SP_SKIP_SYNC:-0} == 1 ]]; then
    log "Skipping rollup-shared-publisher sync (ROLLUP_SP_SKIP_SYNC=1)"
    return
  fi

  if [[ ! -d "$ROLLUP_SP_SOURCE" ]]; then
    log "[WARN] rollup-shared-publisher source not found at $ROLLUP_SP_SOURCE"
    log "       TODO: switch to cloning the private repository once available."
    return
  fi

  if [[ -d "$ROLLUP_SP_DIR/.git" || -d "$ROLLUP_SP_DIR" ]]; then
    log "Using existing rollup-shared-publisher at $ROLLUP_SP_DIR"
    return
  fi

  log "Syncing rollup-shared-publisher from $ROLLUP_SP_SOURCE"
  mkdir -p "$ROLLUP_SP_DIR"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete --exclude .git "$ROLLUP_SP_SOURCE"/ "$ROLLUP_SP_DIR"/
  else
    rm -rf "$ROLLUP_SP_DIR"
    cp -a "$ROLLUP_SP_SOURCE" "$ROLLUP_SP_DIR"
    rm -rf "$ROLLUP_SP_DIR/.git"
  fi
  log "rollup-shared-publisher synced"
  log "TODO: replace local copy with a git clone once the repository is public."
}

copy_contract_bundle() {
  if [[ ${CONTRACTS_SKIP_SYNC:-0} == 1 ]]; then
    log "Skipping contracts bundle sync (CONTRACTS_SKIP_SYNC=1)"
    return
  fi

  if [[ -d "$CONTRACTS_DIR" && -n "$(ls -A "$CONTRACTS_DIR" 2>/dev/null)" ]]; then
    log "Using existing contracts bundle at $CONTRACTS_DIR"
    return
  fi

  if [[ ! -d "$CONTRACTS_SOURCE" ]]; then
    log "[ERROR] Contracts source not found at $CONTRACTS_SOURCE"
    log "        Provide a Foundry project via CONTRACTS_SOURCE or disable deployment with DEPLOY_CONTRACTS=0."
    exit 1
  fi

  log "Syncing contracts bundle from $CONTRACTS_SOURCE"
  rm -rf "$CONTRACTS_DIR"
  mkdir -p "$CONTRACTS_DIR"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete --exclude .git "$CONTRACTS_SOURCE"/ "$CONTRACTS_DIR"/
  else
    cp -a "$CONTRACTS_SOURCE"/. "$CONTRACTS_DIR"/
    rm -rf "$CONTRACTS_DIR/.git"
  fi
  log "Contracts bundle synced to $CONTRACTS_DIR"
}

reset_workspace() {
  log "Resetting workspace (state, networks, contracts)"
  rm -rf "$ROOT_DIR/state" "$ROOT_DIR/networks"

  if [[ -d "$CONTRACTS_DIR" ]]; then
    local sub
    for sub in artifacts broadcast cache out lib; do
      rm -rf "$CONTRACTS_DIR/$sub"
    done
    mkdir -p "$CONTRACTS_DIR/artifacts" "$CONTRACTS_DIR/broadcast" "$CONTRACTS_DIR/cache" "$CONTRACTS_DIR/out"
  fi
}

stop_compose_stack() {
  log "Stopping any existing Compose stack"
  (cd "$ROOT_DIR" && docker compose down -v >/dev/null 2>&1 || true)
}

start_compose_stack() {
  log "Starting Compose stack (build + up)"
  (cd "$ROOT_DIR" && docker compose up --build -d)
}

get_balance() {
  local url=$1
  python3 - "$url" "$wallet_address_norm" <<'PY'
import json
import sys
import urllib.request

url, addr = sys.argv[1:3]
payload = json.dumps({
    "jsonrpc": "2.0",
    "id": 1,
    "method": "eth_getBalance",
    "params": [addr, "latest"],
}).encode()
req = urllib.request.Request(
    url,
    data=payload,
    headers={"Content-Type": "application/json"},
)
try:
    with urllib.request.urlopen(req, timeout=5) as resp:
        data = json.load(resp)
    print(int(data.get("result", "0x0"), 16))
except Exception:
    print(-1)
PY
}

load_portal_address() {
  local file=$1
  python3 - "$file" <<'PY'
import json
import sys

path = sys.argv[1]
try:
    with open(path) as f:
        data = json.load(f)
except FileNotFoundError:
    print("")
    sys.exit(0)

if isinstance(data, dict):
    portal = data.get('OPTIMISM_PORTAL')
    if portal:
        print(portal)
        sys.exit(0)
    parent = data.get('parent')
    if isinstance(parent, dict):
        addresses = parent.get('addresses', {})
        portal = addresses.get('OPTIMISM_PORTAL')
        if portal:
            print(portal)
            sys.exit(0)

print("")
PY
}

wait_for_balance() {
  local name=$1
  local url=$2
  local target=$3
  local attempts=${ROLLUP_DEPOSIT_WAIT_ATTEMPTS:-120}
  local delay=${ROLLUP_DEPOSIT_WAIT_DELAY:-5}
  local i=0
  while (( i < attempts )); do
    local balance
    balance=$(get_balance "$url")
    if python3 - "$balance" "$target" <<'PY'
import sys
bal=int(sys.argv[1])
target=int(sys.argv[2])
sys.exit(0 if bal >= target else 1)
PY
    then
      log "$name balance ready: $balance wei"
      return 0
    fi
    sleep "$delay"
    ((i++))
  done
  echo "Timed out waiting for $name balance to reach $target wei" >&2
  return 1
}

fund_rollup_account() {
  local name=$1
  local url=$2
  local addresses_file=$3
  local min_balance=${ROLLUP_ACCOUNT_MIN_BALANCE_WEI:-1000000000000000000}
  local deposit_amount=${ROLLUP_ACCOUNT_DEPOSIT_WEI:-0}
  local gas_limit=${ROLLUP_DEPOSIT_GAS_LIMIT:-200000}

  local balance
  balance=$(get_balance "$url")
  if python3 - "$balance" "$min_balance" <<'PY'
import sys
bal=int(sys.argv[1])
target=int(sys.argv[2])
sys.exit(0 if bal >= target else 1)
PY
  then
    log "$name wallet already funded (balance: $balance wei)"
    return
  fi

  if python3 - "$deposit_amount" <<'PY'
import sys
sys.exit(0 if int(sys.argv[1]) > 0 else 1)
PY
  then
    :
  else
    log "Deposit amount for $name is 0; skipping L1 bridge"
    return
  fi

  local portal
  portal=$(load_portal_address "$addresses_file")
  if [[ -z "$portal" ]]; then
    log "[WARN] Could not determine OptimismPortal for $name (checked $addresses_file); skipping deposit"
    return
  fi

  log "Depositing $(printf '%s' "$deposit_amount") wei from L1 into $name via $portal"
  if ! docker run --rm $DOCKER_USER_FLAG \
    --entrypoint cast \
    "$FOUNDRY_IMAGE" \
    send --rpc-url "$HOODI_EL_RPC" \
    --private-key "$forge_private_key_norm" \
    "$portal" \
    "depositTransaction(address,uint256,uint64,bool,bytes)" \
    "$wallet_address_norm" \
    "$deposit_amount" \
    "$gas_limit" \
    false \
    0x; then
    echo "Failed to deposit funds to $name" >&2
    exit 1
  fi

  if ! wait_for_balance "$name" "$url" "$min_balance"; then
    exit 1
  fi
}

write_helper_config() {
  local mailbox_addr=$1
  local pingpong_addr=$2
  local mytoken_addr=$3
  local bridge_addr=$4

  if [[ -z "$mailbox_addr" || -z "$pingpong_addr" || -z "$mytoken_addr" || -z "$bridge_addr" ]]; then
    log "[WARN] Missing helper contract addresses; skipping helper config generation"
    return
  fi

  local config_path="$OP_GETH_DIR/config.yml"
  cat >"$config_path" <<EOF
token: ${mytoken_addr}
rollups:
  A:
    rpc: ${ROLLUP_A_RPC_URL}
    chain_id: ${ROLLUP_A_CHAIN_ID}
    private_key: ${WALLET_PRIVATE_KEY}
    bridge: ${bridge_addr}
    contracts:
      bridge: ${bridge_addr}
      pingpong: ${pingpong_addr}
      mailbox: ${mailbox_addr}
      token: ${mytoken_addr}
  B:
    rpc: ${ROLLUP_B_RPC_URL}
    chain_id: ${ROLLUP_B_CHAIN_ID}
    private_key: ${WALLET_PRIVATE_KEY}
    bridge: ${bridge_addr}
    contracts:
      bridge: ${bridge_addr}
      pingpong: ${pingpong_addr}
      mailbox: ${mailbox_addr}
      token: ${mytoken_addr}
EOF
  log "[setup] helper config written to $config_path"

  if [[ ${ROLLUP_RESTART_WITH_MAILBOX:-1} == 1 ]]; then
    log "Restarting services with mailbox configuration"
    local -a restart_targets=(
      op-geth-a op-geth-b
      op-node-a op-node-b
      blockscout-a blockscout-b
      blockscout-a-frontend blockscout-b-frontend
      blockscout-a-proxy blockscout-b-proxy
    )
    (
      cd "$ROOT_DIR"
      ROLLUP_A_MAILBOX_ADDR="$mailbox_addr" \
      ROLLUP_B_MAILBOX_ADDR="$mailbox_addr" \
      docker compose up -d "${restart_targets[@]}" >/dev/null
    )
  fi
}

ensure_placeholder_contracts_json() {
  local dir=$1
  local chain_id=$2
  local file="$dir/contracts.json"

  if [[ -s "$file" ]]; then
    return
  fi

  mkdir -p "$dir"
  cat >"$file" <<EOF
{
  "chainInfo": {
    "chainId": ${chain_id}
  },
  "addresses": {
    "Mailbox": "0x0000000000000000000000000000000000000000",
    "PingPong": "0x0000000000000000000000000000000000000000",
    "MyToken": "0x0000000000000000000000000000000000000000",
    "Bridge": "0x0000000000000000000000000000000000000000"
  }
}
EOF
  log "Seeded placeholder contracts.json at $file"
}

rpc_ready() {
  local url=$1
  if ! command -v curl >/dev/null 2>&1; then
    echo "curl is required to deploy contracts" >&2
    exit 1
  fi
  local attempts=${RPC_WAIT_ATTEMPTS:-100}
  local delay=${RPC_WAIT_DELAY:-3}
  local i=0
  while (( i < attempts )); do
    if curl -fsS --max-time 5 \
      -H 'Content-Type: application/json' \
      -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
      "$url" >/dev/null; then
      return 0
    fi
    ((i++))
    sleep "$delay"
  done
  return 1
}

deploy_contracts() {
  if [[ $DEPLOY_CONTRACTS != 1 ]]; then
    log "Skipping contract deployment (DEPLOY_CONTRACTS=$DEPLOY_CONTRACTS)"
    return
  fi

  if [[ ! -d "$CONTRACTS_DIR" ]]; then
    log "[ERROR] Contracts path $CONTRACTS_DIR not found"
    log "        Ensure copy_contract_bundle completed or set CONTRACTS_SOURCE to a valid Foundry project. Export DEPLOY_CONTRACTS=0 to skip helper deployment."
    exit 1
  fi

  local contracts_dir
  contracts_dir=$(cd "$CONTRACTS_DIR" && pwd)

  local rpc_a_container="$ROLLUP_A_RPC_URL"
  local rpc_b_container="$ROLLUP_B_RPC_URL"
  local -a foundry_network_args=()
  if docker network inspect "$FOUNDRY_NETWORK" >/dev/null 2>&1; then
    foundry_network_args=(--network "$FOUNDRY_NETWORK")
    rpc_a_container=${ROLLUP_A_RPC_URL_CONTAINER:-$ROLLUP_A_RPC_URL}
    rpc_b_container=${ROLLUP_B_RPC_URL_CONTAINER:-$ROLLUP_B_RPC_URL}
  else
    log "[WARN] Docker network $FOUNDRY_NETWORK not found; falling back to host networking"
  fi

  local -a docker_common=(
    docker run --rm $DOCKER_USER_FLAG
    -v "$contracts_dir:/contracts"
    -w /contracts
  )
  if ((${#foundry_network_args[@]})); then
    docker_common+=("${foundry_network_args[@]}")
  fi
  docker_common+=(
    -e ROLLUP_A_CHAIN_ID="${ROLLUP_A_CHAIN_ID}"
    -e ROLLUP_B_CHAIN_ID="${ROLLUP_B_CHAIN_ID}"
    -e ROLLUP_A_RPC_URL="${rpc_a_container}"
    -e ROLLUP_B_RPC_URL="${rpc_b_container}"
    -e DEPLOYER_ADDRESS="$wallet_address_norm"
    -e DEPLOYER_PRIVATE_KEY="$forge_private_key_norm"
    -e HOME="$FOUNDRY_HOME_DIR"
    -e SVM_HOME="$FOUNDRY_HOME_DIR/.svm"
    -e FOUNDRY_DIR="$FOUNDRY_HOME_DIR/.foundry"
    --entrypoint forge
    "$FOUNDRY_IMAGE"
    script
  )

  run_forge_script() {
    local label=$1
    local script_path=$2
    local rpc_url=$3
    shift 3
    local max_attempts=${CONTRACT_DEPLOY_MAX_ATTEMPTS:-5}
    local retry_delay=${CONTRACT_DEPLOY_RETRY_DELAY_SECONDS:-5}
    local attempt=1
    local use_resume=0
    while (( attempt <= max_attempts )); do
      local log_file
      log_file=$(mktemp)
      set +e
      if (( use_resume )); then
        "${docker_common[@]}" "$script_path" --rpc-url "$rpc_url" --private-key "$forge_private_key_norm" --broadcast --force -vvv --resume "$@" 2>&1 | tee "$log_file"
      else
        "${docker_common[@]}" "$script_path" --rpc-url "$rpc_url" --private-key "$forge_private_key_norm" --broadcast --force -vvv "$@" 2>&1 | tee "$log_file"
      fi
      local status=${PIPESTATUS[0]}
      set -e
      if [[ $status -eq 0 ]]; then
        rm -f "$log_file"
        return
      fi
      if ! grep -q "transaction indexing is in progress" "$log_file"; then
        cat "$log_file" >&2
        rm -f "$log_file"
        exit $status
      fi
      rm -f "$log_file"
      if (( attempt == max_attempts )); then
        log "[ERROR] $label helper deployment failed after $max_attempts attempts"
        exit 1
      fi
      use_resume=1
      local next_attempt=$((attempt + 1))
      log "[$label] Transaction indexer not ready (retrying in ${retry_delay}s, attempt ${next_attempt}/${max_attempts})"
      sleep "$retry_delay"
      (( attempt++ ))
    done
    log "[ERROR] $label helper deployment failed"
    exit 1
  }

  if [[ ! -d "$contracts_dir/lib/forge-std/src" ]]; then
    log "Installing forge-std"
    rm -rf "$contracts_dir/lib/forge-std"
    docker run --rm $DOCKER_USER_FLAG \
      -v "$contracts_dir:/contracts" \
      -w /contracts \
      "${foundry_network_args[@]}" \
      -e HOME="$FOUNDRY_HOME_DIR" \
      -e SVM_HOME="$FOUNDRY_HOME_DIR/.svm" \
      -e FOUNDRY_DIR="$FOUNDRY_HOME_DIR/.foundry" \
      --entrypoint forge \
      "$FOUNDRY_IMAGE" \
      install --no-git foundry-rs/forge-std
  fi

  if [[ ! -d "$contracts_dir/lib/openzeppelin-contracts/contracts" ]]; then
    log "Installing openzeppelin-contracts"
    rm -rf "$contracts_dir/lib/openzeppelin-contracts"
    docker run --rm $DOCKER_USER_FLAG \
      -v "$contracts_dir:/contracts" \
      -w /contracts \
      "${foundry_network_args[@]}" \
      -e HOME="$FOUNDRY_HOME_DIR" \
      -e SVM_HOME="$FOUNDRY_HOME_DIR/.svm" \
      -e FOUNDRY_DIR="$FOUNDRY_HOME_DIR/.foundry" \
      --entrypoint forge \
      "$FOUNDRY_IMAGE" \
      install --no-git OpenZeppelin/openzeppelin-contracts
  fi

  if [[ ! -d "$contracts_dir/lib/openzeppelin-contracts-upgradeable/contracts" ]]; then
    log "Installing openzeppelin-contracts-upgradeable"
    rm -rf "$contracts_dir/lib/openzeppelin-contracts-upgradeable"
    docker run --rm $DOCKER_USER_FLAG \
      -v "$contracts_dir:/contracts" \
      -w /contracts \
      "${foundry_network_args[@]}" \
      -e HOME="$FOUNDRY_HOME_DIR" \
      -e SVM_HOME="$FOUNDRY_HOME_DIR/.svm" \
      -e FOUNDRY_DIR="$FOUNDRY_HOME_DIR/.foundry" \
      --entrypoint forge \
      "$FOUNDRY_IMAGE" \
      install --no-git OpenZeppelin/openzeppelin-contracts-upgradeable
  fi

  log "Checking Rollup RPC availability"
  if ! rpc_ready "$ROLLUP_A_RPC_URL"; then
    log "[ERROR] Rollup A RPC ($ROLLUP_A_RPC_URL) did not become reachable"
    log "        Check docker compose logs or retry after the stack finishes booting."
    exit 1
  fi
  if ! rpc_ready "$ROLLUP_B_RPC_URL"; then
    log "[ERROR] Rollup B RPC ($ROLLUP_B_RPC_URL) did not become reachable"
    log "        Check docker compose logs or retry after the stack finishes booting."
    exit 1
  fi

  local settle_delay=${CONTRACT_DEPLOY_DELAY_SECONDS:-10}
  if (( settle_delay > 0 )); then
    log "Waiting $settle_delay seconds for rollup services to finish indexing"
    sleep "$settle_delay"
  fi

  fund_rollup_account "Rollup A" "$ROLLUP_A_RPC_URL" "$ROLLUP_A_DIR/addresses.json"
  fund_rollup_account "Rollup B" "$ROLLUP_B_RPC_URL" "$ROLLUP_B_DIR/addresses.json"

  log "Building contracts in $contracts_dir"
  docker run --rm $DOCKER_USER_FLAG \
    -v "$contracts_dir:/contracts" \
    -w /contracts \
    "${foundry_network_args[@]}" \
    -e HOME="$FOUNDRY_HOME_DIR" \
    -e SVM_HOME="$FOUNDRY_HOME_DIR/.svm" \
    -e FOUNDRY_DIR="$FOUNDRY_HOME_DIR/.foundry" \
    --entrypoint forge \
    "$FOUNDRY_IMAGE" \
    build

  log "Deploying contracts to Rollup A"
  run_forge_script "Rollup A" "script/DeployRollupA.s.sol" "$rpc_a_container"

  log "Deploying contracts to Rollup B"
  run_forge_script "Rollup B" "script/DeployRollupB.s.sol" "$rpc_b_container"

  local artifact_a="$contracts_dir/artifacts/deploy-rollup-a.json"
  if [[ -f "$artifact_a" ]]; then
    cp "$artifact_a" "$ROLLUP_A_DIR/contracts.json"
    log "Saved Rollup A contract addresses to $ROLLUP_A_DIR/contracts.json"
  fi

  local artifact_b="$contracts_dir/artifacts/deploy-rollup-b.json"
  if [[ -f "$artifact_b" ]]; then
    cp "$artifact_b" "$ROLLUP_B_DIR/contracts.json"
    log "Saved Rollup B contract addresses to $ROLLUP_B_DIR/contracts.json"
  fi

  if [[ -f "$ROLLUP_A_DIR/contracts.json" && -f "$ROLLUP_B_DIR/contracts.json" ]]; then
    helper_output=$(python3 - <<'PYCOMPARE'
import json
import os
import sys

def load_addresses(path):
    with open(path) as f:
        data = json.load(f)
    parent = data.get('parent', data)
    addresses = parent.get('addresses', {})
    lowered = {k: v.lower() for k, v in addresses.items()}
    return addresses, lowered

a_raw, a = load_addresses(os.path.join(os.environ['ROLLUP_A_DIR'], 'contracts.json'))
b_raw, b = load_addresses(os.path.join(os.environ['ROLLUP_B_DIR'], 'contracts.json'))

required_keys = {'Mailbox', 'PingPong', 'MyToken', 'Bridge'}
missing = [k for k in required_keys if k not in a or k not in b]
if missing:
    print(f"Missing expected addresses in artifacts: {missing}", file=sys.stderr)
    sys.exit(1)

diff = {k: (a[k], b[k]) for k in required_keys if a[k] != b[k]}
if diff:
    print("Compose helper contracts mismatch between rollups:", file=sys.stderr)
    for k, (addr_a, addr_b) in diff.items():
        print(f"  {k}: rollup-a={addr_a}, rollup-b={addr_b}", file=sys.stderr)
    sys.exit(1)

print("Helper contracts deployed with matching addresses:")
for k in sorted(required_keys):
    print(f"  {k}: {a_raw[k]}")

for key in sorted(required_keys):
    print(f"SET_ENV {key} {a_raw[key]}")
PYCOMPARE
    )
    printf "%s\n" "$helper_output"

    local mailbox_addr="" pingpong_addr="" mytoken_addr="" bridge_addr=""
    while IFS= read -r line; do
      case "$line" in
        SET_ENV\ Mailbox\ *) mailbox_addr=${line#SET_ENV Mailbox } ;;
        SET_ENV\ PingPong\ *) pingpong_addr=${line#SET_ENV PingPong } ;;
        SET_ENV\ MyToken\ *) mytoken_addr=${line#SET_ENV MyToken } ;;
        SET_ENV\ Bridge\ *) bridge_addr=${line#SET_ENV Bridge } ;;
      esac
    done <<< "$helper_output"

    write_helper_config "$mailbox_addr" "$pingpong_addr" "$mytoken_addr" "$bridge_addr"
  fi

  log "Contract deployment finished"
}

stop_compose_stack
reset_workspace

clone_repo_if_missing "$OPTIMISM_REPO" "$OPTIMISM_DIR" "$OPTIMISM_REF"

if [[ ${OP_GETH_SKIP_SYNC:-0} == 1 ]]; then
  log "Skipping op-geth sync (OP_GETH_SKIP_SYNC=1)"
  if [[ ! -d "$OP_GETH_DIR" ]]; then
    log "[ERROR] OP_GETH_PATH ($OP_GETH_DIR) not found"
    exit 1
  fi
else
  clone_repo_if_missing "$OP_GETH_REPO" "$OP_GETH_DIR" "$OP_GETH_BRANCH"
fi

copy_rollup_shared_publisher
copy_contract_bundle

if ! OP_GETH_DIR=$(cd "$OP_GETH_DIR" 2>/dev/null && pwd); then
  log "[ERROR] Unable to access op-geth directory at $OP_GETH_DIR"
  exit 1
fi

if ! ROLLUP_SP_DIR=$(cd "$ROLLUP_SP_DIR" 2>/dev/null && pwd); then
  log "[ERROR] rollup-shared-publisher path $ROLLUP_SP_DIR is not accessible"
  exit 1
fi

mkdir -p "$STATE_DIR" "$ROLLUP_A_DIR" "$ROLLUP_B_DIR"
mkdir -p "$STATE_DIR/.cache"

ensure_placeholder_contracts_json "$ROLLUP_A_DIR" "$ROLLUP_A_CHAIN_ID"
ensure_placeholder_contracts_json "$ROLLUP_B_DIR" "$ROLLUP_B_CHAIN_ID"

if ! docker image inspect "$OP_DEPLOYER_IMAGE" >/dev/null 2>&1; then
  log "Building op-deployer image ($OP_DEPLOYER_IMAGE)"
  docker build -t "$OP_DEPLOYER_IMAGE" -f "$ROOT_DIR/docker/op-deployer.Dockerfile" "$ROOT_DIR"
fi

if [[ ! -f "$STATE_DIR/state.json" ]]; then
  log "Initializing op-deployer state"
  docker run --rm $DOCKER_USER_FLAG \
    -v "$STATE_DIR:/work" \
    -w /work \
    -e HOME=/work \
    -e DEPLOYER_CACHE_DIR=/work/.cache \
    "$OP_DEPLOYER_IMAGE" \
    init --intent-type custom --l1-chain-id "$HOODI_CHAIN_ID" --l2-chain-ids "$ROLLUP_A_CHAIN_ID,$ROLLUP_B_CHAIN_ID"
fi

chain_id_to_hash() {
  local id=$1
  printf "0x%064x" "$id"
}

ROLLUP_A_ID_HEX=$(chain_id_to_hash "$ROLLUP_A_CHAIN_ID")
ROLLUP_B_ID_HEX=$(chain_id_to_hash "$ROLLUP_B_CHAIN_ID")
export ROLLUP_A_ID_HEX ROLLUP_B_ID_HEX STATE_DIR ROLLUP_A_DIR ROLLUP_B_DIR

wallet_address_norm=${WALLET_ADDRESS,,}
if [[ $wallet_address_norm != 0x* ]]; then
  wallet_address_norm="0x${wallet_address_norm}"
fi
forge_private_key_norm=${WALLET_PRIVATE_KEY,,}
if [[ $forge_private_key_norm != 0x* ]]; then
  forge_private_key_norm="0x${forge_private_key_norm}"
fi
export WALLET_ADDRESS_NORM=$wallet_address_norm

cat > "$STATE_DIR/intent.toml" <<EOF
configType = "custom"
l1ChainID = $HOODI_CHAIN_ID
fundDevAccounts = false
l1ContractsLocator = "$L1_CONTRACTS_TAG"
l2ContractsLocator = "$L2_CONTRACTS_TAG"

[superchainRoles]
  proxyAdminOwner = "$wallet_address_norm"
  protocolVersionsOwner = "$wallet_address_norm"
  guardian = "$wallet_address_norm"

[[chains]]
  id = "$ROLLUP_A_ID_HEX"
  baseFeeVaultRecipient = "$wallet_address_norm"
  l1FeeVaultRecipient = "$wallet_address_norm"
  sequencerFeeVaultRecipient = "$wallet_address_norm"
  eip1559DenominatorCanyon = 250
  eip1559Denominator = 50
  eip1559Elasticity = 6
  gasLimit = 60000000
  operatorFeeScalar = 0
  operatorFeeConstant = 0
  minBaseFee = 0
  [chains.roles]
    l1ProxyAdminOwner = "$wallet_address_norm"
    l2ProxyAdminOwner = "$wallet_address_norm"
    systemConfigOwner = "$wallet_address_norm"
    unsafeBlockSigner = "$wallet_address_norm"
    batcher = "$wallet_address_norm"
    proposer = "$wallet_address_norm"
    challenger = "$wallet_address_norm"

[[chains]]
  id = "$ROLLUP_B_ID_HEX"
  baseFeeVaultRecipient = "$wallet_address_norm"
  l1FeeVaultRecipient = "$wallet_address_norm"
  sequencerFeeVaultRecipient = "$wallet_address_norm"
  eip1559DenominatorCanyon = 250
  eip1559Denominator = 50
  eip1559Elasticity = 6
  gasLimit = 60000000
  operatorFeeScalar = 0
  operatorFeeConstant = 0
  minBaseFee = 0
  [chains.roles]
    l1ProxyAdminOwner = "$wallet_address_norm"
    l2ProxyAdminOwner = "$wallet_address_norm"
    systemConfigOwner = "$wallet_address_norm"
    unsafeBlockSigner = "$wallet_address_norm"
    batcher = "$wallet_address_norm"
    proposer = "$wallet_address_norm"
    challenger = "$wallet_address_norm"
EOF

log "Running op-deployer apply"
docker run --rm $DOCKER_USER_FLAG \
  -v "${STATE_DIR}:/work" \
  -w /work \
  -e HOME=/work \
  -e L1_RPC_URL="${HOODI_EL_RPC}" \
  -e DEPLOYER_PRIVATE_KEY="${WALLET_PRIVATE_KEY}" \
  -e DEPLOYER_CACHE_DIR=/work/.cache \
  "${OP_DEPLOYER_IMAGE}" \
  apply --deployment-target="${DEPLOYMENT_TARGET}"

python3 - <<'PYGENESIS'
import base64
import gzip
import json
import os
import sys

state_path = os.path.join(os.environ['STATE_DIR'], 'state.json')
addr = os.environ.get('WALLET_ADDRESS_NORM', '').lower()
amount_raw = os.environ.get('ROLLUP_ACCOUNT_GENESIS_BALANCE_WEI', '0')

try:
    amount = int(amount_raw, 0)
except ValueError:
    print(f"Invalid ROLLUP_ACCOUNT_GENESIS_BALANCE_WEI: {amount_raw}", file=sys.stderr)
    sys.exit(1)

if amount > 0 and addr:
    with open(state_path) as f:
        state = json.load(f)

    allocs_hex = hex(amount)
    updated = False

    for deployment in state.get('opChainDeployments', []):
        raw = base64.b64decode(deployment['allocs'])
        allocs = json.loads(gzip.decompress(raw))
        if allocs.get(addr) != {'balance': allocs_hex}:
            allocs[addr] = {'balance': allocs_hex}
            deployment['allocs'] = base64.b64encode(
                gzip.compress(json.dumps(allocs, separators=(',', ':')).encode('utf-8'))
            ).decode('utf-8')
            updated = True

    if updated:
        with open(state_path, 'w') as f:
            json.dump(state, f, indent=2)
PYGENESIS

log "Exporting chain artifacts"
python3 - <<'PYADDR'
import json
import os
from pathlib import Path

state_path = Path(os.environ['STATE_DIR']) / 'state.json'
try:
    state = json.loads(state_path.read_text())
except FileNotFoundError:
    state = {}
chains = {}
for chain in state.get('opChainDeployments') or []:
    chains[chain['id'].lower()] = chain

rollups = {
    os.environ['ROLLUP_A_ID_HEX'].lower(): Path(os.environ['ROLLUP_A_DIR']),
    os.environ['ROLLUP_B_ID_HEX'].lower(): Path(os.environ['ROLLUP_B_DIR']),
}
for chain_id_hex, target_dir in rollups.items():
    chain = chains.get(chain_id_hex)
    if not chain:
        continue
    interesting_keys = {
        'L2OutputOracleProxyAddress': 'L2_OUTPUT_ORACLE',
        'systemConfigProxyAddress': 'SYSTEM_CONFIG',
        'SystemConfigProxyAddress': 'SYSTEM_CONFIG',
        'optimismPortalProxyAddress': 'OPTIMISM_PORTAL',
        'l1StandardBridgeProxyAddress': 'L1_STANDARD_BRIDGE',
        'disputeGameFactoryProxyAddress': 'DISPUTE_GAME_FACTORY',
    }

    addresses = {}
    for key, label in interesting_keys.items():
        if key in chain and chain[key]:
            addresses[label] = chain[key]

    if addresses:
        target_dir.mkdir(parents=True, exist_ok=True)
        (target_dir / 'addresses.json').write_text(json.dumps(addresses, indent=2))
        env_lines = []
        if addresses.get('L2_OUTPUT_ORACLE'):
            env_lines.append(f"L2OO_ADDRESS={addresses['L2_OUTPUT_ORACLE']}")
            env_lines.append(f"OP_PROPOSER_L2OO_ADDRESS={addresses['L2_OUTPUT_ORACLE']}")
        if addresses.get('DISPUTE_GAME_FACTORY'):
            env_lines.append(f"DISPUTE_GAME_FACTORY_ADDRESS={addresses['DISPUTE_GAME_FACTORY']}")
            env_lines.append(f"OP_PROPOSER_GAME_FACTORY_ADDRESS={addresses['DISPUTE_GAME_FACTORY']}")
        if addresses.get('SYSTEM_CONFIG'):
            env_lines.append(f"SYSTEM_CONFIG_PROXY={addresses['SYSTEM_CONFIG']}")
        if env_lines:
            (target_dir / 'runtime.env').write_text('\n'.join(env_lines) + '\n')
PYADDR
for CHAIN_ID in "${ROLLUP_A_CHAIN_ID}" "${ROLLUP_B_CHAIN_ID}"; do
  TARGET_DIR="${ROLLUP_A_DIR}"
  if [[ "${CHAIN_ID}" == "${ROLLUP_B_CHAIN_ID}" ]]; then
    TARGET_DIR="${ROLLUP_B_DIR}"
  fi

  docker run --rm $DOCKER_USER_FLAG \
    -v "${STATE_DIR}:/work" \
    -w /work \
    -e HOME=/work \
    -e DEPLOYER_CACHE_DIR=/work/.cache \
    "${OP_DEPLOYER_IMAGE}" \
    inspect genesis "${CHAIN_ID}" > "${TARGET_DIR}/genesis.json"

  python3 - <<'PYGENFILE' "${TARGET_DIR}/genesis.json"
import json
import os
import sys

path = sys.argv[1]
addr = os.environ.get('WALLET_ADDRESS_NORM', '').lower()
amount_raw = os.environ.get('ROLLUP_ACCOUNT_GENESIS_BALANCE_WEI', '0')

try:
    amount = int(amount_raw, 0)
except ValueError:
    print(f"Invalid ROLLUP_ACCOUNT_GENESIS_BALANCE_WEI: {amount_raw}", file=sys.stderr)
    sys.exit(1)

if amount <= 0 or not addr:
    sys.exit(0)

with open(path) as f:
    genesis = json.load(f)

alloc = genesis.setdefault('alloc', {})
entry = alloc.setdefault(addr.lower(), {})
entry['balance'] = hex(amount)

def parse_timestamp(env_name, fallback):
    raw = os.environ.get(env_name, str(fallback))
    try:
        return int(raw, 0)
    except ValueError as exc:
        print(f"Invalid {env_name}: {raw}", file=sys.stderr)
        raise exc

prague_ts = parse_timestamp('ROLLUP_PRAGUE_TIMESTAMP', 0)
isthmus_ts = parse_timestamp('ROLLUP_ISTHMUS_TIMESTAMP', prague_ts)

config = genesis.setdefault('config', {})
config['pragueTime'] = prague_ts
config['isthmusTime'] = isthmus_ts

with open(path, 'w') as f:
    json.dump(genesis, f, indent=2)
PYGENFILE

  genesis_rel_path=$(python3 -c 'import os, sys; print(os.path.relpath(sys.argv[1], sys.argv[2]))' "$TARGET_DIR" "$ROOT_DIR")
  mkdir -p "${GENESIS_HASH_CACHE_DIR}/mod" "${GENESIS_HASH_CACHE_DIR}/build"
  genesis_hash=$(docker run --rm \
    -v "${ROOT_DIR}:/workspace" \
    -v "${OP_GETH_DIR}:/op-geth" \
    -v "${GENESIS_HASH_CACHE_DIR}/mod:/go/pkg/mod" \
    -v "${GENESIS_HASH_CACHE_DIR}/build:/root/.cache/go-build" \
    -w /workspace/scripts/genesis_hash \
    -e HOME=/tmp/home \
    -e GOMODCACHE=/go/pkg/mod \
    -e GOCACHE=/root/.cache/go-build \
    golang:1.24-alpine \
    sh -c "set -e; apk add --no-cache git >/dev/null; mkdir -p /tmp/home; go run . /workspace/${genesis_rel_path}/genesis.json")

  docker run --rm $DOCKER_USER_FLAG \
    -v "${STATE_DIR}:/work" \
    -w /work \
    -e HOME=/work \
    -e DEPLOYER_CACHE_DIR=/work/.cache \
    "${OP_DEPLOYER_IMAGE}" \
    inspect rollup "${CHAIN_ID}" > "${TARGET_DIR}/rollup.json"

  GENESIS_HASH="$genesis_hash" python3 - <<'PYROLLUP' "${TARGET_DIR}/rollup.json"
import json
import os
import sys

path = sys.argv[1]

def parse_timestamp(env_name, fallback):
    raw = os.environ.get(env_name, str(fallback))
    try:
        return int(raw, 0)
    except ValueError as exc:
        print(f"Invalid {env_name}: {raw}", file=sys.stderr)
        raise exc

prague_ts = parse_timestamp('ROLLUP_PRAGUE_TIMESTAMP', 0)
isthmus_ts = parse_timestamp('ROLLUP_ISTHMUS_TIMESTAMP', prague_ts)

with open(path) as f:
    rollup = json.load(f)

rollup['isthmus_time'] = isthmus_ts
genesis = rollup.setdefault('genesis', {})
l2 = genesis.setdefault('l2', {})
hash_from_env = os.environ.get('GENESIS_HASH')
if hash_from_env:
    l2['hash'] = hash_from_env

with open(path, 'w') as f:
    json.dump(rollup, f, indent=2)
PYROLLUP

  if [[ ! -f "${TARGET_DIR}/jwt.txt" ]]; then
    python3 - <<'PYJWT' > "${TARGET_DIR}/jwt.txt"
import secrets
print(secrets.token_hex(32))
PYJWT
  fi

  if [[ ! -f "${TARGET_DIR}/password.txt" ]]; then
    printf '
' > "${TARGET_DIR}/password.txt"
  fi

done

ROOT_DIR="$ROOT_DIR" \
ROLLUP_A_RPC_URL_CONTAINER="$ROLLUP_A_RPC_URL_CONTAINER" \
ROLLUP_B_RPC_URL_CONTAINER="$ROLLUP_B_RPC_URL_CONTAINER" \
ROLLUP_A_WS_URL_CONTAINER="${ROLLUP_A_WS_URL_CONTAINER:-}" \
ROLLUP_B_WS_URL_CONTAINER="${ROLLUP_B_WS_URL_CONTAINER:-}" \
HOODI_EL_RPC="$HOODI_EL_RPC" \
python3 - <<'PYBLOCK'
import json
import os
import secrets
from pathlib import Path
from urllib.parse import urlparse, urlunparse

root = Path(os.environ["ROOT_DIR"])

def load_json(path):
    try:
        return json.loads(Path(path).read_text())
    except FileNotFoundError:
        return {}
    except json.JSONDecodeError:
        return {}

def compute_ws(http_url):
    if not http_url:
        return ""
    parsed = urlparse(http_url)
    if not parsed.scheme or not parsed.hostname:
        return ""
    scheme = "ws" if parsed.scheme == "http" else ("wss" if parsed.scheme == "https" else parsed.scheme)
    if parsed.port in (None, 80, 443, 8545):
        ws_port = 8546
    else:
        ws_port = parsed.port
    netloc = parsed.hostname if ws_port in (None, 80, 443) else f"{parsed.hostname}:{ws_port}"
    return urlunparse(parsed._replace(scheme=scheme, netloc=netloc))

chains = [
    {
        "key": "rollup-a",
        "label": "Rollup A",
        "suffix": "a",
        "rpc_http": os.environ.get("ROLLUP_A_RPC_URL_CONTAINER", "http://op-geth-a:8545"),
        "rpc_ws_env": "ROLLUP_A_WS_URL_CONTAINER",
    },
    {
        "key": "rollup-b",
        "label": "Rollup B",
        "suffix": "b",
        "rpc_http": os.environ.get("ROLLUP_B_RPC_URL_CONTAINER", "http://op-geth-b:8545"),
        "rpc_ws_env": "ROLLUP_B_WS_URL_CONTAINER",
    },
]

l1_rpc = os.environ.get("HOODI_EL_RPC", "")

for chain in chains:
    base_dir = root / "networks" / chain["key"]
    rollup_path = base_dir / "rollup.json"
    if not rollup_path.exists():
        continue

    addresses_path = base_dir / "addresses.json"
    addresses = load_json(addresses_path)
    rollup = load_json(rollup_path)

    chain_id = rollup.get("l2_chain_id")
    if chain_id is None:
        continue

    rpc_http = chain["rpc_http"]
    ws_override = os.environ.get(chain.get("rpc_ws_env", ""))
    rpc_ws = ws_override or compute_ws(rpc_http)

    system_config = addresses.get("SYSTEM_CONFIG") or rollup.get("l1_system_config_address")
    output_oracle = addresses.get("L2_OUTPUT_ORACLE") or ""

    env_path = base_dir / "blockscout.env"
    existing_values = {}
    if env_path.exists():
        for raw_line in env_path.read_text().splitlines():
            line = raw_line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.split("=", 1)
            existing_values[key.strip()] = value.strip()

    secret_key = existing_values.get("SECRET_KEY_BASE") or secrets.token_hex(32)

    env_lines = [
        "# Autogenerated by scripts/setup.sh",
        f"CHAIN_ID={chain_id}",
        f"CHAIN_NAME={chain['label']} Compose",
        f"NETWORK={chain['label']}",
        "SUBNETWORK=Compose Rollups",
        "CHAIN_TYPE=optimism",
        "ETHEREUM_JSONRPC_VARIANT=geth",
        "ETHEREUM_JSONRPC_TRANSPORT=http",
        f"ETHEREUM_JSONRPC_HTTP_URL={rpc_http}",
        f"ETHEREUM_JSONRPC_TRACE_URL={rpc_http}",
        f"ETHEREUM_JSONRPC_WS_URL={rpc_ws}",
        f"DATABASE_URL=postgresql://blockscout:blockscout@blockscout-{chain['suffix']}-db:5432/blockscout",
        f"REDIS_URL=redis://blockscout-{chain['suffix']}-redis:6379/0",
        f"SECRET_KEY_BASE={secret_key}",
        "PORT=4000",
        "POOL_SIZE=40",
        "POOL_SIZE_API=10",
        "ECTO_USE_SSL=false",
        "COIN=ETH",
        "COIN_NAME=Ether",
        "API_V1_READ_METHODS_DISABLED=false",
        "API_V1_WRITE_METHODS_DISABLED=false",
        "API_RATE_LIMIT_DISABLED=true",
        "ADMIN_PANEL_ENABLED=false",
        "DISABLE_WEBAPP=false",
        "INDEXER_OPTIMISM_L1_DEPOSITS_BATCH_SIZE=500",
        "INDEXER_OPTIMISM_L1_DEPOSITS_TRANSACTION_TYPE=126",
        "INDEXER_OPTIMISM_L1_BATCH_BLOCKS_CHUNK_SIZE=4",
        "INDEXER_OPTIMISM_L2_BATCH_GENESIS_BLOCK_NUMBER=0",
        "INDEXER_OPTIMISM_L2_WITHDRAWALS_START_BLOCK=1",
        "INDEXER_OPTIMISM_L2_MESSAGE_PASSER_CONTRACT=0x4200000000000000000000000000000000000016",
    ]

    if l1_rpc:
        env_lines.append(f"INDEXER_OPTIMISM_L1_RPC={l1_rpc}")
    if system_config:
        env_lines.append(f"INDEXER_OPTIMISM_L1_SYSTEM_CONFIG_CONTRACT={system_config}")
    if output_oracle:
        env_lines.append(f"INDEXER_OPTIMISM_L1_OUTPUT_ORACLE_CONTRACT={output_oracle}")

    env_path.write_text("\n".join(env_lines) + "\n")
    try:
        rel_path = env_path.relative_to(root)
    except ValueError:
        rel_path = env_path
    print(f"[setup] blockscout env updated: {rel_path}")

    frontend_env_path = base_dir / "blockscout-frontend.env"
    app_port = "19000" if chain["suffix"] == "a" else "29000"
    app_host = f"localhost:{app_port}"
    api_host = app_host
    frontend_lines = [
        "# Autogenerated by scripts/setup.sh",
        f"NEXT_PUBLIC_API_PROTOCOL=http",
        f"NEXT_PUBLIC_API_HOST={api_host}",
        "NEXT_PUBLIC_API_BASE_PATH=",
        "NEXT_PUBLIC_API_WEBSOCKET_PROTOCOL=ws",
        f"NEXT_PUBLIC_NETWORK_NAME={chain['label']} Compose",
        f"NEXT_PUBLIC_NETWORK_SHORT_NAME={chain['label']}",
        f"NEXT_PUBLIC_NETWORK_ID={chain_id}",
        "NEXT_PUBLIC_NETWORK_CURRENCY_NAME=Ether",
        "NEXT_PUBLIC_NETWORK_CURRENCY_SYMBOL=ETH",
        "NEXT_PUBLIC_NETWORK_CURRENCY_DECIMALS=18",
        f"NEXT_PUBLIC_APP_PROTOCOL=http",
        f"NEXT_PUBLIC_APP_HOST={app_host}",
        "NEXT_PUBLIC_IS_TESTNET=true",
        "NEXT_PUBLIC_HOMEPAGE_CHARTS=['daily_txs']",
        "NEXT_PUBLIC_API_SPEC_URL=https://raw.githubusercontent.com/blockscout/blockscout-api-v2-swagger/main/swagger.yaml",
    ]
    frontend_env_path.write_text("\n".join(frontend_lines) + "\n")
    try:
        rel_front = frontend_env_path.relative_to(root)
    except ValueError:
        rel_front = frontend_env_path
    print(f"[setup] blockscout frontend env updated: {rel_front}")

    nginx_conf_path = base_dir / "blockscout-nginx.conf"
    backend_host = f"blockscout-{chain['suffix']}:4000"
    frontend_host = f"blockscout-{chain['suffix']}-frontend:3000"
    nginx_conf = f"""
map $http_upgrade $connection_upgrade {{
  default upgrade;
  ''      close;
}}

server {{
  listen 80;
  listen [::]:80;
  server_name _;
  proxy_http_version 1.1;

  location ~ ^/(api(?!-docs$)|socket|sitemap.xml|auth/auth0|auth/auth0/callback|auth/logout) {{
    proxy_pass http://{backend_host};
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_cache_bypass $http_upgrade;
  }}

  location / {{
    proxy_pass http://{frontend_host};
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_cache_bypass $http_upgrade;
  }}
}}
"""
    nginx_conf_path.write_text(nginx_conf.strip() + "\n")
    try:
        rel_nginx = nginx_conf_path.relative_to(root)
    except ValueError:
        rel_nginx = nginx_conf_path
    print(f"[setup] blockscout proxy config updated: {rel_nginx}")
PYBLOCK

start_compose_stack

deploy_contracts

log "Setup complete"
log "Rollup A RPC: $ROLLUP_A_RPC_URL"
log "Rollup B RPC: $ROLLUP_B_RPC_URL"
log "Shared publisher health: http://localhost:18081/health"
