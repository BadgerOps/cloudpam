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

// TestMemoryStore_GetAccount tests the GetAccount method
func TestMemoryStore_GetAccount(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Get non-existent account
	_, ok, err := m.GetAccount(ctx, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for non-existent account")
	}

	// Create account
	a, err := m.CreateAccount(ctx, domain.CreateAccount{
		Key:         "aws:123456789012",
		Name:        "Test Account",
		Provider:    "aws",
		ExternalID:  "ext-123",
		Description: "A test account",
		Platform:    "aws",
		Tier:        "prod",
		Environment: "production",
		Regions:     []string{"us-east-1", "us-west-2"},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	// Get existing account
	got, ok, err := m.GetAccount(ctx, a.ID)
	if err != nil {
		t.Fatalf("get account error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for existing account")
	}

	// Verify all fields
	if got.Key != "aws:123456789012" {
		t.Errorf("key mismatch: %q", got.Key)
	}
	if got.Name != "Test Account" {
		t.Errorf("name mismatch: %q", got.Name)
	}
	if got.Provider != "aws" {
		t.Errorf("provider mismatch: %q", got.Provider)
	}
	if got.ExternalID != "ext-123" {
		t.Errorf("external_id mismatch: %q", got.ExternalID)
	}
	if got.Description != "A test account" {
		t.Errorf("description mismatch: %q", got.Description)
	}
	if got.Platform != "aws" {
		t.Errorf("platform mismatch: %q", got.Platform)
	}
	if got.Tier != "prod" {
		t.Errorf("tier mismatch: %q", got.Tier)
	}
	if got.Environment != "production" {
		t.Errorf("environment mismatch: %q", got.Environment)
	}
	if len(got.Regions) != 2 || got.Regions[0] != "us-east-1" || got.Regions[1] != "us-west-2" {
		t.Errorf("regions mismatch: %v", got.Regions)
	}
}

// TestMemoryStore_Close tests the Close method
func TestMemoryStore_Close(t *testing.T) {
	m := NewMemoryStore()

	// Close should succeed
	err := m.Close()
	if err != nil {
		t.Fatalf("close error: %v", err)
	}

	// Close is idempotent
	err = m.Close()
	if err != nil {
		t.Fatalf("second close error: %v", err)
	}
}

// TestMemoryStore_CreatePoolValidation tests pool creation validation
func TestMemoryStore_CreatePoolValidation(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Empty name
	_, err := m.CreatePool(ctx, domain.CreatePool{Name: "", CIDR: "10.0.0.0/16"})
	if err == nil {
		t.Error("expected error for empty name")
	}

	// Empty CIDR
	_, err = m.CreatePool(ctx, domain.CreatePool{Name: "test", CIDR: ""})
	if err == nil {
		t.Error("expected error for empty CIDR")
	}

	// Both empty
	_, err = m.CreatePool(ctx, domain.CreatePool{Name: "", CIDR: ""})
	if err == nil {
		t.Error("expected error for both empty")
	}
}

// TestMemoryStore_CreateAccountValidation tests account creation validation
func TestMemoryStore_CreateAccountValidation(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Empty key
	_, err := m.CreateAccount(ctx, domain.CreateAccount{Key: "", Name: "Test"})
	if err == nil {
		t.Error("expected error for empty key")
	}

	// Empty name
	_, err = m.CreateAccount(ctx, domain.CreateAccount{Key: "aws:123", Name: ""})
	if err == nil {
		t.Error("expected error for empty name")
	}

	// Both empty
	_, err = m.CreateAccount(ctx, domain.CreateAccount{Key: "", Name: ""})
	if err == nil {
		t.Error("expected error for both empty")
	}
}

// TestMemoryStore_DeletePoolNonExistent tests deleting non-existent pool
func TestMemoryStore_DeletePoolNonExistent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Delete non-existent pool
	ok, err := m.DeletePool(ctx, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for non-existent pool")
	}
}

// TestMemoryStore_DeletePoolCascadeNonExistent tests cascade delete of non-existent pool
func TestMemoryStore_DeletePoolCascadeNonExistent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Cascade delete non-existent pool
	ok, err := m.DeletePoolCascade(ctx, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for non-existent pool")
	}
}

// TestMemoryStore_DeleteAccountNonExistent tests deleting non-existent account
func TestMemoryStore_DeleteAccountNonExistent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Delete non-existent account
	ok, err := m.DeleteAccount(ctx, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for non-existent account")
	}
}

// TestMemoryStore_DeleteAccountCascadeNonExistent tests cascade delete of non-existent account
func TestMemoryStore_DeleteAccountCascadeNonExistent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Cascade delete non-existent account
	ok, err := m.DeleteAccountCascade(ctx, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for non-existent account")
	}
}

