-- OpenTermius User Role Column (persistent Gateway Admin role)
-- Up Migration

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'user';

-- Promote existing default admin account to persistent 'admin' role
UPDATE users
SET role = 'admin'
WHERE LOWER(email) = 'admin@opentermius.local';

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
