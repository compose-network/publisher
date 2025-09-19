#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
STATUS_TIMEOUT=${STATUS_TIMEOUT:-2}
RPC_RETRIES=${RPC_RETRIES:-240}
RPC_DELAY=${RPC_DELAY:-1}
PUBLISHER_HEALTH_TIMEOUT=${PUBLISHER_HEALTH_TIMEOUT:-2}
PUBLISHER_RETRIES=${PUBLISHER_RETRIES:-240}
TABLE_DELIM=$'\x1f'
declare -A CONTRACTS=()

log() {
  printf '[local.sh] %s\n' "$*"
}

err() {
  printf '[local.sh] %s\n' "$*" >&2
}

usage() {
  cat <<'USAGE'
Usage: ./local.sh <command> [options]

Commands:
  up                    Bootstrap (runs setup on first use) or start the stack.
  down                  Stop containers without removing volumes.
  status                Show container state, RPC endpoints, and health summaries.
  logs [service ...]    Stream logs for services (aliases: op-geth, publisher, all, or compose names).
  restart <target>      Restart services (target: op-geth | publisher | all).
  deploy <target>       Rebuild images then restart (target: op-geth | publisher | all).
  purge [--force]       Stop everything, remove volumes, and delete generated state.
  help                  Show this message.

Environment:
  STATUS_TIMEOUT (default 2s)   RPC and health check curl timeout.
  RPC_RETRIES (default 240)     Attempts when waiting for RPC readiness (4 min with 1s delay).
  RPC_DELAY (default 1s)        Delay between RPC readiness checks.
USAGE
}

load_env() {
  local required=${1:-1}
  if [[ -f "${ROOT_DIR}/.env" ]]; then
    set -a
    # shellcheck source=/dev/null
    source "${ROOT_DIR}/.env"
    set +a
  elif [[ $required -eq 1 ]]; then
    err "Missing .env; copy .env.example and fill in the required values."
    exit 1
  fi

  if [[ -f "${ROOT_DIR}/toolkit.env" ]]; then
    set -a
    # shellcheck source=/dev/null
    source "${ROOT_DIR}/toolkit.env"
    set +a
  fi

  export ROLLUP_A_CHAIN_ID=${ROLLUP_A_CHAIN_ID:-77771}
  export ROLLUP_B_CHAIN_ID=${ROLLUP_B_CHAIN_ID:-77772}
}

compose() {
  (cd "$ROOT_DIR" && docker compose "$@")
}

remove_path() {
  local rel=$1
  local target="$ROOT_DIR/$rel"
  [[ -e $target ]] || return 0
  if rm -rf "$target" 2>/dev/null; then
    return 0
  fi
  if docker run --rm -v "$ROOT_DIR:/workspace" alpine:3 sh -c "rm -rf \"/workspace/$rel\"" >/dev/null 2>&1; then
    return 0
  fi
  err "Unable to remove $rel (permission issues); delete manually"
  return 0
}

remove_generated_artifacts() {
  remove_path "state"
  remove_path "networks"
  remove_path "contracts"
  remove_path ".cache/genesis-go"
}

is_bootstrapped() {
  [[ -f "$ROOT_DIR/networks/rollup-a/rollup.json" && -f "$ROOT_DIR/networks/rollup-b/rollup.json" ]]
}

running_services() {
  compose ps --status running --services 2>/dev/null || true
}

existing_services() {
  compose ps --all --services 2>/dev/null || true
}

repeat_char() {
  local char=$1
  local count=${2:-0}
  if (( count <= 0 )); then
    printf ''
    return
  fi
  local buf
  printf -v buf '%*s' "$count" ''
  printf '%s' "${buf// /$char}"
}

