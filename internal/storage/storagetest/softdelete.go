package storagetest

import (
	"context"
	"errors"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// RunSoftDeleteReuseSuite checks that a soft-deleted account key or pool CIDR is
// free for reuse while a live duplicate is still a conflict.
func RunSoftDeleteReuseSuite(t *testing.T, s storage.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("account key is reusable after soft delete", func(t *testing.T) {
		const key = "aws:softdelete-reuse-268"
		first, err := s.CreateAccount(ctx, domain.CreateAccount{Key: key, Name: "first"})
		if err != nil {
			t.Fatalf("CreateAccount(first): %v", err)
		}
		if ok, err := s.DeleteAccount(ctx, first.ID); err != nil || !ok {
			t.Fatalf("DeleteAccount() = %v, %v", ok, err)
		}
		second, err := s.CreateAccount(ctx, domain.CreateAccount{Key: key, Name: "second"})
		if err != nil {
			t.Fatalf("CreateAccount(second) after soft delete: %v", err)
		}
		if second.ID == first.ID {
			t.Fatalf("recreated account reused id %d", first.ID)
		}
		got, err := s.GetAccountByKey(ctx, key)
		if err != nil {
			t.Fatalf("GetAccountByKey(): %v", err)
		}
		if got == nil || got.ID != second.ID {
			t.Fatalf("GetAccountByKey() = %+v, want id %d", got, second.ID)
		}
		if _, err := s.CreateAccount(ctx, domain.CreateAccount{Key: key, Name: "third"}); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("CreateAccount(live duplicate) error = %v, want ErrConflict", err)
		}
	})

	t.Run("pool cidr is reusable after soft delete", func(t *testing.T) {
		root, err := s.CreatePool(ctx, domain.CreatePool{Name: "root", CIDR: "100.68.0.0/16"})
		if err != nil {
			t.Fatalf("CreatePool(root): %v", err)
		}
		child, err := s.CreatePool(ctx, domain.CreatePool{Name: "child", CIDR: "100.68.1.0/24", ParentID: &root.ID})
		if err != nil {
			t.Fatalf("CreatePool(child): %v", err)
		}
		if ok, err := s.DeletePool(ctx, child.ID); err != nil || !ok {
			t.Fatalf("DeletePool(child) = %v, %v", ok, err)
		}
		if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "child again", CIDR: child.CIDR, ParentID: &root.ID}); err != nil {
			t.Fatalf("CreatePool(child) after soft delete: %v", err)
		}
		if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "child dup", CIDR: child.CIDR, ParentID: &root.ID}); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("CreatePool(live child duplicate) error = %v, want ErrConflict", err)
		}

		if ok, err := s.DeletePoolCascade(ctx, root.ID); err != nil || !ok {
			t.Fatalf("DeletePoolCascade(root) = %v, %v", ok, err)
		}
		if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "root again", CIDR: root.CIDR}); err != nil {
			t.Fatalf("CreatePool(root) after soft delete: %v", err)
		}
		if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "root dup", CIDR: root.CIDR}); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("CreatePool(live root duplicate) error = %v, want ErrConflict", err)
		}
	})
}
