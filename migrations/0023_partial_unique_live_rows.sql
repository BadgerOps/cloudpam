-- Scope account key and pool CIDR uniqueness to live rows, as PostgreSQL does,
-- so a soft-deleted key or CIDR can be created again.
--
-- accounts.key has an inline UNIQUE constraint that only a table rebuild removes.
-- The migrator runs with foreign_keys off, so DROP TABLE accounts leaves child
-- rows (and their ON DELETE CASCADE actions) alone; ids are copied verbatim.

CREATE TABLE accounts_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT NOT NULL,
    name TEXT NOT NULL,
    provider TEXT NULL,
    external_id TEXT NULL,
    description TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    platform TEXT NULL,
    tier TEXT NULL,
    environment TEXT NULL,
    regions TEXT NULL,
    updated_at TIMESTAMP DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    deleted_at TEXT
);

INSERT INTO accounts_new (id, key, name, provider, external_id, description, created_at, platform, tier, environment, regions, updated_at, deleted_at)
SELECT id, key, name, provider, external_id, description, created_at, platform, tier, environment, regions, updated_at, deleted_at
FROM accounts;

-- Carry the AUTOINCREMENT high-water mark over so ids of hard-deleted rows are never reissued.
DELETE FROM sqlite_sequence WHERE name = 'accounts_new';
UPDATE sqlite_sequence SET name = 'accounts_new' WHERE name = 'accounts';

DROP TABLE accounts;
ALTER TABLE accounts_new RENAME TO accounts;

CREATE UNIQUE INDEX idx_accounts_key_unique ON accounts(key) WHERE deleted_at IS NULL;
CREATE INDEX idx_accounts_deleted_at ON accounts(deleted_at);

CREATE TRIGGER account_credentials_drop_on_account_soft_delete
AFTER UPDATE OF deleted_at ON accounts
FOR EACH ROW WHEN NEW.deleted_at IS NOT NULL
BEGIN
    DELETE FROM account_credentials WHERE account_id = NEW.id;
END;

DROP INDEX uniq_pools_parent_cidr;
CREATE UNIQUE INDEX uniq_pools_parent_cidr ON pools (COALESCE(parent_id, -1), cidr) WHERE deleted_at IS NULL;
