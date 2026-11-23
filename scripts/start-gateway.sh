#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_DIR="$ROOT_DIR/.run"
mkdir -p "$RUN_DIR"

load_env_file() {
  local file="$1" line key val
  [ -f "$file" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    [ -z "$line" ] || [[ "$line" == \#* ]] && continue
    if [[ "$line" == *=* ]]; then
      key="${line%%=*}"
      val="${line#*=}"
      key="${key%"${key##*[![:space:]]}"}"
      val="${val#"${val%%[![:space:]]*}"}"
      if [[ "$val" =~ ^\".*\"$ ]] || [[ "$val" =~ ^\'.*\'$ ]]; then
        val="${val:1:${#val}-2}"
      fi
      if [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
        export "$key=$val"
      fi
    fi
  done < "$file"
}

if [ -f "$ROOT_DIR/.env" ]; then
  load_env_file "$ROOT_DIR/.env"
fi

"$ROOT_DIR/scripts/stop-gateway.sh" >/dev/null 2>&1 || true

echo "Starting OpenTermius Gateway API on :${SERVER_PORT:-8080}..."
(cd "$ROOT_DIR/backend" && nohup ./bin/server > "$RUN_DIR/api.log" 2>&1 & echo $! > "$RUN_DIR/api.pid")

echo "Starting OpenTermius Gateway Web on :${WEB_PORT:-5174}..."
(cd "$ROOT_DIR/gateway-web" && nohup npx vite preview --host 0.0.0.0 --port "${WEB_PORT:-5174}" > "$RUN_DIR/web.log" 2>&1 & echo $! > "$RUN_DIR/web.pid")

sleep 1
echo "Started. Logs: $RUN_DIR/api.log and $RUN_DIR/web.log"
