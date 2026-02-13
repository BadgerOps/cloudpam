-- CloudPAM PostgreSQL Core Schema
-- Migration 001: Core tables for pools, accounts, audit, and API tokens
-- Following DATABASE_SCHEMA.md design: UUID PKs, JSONB, soft deletes, native CIDR

-- =============================================================================
-- Trigger Functions
-- =============================================================================

CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION update_pool_path()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.parent_id IS NULL THEN
        NEW.path = '/' || NEW.id::text;
        NEW.depth = 0;
    ELSE
        SELECT path || '/' || NEW.id::text, depth + 1
        INTO NEW.path, NEW.depth
        FROM pools WHERE id = NEW.parent_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- =============================================================================
-- Schema Migrations Tracking
-- =============================================================================

CREATE TABLE IF NOT EXISTS schema_migrations (
    version   BIGINT PRIMARY KEY,
    name      TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS schema_info (
    id                   INTEGER PRIMARY KEY CHECK (id = 1),
    schema_version       INTEGER NOT NULL,
    min_supported_schema INTEGER NOT NULL DEFAULT 1,
    app_version          TEXT NOT NULL,
    applied_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- Organizations (multi-tenancy root)
-- =============================================================================

CREATE TABLE organizations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    seq_id     BIGSERIAL UNIQUE NOT NULL,
    name       VARCHAR(255) NOT NULL,
    slug       VARCHAR(100) UNIQUE NOT NULL,
    plan       VARCHAR(50) NOT NULL DEFAULT 'free',
    settings   JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE TRIGGER update_organizations_updated_at
    BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- Insert a default organization for single-tenant deployments
INSERT INTO organizations (id, name, slug, plan)
VALUES ('00000000-0000-0000-0000-000000000001', 'Default', 'default', 'free');

-- =============================================================================
-- Accounts (cloud provider accounts)
-- =============================================================================

CREATE TABLE accounts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    seq_id          BIGSERIAL UNIQUE NOT NULL,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    key             VARCHAR(255) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    provider        VARCHAR(20),
    external_id     VARCHAR(255),
    description     TEXT DEFAULT '',
    platform        VARCHAR(50),
    tier            VARCHAR(50),
    environment     VARCHAR(50),
    regions         JSONB DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_accounts_org_key
    ON accounts (organization_id, key) WHERE deleted_at IS NULL;
CREATE INDEX idx_accounts_org_provider
    ON accounts (organization_id, provider);
CREATE INDEX idx_accounts_seq_id
    ON accounts (seq_id);

CREATE TRIGGER update_accounts_updated_at
    BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- =============================================================================
-- Pools (hierarchical IP address pools)
-- =============================================================================

CREATE TABLE pools (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    seq_id          BIGSERIAL UNIQUE NOT NULL,
    organization_id UUID NOT NULL REFERENCES organizations(id),
    parent_id       UUID REFERENCES pools(id),
    account_id      UUID REFERENCES accounts(id),
    name            VARCHAR(255) NOT NULL,
    description     TEXT DEFAULT '',
    cidr            INET NOT NULL,
    type            VARCHAR(50) NOT NULL DEFAULT 'subnet',
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    source          VARCHAR(20) NOT NULL DEFAULT 'manual',
    path            TEXT NOT NULL DEFAULT '',
    depth           INTEGER NOT NULL DEFAULT 0,
    tags            JSONB DEFAULT '{}',
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_pools_org_cidr
    ON pools (organization_id, cidr) WHERE deleted_at IS NULL;
CREATE INDEX idx_pools_parent_id
    ON pools (parent_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_pools_account_id
    ON pools (account_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_pools_org_type
    ON pools (organization_id, type) WHERE deleted_at IS NULL;
CREATE INDEX idx_pools_path
    ON pools (path) WHERE deleted_at IS NULL;
CREATE INDEX idx_pools_status
    ON pools (status) WHERE deleted_at IS NULL;
CREATE INDEX idx_pools_seq_id
    ON pools (seq_id);
CREATE INDEX idx_pools_cidr
    ON pools USING gist (cidr inet_ops);
CREATE INDEX idx_pools_tags
    ON pools USING gin (tags);

-- Check constraints
ALTER TABLE pools ADD CONSTRAINT chk_pools_depth CHECK (depth >= 0);
ALTER TABLE pools ADD CONSTRAINT chk_pools_type
    CHECK (type IN ('supernet', 'region', 'environment', 'vpc', 'subnet', 'reserved'));
ALTER TABLE pools ADD CONSTRAINT chk_pools_status
    CHECK (status IN ('planned', 'active', 'deprecated'));
ALTER TABLE pools ADD CONSTRAINT chk_pools_source
    CHECK (source IN ('manual', 'discovered', 'imported'));

CREATE TRIGGER update_pools_updated_at
    BEFORE UPDATE ON pools
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER update_pools_path
    BEFORE INSERT OR UPDATE OF parent_id ON pools
    FOR EACH ROW EXECUTE FUNCTION update_pool_path();

-- =============================================================================
-- Pool Utilization Cache
-- =============================================================================

CREATE TABLE pool_utilization_cache (
    pool_id             UUID PRIMARY KEY REFERENCES pools(id) ON DELETE CASCADE,
    total_addresses     BIGINT NOT NULL,
    allocated_addresses BIGINT NOT NULL DEFAULT 0,
    child_count         INTEGER NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- Roles (RBAC)
-- =============================================================================

CREATE TABLE roles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID REFERENCES organizations(id),
    name            VARCHAR(100) NOT NULL,
    description     TEXT,
    is_builtin      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_roles_org_name
    ON roles (COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid), name);

CREATE TRIGGER update_roles_updated_at
    BEFORE UPDATE ON roles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- =============================================================================
-- Permissions
-- =============================================================================

CREATE TABLE permissions (
    id          VARCHAR(100) PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    category    VARCHAR(50) NOT NULL
);

-- =============================================================================
-- Role-Permission Mapping
-- =============================================================================

CREATE TABLE role_permissions (
    role_id       UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id VARCHAR(100) NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- =============================================================================
-- Audit Events
-- =============================================================================

CREATE TABLE audit_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    timestamp     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor_id      TEXT,
    actor_type    VARCHAR(20) NOT NULL,
    actor_email   VARCHAR(255),
    actor_ip      VARCHAR(50),
    action        VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50) NOT NULL,
    resource_id   TEXT NOT NULL,
    resource_name VARCHAR(255),
    changes       JSONB,
    metadata      JSONB DEFAULT '{}',
    request_id    TEXT,
    status_code   INTEGER
);

CREATE INDEX idx_audit_events_org_timestamp
    ON audit_events (organization_id, timestamp DESC);
CREATE INDEX idx_audit_events_actor
    ON audit_events (actor_id, timestamp DESC);
CREATE INDEX idx_audit_events_resource
    ON audit_events (resource_type, resource_id);
CREATE INDEX idx_audit_events_action
    ON audit_events (action);
CREATE INDEX idx_audit_events_timestamp
    ON audit_events (timestamp DESC);

-- =============================================================================
-- API Tokens
-- =============================================================================

CREATE TABLE api_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    name            VARCHAR(255) NOT NULL,
    prefix          VARCHAR(20) NOT NULL,
    token_hash      BYTEA NOT NULL,
    token_salt      BYTEA NOT NULL,
    scopes          JSONB NOT NULL DEFAULT '[]',
    owner_id        TEXT,
    last_used_at    TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_api_tokens_prefix
    ON api_tokens (prefix);
CREATE INDEX idx_api_tokens_org
    ON api_tokens (organization_id);

-- =============================================================================
-- Seed Data: Built-in Roles and Permissions
-- =============================================================================

-- Permissions
INSERT INTO permissions (id, name, description, category) VALUES
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

-- Built-in Roles (no organization_id = global)
INSERT INTO roles (id, organization_id, name, description, is_builtin) VALUES
    ('00000000-0000-0000-0000-000000000010', NULL, 'admin', 'Full access to all resources', TRUE),
    ('00000000-0000-0000-0000-000000000020', NULL, 'operator', 'Read/write pools and accounts', TRUE),
    ('00000000-0000-0000-0000-000000000030', NULL, 'viewer', 'Read-only access to pools and accounts', TRUE),
    ('00000000-0000-0000-0000-000000000040', NULL, 'auditor', 'Access to audit logs only', TRUE);

-- Admin: all permissions
INSERT INTO role_permissions (role_id, permission_id)
SELECT '00000000-0000-0000-0000-000000000010'::uuid, id FROM permissions;

-- Operator: pools + accounts (all actions)
INSERT INTO role_permissions (role_id, permission_id)
SELECT '00000000-0000-0000-0000-000000000020'::uuid, id FROM permissions
WHERE category IN ('pools', 'accounts');

-- Viewer: pools:read, pools:list, accounts:read, accounts:list
INSERT INTO role_permissions (role_id, permission_id) VALUES
    ('00000000-0000-0000-0000-000000000030', 'pools:read'),
    ('00000000-0000-0000-0000-000000000030', 'pools:list'),
    ('00000000-0000-0000-0000-000000000030', 'accounts:read'),
    ('00000000-0000-0000-0000-000000000030', 'accounts:list');

-- Auditor: audit:read, audit:list
INSERT INTO role_permissions (role_id, permission_id) VALUES
    ('00000000-0000-0000-0000-000000000040', 'audit:read'),
    ('00000000-0000-0000-0000-000000000040', 'audit:list');
