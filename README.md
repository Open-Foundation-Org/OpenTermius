<div align="center">
  <img src="site/icon-256.png" alt="OpenTermius Logo" width="112" height="112" />
  <h1>OpenTermius</h1>
  <p><strong>Open-source, zero-paywall SSH &amp; SFTP client and self-hostable team gateway for macOS, Windows, and Android.</strong></p>

  <p>
    <a href="https://github.com/wisnu-w/OpenTermius/releases/latest">
      <img src="https://img.shields.io/github/v/release/wisnu-w/OpenTermius?style=for-the-badge&color=10b981" alt="Latest Release" />
    </a>
    <a href="LICENSE">
      <img src="https://img.shields.io/github/license/wisnu-w/OpenTermius?style=for-the-badge&color=3b82f6" alt="License" />
    </a>
    <img src="https://img.shields.io/badge/Platforms-macOS%20%7C%20Windows%20%7C%20Android%20%7C%20Linux-18181b?style=for-the-badge" alt="Platforms" />
  </p>

  <p>
    <a href="https://wisnu-w.github.io/OpenTermius/">Website</a> ·
    <a href="https://github.com/wisnu-w/OpenTermius/releases/latest">Download App</a> ·
    <a href="#self-host-gateway-1-command">Self-Host Gateway</a> ·
    <a href="docs/api.md">API Docs</a>
  </p>
</div>

---

## Features

- **Standalone or Gateway Mode** — Work offline with local encrypted storage, or connect to your self-hosted Gateway with automatic `Online · Gateway` / `Offline · Local` fallback.
- **Zero-Knowledge E2EE Vaults** — Hosts, SSH keys, identities, snippets, and tunnels are encrypted on-device with WebCrypto (`PBKDF2` + `AES-256-GCM`). The server never holds your vault keys.
- **Team Live SSH & Remote Relay** — See teammates' online presence and active SSH sessions in shared Team Vaults, request `View-Only` or `Control` access, stream PTY frames over a blind WebSocket Binary Relay, 1-click **Rejoin**, live participant avatars, typing indicators, and automatic close when the host stops sharing.
- **Built for Mobile & Desktop** — Native `russh` SSH/SFTP client, Android Foreground Service (`SshKeepAliveService`) so lock screen/minimize never drops sessions, Termux-style 2x7 Extra Keys, touch text selection, Focus Mode, and macOS/Windows/Linux desktop support via Tauri v2 with built-in 1-click **Desktop Auto-Updater** (`tauri-plugin-updater`).
- **Keychain, Snippets & Tunnels** — ED25519/RSA key generator, PEM import, dual-pane SFTP file manager, startup snippets, local/remote port forwarding, and session logs.

---

## Download Client

Grab the latest installers from **[GitHub Releases](https://github.com/wisnu-w/OpenTermius/releases/latest)**:

- **macOS (`aarch64` Apple Silicon):** `OpenTermius_X.Y.Z_aarch64.dmg`
- **Windows (`x64` NSIS Installer):** `opentermius_X.Y.Z_x64-setup.exe`
- **Android (Universal APK):** `app-universal-release.apk`

> [!NOTE]
> **macOS Gatekeeper Note (Un-notarized OSS build)**
> If macOS shows `"OpenTermius" is damaged and can't be opened` after downloading via browser, drag `OpenTermius.app` to `/Applications` and run once:
> ```bash
> xattr -cr /Applications/OpenTermius.app
> ```
>
> **Windows SmartScreen Note**
> If Windows Defender SmartScreen displays `Windows protected your PC` on first install, click **More info** &rarr; **Run anyway**.

---

## Self-Host Gateway (1-Command)

Install PostgreSQL 18+, Go 1.26+, Node.js 24 LTS, database migrations, Gateway API, and Gateway Web Admin Portal on **Ubuntu, Debian, Fedora, RHEL/Alma/Rocky, Arch, Alpine, or macOS**:

```bash
curl -fsSL https://raw.githubusercontent.com/wisnu-w/OpenTermius/main/scripts/install.sh | bash
```

- Detects existing PostgreSQL (native, Docker, or cloud like Supabase with `sslmode=require`) and busy ports (`8080`/`5174`) interactively.
- Pass an external DB directly if preferred:
  ```bash
  DATABASE_URL="postgres://user:pass@host:5432/opentermius?sslmode=require" curl -fsSL https://raw.githubusercontent.com/wisnu-w/OpenTermius/main/scripts/install.sh | bash
  ```
- **Default Gateway Admin login:** `admin@opentermius.local` / `OpenTermius2026!` (change after first login).
- **Client Gateway URL format:** `http://<your-server-ip>:<api-port>/api/v1` (or `https://<your-domain>/api/v1`).

---

## Workspace & Architecture

| Directory | Stack | Role |
| :--- | :--- | :--- |
| [`backend/`](backend/) | Go 1.26 (Clean / Hexagonal) | REST API, JWT auth, E2EE vault sync, Presence & WebSocket Binary Relay |
| [`client-app/`](client-app/) | Tauri v2 + Rust (`russh`) + Svelte 5 + Vite 8 | Multiplatform SSH/SFTP client for macOS, Windows, Android, Linux |
| [`gateway-web/`](gateway-web/) | Svelte 5 + Vite 8 + Tailwind CSS | Web admin portal for users, team vaults, live presence, and audit logs |
| [`migrations/`](migrations/) | PostgreSQL SQL | Versioned schema migrations |
| [`scripts/`](scripts/) | Bash | Cross-platform Gateway installer & service scripts |
| [`docs/`](docs/) | Markdown | Living architecture, database, API, and security docs |

---

## Building from Source

### Prerequisites
- Go 1.26+, Node.js 24 LTS+, PostgreSQL 18+ (or 15+)
- Rust (`rustup default stable`) for desktop/mobile client builds
- *(Windows cross-build on macOS/Linux)* `cargo-xwin`, `llvm` (`lld`), `nsis` (`makensis`)
- *(Android only)* Android SDK (`ANDROID_HOME`) & NDK (`NDK_HOME`)

### Manual Local Development
```bash
# 1. Configure environment & apply migrations
cp .env.example .env
psql "$DATABASE_URL" -f migrations/000001_init.up.sql

# 2. Run Gateway API (:8080)
cd backend && go run cmd/api/main.go

# 3. Run Gateway Web (:5174)
cd gateway-web && npm install && npm run dev

# 4. Run Desktop Client
cd client-app && npm install && npm run tauri:dev
```

### Build Client Installers
```bash
# macOS aarch64 DMG (+ updater .app.tar.gz & .sig)
cd client-app && ./scripts/build-dmg.sh

# Windows x64 NSIS Installer (.exe + .sig via cargo-xwin)
cd client-app && npx tauri build --runner cargo-xwin --target x86_64-pc-windows-msvc --bundles nsis

# Android Universal Release APK
cd client-app && ./scripts/build-apk.sh release
```

---

## Documentation & License

- [Architecture](docs/architecture.md) · [API Reference](docs/api.md) · [Database Schema](docs/database.md) · [Security Model](docs/security.md)
- Licensed under the [Apache-2.0 License](LICENSE).

## Privacy Policy

- **Privacy Policy:** [https://wisnu-w.github.io/OpenTermius/privacy.html](https://wisnu-w.github.io/OpenTermius/privacy.html)
- This program will not transfer any information to other networked systems unless specifically requested by the user or the person installing or operating it. OpenTermius operates in 100% offline Standalone Mode by default, or connects only to user-configured SSH servers and self-hosted Gateway URLs.
