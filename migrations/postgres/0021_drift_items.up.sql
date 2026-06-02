-- CloudPAM PostgreSQL Drift Schema
-- Migration 0021: drift detection results and network conflict resolution records

CREATE TABLE IF NOT EXISTS drift_items (
    id              TEXT PRIMARY KEY,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id      BIGINT NOT NULL REFERENCES accounts(seq_id) ON DELETE CASCADE,
    resource_id     UUID,
    pool_id         BIGINT REFERENCES pools(seq_id) ON DELETE SET NULL,
    type            VARCHAR(50) NOT NULL CHECK (type IN ('unmanaged','cidr_mismatch','orphaned_pool','name_mismatch','account_drift')),
    severity        VARCHAR(20) NOT NULL DEFAULT 'warning' CHECK (severity IN ('critical','warning','info')),
    status          VARCHAR(20) NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','ignored')),
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    resource_cidr   TEXT NOT NULL DEFAULT '',
    pool_cidr       TEXT NOT NULL DEFAULT '',
    details         JSONB NOT NULL DEFAULT '{}',
    ignore_reason   TEXT,
    resolved_at     TIMESTAMPTZ,
    detected_at     TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_drift_items_org_account ON drift_items(organization_id, account_id);
CREATE INDEX IF NOT EXISTS idx_drift_items_status ON drift_items(status);
CREATE INDEX IF NOT EXISTS idx_drift_items_type ON drift_items(type);
CREATE INDEX IF NOT EXISTS idx_drift_items_severity ON drift_items(severity);
