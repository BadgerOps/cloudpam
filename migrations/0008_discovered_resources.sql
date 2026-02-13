-- 0008_discovered_resources.sql
-- Cloud discovery: discovered resources and sync jobs

CREATE TABLE IF NOT EXISTS discovered_resources (
    id               TEXT PRIMARY KEY,
    account_id       INTEGER NOT NULL REFERENCES accounts(id),
    provider         TEXT NOT NULL,
    region           TEXT NOT NULL DEFAULT '',
    resource_type    TEXT NOT NULL,
    resource_id      TEXT NOT NULL,
    name             TEXT NOT NULL DEFAULT '',
    cidr             TEXT NOT NULL DEFAULT '',
    parent_resource_id TEXT,
    pool_id          INTEGER REFERENCES pools(id) ON DELETE SET NULL,
    status           TEXT NOT NULL DEFAULT 'active',
    metadata         TEXT NOT NULL DEFAULT '{}',
    discovered_at    TEXT NOT NULL,
    last_seen_at     TEXT NOT NULL,
    UNIQUE(account_id, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_discovered_resources_account ON discovered_resources(account_id);
CREATE INDEX IF NOT EXISTS idx_discovered_resources_provider ON discovered_resources(provider);
CREATE INDEX IF NOT EXISTS idx_discovered_resources_status ON discovered_resources(status);
CREATE INDEX IF NOT EXISTS idx_discovered_resources_pool ON discovered_resources(pool_id);
CREATE INDEX IF NOT EXISTS idx_discovered_resources_type ON discovered_resources(resource_type);

CREATE TABLE IF NOT EXISTS sync_jobs (
    id                 TEXT PRIMARY KEY,
    account_id         INTEGER NOT NULL REFERENCES accounts(id),
    status             TEXT NOT NULL DEFAULT 'pending',
    started_at         TEXT,
    completed_at       TEXT,
    resources_found    INTEGER NOT NULL DEFAULT 0,
    resources_created  INTEGER NOT NULL DEFAULT 0,
    resources_updated  INTEGER NOT NULL DEFAULT 0,
    resources_deleted  INTEGER NOT NULL DEFAULT 0,
    error_message      TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sync_jobs_account ON sync_jobs(account_id);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_status ON sync_jobs(status);
