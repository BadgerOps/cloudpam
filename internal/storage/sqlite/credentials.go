//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

const accountCredentialColumns = `c.account_id, c.type, c.payload_encrypted, c.summary, c.last_tested_at, c.last_test_status, c.last_test_error, c.created_at, c.updated_at`

func (s *Store) GetAccountCredential(ctx context.Context, accountID int64) (domain.AccountCredentialRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+accountCredentialColumns+`
		 FROM account_credentials c JOIN accounts a ON a.id = c.account_id
		 WHERE c.account_id = ? AND a.deleted_at IS NULL`, accountID)
	return scanAccountCredential(row)
}

func (s *Store) PutAccountCredential(ctx context.Context, in domain.SealedAccountCredential) (domain.AccountCredentialRecord, error) {
	summary, err := json.Marshal(in.Summary)
	if err != nil {
		return domain.AccountCredentialRecord{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO account_credentials (account_id, type, payload_encrypted, summary, created_at, updated_at)
		 SELECT id, ?, ?, ?, ?, ? FROM accounts WHERE id = ? AND deleted_at IS NULL
		 ON CONFLICT(account_id) DO UPDATE SET
		     type = excluded.type, payload_encrypted = excluded.payload_encrypted, summary = excluded.summary,
		     last_tested_at = NULL, last_test_status = NULL, last_test_error = NULL, updated_at = excluded.updated_at`,
		string(in.Type), in.PayloadEncrypted, string(summary), now, now, in.AccountID)
	if err != nil {
		return domain.AccountCredentialRecord{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.AccountCredentialRecord{}, storage.ErrNotFound
	}
	return s.GetAccountCredential(ctx, in.AccountID)
}

func (s *Store) DeleteAccountCredential(ctx context.Context, accountID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM account_credentials WHERE account_id = ?`, accountID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) RecordAccountCredentialTest(ctx context.Context, accountID int64, sealedPayload string, test domain.CredentialTest) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE account_credentials SET last_tested_at = ?, last_test_status = ?, last_test_error = ?
		 WHERE account_id = ? AND payload_encrypted = ?`,
		test.TestedAt.UTC().Format(time.RFC3339Nano), string(test.Status), test.Error, accountID, sealedPayload)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrConflict
	}
	return nil
}

func scanAccountCredential(row *sql.Row) (domain.AccountCredentialRecord, error) {
	var rec domain.AccountCredentialRecord
	var typ, summary, createdAt, updatedAt string
	var testedAt, testStatus, testError sql.NullString
	err := row.Scan(&rec.AccountID, &typ, &rec.PayloadEncrypted, &summary, &testedAt, &testStatus, &testError, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AccountCredentialRecord{}, storage.ErrNotFound
	}
	if err != nil {
		return domain.AccountCredentialRecord{}, err
	}
	rec.Type = domain.CredentialType(typ)
	if err := json.Unmarshal([]byte(summary), &rec.Summary); err != nil {
		return domain.AccountCredentialRecord{}, fmt.Errorf("decode credential summary: %w", err)
	}
	if rec.CreatedAt, err = time.Parse(time.RFC3339, createdAt); err != nil {
		return domain.AccountCredentialRecord{}, err
	}
	if rec.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt); err != nil {
		return domain.AccountCredentialRecord{}, err
	}
	if testedAt.Valid {
		at, err := time.Parse(time.RFC3339Nano, testedAt.String)
		if err != nil {
			return domain.AccountCredentialRecord{}, err
		}
		rec.LastTest = &domain.CredentialTest{TestedAt: at, Status: domain.CredentialTestStatus(testStatus.String), Error: testError.String}
	}
	return rec, nil
}
