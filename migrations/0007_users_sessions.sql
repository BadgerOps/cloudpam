-- Local users table
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT DEFAULT '',
    role TEXT NOT NULL DEFAULT 'viewer',
    password_hash BLOB NOT NULL,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_login_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

-- Sessions table
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    metadata TEXT DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- Link API keys to users (nullable = bot/standalone key)
ALTER TABLE api_keys ADD COLUMN owner_id TEXT REFERENCES users(id) ON DELETE SET NULL;