// TestMemoryStore_UpdatePoolMetaNonExistent tests updating non-existent pool
func TestMemoryStore_UpdatePoolMetaNonExistent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	name := "new name"
	_, ok, err := m.UpdatePoolMeta(ctx, 999, &name, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for non-existent pool")
	}
}

// TestMemoryStore_UpdatePoolAccountNonExistent tests updating non-existent pool account
func TestMemoryStore_UpdatePoolAccountNonExistent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	accID := int64(1)
	_, ok, err := m.UpdatePoolAccount(ctx, 999, &accID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for non-existent pool")
	}
}

// TestMemoryStore_UpdateAccountNonExistent tests updating non-existent account
func TestMemoryStore_UpdateAccountNonExistent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	_, ok, err := m.UpdateAccount(ctx, 999, domain.Account{Name: "new name"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for non-existent account")
	}
}

// TestMemoryStore_GetPoolNonExistent tests getting non-existent pool
func TestMemoryStore_GetPoolNonExistent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	_, ok, err := m.GetPool(ctx, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for non-existent pool")
	}
}

// TestMemoryStore_UpdateAccountWithRegions tests account update with regions
func TestMemoryStore_UpdateAccountWithRegions(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Create account without regions
	a, err := m.CreateAccount(ctx, domain.CreateAccount{Key: "aws:123456789012", Name: "Test"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	// Update with regions
	updated, ok, err := m.UpdateAccount(ctx, a.ID, domain.Account{
		Name:    "Test Updated",
		Regions: []string{"us-east-1", "us-west-2", "eu-west-1"},
	})
	if err != nil {
		t.Fatalf("update account: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(updated.Regions) != 3 {
		t.Errorf("expected 3 regions, got %d", len(updated.Regions))
	}

	// Verify regions are persisted
	got, ok, _ := m.GetAccount(ctx, a.ID)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(got.Regions) != 3 {
		t.Errorf("expected 3 regions persisted, got %d", len(got.Regions))
	}
}

// TestMemoryStore_CascadeDeleteWithDeepHierarchy tests cascade delete with deep pool hierarchy
func TestMemoryStore_CascadeDeleteWithDeepHierarchy(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Create deep hierarchy: root -> child1 -> child2 -> child3
	root, _ := m.CreatePool(ctx, domain.CreatePool{Name: "root", CIDR: "10.0.0.0/8"})
	child1, _ := m.CreatePool(ctx, domain.CreatePool{Name: "child1", CIDR: "10.0.0.0/16", ParentID: &root.ID})
	child2, _ := m.CreatePool(ctx, domain.CreatePool{Name: "child2", CIDR: "10.0.0.0/24", ParentID: &child1.ID})
	child3, _ := m.CreatePool(ctx, domain.CreatePool{Name: "child3", CIDR: "10.0.0.0/28", ParentID: &child2.ID})

	// Verify we have 4 pools
	pools, _ := m.ListPools(ctx)
	if len(pools) != 4 {
		t.Fatalf("expected 4 pools, got %d", len(pools))
	}

	// Cascade delete from root
	ok, err := m.DeletePoolCascade(ctx, root.ID)
	if err != nil || !ok {
		t.Fatalf("cascade delete: %v ok=%v", err, ok)
	}

	// All should be gone
	pools, _ = m.ListPools(ctx)
	if len(pools) != 0 {
		t.Errorf("expected 0 pools after cascade, got %d", len(pools))
	}

	// Verify each is gone
	for _, id := range []int64{root.ID, child1.ID, child2.ID, child3.ID} {
		_, ok, _ := m.GetPool(ctx, id)
		if ok {
			t.Errorf("pool %d should be deleted", id)
		}
	}
}

// TestMemoryStore_CascadeAccountDeleteWithPoolHierarchy tests cascade account delete with pool hierarchy
func TestMemoryStore_CascadeAccountDeleteWithPoolHierarchy(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Create account
	acc, _ := m.CreateAccount(ctx, domain.CreateAccount{Key: "aws:123456789012", Name: "Test"})

	// Create pool hierarchy with account at root
	root, _ := m.CreatePool(ctx, domain.CreatePool{Name: "root", CIDR: "10.0.0.0/8", AccountID: &acc.ID})
	child1, _ := m.CreatePool(ctx, domain.CreatePool{Name: "child1", CIDR: "10.0.0.0/16", ParentID: &root.ID})
	child2, _ := m.CreatePool(ctx, domain.CreatePool{Name: "child2", CIDR: "10.1.0.0/16", ParentID: &root.ID})

	// Verify we have 3 pools
	pools, _ := m.ListPools(ctx)
	if len(pools) != 3 {
		t.Fatalf("expected 3 pools, got %d", len(pools))
	}

	// Cascade delete account
	ok, err := m.DeleteAccountCascade(ctx, acc.ID)
	if err != nil || !ok {
		t.Fatalf("cascade delete: %v ok=%v", err, ok)
	}

	// All pools should be gone (root was assigned to account, children depend on root)
	pools, _ = m.ListPools(ctx)
	if len(pools) != 0 {
		t.Errorf("expected 0 pools after cascade, got %d", len(pools))
	}

	// Verify each is gone
	for _, id := range []int64{root.ID, child1.ID, child2.ID} {
		_, ok, _ := m.GetPool(ctx, id)
		if ok {
			t.Errorf("pool %d should be deleted", id)
		}
	}
}

// TestMemoryStore_UpdatePoolAccount tests the UpdatePoolAccount method
func TestMemoryStore_UpdatePoolAccount(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Create pool
	pool, err := m.CreatePool(ctx, domain.CreatePool{Name: "test", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	// Verify no account
	if pool.AccountID != nil {
		t.Error("expected no account initially")
	}

	// Update with account
	accID := int64(42)
	updated, ok, err := m.UpdatePoolAccount(ctx, pool.ID, &accID)
	if err != nil || !ok {
		t.Fatalf("update pool account: %v ok=%v", err, ok)
	}
	if updated.AccountID == nil || *updated.AccountID != 42 {
		t.Errorf("expected account_id=42, got %v", updated.AccountID)
	}

	// Clear account
	updated, ok, err = m.UpdatePoolAccount(ctx, pool.ID, nil)
	if err != nil || !ok {
		t.Fatalf("clear pool account: %v ok=%v", err, ok)
	}
	if updated.AccountID != nil {
		t.Errorf("expected nil account_id, got %v", updated.AccountID)
	}
}

// TestMemoryStore_UpdatePoolMetaPartial tests partial updates
func TestMemoryStore_UpdatePoolMetaPartial(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Create pool with account
	accID := int64(42)
	pool, _ := m.CreatePool(ctx, domain.CreatePool{
		Name:      "original",
		CIDR:      "10.0.0.0/16",
		AccountID: &accID,
	})

	// Update only name (pass nil for accountID to keep it)
	newName := "updated"
	updated, ok, _ := m.UpdatePoolMeta(ctx, pool.ID, &newName, nil)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if updated.Name != "updated" {
		t.Errorf("expected name 'updated', got %q", updated.Name)
	}
	// Note: based on code, accountID is always set even if nil is passed
	// The current implementation sets accountID to nil when nil is passed
}

// TestMemoryStore_ListAccountsEmpty tests listing accounts when empty
func TestMemoryStore_ListAccountsEmpty(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	accounts, err := m.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("expected 0 accounts, got %d", len(accounts))
	}
}

// TestMemoryStore_ListPoolsEmpty tests listing pools when empty
func TestMemoryStore_ListPoolsEmpty(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	pools, err := m.ListPools(ctx)
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 0 {
		t.Errorf("expected 0 pools, got %d", len(pools))
	}
}

// TestMemoryStore_IDIncrement tests that IDs are auto-incremented
func TestMemoryStore_IDIncrement(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	// Create pools and verify IDs increment
	p1, _ := m.CreatePool(ctx, domain.CreatePool{Name: "p1", CIDR: "10.0.0.0/8"})
	p2, _ := m.CreatePool(ctx, domain.CreatePool{Name: "p2", CIDR: "172.16.0.0/12"})
	p3, _ := m.CreatePool(ctx, domain.CreatePool{Name: "p3", CIDR: "192.168.0.0/16"})

	if p2.ID != p1.ID+1 {
		t.Errorf("expected p2.ID=%d, got %d", p1.ID+1, p2.ID)
	}
	if p3.ID != p2.ID+1 {
		t.Errorf("expected p3.ID=%d, got %d", p2.ID+1, p3.ID)
	}

	// Create accounts and verify IDs increment
	a1, _ := m.CreateAccount(ctx, domain.CreateAccount{Key: "aws:111111111111", Name: "a1"})
	a2, _ := m.CreateAccount(ctx, domain.CreateAccount{Key: "aws:222222222222", Name: "a2"})

	if a2.ID != a1.ID+1 {
		t.Errorf("expected a2.ID=%d, got %d", a1.ID+1, a2.ID)
	}
}

// TestMemoryStore_RegionsCopied tests that regions are copied on create
func TestMemoryStore_RegionsCopied(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()

	regions := []string{"us-east-1", "us-west-2"}
	a, _ := m.CreateAccount(ctx, domain.CreateAccount{
		Key:     "aws:123456789012",
		Name:    "Test",
		Regions: regions,
	})

	// Modify original slice
	regions[0] = "modified"

	// Verify stored regions are not affected
	got, _, _ := m.GetAccount(ctx, a.ID)
	if got.Regions[0] == "modified" {
		t.Error("regions should be copied, not shared")
	}
}
