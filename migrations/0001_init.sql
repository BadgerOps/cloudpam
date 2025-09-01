-- Initial schema for CloudPAM

-- Accounts table
CREATE TABLE IF NOT EXISTS accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    provider TEXT NULL,
    external_id TEXT NULL,
    description TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

-- Pools table
CREATE TABLE IF NOT EXISTS pools (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    cidr TEXT NOT NULL,
    parent_id INTEGER NULL,
    account_id INTEGER NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    FOREIGN KEY(parent_id) REFERENCES pools(id) ON DELETE RESTRICT,
    FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE RESTRICT
);

-- Indexes to improve common queries
CREATE INDEX IF NOT EXISTS idx_pools_parent_id ON pools(parent_id);
CREATE INDEX IF NOT EXISTS idx_pools_account_id ON pools(account_id);
CREATE INDEX IF NOT EXISTS idx_pools_cidr ON pools(cidr);

-- Enforce uniqueness of (parent_id, cidr). For NULL parent_id, treat as -1 to disallow duplicates at root.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_pools_parent_cidr ON pools (COALESCE(parent_id, -1), cidr);
