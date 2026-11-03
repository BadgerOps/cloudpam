-- Optional per-key rate limit. Both columns are set together; both NULL means
-- the key takes the server default (RATE_LIMIT_RPS / RATE_LIMIT_BURST).
ALTER TABLE api_tokens
    ADD COLUMN IF NOT EXISTS rate_limit_rps DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS rate_limit_burst INTEGER;

ALTER TABLE api_tokens
    ADD CONSTRAINT api_tokens_rate_limit_pair
    CHECK ((rate_limit_rps IS NULL) = (rate_limit_burst IS NULL)
        AND (rate_limit_rps IS NULL OR (rate_limit_rps > 0 AND rate_limit_burst > 0)));
