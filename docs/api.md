# OpenTermius API Specification

This document serves as the single source of truth for all OpenTermius Gateway REST API endpoints and Tauri native IPC commands.

## Base URL & Authentication
- Gateway REST Base URL: `http://localhost:8080/api/v1`
- Auth Header: `Authorization: Bearer <jwt_token>`
- **SSH & SFTP Transport:** 100% direct from the client device via Tauri Native IPC (`russh` + `russh-sftp` + `portable-pty`). The Gateway server never proxies SSH or SFTP connections and never receives plaintext credentials.

---

## Gateway REST Endpoint Catalog

| Method | Path | Summary | Auth Required |
|---|---|---|---|
| `POST` | `/api/v1/auth/register` | Register new user account (Gateway Web Console) | No |
| `POST` | `/api/v1/auth/login` | Login and receive JWT access + refresh token | No |
| `POST` | `/api/v1/auth/refresh` | Exchange valid refresh token for a fresh access + refresh token pair | No |
| `GET` | `/api/v1/auth/me` | Fetch authenticated user profile | Yes |
| `GET` | `/api/v1/users` | List all registered users (Gateway Admin Console) | Yes |
| `POST` | `/api/v1/users` | Create/register a new user account (Gateway Admin Console) | Yes |
| `DELETE`| `/api/v1/users/:id` | Delete a user account (Gateway Admin Console) | Yes |
| `GET` | `/api/v1/vaults` | List accessible personal & team vaults | Yes |
| `POST` | `/api/v1/vaults` | Create a new personal or team vault | Yes |
| `GET` | `/api/v1/vaults/:id` | Get vault details & wrapped keys | Yes |
| `GET` | `/api/v1/vaults/:id/items` | List encrypted items in vault | Yes |
| `POST` | `/api/v1/vaults/:id/items` | Add encrypted item to vault | Yes |
| `PUT` | `/api/v1/vaults/:id/items/:itemId` | Update encrypted item | Yes |
| `PUT` | `/api/v1/vaults/:id/items/:itemId/move` | Move item to another vault | Yes |
| `DELETE`| `/api/v1/vaults/:id/items/:itemId` | Delete item from vault | Yes |
| `GET` | `/api/v1/vaults/:id/members` | List members of a team vault | Yes |
| `POST` | `/api/v1/vaults/:id/members` | Invite a teammate to vault by email (sends SMTP notification if enabled) | Yes |
| `GET` | `/api/v1/settings/smtp` | Read Gateway SMTP settings (password masked) | Admin |
| `PUT` | `/api/v1/settings/smtp` | Save Gateway SMTP settings in PostgreSQL (password encrypted at rest) | Admin |
| `POST` | `/api/v1/settings/smtp/test` | Send a test email using saved SMTP settings | Admin |
| `GET` | `/api/v1/invites` | List pending (unused, unexpired) email user invites | Admin |
| `POST` | `/api/v1/invites` | Create one-time user invite link & send via SMTP | Admin |
| `DELETE` | `/api/v1/invites/:id` | Revoke a pending user invite | Admin |
| `GET` | `/api/v1/invites/verify` | Verify one-time invite token (`?token=...`) before password setup | No (rate-limited) |
| `POST` | `/api/v1/invites/accept` | Accept invite & set user's own password | No (rate-limited) |

---

## Native Tauri IPC Commands (`client-app/src-tauri`)

| Command | Summary |
|---|---|
| `terminal_open` | Open native SSH PTY (`russh`) or Local Shell PTY (`portable-pty`) |
| `terminal_write` | Write user keystrokes/bytes to active SSH or Local PTY session |
| `terminal_resize` | Resize active SSH channel window or Local PTY dimensions |
| `terminal_close` | Close and disconnect active SSH or Local PTY session |
| `sftp_list` | List directory entries on remote host via native `russh-sftp` |
| `sftp_mkdir` | Create directory on remote host via native `russh-sftp` |
| `sftp_delete` | Delete file or directory on remote host via native `russh-sftp` |
| `sftp_read_bytes` | Read/download file bytes from remote host via native `russh-sftp` |
| `sftp_write_bytes` | Write/upload file bytes to remote host via native `russh-sftp` |
| `local_fs_list` | List local device filesystem directory |
| `local_fs_mkdir` | Create directory on local device filesystem |
| `local_fs_delete` | Delete file or directory on local device filesystem |
| `local_fs_read_bytes` | Read file bytes from local device filesystem |
| `local_fs_write_bytes` | Write file bytes to local device filesystem |

---

## Detailed Request & Response Schemas

### 1. `POST /api/v1/auth/register`
**Request Body**:
```json
{
  "email": "user@example.com",
  "password": "SecurePassword123!",
  "name": "Jane Doe"
}
```
**Response (201 Created)**:
```json
{
  "user": {
    "id": "8a004dbc-b8c1-45c8-9cc6-32189cd9d327",
    "email": "user@example.com",
    "name": "Jane Doe",
    "created_at": "2026-11-12T11:00:00Z"
  },
  "token": "eyJhbGciOi..."
}
```

### 2. `POST /api/v1/auth/login`
**Request Body**:
```json
{
  "email": "user@example.com",
  "password": "SecurePassword123!"
}
```
**Response (200 OK)**:
```json
{
  "user": {
    "id": "8a004dbc-b8c1-45c8-9cc6-32189cd9d327",
    "email": "user@example.com",
    "name": "Jane Doe"
  },
  "token": "eyJhbGciOi..."
}
```

### 3. `POST /api/v1/vaults/:id/items`
**Request Body**:
```json
{
  "kind": "host",
  "label": "Production Web Server",
  "ciphertext": "eyJpdiI6IlpB...",
  "nonce": "aW5pdGlhbGl6YXRpb252ZWN0b3I="
}
```
**Response (201 Created)**:
```json
{
  "id": "b3e04746-fc93-4a0d-b4cb-7dca551e7f60",
  "vault_id": "8a004dbc-b8c1-45c8-9cc6-32189cd9d327",
  "kind": "host",
  "label": "Production Web Server",
  "ciphertext": "eyJpdiI6IlpB...",
  "nonce": "aW5pdGlhbGl6YXRpb252ZWN0b3I=",
  "version": 1,
  "created_at": "2026-11-12T11:05:00Z",
  "updated_at": "2026-11-12T11:05:00Z"
}
```

### 4. Presence & Remote Collaboration Relay (Zero-SSH Blind Relay)
| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/presence/heartbeat` | Report device online status & active native SSH sessions; returns pending remote requests |
| `GET` | `/api/v1/presence?vault_id=...` | List online devices, active SSH sessions, connected relay avatars (`viewers`), and caller's reusable `approved_request_id` |
| `POST` | `/api/v1/sessions/:sessionId/request` | Request remote view/control access (auto-reuses existing approval for the same session/user) |
| `GET` | `/api/v1/sessions/requests/:requestId` | Poll approval status of an outbound remote access request |
| `POST` | `/api/v1/sessions/requests/:requestId/respond` | Approve (`view` or `control`) or reject an incoming remote access request |
| `GET` | `/api/v1/relay/:sessionId?token=...&role=host\|viewer&request_id=...` | WebSocket Binary Relay forwarding PTY frames + `relay_state` participant avatars & throttled `typing` indicators |


