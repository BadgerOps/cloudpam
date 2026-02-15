# CloudPAM Database Schema

This document defines the database schema for CloudPAM, supporting both PostgreSQL (production) and SQLite (development/lightweight deployments).

## Design Principles

1. **UUID Primary Keys**: All tables use UUIDs for global uniqueness and easier multi-instance sync
2. **Soft Deletes**: Critical entities support soft deletion via `deleted_at` timestamp
3. **Audit Trail**: All mutations tracked via triggers and audit_events table
4. **Hierarchical Data**: Pools use adjacency list with materialized path for efficient queries
5. **JSON Metadata**: Flexible metadata stored as JSONB (PostgreSQL) or JSON (SQLite)

## Database Compatibility

| Feature | PostgreSQL | SQLite |
|---------|------------|--------|
| UUID type | Native `UUID` | `TEXT` (36 chars) |
| JSON | `JSONB` (indexed) | `JSON` (text) |
| Arrays | Native `TEXT[]` | JSON array in TEXT |
| INET type | Native `INET` | `TEXT` |
| Timestamps | `TIMESTAMPTZ` | `TEXT` (ISO 8601) |
| Full-text search | `tsvector` + GIN | FTS5 virtual table |
| CIDR operations | Native operators | Custom functions |

## Entity Relationship Diagram

```
┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│  organizations  │───┐   │     users       │       │     roles       │
└─────────────────┘   │   └────────┬────────┘       └────────┬────────┘
                      │            │                         │
                      │            │    ┌────────────────────┘
                      │            │    │
                      │            ▼    ▼
                      │   ┌─────────────────┐       ┌─────────────────┐
                      │   │   user_roles    │       │   permissions   │
                      │   └─────────────────┘       └─────────────────┘
                      │                                      │
                      │   ┌─────────────────┐       ┌────────┴────────┐
                      │   │     teams       │◄──────│ role_permissions│
                      │   └────────┬────────┘       └─────────────────┘
                      │            │
                      │            ▼
                      │   ┌─────────────────┐       ┌─────────────────┐
                      │   │  team_members   │       │team_pool_access │
                      │   └─────────────────┘       └─────────────────┘
                      │                                      │
                      │                                      │
                      ▼                                      ▼
┌─────────────────────────────────────────────────────────────────────┐
│                              pools                                   │
│  (hierarchical - parent_id references self)                         │
└─────────────────────────────────────────────────────────────────────┘
         │                    │                           │
         │                    │                           │
         ▼                    ▼                           ▼
┌─────────────────┐  ┌─────────────────┐       ┌─────────────────┐
│  planned_pools  │  │linked_resources │       │   allocations   │
└────────┬────────┘  └─────────────────┘       └─────────────────┘
         │                    ▲
         │                    │
┌────────┴────────┐  ┌────────┴────────┐       ┌─────────────────┐
│  schema_plans   │  │discovered_      │◄──────│    accounts     │
└─────────────────┘  │resources        │       └────────┬────────┘
         ▲           └─────────────────┘                │
         │                    ▲                         │
┌────────┴────────┐           │                         ▼
│schema_templates │  ┌────────┴────────┐       ┌─────────────────┐
└─────────────────┘  │   sync_jobs     │       │   collectors    │
                     └─────────────────┘       └─────────────────┘

┌─────────────────┐  ┌─────────────────┐       ┌─────────────────┐
│   api_tokens    │  │    sessions     │       │  audit_events   │
└─────────────────┘  └─────────────────┘       └─────────────────┘

┌─────────────────┐
│   sso_config    │
└─────────────────┘
```

## Table Definitions

### Core Tables

#### organizations
Primary tenant/organization entity.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Organization ID |
| name | VARCHAR(255) | NOT NULL | Display name |
| slug | VARCHAR(100) | UNIQUE, NOT NULL | URL-safe identifier |
| plan | VARCHAR(50) | NOT NULL DEFAULT 'free' | Subscription plan |
| settings | JSONB | DEFAULT '{}' | Organization settings |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | Creation timestamp |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | Last update |
| deleted_at | TIMESTAMPTZ | NULL | Soft delete timestamp |

