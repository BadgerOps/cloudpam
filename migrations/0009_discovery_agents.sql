-- Migration: Discovery Agents
-- Description: Add discovery_agents table and extend sync_jobs with source tracking

-- Discovery agents table
CREATE TABLE IF NOT EXISTS discovery_agents (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    api_key_id    TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    version       TEXT NOT NULL DEFAULT '',
    hostname      TEXT NOT NULL DEFAULT '',
    last_seen_at  TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_discovery_agents_account ON discovery_agents(account_id);
CREATE INDEX IF NOT EXISTS idx_discovery_agents_api_key ON discovery_agents(api_key_id);
CREATE INDEX IF NOT EXISTS idx_discovery_agents_last_seen ON discovery_agents(last_seen_at);

-- Add source tracking to sync_jobs
ALTER TABLE sync_jobs ADD COLUMN source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE sync_jobs ADD COLUMN agent_id TEXT REFERENCES discovery_agents(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_sync_jobs_source ON sync_jobs(source);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_agent ON sync_jobs(agent_id);
