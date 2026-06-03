-- Durable managed network objects and explicit network relationships.

CREATE TABLE IF NOT EXISTS network_objects (
    id                     BIGSERIAL PRIMARY KEY,
    organization_id        UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    object_type            TEXT NOT NULL,
    provider               TEXT NOT NULL DEFAULT '',
    account_id             BIGINT NOT NULL REFERENCES accounts(seq_id) ON DELETE CASCADE,
    region                 TEXT NOT NULL DEFAULT '',
    name                   TEXT NOT NULL,
    cidr                   TEXT NOT NULL DEFAULT '',
    ip_address             TEXT NOT NULL DEFAULT '',
    provider_resource_id   TEXT NOT NULL DEFAULT '',
    parent_object_id       BIGINT REFERENCES network_objects(id) ON DELETE SET NULL,
    pool_id                BIGINT REFERENCES pools(seq_id) ON DELETE SET NULL,
    source_discovered_id   UUID,
    state                  TEXT NOT NULL DEFAULT 'managed',
    metadata               JSONB NOT NULL DEFAULT '{}',
    created_at             TIMESTAMPTZ NOT NULL,
    updated_at             TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_network_objects_org_account ON network_objects(organization_id, account_id);
CREATE INDEX IF NOT EXISTS idx_network_objects_provider ON network_objects(provider);
CREATE INDEX IF NOT EXISTS idx_network_objects_type ON network_objects(object_type);
CREATE INDEX IF NOT EXISTS idx_network_objects_state ON network_objects(state);
CREATE INDEX IF NOT EXISTS idx_network_objects_pool ON network_objects(pool_id);

CREATE TABLE IF NOT EXISTS network_relationships (
    id               TEXT NOT NULL,
    organization_id  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    type             TEXT NOT NULL,
    source_kind      TEXT NOT NULL,
    source_id        TEXT NOT NULL,
    target_kind      TEXT NOT NULL,
    target_id        TEXT NOT NULL,
    confidence       DOUBLE PRECISION NOT NULL DEFAULT 1,
    reason           TEXT NOT NULL DEFAULT '',
    evidence         JSONB NOT NULL DEFAULT '[]',
    resolution_state TEXT NOT NULL DEFAULT 'open',
    created_at       TIMESTAMPTZ NOT NULL,
    updated_at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_network_relationships_org_type ON network_relationships(organization_id, type);
CREATE INDEX IF NOT EXISTS idx_network_relationships_source ON network_relationships(organization_id, source_kind, source_id);
CREATE INDEX IF NOT EXISTS idx_network_relationships_target ON network_relationships(organization_id, target_kind, target_id);
CREATE INDEX IF NOT EXISTS idx_network_relationships_state ON network_relationships(organization_id, resolution_state);