#### users
User accounts (authenticated via OAuth/OIDC).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | User ID |
| organization_id | UUID | FK organizations(id) | Owning organization |
| email | VARCHAR(255) | NOT NULL | Email address |
| name | VARCHAR(255) | | Display name |
| avatar_url | TEXT | | Profile picture URL |
| status | VARCHAR(20) | NOT NULL DEFAULT 'invited' | active/invited/disabled |
| external_id | VARCHAR(255) | | IdP subject identifier |
| metadata | JSONB | DEFAULT '{}' | Additional user data |
| last_login_at | TIMESTAMPTZ | | Last successful login |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| deleted_at | TIMESTAMPTZ | | Soft delete |

**Indexes:**
- UNIQUE (organization_id, email) WHERE deleted_at IS NULL
- INDEX (external_id)
- INDEX (status)

#### roles
Role definitions for RBAC.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Role ID |
| organization_id | UUID | FK organizations(id), NULL | NULL = built-in role |
| name | VARCHAR(100) | NOT NULL | Role name |
| description | TEXT | | Role description |
| is_builtin | BOOLEAN | NOT NULL DEFAULT FALSE | System-defined role |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

**Indexes:**
- UNIQUE (organization_id, name)

#### permissions
Available permissions in the system.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | VARCHAR(100) | PK | Permission ID (e.g., 'pools:write') |
| name | VARCHAR(100) | NOT NULL | Display name |
| description | TEXT | | Permission description |
| category | VARCHAR(50) | NOT NULL | Grouping category |

#### role_permissions
Many-to-many: roles to permissions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| role_id | UUID | PK, FK roles(id) | |
| permission_id | VARCHAR(100) | PK, FK permissions(id) | |

#### user_roles
User role assignments.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| user_id | UUID | PK, FK users(id) | |
| role_id | UUID | PK, FK roles(id) | |
| assigned_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| assigned_by | UUID | FK users(id) | Who assigned the role |

### Pool Management

#### pools
Hierarchical IP address pools.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Pool ID |
| organization_id | UUID | FK organizations(id), NOT NULL | Owning org |
| parent_id | UUID | FK pools(id), NULL | Parent pool (NULL = root) |
| name | VARCHAR(255) | NOT NULL | Pool name |
| description | TEXT | | Pool description |
| cidr | VARCHAR(50) | NOT NULL | CIDR notation (e.g., 10.0.0.0/16) |
| type | VARCHAR(50) | NOT NULL | supernet/region/environment/vpc/subnet/reserved |
| path | TEXT | NOT NULL | Materialized path (e.g., '/root-id/parent-id/this-id') |
| depth | INTEGER | NOT NULL DEFAULT 0 | Depth in hierarchy |
| tags | JSONB | DEFAULT '{}' | Key-value tags |
| metadata | JSONB | DEFAULT '{}' | Custom metadata |
| created_by | UUID | FK users(id) | Creator |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| deleted_at | TIMESTAMPTZ | | Soft delete |

**Indexes:**
- UNIQUE (organization_id, cidr) WHERE deleted_at IS NULL
- INDEX (parent_id)
- INDEX (organization_id, type)
- INDEX (path) -- For hierarchical queries
- INDEX USING GIN (tags) -- PostgreSQL only
- INDEX (cidr) -- For overlap detection

**Constraints:**
- CHECK (cidr ~ '^[0-9]{1,3}(\.[0-9]{1,3}){3}/[0-9]{1,2}$') -- PostgreSQL
- CHECK (depth >= 0)

#### pool_utilization_cache
Cached utilization stats (updated by triggers/background job).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| pool_id | UUID | PK, FK pools(id) | |
| total_addresses | BIGINT | NOT NULL | Total IPs in CIDR |
| allocated_addresses | BIGINT | NOT NULL DEFAULT 0 | IPs in children |
| child_count | INTEGER | NOT NULL DEFAULT 0 | Direct children |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | Last recalculation |

### Schema Planning

