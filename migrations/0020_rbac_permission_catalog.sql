-- Expand RBAC permissions to cover all app surfaces used by runtime checks.

INSERT INTO permissions (id, name, description, category) VALUES
    ('pools:create', 'Create pools', 'Create address pools and planned allocations', 'IPAM'),
    ('pools:read', 'Read pools', 'View pool details, blocks, utilization, and schema checks', 'IPAM'),
    ('pools:update', 'Update pools', 'Edit pool metadata, hierarchy, and assignment', 'IPAM'),
    ('pools:delete', 'Delete pools', 'Delete pools and planned allocations', 'IPAM'),
    ('pools:list', 'List pools', 'Browse pool lists and tree views', 'IPAM'),
    ('accounts:create', 'Create accounts', 'Create cloud account records', 'Accounts'),
    ('accounts:read', 'Read accounts', 'View account details and account-linked resources', 'Accounts'),
    ('accounts:update', 'Update accounts', 'Edit account metadata', 'Accounts'),
    ('accounts:delete', 'Delete accounts', 'Delete account records', 'Accounts'),
    ('accounts:list', 'List accounts', 'Browse account lists', 'Accounts'),
    ('apikeys:create', 'Create API keys', 'Create API tokens within the caller permission envelope', 'Identity'),
    ('apikeys:read', 'Read API keys', 'View API key metadata', 'Identity'),
    ('apikeys:update', 'Update API keys', 'Reserved for future API key metadata updates', 'Identity'),
    ('apikeys:delete', 'Delete API keys', 'Revoke API keys', 'Identity'),
    ('apikeys:list', 'List API keys', 'Browse API key metadata', 'Identity'),
    ('audit:read', 'Read audit logs', 'View audit event details', 'Audit'),
    ('audit:list', 'List audit logs', 'Browse audit events', 'Audit'),
    ('users:create', 'Create users', 'Create local user accounts', 'Identity'),
    ('users:read', 'Read users', 'View user account details', 'Identity'),
    ('users:update', 'Update users', 'Edit users, roles, password state, and active status', 'Identity'),
    ('users:delete', 'Delete users', 'Deactivate user accounts', 'Identity'),
    ('users:list', 'List users', 'Browse user accounts', 'Identity'),
    ('discovery:create', 'Start discovery', 'Start discovery syncs and register agents', 'Discovery'),
    ('discovery:read', 'Read discovery', 'View discovered resources, agents, drift, and recommendations', 'Discovery'),
    ('discovery:update', 'Update discovery', 'Apply discovery results and reconcile drift', 'Discovery'),
    ('discovery:delete', 'Delete discovery', 'Reserved for future discovery cleanup operations', 'Discovery'),
    ('discovery:list', 'List discovery', 'Browse discovery resources, jobs, agents, drift, and recommendations', 'Discovery'),
    ('settings:read', 'Read settings', 'View security, OIDC, update, and system configuration', 'Settings'),
    ('settings:write', 'Write settings', 'Change security, OIDC, update, and system configuration', 'Settings')
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    description = excluded.description,
    category = excluded.category;

DELETE FROM role_permissions WHERE role_id IN (10, 20, 30, 40);

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 10, id FROM permissions;

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 20, id FROM permissions
WHERE id IN (
    'pools:create', 'pools:read', 'pools:update', 'pools:delete', 'pools:list',
    'accounts:create', 'accounts:read', 'accounts:update', 'accounts:delete', 'accounts:list',
    'discovery:create', 'discovery:read', 'discovery:update', 'discovery:list'
);

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 30, id FROM permissions
WHERE id IN ('pools:read', 'pools:list', 'accounts:read', 'accounts:list', 'discovery:read', 'discovery:list');

INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
SELECT 40, id FROM permissions
WHERE id IN ('audit:read', 'audit:list');
