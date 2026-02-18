-- Add soft-delete support to pools and accounts.
-- Rows with deleted_at IS NOT NULL are treated as deleted;
-- all existing queries filter on deleted_at IS NULL.

ALTER TABLE pools ADD COLUMN deleted_at TEXT;
ALTER TABLE accounts ADD COLUMN deleted_at TEXT;

-- Index for efficient filtering of non-deleted rows.
CREATE INDEX IF NOT EXISTS idx_pools_deleted_at ON pools(deleted_at);
CREATE INDEX IF NOT EXISTS idx_accounts_deleted_at ON accounts(deleted_at);
