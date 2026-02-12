-- CloudPAM PostgreSQL Core Schema - Rollback
-- Migration 001 DOWN: Drop all core tables in reverse dependency order

DROP TABLE IF EXISTS role_permissions CASCADE;
DROP TABLE IF EXISTS permissions CASCADE;
DROP TABLE IF EXISTS roles CASCADE;
DROP TABLE IF EXISTS api_tokens CASCADE;
DROP TABLE IF EXISTS audit_events CASCADE;
DROP TABLE IF EXISTS pool_utilization_cache CASCADE;
DROP TABLE IF EXISTS pools CASCADE;
DROP TABLE IF EXISTS accounts CASCADE;
DROP TABLE IF EXISTS organizations CASCADE;
DROP TABLE IF EXISTS schema_info CASCADE;
DROP TABLE IF EXISTS schema_migrations CASCADE;

DROP FUNCTION IF EXISTS update_pool_path() CASCADE;
DROP FUNCTION IF EXISTS update_updated_at() CASCADE;
