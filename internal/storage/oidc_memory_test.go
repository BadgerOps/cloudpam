package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

func newTestProvider(id, name, issuer string) *domain.OIDCProvider {
	now := time.Now().UTC()
	return &domain.OIDCProvider{
		ID:                    id,
		Name:                  name,
		IssuerURL:             issuer,
		ClientID:              "client-" + id,
		ClientSecretEncrypted: "secret-encrypted",
		Scopes:                "openid profile email",
		RoleMapping:           map[string]string{"admin": "admin"},
		DefaultRole:           "viewer",
		AutoProvision:         true,
		Enabled:               true,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func TestOIDCMemoryStore_CreateAndGet(t *testing.T) {
	store := NewMemoryOIDCProviderStore()
	ctx := context.Background()

	p := newTestProvider("p1", "Test IdP", "https://idp.example.com")
	if err := store.CreateProvider(ctx, p); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	got, err := store.GetProvider(ctx, "p1")
	if err != nil {
		t.Fatalf("GetProvider: %v", err)
	}
	if got.ID != "p1" || got.Name != "Test IdP" || got.IssuerURL != "https://idp.example.com" {
		t.Errorf("unexpected provider: %+v", got)
	}
	if got.ClientID != "client-p1" {
		t.Errorf("expected ClientID=client-p1, got %s", got.ClientID)
	}

	// Verify deep copy: mutating returned value should not affect stored value.
	got.Name = "Mutated"
	got2, _ := store.GetProvider(ctx, "p1")
	if got2.Name != "Test IdP" {
		t.Error("GetProvider did not return a deep copy")
	}

	// Not found case.
	_, err = store.GetProvider(ctx, "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestOIDCMemoryStore_ListProviders(t *testing.T) {
	store := NewMemoryOIDCProviderStore()
	ctx := context.Background()

	_ = store.CreateProvider(ctx, newTestProvider("p1", "IdP 1", "https://idp1.example.com"))
	_ = store.CreateProvider(ctx, newTestProvider("p2", "IdP 2", "https://idp2.example.com"))

	list, err := store.ListProviders(ctx)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 providers, got %d", len(list))
	}
}

func TestOIDCMemoryStore_ListEnabledProviders(t *testing.T) {
	store := NewMemoryOIDCProviderStore()
	ctx := context.Background()

	p1 := newTestProvider("p1", "Enabled IdP", "https://idp1.example.com")
	p1.Enabled = true
	p2 := newTestProvider("p2", "Disabled IdP", "https://idp2.example.com")
	p2.Enabled = false

	_ = store.CreateProvider(ctx, p1)
	_ = store.CreateProvider(ctx, p2)

	enabled, err := store.ListEnabledProviders(ctx)
	if err != nil {
		t.Fatalf("ListEnabledProviders: %v", err)
	}
	if len(enabled) != 1 {
		t.Errorf("expected 1 enabled provider, got %d", len(enabled))
	}
	if enabled[0].ID != "p1" {
		t.Errorf("expected enabled provider p1, got %s", enabled[0].ID)
	}
}

func TestOIDCMemoryStore_Update(t *testing.T) {
	store := NewMemoryOIDCProviderStore()
	ctx := context.Background()

	p := newTestProvider("p1", "Original", "https://idp.example.com")
	_ = store.CreateProvider(ctx, p)

	// Update name and issuer URL.
	p.Name = "Updated"
	p.IssuerURL = "https://new-idp.example.com"
	if err := store.UpdateProvider(ctx, p); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}

	got, _ := store.GetProvider(ctx, "p1")
	if got.Name != "Updated" {
		t.Errorf("expected name Updated, got %s", got.Name)
	}
	if got.IssuerURL != "https://new-idp.example.com" {
		t.Errorf("expected new issuer URL, got %s", got.IssuerURL)
	}

	// Verify old issuer URL is removed from index.
	_, err := store.GetProviderByIssuer(ctx, "https://idp.example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Error("old issuer URL should not be found")
	}

	// Verify new issuer URL is in index.
	got2, err := store.GetProviderByIssuer(ctx, "https://new-idp.example.com")
	if err != nil {
		t.Fatalf("GetProviderByIssuer: %v", err)
	}
	if got2.ID != "p1" {
		t.Errorf("expected p1, got %s", got2.ID)
	}

	// Update nonexistent provider.
	missing := newTestProvider("missing", "Nope", "https://missing.example.com")
	if err := store.UpdateProvider(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestOIDCMemoryStore_Delete(t *testing.T) {
	store := NewMemoryOIDCProviderStore()
	ctx := context.Background()

	p := newTestProvider("p1", "Delete Me", "https://idp.example.com")
	_ = store.CreateProvider(ctx, p)

	if err := store.DeleteProvider(ctx, "p1"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	_, err := store.GetProvider(ctx, "p1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}

	// Issuer index should also be cleaned up.
	_, err = store.GetProviderByIssuer(ctx, "https://idp.example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Error("issuer index should be cleaned up after delete")
	}

	// Delete nonexistent.
	if err := store.DeleteProvider(ctx, "nonexistent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestOIDCMemoryStore_DuplicateIssuer(t *testing.T) {
	store := NewMemoryOIDCProviderStore()
	ctx := context.Background()

	p1 := newTestProvider("p1", "First", "https://idp.example.com")
	_ = store.CreateProvider(ctx, p1)

	// Create with same issuer URL should fail.
	p2 := newTestProvider("p2", "Second", "https://idp.example.com")
	err := store.CreateProvider(ctx, p2)
	if !errors.Is(err, ErrDuplicateIssuer) {
		t.Errorf("expected ErrDuplicateIssuer, got %v", err)
	}

	// Update to collide with existing issuer should fail.
	p3 := newTestProvider("p3", "Third", "https://idp3.example.com")
	_ = store.CreateProvider(ctx, p3)

	p3.IssuerURL = "https://idp.example.com"
	err = store.UpdateProvider(ctx, p3)
	if !errors.Is(err, ErrDuplicateIssuer) {
		t.Errorf("expected ErrDuplicateIssuer on update, got %v", err)
	}
}

func TestOIDCMemoryStore_GetByIssuer(t *testing.T) {
	store := NewMemoryOIDCProviderStore()
	ctx := context.Background()

	p := newTestProvider("p1", "Test IdP", "https://idp.example.com")
	_ = store.CreateProvider(ctx, p)

	got, err := store.GetProviderByIssuer(ctx, "https://idp.example.com")
	if err != nil {
		t.Fatalf("GetProviderByIssuer: %v", err)
	}
	if got.ID != "p1" {
		t.Errorf("expected p1, got %s", got.ID)
	}

	// Not found.
	_, err = store.GetProviderByIssuer(ctx, "https://unknown.example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
