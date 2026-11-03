// Package storagetest holds conformance suites every storage backend must pass.
package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// RunAccountCredentialSuite exercises AccountCredentialStore against a fresh store.
func RunAccountCredentialSuite(t *testing.T, s storage.Store) {
	t.Helper()
	ctx := context.Background()

	newAccount := func(t *testing.T, key string) domain.Account {
		t.Helper()
		a, err := s.CreateAccount(ctx, domain.CreateAccount{Key: key, Name: key, Provider: "aws"})
		if err != nil {
			t.Fatalf("create account %s: %v", key, err)
		}
		return a
	}
	sealed := func(accountID int64, payload string) domain.SealedAccountCredential {
		return domain.SealedAccountCredential{
			AccountID:        accountID,
			Type:             domain.CredentialTypeAWSAssumeRole,
			Summary:          domain.CredentialSummary{RoleARN: "arn:aws:iam::123456789012:role/CloudPAM", HasExternalID: true},
			PayloadEncrypted: payload,
		}
	}

	t.Run("get without a credential is ErrNotFound", func(t *testing.T) {
		a := newAccount(t, "aws:cred-none")
		if _, err := s.GetAccountCredential(ctx, a.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetAccountCredential() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("put on a missing account is ErrNotFound", func(t *testing.T) {
		if _, err := s.PutAccountCredential(ctx, sealed(999999, "c0")); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("PutAccountCredential() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("put on a deleted account is ErrNotFound", func(t *testing.T) {
		a := newAccount(t, "aws:cred-deleted")
		if ok, err := s.DeleteAccount(ctx, a.ID); err != nil || !ok {
			t.Fatalf("DeleteAccount() = %v, %v", ok, err)
		}
		if _, err := s.PutAccountCredential(ctx, sealed(a.ID, "c0")); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("PutAccountCredential() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("put, get, record, replace, conflict, delete", func(t *testing.T) {
		a := newAccount(t, "aws:cred-lifecycle")
		first, err := s.PutAccountCredential(ctx, sealed(a.ID, "cipher-1"))
		if err != nil {
			t.Fatalf("PutAccountCredential() error = %v", err)
		}
		if first.AccountID != a.ID || first.Type != domain.CredentialTypeAWSAssumeRole || first.PayloadEncrypted != "cipher-1" {
			t.Fatalf("put returned %+v", first)
		}
		if first.Summary.RoleARN != "arn:aws:iam::123456789012:role/CloudPAM" || !first.Summary.HasExternalID {
			t.Fatalf("summary not stored: %+v", first.Summary)
		}
		if first.LastTest != nil {
			t.Fatalf("new credential has LastTest %+v, want nil", first.LastTest)
		}
		if first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
			t.Fatalf("timestamps not stamped: %+v", first.AccountCredential)
		}

		got, err := s.GetAccountCredential(ctx, a.ID)
		if err != nil {
			t.Fatalf("GetAccountCredential() error = %v", err)
		}
		if got.PayloadEncrypted != "cipher-1" || got.Summary != first.Summary || !got.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("get returned %+v, want %+v", got, first)
		}

		testedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
		test := domain.CredentialTest{TestedAt: testedAt, Status: domain.CredentialTestFailed, Error: "assume_role: AccessDenied"}
		if err := s.RecordAccountCredentialTest(ctx, a.ID, "cipher-1", test); err != nil {
			t.Fatalf("RecordAccountCredentialTest() error = %v", err)
		}
		got, err = s.GetAccountCredential(ctx, a.ID)
		if err != nil {
			t.Fatalf("GetAccountCredential() after test error = %v", err)
		}
		if got.LastTest == nil {
			t.Fatal("LastTest = nil after RecordAccountCredentialTest")
		}
		if !got.LastTest.TestedAt.Equal(testedAt) || got.LastTest.Status != domain.CredentialTestFailed || got.LastTest.Error != test.Error {
			t.Fatalf("LastTest = %+v, want %+v", *got.LastTest, test)
		}

		if err := s.RecordAccountCredentialTest(ctx, a.ID, "cipher-stale", test); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("RecordAccountCredentialTest() with stale ciphertext error = %v, want ErrConflict", err)
		}

		replaced, err := s.PutAccountCredential(ctx, domain.SealedAccountCredential{
			AccountID:        a.ID,
			Type:             domain.CredentialTypeGCPServiceAccount,
			Summary:          domain.CredentialSummary{ClientEmail: "sa@proj.iam.gserviceaccount.com", ProjectID: "proj"},
			PayloadEncrypted: "cipher-2",
		})
		if err != nil {
			t.Fatalf("replace error = %v", err)
		}
		if replaced.LastTest != nil {
			t.Fatalf("replace kept LastTest %+v, want cleared", replaced.LastTest)
		}
		if replaced.Type != domain.CredentialTypeGCPServiceAccount || replaced.PayloadEncrypted != "cipher-2" || replaced.Summary.ClientEmail != "sa@proj.iam.gserviceaccount.com" {
			t.Fatalf("replace returned %+v", replaced)
		}
		if replaced.Summary.RoleARN != "" || replaced.Summary.HasExternalID {
			t.Fatalf("replace kept the old summary fields: %+v", replaced.Summary)
		}
		if !replaced.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("replace moved CreatedAt from %v to %v", first.CreatedAt, replaced.CreatedAt)
		}
		if replaced.UpdatedAt.Before(first.UpdatedAt) {
			t.Fatalf("replace moved UpdatedAt backwards: %v -> %v", first.UpdatedAt, replaced.UpdatedAt)
		}

		if err := s.RecordAccountCredentialTest(ctx, a.ID, "cipher-1", test); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("RecordAccountCredentialTest() against the replaced ciphertext error = %v, want ErrConflict", err)
		}
		if err := s.RecordAccountCredentialTest(ctx, a.ID, "cipher-2", domain.CredentialTest{TestedAt: testedAt, Status: domain.CredentialTestOK}); err != nil {
			t.Fatalf("RecordAccountCredentialTest() ok error = %v", err)
		}
		got, _ = s.GetAccountCredential(ctx, a.ID)
		if got.LastTest == nil || got.LastTest.Status != domain.CredentialTestOK || got.LastTest.Error != "" {
			t.Fatalf("LastTest after ok = %+v", got.LastTest)
		}

		ok, err := s.DeleteAccountCredential(ctx, a.ID)
		if err != nil || !ok {
			t.Fatalf("DeleteAccountCredential() = %v, %v, want true", ok, err)
		}
		if _, err := s.GetAccountCredential(ctx, a.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetAccountCredential() after delete error = %v, want ErrNotFound", err)
		}
		ok, err = s.DeleteAccountCredential(ctx, a.ID)
		if err != nil || ok {
			t.Fatalf("second DeleteAccountCredential() = %v, %v, want false", ok, err)
		}
		if err := s.RecordAccountCredentialTest(ctx, a.ID, "cipher-2", test); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("RecordAccountCredentialTest() after delete error = %v, want ErrConflict", err)
		}
	})

	t.Run("account delete removes the credential", func(t *testing.T) {
		a := newAccount(t, "aws:cred-acct-delete")
		if _, err := s.PutAccountCredential(ctx, sealed(a.ID, "c1")); err != nil {
			t.Fatalf("put: %v", err)
		}
		if ok, err := s.DeleteAccount(ctx, a.ID); err != nil || !ok {
			t.Fatalf("DeleteAccount() = %v, %v", ok, err)
		}
		if _, err := s.GetAccountCredential(ctx, a.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetAccountCredential() after account delete error = %v, want ErrNotFound", err)
		}
		if ok, _ := s.DeleteAccountCredential(ctx, a.ID); ok {
			t.Fatal("credential row survived the account delete")
		}
	})

	t.Run("account cascade delete removes the credential", func(t *testing.T) {
		a := newAccount(t, "aws:cred-acct-cascade")
		if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "p", CIDR: "10.250.0.0/16", AccountID: &a.ID}); err != nil {
			t.Fatalf("create pool: %v", err)
		}
		if _, err := s.PutAccountCredential(ctx, sealed(a.ID, "c1")); err != nil {
			t.Fatalf("put: %v", err)
		}
		if ok, err := s.DeleteAccountCascade(ctx, a.ID); err != nil || !ok {
			t.Fatalf("DeleteAccountCascade() = %v, %v", ok, err)
		}
		if _, err := s.GetAccountCredential(ctx, a.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetAccountCredential() after cascade error = %v, want ErrNotFound", err)
		}
		if ok, _ := s.DeleteAccountCredential(ctx, a.ID); ok {
			t.Fatal("credential row survived the cascade delete")
		}
	})
}