build_title_line() {
  local title=$1
  local width=$2
  local display=" $title "
  local disp_len=${#display}
  if (( disp_len > width )); then
    display=${display:0:width}
    disp_len=${#display}
  fi
  local pad=$((width - disp_len))
  (( pad < 0 )) && pad=0
  local left=$((pad / 2))
  local right=$((pad - left))
  printf '+%s%s%s+' "$(repeat_char '=' "$left")" "$display" "$(repeat_char '=' "$right")"
}

normalize_status() {
  local raw=${1:-}
  [[ -z $raw ]] && { echo '-'; return; }
  raw=${raw%% (healthy)*}
  raw=${raw%% (health:*)}
  raw=${raw%% (starting)*}
  raw=$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')
  raw=${raw//about /~}
  raw=${raw//less than a second/<1s}
  raw=${raw//seconds/s}
  raw=${raw//second/s}
  raw=${raw//minutes/m}
  raw=${raw//minute/1m}
  raw=${raw//hours/h}
  raw=${raw//hour/h}
  raw=${raw//days/d}
  raw=${raw//day/d}
  raw=${raw// ago/}
  raw=${raw//  / }
  raw=$(printf '%s\n' "$raw" | sed -E 's/([0-9]) m/\1m/g; s/([0-9]) h/\1h/g; s/([0-9]) d/\1d/g; s/([0-9]) s/\1s/g; s/ +/ /g')
  raw=${raw## }
  raw=${raw%% }
  [[ -z $raw ]] && raw='-'
  echo "$raw"
}

format_status_text() {
  local state=$1
  local status=$2
  local icon
  case $state in
    running) icon="✅" ;;
    restarting) icon="♻️" ;;
    exited|dead|removing) icon="❌" ;;
    paused) icon="⏸️" ;;
    *) icon="❔" ;;
  esac
  local normalized
  normalized=$(normalize_status "$status")
  if [[ $normalized == '-' || -z $normalized ]]; then
    if [[ -n $state && $state != 'running' ]]; then
      normalized=$state
    else
      normalized='unknown'
    fi
  fi
  printf '%s %s' "$icon" "$normalized"
}

render_panel() {
  local title=$1
  shift
  local rows=("$@")
  local delim=${TABLE_DELIM:-$'\x1f'}
  if ((${#rows[@]} == 0)); then
    local width=${#title}
    printf '%s\n' "$(build_title_line "$title" "$width")"
    printf '+%s+\n' "$(repeat_char '=' "$width")"
    return
  fi

  local -a widths
  local col_count=0
  local row
  for row in "${rows[@]}"; do
    IFS=$delim read -r -a cols <<< "$row"
    (( ${#cols[@]} > col_count )) && col_count=${#cols[@]}
    local i
    for ((i=0; i<${#cols[@]}; i++)); do
      local len=${#cols[i]}
      if [[ -z ${widths[i]:-} || ${widths[i]} -lt $len ]]; then
        widths[i]=$len
      fi
    done
  done
  local i
  for ((i=0; i<col_count; i++)); do
    widths[i]=${widths[i]:-0}
  done

  local -a row_strings=()
  for row in "${rows[@]}"; do
    IFS=$delim read -r -a cols <<< "$row"
    local line='|'
    for ((i=0; i<col_count; i++)); do
      local val=${cols[i]:-}
      local segment
      printf -v segment ' %-*s |' "${widths[i]}" "$val"
      line+="$segment"
    done
    row_strings+=("$line")
  done

  local total_width=${#row_strings[0]}
  local inner_width=$((total_width - 2))
  printf '%s\n' "$(build_title_line "$title" "$inner_width")"
  for row in "${row_strings[@]}"; do
    printf '%s\n' "$row"
  done
  printf '+%s+\n' "$(repeat_char '=' "$inner_width")"
}

get_service_entry() {
  local svc=$1
  local default_state=$2
  local entry=${STATES[$svc]:-}
  if [[ -z $entry ]]; then
    printf '%s|\n' "$default_state"
  else
    printf '%s\n' "$entry"
  fi
}

format_service_status() {
  local svc=$1
  local default_state=$2
  local entry
  entry=$(get_service_entry "$svc" "$default_state")
  local state=${entry%%|*}
  local status=${entry#*|}
  format_status_text "$state" "$status"
}

load_contracts() {
  CONTRACTS=()
  local file="$ROOT_DIR/networks/rollup-a/contracts.json"
  if [[ ! -f $file ]]; then
    file="$ROOT_DIR/networks/rollup-b/contracts.json"
  fi
  [[ ! -f $file ]] && return
  while IFS= read -r line; do
    [[ -z $line ]] && continue
    local key=${line%%=*}
    local value=${line#*=}
    CONTRACTS["$key"]=$value
  done < <(python3 - "$file" <<'PY'
import json
import sys
path = sys.argv[1]
try:
    with open(path) as f:
        data = json.load(f)
except FileNotFoundError:
    sys.exit(0)
for key, value in data.get('addresses', {}).items():
    print(f"{key}={value}")
PY
  )
}

fetch_block_number() {
  local url=$1
  local payload='{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}'
  local response
  response=$(curl --silent --show-error --fail --max-time "$STATUS_TIMEOUT" --connect-timeout "$STATUS_TIMEOUT" \
    --header 'Content-Type: application/json' --data "$payload" "$url" 2>/dev/null) || return 1
  local block
  block=$(python3 - "$response" 2>/dev/null <<'PY'
import json, sys
try:
    data = json.loads(sys.argv[1])
    result = data.get('result')
    if isinstance(result, str) and result.startswith('0x'):
        print(int(result, 16))
except Exception:
    pass
PY
)
  [[ -n ${block:-} ]] || return 1
  printf '%s\n' "$block"
}

wait_for_rpc() {
  local url=$1
  local label=$2
  local tries=$RPC_RETRIES
  local delay=$RPC_DELAY
  local block
  for ((i=1; i<=tries; i++)); do
    if block=$(fetch_block_number "$url"); then
      log "${label} ready at block ${block}"
      return 0
    fi
    sleep "$delay"
  done
  err "Timed out waiting for ${label} at ${url}"
  return 1
}

publisher_health() {
  local url=$1
  curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
    --max-time "$PUBLISHER_HEALTH_TIMEOUT" --connect-timeout "$PUBLISHER_HEALTH_TIMEOUT" "$url" 2>/dev/null || true
}

wait_for_publisher() {
  local url=$1
  for ((i=1; i<=PUBLISHER_RETRIES; i++)); do
    local code
    code=$(publisher_health "$url")
    if [[ $code == "200" ]]; then
      log "Shared publisher ready (${url})"
      return 0
    fi
    sleep "$RPC_DELAY"
  done
  err "Timed out waiting for shared publisher health at ${url}"
  return 1
}

service_status_map() {
  compose ps --format '{{.Service}}|{{.State}}|{{.Status}}' 2>/dev/null || true
}

print_status() {
  load_env 0
  local rollup_a_url=${ROLLUP_A_RPC_URL:-http://localhost:18545}
  local rollup_b_url=${ROLLUP_B_RPC_URL:-http://localhost:28545}
  local publisher_http_url=${ROLLUP_SHARED_PUBLISHER_HTTP_URL:-http://localhost:18080}
  local publisher_health_url=${ROLLUP_SHARED_PUBLISHER_HEALTH_URL:-http://localhost:18081/health}
  local delim=$TABLE_DELIM

  declare -A STATES
  while IFS='|' read -r svc state status; do
    [[ -z $svc ]] && continue
    STATES[$svc]="$state|$status"
  done < <(service_status_map)
  local block_a="-"
  if block_a=$(fetch_block_number "$rollup_a_url"); then
    :
  else
    block_a="(no response)"
  fi

  local block_b="-"
  if block_b=$(fetch_block_number "$rollup_b_url"); then
    :
  else
    block_b="(no response)"
  fi

  local pub_code
  pub_code=$(publisher_health "$publisher_health_url")
  if [[ -z $pub_code ]]; then
    pub_code="(no response)"
  fi

  local publisher_entry
  publisher_entry=$(get_service_entry "rollup-shared-publisher" "missing")
  local publisher_state=${publisher_entry%%|*}
  local publisher_status=${publisher_entry#*|}
  local publisher_status_text
  publisher_status_text=$(format_status_text "$publisher_state" "$publisher_status")
  local health_label
  if [[ $pub_code == "200" ]]; then
    health_label="❤️ healthy"
  else
    health_label="⚠️ health $pub_code"
  fi
  local publisher_rows=("$publisher_status_text${delim}$health_label${delim}$publisher_http_url")
  render_panel "rollup-shared-publisher" "${publisher_rows[@]}"
  printf '\n'

  local block_label_a
  if [[ $block_a =~ ^[0-9]+$ ]]; then
    block_label_a="⛏️ block $block_a"
  else
    block_label_a="⚠️ block $block_a"
  fi
  local block_label_b
  if [[ $block_b =~ ^[0-9]+$ ]]; then
    block_label_b="⛏️ block $block_b"
  else
    block_label_b="⚠️ block $block_b"
  fi

  local rollup_a_rows=()
  rollup_a_rows+=("op-geth${delim}$(format_service_status "op-geth-a" "missing")${delim}$block_label_a${delim}rpc $rollup_a_url")
  rollup_a_rows+=("op-node${delim}$(format_service_status "op-node-a" "missing")${delim}${delim}rpc http://localhost:19545")
  rollup_a_rows+=("batcher${delim}$(format_service_status "op-batcher-a" "missing")${delim}${delim}port 18548")
  rollup_a_rows+=("proposer${delim}$(format_service_status "op-proposer-a" "missing")${delim}${delim}port 18560")
  render_panel "rollup-a (${ROLLUP_A_CHAIN_ID:-?})" "${rollup_a_rows[@]}"
  printf '\n'

  local rollup_b_rows=()
  rollup_b_rows+=("op-geth${delim}$(format_service_status "op-geth-b" "missing")${delim}$block_label_b${delim}rpc $rollup_b_url")
  rollup_b_rows+=("op-node${delim}$(format_service_status "op-node-b" "missing")${delim}${delim}rpc http://localhost:29545")
  rollup_b_rows+=("batcher${delim}$(format_service_status "op-batcher-b" "missing")${delim}${delim}port 28548")
  rollup_b_rows+=("proposer${delim}$(format_service_status "op-proposer-b" "missing")${delim}${delim}port 28560")
  render_panel "rollup-b (${ROLLUP_B_CHAIN_ID:-?})" "${rollup_b_rows[@]}"
  printf '\n'

  load_contracts
  if (( ${#CONTRACTS[@]} > 0 )); then
    local contract_rows=()
    local keys=(Mailbox Bridge PingPong MyToken Coordinator)
    local key
    for key in "${keys[@]}"; do
      local value=${CONTRACTS[$key]:-}
      [[ -n $value ]] && contract_rows+=("$key${delim}$value")
    done
    if (( ${#contract_rows[@]} > 0 )); then
      render_panel "CONTRACTS" "${contract_rows[@]}"
      printf '\n'
    fi
  fi
}

cmd_up() {
  load_env 1
  local running
  running=$(running_services)
  if [[ -n $running ]]; then
    log "Stack already running"
    return 0
  fi

  local existing
  existing=$(existing_services)
  if [[ -n $existing ]]; then
    log "Starting existing containers"
    compose up -d
    return 0
  fi

  if ! is_bootstrapped; then
    log "First run detected; executing scripts/setup.sh"
    (cd "$ROOT_DIR" && ./scripts/setup.sh)
    return 0
  fi

  log "Bringing stack up"
  compose up -d
}

cmd_down() {
  log "Stopping containers"
  compose down || true
}

cmd_purge() {
  local force=0
  if [[ ${1:-} == "--force" || ${1:-} == "-f" ]]; then
    force=1
    shift || true
  fi
  if [[ $force -ne 1 ]]; then
    read -r -p "This will remove containers, volumes, and generated artifacts. Continue? [y/N] " answer
    if [[ ! $answer =~ ^[Yy]$ ]]; then
      log "Aborted"
      return 0
    fi
  fi
  if [[ -f "$ROOT_DIR/networks/rollup-a/runtime.env" || -f "$ROOT_DIR/networks/rollup-b/runtime.env" ]]; then
    log "Stopping and removing containers/volumes"
    compose down -v || true
  else
    log "Skipping compose down (no rollup artifacts present)"
  fi
  log "Removing generated artifacts"
  remove_generated_artifacts
  log "Purge complete"
}

service_targets() {
  local target=$1
  case "$target" in
    op-geth)
      printf '%s\n' "op-geth-a" "op-geth-b" "op-node-a" "op-node-b" "op-batcher-a" "op-batcher-b" "op-proposer-a" "op-proposer-b"
      ;;
    publisher)
      printf '%s\n' "rollup-shared-publisher"
      ;;
    all)
      printf '%s\n' "rollup-shared-publisher" "op-geth-a" "op-geth-b" "op-node-a" "op-node-b" "op-batcher-a" "op-batcher-b" "op-proposer-a" "op-proposer-b"
      ;;
    *)
      err "Unknown target: ${target} (expected op-geth, publisher, or all)"
      exit 1
      ;;
  esac
}

build_targets() {
  local target=$1
  case "$target" in
    op-geth)
      printf '%s\n' "op-geth-a" "op-geth-b"
      ;;
    publisher)
      printf '%s\n' "rollup-shared-publisher"
      ;;
    all)
      printf '%s\n' "rollup-shared-publisher" "op-geth-a" "op-geth-b"
      ;;
  esac
}

restart_services() {
  local target=${1:-all}
  load_env 1
  local -a services
  mapfile -t services < <(service_targets "$target")
  if ((${#services[@]} == 0)); then
    err "No services resolved for target ${target}"
    exit 1
  fi
  log "Restarting target ${target}: ${services[*]}"
  if ! compose restart "${services[@]}" >/dev/null 2>&1; then
    compose up -d "${services[@]}"
  else
    compose up -d "${services[@]}" >/dev/null
  fi
  case "$target" in
    op-geth|all)
      wait_for_rpc "${ROLLUP_A_RPC_URL:-http://localhost:18545}" "rollup-a"
      wait_for_rpc "${ROLLUP_B_RPC_URL:-http://localhost:28545}" "rollup-b"
      ;;
  esac
  case "$target" in
    publisher|all)
      wait_for_publisher "${ROLLUP_SHARED_PUBLISHER_HEALTH_URL:-http://localhost:18081/health}"
      ;;
  esac
}

deploy_services() {
  local target=${1:-all}
  load_env 1
  local -a builds
  mapfile -t builds < <(build_targets "$target")
  if ((${#builds[@]} > 0)); then
    log "Rebuilding images: ${builds[*]}"
    compose build "${builds[@]}"
  fi
  restart_services "$target"
}

main() {
  local cmd=${1:-help}
  case "$cmd" in
    up)
      shift || true
      cmd_up "$@"
      ;;
    down)
      shift || true
      cmd_down "$@"
      ;;
    status)
      shift || true
      print_status "$@"
      ;;
    restart)
      shift || true
      restart_services "${1:-all}"
      ;;
    deploy)
      shift || true
      deploy_services "${1:-all}"
      ;;
    purge)
      shift || true
      cmd_purge "$@"
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      err "Unknown command: $cmd"
      usage
      exit 1
      ;;
  esac
}

main "$@"