#### schema_templates
IP schema planning templates.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Template ID |
| organization_id | UUID | FK organizations(id), NULL | NULL = built-in |
| name | VARCHAR(255) | NOT NULL | Template name |
| description | TEXT | | Template description |
| category | VARCHAR(50) | NOT NULL | enterprise/cloud/hybrid/small-business/custom |
| structure | JSONB | NOT NULL | Hierarchical structure definition |
| parameters | JSONB | DEFAULT '[]' | Configurable parameters |
| is_builtin | BOOLEAN | NOT NULL DEFAULT FALSE | System template |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

#### schema_plans
Saved schema plans (draft or applied).

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Plan ID |
| organization_id | UUID | FK organizations(id), NOT NULL | |
| template_id | UUID | FK schema_templates(id), NULL | Source template |
| name | VARCHAR(255) | NOT NULL | Plan name |
| description | TEXT | | Plan description |
| status | VARCHAR(20) | NOT NULL DEFAULT 'draft' | draft/applied/archived |
| root_cidr | VARCHAR(50) | NOT NULL | Root CIDR for plan |
| parameters | JSONB | DEFAULT '{}' | Template parameters used |
| created_by | UUID | FK users(id) | |
| applied_by | UUID | FK users(id), NULL | Who applied it |
| applied_at | TIMESTAMPTZ | NULL | When applied |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

**Indexes:**
- INDEX (organization_id, status)
- INDEX (template_id)

#### planned_pools
Pools defined within a schema plan.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Planned pool ID |
| plan_id | UUID | FK schema_plans(id), NOT NULL | Parent plan |
| temp_id | VARCHAR(100) | NOT NULL | Temporary ID within plan |
| parent_temp_id | VARCHAR(100) | NULL | Parent's temp_id |
| name | VARCHAR(255) | NOT NULL | Planned pool name |
| cidr | VARCHAR(50) | NOT NULL | Planned CIDR |
| type | VARCHAR(50) | NOT NULL | Pool type |
| tags | JSONB | DEFAULT '{}' | Planned tags |
| pool_id | UUID | FK pools(id), NULL | Actual pool after apply |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

**Indexes:**
- INDEX (plan_id)
- UNIQUE (plan_id, temp_id)

### Cloud Integration

#### accounts
Cloud provider accounts.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Account ID |
| organization_id | UUID | FK organizations(id), NOT NULL | |
| name | VARCHAR(255) | NOT NULL | Display name |
| provider | VARCHAR(20) | NOT NULL | aws/gcp/azure |
| provider_account_id | VARCHAR(100) | | AWS Account ID, GCP Project, etc. |
| status | VARCHAR(20) | NOT NULL DEFAULT 'disconnected' | connected/disconnected/error |
| auth_type | VARCHAR(50) | NOT NULL | iam_role/workload_identity/service_account/access_key |
| auth_config_encrypted | BYTEA | NOT NULL | Encrypted auth configuration |
| regions | JSONB | DEFAULT '[]' | Enabled regions array |
| auto_sync | BOOLEAN | NOT NULL DEFAULT TRUE | Auto-sync enabled |
| sync_interval_minutes | INTEGER | NOT NULL DEFAULT 60 | Sync frequency |
| last_sync_at | TIMESTAMPTZ | NULL | Last successful sync |
| last_error | TEXT | NULL | Last error message |
| created_by | UUID | FK users(id) | |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| deleted_at | TIMESTAMPTZ | | Soft delete |

**Indexes:**
- UNIQUE (organization_id, provider, provider_account_id) WHERE deleted_at IS NULL
- INDEX (status)
- INDEX (organization_id, provider)

#### collectors
Discovery collector registrations.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Collector ID |
| organization_id | UUID | FK organizations(id), NOT NULL | |
| name | VARCHAR(255) | NOT NULL | Collector name |
| version | VARCHAR(50) | | Collector version |
| status | VARCHAR(20) | NOT NULL DEFAULT 'offline' | online/offline/degraded |
| last_heartbeat_at | TIMESTAMPTZ | NULL | Last heartbeat |
| metadata | JSONB | DEFAULT '{}' | Collector metadata |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

