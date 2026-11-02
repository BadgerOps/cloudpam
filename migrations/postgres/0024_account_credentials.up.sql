CREATE TABLE IF NOT EXISTS account_credentials (
    account_id        BIGINT PRIMARY KEY REFERENCES accounts(seq_id) ON DELETE CASCADE,
    type              TEXT NOT NULL,
    payload_encrypted TEXT NOT NULL,
    summary           JSONB NOT NULL DEFAULT '{}',
    last_tested_at    TIMESTAMPTZ NULL,
    last_test_status  TEXT NULL,
    last_test_error   TEXT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL,
    CHECK ((last_tested_at IS NULL) = (last_test_status IS NULL))
);

CREATE OR REPLACE FUNCTION drop_account_credential_on_soft_delete()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.deleted_at IS NOT NULL THEN
        DELETE FROM account_credentials WHERE account_id = NEW.seq_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS account_credentials_drop_on_account_soft_delete ON accounts;
CREATE TRIGGER account_credentials_drop_on_account_soft_delete
    AFTER UPDATE OF deleted_at ON accounts
    FOR EACH ROW EXECUTE FUNCTION drop_account_credential_on_soft_delete();
