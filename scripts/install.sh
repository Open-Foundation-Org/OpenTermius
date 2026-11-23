#!/usr/bin/env bash
set -euo pipefail

# OpenTermius Universal Installer (Ubuntu/Debian, RHEL/Fedora, Arch, Alpine, macOS)
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Open-Foundation-Org/OpenTermius/main/scripts/install.sh | bash

REPO_URL="${OPENTERMIUS_REPO:-https://github.com/Open-Foundation-Org/OpenTermius.git}"
INSTALL_DIR="${OPENTERMIUS_DIR:-$HOME/opentermius}"
GO_REQUIRED_MINOR=24
GO_FALLBACK_VERSION="1.27.4"

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
    warn "Go $current_ver is older than 1.${GO_REQUIRED_MINOR}; upgrading to latest stable..."
  fi

  local go_ver
  go_ver="$(curl -fsSL "https://go.dev/VERSION?m=text" 2>/dev/null | head -n 1 | sed 's/^go//' || true)"
  [ -z "$go_ver" ] && go_ver="$GO_FALLBACK_VERSION"

  local url="https://go.dev/dl/go${go_ver}.linux-${GO_ARCH}.tar.gz"
  log "Downloading Go ${go_ver} (${GO_ARCH})..."
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
    if version_ge "$nver" "24.0"; then
      ok "Node.js v$nver already installed"
      return
    fi
  fi

  case "$PLATFORM" in
    debian)
      log "Installing Node.js 24 LTS via NodeSource..."
      curl -fsSL https://deb.nodesource.com/setup_24.x | as_root bash -
      as_root apt-get install -y nodejs
      ;;
    fedora|rhel)
      log "Installing Node.js 24 LTS via NodeSource..."
      curl -fsSL https://rpm.nodesource.com/setup_24.x | as_root bash -
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

postgres_already_running() {
  if [ -n "${DATABASE_URL:-}" ]; then
    return 0
  fi
  if command -v pg_isready >/dev/null 2>&1 && pg_isready -q 2>/dev/null; then
    return 0
  fi
  if (echo >/dev/tcp/127.0.0.1/"${PGPORT:-5432}") 2>/dev/null; then
    return 0
  fi
  return 1
}

PG_MODE="${OPENTERMIUS_PG_MODE:-}"

prompt_input() {
  local __var="$1" __label="$2" __default="$3" __val=""
  if [ -r /dev/tty ]; then
    printf "%s [%s]: " "$__label" "$__default" >/dev/tty
    IFS= read -r __val </dev/tty || true
  fi
  [ -z "$__val" ] && __val="$__default"
  printf -v "$__var" '%s' "$__val"
}

prompt_secret() {
  local __var="$1" __label="$2" __default="$3" __val=""
  if [ -r /dev/tty ]; then
    printf "%s: " "$__label" >/dev/tty
    IFS= read -rs __val </dev/tty || true
    printf "\n" >/dev/tty
  fi
  [ -z "$__val" ] && __val="$__default"
  printf -v "$__var" '%s' "$__val"
}

