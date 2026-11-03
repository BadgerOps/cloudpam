-- Optional per-key rate limit. Both columns are set together; both NULL means
-- the key takes the server default (RATE_LIMIT_RPS / RATE_LIMIT_BURST).
ALTER TABLE api_keys ADD COLUMN rate_limit_rps REAL;
ALTER TABLE api_keys ADD COLUMN rate_limit_burst INTEGER;
