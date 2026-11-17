#!/usr/bin/env bash
set -euo pipefail

# OpenTermius Universal Installer (Ubuntu/Debian, RHEL/Fedora, Arch, Alpine, macOS)
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/wisnu-w/OpenTermius/main/scripts/install.sh | bash

REPO_URL="${OPENTERMIUS_REPO:-https://github.com/wisnu-w/OpenTermius.git}"
INSTALL_DIR="${OPENTERMIUS_DIR:-$HOME/opentermius}"
GO_REQUIRED_MINOR=24
GO_INSTALL_VERSION="1.24.2"

log()  { printf "\033[1;34m[OpenTermius]\033[0m %s\n" "$*"; }
ok()   { printf "\033[1;32m[OK]\033[0m %s\n" "$*"; }
warn() { printf "\033[1;33m[WARN]\033[0m %s\n" "$*"; }
err()  { printf "\033[1;31m[ERR]\033[0m %s\n" "$*" >&2; exit 1; }

as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    err "Root privileges required for: $* (sudo not found)"
  fi
}

detect_platform() {
  OS="$(uname -s)"
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64|amd64) GO_ARCH="amd64" ;;
    arm64|aarch64) GO_ARCH="arm64" ;;
    *) err "Unsupported CPU architecture: $ARCH" ;;
  esac

  case "$OS" in
    Darwin)
      PLATFORM="macos"
      ;;
    Linux)
      if command -v apt-get >/dev/null 2>&1; then
        PLATFORM="debian"
      elif command -v dnf >/dev/null 2>&1; then
        PLATFORM="fedora"
      elif command -v yum >/dev/null 2>&1; then
        PLATFORM="rhel"
      elif command -v pacman >/dev/null 2>&1; then
        PLATFORM="arch"
      elif command -v apk >/dev/null 2>&1; then
        PLATFORM="alpine"
      else
        err "Unsupported Linux distribution (no apt/dnf/yum/pacman/apk found)."
      fi
      ;;
    *)
      err "Unsupported OS: $OS (use Linux or macOS)."
      ;;
  esac
  log "Detected platform: $PLATFORM ($OS / $ARCH)"
}

version_ge() {
  # returns 0 if $1 >= $2 (major.minor)
  local cur_maj cur_min req_maj req_min
  cur_maj="${1%%.*}"; cur_min="${1#*.}"; cur_min="${cur_min%%.*}"
  req_maj="${2%%.*}"; req_min="${2#*.}"; req_min="${req_min%%.*}"
  [ "$cur_maj" -gt "$req_maj" ] || { [ "$cur_maj" -eq "$req_maj" ] && [ "$cur_min" -ge "$req_min" ]; }
}

install_go_tarball() {
  local current_ver=""
  if command -v go >/dev/null 2>&1; then
    current_ver="$(go version | awk '{print $3}' | sed 's/^go//')"
    if version_ge "$current_ver" "1.${GO_REQUIRED_MINOR}"; then
      ok "Go $current_ver already installed"
      return
    fi
    warn "Go $current_ver is older than 1.${GO_REQUIRED_MINOR}; upgrading to $GO_INSTALL_VERSION..."
  fi

  local url="https://go.dev/dl/go${GO_INSTALL_VERSION}.linux-${GO_ARCH}.tar.gz"
  log "Downloading Go ${GO_INSTALL_VERSION} (${GO_ARCH})..."
  curl -fsSL "$url" -o /tmp/go.tar.gz
  as_root rm -rf /usr/local/go
  as_root tar -C /usr/local -xzf /tmp/go.tar.gz
  rm -f /tmp/go.tar.gz
  export PATH="/usr/local/go/bin:$PATH"
  if ! grep -q '/usr/local/go/bin' "$HOME/.profile" 2>/dev/null; then
    echo 'export PATH="/usr/local/go/bin:$PATH"' >> "$HOME/.profile"
  fi
  ok "Go $(go version | awk '{print $3}') installed"
}

install_node_linux() {
  if command -v node >/dev/null 2>&1; then
    local nver
    nver="$(node -v | sed 's/^v//')"
    if version_ge "$nver" "20.0"; then
      ok "Node.js v$nver already installed"
      return
    fi
  fi

  case "$PLATFORM" in
    debian)
      log "Installing Node.js 22 LTS via NodeSource..."
      curl -fsSL https://deb.nodesource.com/setup_22.x | as_root bash -
      as_root apt-get install -y nodejs
      ;;
    fedora|rhel)
      log "Installing Node.js 22 LTS via NodeSource..."
      curl -fsSL https://rpm.nodesource.com/setup_22.x | as_root bash -
      if [ "$PLATFORM" = "fedora" ]; then
        as_root dnf install -y nodejs
      else
        as_root yum install -y nodejs
      fi
      ;;
    arch)
      as_root pacman -Sy --noconfirm --needed nodejs npm
      ;;
    alpine)
      as_root apk add --no-cache nodejs npm
      ;;
  esac
  ok "Node.js $(node -v) installed"
}

