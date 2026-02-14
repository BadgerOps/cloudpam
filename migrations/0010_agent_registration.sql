-- Migration: Agent Registration & Bootstrap Tokens
-- Description: Add agent approval workflow and bootstrap token system for auto-registration

-- Add status column to discovery_agents
ALTER TABLE discovery_agents ADD COLUMN status TEXT NOT NULL DEFAULT 'approved';
-- status: pending_approval, approved, rejected

-- Create bootstrap_tokens table for agent registration
CREATE TABLE IF NOT EXISTS bootstrap_tokens (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    token_hash    BLOB NOT NULL,
    account_id    INTEGER REFERENCES accounts(id) ON DELETE CASCADE,
    created_by    TEXT NOT NULL,
    expires_at    TEXT,
    revoked       INTEGER NOT NULL DEFAULT 0,
    used_count    INTEGER NOT NULL DEFAULT 0,
    max_uses      INTEGER,
    created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_bootstrap_tokens_account ON bootstrap_tokens(account_id);
CREATE INDEX IF NOT EXISTS idx_bootstrap_tokens_revoked ON bootstrap_tokens(revoked);
CREATE INDEX IF NOT EXISTS idx_bootstrap_tokens_expires ON bootstrap_tokens(expires_at);

-- Add metadata columns to discovery_agents for tracking
ALTER TABLE discovery_agents ADD COLUMN bootstrap_token_id TEXT REFERENCES bootstrap_tokens(id) ON DELETE SET NULL;
ALTER TABLE discovery_agents ADD COLUMN registered_at TEXT;
ALTER TABLE discovery_agents ADD COLUMN approved_at TEXT;
ALTER TABLE discovery_agents ADD COLUMN approved_by TEXT;

CREATE INDEX IF NOT EXISTS idx_discovery_agents_status ON discovery_agents(status);
CREATE INDEX IF NOT EXISTS idx_discovery_agents_bootstrap_token ON discovery_agents(bootstrap_token_id);
