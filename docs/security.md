# OpenTermius Security & Trust Model

This document outlines the threat model, cryptography standards, trust boundaries, and authorization policies of OpenTermius.

## 1. Concrete Trust Boundaries

```
[ Untrusted Client Device ]
           │
           │ TLS 1.3 + JWT Auth
           ▼
┌────────────────────────────────────────────────────────┐
│ Boundary 1: Perimeter API Gateway / Reverse Proxy     │
│ - Strict TLS enforcement                               │
│ - Rate limiting (10 req/s login, 100 req/s API)        │
│ - Input validation & request size limits (max 5 MB)    │
└──────────────────────────┬─────────────────────────────┘
                           │
                           ▼
┌────────────────────────────────────────────────────────┐
│ Boundary 2: Application Layer (Go Backend)             │
│ - JWT validation & Claims parsing                      │
│ - Role-Based Access Control (RBAC: Owner/Admin/Member) │
│ - ZERO-KNOWLEDGE: Stores ciphertext blobs only         │
│ - Parameterized SQL (pgx) with zero raw string queries │
└──────────────────────────┬─────────────────────────────┘
                           │
                           ▼
┌────────────────────────────────────────────────────────┐
│ Boundary 3: Database Layer (PostgreSQL)                │
│ - Dedicated non-superuser DB user                      │
│ - Foreign Key integrity + cascade deletion rules       │
│ - Encrypted-at-rest volumes                            │
└────────────────────────────────────────────────────────┘
```

## 2. Zero-Knowledge End-to-End Encryption (E2EE)

### Cryptographic Primitive Selection
- **Symmetric Encryption:** AES-256-GCM (Galois/Counter Mode) with a unique 96-bit (12-byte) initialization vector (IV / nonce) per item.
- **Key Derivation (KDF):** Argon2id (`time=3`, `memory=64MB`, `parallelism=4`) derived from user master password with unique user salt.
- **Key Wrapping:** Team Vault Master Keys are encrypted using AES-KeyWrap (or RSA-OAEP / ECDH P-256) using each authorized member's public key.

### Zero-Knowledge Guarantee
The server never receives:
1. The plaintext master password.
2. The decrypted vault keys.
3. The plaintext hostnames, IP addresses, credentials, private SSH keys, or passwords.
All encryption and decryption occurs in client memory using WebCrypto APIs (`window.crypto.subtle`) or desktop native keyrings (`keytar`).

## 3. Terminal & SSH Security

- **Direct vs Relayed Connection:** By default, OpenTermius client connects directly to target SSH servers whenever the target is network-routable.
- **WebSocket Gateway:** When proxying via the self-hosted backend, the connection utilizes Go's `golang.org/x/crypto/ssh` implementation with strict host key verification.
- **No Command Injection:** SSH command invocation and shell allocation strictly use pseudo-terminal handles (`node-pty` / Go PTY) without string concatenation through `/bin/sh -c`.
- **Session Multiplayer Authorization:** Only authorized vault members can join shared live terminal sessions (`/ws/sessions/:id/join`). The host can terminate live multiplayer sharing at any time.

## 4. Anti-Vulnerability Verification Checklist

- [x] **No SQL Injection:** All queries use `$1, $2` parameters via `pgx`.
- [x] **No IDOR / Presence Leak / Relay Hijack:** Every vault item and presence query filters strictly by caller membership (`vault_members`) or ownership, Team Vault `member` role is prevented from deleting/moving shared items or escalating roles, and WebSocket `RelayWS` verifies session ownership before allowing `role=host`.
- [x] **Cryptographic Vault Keys & Optional Client Master Passphrase:** New vaults generate 256-bit CSPRNG keys (`crypto/rand` & `crypto.getRandomValues`) and support local-only client Master Passphrase derivation (`opentermius_master_passphrase`).
- [x] **CSRF / CORS & Content-Security-Policy:** Explicit origin validation (`CORS_ALLOWED_ORIGINS`) for REST and WebSocket relay, plus strict WebView CSP in `tauri.conf.json`.
- [x] **Anti-Spoof Rate Limiting, Request Size Caps & Fail-Closed STARTTLS:** `clientIP` trusts forwarding headers only from loopback/private proxies, request bodies (`4 MiB`) and WebSocket frames (`1 MiB`) are bounded, invite redemption is atomic (`WHERE used_at IS NULL`), and `SMTPMailer` refuses STARTTLS downgrade when `use_tls` is enabled.
- [x] **SSH Host Key Verification (TOFU):** Native Rust `russh` client verifies and persists server host key fingerprints (`.opentermius_known_hosts.json` across macOS, Linux, Windows, and Android) on first connection, distinguishes network errors from `HOST_KEY_MISMATCH`, and requires explicit user action (`ssh_trust_host_key` / "Trust New Key") to rotate a changed server key.
- [x] **Zero-Knowledge E2EE WebSocket Relay & Ephemeral Tickets:** Terminal multiplayer frames are encrypted end-to-end on client devices (`AES-256-GCM` binary frames `[2B magic "OT"][12B IV][ciphertext+16B tag]` using the shared Team Vault key and sequential promise queues), authenticated via 20-second single-use tickets (`POST /api/v1/relay/:sessionId/ticket`), and isolated with per-viewer non-blocking buffered channels (`cap 64`) + `Ping`/`Pong` read deadlines.
