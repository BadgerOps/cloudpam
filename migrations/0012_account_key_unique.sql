-- Add unique index on accounts.key for efficient GetAccountByKey lookups
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_key_unique ON accounts(key);