install_deps_macos() {
  if ! command -v brew >/dev/null 2>&1; then
    log "Installing Homebrew..."
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
    eval "$(/opt/homebrew/bin/brew shellenv 2>/dev/null || /usr/local/bin/brew shellenv 2>/dev/null || true)"
  fi

  log "Installing PostgreSQL, Go, Node.js, git, openssl via Homebrew..."
  brew install postgresql@16 go node git openssl@3
  brew services start postgresql@16 || true

  local pg_bin
  pg_bin="$(brew --prefix postgresql@16)/bin"
  export PATH="$pg_bin:$PATH"
  if ! grep -q "$pg_bin" "$HOME/.zshrc" 2>/dev/null; then
    echo "export PATH=\"$pg_bin:\$PATH\"" >> "$HOME/.zshrc"
  fi
  ok "macOS dependencies ready"
}

install_deps_linux() {
  case "$PLATFORM" in
    debian)
      log "Updating apt & installing base packages + PostgreSQL..."
      as_root apt-get update -y
      as_root apt-get install -y curl git build-essential openssl ca-certificates postgresql postgresql-contrib
      if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
        as_root systemctl enable --now postgresql
      else
        as_root service postgresql start || true
      fi
      ;;
    fedora)
      log "Installing base packages + PostgreSQL via dnf..."
      as_root dnf install -y curl git gcc make openssl ca-certificates postgresql-server postgresql-contrib
      if [ ! -f /var/lib/pgsql/data/PG_VERSION ]; then
        as_root postgresql-setup --initdb
      fi
      if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
        as_root systemctl enable --now postgresql
      fi
      ;;
    rhel)
      log "Installing base packages + PostgreSQL via yum..."
      as_root yum install -y curl git gcc make openssl ca-certificates postgresql-server postgresql-contrib
      if [ ! -f /var/lib/pgsql/data/PG_VERSION ]; then
        as_root postgresql-setup initdb || as_root postgresql-setup --initdb
      fi
      if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
        as_root systemctl enable --now postgresql
      fi
      ;;
    arch)
      log "Installing base packages + PostgreSQL via pacman..."
      as_root pacman -Sy --noconfirm --needed curl git base-devel openssl ca-certificates postgresql
      if [ ! -f /var/lib/postgres/data/PG_VERSION ]; then
        as_root -u postgres initdb -D /var/lib/postgres/data
      fi
      if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
        as_root systemctl enable --now postgresql
      fi
      ;;
    alpine)
      log "Installing base packages + PostgreSQL via apk..."
      as_root apk add --no-cache bash curl git build-base openssl ca-certificates postgresql postgresql-contrib
      if [ ! -f /var/lib/postgresql/data/PG_VERSION ]; then
        as_root mkdir -p /var/lib/postgresql/data /run/postgresql
        as_root chown -R postgres:postgres /var/lib/postgresql /run/postgresql
        as_root su - postgres -c "initdb -D /var/lib/postgresql/data"
      fi
      as_root rc-service postgresql start 2>/dev/null || as_root su - postgres -c "pg_ctl -D /var/lib/postgresql/data start" || true
      ;;
  esac

  install_go_tarball
  install_node_linux
}

clone_or_update_repo() {
  # If user runs this script from inside an existing clone, use that directory
  if [ -f "./backend/go.mod" ] && [ -d "./gateway-web" ] && [ -d "./migrations" ]; then
    INSTALL_DIR="$(pwd)"
    ok "Using existing workspace: $INSTALL_DIR"
    return
  fi

  if [ -d "$INSTALL_DIR/.git" ]; then
    log "Updating existing repo in $INSTALL_DIR..."
    git -C "$INSTALL_DIR" pull --ff-only || warn "git pull skipped (local changes present)"
  else
    log "Cloning OpenTermius into $INSTALL_DIR..."
    git clone "$REPO_URL" "$INSTALL_DIR"
  fi
}

run_psql_admin() {
  if [ "$PLATFORM" = "macos" ]; then
    psql -d postgres "$@"
  else
    as_root su - postgres -c "psql -v ON_ERROR_STOP=1 $*"
  fi
}

