CREATE TABLE sync_schedules (
 account_id BIGINT PRIMARY KEY REFERENCES accounts(seq_id) ON DELETE CASCADE,
 interval_seconds BIGINT NOT NULL DEFAULT 0,
 next_run_at BIGINT NOT NULL DEFAULT 0,
 lease_until BIGINT NOT NULL DEFAULT 0,
 lease_token TEXT NOT NULL DEFAULT '',
 last_agent_id TEXT NOT NULL DEFAULT ''
);
ALTER TABLE sync_jobs ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sync_jobs ADD COLUMN trigger TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_jobs ADD COLUMN identity_source TEXT NOT NULL DEFAULT '';
ALTER TABLE discovered_resources ADD COLUMN updated_at TIMESTAMPTZ;
UPDATE discovered_resources SET updated_at = discovered_at;
