-- Migration 0023: recommendations table (postgres parity with migrations/0011_recommendations.sql)

CREATE TABLE IF NOT EXISTS recommendations (
    id              TEXT PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    pool_id         BIGINT NOT NULL REFERENCES pools(seq_id) ON DELETE CASCADE,
    type            VARCHAR(50) NOT NULL CHECK (type IN ('allocation','compliance','consolidation','resize','reclaim')),
    status          VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','applied','dismissed')),
    priority        VARCHAR(20) NOT NULL DEFAULT 'medium' CHECK (priority IN ('high','medium','low')),
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    suggested_cidr  TEXT NOT NULL DEFAULT '',
    rule_id         TEXT NOT NULL DEFAULT '',
    score           INTEGER NOT NULL DEFAULT 0,
    metadata        JSONB NOT NULL DEFAULT '{}',
    dismiss_reason  TEXT NOT NULL DEFAULT '',
    applied_pool_id BIGINT REFERENCES pools(seq_id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_recommendations_org_pool_status
    ON recommendations (organization_id, pool_id, status);

CREATE INDEX IF NOT EXISTS idx_recommendations_org_created
    ON recommendations (organization_id, created_at DESC, id DESC);
