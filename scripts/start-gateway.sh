#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="$ROOT_DIR/.run"
mkdir -p "$RUN_DIR"

if [ -f "$ROOT_DIR/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT_DIR/.env"
  set +a
fi

"$ROOT_DIR/scripts/stop-gateway.sh" >/dev/null 2>&1 || true

echo "Starting OpenTermius Gateway API on :${SERVER_PORT:-8080}..."
(cd "$ROOT_DIR/backend" && nohup ./bin/server > "$RUN_DIR/api.log" 2>&1 & echo $! > "$RUN_DIR/api.pid")

echo "Starting OpenTermius Gateway Web on :5174..."
(cd "$ROOT_DIR/gateway-web" && nohup npx vite preview --host 0.0.0.0 --port 5174 > "$RUN_DIR/web.log" 2>&1 & echo $! > "$RUN_DIR/web.pid")

sleep 1
echo "Started. Logs: $RUN_DIR/api.log and $RUN_DIR/web.log"
