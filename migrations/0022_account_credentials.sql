CREATE TABLE IF NOT EXISTS account_credentials (
    account_id        INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    type              TEXT NOT NULL,
    payload_encrypted TEXT NOT NULL,
    summary           TEXT NOT NULL DEFAULT '{}',
    last_tested_at    TEXT NULL,
    last_test_status  TEXT NULL,
    last_test_error   TEXT NULL,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    CHECK ((last_tested_at IS NULL) = (last_test_status IS NULL))
);

CREATE TRIGGER IF NOT EXISTS account_credentials_drop_on_account_soft_delete
AFTER UPDATE OF deleted_at ON accounts
FOR EACH ROW WHEN NEW.deleted_at IS NOT NULL
BEGIN
    DELETE FROM account_credentials WHERE account_id = NEW.id;
END;
