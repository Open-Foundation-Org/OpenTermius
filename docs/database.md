# OpenTermius Database Specification

This document details the database schema, data types, relationships, indexes, and migration strategy for OpenTermius.

## 1. Relational Schema & Tables

### `users`
Represents an authenticated account.
| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `UUID` | `PRIMARY KEY DEFAULT gen_random_uuid()` | Unique user identifier |
| `email` | `VARCHAR(255)` | `NOT NULL UNIQUE` | Account email address |
| `password_hash` | `TEXT` | `NOT NULL` | Argon2id password hash |
| `name` | `VARCHAR(100)` | `NOT NULL` | Display name |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Account creation timestamp |
| `updated_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Last profile update timestamp |

### `vaults`
Represents an encrypted data container (Personal or Team).
| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `UUID` | `PRIMARY KEY DEFAULT gen_random_uuid()` | Unique vault ID |
| `owner_id` | `UUID` | `NOT NULL REFERENCES users(id) ON DELETE CASCADE` | Creator/Owner user ID |
| `name` | `VARCHAR(100)` | `NOT NULL` | Human-readable vault name |
| `type` | `VARCHAR(20)` | `NOT NULL CHECK (type IN ('personal', 'team'))` | Vault type |
| `encrypted_key` | `TEXT` | `NOT NULL` | Master key encrypted with owner's key |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Creation time |
| `updated_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Last updated time |

### `vault_members`
Association table for shared team vaults with RBAC permissions.
| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `UUID` | `PRIMARY KEY DEFAULT gen_random_uuid()` | Association ID |
| `vault_id` | `UUID` | `NOT NULL REFERENCES vaults(id) ON DELETE CASCADE` | Vault reference |
| `user_id` | `UUID` | `NOT NULL REFERENCES users(id) ON DELETE CASCADE` | Member reference |
| `role` | `VARCHAR(20)` | `NOT NULL CHECK (role IN ('owner', 'admin', 'member'))` | Access permission |
| `encrypted_vault_key`| `TEXT` | `NOT NULL` | Vault key wrapped with member public key |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Join timestamp |

### `vault_items`
Encrypted records stored within a vault (Hosts, Keys, Identities, Snippets, Tunnels).
| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `UUID` | `PRIMARY KEY DEFAULT gen_random_uuid()` | Item ID |
| `vault_id` | `UUID` | `NOT NULL REFERENCES vaults(id) ON DELETE CASCADE` | Parent vault reference |
| `kind` | `VARCHAR(30)` | `NOT NULL CHECK (kind IN ('host', 'key', 'identity', 'snippet', 'tunnel'))`| Entity type |
| `label` | `VARCHAR(150)` | `NOT NULL` | Plaintext search label / tag |
| `ciphertext` | `TEXT` | `NOT NULL` | AES-GCM encrypted payload JSON |
| `nonce` | `VARCHAR(64)` | `NOT NULL` | Initialization vector (base64) |
| `version` | `BIGINT` | `NOT NULL DEFAULT 1` | Optimistic locking counter |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Creation time |
| `updated_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Last update time |

### `session_logs`
Searchable terminal session history and audit log.
| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `UUID` | `PRIMARY KEY DEFAULT gen_random_uuid()` | Session log ID |
| `vault_id` | `UUID` | `NOT NULL REFERENCES vaults(id) ON DELETE CASCADE` | Associated vault ID |
| `user_id` | `UUID` | `NOT NULL REFERENCES users(id) ON DELETE CASCADE` | Session creator ID |
| `host_label` | `VARCHAR(150)` | `NOT NULL` | Host label connected to |
| `started_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Sesi start time |
| `ended_at` | `TIMESTAMPTZ` | `NULL` | Sesi finish time |
| `recorded_data` | `TEXT` | `NOT NULL` | Compressed asciinema/raw session bytes |

### `smtp_settings` (Migration `000002_smtp_and_invites`)
Singleton table (`id = 1`) storing Gateway SMTP settings configurable from `gateway-web`.

| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `SMALLINT` | `PRIMARY KEY DEFAULT 1 CHECK (id = 1)` | Singleton row guard |
| `enabled` | `BOOLEAN` | `NOT NULL DEFAULT FALSE` | Enables outbound email invites & notifications |
| `host` | `VARCHAR(255)` | `NOT NULL DEFAULT ''` | SMTP server hostname |
| `port` | `INTEGER` | `NOT NULL DEFAULT 587` | SMTP port (587 STARTTLS / 465 TLS) |
| `username` | `VARCHAR(255)` | `NOT NULL DEFAULT ''` | SMTP auth username |
| `password_enc` | `TEXT` | `NOT NULL DEFAULT ''` | AES-256-GCM encrypted SMTP password |
| `from_name` | `VARCHAR(150)` | `NOT NULL DEFAULT 'OpenTermius Gateway'` | Sender display name |
| `from_email` | `VARCHAR(255)` | `NOT NULL DEFAULT ''` | Sender email address |
| `use_tls` | `BOOLEAN` | `NOT NULL DEFAULT TRUE` | Use TLS/STARTTLS |
| `web_base_url` | `VARCHAR(500)` | `NOT NULL DEFAULT ''` | Public Gateway Web URL for invite links |
| `updated_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Last update timestamp |

### `user_invites` (Migration `000002_smtp_and_invites`)
One-time user onboarding invites so invited teammates set their own password.

| Column | Type | Constraints | Description |
|---|---|---|---|
| `id` | `UUID` | `PRIMARY KEY DEFAULT gen_random_uuid()` | Invite ID |
| `email` | `VARCHAR(255)` | `NOT NULL` | Invited user's email |
| `name` | `VARCHAR(100)` | `NOT NULL` | Invited user's display name |
| `token_hash` | `VARCHAR(64)` | `NOT NULL UNIQUE` | SHA-256 hash of raw 32-byte invite token |
| `invited_by` | `UUID` | `NOT NULL REFERENCES users(id) ON DELETE CASCADE` | Admin user ID who sent invite |
| `expires_at` | `TIMESTAMPTZ` | `NOT NULL` | Invite expiry (24h) |
| `used_at` | `TIMESTAMPTZ` | `NULL` | Timestamp when user completed signup |
| `created_at` | `TIMESTAMPTZ` | `NOT NULL DEFAULT CURRENT_TIMESTAMP` | Creation timestamp |

---

## 2. Index Catalog & Rules

Every foreign key has an explicit index for query performance and cascade safety:
- `idx_vaults_owner_id` ON `vaults(owner_id)`: Quick retrieval of all vaults owned by a user.
- `idx_vault_members_vault_id` ON `vault_members(vault_id)`: Listing members of a vault.
- `idx_vault_members_user_id` ON `vault_members(user_id)`: Listing vaults a user participates in.
- `idx_vault_members_unique` ON `vault_members(vault_id, user_id)`: Enforces unique membership per vault.
- `idx_vault_items_vault_id` ON `vault_items(vault_id)`: Fetching all encrypted items in a vault.
- `idx_vault_items_kind` ON `vault_items(vault_id, kind)`: Filtering items by type (e.g. hosts only).
- `idx_session_logs_vault_id` ON `session_logs(vault_id)`: Listing session logs by vault.
- `idx_session_logs_user_id` ON `session_logs(user_id)`: Listing logs generated by a user.
- `idx_user_invites_invited_by` ON `user_invites(invited_by)`: FK index for admin creator lookup/cascade.
- `idx_user_invites_email` ON `user_invites(email)`: Lookup pending invites by email.
- `idx_user_invites_expires_at` ON `user_invites(expires_at)`: Fast filtering of unexpired pending invites.
- `idx_users_role` ON `users(role)` (Migration `000003_user_role`): Persistent `role` (`admin` / `user`) on `users` so admin email can be updated safely without losing privileges.

---

## 3. Migration Instructions

Migration files are located in `migrations/`:
- Up migrations: `migrations/*.up.sql`
- Down migrations: `migrations/*.down.sql`

To apply:
```bash
psql $DATABASE_URL -f migrations/000001_init.up.sql
```
To rollback:
```bash
psql $DATABASE_URL -f migrations/000001_init.down.sql
```
