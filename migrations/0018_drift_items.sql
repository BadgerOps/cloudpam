-- Drift detection results: track mismatches between discovered cloud resources and managed IPAM pools.
CREATE TABLE IF NOT EXISTS drift_items (
    id            TEXT PRIMARY KEY,
    account_id    INTEGER NOT NULL REFERENCES accounts(id),
    resource_id   TEXT,
    pool_id       INTEGER,
    type          TEXT NOT NULL CHECK (type IN ('unmanaged','cidr_mismatch','orphaned_pool','name_mismatch','account_drift')),
    severity      TEXT NOT NULL DEFAULT 'warning' CHECK (severity IN ('critical','warning','info')),
    status        TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','ignored')),
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    resource_cidr TEXT NOT NULL DEFAULT '',
    pool_cidr     TEXT NOT NULL DEFAULT '',
    details       TEXT NOT NULL DEFAULT '{}',
    ignore_reason TEXT,
    resolved_at   TEXT,
    detected_at   TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_drift_items_account  ON drift_items(account_id);
CREATE INDEX IF NOT EXISTS idx_drift_items_status   ON drift_items(status);
CREATE INDEX IF NOT EXISTS idx_drift_items_type     ON drift_items(type);
CREATE INDEX IF NOT EXISTS idx_drift_items_severity ON drift_items(severity);
