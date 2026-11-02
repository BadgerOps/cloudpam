package storage

import (
	"context"

	"cloudpam/internal/domain"
)

// AccountCredentialStore persists at most one sealed credential per live account.
// It never sees plaintext.
type AccountCredentialStore interface {
	// GetAccountCredential returns the credential of a live account, or ErrNotFound.
	GetAccountCredential(ctx context.Context, accountID int64) (domain.AccountCredentialRecord, error)
	// PutAccountCredential inserts or replaces in one statement that also requires the
	// account to be live (ErrNotFound otherwise). A replace keeps CreatedAt, moves
	// UpdatedAt, and clears the test result.
	PutAccountCredential(ctx context.Context, in domain.SealedAccountCredential) (domain.AccountCredentialRecord, error)
	// DeleteAccountCredential removes the credential and reports whether one existed.
	DeleteAccountCredential(ctx context.Context, accountID int64) (bool, error)
	// RecordAccountCredentialTest stores test only while the stored ciphertext still
	// equals sealedPayload; ErrConflict otherwise.
	RecordAccountCredentialTest(ctx context.Context, accountID int64, sealedPayload string, test domain.CredentialTest) error
}
