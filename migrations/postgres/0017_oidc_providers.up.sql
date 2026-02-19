-- Users table (matches SQLite migration 0007)
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT DEFAULT '',
    role TEXT NOT NULL DEFAULT 'viewer',
    password_hash BYTEA NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

-- Sessions table
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    metadata TEXT DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- Settings table (matches SQLite migration 0016)
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- OIDC providers table
CREATE TABLE IF NOT EXISTS oidc_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    issuer_url TEXT NOT NULL UNIQUE,
    client_id TEXT NOT NULL,
    client_secret_encrypted TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT 'openid profile email',
    role_mapping TEXT NOT NULL DEFAULT '{}',
    default_role TEXT NOT NULL DEFAULT 'viewer',
    auto_provision BOOLEAN NOT NULL DEFAULT TRUE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Add OIDC columns to users
ALTER TABLE users ADD COLUMN IF NOT EXISTS auth_provider TEXT NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_subject TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_issuer TEXT;

-- Seed default setting for local auth
INSERT INTO settings (key, value, updated_at)
VALUES ('local_auth_enabled', 'true', NOW())
ON CONFLICT (key) DO NOTHING;