ask_postgres_mode() {
  if [ -n "$PG_MODE" ]; then
    ok "PostgreSQL mode (from env): $PG_MODE"
    return
  fi
  if [ -n "${DATABASE_URL:-}" ]; then
    PG_MODE="existing"
    ok "DATABASE_URL provided — using existing PostgreSQL (skipping server install)"
    return
  fi
  if [ -f "./.env" ] && grep -q "^DATABASE_URL=" "./.env" 2>/dev/null; then
    PG_MODE="existing"
    ok "Existing .env with DATABASE_URL detected — skipping PostgreSQL server install"
    return
  fi
  if [ -f "$INSTALL_DIR/.env" ] && grep -q "^DATABASE_URL=" "$INSTALL_DIR/.env" 2>/dev/null; then
    PG_MODE="existing"
    ok "Existing $INSTALL_DIR/.env with DATABASE_URL detected — skipping PostgreSQL server install"
    return
  fi

  local default_choice="2"
  if postgres_already_running || command -v psql >/dev/null 2>&1; then
    default_choice="1"
  fi

  if [ -r /dev/tty ]; then
    printf "\nPostgreSQL Setup:\n" >/dev/tty
    printf "  1) Use existing PostgreSQL (local / remote / Docker — skip server install)\n" >/dev/tty
    printf "  2) Install new local PostgreSQL server\n" >/dev/tty
  fi

  local choice=""
  prompt_input choice "Select PostgreSQL option (1/2)" "$default_choice"
  case "$choice" in
    1|existing|Existing|EXISTING)
      PG_MODE="existing"
      ok "Using existing PostgreSQL (server install/upgrade skipped)"
      ;;
    2|install|Install|INSTALL)
      PG_MODE="install"
      ok "Will install local PostgreSQL server"
      ;;
    *)
      if [ "$default_choice" = "1" ]; then
        PG_MODE="existing"
      else
        PG_MODE="install"
      fi
      warn "Invalid choice '$choice', defaulting to: $PG_MODE"
      ;;
  esac
}

install_deps_macos() {
  if ! command -v brew >/dev/null 2>&1; then
    log "Installing Homebrew..."
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
    eval "$(/opt/homebrew/bin/brew shellenv 2>/dev/null || /usr/local/bin/brew shellenv 2>/dev/null || true)"
  fi

  log "Checking Go, Node.js, git, openssl..."
  local brew_pkgs=()
  command -v go >/dev/null 2>&1 || brew_pkgs+=(go)
  command -v node >/dev/null 2>&1 || brew_pkgs+=(node)
  command -v git >/dev/null 2>&1 || brew_pkgs+=(git)
  command -v openssl >/dev/null 2>&1 || brew_pkgs+=(openssl@3)
  if [ "${#brew_pkgs[@]}" -gt 0 ]; then
    brew install "${brew_pkgs[@]}"
  fi

  if [ "$PG_MODE" = "existing" ]; then
    ok "Skipping PostgreSQL server install (using existing PostgreSQL)"
    if ! command -v psql >/dev/null 2>&1; then
      log "Installing psql client (libpq)..."
      brew install libpq
      export PATH="$(brew --prefix libpq)/bin:$PATH"
    fi
  else
    if postgres_already_running; then
      ok "PostgreSQL already running on port ${PGPORT:-5432}"
    else
      log "Installing PostgreSQL via Homebrew..."
      brew install postgresql
      brew services start postgresql || true
      local pg_bin
      pg_bin="$(brew --prefix postgresql)/bin"
      export PATH="$pg_bin:$PATH"
      if ! grep -q "$pg_bin" "$HOME/.zshrc" 2>/dev/null; then
        echo "export PATH=\"$pg_bin:\$PATH\"" >> "$HOME/.zshrc"
      fi
    fi
  fi
  ok "macOS dependencies ready"
}

