package storagetest

import (
	"context"
	"errors"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// RunRestoreSuite checks that soft-deleted pools and accounts are listed and can be
// restored, and that a restore which would break a live invariant is a conflict.
func RunRestoreSuite(t *testing.T, s storage.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("deleted pool is listed and restored", func(t *testing.T) {
		older := mustPool(t, s, domain.CreatePool{Name: "older", CIDR: "100.70.0.0/24", Description: "kept"})
		newer := mustPool(t, s, domain.CreatePool{Name: "newer", CIDR: "100.70.1.0/24"})
		mustDeletePool(t, s, older.ID)
		mustDeletePool(t, s, newer.ID)

		deleted, err := s.ListDeletedPools(ctx)
		if err != nil {
			t.Fatalf("ListDeletedPools(): %v", err)
		}
		iOlder, iNewer := poolIndex(deleted, older.ID), poolIndex(deleted, newer.ID)
		if iOlder < 0 || iNewer < 0 {
			t.Fatalf("ListDeletedPools() = %+v, want pools %d and %d", deleted, older.ID, newer.ID)
		}
		if iNewer > iOlder {
			t.Fatalf("ListDeletedPools() lists pool %d before the later-deleted pool %d", older.ID, newer.ID)
		}
		if deleted[iOlder].DeletedAt == nil {
			t.Fatalf("listed deleted pool has no deleted_at: %+v", deleted[iOlder])
		}

		got, ok, err := s.RestorePool(ctx, older.ID)
		if err != nil || !ok {
			t.Fatalf("RestorePool() = %v, %v", ok, err)
		}
		if got.DeletedAt != nil || got.CIDR != older.CIDR || got.Description != "kept" {
			t.Fatalf("RestorePool() = %+v, want the live original", got)
		}
		if _, found, err := s.GetPool(ctx, older.ID); err != nil || !found {
			t.Fatalf("GetPool(restored) = %v, %v", found, err)
		}
		deleted, err = s.ListDeletedPools(ctx)
		if err != nil {
			t.Fatalf("ListDeletedPools(): %v", err)
		}
		if poolIndex(deleted, older.ID) >= 0 {
			t.Fatalf("restored pool %d is still listed as deleted", older.ID)
		}
		if _, ok, err := s.RestorePool(ctx, older.ID); err != nil || ok {
			t.Fatalf("RestorePool(live) = %v, %v, want false, nil", ok, err)
		}
		if _, ok, err := s.RestorePool(ctx, 987654321); err != nil || ok {
			t.Fatalf("RestorePool(unknown) = %v, %v, want false, nil", ok, err)
		}
	})

	t.Run("pool under a deleted parent is a conflict until the parent is restored", func(t *testing.T) {
		root := mustPool(t, s, domain.CreatePool{Name: "root", CIDR: "100.71.0.0/16"})
		child := mustPool(t, s, domain.CreatePool{Name: "child", CIDR: "100.71.1.0/24", ParentID: &root.ID})
		if ok, err := s.DeletePoolCascade(ctx, root.ID); err != nil || !ok {
			t.Fatalf("DeletePoolCascade() = %v, %v", ok, err)
		}
		if _, _, err := s.RestorePool(ctx, child.ID); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("RestorePool(child of deleted parent) error = %v, want ErrConflict", err)
		}
		if _, ok, err := s.RestorePool(ctx, root.ID); err != nil || !ok {
			t.Fatalf("RestorePool(root) = %v, %v", ok, err)
		}
		got, ok, err := s.RestorePool(ctx, child.ID)
		if err != nil || !ok {
			t.Fatalf("RestorePool(child) after parent restore = %v, %v", ok, err)
		}
		if got.ParentID == nil || *got.ParentID != root.ID {
			t.Fatalf("restored child parent = %v, want %d", got.ParentID, root.ID)
		}
	})

	t.Run("pool whose cidr is taken again is a conflict", func(t *testing.T) {
		first := mustPool(t, s, domain.CreatePool{Name: "first", CIDR: "100.72.0.0/24"})
		mustDeletePool(t, s, first.ID)
		mustPool(t, s, domain.CreatePool{Name: "second", CIDR: first.CIDR})
		if _, _, err := s.RestorePool(ctx, first.ID); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("RestorePool(cidr taken) error = %v, want ErrConflict", err)
		}
	})

	t.Run("pool whose account is deleted is a conflict", func(t *testing.T) {
		acct := mustAccount(t, s, "aws:restore-pool-account-270")
		p := mustPool(t, s, domain.CreatePool{Name: "owned", CIDR: "100.73.0.0/24", AccountID: &acct.ID})
		mustDeletePool(t, s, p.ID)
		if ok, err := s.DeleteAccount(ctx, acct.ID); err != nil || !ok {
			t.Fatalf("DeleteAccount() = %v, %v", ok, err)
		}
		if _, _, err := s.RestorePool(ctx, p.ID); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("RestorePool(account deleted) error = %v, want ErrConflict", err)
		}
		if _, ok, err := s.RestoreAccount(ctx, acct.ID); err != nil || !ok {
			t.Fatalf("RestoreAccount() = %v, %v", ok, err)
		}
		got, ok, err := s.RestorePool(ctx, p.ID)
		if err != nil || !ok {
			t.Fatalf("RestorePool() after account restore = %v, %v", ok, err)
		}
		if got.AccountID == nil || *got.AccountID != acct.ID {
			t.Fatalf("restored pool account = %v, want %d", got.AccountID, acct.ID)
		}
	})

	t.Run("deleted account is listed and restored unless its key is taken", func(t *testing.T) {
		const key = "aws:restore-account-270"
		first := mustAccount(t, s, key)
		if ok, err := s.DeleteAccount(ctx, first.ID); err != nil || !ok {
			t.Fatalf("DeleteAccount() = %v, %v", ok, err)
		}
		deleted, err := s.ListDeletedAccounts(ctx)
		if err != nil {
			t.Fatalf("ListDeletedAccounts(): %v", err)
		}
		i := accountIndex(deleted, first.ID)
		if i < 0 || deleted[i].DeletedAt == nil || deleted[i].Key != key {
			t.Fatalf("ListDeletedAccounts() = %+v, want account %d with deleted_at", deleted, first.ID)
		}
		got, ok, err := s.RestoreAccount(ctx, first.ID)
		if err != nil || !ok {
			t.Fatalf("RestoreAccount() = %v, %v", ok, err)
		}
		if got.DeletedAt != nil || got.Key != key {
			t.Fatalf("RestoreAccount() = %+v, want the live original", got)
		}
		if byKey, err := s.GetAccountByKey(ctx, key); err != nil || byKey.ID != first.ID {
			t.Fatalf("GetAccountByKey() = %+v, %v, want id %d", byKey, err, first.ID)
		}
		if _, ok, err := s.RestoreAccount(ctx, first.ID); err != nil || ok {
			t.Fatalf("RestoreAccount(live) = %v, %v, want false, nil", ok, err)
		}

		if ok, err := s.DeleteAccount(ctx, first.ID); err != nil || !ok {
			t.Fatalf("DeleteAccount() again = %v, %v", ok, err)
		}
		mustAccount(t, s, key)
		if _, _, err := s.RestoreAccount(ctx, first.ID); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("RestoreAccount(key taken) error = %v, want ErrConflict", err)
		}
	})
}

func mustPool(t *testing.T, s storage.Store, in domain.CreatePool) domain.Pool {
	t.Helper()
	p, err := s.CreatePool(context.Background(), in)
	if err != nil {
		t.Fatalf("CreatePool(%s): %v", in.CIDR, err)
	}
	return p
}

func mustDeletePool(t *testing.T, s storage.Store, id int64) {
	t.Helper()
	if ok, err := s.DeletePool(context.Background(), id); err != nil || !ok {
		t.Fatalf("DeletePool(%d) = %v, %v", id, ok, err)
	}
}

func mustAccount(t *testing.T, s storage.Store, key string) domain.Account {
	t.Helper()
	a, err := s.CreateAccount(context.Background(), domain.CreateAccount{Key: key, Name: key})
	if err != nil {
		t.Fatalf("CreateAccount(%s): %v", key, err)
	}
	return a
}

func poolIndex(ps []domain.Pool, id int64) int {
	for i, p := range ps {
		if p.ID == id {
			return i
		}
	}
	return -1
}

func accountIndex(as []domain.Account, id int64) int {
	for i, a := range as {
		if a.ID == id {
			return i
		}
	}
	return -1
}
