# Project Context & AI Rules: OpenTermius

Open-source, self-hostable, zero-paywall alternative to Termius for desktop and web.
Includes E2EE Personal & Team Vaults, multi-device sync, session logs, startup snippets automation, and terminal multiplayer.

## 1. Tech Stack & Architecture
- **Backend:** Go 1.24 Clean Architecture (Hexagonal: `domain` -> `ports` -> `usecase` -> `adapters`).
- **Frontend:** Svelte 5 (Runes: `$state`, `$derived`, `$props`, `$effect`) + Tailwind CSS + shadcn-svelte.
- **Database:** PostgreSQL with versioned `.sql` migrations in `migrations/`. Client-side E2EE using WebCrypto (AES-256-GCM).
- **Communication:** REST API for resource management, WebSockets for interactive SSH PTY streaming & multiplayer terminal sharing.

## 2. Anti-Duplicate & Pre-Flight Check (STRICT)
Before adding ANY new function, endpoint, route, or UI component:
1. **Read `docs/api.md` first:** Check if the route or similar endpoint already exists.
2. **Search existing code:** Check existing symbols and handlers before writing new ones.
3. **Reuse or Extend:** If an endpoint/function exists that covers >70% of the need, refactor or extend it. NEVER create parallel/duplicate routes (e.g. creating `/api/v1/vaults/list` when `/api/v1/vaults` exists).

## 3. Documentation Maintenance (Definition of Done)
`docs/` is the living single source of truth. You are FORBIDDEN from finishing a task that changes code without updating documentation:
- **API Change:** When adding/modifying endpoints, update `docs/api.md`.
- **Database Change:** When adding/altering tables or indexes, update `docs/database.md`.
- **Architecture Change:** When introducing new layers or packages, update `docs/architecture.md`.
- **Architectural Decision:** When choosing a new library or structural pattern, add an ADR in `docs/decisions/`.

## 4. Production Discipline & Zero Dummy Data
- **Zero Dummy Data:** Never invent `const mockData = [...]` or return hardcoded mock responses in handlers (`gin.H{"dummy": ...}`) or in-memory slice DBs. Handlers call Services; Services call Repositories; Repositories query PostgreSQL.
- **All 4 UI States:** Frontend components must explicitly cover Loading, Error, Empty, and Success states.
- **Explicit SQL:** No `SELECT *` in backend queries; enumerate columns explicitly. Every Foreign Key column must have an explicit `CREATE INDEX idx_...`.
- **Svelte 5 Only:** Default to Svelte 5 Runes (`$state`, `$derived`, `$props`, `$effect`, snippets). Never use deprecated Svelte 4 syntax (`export let`, `$:`) unless explicitly asked.

## 5. Security-First Baseline (security-audit)
- **Concrete Trust Boundaries:** Public API vs Authenticated User vs Team Admin vs System. Every endpoint validates input strictly.
- **Zero Secrets:** Zero hardcoded secrets, tokens, or credentials in codebase or git. All secrets managed via `.env`.
- **Zero-Knowledge Encryption:** Vault secrets (private keys, passwords, connection configs) are encrypted on client-side before transmission. The server never holds vault master keys.
- **Defensive Safeguards:** Strict input validation, parameterized SQL queries (no string concatenation), rate limiting on authentication and terminal connection initiation.
- **Keep `docs/security.md` updated:** Maintain documented threat models, trust boundaries, and cipher configurations.

## 6. Release & Packaging Workflow (APK + DMG + GitHub Release)
When releasing new client-app changes, ALWAYS bump the version and publish a new GitHub Release:
0. **Auto-Determine SemVer Bump (`X.Y.Z`)** from changes since the last tag (`git log <last-tag>..HEAD`):
   - **MAJOR (`X+1.0.0`):** Breaking API/schema/encryption changes or incompatible protocol overhaul.
   - **MINOR (`X.Y+1.0`):** New user-facing features, capabilities, or new endpoints/views (e.g., Remote Relay, Presence, E2EE wiring).
   - **PATCH (`X.Y.Z+1`):** Bug fixes, UI/UX tweaks, performance/refactor, or small polish without new major features.
1. **Bump Version (`X.Y.Z`)** in:
   - `client-app/package.json`
   - `client-app/src-tauri/tauri.conf.json`
   - `client-app/src-tauri/Cargo.toml`
   - `client-app/scripts/build-dmg.sh` (`DMG_NAME`)
   - `client-app/src/App.svelte` (Settings version label)
   - `site/index.html` (version badge, CTA, download heading)
2. **Build Artifacts Locally (keep `CLOUD-AGENT-AI` runner for GitHub Pages only):**
   - Export Tauri updater signing key before desktop builds:
     `export TAURI_SIGNING_PRIVATE_KEY="$HOME/.tauri/opentermius.key"`
   - Android universal release APK: `cd client-app && ./scripts/build-apk.sh release`
   - macOS `aarch64` DMG (+ `.app.tar.gz` & `.sig`): `cd client-app && ./scripts/build-dmg.sh`
   - Windows x64 NSIS `.exe` (+ `.sig`):
     `export PATH="/opt/homebrew/opt/lld/bin:/opt/homebrew/opt/llvm/bin:$PATH" && cd client-app && npx tauri build --runner cargo-xwin --target x86_64-pc-windows-msvc --bundles nsis`
3. **Tag & Publish Release:**
   - Commit & push to `main`: `git commit -m "chore(release): bump version to X.Y.Z" && git tag vX.Y.Z && git push origin main vX.Y.Z`
   - Generate `latest.json` (with `darwin-aarch64` and `windows-x86_64` URLs + `.sig` signatures) and create GitHub Release with APK, DMG, EXE, updater bundles/sigs, and `latest.json`. ALWAYS include the macOS Gatekeeper Note (`xattr -cr /Applications/OpenTermius.app`) in `--notes`:
     ```bash
     gh release create vX.Y.Z \
       "client-app/src-tauri/gen/android/app/build/outputs/apk/universal/release/app-universal-release.apk#OpenTermius_X.Y.Z_android_universal.apk" \
       "client-app/src-tauri/target/release/bundle/dmg/OpenTermius_X.Y.Z_aarch64.dmg#OpenTermius_X.Y.Z_macOS_aarch64.dmg" \
       "client-app/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/opentermius_X.Y.Z_x64-setup.exe#OpenTermius_X.Y.Z_windows_x64-setup.exe" \
       --title "OpenTermius vX.Y.Z"
     ```
