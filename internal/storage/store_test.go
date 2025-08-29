package storage

import (
	"cloudpam/internal/domain"
	"context"
	"testing"
)

func TestMemoryStore_PoolsCRUD(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Create top-level pool
	p1, err := m.CreatePool(ctx, domain.CreatePool{Name: "root", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	// Create child pool
	p2, err := m.CreatePool(ctx, domain.CreatePool{Name: "child", CIDR: "10.0.1.0/24", ParentID: &p1.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	// Get
	got, ok, err := m.GetPool(ctx, p2.ID)
	if err != nil || !ok {
		t.Fatalf("get child failed: %v ok=%v", err, ok)
	}
	if got.Name != "child" {
		t.Fatalf("unexpected name: %s", got.Name)
	}

	// List
	lst, err := m.ListPools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(lst) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(lst))
	}

	// Update account and name
	accID := int64(42)
	upd, ok, err := m.UpdatePoolMeta(ctx, p2.ID, strPtr("child2"), &accID)
	if err != nil || !ok {
		t.Fatalf("update meta: %v ok=%v", err, ok)
	}
	if upd.Name != "child2" || upd.AccountID == nil || *upd.AccountID != accID {
		t.Fatalf("update not applied: %+v", upd)
	}

	// Delete should fail if has child
	if _, err := m.DeletePool(ctx, p1.ID); err == nil {
		t.Fatalf("expected error deleting parent with child")
	}
	// Cascade delete should remove both
	ok, err = m.DeletePoolCascade(ctx, p1.ID)
	if err != nil || !ok {
		t.Fatalf("cascade delete: %v ok=%v", err, ok)
	}
	_, ok, _ = m.GetPool(ctx, p1.ID)
	if ok {
		t.Fatalf("parent still exists after cascade")
	}
	_, ok, _ = m.GetPool(ctx, p2.ID)
	if ok {
		t.Fatalf("child still exists after cascade")
	}
}

func TestMemoryStore_AccountsCRUD(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	a, err := m.CreateAccount(ctx, domain.CreateAccount{Key: "aws:123", Name: "Prod"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if a.ID == 0 {
		t.Fatalf("invalid id")
	}

	// Update
	upd, ok, err := m.UpdateAccount(ctx, a.ID, domain.Account{Name: "Prod-1", Provider: "aws"})
	if err != nil || !ok {
		t.Fatalf("update account: %v ok=%v", err, ok)
	}
	if upd.Name != "Prod-1" || upd.Provider != "aws" {
		t.Fatalf("update not applied: %+v", upd)
	}

	// Attach pools referencing account
	_, err = m.CreatePool(ctx, domain.CreatePool{Name: "root", CIDR: "10.0.0.0/16", AccountID: &a.ID})
	if err != nil {
		t.Fatalf("create pool with account: %v", err)
	}
	// Delete should fail when referenced
	ok, err = m.DeleteAccount(ctx, a.ID)
	if err == nil || ok {
		t.Fatalf("expected failure deleting referenced account")
	}
	// Cascade should delete referencing pools and the account
	ok, err = m.DeleteAccountCascade(ctx, a.ID)
	if err != nil || !ok {
		t.Fatalf("cascade account delete: %v ok=%v", err, ok)
	}
}

func strPtr(s string) *string { return &s }
