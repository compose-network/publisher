#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

function log() {
  printf '[local-compose] %s\n' "$*"
}

function require_env() {
  local name=$1
  if [[ -z ${!name:-} ]]; then
    echo "Environment variable $name is required" >&2
    exit 1
  fi
}

function load_env() {
  if [[ -f "$ROOT_DIR/.env" ]]; then
    set -a
    # shellcheck source=/dev/null
    source "$ROOT_DIR/.env"
    set +a
  else
    echo "Missing .env file" >&2
    exit 1
  fi
}

