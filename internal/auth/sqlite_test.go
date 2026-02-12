//go:build sqlite

package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE api_keys (
			id TEXT PRIMARY KEY,
			prefix TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			hash BLOB NOT NULL,
			salt BLOB NOT NULL,
			scopes TEXT NOT NULL DEFAULT '[]',
			created_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP,
			last_used_at TIMESTAMP,
			revoked INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX idx_api_keys_prefix ON api_keys(prefix);
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestSQLiteKeyStore_CRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteKeyStoreFromDB(db)
	ctx := context.Background()

	// Generate a key
	plaintext, apiKey, err := GenerateAPIKey(GenerateAPIKeyOptions{
		Name:   "Test Key",
		Scopes: []string{"pools:read", "accounts:write"},
	})
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}

	// Create
	if err := store.Create(ctx, apiKey); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// GetByPrefix
	got, err := store.GetByPrefix(ctx, apiKey.Prefix)
	if err != nil {
		t.Fatalf("GetByPrefix: %v", err)
	}
	if got == nil {
		t.Fatal("GetByPrefix returned nil")
	}
	if got.ID != apiKey.ID {
		t.Errorf("ID: got %q, want %q", got.ID, apiKey.ID)
	}
	if got.Name != "Test Key" {
		t.Errorf("Name: got %q, want %q", got.Name, "Test Key")
	}
	if len(got.Scopes) != 2 {
		t.Errorf("Scopes: got %d, want 2", len(got.Scopes))
	}

	// Validate key roundtrip
	if err := ValidateAPIKey(plaintext, got); err != nil {
		t.Errorf("ValidateAPIKey after roundtrip: %v", err)
	}

	// GetByID
	byID, err := store.GetByID(ctx, apiKey.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if byID == nil {
		t.Fatal("GetByID returned nil")
	}
	if byID.Prefix != apiKey.Prefix {
		t.Errorf("Prefix: got %q, want %q", byID.Prefix, apiKey.Prefix)
	}

	// List
	keys, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("List: got %d keys, want 1", len(keys))
	}
	// List should not include hash/salt
	if keys[0].Hash != nil {
		t.Error("List should not include hash")
	}
	if keys[0].Salt != nil {
		t.Error("List should not include salt")
	}

	// UpdateLastUsed
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := store.UpdateLastUsed(ctx, apiKey.ID, now); err != nil {
		t.Fatalf("UpdateLastUsed: %v", err)
	}
	updated, _ := store.GetByID(ctx, apiKey.ID)
	if updated.LastUsedAt == nil {
		t.Fatal("LastUsedAt should not be nil after update")
	}

	// Revoke
	if err := store.Revoke(ctx, apiKey.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	revoked, _ := store.GetByID(ctx, apiKey.ID)
	if !revoked.Revoked {
		t.Error("key should be revoked")
	}

	// Delete
	if err := store.Delete(ctx, apiKey.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deleted, _ := store.GetByID(ctx, apiKey.ID)
	if deleted != nil {
		t.Error("key should be nil after delete")
	}
}

func TestSQLiteKeyStore_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteKeyStoreFromDB(db)
	ctx := context.Background()

	// GetByPrefix not found
	got, err := store.GetByPrefix(ctx, "nonexistent")
	if err != nil {
		t.Errorf("GetByPrefix error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent prefix")
	}

	// GetByID not found
	got, err = store.GetByID(ctx, "nonexistent")
	if err != nil {
		t.Errorf("GetByID error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent ID")
	}

	// Revoke not found
	if err := store.Revoke(ctx, "nonexistent"); err != ErrKeyNotFound {
		t.Errorf("Revoke: got %v, want ErrKeyNotFound", err)
	}

	// UpdateLastUsed not found
	if err := store.UpdateLastUsed(ctx, "nonexistent", time.Now()); err != ErrKeyNotFound {
		t.Errorf("UpdateLastUsed: got %v, want ErrKeyNotFound", err)
	}

	// Delete not found
	if err := store.Delete(ctx, "nonexistent"); err != ErrKeyNotFound {
		t.Errorf("Delete: got %v, want ErrKeyNotFound", err)
	}
}

func TestSQLiteKeyStore_CreateNil(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteKeyStoreFromDB(db)
	if err := store.Create(context.Background(), nil); err != ErrKeyNotFound {
		t.Errorf("Create(nil): got %v, want ErrKeyNotFound", err)
	}
}

func TestSQLiteKeyStore_DuplicatePrefix(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteKeyStoreFromDB(db)
	ctx := context.Background()

	_, key1, _ := GenerateAPIKey(GenerateAPIKeyOptions{Name: "Key 1", Scopes: []string{"*"}})
	if err := store.Create(ctx, key1); err != nil {
		t.Fatalf("Create key1: %v", err)
	}

	// Force duplicate prefix
	_, key2, _ := GenerateAPIKey(GenerateAPIKeyOptions{Name: "Key 2", Scopes: []string{"*"}})
	key2.Prefix = key1.Prefix

	err := store.Create(ctx, key2)
	if err != ErrInvalidKeyFormat {
		t.Errorf("Create duplicate prefix: got %v, want ErrInvalidKeyFormat", err)
	}
}

func TestSQLiteKeyStore_WithExpiration(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteKeyStoreFromDB(db)
	ctx := context.Background()

	expires := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Millisecond)
	_, apiKey, _ := GenerateAPIKey(GenerateAPIKeyOptions{
		Name:      "Expiring Key",
		Scopes:    []string{"pools:read"},
		ExpiresAt: &expires,
	})

	if err := store.Create(ctx, apiKey); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetByID(ctx, apiKey.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil")
	}
}

func TestSQLiteKeyStore_MultipleKeys(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteKeyStoreFromDB(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, apiKey, _ := GenerateAPIKey(GenerateAPIKeyOptions{
			Name:   "Key",
			Scopes: []string{"pools:read"},
		})
		if err := store.Create(ctx, apiKey); err != nil {
			t.Fatalf("Create key %d: %v", i, err)
		}
	}

	keys, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 5 {
		t.Errorf("List: got %d keys, want 5", len(keys))
	}
}