#### collector_accounts
Many-to-many: collectors to accounts.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| collector_id | UUID | PK, FK collectors(id) | |
| account_id | UUID | PK, FK accounts(id) | |

#### discovered_resources
Resources found via cloud discovery.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Resource ID |
| organization_id | UUID | FK organizations(id), NOT NULL | |
| account_id | UUID | FK accounts(id), NOT NULL | Source account |
| provider | VARCHAR(20) | NOT NULL | aws/gcp/azure |
| resource_type | VARCHAR(50) | NOT NULL | vpc/subnet/network_interface/elastic_ip/instance |
| resource_id | VARCHAR(255) | NOT NULL | Cloud provider resource ID |
| name | VARCHAR(255) | | Resource name |
| region | VARCHAR(50) | NOT NULL | Cloud region |
| cidr | VARCHAR(50) | NULL | CIDR if applicable |
| ip_address | VARCHAR(50) | NULL | IP address if applicable |
| status | VARCHAR(20) | NOT NULL DEFAULT 'untracked' | tracked/untracked/conflict/orphaned |
| linked_pool_id | UUID | FK pools(id), NULL | Linked pool |
| metadata | JSONB | DEFAULT '{}' | Provider-specific metadata |
| first_seen_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | First discovery |
| last_seen_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | Latest sync |
| deleted_at | TIMESTAMPTZ | NULL | No longer exists in cloud |

**Indexes:**
- UNIQUE (account_id, resource_type, resource_id)
- INDEX (organization_id, status)
- INDEX (linked_pool_id)
- INDEX (cidr) -- For conflict detection
- INDEX (last_seen_at) -- For orphan detection

#### sync_jobs
Discovery sync job tracking.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Job ID |
| account_id | UUID | FK accounts(id), NOT NULL | |
| collector_id | UUID | FK collectors(id), NULL | |
| status | VARCHAR(20) | NOT NULL DEFAULT 'pending' | pending/running/completed/failed |
| started_at | TIMESTAMPTZ | NULL | Job start |
| completed_at | TIMESTAMPTZ | NULL | Job completion |
| resources_discovered | INTEGER | DEFAULT 0 | Total found |
| resources_added | INTEGER | DEFAULT 0 | New resources |
| resources_updated | INTEGER | DEFAULT 0 | Changed resources |
| resources_removed | INTEGER | DEFAULT 0 | Disappeared |
| error | TEXT | NULL | Error message if failed |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

**Indexes:**
- INDEX (account_id, created_at DESC)
- INDEX (status)

### Teams

#### teams
Team definitions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Team ID |
| organization_id | UUID | FK organizations(id), NOT NULL | |
| name | VARCHAR(255) | NOT NULL | Team name |
| description | TEXT | | Team description |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| deleted_at | TIMESTAMPTZ | | Soft delete |

**Indexes:**
- UNIQUE (organization_id, name) WHERE deleted_at IS NULL

#### team_members
Team membership.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| team_id | UUID | PK, FK teams(id) | |
| user_id | UUID | PK, FK users(id) | |
| role | VARCHAR(20) | NOT NULL DEFAULT 'member' | member/lead |
| joined_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

#### team_pool_access
Team access to specific pools.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Access rule ID |
| team_id | UUID | FK teams(id), NOT NULL | |
| pool_id | UUID | FK pools(id), NOT NULL | |
| access_level | VARCHAR(20) | NOT NULL | view/edit/admin |
| include_children | BOOLEAN | NOT NULL DEFAULT TRUE | Apply to children |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

**Indexes:**
- UNIQUE (team_id, pool_id)
- INDEX (pool_id)

### Authentication

#### sessions
User sessions.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Session ID |
| user_id | UUID | FK users(id), NOT NULL | |
| access_token_encrypted | BYTEA | NOT NULL | Encrypted access token |
| refresh_token_encrypted | BYTEA | NOT NULL | Encrypted refresh token |
| access_token_expires_at | TIMESTAMPTZ | NOT NULL | Access token expiry |
| refresh_token_expires_at | TIMESTAMPTZ | NOT NULL | Refresh token expiry |
| ip_address | VARCHAR(50) | | Client IP |
| user_agent | TEXT | | Browser/client info |
| last_activity_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| revoked_at | TIMESTAMPTZ | NULL | Revocation timestamp |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