install_deps_linux() {
  local has_pg=0
  if [ "$PG_MODE" = "existing" ]; then
    has_pg=1
    ok "Skipping PostgreSQL server install/upgrade (using existing PostgreSQL)"
  elif postgres_already_running; then
    has_pg=1
    ok "Existing PostgreSQL already running (skipping server install/initdb)"
  fi

  case "$PLATFORM" in
    debian)
      log "Updating apt & installing base packages..."
      as_root apt-get update -y
      if [ "$has_pg" -eq 1 ]; then
        as_root apt-get install -y curl git build-essential openssl ca-certificates postgresql-client
      else
        as_root apt-get install -y curl git build-essential openssl ca-certificates postgresql postgresql-contrib
        if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
          as_root systemctl enable --now postgresql
        else
          as_root service postgresql start || true
        fi
      fi
      ;;
    fedora)
      log "Installing base packages via dnf..."
      if [ "$has_pg" -eq 1 ]; then
        as_root dnf install -y curl git gcc make openssl ca-certificates postgresql
      else
        as_root dnf install -y curl git gcc make openssl ca-certificates postgresql-server postgresql-contrib
        if [ ! -f /var/lib/pgsql/data/PG_VERSION ]; then
          as_root postgresql-setup --initdb
        fi
        if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
          as_root systemctl enable --now postgresql
        fi
      fi
      ;;
    rhel)
      log "Installing base packages via yum..."
      if [ "$has_pg" -eq 1 ]; then
        as_root yum install -y curl git gcc make openssl ca-certificates postgresql
      else
        as_root yum install -y curl git gcc make openssl ca-certificates postgresql-server postgresql-contrib
        if [ ! -f /var/lib/pgsql/data/PG_VERSION ]; then
          as_root postgresql-setup initdb || as_root postgresql-setup --initdb
        fi
        if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
          as_root systemctl enable --now postgresql
        fi
      fi
      ;;
    arch)
      log "Installing base packages via pacman..."
      as_root pacman -Sy --noconfirm --needed curl git base-devel openssl ca-certificates postgresql
      if [ "$has_pg" -eq 0 ]; then
        if [ ! -f /var/lib/postgres/data/PG_VERSION ]; then
          as_root -u postgres initdb -D /var/lib/postgres/data
        fi
        if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
          as_root systemctl enable --now postgresql
        fi
      fi
      ;;
    alpine)
      log "Installing base packages via apk..."
      if [ "$has_pg" -eq 1 ]; then
        as_root apk add --no-cache bash curl git build-base openssl ca-certificates postgresql-client
      else
        as_root apk add --no-cache bash curl git build-base openssl ca-certificates postgresql postgresql-contrib
        if [ ! -f /var/lib/postgresql/data/PG_VERSION ]; then
          as_root mkdir -p /var/lib/postgresql/data /run/postgresql
          as_root chown -R postgres:postgres /var/lib/postgresql /run/postgresql
          as_root su - postgres -c "initdb -D /var/lib/postgresql/data"
        fi
        as_root rc-service postgresql start 2>/dev/null || as_root su - postgres -c "pg_ctl -D /var/lib/postgresql/data start" || true
      fi
      ;;
  esac

  install_go_tarball
  install_node_linux
}

