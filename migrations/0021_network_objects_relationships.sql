-- Durable managed network objects and explicit network relationships.

CREATE TABLE IF NOT EXISTS network_objects (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    object_type            TEXT NOT NULL,
    provider               TEXT NOT NULL DEFAULT '',
    account_id             INTEGER NOT NULL REFERENCES accounts(id),
    region                 TEXT NOT NULL DEFAULT '',
    name                   TEXT NOT NULL,
    cidr                   TEXT NOT NULL DEFAULT '',
    ip_address             TEXT NOT NULL DEFAULT '',
    provider_resource_id   TEXT NOT NULL DEFAULT '',
    parent_object_id       INTEGER REFERENCES network_objects(id) ON DELETE SET NULL,
    pool_id                INTEGER REFERENCES pools(id) ON DELETE SET NULL,
    source_discovered_id   TEXT REFERENCES discovered_resources(id) ON DELETE SET NULL,
    state                  TEXT NOT NULL DEFAULT 'managed',
    metadata               TEXT NOT NULL DEFAULT '{}',
    created_at             TEXT NOT NULL,
    updated_at             TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_network_objects_account ON network_objects(account_id);
CREATE INDEX IF NOT EXISTS idx_network_objects_provider ON network_objects(provider);
CREATE INDEX IF NOT EXISTS idx_network_objects_type ON network_objects(object_type);
CREATE INDEX IF NOT EXISTS idx_network_objects_state ON network_objects(state);
CREATE INDEX IF NOT EXISTS idx_network_objects_pool ON network_objects(pool_id);

CREATE TABLE IF NOT EXISTS network_relationships (
    id               TEXT PRIMARY KEY,
    type             TEXT NOT NULL,
    source_kind      TEXT NOT NULL,
    source_id        TEXT NOT NULL,
    target_kind      TEXT NOT NULL,
    target_id        TEXT NOT NULL,
    confidence       REAL NOT NULL DEFAULT 1,
    reason           TEXT NOT NULL DEFAULT '',
    evidence         TEXT NOT NULL DEFAULT '[]',
    resolution_state TEXT NOT NULL DEFAULT 'open',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_network_relationships_type ON network_relationships(type);
CREATE INDEX IF NOT EXISTS idx_network_relationships_source ON network_relationships(source_kind, source_id);
CREATE INDEX IF NOT EXISTS idx_network_relationships_target ON network_relationships(target_kind, target_id);
CREATE INDEX IF NOT EXISTS idx_network_relationships_state ON network_relationships(resolution_state);
