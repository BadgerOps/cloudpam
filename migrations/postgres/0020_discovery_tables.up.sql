-- CloudPAM PostgreSQL Discovery Schema
-- Migration 0020: discovered resources, sync jobs, and discovery agents

CREATE TABLE IF NOT EXISTS discovered_resources (
    id                 UUID PRIMARY KEY,
    organization_id    UUID NOT NULL REFERENCES organizations(id),
    account_id         BIGINT NOT NULL REFERENCES accounts(seq_id) ON DELETE CASCADE,
    provider           VARCHAR(50) NOT NULL,
    region             VARCHAR(100) NOT NULL DEFAULT '',
    resource_type      VARCHAR(50) NOT NULL,
    resource_id        TEXT NOT NULL,
    name               TEXT NOT NULL DEFAULT '',
    cidr               TEXT NOT NULL DEFAULT '',
    parent_resource_id TEXT,
    pool_id            BIGINT REFERENCES pools(seq_id) ON DELETE SET NULL,
    status             VARCHAR(20) NOT NULL DEFAULT 'active',
    metadata           JSONB NOT NULL DEFAULT '{}',
    discovered_at      TIMESTAMPTZ NOT NULL,
    last_seen_at       TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, account_id, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_discovered_resources_org_account ON discovered_resources(organization_id, account_id);
CREATE INDEX IF NOT EXISTS idx_discovered_resources_provider ON discovered_resources(provider);
CREATE INDEX IF NOT EXISTS idx_discovered_resources_status ON discovered_resources(status);
CREATE INDEX IF NOT EXISTS idx_discovered_resources_pool ON discovered_resources(pool_id);
CREATE INDEX IF NOT EXISTS idx_discovered_resources_type ON discovered_resources(resource_type);

CREATE TABLE IF NOT EXISTS discovery_agents (
    id              UUID PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    name            TEXT NOT NULL,
    account_id      BIGINT NOT NULL,
    api_key_id      TEXT NOT NULL,
    version         TEXT NOT NULL DEFAULT '',
    hostname        TEXT NOT NULL DEFAULT '',
    last_seen_at    TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    status          VARCHAR(32) NOT NULL DEFAULT 'approved',
    registered_at   TIMESTAMPTZ,
    approved_at     TIMESTAMPTZ,
    approved_by     TEXT
);

CREATE INDEX IF NOT EXISTS idx_discovery_agents_org_account ON discovery_agents(organization_id, account_id);
CREATE INDEX IF NOT EXISTS idx_discovery_agents_api_key ON discovery_agents(api_key_id);
CREATE INDEX IF NOT EXISTS idx_discovery_agents_last_seen ON discovery_agents(last_seen_at);
CREATE INDEX IF NOT EXISTS idx_discovery_agents_status ON discovery_agents(status);

CREATE TABLE IF NOT EXISTS sync_jobs (
    id                UUID PRIMARY KEY,
    organization_id   UUID NOT NULL REFERENCES organizations(id),
    account_id        BIGINT NOT NULL REFERENCES accounts(seq_id) ON DELETE CASCADE,
    status            VARCHAR(20) NOT NULL DEFAULT 'pending',
    source            VARCHAR(20) NOT NULL DEFAULT 'local',
    agent_id          UUID REFERENCES discovery_agents(id) ON DELETE SET NULL,
    started_at        TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    resources_found   INTEGER NOT NULL DEFAULT 0,
    resources_created INTEGER NOT NULL DEFAULT 0,
    resources_updated INTEGER NOT NULL DEFAULT 0,
    resources_deleted INTEGER NOT NULL DEFAULT 0,
    error_message     TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sync_jobs_org_account ON sync_jobs(organization_id, account_id);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_status ON sync_jobs(status);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_source ON sync_jobs(source);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_agent ON sync_jobs(agent_id);