**Indexes:**
- INDEX (user_id)
- INDEX (refresh_token_expires_at) WHERE revoked_at IS NULL

#### api_tokens
API tokens for programmatic access.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Token ID |
| organization_id | UUID | FK organizations(id), NOT NULL | |
| user_id | UUID | FK users(id), NOT NULL | Token owner |
| name | VARCHAR(255) | NOT NULL | Token name |
| prefix | VARCHAR(20) | NOT NULL | First chars for identification |
| token_hash | BYTEA | NOT NULL | Argon2id hash of token |
| scopes | JSONB | NOT NULL | Array of scope strings |
| last_used_at | TIMESTAMPTZ | NULL | Last usage |
| expires_at | TIMESTAMPTZ | NULL | NULL = no expiration |
| revoked_at | TIMESTAMPTZ | NULL | Revocation timestamp |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

**Indexes:**
- INDEX (organization_id, user_id)
- INDEX (prefix) -- For lookup by prefix
- INDEX (token_hash) -- For validation

#### sso_config
SSO/OIDC configuration.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Config ID |
| organization_id | UUID | FK organizations(id), UNIQUE | One per org |
| enabled | BOOLEAN | NOT NULL DEFAULT FALSE | SSO enabled |
| provider_type | VARCHAR(20) | NOT NULL DEFAULT 'oidc' | oidc/saml |
| issuer_url | TEXT | | OIDC issuer URL |
| client_id | VARCHAR(255) | | OAuth client ID |
| client_secret_encrypted | BYTEA | | Encrypted client secret |
| scopes | JSONB | DEFAULT '["openid", "profile", "email"]' | |
| role_mapping | JSONB | DEFAULT '[]' | Claim to role mapping |
| auto_provision_users | BOOLEAN | NOT NULL DEFAULT TRUE | |
| default_role_id | UUID | FK roles(id), NULL | Default role for new users |
| created_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |
| updated_at | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | |

### Audit

#### audit_events
Audit log of all changes.

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Event ID |
| organization_id | UUID | NOT NULL | Org context |
| timestamp | TIMESTAMPTZ | NOT NULL DEFAULT NOW() | Event time |
| actor_id | UUID | NULL | User/token ID |
| actor_type | VARCHAR(20) | NOT NULL | user/api_token/system |
| actor_email | VARCHAR(255) | | Actor email if user |
| actor_ip | VARCHAR(50) | | Client IP |
| action | VARCHAR(50) | NOT NULL | create/update/delete/login/etc. |
| resource_type | VARCHAR(50) | NOT NULL | pool/account/user/etc. |
| resource_id | UUID | NOT NULL | Affected resource |
| resource_name | VARCHAR(255) | | Resource name at time of event |
| changes | JSONB | | { before: {}, after: {} } |
| metadata | JSONB | DEFAULT '{}' | Additional context |

**Indexes:**
- INDEX (organization_id, timestamp DESC)
- INDEX (actor_id, timestamp DESC)
- INDEX (resource_type, resource_id)
- INDEX (action)

**Partitioning (PostgreSQL):**
Consider range partitioning by timestamp for large deployments.

## CIDR Operations

### PostgreSQL
Use native `inet` and `cidr` types with operators:

```sql
-- Check if CIDR contains another
SELECT '10.0.0.0/8'::cidr >> '10.1.0.0/16'::cidr;  -- true

-- Check overlap
SELECT '10.0.0.0/16'::cidr && '10.0.128.0/17'::cidr;  -- true

-- Get network address
SELECT network('10.0.0.5/24'::cidr);  -- 10.0.0.0/24
```

### SQLite
Use custom functions (registered in Go):

