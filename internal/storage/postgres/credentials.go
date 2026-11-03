//go:build postgres

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

const accountCredentialColumns = `c.account_id, c.type, c.payload_encrypted, c.summary, c.last_tested_at, c.last_test_status, c.last_test_error, c.created_at, c.updated_at`

func (s *Store) GetAccountCredential(ctx context.Context, accountID int64) (domain.AccountCredentialRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+accountCredentialColumns+`
		 FROM account_credentials c JOIN accounts a ON a.seq_id = c.account_id
		 WHERE c.account_id = $1 AND a.organization_id = $2 AND a.deleted_at IS NULL`, accountID, s.orgID)
	return scanAccountCredential(row)
}

func (s *Store) PutAccountCredential(ctx context.Context, in domain.SealedAccountCredential) (domain.AccountCredentialRecord, error) {
	summary, err := json.Marshal(in.Summary)
	if err != nil {
		return domain.AccountCredentialRecord{}, err
	}
	// FOR SHARE holds the account row so a concurrent soft delete cannot slip between
	// the liveness check and the insert.
	row := s.pool.QueryRow(ctx,
		`INSERT INTO account_credentials (account_id, type, payload_encrypted, summary, created_at, updated_at)
		 SELECT seq_id, $2, $3, $4::jsonb, $5, $5 FROM accounts
		 WHERE seq_id = $1 AND organization_id = $6 AND deleted_at IS NULL FOR SHARE
		 ON CONFLICT (account_id) DO UPDATE SET
		     type = EXCLUDED.type, payload_encrypted = EXCLUDED.payload_encrypted, summary = EXCLUDED.summary,
		     last_tested_at = NULL, last_test_status = NULL, last_test_error = NULL, updated_at = EXCLUDED.updated_at
		 RETURNING account_id, type, payload_encrypted, summary, last_tested_at, last_test_status, last_test_error, created_at, updated_at`,
		in.AccountID, string(in.Type), in.PayloadEncrypted, string(summary), time.Now().UTC(), s.orgID)
	return scanAccountCredential(row)
}

func (s *Store) DeleteAccountCredential(ctx context.Context, accountID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM account_credentials
		 WHERE account_id = $1 AND account_id IN (SELECT seq_id FROM accounts WHERE organization_id = $2)`,
		accountID, s.orgID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) RecordAccountCredentialTest(ctx context.Context, accountID int64, sealedPayload string, test domain.CredentialTest) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE account_credentials SET last_tested_at = $1, last_test_status = $2, last_test_error = $3
		 WHERE account_id = $4 AND payload_encrypted = $5
		   AND account_id IN (SELECT seq_id FROM accounts WHERE organization_id = $6)`,
		test.TestedAt.UTC(), string(test.Status), test.Error, accountID, sealedPayload, s.orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrConflict
	}
	return nil
}

func scanAccountCredential(row pgx.Row) (domain.AccountCredentialRecord, error) {
	var rec domain.AccountCredentialRecord
	var typ string
	var summary []byte
	var testedAt *time.Time
	var testStatus, testError *string
	err := row.Scan(&rec.AccountID, &typ, &rec.PayloadEncrypted, &summary, &testedAt, &testStatus, &testError, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AccountCredentialRecord{}, storage.ErrNotFound
	}
	if err != nil {
		return domain.AccountCredentialRecord{}, err
	}
	rec.Type = domain.CredentialType(typ)
	if err := json.Unmarshal(summary, &rec.Summary); err != nil {
		return domain.AccountCredentialRecord{}, fmt.Errorf("decode credential summary: %w", err)
	}
	if testedAt != nil {
		lt := domain.CredentialTest{TestedAt: *testedAt, Status: domain.CredentialTestStatus(*testStatus)}
		if testError != nil {
			lt.Error = *testError
		}
		rec.LastTest = &lt
	}
	return rec, nil
}