clone_or_update_repo() {
  # If user runs this script from inside an existing clone, use that directory and pull updates
  if [ -f "./backend/go.mod" ] && [ -d "./gateway-web" ] && [ -d "./migrations" ]; then
    INSTALL_DIR="$(pwd)"
    ok "Using existing workspace: $INSTALL_DIR"
    if [ -d "$INSTALL_DIR/.git" ]; then
      log "Pulling latest updates in $INSTALL_DIR..."
      git -C "$INSTALL_DIR" pull --ff-only || warn "git pull skipped (local changes present)"
    fi
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

urlencode() {
  local s="$1" out="" i c hex
  for ((i = 0; i < ${#s}; i++)); do
    c="${s:i:1}"
    case "$c" in
      [a-zA-Z0-9.~_-]) out+="$c" ;;
      *) printf -v hex '%%%02X' "'$c"; out+="$hex" ;;
    esac
  done
  printf '%s' "$out"
}

ensure_pg_hba_password_auth() {
  [ "$PLATFORM" = "macos" ] && return 0
  id postgres >/dev/null 2>&1 || return 0

  local hba_file
  hba_file="$(as_root su - postgres -c "psql -tAc 'SHOW hba_file;'" 2>/dev/null | tr -d '[:space:]' || true)"
  [ -n "$hba_file" ] && [ -f "$hba_file" ] || return 0

  if as_root grep -Eq '^(host|hostnossl)[[:space:]]+all[[:space:]]+all[[:space:]]+(127\.0\.0\.1/32|::1/128)[[:space:]]+ident' "$hba_file"; then
    log "Updating $hba_file: switching localhost ident -> scram-sha-256..."
    as_root sed -i.bak -E \
      's/^((host|hostnossl)[[:space:]]+all[[:space:]]+all[[:space:]]+(127\.0\.0\.1\/32|::1\/128)[[:space:]]+)ident/\1scram-sha-256/' \
      "$hba_file"
    as_root su - postgres -c "psql -c 'SELECT pg_reload_conf();'" >/dev/null 2>&1 || true
    ok "PostgreSQL localhost password auth enabled"
  fi
}

ensure_local_pg_role_and_db() {
  local db_user="$1" db_pass="$2" db_name="$3"
  local sql_user="${db_user//\'/\'\'}"
  local sql_pass="${db_pass//\'/\'\'}"
  local sql_db="${db_name//\'/\'\'}"

  if [ "$PLATFORM" = "macos" ] && psql -d postgres -c "SELECT 1;" >/dev/null 2>&1; then
    if psql -d postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='${sql_user}'" | grep -q 1; then
      [ -n "$db_pass" ] && psql -d postgres -c "ALTER ROLE \"${db_user}\" WITH LOGIN PASSWORD '${sql_pass}';" >/dev/null
    else
      if [ -n "$db_pass" ]; then
        psql -d postgres -c "CREATE ROLE \"${db_user}\" WITH LOGIN PASSWORD '${sql_pass}';" >/dev/null
      else
        psql -d postgres -c "CREATE ROLE \"${db_user}\" WITH LOGIN;" >/dev/null
      fi
    fi
    psql -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${sql_db}'" | grep -q 1 || \
      psql -d postgres -c "CREATE DATABASE \"${db_name}\" OWNER \"${db_user}\";" >/dev/null
    return 0
  elif id postgres >/dev/null 2>&1; then
    ensure_pg_hba_password_auth
    if as_root su - postgres -c "psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname='${sql_user}'\"" | grep -q 1; then
      [ -n "$db_pass" ] && as_root su - postgres -c "psql -c \"ALTER ROLE \\\"${db_user}\\\" WITH LOGIN PASSWORD '${sql_pass}';\"" >/dev/null
    else
      if [ -n "$db_pass" ]; then
        as_root su - postgres -c "psql -c \"CREATE ROLE \\\"${db_user}\\\" WITH LOGIN PASSWORD '${sql_pass}';\"" >/dev/null
      else
        as_root su - postgres -c "psql -c \"CREATE ROLE \\\"${db_user}\\\" WITH LOGIN;\"" >/dev/null
      fi
    fi
    as_root su - postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname='${sql_db}'\"" | grep -q 1 || \
      as_root su - postgres -c "psql -c \"CREATE DATABASE \\\"${db_name}\\\" OWNER \\\"${db_user}\\\";\"" >/dev/null
    return 0
  fi
  return 1
}

ensure_env_key() {
  local file="$1" key="$2" val="$3"
  if ! grep -Eq "^${key}=" "$file" 2>/dev/null; then
    printf "%s=%s\n" "$key" "$val" >> "$file"
  fi
}

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

port_in_use_by_other() {
  local port="$1" svc="$2"
  if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet "$svc" 2>/dev/null; then
    return 1
  fi
  (echo >/dev/tcp/127.0.0.1/"$port") 2>/dev/null
}

setup_database_and_env() {
  local env_file="$INSTALL_DIR/.env"
  local keep_env="Y"
  local db_host="localhost"
  local db_port="${PGPORT:-5432}"
  local db_name="opentermius"
  local db_user="opentermius"
  local db_pass=""
  local use_existing="N"

  if [ -f "$env_file" ]; then
    prompt_input keep_env "Existing .env found at $env_file. Keep it? (Y/n)" "Y"
    if [[ "$keep_env" =~ ^[Yy]$ ]]; then
      ok "Preserving existing $env_file (backfilling any new env keys)"
      ensure_env_key "$env_file" "DEFAULT_ADMIN_EMAIL" "admin@opentermius.local"
      ensure_env_key "$env_file" "DEFAULT_ADMIN_NAME" "Gateway Admin"
      ensure_env_key "$env_file" "ALLOW_PUBLIC_REGISTER" "false"
      ensure_env_key "$env_file" "ALLOWED_HOSTS" ""
      ensure_env_key "$env_file" "API_PROXY_TARGET" ""
      ensure_env_key "$env_file" "VITE_API_BASE" ""
      load_env_file "$env_file"
    fi
  fi

  if [ ! -f "$env_file" ] || [[ ! "$keep_env" =~ ^[Yy]$ ]]; then
    local app_secret jwt_secret
    app_secret="$(openssl rand -hex 32)"
    jwt_secret="$(openssl rand -hex 32)"

    if [ -n "${DATABASE_URL:-}" ]; then
      ok "Using user-provided DATABASE_URL"
    else
      if [ "$PG_MODE" = "existing" ]; then
        use_existing="Y"
      elif [ "$PG_MODE" = "install" ]; then
        use_existing="N"
      elif postgres_already_running; then
        prompt_input use_existing "PostgreSQL detected. Connect to an existing database? (Y/n)" "Y"
      else
        prompt_input use_existing "Connect to an existing external/local PostgreSQL database? (y/N)" "N"
      fi

      if [[ "$use_existing" =~ ^[Yy]$ ]]; then
        prompt_input db_host "Database host" "localhost"
        prompt_input db_port "Database port" "${PGPORT:-5432}"
        prompt_input db_name "Database name" "opentermius"
        prompt_input db_user "Database username" "opentermius"
        prompt_secret db_pass "Database password (hidden; leave empty if none)" ""

        local default_ssl="require"
        if [ "$db_host" = "localhost" ] || [ "$db_host" = "127.0.0.1" ]; then
          default_ssl="disable"
          ensure_pg_hba_password_auth
        fi
        local db_ssl=""
        prompt_input db_ssl "Database SSL mode (disable/require/verify-full)" "$default_ssl"

        local enc_user enc_pass
        enc_user="$(urlencode "$db_user")"
        enc_pass="$(urlencode "$db_pass")"
        if [ -n "$db_pass" ]; then
          DATABASE_URL="postgres://${enc_user}:${enc_pass}@${db_host}:${db_port}/${db_name}?sslmode=${db_ssl}"
        else
          DATABASE_URL="postgres://${enc_user}@${db_host}:${db_port}/${db_name}?sslmode=${db_ssl}"
        fi

        local psql_err=""
        if ! psql_err="$(psql "$DATABASE_URL" -c "SELECT 1;" 2>&1 >/dev/null)"; then
          if [ "$db_host" = "localhost" ] || [ "$db_host" = "127.0.0.1" ]; then
            warn "Could not connect as '${db_user}' yet; attempting to create/sync role & DB locally..."
            ensure_local_pg_role_and_db "$db_user" "$db_pass" "$db_name" || \
              err "Failed to connect or initialize local database '${db_name}' for user '${db_user}': ${psql_err}"
          else
            err "Cannot connect to ${db_host}:${db_port}/${db_name} as '${db_user}': ${psql_err}"
          fi
        fi
      else
        prompt_input db_user "New database username" "opentermius"
        prompt_secret db_pass "New database password (hidden; Enter = auto-generate)" ""
        [ -z "$db_pass" ] && db_pass="$(openssl rand -hex 16)"

        log "Ensuring PostgreSQL role '$db_user' and database '$db_name'..."
        ensure_local_pg_role_and_db "$db_user" "$db_pass" "$db_name" || \
          err "Could not create role/database automatically. Re-run and choose existing DB or pass DATABASE_URL."

        local enc_user enc_pass
        enc_user="$(urlencode "$db_user")"
        enc_pass="$(urlencode "$db_pass")"
        DATABASE_URL="postgres://${enc_user}:${enc_pass}@${db_host}:${db_port}/${db_name}?sslmode=disable"
      fi
    fi

    local api_port="8080"
    local web_port="5174"
    if port_in_use_by_other 8080 opentermius-api; then
      warn "Port 8080 is already in use by another process."
      api_port="8081"
      while port_in_use_by_other "$api_port" opentermius-api; do
        api_port=$((api_port + 1))
      done
    fi
    if port_in_use_by_other 5174 opentermius-web; then
      warn "Port 5174 is already in use by another process."
      web_port="5175"
      while port_in_use_by_other "$web_port" opentermius-web; do
        web_port=$((web_port + 1))
      done
    fi
    prompt_input api_port "Gateway API port" "$api_port"
    prompt_input web_port "Gateway Web UI port" "$web_port"
    SERVER_PORT="$api_port"
    WEB_PORT="$web_port"

    local detected_host="localhost"
    if command -v hostname >/dev/null 2>&1; then
      detected_host="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
      [ -z "$detected_host" ] && detected_host="localhost"
    fi
    local public_host="$detected_host"
    prompt_input public_host "Gateway public domain or IP (e.g. webgw.example.com)" "$detected_host"
    public_host="${public_host#http://}"
    public_host="${public_host#https://}"
    public_host="${public_host%%/*}"
    GATEWAY_HOST="$public_host"

    local admin_email="admin@opentermius.local"
    local admin_name="Gateway Admin"
    local admin_pass=""
    prompt_input admin_email "Initial Gateway Admin Email" "$admin_email"
    prompt_input admin_name "Initial Gateway Admin Name" "$admin_name"
    prompt_secret admin_pass "Initial Gateway Admin Password (hidden; Enter = OpenTermius2026!)" "OpenTermius2026!"
    DEFAULT_ADMIN_EMAIL="$admin_email"
    DEFAULT_ADMIN_NAME="$admin_name"
    DEFAULT_ADMIN_PASSWORD="$admin_pass"

    local allow_reg_choice="N"
    local allow_public_register="false"
    prompt_input allow_reg_choice "Allow public user self-registration? (y/N — No = invite-only)" "N"
    if [[ "$allow_reg_choice" =~ ^[Yy]$ ]]; then
      allow_public_register="true"
    fi
    ALLOW_PUBLIC_REGISTER="$allow_public_register"

    local cors_origins=""
    prompt_input cors_origins "Restrict CORS origins? (Leave empty to allow all / reverse proxy & desktop apps)" ""
    local allowed_hosts=""
    prompt_input allowed_hosts "Restrict Vite preview allowed hosts? (Leave empty to allow any reverse-proxy domain)" ""

    cat > "$env_file" <<EOF
# OpenTermius Gateway Configuration (generated by install.sh)

# 1. Server & Gateway Web UI
SERVER_PORT=${SERVER_PORT}
SERVER_HOST=0.0.0.0
WEB_PORT=${WEB_PORT}
GATEWAY_HOST=${GATEWAY_HOST}
APP_ENV=production

# 2. Security & Encryption Secrets
APP_SECRET=${app_secret}
JWT_SECRET=${jwt_secret}

# 3. Default Gateway Admin Account (seeded on first startup)
DEFAULT_ADMIN_EMAIL=${DEFAULT_ADMIN_EMAIL}
DEFAULT_ADMIN_PASSWORD=${DEFAULT_ADMIN_PASSWORD}
DEFAULT_ADMIN_NAME=${DEFAULT_ADMIN_NAME}

# 4. Registration & Access Policy
ALLOW_PUBLIC_REGISTER=${ALLOW_PUBLIC_REGISTER}

# 5. Reverse Proxy, CORS & Vite Preview Hosts
CORS_ALLOWED_ORIGINS=${cors_origins}
ALLOWED_HOSTS=${allowed_hosts}
API_PROXY_TARGET=
VITE_API_BASE=

# 6. Database (PostgreSQL)
DATABASE_URL=${DATABASE_URL}
EOF
    chmod 600 "$env_file"
    ok "Generated $env_file with secure random secrets"
  fi

  log "Applying PostgreSQL migrations..."
  for mig in "$INSTALL_DIR"/migrations/*.up.sql; do
    [ -f "$mig" ] || continue
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$mig" >/dev/null
  done
  ok "Database schema migrated"
}

build_components() {
  log "Building Go Gateway API (backend/bin/server)..."
  (cd "$INSTALL_DIR/backend" && go build -o bin/server ./cmd/api)
  ok "Built $INSTALL_DIR/backend/bin/server"

  log "Installing & building Gateway Web UI..."
  (
    if [ -f "$INSTALL_DIR/.env" ]; then
      load_env_file "$INSTALL_DIR/.env"
    fi
    cd "$INSTALL_DIR/gateway-web"
    export VITE_API_PORT="${VITE_API_PORT:-${SERVER_PORT:-8080}}"
    if [ -n "${VITE_API_BASE:-}" ]; then
      export VITE_API_BASE
    fi
    (npm ci --no-audit --no-fund 2>/dev/null || npm install --no-audit --no-fund) && npm run build
  )
  ok "Built $INSTALL_DIR/gateway-web/dist"
}

setup_services() {
  chmod +x "$INSTALL_DIR/scripts/start-gateway.sh" "$INSTALL_DIR/scripts/stop-gateway.sh" 2>/dev/null || true
  local web_port="${WEB_PORT:-5174}"

  if [ "$PLATFORM" != "macos" ] && command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    local run_user
    run_user="$(stat -c '%U' "$INSTALL_DIR" 2>/dev/null || echo "${SUDO_USER:-$(id -un)}")"
    if [[ "$INSTALL_DIR" == /root* ]]; then
      run_user="root"
    fi
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
EnvironmentFile=${INSTALL_DIR}/.env
ExecStart=${node_bin} vite preview --host 0.0.0.0 --port ${web_port}
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

    as_root systemctl daemon-reload
    as_root systemctl enable opentermius-api opentermius-web
    as_root systemctl restart opentermius-api opentermius-web
    ok "systemd services started (opentermius-api, opentermius-web)"
  else
    log "Starting OpenTermius Gateway in background via scripts/start-gateway.sh..."
    "$INSTALL_DIR/scripts/start-gateway.sh"
  fi
}

print_summary() {
  local host_ip="${GATEWAY_HOST:-localhost}"
  if [ "$host_ip" = "localhost" ] && command -v hostname >/dev/null 2>&1; then
    host_ip="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
    [ -z "$host_ip" ] && host_ip="localhost"
  fi

  cat <<EOF

============================================================
  OpenTermius Gateway Installed Successfully!
============================================================
  Install Dir : ${INSTALL_DIR}
  Env File    : ${INSTALL_DIR}/.env

  Gateway API : http://${host_ip}:${SERVER_PORT:-8080}  (Health: /health)
  Gateway Web : http://${host_ip}:${WEB_PORT:-5174}

  Default Admin Login:
    Email     : ${DEFAULT_ADMIN_EMAIL:-admin@opentermius.local}
    Password  : ${DEFAULT_ADMIN_PASSWORD:-OpenTermius2026!}
    (Can be changed anytime from Gateway Web -> Settings -> Admin Account)

  Manage Services:
    Linux (systemd): sudo systemctl status opentermius-api opentermius-web
    Manual / macOS : ${INSTALL_DIR}/scripts/start-gateway.sh
                     ${INSTALL_DIR}/scripts/stop-gateway.sh
============================================================
EOF
}

main() {
  detect_platform
  ask_postgres_mode
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