```sql
-- Custom functions needed:
-- cidr_contains(parent, child) -> boolean
-- cidr_overlaps(cidr1, cidr2) -> boolean
-- cidr_network(cidr) -> text
-- cidr_broadcast(cidr) -> text
-- cidr_size(cidr) -> integer
```

## Triggers

### PostgreSQL Triggers

```sql
-- Update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply to all tables with updated_at
CREATE TRIGGER update_pools_updated_at
    BEFORE UPDATE ON pools
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at();

-- Update pool path on insert/update
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

CREATE TRIGGER update_pool_path_trigger
    BEFORE INSERT OR UPDATE OF parent_id ON pools
    FOR EACH ROW
    EXECUTE FUNCTION update_pool_path();

-- Audit trigger
CREATE OR REPLACE FUNCTION audit_trigger()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO audit_events (
        id, organization_id, timestamp, actor_id, actor_type,
        action, resource_type, resource_id, resource_name, changes
    ) VALUES (
        gen_random_uuid(),
        COALESCE(NEW.organization_id, OLD.organization_id),
        NOW(),
        current_setting('app.current_user_id', true)::uuid,
        COALESCE(current_setting('app.current_actor_type', true), 'system'),
        TG_OP,
        TG_TABLE_NAME,
        COALESCE(NEW.id, OLD.id),
        COALESCE(NEW.name, OLD.name),
        jsonb_build_object(
            'before', CASE WHEN TG_OP != 'INSERT' THEN to_jsonb(OLD) END,
            'after', CASE WHEN TG_OP != 'DELETE' THEN to_jsonb(NEW) END
        )
    );
    RETURN COALESCE(NEW, OLD);
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_pools
    AFTER INSERT OR UPDATE OR DELETE ON pools
    FOR EACH ROW EXECUTE FUNCTION audit_trigger();
```

### SQLite Triggers

SQLite triggers are more limited but can achieve similar results:

```sql
-- Updated_at trigger
CREATE TRIGGER update_pools_updated_at
    AFTER UPDATE ON pools
BEGIN
    UPDATE pools SET updated_at = datetime('now')
    WHERE id = NEW.id;
END;

-- Pool path trigger (simplified)
CREATE TRIGGER update_pool_path_insert
    AFTER INSERT ON pools
    WHEN NEW.parent_id IS NOT NULL
BEGIN
    UPDATE pools SET
        path = (SELECT path FROM pools WHERE id = NEW.parent_id) || '/' || NEW.id,
        depth = (SELECT depth + 1 FROM pools WHERE id = NEW.parent_id)
    WHERE id = NEW.id;
END;
```

## Migration Strategy

### Version Table

```sql
CREATE TABLE schema_migrations (
    version BIGINT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    description TEXT
);
```

### Migration Naming

```
migrations/
├── postgres/
│   ├── 001_initial_schema.up.sql
│   ├── 001_initial_schema.down.sql
│   ├── 002_add_teams.up.sql
│   ├── 002_add_teams.down.sql
│   └── ...
└── sqlite/
    ├── 001_initial_schema.up.sql
    ├── 001_initial_schema.down.sql
    └── ...
```

## Performance Considerations

### Indexing Strategy

1. **Primary lookups**: UUID primary keys, indexed by default
2. **Foreign key columns**: Always indexed
3. **Frequently filtered columns**: status, type, provider
4. **Range queries**: timestamp columns
5. **Text search**: Full-text indexes for name/description
6. **JSON fields**: GIN indexes on JSONB columns (PostgreSQL)

### Query Optimization

1. **Pool tree queries**: Use materialized path for subtree queries
   ```sql
   SELECT * FROM pools WHERE path LIKE '/root-id/%';
   ```

2. **Utilization calculation**: Cache in pool_utilization_cache, refresh async

3. **Audit queries**: Partition by timestamp for large datasets

4. **Discovery queries**: Index on (account_id, last_seen_at) for sync operations

### Connection Pooling

- PostgreSQL: Use PgBouncer or built-in pool (20-50 connections)
- SQLite: Single connection with WAL mode

```sql
-- SQLite WAL mode
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA cache_size=-64000;  -- 64MB cache
PRAGMA busy_timeout=5000;
```
