-- Organizations, roles, permissions for SQLite feature parity with PostgreSQL
-- Backport of tables from migrations/postgres/001_core_schema.up.sql

-- =============================================================================
-- Organizations (multi-tenancy root)
-- =============================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    plan       TEXT NOT NULL DEFAULT 'free',
    settings   TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Default organization for single-tenant deployments
INSERT OR IGNORE INTO organizations (id, name, slug, plan)
VALUES (1, 'Default', 'default', 'free');

-- =============================================================================
-- Roles (RBAC)
-- =============================================================================

CREATE TABLE IF NOT EXISTS roles (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    organization_id INTEGER REFERENCES organizations(id),
    name            TEXT NOT NULL,
    description     TEXT,
    is_builtin      INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at      TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_org_name
    ON roles (COALESCE(organization_id, 0), name);

-- =============================================================================
-- Permissions
-- =============================================================================

CREATE TABLE IF NOT EXISTS permissions (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    category    TEXT NOT NULL
);

-- =============================================================================
-- Role-Permission Mapping
-- =============================================================================

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id       INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id TEXT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- =============================================================================
-- Pool Utilization Cache
-- =============================================================================

CREATE TABLE IF NOT EXISTS pool_utilization_cache (
    pool_id             INTEGER PRIMARY KEY REFERENCES pools(id) ON DELETE CASCADE,
    total_addresses     INTEGER NOT NULL,
    allocated_addresses INTEGER NOT NULL DEFAULT 0,
    child_count         INTEGER NOT NULL DEFAULT 0,
    updated_at          TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- =============================================================================
-- Add updated_at to accounts table
-- =============================================================================

ALTER TABLE accounts ADD COLUMN updated_at TIMESTAMP DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'));

-- =============================================================================
-- Seed Data: Built-in Permissions
-- =============================================================================

INSERT OR IGNORE INTO permissions (id, name, description, category) VALUES
    ('pools:create', 'Create Pools', 'Create new IP address pools', 'pools'),
    ('pools:read', 'Read Pools', 'View pool details and listings', 'pools'),
    ('pools:update', 'Update Pools', 'Modify pool properties', 'pools'),
    ('pools:delete', 'Delete Pools', 'Delete pools', 'pools'),
    ('pools:list', 'List Pools', 'List all pools', 'pools'),
    ('accounts:create', 'Create Accounts', 'Create new cloud accounts', 'accounts'),
    ('accounts:read', 'Read Accounts', 'View account details', 'accounts'),
    ('accounts:update', 'Update Accounts', 'Modify account properties', 'accounts'),
    ('accounts:delete', 'Delete Accounts', 'Delete accounts', 'accounts'),
    ('accounts:list', 'List Accounts', 'List all accounts', 'accounts'),
    ('apikeys:create', 'Create API Keys', 'Create new API keys', 'apikeys'),
    ('apikeys:read', 'Read API Keys', 'View API key details', 'apikeys'),
    ('apikeys:delete', 'Delete API Keys', 'Revoke or delete API keys', 'apikeys'),
    ('apikeys:list', 'List API Keys', 'List all API keys', 'apikeys'),
    ('audit:read', 'Read Audit Logs', 'View audit log entries', 'audit'),
    ('audit:list', 'List Audit Logs', 'List audit log entries', 'audit');

-- =============================================================================
-- Seed Data: Built-in Roles
-- =============================================================================

-- Use well-known IDs: admin=10, operator=20, viewer=30, auditor=40
INSERT OR IGNORE INTO roles (id, organization_id, name, description, is_builtin) VALUES
    (10, NULL, 'admin', 'Full access to all resources', 1),
    (20, NULL, 'operator', 'Read/write pools and accounts', 1),
    (30, NULL, 'viewer', 'Read-only access to pools and accounts', 1),
    (40, NULL, 'auditor', 'Access to audit logs only', 1);

-- Admin: all permissions
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 10, id FROM permissions;

-- Operator: pools + accounts (all actions)
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 20, id FROM permissions WHERE category IN ('pools', 'accounts');

-- Viewer: pools:read, pools:list, accounts:read, accounts:list
INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES
    (30, 'pools:read'),
    (30, 'pools:list'),
    (30, 'accounts:read'),
    (30, 'accounts:list');

-- Auditor: audit:read, audit:list
INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES
    (40, 'audit:read'),
    (40, 'audit:list');
