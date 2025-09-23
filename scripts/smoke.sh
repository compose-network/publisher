#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT_DIR/bin/rollup-shared-publisher"
CFG_DEFAULT="$ROOT_DIR/shared-publisher-leader-app/configs/config.yaml"
CFG="${1:-$CFG_DEFAULT}"
API="http://127.0.0.1:8081"

echo "[smoke] building binary…"
make -C "$ROOT_DIR" build >/dev/null

LOG="$(mktemp -t sp_smoke.XXXX).log"
echo "[smoke] starting server (logs: $LOG)…"
"$BIN" --config "$CFG" >"$LOG" 2>&1 &
PID=$!
cleanup() { kill "$PID" 2>/dev/null || true; sleep 0.2; }
trap cleanup EXIT

echo -n "[smoke] waiting for $API/health"
for i in {1..50}; do
  if curl -sf "$API/health" >/dev/null 2>&1; then echo " ✓"; break; fi
  echo -n "."; sleep 0.2
done

echo "[smoke] GET /health"
curl -sSf "$API/health" | jq . || true

echo "[smoke] GET /ready (may be 503 when idle)"
curl -sS -o /dev/null -w "status=%{http_code}\n" "$API/ready" || true

echo "[smoke] GET /stats"
curl -sSf "$API/stats" | jq . | head -n 20 || true

echo "[smoke] GET /metrics"
curl -sSf "$API/metrics" | head -n 10 || true

echo "[smoke] GET /v1/proofs/status/{sbHash} (expect 400 invalid length)"
curl -sS -o /dev/null -w "status=%{http_code}\n" "$API/v1/proofs/status/0x00" || true

echo "[smoke] POST /v1/proofs/op-succinct (expect 400 invalid json)"
curl -sS -o /dev/null -w "status=%{http_code}\n" -X POST "$API/v1/proofs/op-succinct" || true

echo "[smoke] done; tailing last 20 log lines"
tail -n 20 "$LOG" || true