setup_database_and_env() {
  local env_file="$INSTALL_DIR/.env"
  local db_user="opentermius"
  local db_name="opentermius"
  local db_pass

  if [ -f "$env_file" ]; then
    ok "Preserving existing $env_file"
    # shellcheck disable=SC1090
    set -a; source "$env_file"; set +a
  else
    db_pass="$(openssl rand -hex 16)"
    local app_secret jwt_secret
    app_secret="$(openssl rand -hex 32)"
    jwt_secret="$(openssl rand -hex 32)"

    log "Creating PostgreSQL role '$db_user' and database '$db_name'..."
    if [ "$PLATFORM" = "macos" ]; then
      psql -d postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='$db_user'" | grep -q 1 || \
        psql -d postgres -c "CREATE ROLE $db_user WITH LOGIN PASSWORD '$db_pass';"
      psql -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='$db_name'" | grep -q 1 || \
        psql -d postgres -c "CREATE DATABASE $db_name OWNER $db_user;"
    else
      as_root su - postgres -c "psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname='$db_user'\"" | grep -q 1 || \
        as_root su - postgres -c "psql -c \"CREATE ROLE $db_user WITH LOGIN PASSWORD '$db_pass';\""
      as_root su - postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname='$db_name'\"" | grep -q 1 || \
        as_root su - postgres -c "psql -c \"CREATE DATABASE $db_name OWNER $db_user;\""
    fi

    DATABASE_URL="postgres://${db_user}:${db_pass}@localhost:5432/${db_name}?sslmode=disable"
    cat > "$env_file" <<EOF
# OpenTermius Gateway Configuration (generated by install.sh)
SERVER_PORT=8080
SERVER_HOST=0.0.0.0
APP_ENV=production
APP_SECRET=${app_secret}

DATABASE_URL=${DATABASE_URL}
DB_MAX_OPEN_CONNS=25
DB_MAX_IDLE_CONNS=5
DB_CONN_MAX_LIFETIME=5m

JWT_SECRET=${jwt_secret}
JWT_EXPIRATION_HOURS=72

CORS_ALLOWED_ORIGINS=http://localhost:5174,http://127.0.0.1:5174,http://localhost:5173

TERMINAL_PING_INTERVAL_SECONDS=30
TERMINAL_MAX_BUFFER_BYTES=1048576
EOF
    chmod 600 "$env_file"
    ok "Generated $env_file with secure random secrets"
  fi

  log "Applying PostgreSQL migrations..."
  for mig in "$INSTALL_DIR"/migrations/*.up.sql; do
    [ -f "$mig" ] || continue
    PGPASSWORD="${db_pass:-}" psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$mig" >/dev/null
  done
  ok "Database schema migrated"
}

build_components() {
  log "Building Go Gateway API (backend/bin/server)..."
  (cd "$INSTALL_DIR/backend" && go build -o bin/server ./cmd/api)
  ok "Built $INSTALL_DIR/backend/bin/server"

  log "Installing & building Gateway Web UI..."
  (cd "$INSTALL_DIR/gateway-web" && (npm ci --no-audit --no-fund 2>/dev/null || npm install --no-audit --no-fund) && npm run build)
  ok "Built $INSTALL_DIR/gateway-web/dist"
}

setup_services() {
  chmod +x "$INSTALL_DIR/scripts/start-gateway.sh" "$INSTALL_DIR/scripts/stop-gateway.sh" 2>/dev/null || true

  if [ "$PLATFORM" != "macos" ] && command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    local run_user="${SUDO_USER:-$USER}"
    local node_bin
    node_bin="$(command -v npx)"

    log "Registering systemd services (opentermius-api & opentermius-web)..."
    as_root tee /etc/systemd/system/opentermius-api.service >/dev/null <<EOF
[Unit]
Description=OpenTermius Gateway API
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
User=${run_user}
WorkingDirectory=${INSTALL_DIR}/backend
EnvironmentFile=${INSTALL_DIR}/.env
ExecStart=${INSTALL_DIR}/backend/bin/server
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

    as_root tee /etc/systemd/system/opentermius-web.service >/dev/null <<EOF
[Unit]
Description=OpenTermius Gateway Web UI
After=network.target opentermius-api.service

[Service]
Type=simple
User=${run_user}
WorkingDirectory=${INSTALL_DIR}/gateway-web
ExecStart=${node_bin} vite preview --host 0.0.0.0 --port 5174
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

    as_root systemctl daemon-reload
    as_root systemctl enable --now opentermius-api opentermius-web
    ok "systemd services started (opentermius-api, opentermius-web)"
  else
    log "Starting OpenTermius Gateway in background via scripts/start-gateway.sh..."
    "$INSTALL_DIR/scripts/start-gateway.sh"
  fi
}

print_summary() {
  local host_ip="localhost"
  if command -v hostname >/dev/null 2>&1; then
    host_ip="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
    [ -z "$host_ip" ] && host_ip="localhost"
  fi

  cat <<EOF

============================================================
  OpenTermius Gateway Installed Successfully!
============================================================
  Install Dir : ${INSTALL_DIR}
  Env File    : ${INSTALL_DIR}/.env

  Gateway API : http://${host_ip}:8080  (Health: /health)
  Gateway Web : http://${host_ip}:5174

  Default Admin Login:
    Email     : admin@opentermius.local
    Password  : OpenTermius2026!
    (Change password after first login!)

  Manage Services:
    Linux (systemd): sudo systemctl status opentermius-api opentermius-web
    Manual / macOS : ${INSTALL_DIR}/scripts/start-gateway.sh
                     ${INSTALL_DIR}/scripts/stop-gateway.sh
============================================================
EOF
}

main() {
  detect_platform
  if [ "$PLATFORM" = "macos" ]; then
    install_deps_macos
  else
    install_deps_linux
  fi
  clone_or_update_repo
  setup_database_and_env
  build_components
  setup_services
  print_summary
}

main "$@"
