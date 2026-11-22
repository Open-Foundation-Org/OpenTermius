-- OpenTermius SMTP Settings & Email User Invites
-- Up Migration

-- 1. Singleton SMTP Settings Table (managed from Gateway Web Settings)
CREATE TABLE IF NOT EXISTS smtp_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    host VARCHAR(255) NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 587,
    username VARCHAR(255) NOT NULL DEFAULT '',
    password_enc TEXT NOT NULL DEFAULT '',
    from_name VARCHAR(150) NOT NULL DEFAULT 'OpenTermius Gateway',
    from_email VARCHAR(255) NOT NULL DEFAULT '',
    use_tls BOOLEAN NOT NULL DEFAULT TRUE,
    web_base_url VARCHAR(500) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 2. User Invites Table (one-time SHA-256 hashed tokens so invited users set their own password)
CREATE TABLE IF NOT EXISTS user_invites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL,
    name VARCHAR(100) NOT NULL,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    invited_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_user_invites_invited_by ON user_invites(invited_by);
CREATE INDEX IF NOT EXISTS idx_user_invites_email ON user_invites(email);
CREATE INDEX IF NOT EXISTS idx_user_invites_expires_at ON user_invites(expires_at);
