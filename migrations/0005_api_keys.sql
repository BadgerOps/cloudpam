-- API keys table for persistent authentication
-- Stores Argon2id-hashed API keys with metadata

CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    prefix TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    hash BLOB NOT NULL,
    salt BLOB NOT NULL,
    scopes TEXT NOT NULL DEFAULT '[]',  -- JSON array of scope strings
    created_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    expires_at TIMESTAMP,
    last_used_at TIMESTAMP,
    revoked INTEGER NOT NULL DEFAULT 0
);

-- Index for fast prefix lookups during authentication
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);
